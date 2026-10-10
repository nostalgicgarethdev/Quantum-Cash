// Package chain implements a Bitcoin-style UTXO ledger and a small Proof-of-History
// clock. Every spend is signed with the W-OTS-style keys in package wots.
// Native coin: QCASH. Experimental testnet. Not money.
package chain

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/bits"
	"os"
	"sync"
	"time"

	"github.com/nostalgicgarethdev/Quantum-Cash/wots"
)

const (
	Ticker       = "QCASH"
	Unit         = 100_000_000 // 1 QCASH = 1e8 "quanta"
	BlockReward  = 50 * Unit
	HalvingEvery = 210_000
	TicksPerSlot = 12_500 // sequential SHA-256 PoH ticks per slot
	DefaultBits  = 16     // PoW difficulty (leading zero bits) — devnet
)

type Hash [32]byte

func (h Hash) String() string               { return hex.EncodeToString(h[:]) }
func (h Hash) MarshalText() ([]byte, error) { return []byte(h.String()), nil }
func (h *Hash) UnmarshalText(b []byte) error {
	x, err := hex.DecodeString(string(b))
	if err != nil || len(x) != 32 {
		return errors.New("bad hash")
	}
	copy(h[:], x)
	return nil
}
func ParseHash(s string) (Hash, error) { var h Hash; return h, h.UnmarshalText([]byte(s)) }

func sha(b ...[]byte) Hash {
	s := sha256.New()
	for _, x := range b {
		s.Write(x)
	}
	var h Hash
	copy(h[:], s.Sum(nil))
	return h
}

type OutPoint struct {
	TxID  Hash   `json:"txid"`
	Index uint32 `json:"index"`
}
type TxIn struct {
	Prev   OutPoint `json:"prev"`
	PubKey []byte   `json:"pubkey"`    // W-OTS public key (2144 bytes)
	Sig    []byte   `json:"signature"` // W-OTS signature (2144 bytes)
}
type TxOut struct {
	Value   uint64 `json:"value"`
	Address Hash   `json:"address"`
}
type Tx struct {
	Inputs  []TxIn  `json:"inputs"`
	Outputs []TxOut `json:"outputs"`
	Memo    string  `json:"memo,omitempty"`
}

// SigHash commits to everything except signatures/pubkeys.
func (t *Tx) SigHash() Hash {
	b := []byte("qc-sighash")
	for _, in := range t.Inputs {
		b = append(b, in.Prev.TxID[:]...)
		b = binary.BigEndian.AppendUint32(b, in.Prev.Index)
	}
	for _, o := range t.Outputs {
		b = binary.BigEndian.AppendUint64(b, o.Value)
		b = append(b, o.Address[:]...)
	}
	b = append(b, []byte(t.Memo)...)
	return sha(b)
}
func (t *Tx) ID() Hash {
	j, _ := json.Marshal(t)
	return sha([]byte("qc-tx"), j)
}
func (t *Tx) IsCoinbase() bool { return len(t.Inputs) == 0 }

type Header struct {
	Height     uint64 `json:"height"`
	Prev       Hash   `json:"prev"`
	MerkleRoot Hash   `json:"merkle_root"`
	Slot       uint64 `json:"slot"`
	PoH        Hash   `json:"poh"`       // PoH hash at end of this slot
	PoHTicks   uint64 `json:"poh_ticks"` // total ticks since genesis
	Time       int64  `json:"time"`
	Bits       uint8  `json:"bits"`
	Nonce      uint64 `json:"nonce"`
}

func (h *Header) Hash() Hash {
	j, _ := json.Marshal(h)
	return sha([]byte("qc-block"), j)
}

type Block struct {
	Header Header `json:"header"`
	Txs    []Tx   `json:"txs"`
}

func Merkle(txs []Tx) Hash {
	if len(txs) == 0 {
		return Hash{}
	}
	l := make([]Hash, len(txs))
	for i := range txs {
		l[i] = txs[i].ID()
	}
	for len(l) > 1 {
		if len(l)%2 == 1 {
			l = append(l, l[len(l)-1])
		}
		n := make([]Hash, len(l)/2)
		for i := range n {
			n[i] = sha(l[2*i][:], l[2*i+1][:])
		}
		l = n
	}
	return l[0]
}

// PoHRun runs the sequential SHA-256 clock: TicksPerSlot hashes, mixing in each tx id
// in order. Ordering is provable because nobody can compute it faster than sequentially.
func PoHRun(start Hash, txs []Tx) Hash {
	x := start
	mix := map[int]Hash{}
	for i := range txs {
		mix[(i*TicksPerSlot)/(len(txs)+1)] = txs[i].ID()
	}
	for i := 0; i < TicksPerSlot; i++ {
		if m, ok := mix[i]; ok {
			x = sha(x[:], m[:])
		} else {
			x = sha(x[:])
		}
	}
	return x
}

