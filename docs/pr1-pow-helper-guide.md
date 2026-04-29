# PR-1 Guide: PR-pow-replay-helper

## Goal

Add a deterministic, side-effect-free PoW nonce decision helper for miner-chain PoW testing and replay.

This PR is public-safe and should not change runtime behavior.

## Allowed Files

```text
omega/minerchain/pow_helper.go
omega/minerchain/pow_helper_test.go
omega/minerchain/testdata/pow_helper_golden.json
```

No other files should change in PR-1.

## Non-Objectives

Do not modify:

```text
omega/minerchain/accept.go
omega/minerchain/mining.go
btcd/chaincfg/params.go
btcd/wire/blockheader.go
collateral selector code
BlockHash implementation
RPC code
configuration code
```

Do not connect the helper to existing accept or solver call sites in PR-1. That belongs to a later PR.

## Required Production Shape

Production file: `omega/minerchain/pow_helper.go`.

Required shape:

```text
unexported traceMinerNonceDecision(...) (powDecisionTrace, bool)
exported or package-visible TryMinerNonce(...) bool as a thin wrapper
```

`traceMinerNonceDecision` is the single production primitive. Tests may call it directly because tests are in the same package.

## Required Semantics

Use accept semantics as the normative rule.

Algorithm:

1. Reject structural invalid inputs: nil header, nil powLimit, h < 1, factorPOW == 0.
2. Shallow-copy the header value.
3. Write `Nonce` and `Bits` to the copy before hashing.
4. Compute `hash := local.BlockHash()`.
5. Compute `hashNum := blockchain.HashToBig(&hash)`.
6. Compute `baseTarget := blockchain.CompactToBig(bits)`.
7. Build target side:
   - start with `rhsTarget = baseTarget * h`;
   - if `factorPOW > 0`, set `cmpFactor = factorPOW` and leave target side unchanged by factor;
   - if `factorPOW < 0`, multiply target side by `abs(factorPOW)` and set `cmpFactor = 1`.
8. Clamp target after h/factor multiplication: `rhsTargetAfterClamp = min(rhsTargetBeforeClamp, powLimit)`.
9. If `hashNum > powLimit`, return false.
10. Otherwise return `hashNum * cmpFactor <= rhsTargetAfterClamp`.

Important boundary:

```text
hash == PowLimit && rhsTarget == PowLimit => expected true
```

Do not use solver-only `hash >= PowLimit` as the shared rule.

## Trace Fields

`powDecisionTrace` must expose enough information for tests to compare golden intermediate values:

```text
Hash
HashNum
BaseTarget
RhsTargetBeforeClamp
RhsTargetAfterClamp
CmpFactor
PowLimitCmp
ExpectedHit
```

`RhsTargetBeforeClamp` and `RhsTargetAfterClamp` must be independent `*big.Int` instances. The aliasing-detection fixture must fail if clamp mutates both values.

## Golden Vector File

Path:

```text
omega/minerchain/testdata/pow_helper_golden.json
```

Shape:

- top-level JSON array;
- at least 32 vectors;
- 16 fields per vector;
- no comments;
- no `0x` prefixes;
- lowercase hex.

Fields:

```text
name
header_serialized_hex
nonce_hex
bits_hex
factor_pow
h
pow_limit_hex
pow_limit_source
header_hash_hex
hash_num_hex
base_target_hex
rhs_target_before_clamp_hex
rhs_target_after_clamp_hex
cmp_factor
pow_limit_cmp
expected_hit
```

Encoding rules:

- numeric `*big.Int` fields use canonical `Text(16)`, no leading zero padding;
- `header_hash_hex` is raw little-endian chainhash bytes and must be exactly 64 hex chars;
- `hash_num_hex` is `blockchain.HashToBig(&hash).Text(16)`;
- `pow_limit_source` is mandatory: `mainnet`, `testnet3`, or `synthetic`.

## Mandatory Boundary Vectors

At least these 9 vectors are required:

1. positive factor normal miss
2. positive factor normal hit
3. negative factor target-side hit
4. negative factor miss
5. target clamp to PowLimit
6. `hash == PowLimit && rhsTarget == PowLimit`, expected true
7. `hash > PowLimit`, expected false
8. `h == 1`
9. rhs before/after clamp aliasing detection

## Test Fixture Requirements

`runFixture` must validate, in this order:

1. JSON shape, nonce range, powLimit parsing, `pow_limit_source` provenance, and canonical `Text(16)` for `pow_limit_hex`.
2. Header decode, `Bits` / `Nonce` consistency, and serialization round-trip.
3. Trace computation through production `traceMinerNonceDecision`.
4. Intermediate field comparison before `expected_hit`.
5. D1-D6 source derivation:
   - D1 `base_target_hex = CompactToBig(bits).Text(16)`;
   - D2 `rhs_target_before_clamp_hex`;
   - D3 `rhs_target_after_clamp_hex`;
   - D4 `pow_limit_cmp`;
   - D5 `expected_hit` full accept semantics;
   - D6 `hash_num_hex = HashToBig(header_hash).Text(16)`.
6. `TryMinerNonce` bool equals trace `ExpectedHit` and golden `expected_hit`.
7. Independent `referenceTryMinerNonce` bool cross-check over 1000+ generated fixtures.

## Validation

Run the focused miner-chain tests:

```bash
go test ./omega/minerchain
```

If the repository requires GOPATH-era setup, document the actual command used and the output summary.

Before commit, confirm only PR-1 files changed:

```bash
git diff --name-only
```

Expected changed files:

```text
omega/minerchain/pow_helper.go
omega/minerchain/pow_helper_test.go
omega/minerchain/testdata/pow_helper_golden.json
```

## Commit Message

Suggested commit message:

```text
test(minerchain): add deterministic PoW nonce decision helper
```
