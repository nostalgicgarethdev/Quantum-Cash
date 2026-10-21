package p2p

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"log"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/nostalgicgarethdev/Quantum-Cash/chain"
)

// Config for a Node. Zero values get sensible defaults.
type Config struct {
	Chain       *chain.Chain
	Listen      string   // e.g. ":7001"; empty = outbound only
	Peers       []string // bootstrap peers host:port
	MaxInbound  int      // default 32
	MaxOutbound int      // default 8
	MaxPerIP    int      // inbound connections per IP, default 4
	BanScore    int      // default 100
	BanTime     time.Duration
	Logger      *log.Logger
	// test hooks
	DialInterval time.Duration // default 5s
	PingInterval time.Duration // default 60s
}

// Misbehavior penalties.
const (
	scoreInvalidBlock = 100
	scoreBadMessage   = 20
	scoreInvalidTx    = 5
	scoreUnsolicited  = 10
	scoreOrphanSpam   = 2
)

type Node struct {
	cfg     Config
	c       *chain.Chain
	nonce   uint64
	ln      net.Listener
	logf    func(string, ...any)
	quit    chan struct{}
	wg      sync.WaitGroup
	mu      sync.Mutex
	peers   map[*peer]bool
	addrs   map[string]time.Time // address book: dialable host:port -> last seen
	bans    map[string]time.Time // ip -> banned until
	dialing map[string]bool
	seenTx  *lru
	syncing bool
}

type peer struct {
	n        *Node
	conn     net.Conn
	inbound  bool
	addr     string // remote host:port
	dialAddr string // address we dialed, or the peer's advertised listen address
	ver      Version
	score    int
	work     uint64
	out      chan envelope
	done     chan struct{}
	once     sync.Once
	lastSync time.Time
}

// PeerInfo is what /peers shows.
type PeerInfo struct {
	Addr      string `json:"addr"`
	Inbound   bool   `json:"inbound"`
	Height    uint64 `json:"height"`
	Work      uint64 `json:"work"`
	Score     int    `json:"score"`
	UserAgent string `json:"user_agent"`
}

func New(cfg Config) *Node {
	if cfg.MaxInbound == 0 {
		cfg.MaxInbound = 32
	}
	if cfg.MaxOutbound == 0 {
		cfg.MaxOutbound = 8
	}
	if cfg.MaxPerIP == 0 {
		cfg.MaxPerIP = 4
	}
	if cfg.BanScore == 0 {
		cfg.BanScore = 100
	}
	if cfg.BanTime == 0 {
		cfg.BanTime = 24 * time.Hour
	}
	if cfg.DialInterval == 0 {
		cfg.DialInterval = 5 * time.Second
	}
	if cfg.PingInterval == 0 {
		cfg.PingInterval = 60 * time.Second
	}
	n := &Node{cfg: cfg, c: cfg.Chain, quit: make(chan struct{}), peers: map[*peer]bool{},
		addrs: map[string]time.Time{}, bans: map[string]time.Time{}, dialing: map[string]bool{}, seenTx: newLRU(20000)}
	var b [8]byte
	rand.Read(b[:])
	n.nonce = binary.BigEndian.Uint64(b[:])
	n.logf = func(string, ...any) {}
	if cfg.Logger != nil {
		n.logf = cfg.Logger.Printf
	}
	for _, p := range cfg.Peers {
		if p != "" {
			n.addrs[p] = time.Now()
		}
	}
	return n
}

// Start listens (if configured) and begins dialing bootstrap peers.
func (n *Node) Start() error {
	if n.cfg.Listen != "" {
		ln, err := net.Listen("tcp", n.cfg.Listen)
		if err != nil {
			return err
		}
		n.ln = ln
		n.wg.Add(1)
		go n.acceptLoop()
	}
	n.wg.Add(1)
	go n.dialLoop()
	return nil
}

// Addr returns the bound listen address.
func (n *Node) Addr() string {
	if n.ln == nil {
		return ""
	}
	return n.ln.Addr().String()
}

func (n *Node) Stop() {
	close(n.quit)
	if n.ln != nil {
		n.ln.Close()
	}
	n.mu.Lock()
	for p := range n.peers {
		p.close()
	}
	n.mu.Unlock()
	n.wg.Wait()
}

func (n *Node) Peers() []PeerInfo {
	n.mu.Lock()
	defer n.mu.Unlock()
	r := []PeerInfo{}
	for p := range n.peers {
		if p.ver.Nonce == 0 {
			continue
		}
		r = append(r, PeerInfo{p.addr, p.inbound, p.ver.Height, p.work, p.score, p.ver.UserAgent})
	}
	sort.Slice(r, func(i, j int) bool { return r[i].Addr < r[j].Addr })
	return r
}

