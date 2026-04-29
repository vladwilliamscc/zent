package wire

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/omegasuite/btcd/chaincfg/chainhash"
)

const blockHashGoldenPath = "testdata/blockhash_golden.json"

var updateBlockHashGolden = flag.Bool("update_blockhash_golden", false, "rewrite BlockHash benchmark golden vectors")

var blockHashBenchSink chainhash.Hash

type blockHashGoldenFixture struct {
	Name          string `json:"name"`
	SerializedHex string `json:"serialized_hex"`
	BlockHashHex  string `json:"block_hash_hex"`
}

type namedMinerBlockHeader struct {
	name   string
	header *MingingRightBlock
}

func TestBlockHeaderBlockHashMatchesSerialization(t *testing.T) {
	header := blockHashBenchmarkHeader()
	for _, nonce := range []int32{0, 1, 0x7fffffff, -1} {
		header.Nonce = nonce

		var serialized bytes.Buffer
		if err := header.Serialize(&serialized); err != nil {
			t.Fatalf("serialize header nonce %d: %v", nonce, err)
		}

		wantHash := chainhash.DoubleHashH(serialized.Bytes())
		if gotHash := header.BlockHash(); gotHash != wantHash {
			t.Fatalf("nonce %d: BlockHash = %x, want %x", nonce, gotHash, wantHash)
		}
	}
}

func TestMingingRightBlockHashSerializationGolden(t *testing.T) {
	if *updateBlockHashGolden {
		writeBlockHashGolden(t, buildBlockHashGoldenFixtures(t))
	}

	fixtures := loadBlockHashGoldenFixtures(t)
	headers := minerBlockHashBenchmarkHeaders()
	if len(fixtures) != len(headers) {
		t.Fatalf("fixture count = %d, want %d", len(fixtures), len(headers))
	}

	byName := make(map[string]*MingingRightBlock, len(headers))
	for _, item := range headers {
		byName[item.name] = item.header
	}

	for _, fixture := range fixtures {
		header := byName[fixture.Name]
		if header == nil {
			t.Fatalf("unknown golden fixture %q", fixture.Name)
		}

		var serialized bytes.Buffer
		if err := header.Serialize(&serialized); err != nil {
			t.Fatalf("%s: serialize header: %v", fixture.Name, err)
		}
		if got := hex.EncodeToString(serialized.Bytes()); got != fixture.SerializedHex {
			t.Fatalf("%s: serialized_hex = %s, want %s", fixture.Name, got, fixture.SerializedHex)
		}

		gotHash := header.BlockHash()
		if got := hex.EncodeToString(gotHash[:]); got != fixture.BlockHashHex {
			t.Fatalf("%s: block_hash_hex = %s, want %s", fixture.Name, got, fixture.BlockHashHex)
		}

		var decoded MingingRightBlock
		if err := decoded.Deserialize(bytes.NewReader(serialized.Bytes())); err != nil {
			t.Fatalf("%s: deserialize serialized header: %v", fixture.Name, err)
		}
		var roundTrip bytes.Buffer
		if err := decoded.Serialize(&roundTrip); err != nil {
			t.Fatalf("%s: serialize decoded header: %v", fixture.Name, err)
		}
		if !bytes.Equal(roundTrip.Bytes(), serialized.Bytes()) {
			t.Fatalf("%s: serialization round trip changed bytes", fixture.Name)
		}
		if decodedHash := decoded.BlockHash(); decodedHash != gotHash {
			t.Fatalf("%s: decoded header hash changed: got %x want %x", fixture.Name, decodedHash, gotHash)
		}
	}
}

