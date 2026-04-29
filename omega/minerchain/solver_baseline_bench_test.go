package minerchain

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"btcd/blockchain"
	"btcd/wire"
	"github.com/omegasuite/btcd/chaincfg/chainhash"
)

const solverBaselineGoldenPath = "testdata/solver_baseline_hitset_golden.json"

var updateSolverBaselineGolden = flag.Bool("update_solver_baseline_golden", false, "rewrite solver baseline hit-set golden vectors")

var solverBaselineHitSink int

type solverBaselineFixture struct {
	Name                string              `json:"name"`
	HeaderSerializedHex string              `json:"header_serialized_hex"`
	BitsHex             string              `json:"bits_hex"`
	FactorPOW           int64               `json:"factor_pow"`
	H                   int64               `json:"h"`
	PowLimitHex         string              `json:"pow_limit_hex"`
	StartNonceHex       string              `json:"start_nonce_hex"`
	Iterations          int                 `json:"iterations"`
	ExpectedHits        []solverBaselineHit `json:"expected_hits"`
}

type solverBaselineHit struct {
	NonceHex     string `json:"nonce_hex"`
	BlockHashHex string `json:"block_hash_hex"`
}

type solverBaselineCase struct {
	name       string
	header     *wire.MingingRightBlock
	bits       uint32
	factorPOW  int64
	h          int64
	powLimit   *big.Int
	startNonce uint32
	iterations int
}

type solverBaselineWork struct {
	header           *wire.MingingRightBlock
	bits             uint32
	powLimit         *big.Int
	targetDifficulty *big.Int
	factorPOW        int64
}

func TestSolverBaselineHitSetGolden(t *testing.T) {
	if *updateSolverBaselineGolden {
		writeSolverBaselineGolden(t, buildSolverBaselineGoldenFixtures(t))
	}

	fixtures := loadSolverBaselineFixtures(t)
	for _, fixture := range fixtures {
		runSolverBaselineFixture(t, fixture)
	}
}

func BenchmarkSolverBaselineLoop(b *testing.B) {
	for _, testCase := range solverBaselineCases() {
		work := prepareSolverBaselineWork(testCase.header, testCase.bits, testCase.factorPOW, testCase.h, testCase.powLimit)
		b.Run(testCase.name, func(b *testing.B) {
			hits := 0
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				nonce := int32((testCase.startNonce + uint32(i)) & uint32(maxNonce))
				if solverBaselineCandidateHit(work, nonce) {
					hits++
				}
			}
			solverBaselineHitSink = hits
			b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "H/s")
		})
	}
}

func runSolverBaselineFixture(t *testing.T, fixture solverBaselineFixture) {
	t.Helper()

	bits := parseSolverUint32Hex(t, fixture.Name, "bits_hex", fixture.BitsHex)
	powLimit := parseSolverBigHex(t, fixture.Name, "pow_limit_hex", fixture.PowLimitHex)
	startNonce := parseSolverUint32Hex(t, fixture.Name, "start_nonce_hex", fixture.StartNonceHex)
	if fixture.Iterations <= 0 {
		t.Fatalf("%s: iterations must be positive", fixture.Name)
	}
	if fixture.H < 1 {
		t.Fatalf("%s: h must be positive", fixture.Name)
	}
	if fixture.FactorPOW == 0 {
		t.Fatalf("%s: factor_pow must be non-zero", fixture.Name)
	}

	serializedHeader := decodeSolverHex(t, fixture.Name, "header_serialized_hex", fixture.HeaderSerializedHex)
	var header wire.MingingRightBlock
	if err := header.Deserialize(bytes.NewReader(serializedHeader)); err != nil {
		t.Fatalf("%s: decode header: %v", fixture.Name, err)
	}
	if header.Bits != bits {
		t.Fatalf("%s: decoded Bits = %08x, want %08x", fixture.Name, header.Bits, bits)
	}
	var roundTrip bytes.Buffer
	if err := header.Serialize(&roundTrip); err != nil {
		t.Fatalf("%s: serialize header: %v", fixture.Name, err)
	}
	if !bytes.Equal(serializedHeader, roundTrip.Bytes()) {
		t.Fatalf("%s: header serialization did not round trip", fixture.Name)
	}

	work := prepareSolverBaselineWork(&header, bits, fixture.FactorPOW, fixture.H, powLimit)
	hits := collectSolverBaselineHits(work, startNonce, fixture.Iterations)
	if !reflect.DeepEqual(hits, fixture.ExpectedHits) {
		t.Fatalf("%s: hit set mismatch\n got: %#v\nwant: %#v", fixture.Name, hits, fixture.ExpectedHits)
	}
	for _, hit := range fixture.ExpectedHits {
		parseSolverUint32Hex(t, fixture.Name, "expected_hits.nonce_hex", hit.NonceHex)
		if len(hit.BlockHashHex) != chainhash.HashSize*2 {
			t.Fatalf("%s: block_hash_hex length = %d, want %d", fixture.Name, len(hit.BlockHashHex), chainhash.HashSize*2)
		}
		decodeSolverHex(t, fixture.Name, "expected_hits.block_hash_hex", hit.BlockHashHex)
	}
}

