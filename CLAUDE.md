# CLAUDE.md

This repository is `zent-public`, the public-safe Zent / NXUS node integration
workspace. It carries reviewed public PR branches over the upstream `nexus`
line.

Private context may be read from sibling repositories, but public commits must
remain public-safe.

## Project Layout

Expected local layout:

```text
~/dev/zent-proj/
  zent-public/       # this repository
  zent-private/      # private architecture, review, and validation docs
  zentwallet/        # clean wallet implementation
```

Use `zent-public` for public code, public tests, public PR branches, and
public-safe documentation.

Use `zent-private` for private design history, internal reviews, deployment
notes, mining strategy, yield analysis, and site-specific operational records.

You may read any documents under `zent-private/docs` for context. Do not copy
private document text, paths, hostnames, deployment topology, mining economics,
private addresses, secrets, or internal strategy into `zent-public`. Public
documentation must be rewritten as a concise public-safe summary.

## Read First

Before starting work, read:

- `README.md`
- `Process.md`
- `docs/INDEX.md`
- `docs/WORKFLOW.md`
- `docs/review-gated-development-workflow-r1.md`

`docs/WORKFLOW.md` points to the generic Review-Gated Development Workflow R2
under the sibling `review-gated-agent-workflow` repository. Use generic R2 plus
the local `zent-public` overrides for new PRs.

For PR-16, also read:

- `docs/public-pr16-nosvp-mining-only-planning-r1.md`

Private PR-16 design and review context may be read from the sibling private
workspace at `../zent-private/docs/internal-review/`, but public work must not
cite the private archive directly.

## Review-Gated Workflow

All future public PRs must follow:

1. preflight architecture;
2. architecture review;
3. preflight revision until P1/P2 findings are resolved;
4. implementation prompt / plan;
5. implementation prompt review;
6. implementation;
7. implementation review;
8. merge and post-merge validation.

Do not implement from a planning note alone. Implementation starts only from an
accepted implementation prompt.

Keep PRs atomic and branch-independent where practical. Do not create
unnecessary dependency chains.

## Current Baseline

- Active integration branch: `public/integration-mining-validation`
- Current stack: PR-1 through PR-15 integrated
- PR-8: intentionally not integrated
- PR-16: planned, not implemented

Recommended PR-16 branch:

```text
public/pr16-nosvp-mining-only
```

## Public Safety Rules

Do not add to public commits:

- private mining strategy;
- route decisions;
- revenue or yield data;
- private benchmark logs;
- deployment topology;
- hostnames;
- secrets or credentials;
- private addresses;
- internal review archive text;
- site-specific operational assumptions.

It is acceptable to use private docs as background and then produce a
public-safe design, PR description, test plan, or operator note.

## Engineering Guidance

1. Current source code is the fact authority.
2. Use `rg` / `rg --files` first for search.
3. Keep changes small and PR-scoped.
4. Preserve the GOPATH-era workflow unless modernization is explicitly scoped.
5. Do not introduce Go modules as incidental work.
6. Do not touch generated `dist/` output unless a reviewed packaging phase says
   so.
7. Do not broaden consensus, miner-chain, tx-chain, RPC, or runtime behavior
   beyond the accepted prompt.
8. Do not pass `-vet=off` to `go build`; it is only a `go test` flag.

## PR-16 Scope Reminder

PR-16 should add an opt-in `--nosvp` mining-only startup mode and any narrow
lifecycle fix needed for deterministic single-protocol startup.

It should not change consensus, validation, mining semantics, collateral
selection, reward attribution, worker scheduling, chain parameters, database
drivers, or PR-15 default full-node SVP behavior.

## Validation

Use the targeted GOPATH-era validation from `Process.md` and the public runbook.
Typical commands:

```bash
go test -vet=off -count=1 \
  ./omega/runtimemetrics \
  ./omega/minerchain \
  ./omega/consensus \
  ./omgd

go build -o /private/tmp/zent-omgd-public-integration ./omgd
```

If live-node or smoke validation is not run, say so and explain what was run
instead.

## Git Guidance

- Check `git status --short` before and after edits.
- Do not commit local generated artifacts, logs, `.DS_Store`, or `dist/`.
- Keep commits narrow.
- Push only when requested.

## Review Style

For reviews, prioritize correctness, data-integrity risk, scope creep,
public/private boundary leaks, and missing validation. Lead with findings by
severity. If no issues remain, say so clearly.
