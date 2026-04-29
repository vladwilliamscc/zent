package main

import (
	"context"
	"fmt"
	"sort"
)

type workerAccumulator struct {
	worker      string
	address     string
	accepted    int64
	routeA      int64
	routeB      int64
	rewardAtoms int64
}

func Scan(ctx context.Context, client ChainClient, cfg ScanConfig) (*Report, error) {
	if cfg.Workers == nil {
		cfg.Workers = make(map[string]string)
	}
	if cfg.RewardReport == nil {
		cfg.RewardReport = &RewardAttributionReport{}
	}

	if cfg.EndMinerHeight < 0 {
		end, err := client.GetMinerBlockCount(ctx)
		if err != nil {
			return nil, err
		}
		cfg.EndMinerHeight = end
	}
	if cfg.StartMinerHeight < 0 {
		cfg.StartMinerHeight = cfg.EndMinerHeight
	}
	if cfg.StartMinerHeight > cfg.EndMinerHeight {
		return nil, fmt.Errorf("start-miner-height %d is greater than end-miner-height %d", cfg.StartMinerHeight, cfg.EndMinerHeight)
	}

	warnings, err := validateRewardReportWindow(cfg.RewardReport, cfg.StartMinerHeight, cfg.EndMinerHeight)
	if err != nil {
		return nil, err
	}

	report := &Report{
		ScanStartMinerHeight: cfg.StartMinerHeight,
		ScanEndMinerHeight:   cfg.EndMinerHeight,
		UnavailableMetrics:   append([]UnavailableMetric(nil), unavailableRuntimeMetrics...),
		Warnings:             warnings,
	}

	workers := make(map[string]*workerAccumulator)
	for address, label := range cfg.Workers {
		if address == "" {
			continue
		}
		workers[address] = &workerAccumulator{
			worker:  workerLabel(address, label),
			address: address,
		}
	}

	mergeRewardReport(workers, cfg.Workers, cfg.RewardReport)
	report.UnmatchedRewardAtoms = cfg.RewardReport.UnmatchedRewardAtoms
	report.UnmatchedReward = formatAtoms(cfg.RewardReport.UnmatchedRewardAtoms)

	for height := cfg.StartMinerHeight; height <= cfg.EndMinerHeight; height++ {
		hash, err := client.GetMinerBlockHash(ctx, height)
		if err != nil {
			return nil, fmt.Errorf("getminerblockhash %d: %w", height, err)
		}
		block, err := client.GetMinerBlock(ctx, hash)
		if err != nil {
			return nil, fmt.Errorf("getminerblock %s: %w", hash, err)
		}
		report.ScannedMinerBlocksTotal++
		if block == nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("miner height %d hash %s returned null block", height, hash))
			continue
		}

		route := routeB
		if block.Connection != "" {
			route = routeA
			report.ConnectionNonemptyBlocksTotal++
		} else {
			report.ConnectionEmptyBlocksTotal++
		}

		if block.Address == "" {
			report.Warnings = append(report.Warnings, fmt.Sprintf("miner height %d hash %s missing address", height, hash))
			continue
		}
		acc := ensureWorker(workers, block.Address, cfg.Workers[block.Address])
		acc.accepted++
		if route == routeA {
			acc.routeA++
		} else {
			acc.routeB++
		}
	}

	appendAcceptedMRBlockMismatchWarnings(report, workers, cfg.RewardReport)

	totalAccepted := report.ConnectionEmptyBlocksTotal + report.ConnectionNonemptyBlocksTotal
	report.RouteAAcceptedShare = formatShare(report.ConnectionNonemptyBlocksTotal, totalAccepted)
	report.RouteBAcceptedShare = formatShare(report.ConnectionEmptyBlocksTotal, totalAccepted)
	report.Workers = buildWorkerMetrics(workers)

	return report, nil
}

func validateRewardReportWindow(rewards *RewardAttributionReport, startHeight, endHeight int64) ([]string, error) {
	if rewards.Metadata == nil {
		return []string{"reward report metadata missing; miner height window could not be verified"}, nil
	}
	if rewards.Metadata.MinerStartHeight == nil || rewards.Metadata.MinerEndHeight == nil {
		return nil, fmt.Errorf("reward report miner height metadata incomplete")
	}
	rewardStart := *rewards.Metadata.MinerStartHeight
	rewardEnd := *rewards.Metadata.MinerEndHeight
	if rewardStart != startHeight || rewardEnd != endHeight {
		return nil, fmt.Errorf("reward report miner height window %d-%d does not match route scan window %d-%d",
			rewardStart, rewardEnd, startHeight, endHeight)
	}
	return nil, nil
}

func mergeRewardReport(workers map[string]*workerAccumulator, labels map[string]string, rewards *RewardAttributionReport) {
	for _, worker := range rewards.Workers {
		if worker.MiningAddress == "" {
			continue
		}
		acc := ensureWorker(workers, worker.MiningAddress, labels[worker.MiningAddress])
		if _, ok := labels[worker.MiningAddress]; !ok && worker.Worker != "" {
			acc.worker = worker.Worker
		}
		acc.rewardAtoms += worker.RewardAtoms
	}
}

func appendAcceptedMRBlockMismatchWarnings(report *Report, workers map[string]*workerAccumulator, rewards *RewardAttributionReport) {
	for _, worker := range rewards.Workers {
		if worker.MiningAddress == "" {
			continue
		}
		var got int64
		if acc, ok := workers[worker.MiningAddress]; ok {
			got = acc.accepted
		}
		if worker.AcceptedMRBlocks != got {
			report.Warnings = append(report.Warnings, fmt.Sprintf(
				"accepted_mrblocks mismatch for %s: reward report=%d route scan=%d",
				worker.MiningAddress, worker.AcceptedMRBlocks, got))
		}
	}
}

func ensureWorker(workers map[string]*workerAccumulator, address, label string) *workerAccumulator {
	if acc, ok := workers[address]; ok {
		if label != "" {
			acc.worker = label
		} else if acc.worker == "" {
			acc.worker = address
		}
		return acc
	}
	acc := &workerAccumulator{
		worker:  workerLabel(address, label),
		address: address,
	}
	workers[address] = acc
	return acc
}

func workerLabel(address, label string) string {
	if label != "" {
		return label
	}
	return address
}

func buildWorkerMetrics(workers map[string]*workerAccumulator) []WorkerMetrics {
	out := make([]WorkerMetrics, 0, len(workers))
	for _, acc := range workers {
		out = append(out, WorkerMetrics{
			Worker:                          acc.worker,
			MiningAddress:                   acc.address,
			PerWorkerAcceptedMRBlocks:       acc.accepted,
			PerWorkerRouteAAcceptedMRBlocks: acc.routeA,
			PerWorkerRouteBAcceptedMRBlocks: acc.routeB,
			PerWorkerAttributedRewardAtoms:  acc.rewardAtoms,
			PerWorkerAttributedReward:       formatAtoms(acc.rewardAtoms),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Worker != out[j].Worker {
			return out[i].Worker < out[j].Worker
		}
		return out[i].MiningAddress < out[j].MiningAddress
	})
	return out
}
