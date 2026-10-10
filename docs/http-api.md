# HTTP API

The node serves JSON. Responses set `Access-Control-Allow-Origin: *` and `Content-Type: application/json`. There is no authentication.

Start a node bound to localhost, then point the examples at it:

```bash
./qcash node -addr 127.0.0.1:8545 -miner "$MINER"
```

The default `-addr` is `:8545`, which is every interface. Do not publish that port.

## `GET /stats`

```bash
curl -s http://127.0.0.1:8545/stats
```

```json
{
  "ticker": "QCASH",
  "height": 0,
  "tip": "hex block hash",
  "supply": 5000000000,
  "utxos": 1,
  "mempool": 0,
  "bits": 16,
  "poh_ticks": 12500
}
```

`supply` and transaction values are quanta, not QCASH. Divide by 100,000,000 to print QCASH. Before the first block, `height` is 0 and `tip` is empty, so an empty `tip` means "no blocks yet" rather than "the genesis block".

`poh_ticks` is copied from the tip header. See [protocol.md](protocol.md) for what is and is not checked.

## `GET /blocks?n=20`

```bash
curl -s "http://127.0.0.1:8545/blocks?n=5"
```

Returns the newest blocks first, each as stored: `header` plus `txs`. `n` below 1 or above 100 becomes 20.

## `GET /utxos?addr=HEX`

```bash
curl -s "http://127.0.0.1:8545/utxos?addr=$MINER"
```

Repeat the query parameter to ask for several addresses. Unknown or malformed `addr` values are skipped. The body is a JSON array of UTXOs. Each object includes `txid`, `index`, `value`, and `address`.

## `POST /tx`

```bash
curl -s -X POST http://127.0.0.1:8545/tx \
  -H 'content-type: application/json' \
  --data-binary @tx.json
```

A valid body is a transaction object with `inputs`, `outputs`, and an optional `memo`. Public keys and signatures are base64 in JSON, matching `encoding/json`.

Success:

```json
{"txid":"64 hex characters"}
```

Rejection is HTTP 400:

```json
{"error":"missing or spent input"}
```

The error strings are the ones returned by `checkTx` and the JSON decoder. A body that is not JSON is also HTTP 400, with the text `{"error":"bad json"}`.

The transaction sits in the mempool until the next block this process produces. Nothing here broadcasts it to another computer.
