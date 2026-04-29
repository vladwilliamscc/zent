# Public PR Guide

This document is the public-safe implementation guide for work done in this repository.

The public repository is for upstreamable bug fixes, tests, benchmarks, and general-purpose code improvements. Private mining strategy, private pool operations, private deployment details, private benchmark data, and internal review archives belong in the private repository only.

## Repository Flow

Expected flow:

```text
nexuszent/zent
      -> vladwilliamscc/zent          public fork, public PR baseline
      -> vladwilliamscc/zent-private  private planning / mining / pool work
```

In this repo:

```text
origin   = vladwilliamscc/zent
upstream = nexuszent/zent, fetch-only
```

Use `origin` for public branch pushes. Do not push to `upstream`.

## Public-Safe PR Classes

Good candidates for public PRs:

- deterministic tests and replay helpers;
- consensus bug fixes with clear activation / validation plans;
- benchmark baselines;
- non-sensitive observability primitives;
- configuration fixes that do not reveal private strategy;
- correctness fixes with minimal scope.

Not public by default:

- private CPU mining optimization strategy;
- private pool routing / operations;
- revenue attribution data from private deployments;
- IP / machine / deployment topology;
- private benchmark logs;
- private review archive or internal planning documents.

## Current Public PR Sequence

Recommended initial sequence:

1. `PR-pow-replay-helper` (PR-1): deterministic PoW helper and tests, no behavior change.
2. `PR-blockhash-benchmark-baseline` (PR-5): benchmark-only baseline.
3. `PR-h1-denominator` (PR-2): consensus bug fix, after PR-1 and PR-5.

PR-1 and PR-5 can be prepared independently. PR-2 must wait until PR-1 and PR-5 are merged.

## Branch Naming

Use explicit public branch names:

```text
public/pr1-pow-helper
public/pr5-blockhash-benchmark
public/pr2-h1-denominator
```

Keep private branches out of this repository.

## General PR Discipline

Every public PR must include:

1. Objective.
2. Non-objectives.
3. Write scope.
4. Behavior change statement.
5. Validation commands and output summary.
6. Rollback impact.
7. Diff scope proof.

Useful local checks:

```bash
git diff --stat
git diff --name-only
git status --short
```

If a PR touches files outside its declared scope, stop and split the work.

## Testing Notes

This repository may use legacy GOPATH-era import paths. If a plain `go test` command fails because the environment is not configured, do not silently skip validation. Record:

- the command attempted;
- the failure mode;
- the correct project-local setup command if known;
- any package-level tests that did run successfully.

For PR-1, prefer the focused package test once helper files exist:

```bash
go test ./omega/minerchain
```

For benchmark PRs, include `-benchmem` and report both micro-benchmark and solver-loop numbers when applicable.
