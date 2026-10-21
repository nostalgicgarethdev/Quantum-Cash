# Devnet

A `qcash node` process mines on a timer, talks to peers over TCP (see [p2p.md](p2p.md)), stores blocks in a JSON file, and serves the HTTP API in [http-api.md](http-api.md).

## Flags

```text
qcash node [-data chain.json] [-addr :8545] [-listen :7001] [-peers host:port,...] [-mine=true] [-miner ADDRESS] [-bits 16] [-interval 2s]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `-data` | `chain.json` | Block log. Rewritten via `chain.json.tmp` and renamed into place |
| `-addr` | `:8545` | Listen address. `:8545` is every interface. Use `127.0.0.1:8545` unless you mean to share it |
| `-listen` | `:7001` | P2P listen address. Empty means outbound connections only |
| `-peers` | empty | Comma-separated bootstrap peers (`host:port`) |
| `-mine` | `true` | Produce blocks. `-mine=false` gives a relay and sync node. Mining pauses while a peer reports more work |
| `-miner` | empty | Hex address that receives the subsidy. Empty pays the all-zero address |
| `-bits` | `16` | Leading zero bits required in the block hash |
| `-interval` | `2s` | Sleep between `Produce` calls. The PoH clock is the hash chain, not this timer |

The timer is the miner's pace. The proof-of-history hash is 12,500 SHA-256 steps per slot, independent of wall-clock time. A verifier does not check `-interval`.

## First blocks

```bash
go build -o qcash ./cmd/qcash
./qcash wallet new -w alice.json
MINER=$(./qcash wallet address -w alice.json)
./qcash node -addr 127.0.0.1:8545 -miner "$MINER"
```

The log line for a block looks like:

```text
QCASH block 0 slot 0 txs 1 hash 0000..............
```

The hash prefix is 16 hex characters of the block hash. Leading zeros are the proof-of-work.

Stopping the process and starting it again with the same `-data` file replays the blocks. The mempool does not survive. If replay fails, the node exits and leaves the file in place.

## Explorer

The explorer is published at [https://quantumcash.network](https://quantumcash.network). The source is the static file [../site/index.html](../site/index.html).

- "Start clock" hashes 12,500 SHA-256 steps per slot in the browser, with `crypto.subtle.digest`. That animation is not your node and it does not extend `chain.json`.
- "Connect" polls `GET /stats` and `GET /blocks?n=10` on the URL in the field, every 2 seconds. The default URL is `http://localhost:8545`.

Open the file directly, or serve the repository root:

```bash
python3 -m http.server 8080
```

Then browse `http://127.0.0.1:8080/site/`. The logo path is `../assets/logo.svg`, so the site expects to live at `site/` inside this repository.

## Tests as a devnet

`go test ./...` mines with 8-bit difficulty in temporary directories. It does not bind a port. Use it as the check that consensus helpers still accept a spend and still reject the cases listed in the README.
