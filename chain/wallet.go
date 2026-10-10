package chain

import (
	"errors"

	"github.com/nostalgicgarethdev/Quantum-Cash/wots"
)

// Wallet: a seed that derives one-time W-OTS keys by index (HD-style).
type Wallet struct {
	Seed Hash   `json:"seed"`
	Next uint64 `json:"next"` // next unused key index
}

func (w *Wallet) Key(i uint64) *wots.PrivateKey { return wots.KeyFromSeed(w.Seed[:], i) }
func (w *Wallet) Address(i uint64) Hash         { return Hash(w.Key(i).Public().Address()) }
func (w *Wallet) NewAddress() Hash              { a := w.Address(w.Next); w.Next++; return a }
func (w *Wallet) Addresses() map[Hash]uint64 {
	m := map[Hash]uint64{}
	for i := uint64(0); i < w.Next; i++ {
		m[w.Address(i)] = i
	}
	return m
}

// BuildTx spends ALL utxos of the chosen keys (a W-OTS key may sign once),
// pays `to`, sends change to a fresh address.
func (w *Wallet) BuildTx(utxos []UTXO, to Hash, amount, fee uint64, memo string) (Tx, error) {
	idx := w.Addresses()
	var t Tx
	var in uint64
	for _, u := range utxos {
		if in >= amount+fee {
			break
		}
		t.Inputs = append(t.Inputs, TxIn{Prev: u.OutPoint})
		in += u.Value
	}
	if in < amount+fee {
		return t, errors.New("insufficient QCASH")
	}
	// include every other utxo on the same addresses so no key is left half-used
	have := map[OutPoint]bool{}
	addrs := map[Hash]bool{}
	for _, i := range t.Inputs {
		have[i.Prev] = true
	}
	for _, u := range utxos {
		if have[u.OutPoint] {
			addrs[u.Address] = true
		}
	}
	for _, u := range utxos {
		if !have[u.OutPoint] && addrs[u.Address] {
			t.Inputs = append(t.Inputs, TxIn{Prev: u.OutPoint})
			in += u.Value
		}
	}
	t.Outputs = []TxOut{{amount, to}}
	if ch := in - amount - fee; ch > 0 {
		t.Outputs = append(t.Outputs, TxOut{ch, w.NewAddress()})
	}
	t.Memo = memo
	sh := t.SigHash()
	addrOf := map[OutPoint]Hash{}
	for _, u := range utxos {
		addrOf[u.OutPoint] = u.Address
	}
	for k := range t.Inputs {
		sk := w.Key(idx[addrOf[t.Inputs[k].Prev]])
		t.Inputs[k].PubKey = sk.Public().Bytes()
		t.Inputs[k].Sig = sk.Sign(sh).Bytes()
	}
	return t, nil
}
