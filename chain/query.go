package chain

// Read-only queries for the explorer API.

// BlocksFrom returns up to n main-chain blocks starting at height from (ascending).
func (c *Chain) BlocksFrom(from uint64, n int) []Block {
	c.mu.Lock()
	defer c.mu.Unlock()
	var r []Block
	for i := from; i < uint64(len(c.Blocks)) && len(r) < n; i++ {
		r = append(r, c.Blocks[i])
	}
	return r
}

// BlockByHash finds a main-chain block.
func (c *Chain) BlockByHash(h Hash) (Block, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i, ok := c.index[h]; ok && i < uint64(len(c.Blocks)) {
		return c.Blocks[i], true
	}
	return Block{}, false
}

// TxLookup finds a transaction in the mempool or main chain.
// height is -1 for mempool transactions.
func (c *Chain) TxLookup(id Hash) (Tx, int64, Hash, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.Mempool {
		if t.ID() == id {
			return t, -1, Hash{}, true
		}
	}
	for i := len(c.Blocks) - 1; i >= 0; i-- {
		for _, t := range c.Blocks[i].Txs {
			if t.ID() == id {
				return t, int64(i), c.Blocks[i].Header.Hash(), true
			}
		}
	}
	return Tx{}, 0, Hash{}, false
}
