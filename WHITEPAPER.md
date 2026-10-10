# QuantumCash: a post-quantum UTXO chain with a Proof-of-History clock

*Experimental testnet. Not money. Not audited. Version 0.1 (October 2026).*

## 1. Why
Bitcoin and Solana both sign transactions with elliptic curves (secp256k1, Ed25519). A large enough quantum computer running Shor's algorithm could forge those signatures. Hash functions are much less exposed: Grover's algorithm only gives a square-root speedup. QuantumCash asks: what does a simple coin look like if **every** spend is signed with hash-based crypto only?

## 2. Ledger (from Bitcoin)
- UTXO model. A transaction consumes outputs and creates new ones; `inputs ≥ outputs`, the gap is the fee.
- An address is `SHA-256("qc-addr" ‖ W-OTS public key)`. The public key is only revealed when you spend.
- Coinbase: 50 QCASH, halving every 210,000 blocks, plus fees. Total cap ≈ 21M QCASH.
- Blocks are sealed with SHA-256 proof-of-work (leading zero bits; devnet default 16).

## 3. Clock (from Solana)
Each block occupies a **slot**. The block carries a Proof-of-History hash: starting from the previous block's PoH value, the producer computes 12,500 sequential SHA-256 hashes, mixing each transaction id in at a fixed tick. Empty slots must still be ticked through. Every node re-runs the sequence to verify, so the transaction order and the passage of slots are provable. (Real Solana verifies PoH in parallel on GPUs; ours is sequential and small.)

## 4. Signatures (post-quantum)
W-OTS with n=32 bytes, w=16: 64 message digits + 3 checksum digits = 67 hash chains. Public key and signature are each 2,144 bytes (vs 64 bytes for Ed25519). That size is the real cost of hash-based security.

W-OTS keys are **one-time**: signing two different messages leaks enough to forge. Consensus therefore:
1. marks an address as burned after it spends;
2. rejects any output (including coinbase) paying a burned address;
3. rejects two different transactions from the same address in one block/mempool.

Wallets derive key *i* from a 32-byte seed, so a single backup covers all addresses.

## 5. Roadmap ideas
- XMSS / SPHINCS+ (SLH-DSA) or ML-DSA (Dilithium) for reusable post-quantum addresses
- P2P gossip + heaviest-chain fork choice
- Difficulty retargeting, leader schedule per slot
- Parallel PoH verification

## Honest limitations
- **Single node.** No P2P networking, no fork choice, no reorgs. It's a devnet you run yourself.
- **Toy security parameters.** Difficulty is tiny; anyone can rewrite the chain.
- **W-OTS is one-time.** Reuse protection is enforced by the chain, but a careless wallet copy that signs twice off-chain could still leak a key.
- **Not audited.** Built quickly as an experiment. Bugs are likely.
- **Not money.** No token, no sale, no bridge, no CA. If someone sells you "QCASH", it's not this.
- The name collides with an old, inactive Waves token called "Quantum Cash (QCASH)". This project has no relation to it.
