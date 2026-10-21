// qcash: QuantumCash devnet node + wallet CLI. Experimental testnet. Not money.
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nostalgicgarethdev/Quantum-Cash/chain"
	"github.com/nostalgicgarethdev/Quantum-Cash/p2p"
)

func usage() {
	fmt.Print(`qcash — QuantumCash ($QCash, ticker QCASH) experimental devnet

  qcash node   [-data chain.json] [-addr :8545] [-listen :7001] [-peers host:port,...]
               [-mine=true] [-miner ADDRESS] [-bits 16] [-interval 2s]
  qcash wallet new      [-w wallet.json]
  qcash wallet address  [-w wallet.json]
  qcash wallet balance  [-w wallet.json] [-node http://localhost:8545]
  qcash wallet send TO AMOUNT_QCASH [-w wallet.json] [-node URL] [-memo text]
`)
	os.Exit(2)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "node":
		node(os.Args[2:])
	case "wallet":
		if len(os.Args) < 3 {
			usage()
		}
		wallet(os.Args[2], os.Args[3:])
	default:
		usage()
	}
}

func node(args []string) {
	fs := flag.NewFlagSet("node", flag.ExitOnError)
	data := fs.String("data", "chain.json", "chain file")
	addr := fs.String("addr", ":8545", "http listen")
	miner := fs.String("miner", "", "reward address (hex); empty = burn address")
	bits := fs.Uint("bits", chain.DefaultBits, "PoW difficulty bits")
	iv := fs.Duration("interval", 2*time.Second, "slot interval")
	listen := fs.String("listen", ":7001", "p2p listen address (empty = outbound only)")
	peers := fs.String("peers", "", "comma-separated bootstrap peers host:port")
	mine := fs.Bool("mine", true, "produce blocks (use -mine=false for a relay/sync-only node)")
	fs.Parse(args)
	c, err := chain.New(*data, uint8(*bits))
	if err != nil {
		log.Fatal(err)
	}
	var m chain.Hash
	if *miner != "" {
		if m, err = chain.ParseHash(*miner); err != nil {
			log.Fatal("bad miner address")
		}
	}
	var boot []string
	for _, p := range strings.Split(*peers, ",") {
		if p = strings.TrimSpace(p); p != "" {
			boot = append(boot, p)
		}
	}
	pn := p2p.New(p2p.Config{Chain: c, Listen: *listen, Peers: boot, Logger: log.Default()})
	if err := pn.Start(); err != nil {
		log.Fatal("p2p: ", err)
	}
	if *listen != "" {
		log.Printf("p2p listening on %s, %d bootstrap peer(s)", pn.Addr(), len(boot))
	}
	go func() {
		if !*mine {
			return
		}
		if len(boot) > 0 {
			time.Sleep(3 * time.Second) // give the first handshake a chance to start a sync
		}
		for {
			if pn.Syncing() {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			if m != (chain.Hash{}) && c.IsUsed(m) {
				log.Println("miner address already signed a spend; rewards now go to the burn address. Restart with a fresh -miner address.")
				m = chain.Hash{}
			}
			b, err := c.Produce(m)
			if err != nil {
				log.Println("produce:", err)
			} else {
				pn.AnnounceBlock(b)
				log.Printf("%s block %d slot %d txs %d hash %s", chain.Ticker, b.Header.Height, b.Header.Slot, len(b.Txs), b.Header.Hash().String()[:16])
			}
			time.Sleep(*iv)
		}
	}()
	cors := func(f http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Content-Type", "application/json")
			f(w, r)
		}
	}
	js := func(w http.ResponseWriter, v any) { json.NewEncoder(w).Encode(v) }
	http.HandleFunc("/stats", cors(func(w http.ResponseWriter, r *http.Request) { js(w, c.Stats()) }))
	http.HandleFunc("/blocks", cors(func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("n"))
		if n <= 0 || n > 100 {
			n = 20
		}
		js(w, c.Recent(n))
	}))
	http.HandleFunc("/utxos", cors(func(w http.ResponseWriter, r *http.Request) {
		set := map[chain.Hash]bool{}
		for _, a := range r.URL.Query()["addr"] {
			if h, err := chain.ParseHash(a); err == nil {
				set[h] = true
			}
		}
		js(w, c.UTXOsFor(set))
	}))
	http.HandleFunc("/peers", cors(func(w http.ResponseWriter, r *http.Request) { js(w, pn.Peers()) }))
	http.HandleFunc("/tx", cors(func(w http.ResponseWriter, r *http.Request) {
		var t chain.Tx
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			http.Error(w, `{"error":"bad json"}`, 400)
			return
		}
		id, err := pn.SubmitTx(t)
		if err != nil {
			w.WriteHeader(400)
			js(w, map[string]string{"error": err.Error()})
			return
		}
		js(w, map[string]string{"txid": id.String()})
	}))
	log.Printf("QuantumCash devnet node on %s — experimental testnet, not money", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func load(p string) *chain.Wallet {
	b, err := os.ReadFile(p)
	if err != nil {
		log.Fatal("no wallet; run: qcash wallet new")
	}
	var w chain.Wallet
	json.Unmarshal(b, &w)
	return &w
}
func save(p string, w *chain.Wallet) {
	b, _ := json.MarshalIndent(w, "", "  ")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		log.Fatal(err)
	}
}

