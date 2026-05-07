# Review-Gated Development Workflow R1

Date: 2026-05-06

This document codifies the workflow that worked across `zent-public` PR-1
through PR-14 and is now required for future public integration work. The same
discipline was also used successfully in `zentwallet`.

The workflow exists to keep public node changes reviewable, auditable, and
safe:

1. split work into small, mostly independent branches;
2. write and review architecture before implementation;
3. write and review the implementation prompt before coding;
4. implement only after the prompt is accepted;
5. review the implementation before merge;
6. merge only after validation gates pass and P1/P2 findings are zero.

## 1. Principles

### 1.1 Small Independent PRs

Each PR should deliver one coherent capability or one bounded group of tightly
related changes.

Prefer:

- one public PR branch per feature;
- branches based on the current integration baseline;
- minimal dependency between active PR branches;
- explicit "not in this PR" language;
- deferring adjacent high-blast-radius work to a later PR.

Avoid:

- bundling unrelated mining, RPC, config, consensus, and documentation changes;
- building a long stack of dependent PRs when independent branches are enough;
- merging cleanup work into behavior-changing PRs without review;
- changing deployment guidance without matching operator documentation.

Dependency rule:

> A PR may depend on a prior PR only after that prior PR is merged into the
> integration branch, unless the dependency is explicitly documented in the
> preflight and accepted by architecture review.

### 1.2 Reviews Are Gates

Reviews are binding gates, not advisory comments. A review must end with a
decision:

- accepted / ready;
- accepted with required edits;
- ready after edits;
- not ready / may not start;
- may merge / may not merge.

Implementation must not proceed past a gate while unresolved P1 or blocking P2
findings remain.

### 1.3 Source Facts Must Be Verified

Reviews must verify source facts directly against the tree.

Good findings cite:

- file path;
- line number;
- function / type / package / CLI flag / RPC method;
- observed behavior;
- expected contract.

For `zent-public`, source-fact verification is especially important because the
tree is inherited, GOPATH-era, and contains historical behavior outside the
current public integration scope.

### 1.4 Scope Fences Are Product Quality

Every PR should state hard non-goals. Common fences:

- no consensus change unless explicitly scoped;
- no RPC surface change unless explicitly scoped;
- no mining selection behavior change unless explicitly scoped;
- no public deployment guidance change without docs;
- no broad `go build ./...` cleanup hidden inside a targeted PR;
- no generated artifacts unless a release-packaging phase says so;
- no private strategy, revenue, topology, credential, or internal archive
  material in public docs.

Scope fences should be backed by tests or grep-style checks when practical.

## 2. Roles

| Role | Responsibility |
| --- | --- |
| PR author | Drafts preflight / implementation prompt and folds in review edits. |
| External architect / reviewer | Verifies source facts, reviews design, issues P1/P2/P3 findings and gate decision. |
| Code agent | Implements only from accepted binding prompt and reports validation outputs. |
| Implementation reviewer | Reviews working-tree implementation against binding prompt and preflight. |
| Integrator | Commits, merges, runs post-merge validation, updates process docs. |

The same person may perform multiple roles, but the artifacts should remain
separate.

## 3. Branching Model

Use explicit branch names:

```text
public/pr16-<topic>
public/pr17-<topic>
public/integration-mining-validation
```

Recommended sequence:

1. create a PR branch from the current integration baseline;
2. commit preflight documents;
3. commit implementation prompt documents;
4. commit implementation;
5. push the branch;
6. merge into `public/integration-mining-validation` after implementation
   review accepts it;
7. update `Process.md` and runbook docs when the public baseline changes.

Keep PR branches independent unless a reviewed dependency says otherwise.

## 4. Stage 1: Preflight Architecture

Preflight answers: "What should be built, and what must not be built?"

Typical filename:

```text
docs/public-pr<N>-<topic>-preflight-r1.md
```

For revisions:

```text
docs/public-pr<N>-<topic>-preflight-r2.md
docs/public-pr<N>-<topic>-preflight-r3.md
```

Required content:

- objective;
- current source facts;
- affected packages;
- proposed scope;
- hard non-goals;
- consensus / RPC / config impact;
- deployment impact;
- test floor;
- validation commands;
- open questions;
- explicit gate decision target.

Preflight review should:

- verify source facts line by line;
- challenge scope creep;
- identify contradictions;
- classify findings as P1, P2, P3, or Watch;
- state whether the PR may proceed to implementation prompt.

Severity taxonomy:

| Severity | Meaning | Gate effect |
| --- | --- | --- |
| P1 | Must fix. Unsafe, wrong, contradictory, or not implementable. | Blocks next stage. |
| P2 | Should fix before handoff. Ambiguity likely to cause implementation churn. | Usually blocks unless explicitly pinned in prompt. |
| P3 | Polish / precision. Useful but not structurally blocking. | May fold into prompt or implementation review. |
| Watch | Known future concern. | Does not block. |

## 5. Stage 2: Implementation Prompt / Plan

The implementation prompt answers: "Exactly what should the code agent do?"

Typical filename:

```text
docs/public-pr<N>-<topic>-implementation-prompt-r1.md
```

Required content:

- binding preflight revision;
- binding review findings;
- objective;
- hard non-goals;
- file ownership;
- explicit do-not-modify list;
- behavior and output contracts;
- exact validation commands;
- test floor;
- required report format;
- implementation-review prompt.

Prompt review should verify:

- prompt faithfully translates accepted preflight;
- no impossible validation gate is introduced;
- test floor is executable;
- file ownership is narrow;
- fixture / seam mechanisms are pinned where needed;
- existing tests that must change are named.

Implementation may start only after prompt review accepts the prompt.

## 6. Stage 3: Implementation

