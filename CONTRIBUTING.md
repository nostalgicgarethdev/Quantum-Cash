# Contributing to QuantumCash

QuantumCash is experimental, unaudited devnet software. QCASH the node mines has no value and is not the Solana $QCash token. Patches that keep the code readable and the claims accurate are welcome.

## Build and test

Go 1.24 or newer:

```bash
go build ./...
go test ./...
go vet ./...
gofmt -l .
```

CI runs those four checks on Ubuntu with Go 1.24. `gofmt -l .` must print nothing.

The devnet CLI:

```bash
go build -o qcash ./cmd/qcash
./qcash node -addr 127.0.0.1:8545 -miner "$(./qcash wallet address -w alice.json)"
```

Do not commit `chain.json`, wallet files, or the `qcash` binary. A wallet file is an unencrypted seed.

## What to change with a test

Consensus behavior lives in `chain/` and `wots/`. A change to what a block or transaction accepts needs a test that fails on the old behavior, plus a sentence in [docs/protocol.md](docs/protocol.md) or [WHITEPAPER.md](WHITEPAPER.md) so the docs match the code.

Please leave the node dependency-free unless the standard library cannot do the job. Say why in the pull request if you add a module requirement.

## Claims

Documentation, the site, and commit messages should describe this repository, not a product it does not ship.

- Call the node an experimental, unaudited devnet. QCASH it mines has no value and is not the Solana $QCash token.
- Call the signature code a W-OTS-style teaching construction over SHA-256. It is not RFC 8391 WOTS+, SLH-DSA, or ML-DSA.
- Do not describe the proof-of-history loop as Solana's production clock. It is 12,500 sequential SHA-256 ticks per slot.
- Do not describe the process as a peer-to-peer network. It is one node.

## Reports

Bugs in the CLI, explorer, or docs can be GitHub issues. Use the templates.

Flaws in signature verification, the one-time-address rule, or the coinbase bound should go through [SECURITY.md](SECURITY.md), not a public issue with a working input attached.

There is no bug bounty.
