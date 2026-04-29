package minerchain

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"btcd/blockchain"
	"btcd/chaincfg"
	"btcd/wire"
	"github.com/omegasuite/btcd/chaincfg/chainhash"
)

const powHelperGoldenPath = "testdata/pow_helper_golden.json"

var updatePowHelperGolden = flag.Bool("update_pow_helper_golden", false, "rewrite pow helper golden vectors")

type powHelperFixture struct {
	Name                    string `json:"name"`
	HeaderSerializedHex     string `json:"header_serialized_hex"`
	NonceHex                string `json:"nonce_hex"`
	BitsHex                 string `json:"bits_hex"`
	FactorPOW               int64  `json:"factor_pow"`
	H                       int64  `json:"h"`
	PowLimitHex             string `json:"pow_limit_hex"`
	PowLimitSource          string `json:"pow_limit_source"`
	HeaderHashHex           string `json:"header_hash_hex"`
	HashNumHex              string `json:"hash_num_hex"`
	BaseTargetHex           string `json:"base_target_hex"`
	RhsTargetBeforeClampHex string `json:"rhs_target_before_clamp_hex"`
	RhsTargetAfterClampHex  string `json:"rhs_target_after_clamp_hex"`
	CmpFactor               string `json:"cmp_factor"`
	PowLimitCmp             int    `json:"pow_limit_cmp"`
	ExpectedHit             bool   `json:"expected_hit"`
}

var powHelperFixtureFields = []string{
	"name",
	"header_serialized_hex",
	"nonce_hex",
	"bits_hex",
	"factor_pow",
	"h",
	"pow_limit_hex",
	"pow_limit_source",
	"header_hash_hex",
	"hash_num_hex",
	"base_target_hex",
	"rhs_target_before_clamp_hex",
	"rhs_target_after_clamp_hex",
	"cmp_factor",
	"pow_limit_cmp",
	"expected_hit",
}

func TestPowHelperGoldenVectors(t *testing.T) {
	if *updatePowHelperGolden {
		writePowHelperGolden(t, buildPowHelperGoldenFixtures(t))
	}

	fixtures := loadPowHelperFixtures(t)
	if len(fixtures) < 32 {
		t.Fatalf("golden fixture count = %d, want at least 32", len(fixtures))
	}
	requireMandatoryPowHelperBoundaries(t, fixtures)

	for i := range fixtures {
		runFixture(t, fixtures[i])
	}
}

func TestTryMinerNonceInvalidInputs(t *testing.T) {
	header := powHelperHeader(500, 0x207fffff, 1)
	powLimit := syntheticPowLimit(256)

	tests := []struct {
		name      string
		header    *wire.MingingRightBlock
		powLimit  *big.Int
		factorPOW int64
		h         int64
		bits      uint32
	}{
		{name: "nil-header", header: nil, powLimit: powLimit, factorPOW: 1, h: 1, bits: 0x207fffff},
		{name: "nil-pow-limit", header: header, powLimit: nil, factorPOW: 1, h: 1, bits: 0x207fffff},
		{name: "zero-pow-limit", header: header, powLimit: new(big.Int), factorPOW: 1, h: 1, bits: 0x207fffff},
		{name: "zero-factor", header: header, powLimit: powLimit, factorPOW: 0, h: 1, bits: 0x207fffff},
		{name: "zero-h", header: header, powLimit: powLimit, factorPOW: 1, h: 0, bits: 0x207fffff},
		{name: "zero-bits", header: header, powLimit: powLimit, factorPOW: 1, h: 1, bits: 0},
	}

	for _, test := range tests {
		if TryMinerNonce(test.header, 7, test.bits, test.factorPOW, test.h, test.powLimit) {
			t.Fatalf("%s: TryMinerNonce returned true for invalid input", test.name)
		}
	}
}

