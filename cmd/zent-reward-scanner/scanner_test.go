package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"btcd/wire/common"
)

type fakeChainClient struct {
	txBlocks    map[int64]*TxBlock
	minerBlocks map[int64]*MinerBlock
}

func newFakeChainClient() *fakeChainClient {
	return &fakeChainClient{
		txBlocks:    make(map[int64]*TxBlock),
		minerBlocks: make(map[int64]*MinerBlock),
	}
}

func (f *fakeChainClient) GetBlockCount(ctx context.Context) (int64, error) {
	return maxTxHeight(f.txBlocks), nil
}

func (f *fakeChainClient) GetBlockHash(ctx context.Context, height int64) (string, error) {
	if _, ok := f.txBlocks[height]; !ok {
		return "", fmt.Errorf("missing tx block %d", height)
	}
	return fmt.Sprintf("txhash-%03d", height), nil
}

func (f *fakeChainClient) GetBlock(ctx context.Context, hash string) (*TxBlock, error) {
	var height int64
	if _, err := fmt.Sscanf(hash, "txhash-%03d", &height); err != nil {
		return nil, err
	}
	return f.txBlocks[height], nil
}

func (f *fakeChainClient) GetMinerBlockCount(ctx context.Context) (int64, error) {
	return maxMinerHeight(f.minerBlocks), nil
}

func (f *fakeChainClient) GetMinerBlockHash(ctx context.Context, height int64) (string, error) {
	if _, ok := f.minerBlocks[height]; !ok {
		return "", fmt.Errorf("missing miner block %d", height)
	}
	return fmt.Sprintf("mrhash-%03d", height), nil
}

func (f *fakeChainClient) GetMinerBlock(ctx context.Context, hash string) (*MinerBlock, error) {
	var height int64
	if _, err := fmt.Sscanf(hash, "mrhash-%03d", &height); err != nil {
		return nil, err
	}
	return f.minerBlocks[height], nil
}

func TestScanOneWorkerOneReward(t *testing.T) {
	client := newFakeChainClient()
	client.minerBlocks[10] = minerBlock(10, "addr-a")
	client.txBlocks[100] = txBlock(100, rewardVout(0, "addr-a", "6.25000000"))

	report := scanFixture(t, client, map[string]string{"addr-a": "worker-a"})
	worker := requireWorker(t, report, "addr-a")
	if worker.AcceptedMRBlocks != 1 {
		t.Fatalf("accepted_mrblocks = %d, want 1", worker.AcceptedMRBlocks)
	}
	if worker.RewardAtoms != 625000000 {
		t.Fatalf("reward_atoms = %d, want 625000000", worker.RewardAtoms)
	}
	if report.TotalRewardAtoms != 625000000 {
		t.Fatalf("total_reward_atoms = %d, want 625000000", report.TotalRewardAtoms)
	}
	if report.AttributedRewardAtoms != 625000000 {
		t.Fatalf("attributed_reward_atoms = %d, want 625000000", report.AttributedRewardAtoms)
	}
	if len(report.UnmatchedRewards) != 0 {
		t.Fatalf("unmatched rewards = %#v, want none", report.UnmatchedRewards)
	}
}

func TestScanTwoWorkersMultipleMinerBlocks(t *testing.T) {
	client := newFakeChainClient()
	client.minerBlocks[10] = minerBlock(10, "addr-a")
	client.minerBlocks[11] = minerBlock(11, "addr-b")
	client.minerBlocks[12] = minerBlock(12, "addr-a")
	client.txBlocks[100] = txBlock(100,
		rewardVout(0, "addr-a", "1.00000000"),
		rewardVout(1, "addr-b", "2.00000000"),
	)

	report := scanFixture(t, client, map[string]string{"addr-a": "worker-a", "addr-b": "worker-b"})
	a := requireWorker(t, report, "addr-a")
	b := requireWorker(t, report, "addr-b")
	if a.AcceptedMRBlocks != 2 || b.AcceptedMRBlocks != 1 {
		t.Fatalf("accepted counts = %d/%d, want 2/1", a.AcceptedMRBlocks, b.AcceptedMRBlocks)
	}
	if a.RewardAtoms != 100000000 || b.RewardAtoms != 200000000 {
		t.Fatalf("reward atoms = %d/%d, want 100000000/200000000", a.RewardAtoms, b.RewardAtoms)
	}
}