func prepareSolverBaselineWork(header *wire.MingingRightBlock, bits uint32, factorPOW int64, h int64, powLimit *big.Int) solverBaselineWork {
	targetDifficulty := blockchain.CompactToBig(bits)
	cmpFactor := factorPOW
	if cmpFactor < 0 {
		targetDifficulty = targetDifficulty.Mul(targetDifficulty, big.NewInt(-cmpFactor))
		cmpFactor = 1
	}
	targetDifficulty = targetDifficulty.Mul(targetDifficulty, big.NewInt(h))
	if targetDifficulty.Cmp(powLimit) > 0 {
		targetDifficulty = new(big.Int).Set(powLimit)
	}

	return solverBaselineWork{
		header:           header,
		bits:             bits,
		powLimit:         new(big.Int).Set(powLimit),
		targetDifficulty: new(big.Int).Set(targetDifficulty),
		factorPOW:        cmpFactor,
	}
}

func solverBaselineCandidateHit(work solverBaselineWork, nonce int32) bool {
	localHeader := *work.header
	localHeader.Bits = work.bits
	localHeader.Nonce = nonce
	hash := localHeader.BlockHash()
	hashNum := blockchain.HashToBig(&hash)
	if hashNum.Cmp(work.powLimit) >= 0 {
		return false
	}
	hashNum = hashNum.Mul(hashNum, big.NewInt(work.factorPOW))
	return hashNum.Cmp(work.targetDifficulty) <= 0
}

func collectSolverBaselineHits(work solverBaselineWork, startNonce uint32, iterations int) []solverBaselineHit {
	hits := make([]solverBaselineHit, 0)
	for i := 0; i < iterations; i++ {
		nonce := int32((startNonce + uint32(i)) & uint32(maxNonce))
		localHeader := *work.header
		localHeader.Bits = work.bits
		localHeader.Nonce = nonce
		hash := localHeader.BlockHash()
		hashNum := blockchain.HashToBig(&hash)
		if hashNum.Cmp(work.powLimit) >= 0 {
			continue
		}
		hashNum = hashNum.Mul(hashNum, big.NewInt(work.factorPOW))
		if hashNum.Cmp(work.targetDifficulty) <= 0 {
			hits = append(hits, solverBaselineHit{
				NonceHex:     fmt.Sprintf("%08x", uint32(nonce)),
				BlockHashHex: hex.EncodeToString(hash[:]),
			})
		}
	}
	return hits
}

func loadSolverBaselineFixtures(t *testing.T) []solverBaselineFixture {
	t.Helper()

	data, err := os.ReadFile(solverBaselineGoldenPath)
	if err != nil {
		t.Fatalf("read %s: %v", solverBaselineGoldenPath, err)
	}

	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode top-level solver golden array: %v", err)
	}

	fixtures := make([]solverBaselineFixture, len(raw))
	for i, fields := range raw {
		validateSolverBaselineShape(t, i, fields)
		encoded, err := json.Marshal(fields)
		if err != nil {
			t.Fatalf("fixture %d: marshal field map: %v", i, err)
		}
		if err := json.Unmarshal(encoded, &fixtures[i]); err != nil {
			t.Fatalf("fixture %d: decode fixture: %v", i, err)
		}
	}
	return fixtures
}

func validateSolverBaselineShape(t *testing.T, index int, fields map[string]json.RawMessage) {
	t.Helper()

	wantFields := []string{
		"name",
		"header_serialized_hex",
		"bits_hex",
		"factor_pow",
		"h",
		"pow_limit_hex",
		"start_nonce_hex",
		"iterations",
		"expected_hits",
	}
	if len(fields) != len(wantFields) {
		keys := make([]string, 0, len(fields))
		for key := range fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		t.Fatalf("fixture %d: field count = %d, want %d: %v", index, len(fields), len(wantFields), keys)
	}
	for _, field := range wantFields {
		if _, ok := fields[field]; !ok {
			t.Fatalf("fixture %d: missing field %q", index, field)
		}
	}
}

