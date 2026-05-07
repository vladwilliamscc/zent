# Zent Public Integration RPC + CPU Mining Runbook

This runbook is for operators who want to run the public
`public/integration-mining-validation` branch as an RPC node and, optionally,
as a miner-chain CPU mining node.

It is intentionally public-safe.  It does not include private mining strategy,
private route decisions, revenue data, private benchmark logs, deployment
topology, or internal review archive material.

## 1. Baseline

Code baseline:

```text
repository: zent-public
branch:     public/integration-mining-validation
head:       4495f1a docs: update integration mining guide for PR-15
merge:      89293bb merge: integrate PR-15 SVP runtime isolation
upstream:   upstream/nexus @ c52f61b
app:        omgd 0.16.3-beta
```

Design-doc context:

```text
SDS:         v2.38
CPU Mining: v2.42
```

Important baseline gap:

- The SDS / CPU Mining documents describe the PR-1 implementation gate from a
  design-review point of view.
- This branch has already integrated the public mining stack through PR-15.
- PR-8 midstate prototype is intentionally not included.
- Use this branch's code and this runbook for deployment behavior; use SDS /
  CPU Mining as protocol background, not as an exact startup checklist for this
  branch.

Included public stack:

```text
PR-1   TryMinerNonce / PoW helper and golden fixtures
PR-2   h1 denominator schedule plumbing, dormant by default
PR-3   collateral cache foundation
PR-4   cache-backed collateral selector
PR-5   BlockHash / solver benchmark baseline
PR-6   BlockHash buffer reuse
PR-7   miner-header pre-serialize + nonce patch
PR-9   reward attribution scanner
PR-10  Route A/B metrics scanner
PR-11  miner worker count config
PR-12  h1 call-site binding parity
PR-13  collateral selector defensive cache / chain-context guard
PR-14  runtime metrics foundation + getminerruntimemetrics RPC
PR-15  SVP child-chain runtime data / listener / externalip isolation
```

## 2. Deployment Differences From SDS / CPU Mining

| Area | SDS / CPU Mining context | Current branch behavior |
| --- | --- | --- |
| PR state | Main docs discuss PR-1 readiness and later PR sequencing. | PR-1 through PR-15 are already integrated on this branch. |
| Miner workers | Older guidance may discuss `--concurrency` workarounds. | Use `--miner-workers` for miner-chain solver workers. If omitted, it defaults to `--concurrency - 1`, clamped to at least 1. |
| h1 denominator | Design describes start/stop schedule and activation drills. | Helper and tests exist, but chain params do not set activation heights, so the consensus-changing rule is dormant. |
| Runtime metrics | CPU docs describe runtime metrics as needed for Route A/B validation. | PR-14 exposes the seven counters through `getminerruntimemetrics`. PR-10 does not yet consume them automatically. |
| SVP data directories | Older guidance assumes one main `--datadir`/`--logdir` interpretation and does not describe child-chain isolation. | PR-15 isolates SVP child-chain runtime ownership. Main data is `<datadir>/mainnet`; child data is `<svpdatadir>/<svpid>` when `--svpdatadir` is set, or `<datadir>/<svpid>` when it is omitted. |
| SVP listeners / external IP | Older startup commands can accidentally leak global `--listen`, `--rpclisten`, or `--externalip` into child-chain config. | PR-15 clears inherited child listeners, RPC listeners, external IPs, and network-selection flags. Main CLI endpoints remain main-chain settings; child chains use their chain params. |
| Collateral selector | Docs describe the cache-backed selector plan. | Selector is cache-backed and defensive. A nil cache refuses to mine rather than falling back to legacy iteration. |
| RPC status checks | Older mining RPC names come from btcd. | `getmininginfo` and `getgenerate` reflect tx-chain CPUMiner, not miner-chain mining. Use `gethashespersec`, logs, and `getminerruntimemetrics` for `--generateminer`. |
| Networks | Some inherited help text still mentions regtest/simnet. | Current `omgd` network selection is mainnet by default or testnet via `--testnet`. Do not use regtest/simnet as the deployment target for this branch. |
| CPU miner meanings | Inherited btcd docs use CPU miner for tx-chain PoW. | `--generate` is tx-chain CPU mining. `--generateminer` is miner-chain CPU mining. This runbook is mainly about `--generateminer`. |

## 3. Build And Validate

This codebase is GOPATH-era.  Use the prepared dependency / overlay GOPATH if
it is present in your workspace.

From the repository root:

```bash
cd /Users/dreamxoo/dev/zent-proj/zent-public
git switch public/integration-mining-validation

export GO111MODULE=off
export GOPATH=/Users/dreamxoo/dev/zent-proj/zent-gopath-deps:/Users/dreamxoo/dev/zent-proj/zent-public-gopath-overlay:/Users/dreamxoo/dev/zent-proj/zent-public-gopath-pr1
export GOCACHE=/private/tmp/zent-go-cache

go test -vet=off -count=1 \
  ./omega/runtimemetrics \
  ./omega/minerchain \
  ./omega/consensus \
  ./omgd

go build -o /private/tmp/zent-omgd-public-integration ./omgd
```

Optional observer-tool tests:

```bash
go test -vet=off -count=1 \
  ./cmd/zent-route-ab-metrics/... \
  ./cmd/zent-reward-scanner/...
```

If those observer tests fail with `httptest: failed to listen on a port`, the
failure is a local loopback sandbox restriction. Re-run where `httptest` may
bind `127.0.0.1` / `::1`.

Do not use `go build ./...` as the first deployment gate. The wider historical
tree still has old GOPATH import and test-hygiene gaps unrelated to the mining
stack. Build `./omgd` explicitly.

## 4. Network And Port Choices

Mainnet defaults:

```text
p2p: 9788
rpc: 9789
```

Testnet defaults:

```text
p2p: 7788
rpc: 7789
flag: --testnet
```

RPC is disabled unless an RPC username/password or limited
username/password is configured.  By default, when RPC is enabled and no
`--rpclisten` is set, the node listens on localhost only.

RPC TLS is enabled by default and uses a self-signed certificate.  For local
curl calls, use `https://` plus `-k`, or explicitly use `--notls` while keeping
RPC bound to localhost.

Do not expose RPC directly to the public Internet, and do not rely on RPC
authentication as the only control.  Bind to `127.0.0.1` and use SSH tunnels, a
private network, or a locked-down reverse proxy if remote access is needed.

## 4.1 SVP Child-Chain Runtime Layout After PR-15

`omgd` starts the main chain first, then starts any SVP child chains discovered
from chainmap data.  Before PR-15, child-chain config re-parsed the process-wide
CLI and could inherit the main `--datadir`, `--listen`, `--rpclisten`, and
`--externalip` values.  That made it possible for a child chain to collide with
main-chain LevelDB paths or advertise/bind the wrong endpoint.

After PR-15:

```text
main data dir:  <datadir>/mainnet
child data dir: <svpdatadir>/<svpid>      when --svpdatadir is set
child data dir: <datadir>/<svpid>         when --svpdatadir is omitted
```

`<svpid>` is the lowercase hex child-chain network magic, for example
`4743546d`.

Recommended operator layout:

```bash
export ZENT_RUN=/Users/dreamxoo/zent-public-mainnet
export ZENT_SVP_DATA="$ZENT_RUN/svp-data"
mkdir -p "$ZENT_RUN/data" "$ZENT_RUN/logs" "$ZENT_SVP_DATA"
```

Then pass both:

```text
--datadir="$ZENT_RUN/data"
--svpdatadir="$ZENT_SVP_DATA"
```

This produces:

```text
$ZENT_RUN/data/mainnet
$ZENT_RUN/svp-data/<svpid>
```

If you omit `--svpdatadir`, the default is still safe after PR-15:

```text
$ZENT_RUN/data/mainnet
$ZENT_RUN/data/<svpid>
```

Use explicit `--svpdatadir` when you want SVP data to be visually and
operationally separate from the main-chain data base.

Do not put runtime-isolated keys in `[<svpid>]` sections of `omega.conf`.
PR-15 rejects these child-group keys:

```text
datadir
logdir
listen
rpclisten
externalip
testnet
regtest
simnet
svpdatadir
```

There is no public `--svplogdir` flag in PR-15.  Logging remains
process-global and controlled by `--logdir`.

PR-15 does not migrate any existing on-disk SVP data automatically.  If an
older local run wrote child-chain data into a legacy or accidental location,
stop `omgd` and either resync the child chain under the new layout or copy the
old data intentionally after verifying the source path.

## 5. Run An RPC-Only Node

### Step 1: Create directories

```bash
export ZENT_RUN=/Users/dreamxoo/zent-public-mainnet
export ZENT_SVP_DATA="$ZENT_RUN/svp-data"
mkdir -p "$ZENT_RUN/data" "$ZENT_RUN/logs" "$ZENT_SVP_DATA"
```

For testnet:

