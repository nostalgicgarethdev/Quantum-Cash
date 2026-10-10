package chain

import (
	"path/filepath"
	"testing"
)

func setup(t *testing.T) (*Chain, *Wallet) {
	c, _ := New(filepath.Join(t.TempDir(), "c.json"), 8)
	w := &Wallet{Seed: sha([]byte("alice"))}
	if _, err := c.Produce(w.NewAddress()); err != nil {
		t.Fatal(err)
	}
	return c, w
}

func TestMineAndSpend(t *testing.T) {
	c, w := setup(t)
	bob := &Wallet{Seed: sha([]byte("bob"))}
	tx, err := w.BuildTx(c.UTXOsFor(keys(w)), bob.NewAddress(), 10*Unit, 1000, "hi")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SubmitTx(tx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SubmitTx(tx); err == nil {
		t.Fatal("double spend accepted into mempool")
	}
	if _, err := c.Produce(w.NewAddress()); err != nil {
		t.Fatal(err)
	}
	if bal(c, bob) != 10*Unit {
		t.Fatal("bob balance", bal(c, bob))
	}
	if s := c.Stats(); s.Supply != 2*BlockReward {
		t.Fatal("supply", s.Supply)
	}
}

func TestRejectsBadSig(t *testing.T) {
	c, w := setup(t)
	tx, _ := w.BuildTx(c.UTXOsFor(keys(w)), sha([]byte("x")), Unit, 0, "")
	tx.Outputs[0].Value = 49 * Unit // change amount after signing
	if _, err := c.SubmitTx(tx); err == nil {
		t.Fatal("tampered tx accepted")
	}
}

func TestRejectsKeyReuseAndBadBlocks(t *testing.T) {
	c, w := setup(t)
	a := w.Address(0)
	// pay to an address after it has signed -> rejected (burned)
	tx, _ := w.BuildTx(c.UTXOsFor(keys(w)), sha([]byte("x")), Unit, 0, "")
	c.SubmitTx(tx)
	c.Produce(sha([]byte("m")))
	w2 := &Wallet{Seed: w.Seed, Next: w.Next}
	tx2, err := w2.BuildTx(c.UTXOsFor(keys(w2)), a, Unit, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SubmitTx(tx2); err == nil {
		t.Fatal("payment to burned one-time address accepted")
	}
	// bad PoH
	b, _ := c.Produce(sha([]byte("m")))
	c2, _ := New("", 8)
	for _, bl := range c.Blocks[:len(c.Blocks)-1] {
		if err := c2.AddBlock(bl); err != nil {
			t.Fatal("replay", err)
		}
	}
	bad := b
	bad.Header.PoH[0] ^= 1
	if c2.AddBlock(bad) == nil {
		t.Fatal("bad PoH accepted")
	}
	bad = b
	bad.Txs[0].Outputs[0].Value += 1
	if c2.AddBlock(bad) == nil {
		t.Fatal("inflated coinbase accepted")
	}
}

func TestPersistence(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.json")
	c, _ := New(p, 8)
	for i := 0; i < 3; i++ {
		c.Produce(sha([]byte("m")))
	}
	c2, err := New(p, 8)
	if err != nil || c2.Stats().Height != 2 {
		t.Fatal("reload failed", err)
	}
}

func TestHalving(t *testing.T) {
	if Reward(0) != 50*Unit || Reward(HalvingEvery) != 25*Unit {
		t.Fatal("halving")
	}
}

func keys(w *Wallet) map[Hash]bool {
	m := map[Hash]bool{}
	for a := range w.Addresses() {
		m[a] = true
	}
	return m
}
func bal(c *Chain, w *Wallet) (s uint64) {
	for _, u := range c.UTXOsFor(keys(w)) {
		s += u.Value
	}
	return
}
