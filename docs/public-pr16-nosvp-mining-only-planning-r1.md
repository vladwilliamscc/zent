# Public PR-16 Planning: `--nosvp` Mining-Only Startup Mode

Date: 2026-05-06

Status: planned, not implemented.

This document is a public-safe planning summary for PR-16. It intentionally
omits non-public review archives, deployment topology, mining strategy, revenue
assumptions, secrets, and site-specific operational logs.

## Current Baseline

The active public integration branch currently includes PR-1 through PR-15.
PR-15 isolates SVP child-chain runtime ownership so child protocols no longer
inherit main-chain data directories, listeners, RPC listeners, external IPs, or
network-selection flags.

PR-15 remains the baseline for full-node behavior.

## Proposed PR-16 Scope

PR-16 should add an opt-in mining-only startup mode:

```text
omgd --nosvp ...
```

When `--nosvp` is set, `omgd` should start only the main protocol:

- main tx-chain database;
- main miner-chain database;
- main P2P/RPC services;
- main tx-chain and miner-chain mining paths, as configured.

It should not create, open, listen for, sync, or monitor SVP child-chain
protocols.

This is a runtime-startup switch only. Chainmap metadata may still be loaded and
reported by existing RPCs because main-chain code can depend on that metadata.
`--nosvp` means "do not run child protocols", not "remove child descriptors from
chain metadata".

## Recommended Branch

Use an independent branch:

```text
public/pr16-nosvp-mining-only
```

Base it on the reviewed public baseline selected during PR-16 preflight. Do not
chain PR-16 on unrelated future work unless the preflight explicitly says so.

## Public-Safe Implementation Boundary

Expected production files:

- `omgd/config.go`
- `omgd/omgd.go`

Expected tests:

- extend existing PR-15 SVP runtime config tests where that is clean;
- otherwise add a narrow `omgd/nosvp_startup_test.go`.

Avoid broader source movement. In particular, PR-16 should not change:

- consensus rules;
- tx-chain validation;
- miner-chain validation;
- PoW solving or acceptance semantics;
- collateral selection;
- reward attribution;
- mining worker scheduling;
- chain parameter constants;
- database driver behavior;
- PR-15 full-node SVP runtime isolation when `--nosvp` is not set.

PR-16 should not add child-port parameterization, `--svplisten`,
`--svprpclisten`, per-SVP external IP flags, automatic data migration,
site-specific deployment scripts, mining strategy logic, or any revenue-oriented
behavior.

## Design Notes

The public flag should be:

```go
NoSVP bool `long:"nosvp" description:"Disable automatic SVP child-chain startup"`
```

The implementation should skip only automatic SVP child protocol creation after
the main protocol is constructed. It should not skip main config loading,
chainmap loading, main database opening, main RPC/P2P startup, or configured
main-chain mining.

`--svpdatadir` should be inert when `--nosvp` is set. Treating that combination
as an error would turn PR-16 into a broader config-precedence change.

PR-16 should also make process wait-group accounting deterministic in the
single-protocol case. The safe shape is:

```text
wg.Add(1) before starting each protocol goroutine
runserver does not call wg.Add
cleanup remains responsible for wg.Done
```

This matters because `--nosvp` can leave only the main protocol running.

## Test Floor

PR-16 should be implemented only after the review-gated workflow produces an
accepted implementation prompt. At minimum, that prompt should require:

- config parsing verifies `--nosvp` sets the config field;
- default behavior still starts SVP child planning as before;
- `--nosvp` returns an empty SVP child startup plan;
- main tx-chain and miner-chain data paths remain main-chain paths;
- no child protocol data directory is created in no-SVP mode;
- child listener/RPC port conflicts do not prevent no-SVP startup;
- full-node behavior without `--nosvp` remains PR-15-compatible;
- wait-group accounting is deterministic when only one protocol is launched;
- validation covers `go test -vet=off -count=1 omgd`, `go build omgd`, and
  `git diff --check`;
- scope proof confirms no consensus, miner-chain, or collateral-selection files
  changed.

If smoke testing is used, use temporary data and log roots, bounded peer
configuration, explicit RPC credentials, and explicit local listen ports.

## Review Requirements

PR-16 must follow `docs/review-gated-development-workflow-r1.md`:

1. preflight architecture;
2. architecture review;
3. implementation prompt / plan;
4. implementation prompt review;
5. implementation;
6. implementation review;
7. merge and post-merge validation.

Do not implement PR-16 directly from this planning note. This file is a
public-safe handoff summary, not the binding implementation prompt.

## Relationship To PR-15

PR-15 made full-node SVP runtime safer. PR-16 should not weaken that behavior.
Instead, PR-16 should add an explicit operator choice for nodes that only need
the main protocol and mining paths.

In short:

- default mode: full PR-15 SVP runtime isolation remains active;
- `--nosvp` mode: main protocol only, no child protocol startup.