```bash
export ZENT_RUN=/Users/dreamxoo/zent-public-testnet
export ZENT_SVP_DATA="$ZENT_RUN/svp-data"
mkdir -p "$ZENT_RUN/data" "$ZENT_RUN/logs" "$ZENT_SVP_DATA"
```

### Step 2: Choose RPC credentials

```bash
export RPC_USER=zent_rpc_user
export RPC_PASS='change-this-long-random-password'
```

### Step 3: Start mainnet RPC node

```bash
/private/tmp/zent-omgd-public-integration \
  --datadir="$ZENT_RUN/data" \
  --svpdatadir="$ZENT_SVP_DATA" \
  --logdir="$ZENT_RUN/logs" \
  --rpcuser="$RPC_USER" \
  --rpcpass="$RPC_PASS" \
  --rpclisten=127.0.0.1:9789 \
  --listen=0.0.0.0:9788 \
  --debuglevel=info
```

### Step 4: Or start testnet RPC node

```bash
/private/tmp/zent-omgd-public-integration \
  --testnet \
  --datadir="$ZENT_RUN/data" \
  --svpdatadir="$ZENT_SVP_DATA" \
  --logdir="$ZENT_RUN/logs" \
  --rpcuser="$RPC_USER" \
  --rpcpass="$RPC_PASS" \
  --rpclisten=127.0.0.1:7789 \
  --listen=0.0.0.0:7788 \
  --debuglevel=info
```

### Step 5: Query RPC

Default TLS:

```bash
curl -sk \
  --user "$RPC_USER:$RPC_PASS" \
  -H 'Content-Type: application/json' \
  --data-binary '{"jsonrpc":"1.0","id":"getinfo","method":"getinfo","params":[]}' \
  https://127.0.0.1:9789/
```

Testnet port:

```bash
curl -sk \
  --user "$RPC_USER:$RPC_PASS" \
  -H 'Content-Type: application/json' \
  --data-binary '{"jsonrpc":"1.0","id":"getinfo","method":"getinfo","params":[]}' \
  https://127.0.0.1:7789/
```

If you start with `--notls`, use `http://127.0.0.1:<port>/` instead.

## 6. Run Miner-Chain CPU Mining

Miner-chain CPU mining is started by `--generateminer`.

Do not confuse it with:

```text
--generate       tx-chain CPU mining
--generateminer  miner-chain CPU mining
```

### Step 1: Prepare a miner identity

The current unlicensed public build has `common.Licensed = false`.

You need at least one mining identity:

- `--privkeys=<WIF>`: recommended for a miner node because the node can derive
  the mining address and has the key material needed for signing paths.
- `--miningaddr=<address>`: accepted as a miner-chain mining address, but by
  itself does not provide private key material.

Avoid passing WIF keys directly on a shared shell command line in production,
because process lists and shell history may leak them. Prefer a protected
config file (`chmod 600`) or your normal secret-injection mechanism. The command
examples below use environment variables for readability; adapt them to your
secret handling before production use.

### Step 2: Choose worker counts

For this branch:

```text
--miner-workers=N  miner-chain solver worker count
--concurrency=M    legacy / signature-verification concurrency input
```

If `--miner-workers` is unset, the miner-chain worker count is:

```text
max(1, --concurrency - 1)
```

For a dedicated CPU mining process, set `--miner-workers` explicitly. Keep
`--concurrency` reasonable for signature verification and legacy code paths.

Example:

```bash
export MINER_WORKERS=8
export SIG_CONCURRENCY=9
```

### Step 3: Start a mainnet miner-chain CPU miner

```bash
/private/tmp/zent-omgd-public-integration \
  --datadir="$ZENT_RUN/data" \
  --svpdatadir="$ZENT_SVP_DATA" \
  --logdir="$ZENT_RUN/logs" \
  --rpcuser="$RPC_USER" \
  --rpcpass="$RPC_PASS" \
  --rpclisten=127.0.0.1:9789 \
  --listen=0.0.0.0:9788 \
  --generateminer \
  --privkeys="$MINER_WIF" \
  --miner-workers="$MINER_WORKERS" \
  --concurrency="$SIG_CONCURRENCY" \
  --debuglevel=info
```

For testnet, add `--testnet` and use ports `7788` / `7789`.

### Step 4: Route A / external IP mode, optional

If this node should advertise a specific public connection identity, add one
`--externalip`:

```bash
--externalip=<public-ip>
# or, when the advertised P2P port is not the chain default:
--externalip=<public-ip>:9788
```

