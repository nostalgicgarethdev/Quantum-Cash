package chain

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Consensus limits added in v0.2 so that blocks arriving from untrusted peers
// are cheap to reject before any expensive work is done.
const (
	MaxBlockTxs    = 1000        // transactions per block, coinbase included
	MaxBlockBytes  = 8 << 20     // JSON-encoded block size
	MaxSlotGap     = 64          // empty PoH slots a block may skip over
	MaxBits        = 40          // sanity cap on the difficulty field
	MaxFutureDrift = 2 * 60 * 60 // seconds a header may be ahead of local time
	maxSideBlocks  = 2048        // fork candidates kept in memory
	sideKeepDepth  = 1000        // drop fork candidates this far below the tip
	MaxLocator     = 64          // hashes accepted in a block locator
)

// Result of ProcessBlock.
type Status int

const (
	Extended  Status = iota // block extended the main chain
	Reorged                 // block made a heavier branch the main chain
	SideChain               // block stored on a lighter branch
	Duplicate               // block already known
)

func (s Status) String() string {
	return [...]string{"extended", "reorged", "side-chain", "duplicate"}[s]
}

var (
	// ErrOrphan means the parent is unknown: the caller should sync from the sender.
	ErrOrphan = errors.New("orphan block: unknown parent")
	// ErrInvalid wraps every consensus failure. Peers that send one misbehaved.
	ErrInvalid = errors.New("invalid block")
)

// BlockWork is the expected number of hashes to find a block at bits difficulty.
func BlockWork(bits uint8) uint64 { return 1 << bits }

// NetworkID identifies a network: nodes only talk to peers with the same genesis
// PoH seed and difficulty.
func NetworkID(bits uint8) Hash { return sha(GenesisPoH[:], []byte{bits}) }

// checkHeader runs the context-free checks: cheap, so they run before storing a block.
func (c *Chain) checkHeader(b *Block) error {
	h := &b.Header
	if h.Bits > MaxBits || (c.Bits != 0 && h.Bits != c.Bits) {
		return errors.New("wrong difficulty")
	}
	if len(b.Txs) == 0 || len(b.Txs) > MaxBlockTxs {
		return errors.New("bad transaction count")
	}
	if h.Time > time.Now().Unix()+MaxFutureDrift {
		return errors.New("timestamp too far in the future")
	}
	if h.PoHTicks != (h.Slot+1)*TicksPerSlot {
		return errors.New("bad poh tick count")
	}
	if c.trusted {
		return nil
	}
	if leadingZeros(h.Hash()) < int(h.Bits) {
		return errors.New("insufficient proof of work")
	}
	if Merkle(b.Txs) != h.MerkleRoot {
		return errors.New("bad merkle root")
	}
	return nil
}

func invalid(err error) error { return fmt.Errorf("%w: %v", ErrInvalid, err) }

// ProcessBlock accepts a block from anywhere (miner, peer, sync), validates it fully
// and applies the most-work rule: a branch replaces the main chain only when it
// carries strictly more cumulative proof of work and every block in it is valid.
func (c *Chain) ProcessBlock(b Block) (Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if raw, err := json.Marshal(b); err != nil || len(raw) > MaxBlockBytes {
		return 0, invalid(errors.New("block too large"))
	}
	id := b.Header.Hash()
	if _, ok := c.index[id]; ok {
		return Duplicate, nil
	}
	if _, ok := c.side[id]; ok {
		return Duplicate, nil
	}
	if err := c.checkHeader(&b); err != nil {
		return 0, invalid(err)
	}
	prev, _, _, _ := c.tip()
	if b.Header.Prev == prev && len(c.Blocks) > 0 || len(c.Blocks) == 0 && b.Header.Height == 0 && b.Header.Prev == (Hash{}) {
		if err := c.apply(b); err != nil {
			return 0, invalid(err)
		}
		c.save()
		return Extended, nil
	}
	// fork candidate: parent must be on the main chain or a known branch
	if !c.knownParent(b.Header) {
		return 0, ErrOrphan
	}
	if b.Header.Height == 0 && b.Header.Prev != (Hash{}) {
		return 0, invalid(errors.New("bad genesis"))
	}
	c.side[id] = b
	c.pruneSide()
	branch, fork, ok := c.branchTo(id)
	if !ok {
		return SideChain, nil
	}
	var w uint64
	for i := uint64(0); i < fork; i++ {
		w += BlockWork(c.Blocks[i].Header.Bits)
	}
	for _, x := range branch {
		w += BlockWork(x.Header.Bits)
	}
	if w <= c.work {
		return SideChain, nil
	}
	if err := c.reorg(fork, branch); err != nil {
		return 0, invalid(err)
	}
	return Reorged, nil
}

func (c *Chain) knownParent(h Header) bool {
	if h.Height == 0 {
		return h.Prev == Hash{}
	}
	if ht, ok := c.index[h.Prev]; ok {
		return ht+1 == h.Height
	}
	p, ok := c.side[h.Prev]
	return ok && p.Header.Height+1 == h.Height
}