func TestTryMinerNonceReferenceCrossCheck(t *testing.T) {
	rng := rand.New(rand.NewSource(0x5150a7e))
	bitChoices := []uint32{0x1e000ff0, 0x1f00ffff, 0x1f0fffff, 0x2000ffff, 0x207fffff, 0x2100ffff}
	factorChoices := []int64{-64, -16, -4, -1, 1, 2, 8, 32, 128}
	powLimitChoices := []*big.Int{
		new(big.Int).Set(chaincfg.MainNetParams.PowLimit),
		new(big.Int).Set(chaincfg.TestNet3Params.PowLimit),
		syntheticPowLimit(244),
		syntheticPowLimit(256),
		syntheticPowLimit(300),
	}

	const generatedFixtures = 1500
	for i := 0; i < generatedFixtures; i++ {
		bits := bitChoices[rng.Intn(len(bitChoices))]
		nonce := int32(rng.Int31n(maxNonce))
		factor := factorChoices[rng.Intn(len(factorChoices))]
		h := int64(rng.Intn(64) + 1)
		powLimit := new(big.Int).Set(powLimitChoices[rng.Intn(len(powLimitChoices))])
		header := powHelperHeader(1000+i, bitChoices[(i+1)%len(bitChoices)], int32(i%maxNonce))

		refHit := referenceTryMinerNonce(header, nonce, bits, factor, h, powLimit)
		trace, gotHit := traceMinerNonceDecision(header, nonce, bits, factor, h, powLimit)
		if gotHit != refHit {
			t.Fatalf("generated fixture %d: hit = %v, want %v", i, gotHit, refHit)
		}
		if trace.ExpectedHit != refHit {
			t.Fatalf("generated fixture %d: trace ExpectedHit = %v, want %v", i, trace.ExpectedHit, refHit)
		}
		if TryMinerNonce(header, nonce, bits, factor, h, powLimit) != refHit {
			t.Fatalf("generated fixture %d: TryMinerNonce mismatch", i)
		}
	}
}

func runFixture(t *testing.T, fixture powHelperFixture) {
	t.Helper()

	nonce := parseUint32HexField(t, fixture.Name, "nonce_hex", fixture.NonceHex)
	if nonce > maxNonce {
		t.Fatalf("%s: nonce %08x outside positive miner nonce range", fixture.Name, nonce)
	}
	bits := parseUint32HexField(t, fixture.Name, "bits_hex", fixture.BitsHex)
	powLimit := parseCanonicalBigHex(t, fixture.Name, "pow_limit_hex", fixture.PowLimitHex)
	requirePowLimitSource(t, fixture, powLimit)
	requireCanonicalBigHex(t, fixture.Name, "cmp_factor", fixture.CmpFactor)

	serializedHeader := decodeHexField(t, fixture.Name, "header_serialized_hex", fixture.HeaderSerializedHex)
	var header wire.MingingRightBlock
	if err := header.Deserialize(bytes.NewReader(serializedHeader)); err != nil {
		t.Fatalf("%s: decode header: %v", fixture.Name, err)
	}
	if header.Bits != bits {
		t.Fatalf("%s: decoded Bits = %08x, want %08x", fixture.Name, header.Bits, bits)
	}
	if header.Nonce != int32(nonce) {
		t.Fatalf("%s: decoded Nonce = %d, want %d", fixture.Name, header.Nonce, nonce)
	}
	var roundTrip bytes.Buffer
	if err := header.Serialize(&roundTrip); err != nil {
		t.Fatalf("%s: serialize header: %v", fixture.Name, err)
	}
	if !bytes.Equal(serializedHeader, roundTrip.Bytes()) {
		t.Fatalf("%s: header serialization did not round trip", fixture.Name)
	}

	callHeader := header
	callHeader.Bits = bits ^ 0x01010101
	callHeader.Nonce = -12345
	trace, gotHit := traceMinerNonceDecision(&callHeader, int32(nonce), bits, fixture.FactorPOW, fixture.H, powLimit)
	if callHeader.Bits != bits^0x01010101 || callHeader.Nonce != -12345 {
		t.Fatalf("%s: traceMinerNonceDecision mutated its input header", fixture.Name)
	}

	compareTraceToFixture(t, fixture, trace)
	validateFixtureDerivations(t, fixture, &header, int32(nonce), bits, powLimit)

	if gotHit != fixture.ExpectedHit {
		t.Fatalf("%s: trace hit = %v, want %v", fixture.Name, gotHit, fixture.ExpectedHit)
	}
	if trace.ExpectedHit != fixture.ExpectedHit {
		t.Fatalf("%s: trace ExpectedHit = %v, want %v", fixture.Name, trace.ExpectedHit, fixture.ExpectedHit)
	}
	if TryMinerNonce(&header, int32(nonce), bits, fixture.FactorPOW, fixture.H, powLimit) != fixture.ExpectedHit {
		t.Fatalf("%s: TryMinerNonce does not match expected_hit", fixture.Name)
	}

	if refHit := referenceTryMinerNonce(&header, int32(nonce), bits, fixture.FactorPOW, fixture.H, powLimit); refHit != fixture.ExpectedHit {
		t.Fatalf("%s: referenceTryMinerNonce = %v, want %v", fixture.Name, refHit, fixture.ExpectedHit)
	}

	if fixture.Name == "aliasing-detection-clamp" {
		if trace.RhsTargetBeforeClamp == trace.RhsTargetAfterClamp {
			t.Fatalf("%s: before/after clamp targets alias the same *big.Int", fixture.Name)
		}
		before := trace.RhsTargetBeforeClamp.Text(16)
		trace.RhsTargetAfterClamp.SetInt64(0)
		if trace.RhsTargetBeforeClamp.Text(16) != before {
			t.Fatalf("%s: mutating after-clamp target changed before-clamp target", fixture.Name)
		}
	}
}

