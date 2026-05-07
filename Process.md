# zent-public Process And Progress

Date: 2026-05-06

This document records the current public integration state, PR branch map, and
future workflow expectations for `zent-public`. It is a progress index and
handoff aid. Detailed deployment behavior remains in
`docs/public-integration-rpc-cpu-mining-guide.md`.

## Current Baseline

- Active integration branch: `public/integration-mining-validation`
- Remote tracking branch: `origin/public/integration-mining-validation`
- Current head at time of writing: `4495f1a docs: update integration mining guide for PR-15`
- Current integration merge baseline: `89293bb merge: integrate PR-15 SVP runtime isolation`
- Upstream base branch: `upstream/nexus`
- Upstream base commit recorded in the integration runbook: `c52f61b`
- Current public stack: PR-1 through PR-15 integrated on the integration branch
- PR-8 midstate prototype: intentionally not integrated in the current public stack

Local note:

- The current working tree may contain local generated `dist/` artifacts. They
  are not part of the source process and should not be committed unless a
  release packaging phase explicitly says so.

## Project Purpose

`zent-public` is the public source tree for the Zent / NXUS node stack used by
the wallet and mining validation work. The active integration branch is focused
on public-safe mining and runtime improvements over the upstream `nexus`
baseline.

Public documentation must not include private mining strategy, private route
decisions, revenue data, private benchmark logs, deployment topology, secrets,
or internal review archive material.

## Documentation Entry Points

- `README.md` - repository-level index.
- `Process.md` - this progress and branch-state document.
- `docs/INDEX.md` - docs directory index.
- `docs/review-gated-development-workflow-r1.md` - required future development
  workflow.
- `docs/public-integration-rpc-cpu-mining-guide.md` - public-safe operator
  runbook for the current integration branch.

## Branch State

| Branch / branch family | Current state | Notes |
| --- | --- | --- |
| `public/integration-mining-validation` | Active public integration branch | Contains PR-1 through PR-15 integration plus public runbook updates. |
| `origin/public/integration-mining-validation` | Remote tracking branch | Current pushed integration baseline. |
| `nexus` / `origin/nexus` / `upstream/nexus` | Upstream baseline family | Do not develop public integration features directly here. |
| `public/pr1-pow-helper-*` | Historical / integrated | PR-1 PoW helper work. |
| `public/pr2-h1-denominator` | Historical / integrated | PR-2 h1 denominator schedule plumbing. |
| `public/pr3-collateral-cache-foundation` | Historical / integrated | PR-3 collateral cache foundation. |
| `public/pr4-collateral-selector-v0-cache` | Historical / integrated | PR-4 cache-backed collateral selector. |
| `public/pr5-blockhash-benchmark*` | Historical / integrated | PR-5 benchmark baseline. |
| `public/pr6-blockhash-buffer-reuse` | Historical / integrated | PR-6 BlockHash allocation reduction. |
| `public/pr7-miner-header-preserialize-nonce-patch` | Historical / integrated | PR-7 miner-header pre-serialization. |
| `public/pr9-reward-attribution-scanner` | Historical / integrated | PR-9 observer scanner. |
| `public/pr10-route-ab-metrics` | Historical / integrated | PR-10 Route A/B metrics scanner. |
| `public/pr11-miner-workers-decouple` | Historical / integrated | PR-11 miner worker-count configuration. |
| `public/pr12-h1-call-site-parity` | Historical / integrated | PR-12 h1 call-site parity bindings. |
| `public/pr13-selector-defensive-cache-guard` | Historical / integrated | PR-13 selector guard. |
| `public/pr14-runtime-metrics-foundation` | Historical / integrated | PR-14 runtime metrics foundation. |
| `public/pr15-svp-runtime-isolation` | Historical / integrated | PR-15 SVP runtime isolation. |

## Integrated PR Map

### PR-1: TryMinerNonce / PoW Helper

Status: integrated.

Branch family:

- `public/pr1-pow-helper-pr`
- `public/pr1-pow-helper-work`

Purpose:

- Introduce the helper surface for miner nonce attempts.
- Establish golden fixtures for deterministic PoW helper behavior.

### PR-2: h1 Denominator Schedule Plumbing

Status: integrated, dormant by default.

Branch:

- `public/pr2-h1-denominator`

Purpose:

- Add h1 denominator schedule plumbing.
- Keep consensus-changing behavior dormant unless activation heights are set.

### PR-3: Collateral Cache Foundation

Status: integrated.

Branch:

- `public/pr3-collateral-cache-foundation`

Purpose:

- Add the cache foundation needed by later collateral selection work.

### PR-4: Cache-Backed Collateral Selector