func TestScanMultipleRewardOutputsToSameAddress(t *testing.T) {
	client := newFakeChainClient()
	client.minerBlocks[10] = minerBlock(10, "addr-a")
	client.txBlocks[100] = txBlock(100,
		rewardVout(0, "addr-a", "1.25000000"),
		rewardVout(1, "addr-a", "0.75000000"),
	)

	report := scanFixture(t, client, nil)
	worker := requireWorker(t, report, "addr-a")
	if worker.RewardAtoms != 200000000 {
		t.Fatalf("reward_atoms = %d, want 200000000", worker.RewardAtoms)
	}
	if len(worker.RewardEvents) != 2 {
		t.Fatalf("reward events = %d, want 2", len(worker.RewardEvents))
	}
}

func TestScanUnmatchedRewardOutput(t *testing.T) {
	client := newFakeChainClient()
	client.minerBlocks[10] = minerBlock(10, "addr-a")
	client.txBlocks[100] = txBlock(100, rewardVout(0, "addr-x", "3.00000000"))

	report := scanFixture(t, client, nil)
	if report.TotalRewardAtoms != 300000000 {
		t.Fatalf("total_reward_atoms = %d, want 300000000", report.TotalRewardAtoms)
	}
	if report.AttributedRewardAtoms != 0 {
		t.Fatalf("attributed_reward_atoms = %d, want 0", report.AttributedRewardAtoms)
	}
	if report.UnmatchedRewardAtoms != 300000000 {
		t.Fatalf("unmatched_reward_atoms = %d, want 300000000", report.UnmatchedRewardAtoms)
	}
	if len(report.UnmatchedRewards) != 1 {
		t.Fatalf("unmatched rewards = %d, want 1", len(report.UnmatchedRewards))
	}
	if report.UnmatchedRewards[0].RewardAtoms != 300000000 {
		t.Fatalf("unmatched reward_atoms = %d, want 300000000", report.UnmatchedRewards[0].RewardAtoms)
	}
	if !strings.Contains(report.UnmatchedRewards[0].Reason, "did not match") {
		t.Fatalf("unmatched reason = %q, want unknown address reason", report.UnmatchedRewards[0].Reason)
	}
}

func TestScanUnmatchedRewardOutputWithoutAddresses(t *testing.T) {
	client := newFakeChainClient()
	client.minerBlocks[10] = minerBlock(10, "addr-a")
	vout := rewardVout(0, "", "4.00000000")
	vout.ScriptPubKey.Addresses = nil
	client.txBlocks[100] = txBlock(100, vout)

	report := scanFixture(t, client, map[string]string{"addr-a": "worker-a"})
	requireUnmatchedTotals(t, report, 400000000)
	if !strings.Contains(report.UnmatchedRewards[0].Reason, "no addresses") {
		t.Fatalf("unmatched reason = %q, want no known address reason", report.UnmatchedRewards[0].Reason)
	}
}

func TestScanUnmatchedRewardOutputWithMultipleKnownAddresses(t *testing.T) {
	client := newFakeChainClient()
	client.minerBlocks[10] = minerBlock(10, "addr-a")
	client.minerBlocks[11] = minerBlock(11, "addr-b")
	vout := rewardVout(0, "addr-a", "7.00000000")
	vout.ScriptPubKey.Addresses = []string{"addr-b", "addr-a"}
	client.txBlocks[100] = txBlock(100, vout)

	report := scanFixture(t, client, map[string]string{"addr-a": "worker-a", "addr-b": "worker-b"})
	requireUnmatchedTotals(t, report, 700000000)
	if !strings.Contains(report.UnmatchedRewards[0].Reason, "multiple output addresses") {
		t.Fatalf("unmatched reason = %q, want multiple-address ambiguity reason", report.UnmatchedRewards[0].Reason)
	}
}

