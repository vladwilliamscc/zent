# zent-public Documentation Index

Date: 2026-05-06

This index points future agents and reviewers to the public-safe documents that
define the current integration branch and development workflow.

## Start Here

- `../README.md` - repository-level entry point.
- `../AGENTS.md` / `../CLAUDE.md` - agent instructions for future Codex /
  Claude sessions, including the public/private boundary and review-gated
  workflow.
- `../Process.md` - public integration progress, PR branch map, and future work.
- `WORKFLOW.md` - local adoption of the generic review-gated workflow R2.
- `review-gated-development-workflow-r1.md` - required workflow for future
  public PRs.
- `public-integration-rpc-cpu-mining-guide.md` - public-safe runbook for
  operating the current `public/integration-mining-validation` branch.
- `public-pr16-nosvp-mining-only-planning-r1.md` - public-safe planning summary
  for the proposed PR-16 `--nosvp` mining-only startup mode.

## Current Integration Context

The current public integration branch includes PR-1 through PR-15. PR-8 is not
integrated. Use `Process.md` for the PR map and branch status. Use the runbook
for build, validation, RPC, mining, and SVP runtime layout details.

PR-16 is not implemented on the current integration branch. Its planned public
scope is documented in `public-pr16-nosvp-mining-only-planning-r1.md`; binding
implementation work must still go through the review-gated workflow.

## Documentation Rules

- Keep public docs free of private mining strategy, private route decisions,
  revenue data, deployment topology, secrets, and internal review archive
  material.
- Update `Process.md` after each integrated public PR.
- Update this index when a new top-level public document is added.