Status: integrated.

Branch:

- `public/pr4-collateral-selector-v0-cache`

Purpose:

- Select collateral from cache instead of legacy iteration paths.

### PR-5: BlockHash / Solver Benchmark Baseline

Status: integrated.

Branch family:

- `public/pr5-blockhash-benchmark`
- `public/pr5-blockhash-benchmark-clean`

Purpose:

- Establish baseline benchmarks for block hashing and solver behavior.

### PR-6: BlockHash Buffer Reuse

Status: integrated.

Branch:

- `public/pr6-blockhash-buffer-reuse`

Purpose:

- Reduce BlockHash buffer allocations.

### PR-7: Miner Header Pre-Serialization + Nonce Patch

Status: integrated.

Branch:

- `public/pr7-miner-header-preserialize-nonce-patch`

Purpose:

- Pre-serialize miner header data and patch nonce fields efficiently.

### PR-8: Midstate Prototype

Status: intentionally not integrated.

Purpose:

- Midstate / deeper hashing prototype work was kept out of the current public
  integration branch. Reconsider only through a new preflight and review gate.

### PR-9: Reward Attribution Scanner

Status: integrated.

Branch:

- `public/pr9-reward-attribution-scanner`

Purpose:

- Add an observer tool for reward attribution analysis.

### PR-10: Route A/B Metrics Scanner

Status: integrated.

Branch:

- `public/pr10-route-ab-metrics`

Purpose:

- Add an observer tool for Route A/B metrics collection.

### PR-11: Miner Worker Count Configuration

Status: integrated.

Branch:

- `public/pr11-miner-workers-decouple`

Purpose:

- Add miner-chain worker-count configuration.
- Decouple miner workers from broader concurrency defaults.

### PR-12: h1 Call-Site Parity

Status: integrated.

Branch:

- `public/pr12-h1-call-site-parity`

Purpose:

- Bind h1 behavior at call sites and add parity tests.

### PR-13: Collateral Selector Defensive Guard

Status: integrated.

Branch:

- `public/pr13-selector-defensive-cache-guard`

Purpose:

- Guard collateral selector chain-context usage.
- Refuse unsafe nil-cache behavior rather than falling back silently.

### PR-14: Runtime Metrics Foundation

Status: integrated.

Branch:

- `public/pr14-runtime-metrics-foundation`

Purpose:

- Add miner runtime metrics foundation.
- Add public RPC exposure for runtime metrics.

### PR-15: SVP Runtime Isolation

Status: integrated.

Branch:

- `public/pr15-svp-runtime-isolation`

Purpose:

- Isolate SVP child-chain runtime data ownership.
- Prevent child-chain inheritance of main-chain listeners, RPC listeners,
  external IPs, and network-selection flags.

## Current Validation Posture

This repository is GOPATH-era. The current public integration runbook uses:

```text
GO111MODULE=off
GOPATH=/Users/dreamxoo/dev/zent-proj/zent-gopath-deps:/Users/dreamxoo/dev/zent-proj/zent-public-gopath-overlay:/Users/dreamxoo/dev/zent-proj/zent-public-gopath-pr1
GOCACHE=/private/tmp/zent-go-cache
```

Primary targeted validation:

```bash
go test -vet=off -count=1 \
  ./omega/runtimemetrics \
  ./omega/minerchain \
  ./omega/consensus \
  ./omgd

go build -o /private/tmp/zent-omgd-public-integration ./omgd
```

Optional observer-tool validation:

```bash
go test -vet=off -count=1 \
  ./cmd/zent-route-ab-metrics/... \
  ./cmd/zent-reward-scanner/...
```

Do not use `go build ./...` as the default gate for public integration work.
The wider inherited tree still contains historical GOPATH and test-hygiene
issues unrelated to the reviewed mining integration stack.

## Future Work

Future public work should use the workflow in
`docs/review-gated-development-workflow-r1.md`.

Known candidates for future preflight:

- PR-16 or later public mining hardening;
- runtime metrics consumption by Route A/B tooling;
- any renewed midstate / hashing optimization effort;
- deeper SVP runtime validation;
- release packaging discipline for generated artifacts;
- long-running deployment validation.

Any consensus-affecting change, runtime ownership change, mining selection
change, RPC surface addition, or public deployment guidance change must go
through preflight, implementation prompt, implementation, and implementation
review before merge.

## Maintenance Rules

Update this file when:

- a public PR branch is merged into `public/integration-mining-validation`;
- a PR is abandoned or superseded;
- a new public runbook changes the validation baseline;
- the active integration branch changes;
- generated artifacts become intentionally tracked release assets.
