package chain

import (
	"errors"
	"testing"
)

func TestReorgToHeavierBranch(t *testing.T) {
	a, w := setup(t)
	b, _ := New("", 8)
	for _, bl := range a.Blocks {
		if st, err := b.ProcessBlock(bl); err != nil || st != Extended {
			t.Fatal(st, err)
		}
	}
	// a: spend on top of the shared block
	bob := &Wallet{Seed: sha([]byte("bob"))}
	tx, err := w.BuildTx(a.UTXOsFor(keys(w)), bob.NewAddress(), Unit, 1000, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SubmitTx(tx); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Produce(sha([]byte("ma"))); err != nil {
		t.Fatal(err)
	}
	if bal(a, bob) != Unit {
		t.Fatal("bob not paid")
	}
	// b: two empty blocks -> more work
	for i := 0; i < 2; i++ {
		if _, err := b.Produce(sha([]byte("mb"))); err != nil {
			t.Fatal(err)
		}
	}
	var last Status
	for _, bl := range b.Blocks[1:] {
		st, err := a.ProcessBlock(bl)
		if err != nil {
			t.Fatal(err)
		}
		last = st
	}
	if last != Reorged || a.TipHash() != b.TipHash() || a.Work() != b.Work() {
		t.Fatal("no reorg", last)
	}
	if bal(a, bob) != 0 || len(a.MempoolTxs()) != 1 {
		t.Fatal("disconnected tx should be back in the mempool", bal(a, bob), len(a.MempoolTxs()))
	}
	// it can be mined again on the new chain
	if _, err := a.Produce(sha([]byte("ma"))); err != nil || bal(a, bob) != Unit {
		t.Fatal("re-mine failed", err)
	}
	// a lighter branch stays a side chain, a duplicate is a duplicate
	if st, _ := a.ProcessBlock(b.Blocks[1]); st != Duplicate {
		t.Fatal(st)
	}
}

func TestRejectsInvalidBranchAndOrphans(t *testing.T) {
	a, _ := setup(t)
	b, _ := New("", 8)
	b.ProcessBlock(a.Blocks[0])
	b.Produce(sha([]byte("x")))
	b.Produce(sha([]byte("x")))
	bad := b.Blocks[2]
	bad.Txs = []Tx{{Outputs: []TxOut{{bad.Txs[0].Outputs[0].Value + 1, bad.Txs[0].Outputs[0].Address}}}} // breaks merkle root
	if _, err := a.ProcessBlock(bad); !errors.Is(err, ErrInvalid) {
		t.Fatal("tampered block accepted", err)
	}
	orphan := b.Blocks[2]
	if _, err := a.ProcessBlock(orphan); !errors.Is(err, ErrOrphan) {
		t.Fatal("expected orphan", err)
	}
	// extending the tip with an overspending coinbase is rejected outright
	if _, err := a.ProcessBlock(mineOn(t, a, func(cb *Tx) { cb.Outputs[0].Value = 1000 * Unit })); !errors.Is(err, ErrInvalid) {
		t.Fatal("overspending coinbase accepted", err)
	}
	// a competing genesis (equal work, stored as side chain) followed by an
	// invalid child: the heavier branch must fail validation and leave state alone
	c, _ := New("", 8)
	g := mineOn(t, c, func(*Tx) {})
	if st, err := a.ProcessBlock(g); err != nil || st != SideChain {
		t.Fatal(st, err)
	}
	evil := mineAfter(g, 1000*Unit)
	if _, err := a.ProcessBlock(evil); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid heavier branch accepted", err)
	}
	if a.Height() != 1 {
		t.Fatal("state changed after failed reorg")
	}
}

// mineOn builds (without applying) a block on c's tip with a modified coinbase.
func mineOn(t *testing.T, c *Chain, mod func(*Tx)) Block {
	prev, poh, height, slot := c.tip()
	cb := Tx{Outputs: []TxOut{{Reward(height), sha([]byte("evil"))}}}
	mod(&cb)
	return seal(Header{Height: height, Prev: prev, Slot: slot, Bits: 8}, poh, []Tx{cb})
}

func mineAfter(p Block, value uint64) Block {
	cb := Tx{Outputs: []TxOut{{value, sha([]byte("evil"))}}}
	return seal(Header{Height: p.Header.Height + 1, Prev: p.Header.Hash(), Slot: p.Header.Slot + 1, Bits: 8}, p.Header.PoH, []Tx{cb})
}

func seal(h Header, poh Hash, txs []Tx) Block {
	h.MerkleRoot = Merkle(txs)
	h.PoH = PoHRun(poh, txs)
	h.PoHTicks = (h.Slot + 1) * TicksPerSlot
	for leadingZeros(h.Hash()) < int(h.Bits) {
		h.Nonce++
	}
	return Block{h, txs}
}