func leadingZeros(h Hash) int {
	n := 0
	for _, b := range h {
		if b == 0 {
			n += 8
			continue
		}
		return n + bits.LeadingZeros8(b)
	}
	return n
}

func Reward(height uint64) uint64 {
	s := height / HalvingEvery
	if s >= 64 {
		return 0
	}
	return BlockReward >> s
}

type UTXO struct {
	OutPoint
	TxOut
}

type Chain struct {
	mu      sync.Mutex
	Blocks  []Block
	utxo    map[OutPoint]TxOut
	used    map[Hash]bool // addresses that have already signed once (W-OTS reuse guard)
	Mempool []Tx
	Bits    uint8
	path    string
}

var GenesisPoH = sha([]byte("QuantumCash genesis — experimental testnet, not money"))

func New(path string, bitsDiff uint8) (*Chain, error) {
	c := &Chain{utxo: map[OutPoint]TxOut{}, used: map[Hash]bool{}, Bits: bitsDiff, path: path}
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			var blocks []Block
			if err := json.Unmarshal(b, &blocks); err != nil {
				return nil, err
			}
			for _, bl := range blocks {
				if err := c.apply(bl); err != nil {
					return nil, fmt.Errorf("replay %d: %w", bl.Header.Height, err)
				}
			}
		}
	}
	return c, nil
}

func (c *Chain) tip() (Hash, Hash, uint64, uint64) {
	if len(c.Blocks) == 0 {
		return Hash{}, GenesisPoH, 0, 0
	}
	h := c.Blocks[len(c.Blocks)-1].Header
	return h.Hash(), h.PoH, h.Height + 1, h.Slot + 1
}

func (c *Chain) checkTx(t *Tx, spent map[OutPoint]bool, usedNow map[Hash]Hash) (uint64, error) {
	if usedNow == nil {
		usedNow = map[Hash]Hash{}
	}
	if t.IsCoinbase() {
		return 0, errors.New("unexpected coinbase")
	}
	sh := t.SigHash()
	var in, out uint64
	for _, i := range t.Inputs {
		o, ok := c.utxo[i.Prev]
		if !ok || spent[i.Prev] {
			return 0, errors.New("missing or spent input")
		}
		pk, err := wots.PublicFromBytes(i.PubKey)
		if err != nil {
			return 0, err
		}
		if pk.Address() != o.Address {
			return 0, errors.New("pubkey does not match address")
		}
		sg, err := wots.SignatureFromBytes(i.Sig)
		if err != nil {
			return 0, err
		}
		if !wots.Verify(pk, sh, sg) {
			return 0, errors.New("bad post-quantum signature")
		}
		if prevSH, ok := usedNow[o.Address]; c.used[o.Address] || (ok && prevSH != sh) {
			return 0, errors.New("one-time key already used")
		}
		spent[i.Prev] = true
		usedNow[o.Address] = sh
		in += o.Value
	}
	for _, o := range t.Outputs {
		if o.Value == 0 {
			return 0, errors.New("zero output")
		}
		if c.used[o.Address] {
			return 0, errors.New("paying to a burned one-time address")
		}
		out += o.Value
	}
	if out > in {
		return 0, errors.New("outputs exceed inputs")
	}
	return in - out, nil
}

func (c *Chain) apply(b Block) error {
	prev, poh, height, slot := c.tip()
	h := b.Header
	if h.Height != height || h.Prev != prev || h.Slot < slot {
		return errors.New("does not extend tip")
	}
	if h.Bits != c.Bits && len(c.Blocks) > 0 {
		return errors.New("wrong difficulty")
	}
	if leadingZeros(h.Hash()) < int(h.Bits) {
		return errors.New("insufficient proof of work")
	}
	if Merkle(b.Txs) != h.MerkleRoot {
		return errors.New("bad merkle root")
	}
	// PoH must be continuous: one segment per slot, empty slots tick with no txs.
	x := poh
	for s := slot; s < h.Slot; s++ {
		x = PoHRun(x, nil)
	}
	if PoHRun(x, b.Txs) != h.PoH {
		return errors.New("bad proof of history")
	}
	if len(b.Txs) == 0 || !b.Txs[0].IsCoinbase() {
		return errors.New("first tx must be coinbase")
	}
	spent := map[OutPoint]bool{}
	un := map[Hash]Hash{}
	var fees uint64
	for i := 1; i < len(b.Txs); i++ {
		f, err := c.checkTx(&b.Txs[i], spent, un)
		if err != nil {
			return fmt.Errorf("tx %d: %w", i, err)
		}
		fees += f
	}
	var cb uint64
	for _, o := range b.Txs[0].Outputs {
		cb += o.Value
	}
	for _, o := range b.Txs[0].Outputs {
		if c.used[o.Address] {
			return errors.New("coinbase pays a burned one-time address")
		}
	}
	if cb > Reward(height)+fees {
		return errors.New("coinbase too large")
	}
	for _, t := range b.Txs {
		id := t.ID()
		for _, in := range t.Inputs {
			c.used[c.utxo[in.Prev].Address] = true
			delete(c.utxo, in.Prev)
		}
		for i, o := range t.Outputs {
			c.utxo[OutPoint{id, uint32(i)}] = o
		}
	}
	c.Blocks = append(c.Blocks, b)
	if c.Bits == 0 {
		c.Bits = h.Bits
	}
	// drop mempool txs that are now invalid
	keep := []Tx{}
	ks, ku := map[OutPoint]bool{}, map[Hash]Hash{}
	for _, t := range c.Mempool {
		if _, err := c.checkTx(&t, ks, ku); err == nil {
			keep = append(keep, t)
		}
	}
	c.Mempool = keep
	return nil
}