func TestMingingRightBlockHashMatchesSerializationEdgeCases(t *testing.T) {
	header := minerBlockHashBenchmarkHeader(4, true, true, true)
	header.Collateral = 0xffffffff
	header.MeanTPH = 0x10000
	header.TphReports = []uint32{0xfc, 0xfd, 0xffff, 0x10000, 0xffffffff}

	var mrBlock chainhash.Hash
	fillBenchmarkHash(&mrBlock, 0xe0)
	header.ViolationReport = []*Violations{{
		Height:  0x7fffffff,
		MRBlock: mrBlock,
		Blocks:  make([]chainhash.Hash, 253),
	}}
	for i := range header.ViolationReport[0].Blocks {
		fillBenchmarkHash(&header.ViolationReport[0].Blocks[i], byte(i))
	}

	header.Instructions = make([]*Instruction, 253)
	for i := range header.Instructions {
		header.Instructions[i] = &Instruction{
			InstCode: InstructionCode(1 + byte(i%2)),
			InstData: []byte{byte(i), byte(i >> 8)},
		}
	}

	assertMinerBlockHashMatchesSerialization(t, "varint-boundary-large-header", header)
}

func BenchmarkBlockHeaderBlockHash(b *testing.B) {
	header := blockHashBenchmarkHeader()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		header.Nonce = int32(i)
		blockHashBenchSink = header.BlockHash()
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "H/s")
}

func BenchmarkMingingRightBlockBlockHash(b *testing.B) {
	for _, item := range minerBlockHashBenchmarkHeaders() {
		header := *item.header
		b.Run(item.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				header.Nonce = int32(i & 0x7fffffff)
				blockHashBenchSink = header.BlockHash()
			}
			b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "H/s")
		})
	}
}

func assertMinerBlockHashMatchesSerialization(t *testing.T, name string, header *MingingRightBlock) {
	t.Helper()

	var serialized bytes.Buffer
	if err := header.Serialize(&serialized); err != nil {
		t.Fatalf("%s: serialize header: %v", name, err)
	}
	wantHash := chainhash.DoubleHashH(serialized.Bytes())
	if gotHash := header.BlockHash(); gotHash != wantHash {
		t.Fatalf("%s: BlockHash = %x, want %x", name, gotHash, wantHash)
	}
}

func loadBlockHashGoldenFixtures(t *testing.T) []blockHashGoldenFixture {
	t.Helper()

	data, err := os.ReadFile(blockHashGoldenPath)
	if err != nil {
		t.Fatalf("read %s: %v", blockHashGoldenPath, err)
	}

	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode top-level golden array: %v", err)
	}
	fixtures := make([]blockHashGoldenFixture, len(raw))
	for i, fields := range raw {
		validateBlockHashGoldenShape(t, i, fields)
		encoded, err := json.Marshal(fields)
		if err != nil {
			t.Fatalf("fixture %d: marshal field map: %v", i, err)
		}
		if err := json.Unmarshal(encoded, &fixtures[i]); err != nil {
			t.Fatalf("fixture %d: decode fixture: %v", i, err)
		}
		requireLowerHex(t, fixtures[i].Name, "serialized_hex", fixtures[i].SerializedHex)
		requireLowerHex(t, fixtures[i].Name, "block_hash_hex", fixtures[i].BlockHashHex)
		if len(fixtures[i].BlockHashHex) != chainhash.HashSize*2 {
			t.Fatalf("%s: block_hash_hex length = %d, want %d", fixtures[i].Name, len(fixtures[i].BlockHashHex), chainhash.HashSize*2)
		}
	}
	return fixtures
}

