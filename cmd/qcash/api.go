package main

// Read-only JSON API for the explorer. Safe to put behind HTTPS (Caddy/nginx)
// and call from a browser: open CORS, GET-only unless writes are enabled,
// per-IP token-bucket rate limit.

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nostalgicgarethdev/Quantum-Cash/chain"
	"github.com/nostalgicgarethdev/Quantum-Cash/p2p"
)

type blockView struct {
	Hash string `json:"hash"`
	chain.Block
}

type txView struct {
	ID string `json:"id"`
	chain.Tx
	Height    int64  `json:"height"` // -1 = mempool
	BlockHash string `json:"block_hash,omitempty"`
}

func viewBlock(b chain.Block) blockView { return blockView{b.Header.Hash().String(), b} }

type limiter struct {
	mu    sync.Mutex
	rate  float64 // tokens per second
	burst float64
	b     map[string]*bucket
}
type bucket struct {
	tok  float64
	last time.Time
}

func newLimiter(perSec, burst float64) *limiter {
	l := &limiter{rate: perSec, burst: burst, b: map[string]*bucket{}}
	go func() {
		for range time.Tick(time.Minute) {
			l.mu.Lock()
			for k, v := range l.b {
				if time.Since(v.last) > 5*time.Minute {
					delete(l.b, k)
				}
			}
			l.mu.Unlock()
		}
	}()
	return l
}

func (l *limiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	k := l.b[ip]
	if k == nil {
		k = &bucket{tok: l.burst, last: now}
		l.b[ip] = k
	}
	k.tok += now.Sub(k.last).Seconds() * l.rate
	if k.tok > l.burst {
		k.tok = l.burst
	}
	k.last = now
	if k.tok < 1 {
		return false
	}
	k.tok--
	return true
}

func clientIP(r *http.Request) string {
	// Only trust X-Forwarded-For from a local reverse proxy.
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if f := r.Header.Get("X-Forwarded-For"); f != "" {
			return strings.TrimSpace(strings.Split(f, ",")[0])
		}
	}
	return host
}

func newAPI(c *chain.Chain, pn *p2p.Node, readonly bool) http.Handler {
	mux := http.NewServeMux()
	lim := newLimiter(10, 40)
	js := func(w http.ResponseWriter, code int, v any) {
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(v)
	}
	notFound := func(w http.ResponseWriter) { js(w, 404, map[string]string{"error": "not found"}) }
	get := func(path string, f func(w http.ResponseWriter, r *http.Request)) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				js(w, 405, map[string]string{"error": "method not allowed"})
				return
			}
			f(w, r)
		})
	}

	get("/stats", func(w http.ResponseWriter, r *http.Request) {
		s := c.Stats()
		js(w, 200, map[string]any{"network": chain.ActiveNetwork.Name, "peers": len(pn.Peers()), "syncing": pn.Syncing(),
			"ticker": s.Ticker, "height": s.Height, "tip": s.Tip, "supply": s.Supply, "utxos": s.UTXOs,
			"mempool": s.Mempool, "bits": s.Bits, "poh_ticks": s.PoHTicks, "work": s.Work, "time": time.Now().Unix()})
	})
	get("/blocks", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		n, _ := strconv.Atoi(q.Get("n"))
		if n <= 0 || n > 100 {
			n = 20
		}
		var bs []chain.Block
		if f := q.Get("from"); f != "" {
			from, err := strconv.ParseUint(f, 10, 64)
			if err != nil {
				js(w, 400, map[string]string{"error": "bad from"})
				return
			}
			bs = c.BlocksFrom(from, n)
		} else {
			bs = c.Recent(n) // newest first
		}
		out := make([]blockView, 0, len(bs))
		for _, b := range bs {
			out = append(out, viewBlock(b))
		}
		js(w, 200, out)
	})
	get("/block/", func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/block/")
		if h, err := strconv.ParseUint(key, 10, 64); err == nil && len(key) < 20 {
			if bs := c.BlocksFrom(h, 1); len(bs) == 1 {
				js(w, 200, viewBlock(bs[0]))
				return
			}
			notFound(w)
			return
		}
		h, err := chain.ParseHash(key)
		if err != nil {
			js(w, 400, map[string]string{"error": "bad hash"})
			return
		}
		if b, ok := c.BlockByHash(h); ok {
			js(w, 200, viewBlock(b))
			return
		}
		notFound(w)
	})
	get("/tx/", func(w http.ResponseWriter, r *http.Request) {
		h, err := chain.ParseHash(strings.TrimPrefix(r.URL.Path, "/tx/"))
		if err != nil {
			js(w, 400, map[string]string{"error": "bad txid"})
			return
		}
		t, height, bh, ok := c.TxLookup(h)
		if !ok {
			notFound(w)
			return
		}
		v := txView{ID: h.String(), Tx: t, Height: height}
		if height >= 0 {
			v.BlockHash = bh.String()
		}
		js(w, 200, v)
	})
	get("/peers", func(w http.ResponseWriter, r *http.Request) {
		ps := pn.Peers()
		for i := range ps { // don't publish peer IPs to the open web
			if ps[i].Inbound {
				ps[i].Addr = "inbound"
			}
		}
		js(w, 200, ps)
	})
	get("/mempool", func(w http.ResponseWriter, r *http.Request) {
		txs := c.MempoolTxs()
		out := make([]txView, 0, len(txs))
		for _, t := range txs {
			out = append(out, txView{ID: t.ID().String(), Tx: t, Height: -1})
		}
		js(w, 200, out)
	})
	get("/utxos", func(w http.ResponseWriter, r *http.Request) {
		set := map[chain.Hash]bool{}
		for i, a := range r.URL.Query()["addr"] {
			if i >= 20 {
				break
			}
			if h, err := chain.ParseHash(a); err == nil {
				set[h] = true
			}
		}
		js(w, 200, c.UTXOsFor(set))
	})
	get("/health", func(w http.ResponseWriter, r *http.Request) { js(w, 200, map[string]bool{"ok": true}) })
	mux.HandleFunc("/tx", func(w http.ResponseWriter, r *http.Request) {
		if readonly || r.Method != http.MethodPost {
			js(w, 405, map[string]string{"error": "writes disabled on this node"})
			return
		}
		var t chain.Tx
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&t); err != nil {
			js(w, 400, map[string]string{"error": "bad json"})
			return
		}
		id, err := pn.SubmitTx(t)
		if err != nil {
			js(w, 400, map[string]string{"error": err.Error()})
			return
		}
		js(w, 200, map[string]string{"txid": id.String()})
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type")
		h.Set("Content-Type", "application/json")
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		if !lim.allow(clientIP(r)) {
			h.Set("Retry-After", "1")
			js(w, 429, map[string]string{"error": "rate limited"})
			return
		}
		mux.ServeHTTP(w, r)
	})
}
