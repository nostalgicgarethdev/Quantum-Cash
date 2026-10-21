# QuantumCash public testnet

> Experimental testnet. QCASH on testnet has **no value** and the chain may be reset.
> Not related to the $QCash Solana token.

The testnet is a separate network from the local devnet: it has its own genesis
seed (`QuantumCash public testnet-1 genesis — 2026-10-21 — not money`), PoW at 18
bits, p2p on port **7101** and the HTTP API on **8645**. Devnet and testnet nodes
refuse to handshake with each other.

## Join in one command

```sh
go install github.com/nostalgicgarethdev/Quantum-Cash/cmd/qcash@latest && qcash node -network testnet
```

`-network testnet` dials the built-in seed list. While the list is empty
(before public seeds launch), point at any node yourself:

```sh
qcash node -network testnet -seeds seed1.quantumcash.network:7101
```

Useful flags: `-mine=false` (relay only), `-miner <address>` (rewards),
`-seeds none` (start a private testnet), `-readonly` (disable `POST /tx`).

Docker:

```sh
docker build -t qcash . && docker run -d -p 7101:7101 -v qcash:/data qcash
```

## Run seed nodes

- One box, three seeds: `docker compose up -d --build`
- A real server: `scripts/deploy-seed.sh root@<ip> seed1.quantumcash.network`
  builds the binary, installs `deploy/qcash-seed.service`, opens port 7101
  with ufw, and puts the API behind HTTPS with Caddy (needs a DNS A record).
- Then add the hostname to `Testnet.Seeds` in `chain/network.go` and release.

## Explorer API (read-only, CORS `*`, 10 req/s per IP, burst 40)

| Endpoint | Returns |
|---|---|
| `GET /stats` | network, height, tip, supply, mempool size, peers, bits, work |
| `GET /blocks?n=20` | newest blocks first (max 100) |
| `GET /blocks?from=H&n=20` | blocks from height H upward |
| `GET /block/:hash` or `/block/:height` | one block with its `hash` |
| `GET /tx/:id` | a transaction with `height` (-1 = mempool) and `block_hash` |
| `GET /peers` | connected peers (inbound IPs hidden) |
| `GET /mempool` | pending transactions |
| `GET /health` | `{"ok":true}` |

The quantumcash.network Explorer tab reads these endpoints from the API URL
configured in the site.