func validateBlockHashGoldenShape(t *testing.T, index int, fields map[string]json.RawMessage) {
	t.Helper()

	wantFields := []string{"name", "serialized_hex", "block_hash_hex"}
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

func requireLowerHex(t *testing.T, fixtureName, field, value string) {
	t.Helper()

	if value == "" {
		t.Fatalf("%s: %s is empty", fixtureName, field)
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("%s: decode %s: %v", fixtureName, field, err)
	}
	if hex.EncodeToString(decoded) != value {
		t.Fatalf("%s: %s must be canonical lowercase hex", fixtureName, field)
	}
}

func writeBlockHashGolden(t *testing.T, fixtures []blockHashGoldenFixture) {
	t.Helper()

	data, err := json.MarshalIndent(fixtures, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden fixtures: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(blockHashGoldenPath, data, 0644); err != nil {
		t.Fatalf("write %s: %v", blockHashGoldenPath, err)
	}
}

func buildBlockHashGoldenFixtures(t *testing.T) []blockHashGoldenFixture {
	t.Helper()

	headers := minerBlockHashBenchmarkHeaders()
	fixtures := make([]blockHashGoldenFixture, 0, len(headers))
	for _, item := range headers {
		var serialized bytes.Buffer
		if err := item.header.Serialize(&serialized); err != nil {
			t.Fatalf("%s: serialize generated header: %v", item.name, err)
		}
		hash := item.header.BlockHash()
		fixtures = append(fixtures, blockHashGoldenFixture{
			Name:          item.name,
			SerializedHex: hex.EncodeToString(serialized.Bytes()),
			BlockHashHex:  hex.EncodeToString(hash[:]),
		})
	}
	return fixtures
}

func TestBlockHashGeneratedFixtureBuilderMatchesGolden(t *testing.T) {
	golden := loadBlockHashGoldenFixtures(t)
	generated := buildBlockHashGoldenFixtures(t)
	if !reflect.DeepEqual(golden, generated) {
		t.Fatalf("static BlockHash golden file does not match deterministic fixture builder")
	}
}

func minerBlockHashBenchmarkHeaders() []namedMinerBlockHeader {
	return []namedMinerBlockHeader{
		{name: "miner-minimal", header: minerBlockHashBenchmarkHeader(1, false, false, false)},
		{name: "miner-with-utxo", header: minerBlockHashBenchmarkHeader(2, true, false, false)},
		{name: "miner-full", header: minerBlockHashBenchmarkHeader(3, true, true, true)},
	}
}

func blockHashBenchmarkHeader() *BlockHeader {
	header := &BlockHeader{
		Version:      0x20003,
		Timestamp:    time.Unix(1700000911, 0).UTC(),
		ContractExec: 610,
		Nonce:        1,
	}
	fillBenchmarkHash(&header.PrevBlock, 0x2b)
	fillBenchmarkHash(&header.MerkleRoot, 0x51)
	return header
}

func minerBlockHashBenchmarkHeader(index int, includeUtxo, includeReports, includeInstructions bool) *MingingRightBlock {
	header := &MingingRightBlock{
		Version:       uint32(0x20000 + index),
		Timestamp:     time.Unix(1700001000+int64(index*47), 0).UTC(),
		Bits:          0x1f0fffff + uint32(index),
		Nonce:         int32(index),
		Connection:    []byte("203.0.113.10:9788"),
		Collateral:    uint32(100 + index),
		MeanTPH:       uint32(8 + index),
		TphReports:    []uint32{},
		ContractLimit: uint32(1000 + index),
	}
	fillBenchmarkHash(&header.PrevBlock, byte(0x10+index))
	fillBenchmarkHash(&header.BestBlock, byte(0x30+index))
	for i := range header.Miner {
		header.Miner[i] = byte(index*19 + i*7)
	}

	if includeUtxo {
		var outpointHash chainhash.Hash
		fillBenchmarkHash(&outpointHash, byte(0x60+index))
		header.Utxos = NewOutPoint(&outpointHash, uint32(7+index))
	}
	if includeReports {
		var reportBlock chainhash.Hash
		var signedA chainhash.Hash
		var signedB chainhash.Hash
		fillBenchmarkHash(&reportBlock, byte(0x80+index))
		fillBenchmarkHash(&signedA, byte(0xa0+index))
		fillBenchmarkHash(&signedB, byte(0xc0+index))
		header.ViolationReport = []*Violations{{
			Height:  int32(400 + index),
			MRBlock: reportBlock,
			Blocks:  []chainhash.Hash{signedA, signedB},
		}}
		header.TphReports = []uint32{11, 13, 17, 19}
	}
	if includeInstructions {
		header.Instructions = []*Instruction{
			{InstCode: AddDns, InstData: []byte("seed-a.example.invalid")},
			{InstCode: AddChain, InstData: []byte{0x02, 0x04, 0x06, 0x08}},
		}
	}

	return header
}

func fillBenchmarkHash(hash *chainhash.Hash, seed byte) {
	for i := range hash {
		hash[i] = seed + byte(i*11)
	}
}
