# AGENTS.md

## Repository Role

This repository is the public PR workspace for Zent.

Use this repo only for public-safe bug fixes, tests, benchmarks, and general-purpose improvements intended for possible upstream contribution.

Do not add private mining strategy, private pool operations, private benchmark data, private deployment scripts, revenue data, IP strategy, or internal review archives to this repository.

## Remotes

Expected remotes:

- `origin`: `git@github.com:vladwilliamscc/zent.git`
- `upstream`: `git@github.com:nexuszent/zent.git` fetch-only

Private planning and private mining work live outside this repo in `zent-private`.

## Current Public PR Priority

Start with PR-1: `PR-pow-replay-helper`.

Purpose:

- add a deterministic, side-effect-free PoW nonce decision helper;
- add tests and golden vectors;
- create infrastructure for later public consensus bugfix work.

PR-1 must not change runtime behavior.

## PR-1 Allowed Scope

Allowed files:

```text
omega/minerchain/pow_helper.go
omega/minerchain/pow_helper_test.go
omega/minerchain/testdata/pow_helper_golden.json
```

Do not modify:

```text
omega/minerchain/accept.go
omega/minerchain/mining.go
btcd/chaincfg/params.go
btcd/wire/blockheader.go
collateral selector code
pool code
RPC code
configuration code
private docs
```

## PR-1 Required Semantics

The helper must follow accept semantics, not solver-only shortcuts.

Required behavior:

- clone the input header before mutation;
- write `Bits` and `Nonce` to the clone before hashing;
- compute `BlockHash()` from the clone;
- compute `hashNum` through `blockchain.HashToBig`;
- use `baseTarget = blockchain.CompactToBig(bits)`;
- for positive `factorPOW`, move factor to hash side via `cmpFactor`;
- for negative `factorPOW`, move `abs(factorPOW)` to target side;
- clamp target after h/factor multiplication;
- reject only when `hashNum > powLimit`, not `>=`;
- final hit condition is `hashNum * cmpFactor <= rhsTargetAfterClamp`;
- invalid structural inputs return false.

Do not copy solver-only `hash >= PowLimit` early reject into the shared helper.

## PR-1 Test Requirements

`pow_helper_golden.json` must contain at least 32 vectors, including these 9 mandatory boundaries:

1. positive factor normal miss
2. positive factor normal hit
3. negative factor target-side hit
4. negative factor miss
5. target clamp to PowLimit
6. `hash == PowLimit && rhsTarget == PowLimit`, expected true
7. `hash > PowLimit`, expected false
8. `h == 1`
9. rhs before/after clamp aliasing detection

Golden vector rules:

- top-level JSON array;
- 16 fields per vector;
- numeric `*big.Int` values use canonical `Text(16)`, no leading zero padding;
- `header_hash_hex` is byte-oriented raw 32-byte hex;
- `pow_limit_source` is mandatory: `mainnet`, `testnet3`, or `synthetic`;
- `runFixture` must enforce D1-D6 derivations.

## Validation

At minimum, run:

```bash
go test ./omega/minerchain
```

If this repository requires GOPATH-era setup or a special command, document the exact command and output in the PR notes.

PR notes must include:

- files changed;
- no behavior change statement;
- golden vector count;
- 9 boundary coverage table;
- D1-D6 validation statement;
- proof that `accept.go` and `mining.go` were not modified.

## Public / Private Boundary

Never import or reference private-only documents in a public PR.

Do not copy files from:

```text
zent-private
docs/internal-review/
private mining notes
private benchmark logs
pool operations notes
```

Public PRs should be clean, minimal, and understandable without internal review history.

## Atomic PR Rule

Each PR must solve one bounded problem.

Do not combine:

- PR-1 helper work;
- PR-2 h1 denominator consensus fix;
- BlockHash optimization;
- collateral selector/cache;
- miner worker config;
- observability/RPC;
- private mining optimization.

When in doubt, keep the PR smaller.