func compareTraceToFixture(t *testing.T, fixture powHelperFixture, trace powDecisionTrace) {
	t.Helper()

	if got := hex.EncodeToString(trace.Hash[:]); got != fixture.HeaderHashHex {
		t.Fatalf("%s: header_hash_hex = %s, want %s", fixture.Name, got, fixture.HeaderHashHex)
	}
	compareBigHex(t, fixture.Name, "hash_num_hex", trace.HashNum, fixture.HashNumHex)
	compareBigHex(t, fixture.Name, "base_target_hex", trace.BaseTarget, fixture.BaseTargetHex)
	compareBigHex(t, fixture.Name, "rhs_target_before_clamp_hex", trace.RhsTargetBeforeClamp, fixture.RhsTargetBeforeClampHex)
	compareBigHex(t, fixture.Name, "rhs_target_after_clamp_hex", trace.RhsTargetAfterClamp, fixture.RhsTargetAfterClampHex)
	compareBigHex(t, fixture.Name, "cmp_factor", trace.CmpFactor, fixture.CmpFactor)
	if trace.PowLimitCmp != fixture.PowLimitCmp {
		t.Fatalf("%s: pow_limit_cmp = %d, want %d", fixture.Name, trace.PowLimitCmp, fixture.PowLimitCmp)
	}
	if trace.RhsTargetBeforeClamp != nil && trace.RhsTargetBeforeClamp == trace.RhsTargetAfterClamp {
		t.Fatalf("%s: before/after clamp targets alias the same *big.Int", fixture.Name)
	}
}

func validateFixtureDerivations(t *testing.T, fixture powHelperFixture, header *wire.MingingRightBlock, nonce int32, bits uint32, powLimit *big.Int) {
	t.Helper()

	ref, refHit := referenceMinerNonceDecision(header, nonce, bits, fixture.FactorPOW, fixture.H, powLimit)

	compareBigHex(t, fixture.Name, "D1 base_target_hex", ref.BaseTarget, fixture.BaseTargetHex)
	compareBigHex(t, fixture.Name, "D2 rhs_target_before_clamp_hex", ref.RhsTargetBeforeClamp, fixture.RhsTargetBeforeClampHex)
	compareBigHex(t, fixture.Name, "D3 rhs_target_after_clamp_hex", ref.RhsTargetAfterClamp, fixture.RhsTargetAfterClampHex)
	if ref.PowLimitCmp != fixture.PowLimitCmp {
		t.Fatalf("%s: D4 pow_limit_cmp = %d, want %d", fixture.Name, ref.PowLimitCmp, fixture.PowLimitCmp)
	}
	if refHit != fixture.ExpectedHit {
		t.Fatalf("%s: D5 expected_hit = %v, want %v", fixture.Name, refHit, fixture.ExpectedHit)
	}
	compareBigHex(t, fixture.Name, "D6 hash_num_hex", ref.HashNum, fixture.HashNumHex)
}

