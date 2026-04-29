package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeChainClient struct {
	minerBlocks map[int64]*MinerBlock
}

func newFakeChainClient() *fakeChainClient {
	return &fakeChainClient{
		minerBlocks: make(map[int64]*MinerBlock),
	}
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

func TestScanRouteABMetricsAndRewardMerge(t *testing.T) {
	client := fixtureClient()
	rewardReport := fixtureRewardReport()

	report := scanFixture(t, client, rewardReport, map[string]string{
		"addr-a": "alpha",
		"addr-b": "bravo",
		"addr-c": "charlie",
	})

	if report.ScannedMinerBlocksTotal != 4 {
		t.Fatalf("scanned_miner_blocks_total = %d, want 4", report.ScannedMinerBlocksTotal)
	}
	if report.ConnectionNonemptyBlocksTotal != 2 || report.ConnectionEmptyBlocksTotal != 2 {
		t.Fatalf("route counts nonempty/empty = %d/%d, want 2/2", report.ConnectionNonemptyBlocksTotal, report.ConnectionEmptyBlocksTotal)
	}
	if report.RouteAAcceptedShare != "0.50000000" || report.RouteBAcceptedShare != "0.50000000" {
		t.Fatalf("shares = %s/%s, want 0.50000000/0.50000000", report.RouteAAcceptedShare, report.RouteBAcceptedShare)
	}

	alpha := requireWorker(t, report, "addr-a")
	if alpha.PerWorkerAcceptedMRBlocks != 2 || alpha.PerWorkerRouteAAcceptedMRBlocks != 1 || alpha.PerWorkerRouteBAcceptedMRBlocks != 1 {
		t.Fatalf("alpha accepted/a/b = %d/%d/%d, want 2/1/1",
			alpha.PerWorkerAcceptedMRBlocks, alpha.PerWorkerRouteAAcceptedMRBlocks, alpha.PerWorkerRouteBAcceptedMRBlocks)
	}
	if alpha.PerWorkerAttributedRewardAtoms != 600000000 || alpha.PerWorkerAttributedReward != "6.00000000" {
		t.Fatalf("alpha reward = %d %s, want 600000000 6.00000000",
			alpha.PerWorkerAttributedRewardAtoms, alpha.PerWorkerAttributedReward)
	}

	bravo := requireWorker(t, report, "addr-b")
	if bravo.PerWorkerAcceptedMRBlocks != 1 || bravo.PerWorkerRouteBAcceptedMRBlocks != 1 {
		t.Fatalf("bravo accepted/b = %d/%d, want 1/1", bravo.PerWorkerAcceptedMRBlocks, bravo.PerWorkerRouteBAcceptedMRBlocks)
	}
	if bravo.PerWorkerAttributedRewardAtoms != 200000000 {
		t.Fatalf("bravo reward = %d, want 200000000", bravo.PerWorkerAttributedRewardAtoms)
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", report.Warnings)
	}
}

func TestScanRewardMetadataWindowMismatchFails(t *testing.T) {
	report, err := Scan(context.Background(), fixtureClient(), ScanConfig{
		StartMinerHeight: 10,
		EndMinerHeight:   14,
		RewardReport:     fixtureRewardReport(),
	})
	if err == nil {
		t.Fatal("expected window mismatch error")
	}
	if report != nil {
		t.Fatalf("report = %#v, want nil on hard window mismatch", report)
	}
	if !strings.Contains(err.Error(), "reward report miner height window 10-13 does not match route scan window 10-14") {
		t.Fatalf("error = %v, want window mismatch", err)
	}
}

func TestScanRewardMetadataPartialFails(t *testing.T) {
	rewardReport := fixtureRewardReport()
	rewardReport.Metadata.MinerEndHeight = nil
	report, err := Scan(context.Background(), fixtureClient(), ScanConfig{
		StartMinerHeight: 10,
		EndMinerHeight:   13,
		RewardReport:     rewardReport,
	})
	if err == nil {
		t.Fatal("expected incomplete metadata error")
	}
	if report != nil {
		t.Fatalf("report = %#v, want nil on incomplete metadata", report)
	}
	if !strings.Contains(err.Error(), "reward report miner height metadata incomplete") {
		t.Fatalf("error = %v, want incomplete metadata", err)
	}
}

func TestScanRewardMetadataAbsentWarns(t *testing.T) {
	rewardReport := fixtureRewardReport()
	rewardReport.Metadata = nil

	report := scanFixture(t, fixtureClient(), rewardReport, nil)
	if len(report.Warnings) != 1 {
		t.Fatalf("warnings = %#v, want one missing metadata warning", report.Warnings)
	}
	if !strings.Contains(report.Warnings[0], "reward report metadata missing") {
		t.Fatalf("warning = %q, want missing metadata warning", report.Warnings[0])
	}
}

func TestScanAcceptedMRBlocksMismatchWarns(t *testing.T) {
	rewardReport := fixtureRewardReport()
	rewardReport.Workers[2].AcceptedMRBlocks = 3

	report := scanFixture(t, fixtureClient(), rewardReport, nil)
	if len(report.Warnings) != 1 {
		t.Fatalf("warnings = %#v, want one accepted_mrblocks mismatch warning", report.Warnings)
	}
	if !strings.Contains(report.Warnings[0], "accepted_mrblocks mismatch for addr-a: reward report=3 route scan=2") {
		t.Fatalf("warning = %q, want accepted_mrblocks mismatch", report.Warnings[0])
	}
}

func TestScanCarriesUnmatchedReward(t *testing.T) {
	report := scanFixture(t, fixtureClient(), fixtureRewardReport(), nil)
	if report.UnmatchedRewardAtoms != 50000000 {
		t.Fatalf("unmatched_reward_atoms = %d, want 50000000", report.UnmatchedRewardAtoms)
	}
	if report.UnmatchedReward != "0.50000000" {
		t.Fatalf("unmatched_reward = %s, want 0.50000000", report.UnmatchedReward)
	}
}

func TestScanWorkerPresentInRewardReportOnly(t *testing.T) {
	report := scanFixture(t, fixtureClient(), fixtureRewardReport(), nil)
	delta := requireWorker(t, report, "addr-d")
	if delta.PerWorkerAcceptedMRBlocks != 0 {
		t.Fatalf("delta accepted = %d, want 0", delta.PerWorkerAcceptedMRBlocks)
	}
	if delta.PerWorkerAttributedRewardAtoms != 300000000 {
		t.Fatalf("delta reward = %d, want 300000000", delta.PerWorkerAttributedRewardAtoms)
	}
}

func TestScanWorkerPresentInMinerBlocksWithZeroReward(t *testing.T) {
	report := scanFixture(t, fixtureClient(), fixtureRewardReport(), nil)
	charlie := requireWorker(t, report, "addr-c")
	if charlie.PerWorkerAcceptedMRBlocks != 1 || charlie.PerWorkerRouteAAcceptedMRBlocks != 1 {
		t.Fatalf("charlie accepted/a = %d/%d, want 1/1", charlie.PerWorkerAcceptedMRBlocks, charlie.PerWorkerRouteAAcceptedMRBlocks)
	}
	if charlie.PerWorkerAttributedRewardAtoms != 0 || charlie.PerWorkerAttributedReward != "0.00000000" {
		t.Fatalf("charlie reward = %d %s, want zero", charlie.PerWorkerAttributedRewardAtoms, charlie.PerWorkerAttributedReward)
	}
}

func TestUnavailableRuntimeMetricsListed(t *testing.T) {
	report := scanFixture(t, fixtureClient(), fixtureRewardReport(), nil)
	got := make([]string, 0, len(report.UnavailableMetrics))
	for _, metric := range report.UnavailableMetrics {
		got = append(got, metric.Name)
		if !strings.Contains(metric.Reason, "runtime") {
			t.Fatalf("metric %s reason = %q, want runtime reason", metric.Name, metric.Reason)
		}
	}
	want := []string{
		"ip_minergap_reject_total",
		"addr_minergap_reject_total",
		"template_build_success_total",
		"template_build_failure_total",
		"miner_nonce_trials_total",
		"committee_dial_failure_total",
		"committee_participation_success_total",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unavailable metrics = %#v, want %#v", got, want)
	}
}

func TestDeterministicJSONOrdering(t *testing.T) {
	report := scanFixture(t, fixtureClient(), fixtureRewardReport(), map[string]string{
		"addr-c": "charlie",
		"addr-a": "alpha",
		"addr-b": "bravo",
		"addr-d": "delta",
	})
	gotOrder := make([]string, 0, len(report.Workers))
	for _, worker := range report.Workers {
		gotOrder = append(gotOrder, worker.Worker)
	}
	wantOrder := []string{"alpha", "bravo", "charlie", "delta"}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Fatalf("worker order = %#v, want %#v", gotOrder, wantOrder)
	}

	var first bytes.Buffer
	var second bytes.Buffer
	if err := writeJSON(&first, report); err != nil {
		t.Fatalf("write first json: %v", err)
	}
	if err := writeJSON(&second, report); err != nil {
		t.Fatalf("write second json: %v", err)
	}
	if first.String() != second.String() {
		t.Fatalf("JSON output is not deterministic\nfirst:\n%s\nsecond:\n%s", first.String(), second.String())
	}
	if !strings.Contains(first.String(), `"route_a_accepted_share": "0.50000000"`) {
		t.Fatalf("JSON output missing route share: %s", first.String())
	}
}