`--externalip` may be an IP/host with no port or `host:port`.  If the port is
omitted, the active main-chain default P2P port is used.  After PR-15 this flag
is intentionally not inherited by SVP child chains.

Keep one process to one mining identity. Do not put many mining addresses or
many external IPs into one `omgd` process to simulate multiple workers. The
miner-gap checks consider the configured mining addresses and external IPs
together, so multi-binding one process can reduce, not increase, effective
eligibility.

To scale horizontally, run separate `omgd` processes with separate:

```text
datadir
svpdatadir
logdir
RPC port
P2P listen port
mining identity
```

### Step 5: Understand the startup gates

The miner loop will not attempt useful work until these conditions are true:

```text
connected peer count >= 3
node is current after the miner-chain genesis phase
at least one mining address is configured
miner-gap check does not exclude this miner/address/connection
chain context can be read safely
collateral cache exists
collateral selector can find an eligible outpoint when the parent requires collateral
```

Common log lines:

```text
Start minging miner blocks.
miner.generateBlocks: sleep because of not enough connections
miner.generateBlocks: sleep on curHeight != 0 && !isCurrent
miner.generateBlocks won't mine because I am in GAP before the best block ...
miner.NewMinerBlockTemplate error: No collateral available
miner.generateBlocks: collateral cache unavailable; refusing to mine
miner Trying to solve block at <height> with difficulty <bits>
New miner block produced by <address> at <height>
Miner Block submitted via CPU miner accepted ...
```

## 7. Collateral Setup

This branch uses a cache-backed collateral selector.

Register candidate collateral outpoints through RPC:

```bash
curl -sk \
  --user "$RPC_USER:$RPC_PASS" \
  -H 'Content-Type: application/json' \
  --data-binary '{"jsonrpc":"1.0","id":"addcollateral","method":"addcollateral","params":["<txid>", <vout>]}' \
  https://127.0.0.1:9789/
```

Remove an outpoint:

```bash
curl -sk \
  --user "$RPC_USER:$RPC_PASS" \
  -H 'Content-Type: application/json' \
  --data-binary '{"jsonrpc":"1.0","id":"dropcollateral","method":"dropcollateral","params":["<txid>", <vout>]}' \
  https://127.0.0.1:9789/
```

List configured miner addresses:

```bash
curl -sk \
  --user "$RPC_USER:$RPC_PASS" \
  -H 'Content-Type: application/json' \
  --data-binary '{"jsonrpc":"1.0","id":"listminingaddr","method":"listminingaddr","params":[]}' \
  https://127.0.0.1:9789/
```

Collateral selection rules to remember:

- The outpoint must belong to the mining address being used.
- The outpoint amount must be at least the current required collateral amount.
- Recently used outpoints are excluded for the chain-parameter reuse window.
- Spent, stale, wrong-token, wrong-owner, missing, or too-small entries are not
  eligible.
- If the parent miner block requires collateral and no eligible outpoint is
  available, template generation fails with `No collateral available`.

Do not rely on nil-collateral mining for production. Keep an eligible collateral
pool ready before enabling `--generateminer`.

## 8. Runtime Observability

### Miner-chain hash rate

Use `gethashespersec` for miner-chain CPU mining:

```bash
curl -sk \
  --user "$RPC_USER:$RPC_PASS" \
  -H 'Content-Type: application/json' \
  --data-binary '{"jsonrpc":"1.0","id":"hps","method":"gethashespersec","params":[]}' \
  https://127.0.0.1:9789/
```

Do not use `getmininginfo.hashespersec`; it is currently hard-coded to `0`.
Do not use `getgenerate` to decide whether `--generateminer` is running;
`getgenerate` reflects tx-chain CPUMiner.

### PR-14 runtime metrics

The new RPC endpoint is:

```bash
curl -sk \
  --user "$RPC_USER:$RPC_PASS" \
  -H 'Content-Type: application/json' \
  --data-binary '{"jsonrpc":"1.0","id":"metrics","method":"getminerruntimemetrics","params":[]}' \
  https://127.0.0.1:9789/
```

Expected counters:

```text
ip_minergap_reject_total
addr_minergap_reject_total
template_build_success_total
template_build_failure_total
miner_nonce_trials_total
committee_dial_failure_total
committee_participation_success_total
```

Use these counters together with logs. The PR-10 Route A/B scanner still keeps
its old "unavailable runtime metrics" list in this branch, so treat
`getminerruntimemetrics` as a manual / external input until a follow-up scanner
PR consumes it.