func referenceTryMinerNonce(header *wire.MingingRightBlock, nonce int32, bits uint32, factorPOW int64, h int64, powLimit *big.Int) bool {
	_, hit := referenceMinerNonceDecision(header, nonce, bits, factorPOW, h, powLimit)
	return hit
}

func referenceMinerNonceDecision(header *wire.MingingRightBlock, nonce int32, bits uint32, factorPOW int64, h int64, powLimit *big.Int) (powDecisionTrace, bool) {
	var trace powDecisionTrace
	if header == nil || powLimit == nil || powLimit.Sign() <= 0 || h < 1 || factorPOW == 0 {
		return trace, false
	}

	localHeader := *header
	localHeader.Nonce = nonce
	localHeader.Bits = bits
	hash := localHeader.BlockHash()
	hashNum := blockchain.HashToBig(&hash)
	baseTarget := blockchain.CompactToBig(bits)
	if baseTarget.Sign() <= 0 {
		return powDecisionTrace{
			Hash:        hash,
			HashNum:     new(big.Int).Set(hashNum),
			BaseTarget:  new(big.Int).Set(baseTarget),
			PowLimitCmp: hashNum.Cmp(powLimit),
		}, false
	}

	hMultiplier := big.NewInt(h)
	rhsBeforeClamp := new(big.Int).Mul(new(big.Int).Set(baseTarget), hMultiplier)
	cmpFactor := big.NewInt(1)
	if factorPOW > 0 {
		cmpFactor = big.NewInt(factorPOW)
	} else {
		absFactor := new(big.Int).Abs(new(big.Int).SetInt64(factorPOW))
		rhsBeforeClamp.Mul(rhsBeforeClamp, absFactor)
	}

	rhsAfterClamp := new(big.Int).Set(rhsBeforeClamp)
	if rhsAfterClamp.Cmp(powLimit) > 0 {
		rhsAfterClamp = new(big.Int).Set(powLimit)
	}

	powLimitCmp := hashNum.Cmp(powLimit)
	hit := false
	if powLimitCmp <= 0 {
		scaledHash := new(big.Int).Mul(new(big.Int).Set(hashNum), cmpFactor)
		hit = scaledHash.Cmp(rhsAfterClamp) <= 0
	}

	trace = powDecisionTrace{
		Hash:                 hash,
		HashNum:              new(big.Int).Set(hashNum),
		BaseTarget:           new(big.Int).Set(baseTarget),
		RhsTargetBeforeClamp: new(big.Int).Set(rhsBeforeClamp),
		RhsTargetAfterClamp:  new(big.Int).Set(rhsAfterClamp),
		CmpFactor:            new(big.Int).Set(cmpFactor),
		PowLimitCmp:          powLimitCmp,
		ExpectedHit:          hit,
	}
	return trace, hit
}

func loadPowHelperFixtures(t *testing.T) []powHelperFixture {
	t.Helper()

	data, err := os.ReadFile(powHelperGoldenPath)
	if err != nil {
		t.Fatalf("read %s: %v", powHelperGoldenPath, err)
	}

	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode top-level golden array: %v", err)
	}

	fixtures := make([]powHelperFixture, len(raw))
	for i, fields := range raw {
		validateFixtureShape(t, i, fields)
		encoded, err := json.Marshal(fields)
		if err != nil {
			t.Fatalf("fixture %d: marshal field map: %v", i, err)
		}
		if err := json.Unmarshal(encoded, &fixtures[i]); err != nil {
			t.Fatalf("fixture %d: decode fixture: %v", i, err)
		}
		if fixtures[i].Name == "" {
			t.Fatalf("fixture %d: empty name", i)
		}
	}

	return fixtures
}

func validateFixtureShape(t *testing.T, index int, fields map[string]json.RawMessage) {
	t.Helper()

	if len(fields) != len(powHelperFixtureFields) {
		keys := make([]string, 0, len(fields))
		for key := range fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		t.Fatalf("fixture %d: field count = %d, want %d: %v", index, len(fields), len(powHelperFixtureFields), keys)
	}

	for _, field := range powHelperFixtureFields {
		if _, ok := fields[field]; !ok {
			t.Fatalf("fixture %d: missing field %q", index, field)
		}
	}
}

