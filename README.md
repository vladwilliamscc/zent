# zent-public

Public Zent / NXUS node integration workspace.

This repository is based on the upstream `nexus` line and carries the public
integration branch used for RPC and CPU-mining validation. It is not the wallet
repository; wallet work lives in the sibling `zentwallet` repository.

## Current Public Baseline

- Active branch: `public/integration-mining-validation`
- Current public stack: PR-1 through PR-15 integrated
- Next planned public PR: PR-16 `--nosvp` mining-only startup mode, currently
  in review-gated design. Public-safe planning summary:
  `docs/public-pr16-nosvp-mining-only-planning-r1.md`
- PR-8 midstate prototype: intentionally not integrated
- Main operator runbook: `docs/public-integration-rpc-cpu-mining-guide.md`

## Documentation

Start with:

- `Process.md` - current PR map, branch state, validation posture, and future
  work.
- `docs/INDEX.md` - documentation index.
- `docs/review-gated-development-workflow-r1.md` - required workflow for future
  public PRs.
- `docs/public-integration-rpc-cpu-mining-guide.md` - public-safe runbook for
  building and running the current integration branch.

## Development Workflow

Future public integration work must follow the review-gated workflow:

1. preflight architecture;
2. architecture review;
3. implementation prompt / plan;
4. implementation prompt review;
5. implementation;
6. implementation review;
7. merge and post-merge validation.

Keep PR branches small and as independent as practical. Do not create
unnecessary branch dependency chains.

## Public Documentation Safety

Do not put private mining strategy, private route decisions, revenue data,
private benchmark logs, deployment topology, secrets, or internal review archive
material in this public repository.

Generated binaries and `dist/` outputs are not source artifacts unless a
reviewed release-packaging phase explicitly says otherwise.
