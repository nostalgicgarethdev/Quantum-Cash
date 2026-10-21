// Package p2p connects QuantumCash nodes over TCP. Standard library only.
//
// Wire format: every message is a 4-byte big-endian length followed by that many
// bytes of JSON: {"type": "...", "data": {...}}. Messages larger than MaxMessage
// are refused and the sender is banned.
package p2p

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"

	"github.com/nostalgicgarethdev/Quantum-Cash/chain"
)

const (
	ProtocolVersion = 2
	UserAgent       = "qcash/0.2"
	MaxMessage      = 16 << 20 // bytes; a full block is capped at 8 MiB
	MaxAddrs        = 100      // addresses per addr message
	MaxBlocksPerMsg = 50       // blocks per sync batch
)

// Message types.
const (
	MsgVersion   = "version"
	MsgVerack    = "verack"
	MsgPing      = "ping"
	MsgPong      = "pong"
	MsgGetAddr   = "getaddr"
	MsgAddr      = "addr"
	MsgTx        = "tx"
	MsgBlock     = "block"
	MsgGetBlocks = "getblocks"
	MsgBlocks    = "blocks"
)

type envelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Version is the handshake. Both sides send one; peers on another network
// (different genesis seed or difficulty) are dropped.
type Version struct {
	Protocol  int        `json:"protocol"`
	Network   chain.Hash `json:"network"`
	Height    uint64     `json:"height"`
	Work      uint64     `json:"work"`
	Tip       chain.Hash `json:"tip"`
	Listen    string     `json:"listen,omitempty"` // port others can dial us on
	Nonce     uint64     `json:"nonce"`            // detects connections to ourselves
	UserAgent string     `json:"user_agent"`
}

type Ping struct {
	Nonce uint64 `json:"nonce"`
}
type Addr struct {
	Addrs []string `json:"addrs"`
}
type GetBlocks struct {
	Locator []chain.Hash `json:"locator"`
}
type Blocks struct {
	Blocks []chain.Block `json:"blocks"`
	Height uint64        `json:"height"` // sender's main-chain height
	Work   uint64        `json:"work"`
}
type BlockMsg struct {
	Block chain.Block `json:"block"`
	Work  uint64      `json:"work"` // sender's chain work after this block
}

var errTooBig = errors.New("message too large")

func writeMsg(w io.Writer, typ string, v any) error {
	var raw json.RawMessage
	if v != nil {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		raw = b
	}
	b, err := json.Marshal(envelope{typ, raw})
	if err != nil {
		return err
	}
	if len(b) > MaxMessage {
		return errTooBig
	}
	buf := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(buf, uint32(len(b)))
	copy(buf[4:], b)
	_, err = w.Write(buf)
	return err
}

func readMsg(r io.Reader) (envelope, error) {
	var e envelope
	var l [4]byte
	if _, err := io.ReadFull(r, l[:]); err != nil {
		return e, err
	}
	n := binary.BigEndian.Uint32(l[:])
	if n > MaxMessage {
		return e, errTooBig
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return e, err
	}
	err := json.Unmarshal(b, &e)
	return e, err
}