func requireMandatoryPowHelperBoundaries(t *testing.T, fixtures []powHelperFixture) {
	t.Helper()

	required := map[string]bool{
		"positive-factor-normal-miss":                false,
		"positive-factor-normal-hit":                 false,
		"negative-factor-target-side-hit":            false,
		"negative-factor-miss":                       false,
		"target-clamp-to-pow-limit":                  false,
		"hash-equals-pow-limit-rhs-equals-pow-limit": false,
		"hash-greater-than-pow-limit":                false,
		"h-equals-one":                               false,
		"aliasing-detection-clamp":                   false,
	}
	for _, fixture := range fixtures {
		if _, ok := required[fixture.Name]; ok {
			required[fixture.Name] = true
		}
	}
	for name, found := range required {
		if !found {
			t.Fatalf("missing mandatory boundary fixture %q", name)
		}
	}
}

func requirePowLimitSource(t *testing.T, fixture powHelperFixture, powLimit *big.Int) {
	t.Helper()

	switch fixture.PowLimitSource {
	case "mainnet":
		if powLimit.Cmp(chaincfg.MainNetParams.PowLimit) != 0 {
			t.Fatalf("%s: mainnet pow_limit_hex does not match chaincfg.MainNetParams.PowLimit", fixture.Name)
		}
	case "testnet3":
		if powLimit.Cmp(chaincfg.TestNet3Params.PowLimit) != 0 {
			t.Fatalf("%s: testnet3 pow_limit_hex does not match chaincfg.TestNet3Params.PowLimit", fixture.Name)
		}
	case "synthetic":
		if powLimit.Sign() <= 0 {
			t.Fatalf("%s: synthetic pow_limit_hex must be positive", fixture.Name)
		}
	default:
		t.Fatalf("%s: unsupported pow_limit_source %q", fixture.Name, fixture.PowLimitSource)
	}
}

