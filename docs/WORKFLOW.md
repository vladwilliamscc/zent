# zent-public Workflow Adoption

Date: 2026-05-07

`zent-public` adopts the generic Review-Gated Development Workflow R2.

Canonical local source:

```text
../review-gated-agent-workflow/docs/review-gated-development-workflow-r2.md
../review-gated-agent-workflow/docs/multi-reviewer-gate-r1.md
../review-gated-agent-workflow/docs/worktree-hygiene-r1.md
../review-gated-agent-workflow/docs/project-adoption-guide-r1.md
```

Pinned local workflow commit at adoption time:

```text
8494ce7 docs: establish review-gated agent workflow
```

The canonical workflow is project-agnostic. This file records the local
`zent-public` overrides.

## Local Overrides

Start every `zent-public` session by reading:

```text
AGENTS.md
CLAUDE.md
README.md
Process.md
docs/INDEX.md
docs/WORKFLOW.md
docs/review-gated-development-workflow-r1.md
```

`docs/review-gated-development-workflow-r1.md` is retained as the original
project-local workflow snapshot. For new PRs, use the generic R2 workflow above
plus the local overrides in this file.

## Public / Private Boundary

Agents may read the sibling private workspace:

```text
../zent-private/docs
../zent-private/docs/internal-review
```

That private context may inform public-safe plans, reviews, and implementation
prompts. Public commits must not cite, copy, or leak private archive text,
deployment topology, hostnames, mining economics, private addresses, secrets,
or internal operational strategy.

Public docs must be rewritten as public-safe summaries.

## Branching

Current integration branch:

```text
public/integration-mining-validation
```

PR-16 pilot branch:

```text
public/pr16-nosvp-mining-only
```

Keep future PRs small and independent where practical.

## Multi-Reviewer Policy

Use the generic risk classes from `multi-reviewer-gate-r1.md`.

For `zent-public`, Class A includes:

- consensus or protocol behavior;
- tx-chain or miner-chain validation;
- mining selection, reward, collateral, or payout behavior;
- public RPC surface;
- daemon startup lifecycle;
- runtime ownership;
- operator deployment guidance that affects production nodes.

PR-16 changes daemon startup lifecycle and runtime ownership, so it should use
a multi-reviewer architecture gate if reviewers are available. The final gate
must record how findings from all reviewers were resolved.

## Worktree Hygiene

Follow `worktree-hygiene-r1.md`.

Local generated artifacts such as `dist/` are not source artifacts and must not
be committed unless a reviewed release-packaging phase explicitly says so.

Review-only agents must not edit files. Code agents must implement only from an
accepted implementation prompt.

## Validation Baseline

The repository is GOPATH-era. Use the targeted validation from `Process.md` and
the public integration runbook.

Typical targeted validation:

```bash
go test -vet=off -count=1 \
  ./omega/runtimemetrics \
  ./omega/minerchain \
  ./omega/consensus \
  ./omgd

go build -o /private/tmp/zent-omgd-public-integration ./omgd
```

Do not use `go build -vet=off`; `-vet=off` is a `go test` flag only.