// Syncing reports whether a connected peer claims more work than we have.
// Miners pause while this is true so they do not build on a stale tip.
func (n *Node) Syncing() bool {
	w := n.c.Work()
	n.mu.Lock()
	defer n.mu.Unlock()
	for p := range n.peers {
		if p.ver.Nonce != 0 && p.work > w {
			return true
		}
	}
	return false
}

func ipOf(addr string) string {
	h, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return h
}

func (n *Node) banned(ip string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	t, ok := n.bans[ip]
	if ok && time.Now().After(t) {
		delete(n.bans, ip)
		return false
	}
	return ok
}

func (n *Node) acceptLoop() {
	defer n.wg.Done()
	for {
		conn, err := n.ln.Accept()
		if err != nil {
			select {
			case <-n.quit:
				return
			default:
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}
		ip := ipOf(conn.RemoteAddr().String())
		if n.banned(ip) {
			conn.Close()
			continue
		}
		n.mu.Lock()
		in, perIP := 0, 0
		for p := range n.peers {
			if p.inbound {
				in++
				if ipOf(p.addr) == ip {
					perIP++
				}
			}
		}
		n.mu.Unlock()
		if in >= n.cfg.MaxInbound || perIP >= n.cfg.MaxPerIP {
			conn.Close()
			continue
		}
		n.addPeer(conn, true, "")
	}
}

func (n *Node) dialLoop() {
	defer n.wg.Done()
	t := time.NewTicker(n.cfg.DialInterval)
	defer t.Stop()
	for {
		n.dialSome()
		select {
		case <-n.quit:
			return
		case <-t.C:
		}
	}
}

func (n *Node) dialSome() {
	n.mu.Lock()
	out := len(n.dialing)
	connected := map[string]bool{}
	for p := range n.peers {
		if !p.inbound {
			out++
		}
		connected[p.dialAddr] = true
		connected[p.addr] = true
	}
	var cands []string
	for a := range n.addrs {
		if !connected[a] && !n.dialing[a] && a != n.Addr() {
			cands = append(cands, a)
		}
	}
	free := n.cfg.MaxOutbound - out
	if free > len(cands) {
		free = len(cands)
	}
	cands = cands[:max(free, 0)]
	for _, a := range cands {
		n.dialing[a] = true
	}
	n.mu.Unlock()
	for _, a := range cands {
		go func(a string) {
			defer func() { n.mu.Lock(); delete(n.dialing, a); n.mu.Unlock() }()
			if n.banned(ipOf(a)) {
				return
			}
			conn, err := net.DialTimeout("tcp", a, 5*time.Second)
			if err != nil {
				return
			}
			select {
			case <-n.quit:
				conn.Close()
				return
			default:
			}
			n.addPeer(conn, false, a)
		}(a)
	}
}

func (n *Node) addPeer(conn net.Conn, inbound bool, dialAddr string) {
	p := &peer{n: n, conn: conn, inbound: inbound, addr: conn.RemoteAddr().String(), dialAddr: dialAddr,
		out: make(chan envelope, 256), done: make(chan struct{})}
	n.mu.Lock()
	n.peers[p] = true
	n.mu.Unlock()
	n.wg.Add(2)
	go p.writeLoop()
	go p.readLoop()
}

func (p *peer) close() {
	p.once.Do(func() {
		close(p.done)
		p.conn.Close()
	})
}

func (p *peer) send(typ string, v any) {
	var raw json.RawMessage
	if v != nil {
		b, err := json.Marshal(v)
		if err != nil {
			return
		}
		raw = b
	}
	select {
	case p.out <- envelope{typ, raw}:
	case <-p.done:
	default:
		p.n.logf("p2p: %s send queue full, dropping peer", p.addr)
		p.close()
	}
}

func (p *peer) writeLoop() {
	defer p.n.wg.Done()
	w := bufio.NewWriter(p.conn)
	ping := time.NewTicker(p.n.cfg.PingInterval)
	defer ping.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-ping.C:
			p.send(MsgPing, Ping{uint64(time.Now().UnixNano())})
		case e := <-p.out:
			p.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if err := writeMsg(w, e.Type, e.Data); err != nil {
				p.close()
				return
			}
			if len(p.out) == 0 {
				if err := w.Flush(); err != nil {
					p.close()
					return
				}
			}
		}
	}
}

// misbehave adds to the peer's score; at BanScore the IP is banned and dropped.
func (p *peer) misbehave(pts int, why string) {
	n := p.n
	n.mu.Lock()
	p.score += pts
	s := p.score
	if s >= n.cfg.BanScore {
		n.bans[ipOf(p.addr)] = time.Now().Add(n.cfg.BanTime)
		delete(n.addrs, p.dialAddr)
	}
	n.mu.Unlock()
	n.logf("p2p: peer %s misbehaved (+%d = %d): %s", p.addr, pts, s, why)
	if s >= n.cfg.BanScore {
		n.logf("p2p: banned %s", ipOf(p.addr))
		p.close()
	}
}