func writePowHelperGolden(t *testing.T, fixtures []powHelperFixture) {
	t.Helper()

	data, err := json.MarshalIndent(fixtures, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden fixtures: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(powHelperGoldenPath, data, 0644); err != nil {
		t.Fatalf("write %s: %v", powHelperGoldenPath, err)
	}
}

func buildPowHelperGoldenFixtures(t *testing.T) []powHelperFixture {
	t.Helper()

	hugePowLimit := syntheticPowLimit(300)
	maxHashPowLimit := syntheticPowLimit(256)

	fixtures := []powHelperFixture{
		makePowHelperFixture(t, "positive-factor-normal-miss", 1, 0x1f00ffff, 11, 64, 1, hugePowLimit, "synthetic"),
		makePowHelperFixture(t, "positive-factor-normal-hit", 2, 0x207fffff, 12, 2, 4, hugePowLimit, "synthetic"),
		makePowHelperFixture(t, "negative-factor-target-side-hit", 3, 0x207fffff, 13, -4, 2, hugePowLimit, "synthetic"),
		makePowHelperFixture(t, "negative-factor-miss", 4, 0x1f00ffff, 14, -2, 1, hugePowLimit, "synthetic"),
		makePowHelperFixtureWithHashPowLimit(t, "target-clamp-to-pow-limit", 5, 0x2100ffff, 15, -16, 4, 1),
		makePowHelperFixtureWithHashPowLimit(t, "hash-equals-pow-limit-rhs-equals-pow-limit", 6, 0x2100ffff, 16, -16, 4, 0),
		makePowHelperFixtureWithHashPowLimit(t, "hash-greater-than-pow-limit", 7, 0x2100ffff, 17, -16, 4, -1),
		makePowHelperFixture(t, "h-equals-one", 8, 0x2100ffff, 18, -16, 1, hugePowLimit, "synthetic"),
		makePowHelperFixtureWithHashPowLimit(t, "aliasing-detection-clamp", 9, 0x2100ffff, 19, -32, 3, 2),
		makePowHelperFixture(t, "mainnet-source-00", 20, 0x1e000ff0, 20, 1, 1, chaincfg.MainNetParams.PowLimit, "mainnet"),
		makePowHelperFixture(t, "mainnet-source-01", 21, 0x1e000ff0, 21, 4, 3, chaincfg.MainNetParams.PowLimit, "mainnet"),
		makePowHelperFixture(t, "mainnet-source-02", 22, 0x1f00ffff, 22, -4, 8, chaincfg.MainNetParams.PowLimit, "mainnet"),
		makePowHelperFixture(t, "testnet3-source-00", 23, 0x1f0fffff, 23, 1, 1, chaincfg.TestNet3Params.PowLimit, "testnet3"),
		makePowHelperFixture(t, "testnet3-source-01", 24, 0x1f0fffff, 24, 8, 4, chaincfg.TestNet3Params.PowLimit, "testnet3"),
		makePowHelperFixture(t, "testnet3-source-02", 25, 0x2000ffff, 25, -8, 2, chaincfg.TestNet3Params.PowLimit, "testnet3"),
		makePowHelperFixture(t, "synthetic-positive-factor-00", 26, 0x1f0fffff, 26, 1, 3, maxHashPowLimit, "synthetic"),
		makePowHelperFixture(t, "synthetic-positive-factor-01", 27, 0x2000ffff, 27, 2, 5, maxHashPowLimit, "synthetic"),
		makePowHelperFixture(t, "synthetic-positive-factor-02", 28, 0x1f00ffff, 28, 16, 2, maxHashPowLimit, "synthetic"),
		makePowHelperFixture(t, "synthetic-positive-factor-03", 29, 0x207fffff, 29, 32, 64, hugePowLimit, "synthetic"),
		makePowHelperFixture(t, "synthetic-positive-factor-04", 30, 0x2000ffff, 30, 128, 1, hugePowLimit, "synthetic"),
		makePowHelperFixture(t, "synthetic-negative-factor-00", 31, 0x1f0fffff, 31, -1, 1, maxHashPowLimit, "synthetic"),
		makePowHelperFixture(t, "synthetic-negative-factor-01", 32, 0x2000ffff, 32, -2, 1, maxHashPowLimit, "synthetic"),
		makePowHelperFixture(t, "synthetic-negative-factor-02", 33, 0x207fffff, 33, -4, 1, maxHashPowLimit, "synthetic"),
		makePowHelperFixture(t, "synthetic-negative-factor-03", 34, 0x2100ffff, 34, -8, 1, hugePowLimit, "synthetic"),
		makePowHelperFixture(t, "synthetic-negative-factor-04", 35, 0x1e000ff0, 35, -16, 16, maxHashPowLimit, "synthetic"),
		makePowHelperFixture(t, "synthetic-clamp-00", 36, 0x2100ffff, 36, 1, 2, maxHashPowLimit, "synthetic"),
		makePowHelperFixture(t, "synthetic-clamp-01", 37, 0x2100ffff, 37, 2, 2, maxHashPowLimit, "synthetic"),
		makePowHelperFixture(t, "synthetic-clamp-02", 38, 0x2100ffff, 38, -2, 2, maxHashPowLimit, "synthetic"),
		makePowHelperFixture(t, "synthetic-clamp-03", 39, 0x2100ffff, 39, -64, 8, maxHashPowLimit, "synthetic"),
		makePowHelperFixtureWithHashPowLimit(t, "synthetic-hash-limit-plus-00", 40, 0x2000ffff, 40, 1, 1, 5),
		makePowHelperFixtureWithHashPowLimit(t, "synthetic-hash-limit-plus-01", 41, 0x1f00ffff, 41, 8, 8, 9),
		makePowHelperFixtureWithHashPowLimit(t, "synthetic-hash-limit-minus-00", 42, 0x207fffff, 42, 1, 1, -3),
	}

	expectFixture(t, fixtures[0], false, false)
	expectFixture(t, fixtures[1], true, false)
	expectFixture(t, fixtures[2], true, false)
	expectFixture(t, fixtures[3], false, false)
	expectFixture(t, fixtures[4], true, true)
	expectFixture(t, fixtures[5], true, true)
	expectFixture(t, fixtures[6], false, true)
	expectFixture(t, fixtures[7], true, false)
	expectFixture(t, fixtures[8], true, true)

	return fixtures
}

func makePowHelperFixtureWithHashPowLimit(t *testing.T, name string, index int, bits uint32, nonce uint32, factorPOW int64, h int64, hashDelta int64) powHelperFixture {
	t.Helper()

	header := powHelperHeader(index, bits, int32(nonce))
	hash := header.BlockHash()
	hashNum := blockchain.HashToBig(&hash)
	powLimit := new(big.Int).Add(hashNum, big.NewInt(hashDelta))
	if powLimit.Sign() <= 0 {
		t.Fatalf("%s: derived non-positive pow limit", name)
	}
	return makePowHelperFixtureFromHeader(t, name, header, bits, nonce, factorPOW, h, powLimit, "synthetic")
}

func makePowHelperFixture(t *testing.T, name string, index int, bits uint32, nonce uint32, factorPOW int64, h int64, powLimit *big.Int, powLimitSource string) powHelperFixture {
	t.Helper()

	header := powHelperHeader(index, bits, int32(nonce))
	return makePowHelperFixtureFromHeader(t, name, header, bits, nonce, factorPOW, h, powLimit, powLimitSource)
}

func makePowHelperFixtureFromHeader(t *testing.T, name string, header *wire.MingingRightBlock, bits uint32, nonce uint32, factorPOW int64, h int64, powLimit *big.Int, powLimitSource string) powHelperFixture {
	t.Helper()

	var serialized bytes.Buffer
	if err := header.Serialize(&serialized); err != nil {
		t.Fatalf("%s: serialize generated fixture header: %v", name, err)
	}

	trace, hit := referenceMinerNonceDecision(header, int32(nonce), bits, factorPOW, h, powLimit)
	return powHelperFixture{
		Name:                    name,
		HeaderSerializedHex:     hex.EncodeToString(serialized.Bytes()),
		NonceHex:                fmt.Sprintf("%08x", nonce),
		BitsHex:                 fmt.Sprintf("%08x", bits),
		FactorPOW:               factorPOW,
		H:                       h,
		PowLimitHex:             powLimit.Text(16),
		PowLimitSource:          powLimitSource,
		HeaderHashHex:           hex.EncodeToString(trace.Hash[:]),
		HashNumHex:              trace.HashNum.Text(16),
		BaseTargetHex:           trace.BaseTarget.Text(16),
		RhsTargetBeforeClampHex: trace.RhsTargetBeforeClamp.Text(16),
		RhsTargetAfterClampHex:  trace.RhsTargetAfterClamp.Text(16),
		CmpFactor:               trace.CmpFactor.Text(16),
		PowLimitCmp:             trace.PowLimitCmp,
		ExpectedHit:             hit,
	}
}

func powHelperHeader(index int, bits uint32, nonce int32) *wire.MingingRightBlock {
	header := &wire.MingingRightBlock{
		Version:       uint32(0x20000 + index),
		Timestamp:     time.Unix(1700000000+int64(index*73), 0).UTC(),
		Bits:          bits,
		Nonce:         nonce,
		Connection:    []byte(fmt.Sprintf("198.51.100.%d:9788", index%255)),
		Collateral:    uint32(1 + index%97),
		MeanTPH:       uint32(1 + index%31),
		TphReports:    []uint32{uint32(1 + index%5), uint32(2 + index%7)},
		ContractLimit: uint32(1000 + index),
	}
	fillHash(&header.PrevBlock, 0x11, index)
	fillHash(&header.BestBlock, 0x29, index*3)
	for i := range header.Miner {
		header.Miner[i] = byte((index*17 + i*23) & 0xff)
	}
	return header
}

func fillHash(hash *chainhash.Hash, seed byte, offset int) {
	for i := range hash {
		hash[i] = seed + byte(offset+i*13)
	}
}

func expectFixture(t *testing.T, fixture powHelperFixture, wantHit bool, wantClamp bool) {
	t.Helper()

	if fixture.ExpectedHit != wantHit {
		t.Fatalf("%s: expected_hit = %v, want %v", fixture.Name, fixture.ExpectedHit, wantHit)
	}
	before := parseCanonicalBigHex(t, fixture.Name, "rhs_target_before_clamp_hex", fixture.RhsTargetBeforeClampHex)
	after := parseCanonicalBigHex(t, fixture.Name, "rhs_target_after_clamp_hex", fixture.RhsTargetAfterClampHex)
	clamped := before.Cmp(after) != 0
	if clamped != wantClamp {
		t.Fatalf("%s: clamp = %v, want %v", fixture.Name, clamped, wantClamp)
	}
}

func syntheticPowLimit(bits uint) *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bits), big.NewInt(1))
}