// branchTo walks side blocks back from id to the main chain. It returns the branch
// oldest-first and the main-chain height at which it forks.
func (c *Chain) branchTo(id Hash) ([]Block, uint64, bool) {
	var rev []Block
	cur := id
	for {
		b, ok := c.side[cur]
		if !ok {
			return nil, 0, false
		}
		rev = append(rev, b)
		if b.Header.Height == 0 {
			break
		}
		if _, ok := c.index[b.Header.Prev]; ok {
			break
		}
		cur = b.Header.Prev
	}
	out := make([]Block, len(rev))
	for i := range rev {
		out[i] = rev[len(rev)-1-i]
	}
	return out, out[0].Header.Height, true
}

// reorg rebuilds state: the common prefix is replayed without re-verifying
// signatures and PoH (this node already did that), then the new branch is
// validated in full. The live state is only swapped if every block passes.
func (c *Chain) reorg(fork uint64, branch []Block) error {
	n := newState("", c.Bits)
	n.trusted = true
	for i := uint64(0); i < fork; i++ {
		if err := n.apply(c.Blocks[i]); err != nil {
			return fmt.Errorf("replay %d: %w", i, err)
		}
	}
	n.trusted = false
	for _, b := range branch {
		if err := n.apply(b); err != nil {
			// drop the bad block and everything built on it
			dead := map[Hash]bool{b.Header.Hash(): true}
			for grew := true; grew; {
				grew = false
				for h, x := range c.side {
					if dead[x.Header.Prev] && !dead[h] {
						dead[h], grew = true, true
					}
				}
			}
			for h := range dead {
				delete(c.side, h)
			}
			return fmt.Errorf("branch block %d: %w", b.Header.Height, err)
		}
	}
	old := c.Blocks[fork:]
	oldPool := c.Mempool
	for _, b := range branch {
		delete(c.side, b.Header.Hash())
	}
	for _, b := range old {
		c.side[b.Header.Hash()] = b
	}
	c.Blocks, c.utxo, c.used, c.index, c.work = n.Blocks, n.utxo, n.used, n.index, n.work
	// return disconnected transactions to the mempool if they are still valid
	c.Mempool = nil
	spent, un := map[OutPoint]bool{}, map[Hash]Hash{}
	seen := map[Hash]bool{}
	cands := []Tx{}
	for _, b := range old {
		cands = append(cands, b.Txs[1:]...)
	}
	cands = append(cands, oldPool...)
	for _, t := range cands {
		if seen[t.ID()] {
			continue
		}
		seen[t.ID()] = true
		if _, err := c.checkTx(&t, spent, un); err == nil {
			c.Mempool = append(c.Mempool, t)
		}
	}
	c.pruneSide()
	c.save()
	return nil
}

func (c *Chain) pruneSide() {
	h := uint64(len(c.Blocks))
	for id, b := range c.side {
		if h > sideKeepDepth && b.Header.Height < h-sideKeepDepth {
			delete(c.side, id)
		}
	}
	for id, b := range c.side {
		if len(c.side) <= maxSideBlocks {
			break
		}
		if b.Header.Height+10 < h {
			delete(c.side, id)
		}
	}
}

// Work returns the cumulative proof of work of the main chain.
func (c *Chain) Work() uint64 { c.mu.Lock(); defer c.mu.Unlock(); return c.work }

// Height returns the number of blocks on the main chain.
func (c *Chain) Height() uint64 { c.mu.Lock(); defer c.mu.Unlock(); return uint64(len(c.Blocks)) }

// TipHash returns the hash of the newest main-chain block (zero if empty).
func (c *Chain) TipHash() Hash { c.mu.Lock(); defer c.mu.Unlock(); h, _, _, _ := c.tip(); return h }

// HasBlock reports whether the block is known on any branch.
func (c *Chain) HasBlock(id Hash) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, a := c.index[id]
	_, b := c.side[id]
	return a || b
}

// HasTx reports whether a transaction is waiting in the mempool.
func (c *Chain) HasTx(id Hash) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.Mempool {
		if t.ID() == id {
			return true
		}
	}
	return false
}

// Locator lists main-chain hashes, dense near the tip and sparse further back,
// so a peer can find the fork point in one round trip.
func (c *Chain) Locator() []Hash {
	c.mu.Lock()
	defer c.mu.Unlock()
	var l []Hash
	step := 1
	for i := len(c.Blocks) - 1; i >= 0 && len(l) < MaxLocator-1; i -= step {
		l = append(l, c.Blocks[i].Header.Hash())
		if len(l) >= 10 {
			step *= 2
		}
	}
	if len(c.Blocks) > 0 {
		l = append(l, c.Blocks[0].Header.Hash())
	}
	return l
}

// BlocksAfter returns up to max main-chain blocks after the first locator hash
// this node recognises, or from genesis if none match.
func (c *Chain) BlocksAfter(locator []Hash, max int) []Block {
	c.mu.Lock()
	defer c.mu.Unlock()
	start := uint64(0)
	for _, h := range locator {
		if ht, ok := c.index[h]; ok {
			start = ht + 1
			break
		}
	}
	var r []Block
	for i := start; i < uint64(len(c.Blocks)) && len(r) < max; i++ {
		r = append(r, c.Blocks[i])
	}
	return r
}

// MempoolTxs returns a copy of the pending transactions.
func (c *Chain) MempoolTxs() []Tx {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Tx(nil), c.Mempool...)
}