The code agent implements from the accepted prompt.

Rules:

- read existing code before editing;
- follow local patterns;
- keep edits inside declared ownership;
- avoid unrelated cleanup;
- avoid generated artifacts unless the prompt says otherwise;
- run validation commands before reporting;
- report any skipped gate explicitly.

Implementation report should include:

- changed files;
- behavior summary;
- scope-fence confirmation;
- test mapping to prompt floor;
- validation outputs;
- generated artifact status;
- skipped validations, if any.

## 7. Stage 4: Implementation Review

Implementation review consumes the working tree, not only committed changes.

Reviewer checklist:

- binding prompt and preflight named;
- changed files match ownership;
- P1/P2 prompt-review findings discharged;
- behavior matches contracts;
- hard non-goals preserved;
- consensus / RPC / deployment impact matches preflight;
- test floor covered;
- validation commands actually run;
- generated artifacts understood;
- worktree state understood.

The review should include:

- verdict;
- validation gate table;
- item-by-item compliance;
- P1/P2/P3 findings;
- watch items;
- merge-readiness decision;
- no-modification confirmation if the review made no edits.

Merge only when P1 and blocking P2 findings are zero.

## 8. Stage 5: Merge And Post-Merge Validation

Recommended sequence:

```text
git add <files>
git commit -m "<type>: <summary>"
git push origin <public/prN-topic>
git switch public/integration-mining-validation
git merge --no-ff <public/prN-topic> -m "merge: integrate PR-<N> <topic>"
```

Run phase-specific validation after merge. For the current public integration
stack, the default targeted gates are:

```bash
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

Optional observer-tool gate:

```bash
go test -vet=off -count=1 \
  ./cmd/zent-route-ab-metrics/... \
  ./cmd/zent-reward-scanner/...
```

Then:

- push the integration branch;
- update `Process.md`;
- update `docs/public-integration-rpc-cpu-mining-guide.md` if operator behavior
  changed.

## 9. Prompt Templates

### 9.1 Architecture Review Prompt

```text
Please perform an expert architecture review of public PR-<N> <topic>.

Document under review:
- docs/public-pr<N>-<topic>-preflight-rM.md at <commit>

Review goals:
1. Verify source facts against the current tree.
2. Check scope and hard non-goals.
3. Check consensus/RPC/config/deployment impact.
4. Check test floor and validation gates.
5. Identify P1/P2/P3 findings and watch items.
6. State whether the PR may proceed to implementation-prompt drafting.

Please cite exact document sections/lines and code references where relevant.
Give a verdict and explicit gate decision.
```

### 9.2 Implementation Prompt Review Prompt

```text
Please perform an expert architecture review of public PR-<N> <topic>
Implementation Prompt R<M>.

Document under review:
- docs/public-pr<N>-<topic>-implementation-prompt-rM.md at <commit>

Binding sources:
- docs/public-pr<N>-<topic>-preflight-rK.md
- prior architecture reviews

Review goals:
1. Verify the prompt faithfully translates the accepted preflight.
2. Verify prior findings are discharged or pinned.
3. Check file ownership, non-goals, test floor, and validation gates.
4. Identify contradictions that would make validation fail.
5. State whether implementation may start.

Give a verdict with P1/P2/P3 findings, watch items, and implementation-gate
decision.
```

### 9.3 Code Agent Prompt

```text
Please implement public PR-<N> <topic>.

Binding prompt:
- docs/public-pr<N>-<topic>-implementation-prompt-rM.md at <commit>

Binding preflight:
- docs/public-pr<N>-<topic>-preflight-rK.md

Scope:
- <in-scope behavior>

Hard non-goals:
- <forbidden behavior>

Run and report the prompt's validation gates.

Final report must include changed files, behavior summary, test mapping,
validation outputs, generated artifact status, and skipped validations if any.
```

### 9.4 Implementation Review Prompt

```text
Please perform the public PR-<N> <topic> implementation review.

Binding prompt:
- docs/public-pr<N>-<topic>-implementation-prompt-rM.md

Binding preflight:
- docs/public-pr<N>-<topic>-preflight-rK.md

Review the working-tree implementation, including untracked files.

Focus:
1. Scope and hard non-goals.
2. Consensus/RPC/config/deployment impact.
3. File ownership and output behavior.
4. Test-floor coverage.
5. Validation gates.
6. Generated artifact status.

Give a verdict with P1/P2/P3 findings, watch items, validation results, and
explicit merge-readiness decision.
```

## 10. Split Criteria

Split a proposed change into a separate PR when it changes blast radius.

Examples:

- benchmark-only vs. runtime behavior;
- observer tooling vs. consensus or mining selection;
- config parsing vs. chain runtime ownership;
- RPC observation vs. RPC mutation;
- public operator docs vs. private deployment strategy;
- bounded one-shot tool vs. long-running runtime mode.

If a change creates new questions about consensus safety, runtime ownership,
operator interpretation, or public deployment behavior, it deserves its own
preflight.

## 11. Anti-Patterns

Avoid:

- implementation before preflight acceptance;
- implementation before implementation-prompt acceptance;
- hiding cleanup inside a behavior PR;
- relying on a large dependent PR stack when independent branches work;
- using private data in public docs;
- committing `dist/` or generated binaries outside a release-packaging phase;
- using broad tree failures as a reason to skip targeted gates;
- changing RPC behavior without an explicit test and public-doc decision;
- treating tests as a substitute for source-fact verification.

## 12. Maintenance

Update this document when:

- the review taxonomy changes;
- the validation gates change;
- future PRs discover recurring failure modes;
- generated artifact policy changes;
- public integration branch policy changes.

This workflow is part of the quality system. Keep it explicit.
