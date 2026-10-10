package wots

import "testing"

func TestSignVerify(t *testing.T) {
	sk := KeyFromSeed([]byte("seed"), 7)
	pk := sk.Public()
	m := h([]byte("hello"))
	s := sk.Sign(m)
	if !Verify(pk, m, s) {
		t.Fatal("valid sig rejected")
	}
	m2 := h([]byte("hellp"))
	if Verify(pk, m2, s) {
		t.Fatal("forged msg accepted")
	}
	s.Bytes()[0] ^= 1
	s2, _ := SignatureFromBytes(s.Bytes())
	s2[3][0] ^= 1
	if Verify(pk, m, s2) {
		t.Fatal("tampered sig accepted")
	}
	if KeyFromSeed([]byte("seed"), 8).Public().Address() == pk.Address() {
		t.Fatal("addresses collide")
	}
}
