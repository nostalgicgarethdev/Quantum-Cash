# Wallet

A wallet is one 32-byte seed and a counter. Key index `i` is derived with `wots.KeyFromSeed`. The same seed and index always produce the same key, on any machine running this code.

## File

`qcash wallet new -w alice.json` writes mode `0600`:

```json
{
  "seed": "64 hex characters",
  "next": 1
}
```

`seed` is the secret. `next` is the next index `NewAddress` will hand out. Indexes `0` through `next-1` are the addresses the balance and send commands scan. The file is not encrypted. Anyone who can read it can spend every output those addresses still hold, and can sign a second message if you have not published one yet.

The names `wallet.json`, `alice.json`, and `bob.json` are gitignored. A different `-w` path is not. Do not commit wallet files.

## Commands

```text
qcash wallet new      [-w wallet.json]
qcash wallet address  [-w wallet.json]
qcash wallet balance  [-w wallet.json] [-node http://localhost:8545]
qcash wallet send TO AMOUNT_QCASH [-w wallet.json] [-node URL] [-memo text]
```

`new` refuses to overwrite an existing file. It allocates index 0 and prints two lines. `address` allocates one more index, saves the file, and prints only the hex address.

`balance` asks the node for UTXOs of every derived address and prints the sum. Amounts use eight decimal places.

`send` parses `AMOUNT_QCASH` as a float and converts it with `uint64(amount * Unit + 0.5)`. Use values that are exact in decimal, such as `12.5` or `1`. The fee is fixed at 1,000 quanta. The memo is optional and is covered by the sighash.

## How a send picks inputs

`BuildTx` takes UTXOs until `amount + fee` is covered, then adds every other UTXO that sits on those same addresses. A W-OTS address cannot sign again later, so leaving a remainder on it would strand the coins. Change, if any, goes to `NewAddress()`.

The CLI saves the wallet, including the new change index, before it posts the transaction.

## Do not sign twice

Consensus burns an address when a spend is applied, and it rejects a second mempool transaction that uses the same address under a different sighash. That does not protect you from a signature that never gets published.

If `send` prints an error, or the node was down, the process may already have signed. Building another transaction that spends those outputs signs again. Two published signatures under different messages are enough to forge with this construction. Move on with a fresh wallet rather than retrying a different payment from the same seed state. Restoring the JSON file from before the failed send and submitting that original transaction once is the safer retry.

Mining does not sign. An address can receive many coinbase outputs until it spends. The burn is recorded when the spend is applied, and the coinbase is checked before that, so the block that includes the spend can still pay the miner address. After that block, restart the node with a new `-miner` address. Otherwise later subsidies go to the all-zero address.