func (n *Node) version() Version {
	v := Version{Protocol: ProtocolVersion, Network: chain.NetworkID(n.c.Bits), Height: n.c.Height(),
		Work: n.c.Work(), Tip: n.c.TipHash(), Nonce: n.nonce, UserAgent: UserAgent}
	if a := n.Addr(); a != "" {
		_, port, _ := net.SplitHostPort(a)
		v.Listen = port
	}
	return v
}

func (p *peer) readLoop() {
	n := p.n
	defer n.wg.Done()
	defer func() {
		p.close()
		n.mu.Lock()
		delete(n.peers, p)
		n.mu.Unlock()
		if p.ver.Nonce != 0 {
			n.logf("p2p: disconnected %s", p.addr)
		}
	}()
	r := bufio.NewReaderSize(p.conn, 64<<10)
	p.send(MsgVersion, n.version())
	// handshake must complete quickly
	p.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	e, err := readMsg(r)
	if err != nil || e.Type != MsgVersion {
		return
	}
	var v Version
	if json.Unmarshal(e.Data, &v) != nil || v.Nonce == 0 {
		p.misbehave(scoreBadMessage, "bad version")
		return
	}
	if v.Nonce == n.nonce {
		n.mu.Lock()
		delete(n.addrs, p.dialAddr) // that address is us
		n.mu.Unlock()
		return
	}
	if v.Network != chain.NetworkID(n.c.Bits) || v.Protocol < ProtocolVersion {
		n.logf("p2p: %s is on another network or version, dropping", p.addr)
		return
	}
	n.mu.Lock()
	for q := range n.peers {
		if q != p && q.ver.Nonce == v.Nonce { // already connected to this node
			n.mu.Unlock()
			return
		}
	}
	p.ver = v
	p.work = v.Work
	if p.inbound && v.Listen != "" {
		if port, err := strconv.Atoi(v.Listen); err == nil && port > 0 && port < 65536 {
			p.dialAddr = net.JoinHostPort(ipOf(p.addr), v.Listen)
			n.addrs[p.dialAddr] = time.Now()
		}
	}
	n.mu.Unlock()
	p.send(MsgVerack, nil)
	p.send(MsgGetAddr, nil)
	n.logf("p2p: connected %s (inbound=%v height=%d %s)", p.addr, p.inbound, v.Height, v.UserAgent)
	if v.Work > n.c.Work() {
		p.requestSync()
	}
	// relay our mempool so a fresh peer learns pending txs
	for _, t := range n.c.MempoolTxs() {
		p.send(MsgTx, t)
	}
	idle := 3 * n.cfg.PingInterval
	for {
		p.conn.SetReadDeadline(time.Now().Add(idle))
		e, err := readMsg(r)
		if err != nil {
			if errors.Is(err, errTooBig) {
				p.misbehave(n.cfg.BanScore, "oversized message")
			} else if _, ok := err.(*json.SyntaxError); ok {
				p.misbehave(scoreBadMessage, "malformed json")
				continue
			}
			return
		}
		if err := n.handle(p, e); err != nil {
			p.misbehave(scoreBadMessage, err.Error())
		}
		select {
		case <-p.done:
			return
		default:
		}
	}
}

func (p *peer) requestSync() {
	p.lastSync = time.Now()
	p.send(MsgGetBlocks, GetBlocks{p.n.c.Locator()})
}

