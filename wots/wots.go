// Package wots implements Winternitz one-time signatures (W-OTS, w=16) over SHA-256.
// Security rests only on SHA-256 preimage resistance, which is believed to hold
// against quantum computers (Grover gives at most a square-root speedup).
// Each key MUST sign exactly one message. QuantumCash enforces this by making
// every address single-use (spent outputs can never be spent again).
package wots

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
)

const (
	N    = 32
	W    = 16
	Len1 = 64 // 256 bits / 4 bits
	Len2 = 3  // checksum digits
	Len  = Len1 + Len2
)

type PrivateKey [Len][N]byte
type PublicKey [Len][N]byte
type Signature [Len][N]byte

func h(b ...[]byte) [N]byte {
	s := sha256.New()
	for _, x := range b {
		s.Write(x)
	}
	var o [N]byte
	copy(o[:], s.Sum(nil))
	return o
}

func chain(x [N]byte, start, steps, i int) [N]byte {
	for j := start; j < start+steps; j++ {
		var t [2]byte
		t[0], t[1] = byte(i), byte(j)
		x = h([]byte("qc-wots"), t[:], x[:])
	}
	return x
}

// KeyFromSeed deterministically derives key #index from a 32-byte seed.
func KeyFromSeed(seed []byte, index uint64) *PrivateKey {
	var sk PrivateKey
	var ib [8]byte
	binary.BigEndian.PutUint64(ib[:], index)
	for i := 0; i < Len; i++ {
		sk[i] = h([]byte("qc-sk"), seed, ib[:], []byte{byte(i)})
	}
	return &sk
}

func digits(msg [N]byte) [Len]int {
	var d [Len]int
	for i := 0; i < N; i++ {
		d[2*i] = int(msg[i] >> 4)
		d[2*i+1] = int(msg[i] & 15)
	}
	c := 0
	for i := 0; i < Len1; i++ {
		c += W - 1 - d[i]
	}
	d[Len1], d[Len1+1], d[Len1+2] = (c>>8)&15, (c>>4)&15, c&15
	return d
}

func (sk *PrivateKey) Public() *PublicKey {
	var pk PublicKey
	for i := 0; i < Len; i++ {
		pk[i] = chain(sk[i], 0, W-1, i)
	}
	return &pk
}

func (sk *PrivateKey) Sign(msg [N]byte) *Signature {
	var s Signature
	d := digits(msg)
	for i := 0; i < Len; i++ {
		s[i] = chain(sk[i], 0, d[i], i)
	}
	return &s
}

func Verify(pk *PublicKey, msg [N]byte, sig *Signature) bool {
	d := digits(msg)
	for i := 0; i < Len; i++ {
		if chain(sig[i], d[i], W-1-d[i], i) != pk[i] {
			return false
		}
	}
	return true
}

// Address is SHA-256 of the public key (like Bitcoin's P2PKH, but post-quantum).
func (pk *PublicKey) Address() [N]byte {
	b := make([]byte, 0, Len*N)
	for i := range pk {
		b = append(b, pk[i][:]...)
	}
	return h([]byte("qc-addr"), b)
}

func (pk *PublicKey) Bytes() []byte {
	b := make([]byte, 0, Len*N)
	for i := range pk {
		b = append(b, pk[i][:]...)
	}
	return b
}
func (s *Signature) Bytes() []byte { return (*PublicKey)(s).Bytes() }

func PublicFromBytes(b []byte) (*PublicKey, error) {
	if len(b) != Len*N {
		return nil, errors.New("bad length")
	}
	var p PublicKey
	for i := 0; i < Len; i++ {
		copy(p[i][:], b[i*N:])
	}
	return &p, nil
}
func SignatureFromBytes(b []byte) (*Signature, error) {
	p, err := PublicFromBytes(b)
	return (*Signature)(p), err
}