func TestPrometheusOutput(t *testing.T) {
	report := scanFixture(t, fixtureClient(), fixtureRewardReport(), map[string]string{"addr-a": "alpha"})
	var out bytes.Buffer
	if err := writePrometheus(&out, report); err != nil {
		t.Fatalf("write prometheus: %v", err)
	}
	text := out.String()
	for _, want := range []string{
		"connection_empty_blocks_total 2\n",
		"connection_nonempty_blocks_total 2\n",
		`per_worker_accepted_mrblocks{mining_address="addr-a",worker="alpha"} 2`,
		`per_worker_attributed_reward_atoms{mining_address="addr-a",worker="alpha"} 600000000`,
		`unavailable_metric_info{name="miner_nonce_trials_total",reason="requires runtime solver instrumentation"} 1`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("prometheus output missing %q\n%s", want, text)
		}
	}
}

func TestLoadRewardReport(t *testing.T) {
	path := writeTempFile(t, `{
  "metadata": {"miner_start_height": 10, "miner_end_height": 13},
  "workers": [
    {"worker":"alpha","mining_address":"addr-a","accepted_mrblocks":2,"reward_atoms":125000000,"reward_decimal":"1.25000000"}
  ],
  "unmatched_reward_atoms": 7
}`)
	report, err := loadRewardReport(path)
	if err != nil {
		t.Fatalf("load reward report: %v", err)
	}
	if len(report.Workers) != 1 || report.Workers[0].RewardAtoms != 125000000 || report.UnmatchedRewardAtoms != 7 {
		t.Fatalf("reward report = %#v", report)
	}
	if report.Metadata == nil || report.Metadata.MinerStartHeight == nil || *report.Metadata.MinerStartHeight != 10 {
		t.Fatalf("metadata = %#v, want miner_start_height 10", report.Metadata)
	}
	if report.Workers[0].AcceptedMRBlocks != 2 {
		t.Fatalf("accepted_mrblocks = %d, want 2", report.Workers[0].AcceptedMRBlocks)
	}
}

