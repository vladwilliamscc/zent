package main

import "context"

const (
	routeA = "route_a"
	routeB = "route_b"
)

var unavailableRuntimeMetrics = []UnavailableMetric{
	{Name: "ip_minergap_reject_total", Reason: "requires runtime accept-path instrumentation"},
	{Name: "addr_minergap_reject_total", Reason: "requires runtime accept-path instrumentation"},
	{Name: "template_build_success_total", Reason: "requires runtime miner template instrumentation"},
	{Name: "template_build_failure_total", Reason: "requires runtime miner template instrumentation"},
	{Name: "miner_nonce_trials_total", Reason: "requires runtime solver instrumentation"},
	{Name: "committee_dial_failure_total", Reason: "requires runtime committee networking instrumentation"},
	{Name: "committee_participation_success_total", Reason: "requires runtime committee signing instrumentation"},
}

type ChainClient interface {
	GetMinerBlockCount(ctx context.Context) (int64, error)
	GetMinerBlockHash(ctx context.Context, height int64) (string, error)
	GetMinerBlock(ctx context.Context, hash string) (*MinerBlock, error)
}

type ScanConfig struct {
	StartMinerHeight int64
	EndMinerHeight   int64
	Workers          map[string]string
	RewardReport     *RewardAttributionReport
}

type MinerBlock struct {
	Hash       string `json:"hash"`
	Height     int64  `json:"height"`
	Address    string `json:"address"`
	Connection string `json:"connection"`
	Best       string `json:"best"`
}

type RewardAttributionReport struct {
	Metadata               *RewardMetadata `json:"metadata,omitempty"`
	Workers                []RewardWorker  `json:"workers"`
	UnmatchedRewardAtoms   int64           `json:"unmatched_reward_atoms"`
	UnmatchedRewardDecimal string          `json:"unmatched_reward_decimal"`
}

type RewardMetadata struct {
	MinerStartHeight *int64 `json:"miner_start_height,omitempty"`
	MinerEndHeight   *int64 `json:"miner_end_height,omitempty"`
}

type RewardWorker struct {
	Worker           string `json:"worker"`
	MiningAddress    string `json:"mining_address"`
	AcceptedMRBlocks int64  `json:"accepted_mrblocks"`
	RewardAtoms      int64  `json:"reward_atoms"`
	RewardDecimal    string `json:"reward_decimal"`
}

type Report struct {
	ScanStartMinerHeight          int64               `json:"scan_start_miner_height"`
	ScanEndMinerHeight            int64               `json:"scan_end_miner_height"`
	ScannedMinerBlocksTotal       int64               `json:"scanned_miner_blocks_total"`
	ConnectionEmptyBlocksTotal    int64               `json:"connection_empty_blocks_total"`
	ConnectionNonemptyBlocksTotal int64               `json:"connection_nonempty_blocks_total"`
	RouteAAcceptedShare           string              `json:"route_a_accepted_share"`
	RouteBAcceptedShare           string              `json:"route_b_accepted_share"`
	UnmatchedRewardAtoms          int64               `json:"unmatched_reward_atoms"`
	UnmatchedReward               string              `json:"unmatched_reward"`
	Workers                       []WorkerMetrics     `json:"workers"`
	UnavailableMetrics            []UnavailableMetric `json:"unavailable_metrics"`
	Warnings                      []string            `json:"warnings,omitempty"`
}

type WorkerMetrics struct {
	Worker                          string `json:"worker"`
	MiningAddress                   string `json:"mining_address"`
	PerWorkerAcceptedMRBlocks       int64  `json:"per_worker_accepted_mrblocks"`
	PerWorkerRouteAAcceptedMRBlocks int64  `json:"per_worker_route_a_accepted_mrblocks"`
	PerWorkerRouteBAcceptedMRBlocks int64  `json:"per_worker_route_b_accepted_mrblocks"`
	PerWorkerAttributedRewardAtoms  int64  `json:"per_worker_attributed_reward_atoms"`
	PerWorkerAttributedReward       string `json:"per_worker_attributed_reward"`
}

type UnavailableMetric struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}