func TestScanUnmatchedRewardOutputWithOneKnownAndOneUnknownAddress(t *testing.T) {
	client := newFakeChainClient()
	client.minerBlocks[10] = minerBlock(10, "addr-a")
	vout := rewardVout(0, "addr-a", "8.00000000")
	vout.ScriptPubKey.Addresses = []string{"addr-a", "addr-x"}
	client.txBlocks[100] = txBlock(100, vout)

	report := scanFixture(t, client, map[string]string{"addr-a": "worker-a"})
	requireUnmatchedTotals(t, report, 800000000)
	worker := requireWorker(t, report, "addr-a")
	if worker.RewardAtoms != 0 {
		t.Fatalf("worker reward_atoms = %d, want 0", worker.RewardAtoms)
	}
	if !strings.Contains(report.UnmatchedRewards[0].Reason, "multiple output addresses") {
		t.Fatalf("unmatched reason = %q, want multiple-address ambiguity reason", report.UnmatchedRewards[0].Reason)
	}
}

func TestScanZeroRewardWindow(t *testing.T) {
	client := newFakeChainClient()
	client.minerBlocks[10] = minerBlock(10, "addr-a")
	client.txBlocks[100] = txBlock(100)

	report := scanFixture(t, client, nil)
	worker := requireWorker(t, report, "addr-a")
	if worker.AcceptedMRBlocks != 1 {
		t.Fatalf("accepted_mrblocks = %d, want 1", worker.AcceptedMRBlocks)
	}
	if worker.RewardAtoms != 0 || report.TotalRewardAtoms != 0 {
		t.Fatalf("reward atoms = %d total = %d, want zero", worker.RewardAtoms, report.TotalRewardAtoms)
	}
}

func TestScanMalformedMissingRPCFields(t *testing.T) {
	client := newFakeChainClient()
	client.minerBlocks[10] = &MinerBlock{Hash: "mrhash-010", Height: 10}
	client.txBlocks[100] = &TxBlock{Hash: "txhash-100", Height: 100}

	report := scanFixture(t, client, nil)
	if len(report.Workers) != 0 {
		t.Fatalf("workers = %#v, want none", report.Workers)
	}
	if len(report.Metadata.Warnings) != 2 {
		t.Fatalf("warnings = %#v, want 2 warnings", report.Metadata.Warnings)
	}
}

func TestScanObservedAttributionDoesNotApplyForecastMultiplier(t *testing.T) {
	client := newFakeChainClient()
	client.minerBlocks[10] = minerBlock(10, "addr-a")
	client.txBlocks[100] = txBlock(100, rewardVout(0, "addr-a", "10.00000000"))

	report := scanFixture(t, client, map[string]string{"addr-a": "forecast-h1-2x-stale-0.5"})
	worker := requireWorker(t, report, "addr-a")
	if worker.RewardAtoms != 1000000000 {
		t.Fatalf("reward_atoms = %d, want exact observed 1000000000 without multipliers", worker.RewardAtoms)
	}
}

func TestRunAgainstHTTPJSONRPCServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
			ID     interface{}       `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		result := rpcFixtureResult(t, req.Method, req.Params)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"result": result,
			"error":  nil,
			"id":     req.ID,
		})
	}))
	defer server.Close()

	workersFile := writeTempFile(t, `{"addr-a":"worker-a"}`)
	var stdout bytes.Buffer
	err := run([]string{
		"--rpc-url", server.URL,
		"--workers-file", workersFile,
		"--start-height", "100",
		"--end-height", "100",
		"--start-miner-height", "10",
		"--end-miner-height", "10",
		"--format", "json",
	}, &stdout, io.Discard)
	if err != nil {
		t.Fatalf("run scanner: %v", err)
	}

	var report Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, stdout.String())
	}
	worker := requireWorker(t, &report, "addr-a")
	if worker.Worker != "worker-a" || worker.AcceptedMRBlocks != 1 || worker.RewardAtoms != 500000000 {
		t.Fatalf("worker report = %#v", worker)
	}
}

func TestWriteCSV(t *testing.T) {
	report := &Report{
		Workers: []WorkerReport{{
			Worker:           "worker-a",
			MiningAddress:    "addr-a",
			AcceptedMRBlocks: 1,
			RewardAtoms:      125000000,
			RewardDecimal:    "1.25000000",
			FirstHeight:      int64Ptr(10),
			LastHeight:       int64Ptr(100),
		}},
	}
	var buf bytes.Buffer
	if err := writeCSV(&buf, report); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	want := "worker,mining_address,accepted_mrblocks,reward_atoms,reward_decimal,first_height,last_height\nworker-a,addr-a,1,125000000,1.25000000,10,100\n"
	if got := buf.String(); got != want {
		t.Fatalf("csv output:\n%s\nwant:\n%s", got, want)
	}
}

func TestLoadWorkersFileCSV(t *testing.T) {
	path := writeTempFile(t, "mining_address,worker\naddr-a,worker-a\naddr-b,worker-b\n")
	got, err := loadWorkersFile(path)
	if err != nil {
		t.Fatalf("load workers: %v", err)
	}
	want := map[string]string{"addr-a": "worker-a", "addr-b": "worker-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("workers = %#v, want %#v", got, want)
	}
}

func TestParseDecimalAtomsExact(t *testing.T) {
	tests := map[string]int64{
		"0":          0,
		"0.00000001": 1,
		"1.23456789": 123456789,
		"6.25":       625000000,
		"1e-8":       1,
		"1.25e0":     125000000,
	}
	for input, want := range tests {
		got, err := parseDecimalAtoms(input)
		if err != nil {
			t.Fatalf("%s: parse: %v", input, err)
		}
		if got != want {
			t.Fatalf("%s: atoms = %d, want %d", input, got, want)
		}
	}
	if _, err := parseDecimalAtoms("1.000000001"); err == nil {
		t.Fatal("expected more-than-8-decimal precision error")
	}
}

func TestParseRewardAtomsExactExponentJSON(t *testing.T) {
	tests := map[string]int64{
		`{"Val":1e-8}`:     1,
		`{"Val":1.25e0}`:   125000000,
		`{"Val":"1e-8"}`:   1,
		`{"Val":"6.25"}`:   625000000,
		`{"Val":0.000000}`: 0,
	}
	for input, want := range tests {
		got, err := parseRewardAtoms(json.RawMessage(input))
		if err != nil {
			t.Fatalf("%s: parse: %v", input, err)
		}
		if got != want {
			t.Fatalf("%s: atoms = %d, want %d", input, got, want)
		}
	}

	bad := []string{
		`{"Val":1.234567891}`,
		`{"Val":"1.234567891"}`,
		`{"Val":"1e-"}`,
		`{"Val":"not-a-number"}`,
		`{"Val":true}`,
	}
	for _, input := range bad {
		if _, err := parseRewardAtoms(json.RawMessage(input)); err == nil {
			t.Fatalf("%s: expected parse error", input)
		}
	}
}

func scanFixture(t *testing.T, client *fakeChainClient, workers map[string]string) *Report {
	t.Helper()
	report, err := Scan(context.Background(), client, ScanConfig{
		StartHeight:      minTxHeight(client.txBlocks),
		EndHeight:        maxTxHeight(client.txBlocks),
		StartMinerHeight: minMinerHeight(client.minerBlocks),
		EndMinerHeight:   maxMinerHeight(client.minerBlocks),
		Workers:          workers,
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	return report
}

func requireWorker(t *testing.T, report *Report, address string) WorkerReport {
	t.Helper()
	for _, worker := range report.Workers {
		if worker.MiningAddress == address {
			return worker
		}
	}
	t.Fatalf("missing worker %s in %#v", address, report.Workers)
	return WorkerReport{}
}

func requireUnmatchedTotals(t *testing.T, report *Report, atoms int64) {
	t.Helper()
	if len(report.UnmatchedRewards) != 1 {
		t.Fatalf("unmatched rewards = %d, want 1", len(report.UnmatchedRewards))
	}
	if report.TotalRewardAtoms != atoms {
		t.Fatalf("total_reward_atoms = %d, want %d", report.TotalRewardAtoms, atoms)
	}
	if report.UnmatchedRewardAtoms != atoms {
		t.Fatalf("unmatched_reward_atoms = %d, want %d", report.UnmatchedRewardAtoms, atoms)
	}
	if report.AttributedRewardAtoms != 0 {
		t.Fatalf("attributed_reward_atoms = %d, want 0", report.AttributedRewardAtoms)
	}
	if report.UnmatchedRewards[0].RewardAtoms != atoms {
		t.Fatalf("unmatched reward_atoms = %d, want %d", report.UnmatchedRewards[0].RewardAtoms, atoms)
	}
}

func txBlock(height int64, vouts ...Vout) *TxBlock {
	return &TxBlock{
		Hash:   fmt.Sprintf("txhash-%03d", height),
		Height: height,
		RawTx: []TxRaw{{
			TxID: fmt.Sprintf("coinbase-%03d", height),
			Vout: vouts,
		}},
	}
}

func minerBlock(height int64, address string) *MinerBlock {
	return &MinerBlock{
		Hash:    fmt.Sprintf("mrhash-%03d", height),
		Height:  height,
		Address: address,
		Best:    fmt.Sprintf("txhash-%03d", 100+height),
	}
}

func rewardVout(n uint32, address, amount string) Vout {
	addresses := []string{address}
	if address == "" {
		addresses = nil
	}
	return Vout{
		TokenType: common.FeeCoinTyp,
		Value:     json.RawMessage(fmt.Sprintf(`{"Val":%q}`, amount)),
		N:         n,
		ScriptPubKey: ScriptPubKey{
			Addresses: addresses,
			Type:      "pubkeyhash",
		},
	}
}

func rpcFixtureResult(t *testing.T, method string, params []json.RawMessage) interface{} {
	t.Helper()
	switch method {
	case "getblockhash":
		return "txhash-100"
	case "getblock":
		return txBlock(100, rewardVout(0, "addr-a", "5.00000000"))
	case "getminerblockhash":
		return "mrhash-010"
	case "getminerblock":
		return minerBlock(10, "addr-a")
	case "getblockcount":
		return 100
	case "getminerblockcount":
		return 10
	default:
		t.Fatalf("unexpected method %s", method)
		return nil
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "fixture-*")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := file.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}
	return file.Name()
}

func minTxHeight(m map[int64]*TxBlock) int64 {
	if len(m) == 0 {
		return 0
	}
	first := true
	var min int64
	for h := range m {
		if first || h < min {
			min = h
			first = false
		}
	}
	return min
}

func maxTxHeight(m map[int64]*TxBlock) int64 {
	var max int64
	for h := range m {
		if h > max {
			max = h
		}
	}
	return max
}

func minMinerHeight(m map[int64]*MinerBlock) int64 {
	if len(m) == 0 {
		return 0
	}
	first := true
	var min int64
	for h := range m {
		if first || h < min {
			min = h
			first = false
		}
	}
	return min
}

func maxMinerHeight(m map[int64]*MinerBlock) int64 {
	var max int64
	for h := range m {
		if h > max {
			max = h
		}
	}
	return max
}

func int64Ptr(v int64) *int64 {
	return &v
}