func writeSolverBaselineGolden(t *testing.T, fixtures []solverBaselineFixture) {
	t.Helper()

	data, err := json.MarshalIndent(fixtures, "", "  ")
	if err != nil {
		t.Fatalf("marshal solver golden fixtures: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(solverBaselineGoldenPath, data, 0644); err != nil {
		t.Fatalf("write %s: %v", solverBaselineGoldenPath, err)
	}
}

func buildSolverBaselineGoldenFixtures(t *testing.T) []solverBaselineFixture {
	t.Helper()

	cases := solverBaselineCases()
	fixtures := make([]solverBaselineFixture, 0, len(cases))
	for _, testCase := range cases {
		var serialized bytes.Buffer
		if err := testCase.header.Serialize(&serialized); err != nil {
			t.Fatalf("%s: serialize generated solver header: %v", testCase.name, err)
		}
		work := prepareSolverBaselineWork(testCase.header, testCase.bits, testCase.factorPOW, testCase.h, testCase.powLimit)
		fixtures = append(fixtures, solverBaselineFixture{
			Name:                testCase.name,
			HeaderSerializedHex: hex.EncodeToString(serialized.Bytes()),
			BitsHex:             fmt.Sprintf("%08x", testCase.bits),
			FactorPOW:           testCase.factorPOW,
			H:                   testCase.h,
			PowLimitHex:         testCase.powLimit.Text(16),
			StartNonceHex:       fmt.Sprintf("%08x", testCase.startNonce),
			Iterations:          testCase.iterations,
			ExpectedHits:        collectSolverBaselineHits(work, testCase.startNonce, testCase.iterations),
		})
	}
	return fixtures
}

func TestSolverBaselineGeneratedFixtureBuilderMatchesGolden(t *testing.T) {
	golden := loadSolverBaselineFixtures(t)
	generated := buildSolverBaselineGoldenFixtures(t)
	if !reflect.DeepEqual(golden, generated) {
		t.Fatalf("static solver baseline golden file does not match deterministic fixture builder")
	}
}

func solverBaselineCases() []solverBaselineCase {
	return []solverBaselineCase{
		{
			name:       "solver-positive-factor",
			header:     solverBaselineHeader(1, 0x207fffff, 1),
			bits:       0x207fffff,
			factorPOW:  8,
			h:          1,
			powLimit:   solverSyntheticPowLimit(256),
			startNonce: 1,
			iterations: 512,
		},
		{
			name:       "solver-negative-factor",
			header:     solverBaselineHeader(2, 0x2000ffff, 1),
			bits:       0x2000ffff,
			factorPOW:  -4,
			h:          2,
			powLimit:   solverSyntheticPowLimit(256),
			startNonce: 1,
			iterations: 512,
		},
		{
			name:       "solver-clamped-target",
			header:     solverBaselineHeader(3, 0x2100ffff, 1),
			bits:       0x2100ffff,
			factorPOW:  1,
			h:          1,
			powLimit:   solverSyntheticPowLimit(248),
			startNonce: 1,
			iterations: 2048,
		},
		{
			name:       "solver-tight-target",
			header:     solverBaselineHeader(4, 0x1e000ff0, 1),
			bits:       0x1e000ff0,
			factorPOW:  1,
			h:          1,
			powLimit:   solverSyntheticPowLimit(256),
			startNonce: 1,
			iterations: 512,
		},
	}
}

func solverBaselineHeader(index int, bits uint32, nonce int32) *wire.MingingRightBlock {
	header := &wire.MingingRightBlock{
		Version:       uint32(0x20000 + index),
		Timestamp:     time.Unix(1700002000+int64(index*83), 0).UTC(),
		Bits:          bits,
		Nonce:         nonce,
		Connection:    []byte(fmt.Sprintf("192.0.2.%d:9788", index)),
		Collateral:    uint32(10 + index),
		MeanTPH:       uint32(4 + index),
		TphReports:    []uint32{uint32(3 + index), uint32(5 + index), uint32(7 + index)},
		ContractLimit: uint32(2000 + index),
	}
	fillSolverBaselineHash(&header.PrevBlock, byte(0x21+index))
	fillSolverBaselineHash(&header.BestBlock, byte(0x41+index))
	for i := range header.Miner {
		header.Miner[i] = byte(index*31 + i*9)
	}
	return header
}

func fillSolverBaselineHash(hash *chainhash.Hash, seed byte) {
	for i := range hash {
		hash[i] = seed + byte(i*17)
	}
}

func solverSyntheticPowLimit(bits uint) *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bits), big.NewInt(1))
}

func parseSolverUint32Hex(t *testing.T, fixtureName, field, value string) uint32 {
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

func parseSolverBigHex(t *testing.T, fixtureName, field, value string) *big.Int {
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
	return n
}

func decodeSolverHex(t *testing.T, fixtureName, field, value string) []byte {
	t.Helper()

	if strings.ToLower(value) != value {
		t.Fatalf("%s: %s must be lowercase hex", fixtureName, field)
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("%s: parse %s: %v", fixtureName, field, err)
	}
	if hex.EncodeToString(decoded) != value {
		t.Fatalf("%s: %s must be canonical lowercase hex", fixtureName, field)
	}
	return decoded
}