func parseUint32HexField(t *testing.T, fixtureName, field, value string) uint32 {
	t.Helper()

	if len(value) != 8 || strings.ToLower(value) != value {
		t.Fatalf("%s: %s must be 8 lowercase hex chars, got %q", fixtureName, field, value)
	}
	parsed, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		t.Fatalf("%s: parse %s: %v", fixtureName, field, err)
	}
	return uint32(parsed)
}

func decodeHexField(t *testing.T, fixtureName, field, value string) []byte {
	t.Helper()

	if strings.ToLower(value) != value {
		t.Fatalf("%s: %s must be lowercase hex", fixtureName, field)
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("%s: parse %s: %v", fixtureName, field, err)
	}
	return decoded
}

func parseCanonicalBigHex(t *testing.T, fixtureName, field, value string) *big.Int {
	t.Helper()

	requireCanonicalBigHex(t, fixtureName, field, value)
	n, ok := new(big.Int).SetString(value, 16)
	if !ok {
		t.Fatalf("%s: parse %s as big.Int", fixtureName, field)
	}
	return n
}

func requireCanonicalBigHex(t *testing.T, fixtureName, field, value string) {
	t.Helper()

	if value == "" || strings.ToLower(value) != value || strings.HasPrefix(value, "0x") {
		t.Fatalf("%s: %s must be canonical lowercase hex without 0x, got %q", fixtureName, field, value)
	}
	n, ok := new(big.Int).SetString(value, 16)
	if !ok {
		t.Fatalf("%s: parse %s as big.Int", fixtureName, field)
	}
	if n.Text(16) != value {
		t.Fatalf("%s: %s = %q, want canonical Text(16) %q", fixtureName, field, value, n.Text(16))
	}
}

