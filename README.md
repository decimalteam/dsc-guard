# Decimal Smart Chain Guard

Decimal Smart Chain Guard is a tool helping to monitor validator node and set it offline if node stops to sign blocks by any reason.

# Guard states

1. Start

2. Connect to multiple nodes: start partial watcher for every node

3. Read and validate set_offline transaction (watcher periodicaly check it)
- if tx is invalid, go to step 6

4. Watch for blocks and count missed blocks
- watchers can infinity reconnect and validate transaction (steps 2+3)

5. If validator is online and missed some count of blocks, then triggered sends set_offline, wait for confirm of transaction

6. Start watch without tx data
- sends warning
- if validator become online, sends very loud warnings

8. Go to step 2 after get new set_offline tx data 