### Basic health RPCs

```bash
getinfo
getblockchaininfo
getbestblockhash
getblockcount
getconnectioncount
getpeerinfo
gethashespersec
getminerruntimemetrics
listminingaddr
```

Use the same curl pattern shown above, changing only `method` and `params`.

## 9. Troubleshooting

### RPC is not listening

Check that you set RPC credentials:

```text
--rpcuser
--rpcpass
```

Without credentials, this branch disables the RPC server by default.

### curl cannot connect with plain HTTP

RPC TLS is on by default. Use:

```text
https://127.0.0.1:<rpc-port>/
curl -k
```

or start with `--notls` while keeping RPC on localhost.

### `getmininginfo` says mining is false

That only means tx-chain CPUMiner is not mining. It does not prove
miner-chain `--generateminer` is stopped. Check:

```text
gethashespersec
getminerruntimemetrics
logs containing "miner Trying to solve block"
```

### Hash rate stays zero

Check these first:

```text
peer count >= 3
node is current
--generateminer is set
mining identity is configured
miner is not inside MinerGap
collateral outpoints are registered and eligible
logs do not show chain_context_unavailable or cache_unavailable
```

### Template build failures keep increasing

Inspect logs for:

```text
No collateral available
collateral cache unavailable
chain context unavailable
sleep on curHeight != 0 && !isCurrent
```

Then verify collateral and sync state.

### SVP data appears in the wrong directory

After PR-15, the expected layout is:

```text
<datadir>/mainnet
<svpdatadir>/<svpid>
```

or, when `--svpdatadir` is omitted:

```text
<datadir>/mainnet
<datadir>/<svpid>
```

If the node exits while reading `omega.conf`, check that no `[<svpid>]` section
sets `datadir`, `logdir`, `listen`, `rpclisten`, `externalip`, `testnet`,
`regtest`, `simnet`, or `svpdatadir`.

### Testnet works differently from regtest/simnet instructions

Use only:

```text
mainnet default
--testnet
```

Regtest / simnet are inherited in old help text and params blocks, but they are
not the deployment target for this public integration branch.

## 10. Minimal Commands Summary

Build:

```bash
export GO111MODULE=off
export GOPATH=/Users/dreamxoo/dev/zent-proj/zent-gopath-deps:/Users/dreamxoo/dev/zent-proj/zent-public-gopath-overlay:/Users/dreamxoo/dev/zent-proj/zent-public-gopath-pr1
export GOCACHE=/private/tmp/zent-go-cache
go build -o /private/tmp/zent-omgd-public-integration ./omgd
```

Run RPC-only mainnet:

```bash
export ZENT_RUN=/Users/dreamxoo/zent-public-mainnet
export ZENT_SVP_DATA="$ZENT_RUN/svp-data"
mkdir -p "$ZENT_RUN/data" "$ZENT_RUN/logs" "$ZENT_SVP_DATA"

/private/tmp/zent-omgd-public-integration \
  --datadir="$ZENT_RUN/data" \
  --svpdatadir="$ZENT_SVP_DATA" \
  --logdir="$ZENT_RUN/logs" \
  --rpcuser="$RPC_USER" \
  --rpcpass="$RPC_PASS" \
  --rpclisten=127.0.0.1:9789 \
  --listen=0.0.0.0:9788
```

Run miner-chain CPU miner:

```bash
/private/tmp/zent-omgd-public-integration \
  --datadir="$ZENT_RUN/data" \
  --svpdatadir="$ZENT_SVP_DATA" \
  --logdir="$ZENT_RUN/logs" \
  --rpcuser="$RPC_USER" \
  --rpcpass="$RPC_PASS" \
  --rpclisten=127.0.0.1:9789 \
  --listen=0.0.0.0:9788 \
  --generateminer \
  --privkeys="$MINER_WIF" \
  --miner-workers="$MINER_WORKERS" \
  --concurrency="$SIG_CONCURRENCY"
```

Check miner-chain mining:

```bash
curl -sk --user "$RPC_USER:$RPC_PASS" \
  -H 'Content-Type: application/json' \
  --data-binary '{"jsonrpc":"1.0","id":"hps","method":"gethashespersec","params":[]}' \
  https://127.0.0.1:9789/

curl -sk --user "$RPC_USER:$RPC_PASS" \
  -H 'Content-Type: application/json' \
  --data-binary '{"jsonrpc":"1.0","id":"metrics","method":"getminerruntimemetrics","params":[]}' \
  https://127.0.0.1:9789/
```
