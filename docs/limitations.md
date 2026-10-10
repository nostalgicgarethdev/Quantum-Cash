# Limitations

**Experimental testnet. Not money.** QCASH has no price, no sale, no bridge, and no contract address. The software is an experiment you run yourself. If someone sells you QCASH, they are not selling an asset this repository creates.

Version 0.1, October 2026. Not audited.

## Network

There is one process. It does not gossip, it does not learn about another tip, and it does not reorganize. "The chain" is the file that process writes. Two processes with different files are two unrelated devnets, even if both say QCASH.

The HTTP API has no authentication. CORS reflects a wildcard. The default listen address is every interface on port 8545.

## Consensus parameters

Proof-of-work defaults to 16 leading zero bits. That stops accidental garbage. It does not stop a laptop from rewriting a devnet. Tests use 8 bits. There is no retarget.

The header timestamp is not validated. The stored PoH tick counter is not checked against the slot. The thing that is checked is the recomputed SHA-256 chain described in [protocol.md](protocol.md).

Transaction ids and block hashes are SHA-256 of Go's `encoding/json` bytes. They are stable for this program. They are not a separately specified binary codec.

The subsidy follows a Bitcoin-shaped schedule (50 QCASH, halving every 210,000 blocks, about 21 million QCASH). Integer right-shifts make the total slightly under 21 million. Nothing in the code markets that cap, sells it, or pegs it to another asset.

## Signatures

`wots` is a compact Winternitz construction: SHA-256, `w=16`, 64 message nibbles, 3 checksum digits, 67 chains, 2,144-byte keys and signatures. It is not RFC 8391 WOTS+, not SPHINCS+ / SLH-DSA, and not ML-DSA.

Hash-based signatures are studied because Grover's algorithm gives only a square-root speedup against preimage search, while Shor's algorithm breaks the elliptic curves Bitcoin and Solana use. That is the motivation in the whitepaper. This implementation does not claim a NIST security level, a proof, or resistance to implementation bugs.

Keys are one-time. The chain burns an address after a spend is applied. A second signature created off-chain, including a retry of a failed send, is outside that check. Wallet files store the seed in plaintext. The coinbase is checked before spends in the same block are applied, so that block can still pay an address it also spends. Later blocks cannot.

## What the repo does not include

- Peers, mempool gossip, or a light client
- Encrypted wallets, hardware wallets, or a seed phrase word list
- Fees other than the fixed 1,000 quanta the CLI attaches, and the implicit input-minus-output fee
- A browser wallet. `site/index.html` only displays a node and runs a local hash animation
- An audit, a bug bounty, or a public network you can join

## Name

An inactive Waves token was called Quantum Cash and used the ticker QCASH. This project is not that token, not a bridge to it, and not a claim on the name beyond this GitHub repository: `github.com/nostalgicgarethdev/Quantum-Cash`.