func (n *Node) handle(p *peer, e envelope) error {
	switch e.Type {
	case MsgVerack, MsgPong:
	case MsgPing:
		var x Ping
		if json.Unmarshal(e.Data, &x) != nil {
			return errors.New("bad ping")
		}
		p.send(MsgPong, x)
	case MsgGetAddr:
		n.mu.Lock()
		var a []string
		for addr := range n.addrs {
			if len(a) >= MaxAddrs {
				break
			}
			a = append(a, addr)
		}
		n.mu.Unlock()
		p.send(MsgAddr, Addr{a})
	case MsgAddr:
		var x Addr
		if json.Unmarshal(e.Data, &x) != nil || len(x.Addrs) > MaxAddrs {
			return errors.New("bad addr")
		}
		n.mu.Lock()
		for _, a := range x.Addrs {
			if _, _, err := net.SplitHostPort(a); err == nil && len(n.addrs) < 1000 {
				if _, ok := n.addrs[a]; !ok {
					n.addrs[a] = time.Now()
				}
			}
		}
		n.mu.Unlock()
	case MsgTx:
		var t chain.Tx
		if json.Unmarshal(e.Data, &t) != nil {
			return errors.New("bad tx")
		}
		n.acceptTx(t, p)
	case MsgBlock:
		var x BlockMsg
		if json.Unmarshal(e.Data, &x) != nil {
			return errors.New("bad block")
		}
		n.mu.Lock()
		if x.Work > p.work {
			p.work = x.Work
		}
		n.mu.Unlock()
		n.acceptBlock(x.Block, p)
	case MsgGetBlocks:
		var x GetBlocks
		if json.Unmarshal(e.Data, &x) != nil || len(x.Locator) > chain.MaxLocator {
			return errors.New("bad getblocks")
		}
		p.send(MsgBlocks, Blocks{n.c.BlocksAfter(x.Locator, MaxBlocksPerMsg), n.c.Height(), n.c.Work()})
	case MsgBlocks:
		var x Blocks
		if json.Unmarshal(e.Data, &x) != nil || len(x.Blocks) > MaxBlocksPerMsg {
			return errors.New("bad blocks")
		}
		n.mu.Lock()
		if x.Work > p.work {
			p.work = x.Work
		}
		n.mu.Unlock()
		progressed := false
		for _, b := range x.Blocks {
			st, err := n.acceptBlock(b, p)
			if err != nil {
				return nil // acceptBlock already scored the peer
			}
			if st != chain.Duplicate {
				progressed = true
			}
		}
		// keep pulling while the peer is ahead and we are getting somewhere
		if x.Work > n.c.Work() && (progressed || len(x.Blocks) == MaxBlocksPerMsg) {
			p.requestSync()
		} else if x.Work > n.c.Work() && !progressed && len(x.Blocks) > 0 {
			p.misbehave(scoreUnsolicited, "claims more work but sent nothing useful")
		}
	default:
		p.misbehave(scoreUnsolicited, "unknown message "+e.Type)
	}
	return nil
}

func (n *Node) acceptTx(t chain.Tx, from *peer) {
	id := t.ID()
	if !n.seenTx.add(id) {
		return
	}
	if _, err := n.c.SubmitTx(t); err != nil {
		// a tx can be honestly invalid for us (already mined, raced double spend),
		// so this is a small penalty
		if from != nil {
			from.misbehave(scoreInvalidTx, "invalid tx: "+err.Error())
		}
		return
	}
	n.broadcast(MsgTx, t, from)
}

// SubmitTx validates a locally created tx and gossips it.
func (n *Node) SubmitTx(t chain.Tx) (chain.Hash, error) {
	id, err := n.c.SubmitTx(t)
	if err != nil {
		return id, err
	}
	n.seenTx.add(id)
	n.broadcast(MsgTx, t, nil)
	return id, nil
}

func (n *Node) acceptBlock(b chain.Block, from *peer) (chain.Status, error) {
	st, err := n.c.ProcessBlock(b)
	switch {
	case errors.Is(err, chain.ErrOrphan):
		if from != nil {
			from.misbehave(scoreOrphanSpam, "orphan block")
			if time.Since(from.lastSync) > time.Second {
				from.requestSync()
			}
		}
		return st, err
	case err != nil:
		if from != nil {
			from.misbehave(scoreInvalidBlock, err.Error())
		}
		return st, err
	}
	if st == chain.Extended || st == chain.Reorged {
		if st == chain.Reorged {
			n.logf("p2p: reorg to height %d tip %s", n.c.Height(), short(n.c.TipHash()))
		}
		n.broadcast(MsgBlock, BlockMsg{b, n.c.Work()}, from)
	}
	return st, nil
}

// AnnounceBlock gossips a block this node mined (already on the chain).
func (n *Node) AnnounceBlock(b chain.Block) {
	n.broadcast(MsgBlock, BlockMsg{b, n.c.Work()}, nil)
}

func (n *Node) broadcast(typ string, v any, except *peer) {
	n.mu.Lock()
	var ps []*peer
	for p := range n.peers {
		if p != except && p.ver.Nonce != 0 {
			ps = append(ps, p)
		}
	}
	n.mu.Unlock()
	for _, p := range ps {
		p.send(typ, v)
	}
}

func short(h chain.Hash) string { return h.String()[:16] }

// lru is a bounded set used to avoid re-gossiping the same tx.
type lru struct {
	mu    sync.Mutex
	max   int
	set   map[chain.Hash]bool
	order []chain.Hash
}

func newLRU(n int) *lru { return &lru{max: n, set: map[chain.Hash]bool{}} }

// add returns false if the id was already present.
func (l *lru) add(h chain.Hash) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.set[h] {
		return false
	}
	l.set[h] = true
	l.order = append(l.order, h)
	if len(l.order) > l.max {
		delete(l.set, l.order[0])
		l.order = l.order[1:]
	}
	return true
}
