package chain

import "testing"

func TestNetworksDiffer(t *testing.T) {
	defer UseNetwork("devnet")
	UseNetwork("devnet")
	d := NetworkID(18)
	if _, err := UseNetwork("testnet"); err != nil {
		t.Fatal(err)
	}
	if NetworkID(18) == d {
		t.Fatal("testnet and devnet share a network id")
	}
	if _, err := UseNetwork("nope"); err == nil {
		t.Fatal("unknown network accepted")
	}
}

func TestQueries(t *testing.T) {
	c := newState("", 4)
	var b Block
	for i := 0; i < 3; i++ {
		var err error
		if b, err = c.Produce(Hash{}); err != nil {
			t.Fatal(err)
		}
	}
	if got := c.BlocksFrom(1, 10); len(got) != 2 || got[0].Header.Height != 1 {
		t.Fatalf("BlocksFrom: %d", len(got))
	}
	if _, ok := c.BlockByHash(b.Header.Hash()); !ok {
		t.Fatal("tip not found by hash")
	}
	if _, h, _, ok := c.TxLookup(b.Txs[0].ID()); !ok || h != 2 {
		t.Fatal("coinbase not found")
	}
}