func compareBigHex(t *testing.T, fixtureName, field string, got *big.Int, wantHex string) {
	t.Helper()

	if got == nil {
		t.Fatalf("%s: %s got nil *big.Int", fixtureName, field)
	}
	requireCanonicalBigHex(t, fixtureName, field, wantHex)
	if got.Text(16) != wantHex {
		t.Fatalf("%s: %s = %s, want %s", fixtureName, field, got.Text(16), wantHex)
	}
}

func TestPowHelperGeneratedFixtureBuilderMatchesGolden(t *testing.T) {
	golden := loadPowHelperFixtures(t)
	generated := buildPowHelperGoldenFixtures(t)
	if !reflect.DeepEqual(golden, generated) {
		t.Fatalf("static golden file does not match deterministic fixture builder")
	}
}

func TestPowHelperHashLimitBoundaryRejectsOnlyGreaterThan(t *testing.T) {
	header := powHelperHeader(9000, 0x2100ffff, 77)
	hash := header.BlockHash()
	hashNum := blockchain.HashToBig(&hash)

	trace, hit := traceMinerNonceDecision(header, 77, 0x2100ffff, -16, 4, hashNum)
	if !hit {
		t.Fatalf("hash == powLimit should be accepted when rhs target clamps to powLimit")
	}
	if trace.PowLimitCmp != 0 {
		t.Fatalf("powLimit cmp = %d, want 0", trace.PowLimitCmp)
	}

	lessThanHash := new(big.Int).Sub(hashNum, big.NewInt(1))
	if lessThanHash.Sign() <= 0 {
		t.Fatal("unexpected zero hash boundary")
	}
	if TryMinerNonce(header, 77, 0x2100ffff, -16, 4, lessThanHash) {
		t.Fatalf("hash > powLimit should be rejected")
	}
}

func TestPowHelperNonceRangeConstant(t *testing.T) {
	if maxNonce != 1<<31-1 {
		t.Fatalf("maxNonce = %d, want 1<<31-1", maxNonce)
	}
}
