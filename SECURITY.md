# Security policy

QuantumCash is experimental devnet software and has not been audited. QCASH the node mines has no value and is not the Solana $QCash token. Please still report flaws that would let a transaction break the rules this repository claims to enforce.

## Report a flaw

Use a private GitHub security advisory:

[https://github.com/nostalgicgarethdev/Quantum-Cash/security/advisories/new](https://github.com/nostalgicgarethdev/Quantum-Cash/security/advisories/new)

Include the commit you tested, the package (`wots` or `chain`), and what a valid node accepts that it should reject. If private advisories are turned off for the repository, open a public issue that describes the symptom without a ready-to-replay transaction, and wait for a maintainer.

There is no bounty and no embargo beyond the time it takes to read the report. This software has no production deployment.

## In scope

- Verifying a W-OTS signature, or accepting a mutated one
- Spending an output that is already spent, or paying an address the chain has burned
- Accepting a coinbase above `subsidy(height) + fees`
- Accepting a proof-of-history hash or merkle root that does not match the block
- Wallet code that signs two different messages with one key during an ordinary CLI flow

## Known properties, not surprises

These are part of the design of the devnet. A report that only restates them will be closed.

- One process, no peers, no fork choice. The operator's node is the chain.
- Proof-of-work defaults to 16 leading zero bits. That is a toy.
- The HTTP server does not authenticate. The default bind is `:8545` (every interface). Use `-addr 127.0.0.1:8545` on a shared machine.
- CORS allows every origin. Chain data and transaction submission are available to any page that can reach the port.
- Wallet files are unencrypted JSON. Mode `0600` is the only protection the CLI applies.
- W-OTS keys are one-time. Retrying a failed send with a different message can leak the signing key. See [docs/wallet.md](docs/wallet.md).
- Transaction and block ids are hashes of Go `encoding/json` bytes.
- The signature scheme is the teaching construction in `wots/wots.go`, not a NIST standard.

## Supported versions

| Version | Status |
| --- | --- |
| `main` | The only line. Experimental. |
