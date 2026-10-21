package p2p

import (
	"crypto/sha256"
	"net"
	"testing"
	"time"

	"github.com/nostalgicgarethdev/Quantum-Cash/chain"
)

func h(s string) chain.Hash { return chain.Hash(sha256.Sum256([]byte(s))) }

func newNode(t *testing.T, peers ...string) *Node {
	t.Helper()
	c, err := chain.New("", 8)
	if err != nil {
		t.Fatal(err)
	}
	n := New(Config{Chain: c, Listen: "127.0.0.1:0", Peers: peers,
		DialInterval: 100 * time.Millisecond, PingInterval: time.Second})
	if err := n.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(n.Stop)
	return n
}

func mine(t *testing.T, n *Node, to chain.Hash) chain.Block {
	t.Helper()
	b, err := n.c.Produce(to)
	if err != nil {
		t.Fatal(err)
	}
	n.AnnounceBlock(b)
	return b
}

func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for", what)
}

func balance(c *chain.Chain, w *chain.Wallet) uint64 {
	set := map[chain.Hash]bool{}
	for a := range w.Addresses() {
		set[a] = true
	}
	var s uint64
	for _, u := range c.UTXOsFor(set) {
		s += u.Value
	}
	return s
}

func TestMultiNodeNetwork(t *testing.T) {
	a := newNode(t)
	b := newNode(t, a.Addr())
	c := newNode(t, b.Addr())
	eventually(t, "handshakes", func() bool { return len(a.Peers()) >= 1 && len(c.Peers()) >= 1 })

	// peer address exchange: c only knows b, but learns a's address and connects
	eventually(t, "addr exchange", func() bool { return len(c.Peers()) == 2 })

	// blocks mined on a reach everyone
	alice := &chain.Wallet{Seed: h("alice")}
	for i := 0; i < 3; i++ {
		mine(t, a, alice.NewAddress())
	}
	eventually(t, "block propagation", func() bool {
		return b.c.TipHash() == a.c.TipHash() && c.c.TipHash() == a.c.TipHash()
	})

	// a tx submitted on c is gossiped to a's mempool and mined there
	bob := &chain.Wallet{Seed: h("bob")}
	set := map[chain.Hash]bool{}
	for k := range alice.Addresses() {
		set[k] = true
	}
	tx, err := alice.BuildTx(c.c.UTXOsFor(set), bob.NewAddress(), 7*chain.Unit, 1000, "p2p")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SubmitTx(tx); err != nil {
		t.Fatal(err)
	}
	eventually(t, "tx gossip", func() bool { return a.c.HasTx(tx.ID()) && b.c.HasTx(tx.ID()) })
	// dedupe: resubmitting the same tx anywhere is rejected, no gossip storm
	if _, err := b.SubmitTx(tx); err == nil {
		t.Fatal("duplicate tx accepted")
	}
	mine(t, a, h("miner"))
	eventually(t, "tx mined everywhere", func() bool {
		return balance(b.c, bob) == 7*chain.Unit && balance(c.c, bob) == 7*chain.Unit && len(c.c.MempoolTxs()) == 0
	})

	// late joiner syncs the whole chain (more than one batch)
	for i := 0; i < MaxBlocksPerMsg+5; i++ {
		mine(t, a, h("miner"))
	}
	d := newNode(t, c.Addr())
	eventually(t, "late joiner sync", func() bool { return d.c.TipHash() == a.c.TipHash() })
	if d.c.Height() != uint64(MaxBlocksPerMsg+9) {
		t.Fatal("height", d.c.Height())
	}
}

func TestForkAndReorg(t *testing.T) {
	// two isolated networks mine competing chains, then connect
	x := newNode(t)
	y := newNode(t)
	for i := 0; i < 2; i++ {
		mine(t, x, h("x"))
	}
	for i := 0; i < 5; i++ {
		mine(t, y, h("y"))
	}
	z := newNode(t, x.Addr()) // follows x
	eventually(t, "z follows x", func() bool { return z.c.TipHash() == x.c.TipHash() })
	oldTip := x.c.TipHash()

	// link the networks: x dials y, the heavier chain wins on x and z
	x.mu.Lock()
	x.addrs[y.Addr()] = time.Now()
	x.mu.Unlock()
	eventually(t, "reorg to heavier chain", func() bool {
		return x.c.TipHash() == y.c.TipHash() && z.c.TipHash() == y.c.TipHash()
	})
	if x.c.HasBlock(oldTip) == false {
		t.Fatal("old branch should be kept as a side chain")
	}
	// x then mines on the winning chain and y follows
	mine(t, x, h("x"))
	eventually(t, "y follows x after reorg", func() bool { return y.c.TipHash() == x.c.TipHash() })
}

func TestBanInvalidBlockAndOversize(t *testing.T) {
	a := newNode(t)
	mine(t, a, h("a"))
	conn, err := net.Dial("tcp", a.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := readMsg(conn); err != nil { // a's version
		t.Fatal(err)
	}
	writeMsg(conn, MsgVersion, Version{Protocol: ProtocolVersion, Network: chain.NetworkID(8), Nonce: 42, UserAgent: "evil"})
	eventually(t, "evil handshake", func() bool { return len(a.Peers()) == 1 })
	// a block that extends a's tip but fails PoW
	bl := chain.Block{Header: chain.Header{Height: 1, Prev: a.c.TipHash(), Slot: 1, Bits: 8, PoHTicks: 2 * chain.TicksPerSlot, Nonce: 1},
		Txs: []chain.Tx{{Outputs: []chain.TxOut{{Value: 1, Address: h("e")}}}}}
	bl.Header.MerkleRoot = chain.Merkle(bl.Txs)
	for bl.Header.Hash()[0] == 0 { // make sure PoW fails
		bl.Header.Nonce++
	}
	writeMsg(conn, MsgBlock, BlockMsg{Block: bl})
	eventually(t, "ban", func() bool { return len(a.Peers()) == 0 && a.banned("127.0.0.1") })
	// banned IP cannot reconnect
	c2, err := net.Dial("tcp", a.Addr())
	if err == nil {
		c2.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, err := readMsg(c2); err == nil {
			t.Fatal("banned peer got a version message")
		}
		c2.Close()
	}
}

func TestRejectsOtherNetworkAndOversize(t *testing.T) {
	a := newNode(t)
	conn, _ := net.Dial("tcp", a.Addr())
	defer conn.Close()
	readMsg(conn)
	writeMsg(conn, MsgVersion, Version{Protocol: ProtocolVersion, Network: chain.NetworkID(9), Nonce: 7})
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		if _, err := readMsg(conn); err != nil {
			break // dropped
		}
	}
	if len(a.Peers()) != 0 {
		t.Fatal("peer from another network kept")
	}
	c2, _ := net.Dial("tcp", a.Addr())
	defer c2.Close()
	readMsg(c2)
	writeMsg(c2, MsgVersion, Version{Protocol: ProtocolVersion, Network: chain.NetworkID(8), Nonce: 8})
	eventually(t, "handshake", func() bool { return len(a.Peers()) == 1 })
	c2.Write([]byte{0xff, 0xff, 0xff, 0xff}) // 4 GiB length prefix
	eventually(t, "oversize ban", func() bool { return a.banned("127.0.0.1") })
}
