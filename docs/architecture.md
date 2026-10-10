# Architecture

QuantumCash is one process. It keeps the chain in memory, writes blocks to a JSON file, and exposes four HTTP routes. Nothing in this repository dials another node.

```text
cmd/qcash     CLI: node loop, wallet, HTTP handlers
chain         UTXO set, PoH, proof-of-work, mempool, wallet helpers
wots          W-OTS-style key derivation, sign, verify, address
site          static page: a browser clock and a read-only explorer
```

## Validation order

`Chain.apply` in `chain/chain.go` accepts a block only if every step below succeeds.

```mermaid
flowchart TD
  block["Incoming block"] --> extend{"Height, previous hash, and slot extend the tip?"}
  extend -->|no| reject["Reject"]
  extend -->|yes| bits{"Difficulty bits match the node, and the header hash has that many leading zero bits?"}
  bits -->|no| reject
  bits -->|yes| merkle{"Merkle root matches the transactions?"}
  merkle -->|no| reject
  merkle -->|yes| poh{"PoH replays, including empty skipped slots?"}
  poh -->|no| reject
  poh -->|yes| coin{"First transaction is a coinbase within subsidy plus fees, and it does not pay a burned address?"}
  coin -->|no| reject
  coin -->|yes| rest{"Every later transaction verifies and respects one-time keys?"}
  rest -->|no| reject
  rest -->|yes| commit["Update the UTXO set, burn spent addresses, drop invalid mempool entries, save"]
```

The first block is the tip of an empty chain: previous hash all zeros, height 0, slot 0, PoH continuing from the genesis personalization string in `GenesisPoH`.

## UTXO set

```mermaid
flowchart LR
  inputs["Inputs consume outpoints"] --> set["map of outpoint to output"]
  outputs["Outputs create outpoints"] --> set
  inputs --> burn["Signing address is marked used"]
  burn --> refuse["Later outputs to that address fail"]
```

An outpoint is a transaction id plus an output index. The id is a hash of the JSON transaction, so it commits to the memo and to the signatures. See [protocol.md](protocol.md).

## Mempool

`SubmitTx` holds transactions in a slice on the process. The slice is not written to disk. On restart the mempool is empty even when `chain.json` reloads.

A submission is checked against the UTXO set and against transactions already in the slice. The same outpoint cannot be spent twice. The same address cannot sign two different sighashes. After a block is applied, transactions that no longer verify are dropped.

## Produce

`Produce` copies the mempool under the lock, builds a coinbase to the miner address, runs PoH, then searches for a proof-of-work nonce. It releases the lock during that search and calls `AddBlock` afterwards. If the miner address is already burned, the coinbase pays the all-zero address instead.

The node goroutine in `cmd/qcash` calls `Produce` on a timer (`-interval`, default 2s).
