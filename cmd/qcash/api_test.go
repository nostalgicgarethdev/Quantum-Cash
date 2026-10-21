package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nostalgicgarethdev/Quantum-Cash/chain"
	"github.com/nostalgicgarethdev/Quantum-Cash/p2p"
)

func TestExplorerAPI(t *testing.T) {
	c, _ := chain.New("", 4)
	var last chain.Block
	for i := 0; i < 3; i++ {
		last, _ = c.Produce(chain.Hash{})
	}
	pn := p2p.New(p2p.Config{Chain: c})
	srv := httptest.NewServer(newAPI(c, pn, true))
	defer srv.Close()
	get := func(p string, code int) map[string]any {
		r, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		if r.StatusCode != code {
			t.Fatalf("%s: got %d want %d", p, r.StatusCode, code)
		}
		if r.Header.Get("Access-Control-Allow-Origin") != "*" {
			t.Fatalf("%s: no CORS", p)
		}
		var v any
		json.NewDecoder(r.Body).Decode(&v)
		m, _ := v.(map[string]any)
		return m
	}
	if s := get("/stats", 200); s["height"].(float64) != 2 {
		t.Fatalf("stats %v", s)
	}
	get("/blocks?from=1&n=5", 200)
	get("/blocks?from=x", 400)
	if b := get("/block/"+last.Header.Hash().String(), 200); b["hash"] != last.Header.Hash().String() {
		t.Fatal("block hash mismatch")
	}
	get("/block/1", 200)
	get("/block/99", 404)
	if tx := get("/tx/"+last.Txs[0].ID().String(), 200); tx["height"].(float64) != 2 {
		t.Fatal("tx height")
	}
	get("/peers", 200)
	get("/mempool", 200)
	r, _ := http.Post(srv.URL+"/tx", "application/json", nil)
	if r.StatusCode != 405 {
		t.Fatal("readonly node accepted a write")
	}
	// rate limit kicks in after the burst
	limited := false
	for i := 0; i < 80; i++ {
		r, _ := http.Get(srv.URL + "/health")
		r.Body.Close()
		if r.StatusCode == 429 {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("no rate limit")
	}
}