func utxos(node string, w *chain.Wallet) []chain.UTXO {
	q := node + "/utxos?"
	for a := range w.Addresses() {
		q += "addr=" + a.String() + "&"
	}
	r, err := http.Get(q)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Body.Close()
	var u []chain.UTXO
	json.NewDecoder(r.Body).Decode(&u)
	return u
}

func wallet(cmd string, args []string) {
	fs := flag.NewFlagSet("wallet", flag.ExitOnError)
	wp := fs.String("w", "wallet.json", "wallet file")
	node := fs.String("node", "http://localhost:8545", "node url")
	memo := fs.String("memo", "", "memo")
	var pos []string
	for len(args) > 0 {
		fs.Parse(args)
		args = fs.Args()
		if len(args) > 0 {
			pos = append(pos, args[0])
			args = args[1:]
		}
	}
	switch cmd {
	case "new":
		if _, err := os.Stat(*wp); err == nil {
			log.Fatal("wallet exists: ", *wp)
		}
		var w chain.Wallet
		rand.Read(w.Seed[:])
		a := w.NewAddress()
		save(*wp, &w)
		fmt.Println("new wallet saved to", *wp, "(keep it secret)\naddress:", a)
	case "address":
		w := load(*wp)
		a := w.NewAddress()
		save(*wp, w)
		fmt.Println(a)
	case "balance":
		w := load(*wp)
		var s uint64
		u := utxos(*node, w)
		for _, x := range u {
			s += x.Value
		}
		label := "outputs"
		if len(u) == 1 {
			label = "output"
		}
		fmt.Printf("%d.%08d QCASH in %d %s\n", s/chain.Unit, s%chain.Unit, len(u), label)
	case "send":
		if len(pos) != 2 {
			usage()
		}
		to, err := chain.ParseHash(pos[0])
		if err != nil {
			log.Fatal("bad address")
		}
		amt, err := strconv.ParseFloat(pos[1], 64)
		if err != nil || amt <= 0 {
			log.Fatal("bad amount")
		}
		w := load(*wp)
		tx, err := w.BuildTx(utxos(*node, w), to, uint64(amt*chain.Unit+0.5), 1000, *memo)
		if err != nil {
			log.Fatal(err)
		}
		save(*wp, w)
		b, _ := json.Marshal(tx)
		r, err := http.Post(*node+"/tx", "application/json", bytes.NewReader(b))
		if err != nil {
			log.Fatal(err)
		}
		var out map[string]string
		json.NewDecoder(r.Body).Decode(&out)
		if out["error"] != "" {
			log.Fatal("rejected: ", out["error"])
		}
		fmt.Println("sent, txid", out["txid"])
	default:
		usage()
	}
}