func TestRunRequiresRewardReport(t *testing.T) {
	var stderr bytes.Buffer
	err := run([]string{}, &bytes.Buffer{}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "--reward-report is required") {
		t.Fatalf("run error = %v, want missing reward-report", err)
	}
}

func TestRPCClientMinerBlockMethods(t *testing.T) {
	var sawHash bool
	var sawBlock bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "rpcuser" || pass != "rpcpass" {
			t.Fatalf("BasicAuth = %q/%q ok=%v, want rpcuser/rpcpass true", user, pass, ok)
		}

		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
			ID     interface{}       `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		var result interface{}
		switch req.Method {
		case "getminerblockhash":
			sawHash = true
			if len(req.Params) != 1 || string(req.Params[0]) != "12" {
				t.Fatalf("getminerblockhash params = %s, want [12]", rawParams(req.Params))
			}
			result = "mrhash-012"
		case "getminerblock":
			sawBlock = true
			if len(req.Params) != 2 || string(req.Params[0]) != `"mrhash-012"` || string(req.Params[1]) != "true" {
				t.Fatalf("getminerblock params = %s, want [mrhash-012,true]", rawParams(req.Params))
			}
			result = MinerBlock{
				Hash:       "mrhash-012",
				Height:     12,
				Address:    "addr-a",
				Connection: "203.0.113.10:9788",
				Best:       "txhash-112",
			}
		default:
			t.Fatalf("unexpected method %s", req.Method)
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"result": result,
			"error":  nil,
			"id":     req.ID,
		})
	}))
	defer server.Close()

	client := NewRPCClient(server.URL, "rpcuser", "rpcpass", time.Second)
	hash, err := client.GetMinerBlockHash(context.Background(), 12)
	if err != nil {
		t.Fatalf("GetMinerBlockHash: %v", err)
	}
	if hash != "mrhash-012" {
		t.Fatalf("hash = %q, want mrhash-012", hash)
	}
	block, err := client.GetMinerBlock(context.Background(), hash)
	if err != nil {
		t.Fatalf("GetMinerBlock: %v", err)
	}
	if block.Hash != "mrhash-012" || block.Address != "addr-a" || block.Connection != "203.0.113.10:9788" {
		t.Fatalf("decoded block = %#v", block)
	}
	if !sawHash || !sawBlock {
		t.Fatalf("sawHash=%v sawBlock=%v, want both true", sawHash, sawBlock)
	}
}

func scanFixture(t *testing.T, client *fakeChainClient, rewardReport *RewardAttributionReport, workers map[string]string) *Report {
	t.Helper()
	report, err := Scan(context.Background(), client, ScanConfig{
		StartMinerHeight: minMinerHeight(client.minerBlocks),
		EndMinerHeight:   maxMinerHeight(client.minerBlocks),
		Workers:          workers,
		RewardReport:     rewardReport,
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	return report
}

func requireWorker(t *testing.T, report *Report, address string) WorkerMetrics {
	t.Helper()
	for _, worker := range report.Workers {
		if worker.MiningAddress == address {
			return worker
		}
	}
	t.Fatalf("missing worker %s in %#v", address, report.Workers)
	return WorkerMetrics{}
}

func fixtureClient() *fakeChainClient {
	client := newFakeChainClient()
	client.minerBlocks[10] = minerBlock(10, "addr-a", "203.0.113.10:9788")
	client.minerBlocks[11] = minerBlock(11, "addr-a", "")
	client.minerBlocks[12] = minerBlock(12, "addr-b", "")
	client.minerBlocks[13] = minerBlock(13, "addr-c", "203.0.113.11:9788")
	return client
}

func fixtureRewardReport() *RewardAttributionReport {
	return &RewardAttributionReport{
		Metadata: &RewardMetadata{
			MinerStartHeight: int64Ptr(10),
			MinerEndHeight:   int64Ptr(13),
		},
		Workers: []RewardWorker{
			{Worker: "delta", MiningAddress: "addr-d", AcceptedMRBlocks: 0, RewardAtoms: 300000000, RewardDecimal: "3.00000000"},
			{Worker: "bravo", MiningAddress: "addr-b", AcceptedMRBlocks: 1, RewardAtoms: 200000000, RewardDecimal: "2.00000000"},
			{Worker: "alpha", MiningAddress: "addr-a", AcceptedMRBlocks: 2, RewardAtoms: 600000000, RewardDecimal: "6.00000000"},
		},
		UnmatchedRewardAtoms:   50000000,
		UnmatchedRewardDecimal: "0.50000000",
	}
}

func minerBlock(height int64, address, connection string) *MinerBlock {
	return &MinerBlock{
		Hash:       fmt.Sprintf("mrhash-%03d", height),
		Height:     height,
		Address:    address,
		Connection: connection,
		Best:       fmt.Sprintf("txhash-%03d", 100+height),
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

func int64Ptr(v int64) *int64 {
	return &v
}

func rawParams(params []json.RawMessage) string {
	raw := make([]string, 0, len(params))
	for _, param := range params {
		raw = append(raw, string(param))
	}
	return "[" + strings.Join(raw, ",") + "]"
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

func TestRewardReportJSONShapeCompatibility(t *testing.T) {
	data, err := json.Marshal(fixtureRewardReport())
	if err != nil {
		t.Fatalf("marshal fixture reward report: %v", err)
	}
	var decoded RewardAttributionReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode fixture reward report: %v", err)
	}
	if len(decoded.Workers) != 3 || decoded.Workers[0].MiningAddress != "addr-d" {
		t.Fatalf("decoded report = %#v", decoded)
	}
	if decoded.Metadata == nil || decoded.Metadata.MinerEndHeight == nil || *decoded.Metadata.MinerEndHeight != 13 {
		t.Fatalf("decoded metadata = %#v, want miner_end_height 13", decoded.Metadata)
	}
	if decoded.Workers[2].AcceptedMRBlocks != 2 {
		t.Fatalf("decoded accepted_mrblocks = %d, want 2", decoded.Workers[2].AcceptedMRBlocks)
	}
}
