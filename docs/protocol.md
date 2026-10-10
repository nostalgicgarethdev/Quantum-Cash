# Protocol

This note matches `wots/wots.go` and `chain/chain.go`. Hashes are SHA-256. Integers inside preimages are big-endian. The ticker is QCASH. 1 QCASH is 100,000,000 quanta (`Unit`).

## Address

A W-OTS public key is 67 chains times 32 bytes, 2,144 bytes, concatenated in chain order.

```text
address = SHA-256( "qc-addr" || pubkey_bytes )
```

The address is 32 bytes. The CLI prints it as 64 hex characters. The public key is not part of the address and appears on chain only when the address spends.

## Keys

```text
sk[i] = SHA-256( "qc-sk" || seed || index_be64 || byte(i) )
pk[i] = chain(sk[i], start=0, steps=15, i)
```

`seed` is 32 bytes. `index` is the wallet key index. `i` runs from 0 through 66.

```text
chain(x, start, steps, i):
  for j = start; j < start+steps; j++:
    x = SHA-256( "qc-wots" || byte(i) || byte(j) || x )
```

`w` is 16, so each chain is 15 steps long (`W-1`).

## Message digits

The signed message is the 32-byte sighash. It becomes 64 nibbles, high nibble of each byte first. The checksum is

```text
c = sum over the 64 nibbles of (15 - nibble)
```

The maximum checksum is 64 * 15 = 960, which fits in three base-16 digits:

```text
digit[64] = (c >> 8) & 15
digit[65] = (c >> 4) & 15
digit[66] = c & 15
```

Signing advances chain `i` by `digit[i]` steps from the secret. Verification advances the signature the rest of the way and compares the result to the public key.

Signing two different messages with one secret reveals intermediate chain values. The scheme has no public randomization. Treat every key as strictly one-time.

## Sighash and transaction id

The sighash covers inputs and outputs and the memo. It does not cover public keys or signatures.

```text
sighash = SHA-256(
  "qc-sighash"
  || for each input:  prev_txid || index_be32
  || for each output: value_be64 || address
  || memo_bytes
)
```

The transaction id is different. It is the hash of this program's JSON encoding of the whole transaction, signatures included:

```text
txid = SHA-256( "qc-tx" || encoding/json(tx) )
```

Go's `encoding/json` field order for `Tx` is `inputs`, `outputs`, `memo`. Byte slices become base64. `<`, `>`, and `&` in a memo are escaped. A second implementation has to reproduce that encoding or it will not agree on ids. The same caveat applies to the block header hash:

```text
block_hash = SHA-256( "qc-block" || encoding/json(header) )
```

## Transactions

A non-coinbase transaction must satisfy `checkTx`:

- Every input outpoint is still unspent, including inside the block and the mempool snapshot under check.
- The public key decodes to 2,144 bytes and its address equals the output being spent.
- The signature verifies under that public key and the sighash.
- That address is not already burned. Inside one block or mempool check, the address may appear again only with the same sighash. A second outpoint of the same address in one transaction shares a sighash, which is allowed. Two transactions with different sighashes are not.
- Every output value is non-zero.
- No output address is burned.
- The sum of outputs does not exceed the sum of inputs.

The fee is the remainder. It is not a separate field.

The wallet spends every UTXO on an address it touches, because a half-spent address cannot sign a second time. See [wallet.md](wallet.md).

## Coinbase and subsidy

The first transaction of a block has no inputs. Its outputs must not pay a burned address. The sum of its output values must be less than or equal to `Reward(height) + fees` from the other transactions.

```text
Reward(height) = (50 * Unit) >> (height / 210_000)
```

After 64 halvings the shift is defined to return 0. The coinbase may pay less than the full allowance. Output values on the coinbase are not checked for zero.

`Produce` pays the miner one output of `Reward(height) + fees`, unless that miner address is already burned, in which case it pays the all-zero address.

The burned-address check looks at the set from before the block. `apply` writes coinbase outputs before it marks later spenders as used. A miner address can still be paid in the block that spends it. The following block sees the burn, and `Produce` switches that miner to the all-zero address.

## Merkle root

Transaction ids are the leaves. If a level has an odd count, the last id is duplicated. Each parent is `SHA-256(left || right)` with no domain tag. An empty list hashes to 32 zero bytes. Blocks in this node are not empty: the coinbase is required.

## Proof of history

```text
TicksPerSlot = 12500
```

`PoHRun(start, txs)` mixes transaction `i` at tick `(i * TicksPerSlot) / (len(txs) + 1)`. If two transactions land on the same tick, the later id overwrites the earlier one. With a handful of transactions and 12,500 ticks, that collision does not come up. A tick with a mix hashes `SHA-256(state || txid)`. A tick without a mix hashes `SHA-256(state)`.

Verification starts from the previous block's PoH (or `GenesisPoH` before any block). For each slot number strictly between the expected next slot and the block's slot, it runs `PoHRun` with no transactions. It then runs `PoHRun` over the block's transactions. The result must equal `header.poh`.

`header.poh_ticks` is written by `Produce` as `(slot + 1) * TicksPerSlot`. It is inside the header JSON, so it affects the proof-of-work hash. `apply` does not compare it to the slot.

`header.time` is the producer's wall clock. It is not checked against the previous block or against the verifier's clock.

## Proof of work

`header.bits` is the number of leading zero bits required in `block_hash`. After the genesis block, `header.bits` must equal the node's configured bits. The devnet default is 16. Passing `-bits 0` to a fresh node lets the first block set the value, because the equality check starts once a block exists.

Leading zeros are counted across the 32-byte hash with `bits.LeadingZeros8`.
