# QuantumCash ($QCash · ticker QCASH)

> ⚠️ **Experimental testnet. Not money.** QCASH has no value, no sale, no token, no CA. It is a research toy. Do not use it to store anything you care about.

QuantumCash is a small, readable blockchain that mixes three ideas:

| From | Idea |
|---|---|
| **Bitcoin** | UTXO ledger, SHA-256 proof-of-work seal, 50 QCASH reward halving every 210,000 blocks, 1 QCASH = 100,000,000 quanta |
| **Solana** | Slots + a **Proof-of-History** clock: a sequential SHA-256 chain (12,500 ticks per slot) that mixes in each transaction, so the order is provable |
| **Post-quantum crypto** | Every spend is signed with **W-OTS** (Winternitz one-time signatures over SHA-256, w=16). No elliptic curves anywhere |

It is written from scratch in Go (it does not copy Bitcoin Core or the Solana validator code) so it fits in a few files you can actually read.

## Quick start

```bash
go build -o qcash ./cmd/qcash
./qcash wallet new -w alice.json          # makes a seed + first address
MINER=$(./qcash wallet address -w alice.json)
./qcash node -miner $MINER                # single-node devnet on :8545
# in another terminal
./qcash wallet balance -w alice.json
./qcash wallet new -w bob.json
./qcash wallet send <bob-address> 12.5 -w alice.json -memo gm
./qcash wallet balance -w bob.json
```

Node HTTP API: `GET /stats`, `GET /blocks?n=20`, `GET /utxos?addr=..`, `POST /tx`.
Open the explorer site and point it at `http://localhost:8545` to watch your node.

## Tests

```bash
go test ./...
```
Covers: signing/forgery, mining + spending, double-spend rejection, tampered tx rejection, one-time key reuse protection, bad PoH / inflated coinbase rejection, persistence replay, halving.

## One-time keys (important)
A W-OTS key may sign **once**. QuantumCash enforces this in consensus: once an address has spent, it is burned forever, and nobody (not even a miner) can pay to it again. The wallet derives a fresh key for every change output from one seed. Mine to a fresh address each time you restart.

## Status / limitations
See [WHITEPAPER.md](WHITEPAPER.md#honest-limitations). Short version: single-node devnet, no P2P, no fork choice, toy difficulty, unaudited.

License: MIT
