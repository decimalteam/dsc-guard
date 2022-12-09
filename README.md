# Decimal Smart Chain Guard

Decimal Smart Chain Guard is a tool helping to monitor validator node and set it offline if node stops to sign blocks by any reason.

# Guard running

1. Start

2. Connect to multiple nodes: start partial watcher for every node
- watchers can infinity reconnect

3. Watch for blocks, validator set changes and count missed blocks
- periodicaly validate set_offline transaction

4. If validator is online and missed some count of blocks, then sends set_offline

5. Start watch without tx data
- if validator become online, report error about unprotected validator

6. Go to state 2-3 after get new set_offline tx data

# Guard configuration

To configure `guard` tool you should create file `.env` at directory `cmd/dsc-guard`. Example of the configuration:

```bash
NODES_ENDPOINTS=tcp://localhost:26657
MISSED_BLOCKS_LIMIT=8
MISSED_BLOCKS_WINDOW=24
NEW_BLOCK_TIMEOUT=10
FALLBACK_PAUSE=2
VALIDATOR_ADDRESS=1A42FDF9FC98931A4BB59EF571D61BB70417657D
HTTP_LISTENER=localhost:11111
SET_OFFLINE_TX=ab01282816a90a1a51f5833b0a14d0c71c31a891e5023ae63fd2bcf2732f04f32158120310be031a6a0a26eb5ae987210279f7e074d08a23e2fc7b7fd9e49a0d6570a28bf6c9cb988e92f678c32935097412407979e0cc483f241e48ed3c371d9d668a5b978fb474afc5fea5803c89bd2a2dac3db15eb84fef1fce25e783e279a33bac7b96bbe6786c9608d52c69baecacf9d02218446563696d616c2047756172642074726967676572726564
```

Where:

- `NODES_ENDPOINTS` - list of Decimal Node RPC endpoints which should be used to listen new blocks (can be specified several endpoints separated by `,`)
- `MISSED_BLOCKS_LIMIT` and `MISSED_BLOCKS_WINDOW` - when at least `MISSED_BLOCKS_LIMIT` blocks of last `MISSED_BLOCKS_WINDOW` blocks are missed to sign by monitoring validator `set_offline` transaction will be send to all connected nodes to turn of validator
- `NEW_BLOCK_TIMEOUT` - timeout of receiving new block in seconds (if no new blocks are received during this duration then assumed node is disconnected)
- `FALLBACK_PAUSE` - time in seconds for reconnect to node
- `VALIDATOR_ADDRESS` - validator address in hex format which should be monitored by the guard. Validator address can be found in file `$HOME/.decimal/daemon/config/priv_validator_key.json`
- `HTTP_LISTENER` - address and port to provide http page with JSON report (see below); if you don't need this feature, set it to empty
- `SET_OFFLINE_TX` - signed tx (ready to broadcast) in hex format which will be used to turn off validator when too many blocks are missed to sign

# Report page

Current status of guard for monitoring
For example if `HTTP_LISTENER=localhost:11111`, then `http://127.0.0.1:11111/` will return page with following JSON:

```
{
    "critical":"",
    "current_height":45080,
    "transaction_status":"valid",
    "validator_online":true,
    "watchers_count":3,
    "watchers_watching":3
}
```

- `critical`: empty string. It will be filled when:
    - all watchers of guard can't connect to nodes
    - validator is online and transaction is invalid
    - no new block after `NEW_BLOCK_TIMEOUT` seconds: blockchain is stuck or all listening nodes disconnected from blockchain
- `current_height` - current blockchain height (block)
- `transaction_status` - valid, invalid, unknown (when guard starts)
- `validator_online`
- `watchers_count` - count of watchers/nodes to listen
- `watchers_watching` - count of watchers, connected to nodes, and watching (listening) node