func (c *Chain) save() {
	if c.path == "" {
		return
	}
	b, _ := json.Marshal(c.Blocks)
	os.WriteFile(c.path+".tmp", b, 0o644)
	os.Rename(c.path+".tmp", c.path)
}

func (c *Chain) AddBlock(b Block) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.apply(b); err != nil {
		return err
	}
	c.save()
	return nil
}

func (c *Chain) SubmitTx(t Tx) (Hash, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	spent := map[OutPoint]bool{}
	un := map[Hash]Hash{}
	for _, m := range c.Mempool {
		c.checkTx(&m, spent, un)
	}
	if _, err := c.checkTx(&t, spent, un); err != nil {
		return Hash{}, err
	}
	c.Mempool = append(c.Mempool, t)
	return t.ID(), nil
}

// Produce builds the next block: PoH over pending txs, then a small PoW seal.
func (c *Chain) Produce(miner Hash) (Block, error) {
	c.mu.Lock()
	prev, poh, height, slot := c.tip()
	txs := append([]Tx(nil), c.Mempool...)
	var fees uint64
	spent := map[OutPoint]bool{}
	un := map[Hash]Hash{}
	valid := []Tx{}
	for _, t := range txs {
		if f, err := c.checkTx(&t, spent, un); err == nil {
			fees += f
			valid = append(valid, t)
		}
	}
	bitsD := c.Bits
	if c.used[miner] {
		miner = Hash{} // never pay a key that has already signed
	}
	c.mu.Unlock()
	cb := Tx{Outputs: []TxOut{{Reward(height) + fees, miner}}, Memo: fmt.Sprintf("QCASH block %d", height)}
	all := append([]Tx{cb}, valid...)
	h := Header{Height: height, Prev: prev, MerkleRoot: Merkle(all), Slot: slot,
		PoH: PoHRun(poh, all), PoHTicks: (slot + 1) * TicksPerSlot, Time: time.Now().Unix(), Bits: bitsD}
	for leadingZeros(h.Hash()) < int(h.Bits) {
		h.Nonce++
	}
	b := Block{h, all}
	return b, c.AddBlock(b)
}

func (c *Chain) UTXOsFor(addrs map[Hash]bool) []UTXO {
	c.mu.Lock()
	defer c.mu.Unlock()
	var r []UTXO
	for op, o := range c.utxo {
		if addrs[o.Address] {
			r = append(r, UTXO{op, o})
		}
	}
	return r
}
func (c *Chain) IsUsed(a Hash) bool { c.mu.Lock(); defer c.mu.Unlock(); return c.used[a] }

type Stats struct {
	Ticker   string `json:"ticker"`
	Height   uint64 `json:"height"`
	Tip      string `json:"tip"`
	Supply   uint64 `json:"supply"`
	UTXOs    int    `json:"utxos"`
	Mempool  int    `json:"mempool"`
	Bits     uint8  `json:"bits"`
	PoHTicks uint64 `json:"poh_ticks"`
}

func (c *Chain) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	var sup uint64
	for _, o := range c.utxo {
		sup += o.Value
	}
	s := Stats{Ticker: Ticker, Supply: sup, UTXOs: len(c.utxo), Mempool: len(c.Mempool), Bits: c.Bits}
	if n := len(c.Blocks); n > 0 {
		h := c.Blocks[n-1].Header
		s.Height, s.Tip, s.PoHTicks = h.Height, h.Hash().String(), h.PoHTicks
	}
	return s
}

func (c *Chain) Recent(n int) []Block {
	c.mu.Lock()
	defer c.mu.Unlock()
	var r []Block
	for i := len(c.Blocks) - 1; i >= 0 && len(r) < n; i-- {
		r = append(r, c.Blocks[i])
	}
	return r
}
