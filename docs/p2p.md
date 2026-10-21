# Peer-to-peer network (v0.2)

Package `p2p` connects `qcash node` processes over plain TCP. It uses only the Go standard library.

## Run a local network

```bash
go build -o qcash ./cmd/qcash
./qcash wallet new -w miner.json
MINER=$(./qcash wallet address -w miner.json)

# node 1 mines
./qcash node -data n1.json -addr 127.0.0.1:8545 -listen :7001 -miner "$MINER"
# node 2 relays and syncs from node 1
./qcash node -data n2.json -addr 127.0.0.1:8546 -listen :7002 -peers 127.0.0.1:7001 -mine=false
# node 3 only knows node 2, and finds node 1 through address exchange
./qcash node -data n3.json -addr 127.0.0.1:8547 -listen :7003 -peers 127.0.0.1:7002 -mine=false

curl -s localhost:8547/peers
```

A transaction sent to any node (`wallet send ... -node http://localhost:8547`) reaches the miner by gossip. Every node must use the same `-bits`, because the difficulty is part of the network id.

## Wire format

Each message is a 4-byte big-endian length followed by JSON: `{"type":"...","data":{...}}`. The node drops any frame over 16 MiB and bans the sender.

| Type | Purpose |
| --- | --- |
| `version` / `verack` | Handshake: protocol version, network id (SHA-256 of the genesis PoH seed and difficulty), height, cumulative work, tip, listen port, and a random nonce that detects self-connections and duplicate connections |
| `getaddr` / `addr` | Address exchange, up to 100 addresses for each message and 1,000 in the address book |
| `tx` | Mempool gossip. The node validates each tx in full (signatures, one-time keys, double spends against the UTXO set and the mempool) before relaying it, and keeps a 20,000-entry seen set so it does not relay the same tx twice |
| `block` | Propagates a new block, which is fully validated before it is relayed |
| `getblocks` / `blocks` | Sync. The request carries a block locator (dense near the tip, then exponentially sparse). The reply has up to 50 main-chain blocks after the fork point, and the requester keeps asking while the peer has more work |
| `ping` / `pong` | Keepalive. A peer that is silent for 3 ping intervals is dropped |

## Fork choice

Each block is checked in this order: its size and transaction count, then difficulty, proof of work, merkle root, timestamp (at most 2 hours ahead) and PoH tick count. After that it goes to `chain.ProcessBlock`:

- **Extends the tip:** full validation (PoH replay, W-OTS signatures, the one-time key rule, double spends, coinbase limit), then applied.
- **Parent known but not the tip:** stored as a side branch. When a branch has **strictly more cumulative work** (the sum of `2^bits`), the node replays the common prefix and validates every branch block in full on a scratch state. Live state is swapped only if all of them pass. Transactions from the disconnected blocks go back into the mempool if they are still valid. An invalid branch block is deleted along with its descendants.
- **Parent unknown:** treated as an orphan, and the node asks that peer to sync.

## Misbehavior and limits

| Event | Score |
| --- | --- |
| Invalid block | +100 (ban) |
| Oversized frame | ban |
| Malformed message | +20 |
| Unknown message type, or claiming more work while sending nothing new | +10 |
| Invalid tx | +5. A tx can be honestly stale, so this stays small |
| Orphan block | +2 |

At a score of 100 the IP is banned for 24 hours. The connection limits are 32 inbound, 8 outbound, and 4 inbound for each IP. Each peer has a 256-message send queue, and a peer that cannot keep up is dropped. Consensus also caps a block at 1,000 transactions, 8 MiB, and 64 skipped PoH slots.
