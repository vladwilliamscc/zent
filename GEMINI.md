# GEMINI.md

This repository is `zent-public`, the public-safe Zent / NXUS node integration
workspace. It is based on the upstream `nexus` line and carries reviewed public
PR branches for mining, runtime, RPC, and node-behavior improvements.

Treat public code and public documentation as release-facing material. Private
planning context may be read, but it must not leak into public commits.

## Repository Roles

Expected local layout:

```text
~/dev/zent-proj/
  zent-public/       # this public-safe source repository
  zent-private/      # private architecture docs, reviews, validation records
  zentwallet/        # clean wallet implementation
```

Use `zent-public` for:

- public-safe code changes;
- public PR branches;
- public tests and validation;
- public-safe runbooks and progress documentation.

Use `zent-private` for:

- private architecture notes;
- expert review archives;
- deployment and validation notes;
- mining strategy, yield analysis, and site-specific operational material.

Agents may freely read `zent-private/docs` and
`zent-private/docs/internal-review` to understand design history, review
decisions, and validation context. However, public branches must not cite,
copy, or paraphrase private-only material in a way that exposes private review
archives, hostnames, deployment topology, mining economics, private addresses,
secrets, or internal operational strategy. Public docs should be rewritten as
clean public-safe summaries.

## Start Here

Before design, implementation, or review work, read:

```text
README.md
Process.md
docs/INDEX.md
docs/WORKFLOW.md
docs/review-gated-development-workflow-r1.md
```

`docs/WORKFLOW.md` points to the generic Review-Gated Development Workflow R2
under the sibling `review-gated-agent-workflow` repository. For new public PRs,
use generic R2 plus the local overrides in `docs/WORKFLOW.md`. The older
`docs/review-gated-development-workflow-r1.md` remains a project-local snapshot
and historical reference.

For PR-16 planning, also read:

```text
docs/public-pr16-nosvp-mining-only-planning-r1.md
```

Private PR-16 design material may be read from the sibling private workspace:

```text
../zent-private/docs/internal-review/
```

but it must remain private unless a public-safe extraction is explicitly
written.

## Required Workflow

Future public PR work must follow the review-gated workflow in
`docs/WORKFLOW.md` and `docs/review-gated-development-workflow-r1.md`:

1. preflight architecture;
2. architecture review;
3. preflight revision until P1/P2 findings are resolved;
4. implementation prompt / plan;
5. implementation prompt review;
6. implementation;
7. implementation review;
8. merge and post-merge validation.

Do not skip directly from a planning note to code. A code agent should implement
only from an accepted implementation prompt.

Keep PR branches small and as independent as practical. Avoid unnecessary
dependency chains between PRs.

## Current Public Baseline

The active integration branch is:

```text
public/integration-mining-validation
```

The current public stack includes PR-1 through PR-15. PR-8 is intentionally not
integrated. PR-16 is planned but not implemented.

PR-16 public-safe planning summary:

```text
docs/public-pr16-nosvp-mining-only-planning-r1.md
```

Recommended PR-16 branch:

```text
public/pr16-nosvp-mining-only
```

## Public / Private Boundary

Public commits must not include:

- private mining strategy;
- private route decisions;
- revenue or yield data;
- private benchmark logs;
- deployment topology;
- hostnames;
- private addresses;
- secrets, credentials, or key material;
- internal review archive text;
- private operational assumptions that are not public-safe.

It is acceptable to use private docs to understand why a public change is
needed, then write a concise public-safe problem statement, test plan, and PR
description.

## Engineering Rules

- Verify current source code facts before relying on design prose.
- Use `rg` / `rg --files` for search.
- Keep changes atomic and PR-scoped.
- Do not modernize the GOPATH-era build system unless explicitly scoped.
- Do not introduce Go modules as incidental feature work.
- Do not broaden mining, consensus, RPC, or runtime behavior beyond the
  accepted prompt.
- Do not modify generated `dist/` artifacts unless a reviewed release-packaging
  phase explicitly says to do so.
- Do not use `go build -vet=off`; `-vet=off` is a `go test` flag only.

## PR-16 Guardrails

For PR-16, the intended scope is an opt-in `--nosvp` startup mode plus any
small lifecycle fix needed to make single-protocol startup deterministic.

PR-16 must not change:

- consensus rules;
- tx-chain validation;
- miner-chain validation;
- PoW solving or acceptance semantics;
- collateral selection;
- reward attribution;
- mining worker scheduling;
- chain parameters;
- database driver behavior;
- PR-15 full-node SVP runtime behavior when `--nosvp` is not set.

PR-16 must not add child-port parameterization, private deployment scripts,
mining strategy logic, or revenue-oriented behavior.

## Validation

Use the GOPATH-era harness documented in `Process.md` and the public integration
runbook. Typical targeted validation:

```bash
go test -vet=off -count=1 \
  ./omega/runtimemetrics \
  ./omega/minerchain \
  ./omega/consensus \
  ./omgd

go build -o /private/tmp/zent-omgd-public-integration ./omgd
```

Do not use full-tree `go build ./...` as the default gate unless the accepted
prompt explicitly requires it; the inherited tree has historical unrelated
issues.

## Git Hygiene

- Run `git status --short` before and after work.
- Do not commit `.DS_Store`, editor files, local logs, generated binaries, or
  `dist/` output.
- Keep commits narrow and phase-scoped.
- Push only the branch requested by the user.

## Reporting

For implementation work, report:

- files changed;
- tests and validation commands run;
- skipped validations and why;
- scope proof;
- go.mod / dependency status;
- any public/private boundary concern.

For reviews, lead with findings by severity. If no issues remain, say so
clearly and name residual test or deployment risks.
