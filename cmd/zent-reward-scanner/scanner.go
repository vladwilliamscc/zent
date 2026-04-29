package main

import (
	"context"
	"fmt"
	"sort"

	"btcd/wire/common"
)

type workerAccumulator struct {
	worker       string
	address      string
	accepted     int64
	acceptedAt   []int64
	rewardAtoms  int64
	rewardEvents []RewardEvent
	firstHeight  *int64
	lastHeight   *int64
}

func Scan(ctx context.Context, client ChainClient, cfg ScanConfig) (*Report, error) {
	if cfg.Workers == nil {
		cfg.Workers = make(map[string]string)
	}

	if cfg.EndHeight < 0 {
		end, err := client.GetBlockCount(ctx)
		if err != nil {
			return nil, err
		}
		cfg.EndHeight = end
	}
	if cfg.EndMinerHeight < 0 {
		end, err := client.GetMinerBlockCount(ctx)
		if err != nil {
			return nil, err
		}
		cfg.EndMinerHeight = end
	}
	if cfg.StartHeight < 0 {
		cfg.StartHeight = cfg.EndHeight
	}
	if cfg.StartMinerHeight < 0 {
		cfg.StartMinerHeight = cfg.EndMinerHeight
	}
	if cfg.StartHeight > cfg.EndHeight {
		return nil, fmt.Errorf("start-height %d is greater than end-height %d", cfg.StartHeight, cfg.EndHeight)
	}
	if cfg.StartMinerHeight > cfg.EndMinerHeight {
		return nil, fmt.Errorf("start-miner-height %d is greater than end-miner-height %d", cfg.StartMinerHeight, cfg.EndMinerHeight)
	}

	report := &Report{
		Metadata: ScanMetadata{
			TxStartHeight:    cfg.StartHeight,
			TxEndHeight:      cfg.EndHeight,
			MinerStartHeight: cfg.StartMinerHeight,
			MinerEndHeight:   cfg.EndMinerHeight,
		},
		UnmatchedRewards: make([]UnmatchedReward, 0),
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

	for height := cfg.StartMinerHeight; height <= cfg.EndMinerHeight; height++ {
		hash, err := client.GetMinerBlockHash(ctx, height)
		if err != nil {
			return nil, fmt.Errorf("getminerblockhash %d: %w", height, err)
		}
		block, err := client.GetMinerBlock(ctx, hash)
		if err != nil {
			return nil, fmt.Errorf("getminerblock %s: %w", hash, err)
		}
		report.Metadata.MinerBlocksScanned++
		if block == nil || block.Address == "" {
			report.Metadata.Warnings = append(report.Metadata.Warnings,
				fmt.Sprintf("miner height %d hash %s missing address; accepted block not attributed", height, hash))
			continue
		}
		blockHeight := height
		if block.Height != 0 {
			blockHeight = block.Height
		}
		acc := ensureWorker(workers, block.Address, cfg.Workers[block.Address])
		acc.accepted++
		acc.acceptedAt = append(acc.acceptedAt, blockHeight)
		updateFirstLast(acc, blockHeight)
	}

	for height := cfg.StartHeight; height <= cfg.EndHeight; height++ {
		hash, err := client.GetBlockHash(ctx, height)
		if err != nil {
			return nil, fmt.Errorf("getblockhash %d: %w", height, err)
		}
		block, err := client.GetBlock(ctx, hash)
		if err != nil {
			return nil, fmt.Errorf("getblock %s: %w", hash, err)
		}
		report.Metadata.TxBlocksScanned++
		if block == nil || len(block.RawTx) == 0 {
			report.Metadata.Warnings = append(report.Metadata.Warnings,
				fmt.Sprintf("tx height %d hash %s missing verbose coinbase transaction", height, hash))
			continue
		}
		blockHeight := height
		if block.Height != 0 {
			blockHeight = block.Height
		}
		scanCoinbaseOutputs(report, workers, cfg.Workers, blockHeight, hash, block.RawTx[0])
	}

	report.Workers = buildWorkerReports(workers)
	for _, worker := range report.Workers {
		report.AttributedRewardAtoms += worker.RewardAtoms
	}
	report.AttributedRewardDecimal = formatAtoms(report.AttributedRewardAtoms)
	report.TotalRewardDecimal = formatAtoms(report.TotalRewardAtoms)
	report.UnmatchedRewardDecimal = formatAtoms(report.UnmatchedRewardAtoms)
	sortUnmatched(report.UnmatchedRewards)

	return report, nil
}

func scanCoinbaseOutputs(report *Report, workers map[string]*workerAccumulator, workerLabels map[string]string, height int64, blockHash string, coinbase TxRaw) {
	for _, out := range coinbase.Vout {
		if out.TokenType != common.FeeCoinTyp {
			continue
		}
		atoms, err := parseRewardAtoms(out.Value)
		if err != nil {
			report.Metadata.Warnings = append(report.Metadata.Warnings,
				fmt.Sprintf("tx height %d hash %s coinbase vout %d has malformed reward value: %v", height, blockHash, out.N, err))
			continue
		}
		if atoms == 0 {
			continue
		}
		report.TotalRewardAtoms += atoms

		addresses := append([]string(nil), out.ScriptPubKey.Addresses...)
		sort.Strings(addresses)

		if len(addresses) != 1 {
			reason := "reward output has no addresses"
			if len(addresses) > 1 {
				reason = "multiple output addresses; attribution ambiguous"
			}
			appendUnmatchedReward(report, height, blockHash, coinbase.TxID, out.N, addresses, atoms, reason)
			report.UnmatchedRewardAtoms += atoms
			continue
		}

		address := addresses[0]
		if _, ok := workers[address]; !ok {
			if _, ok := workerLabels[address]; !ok {
				appendUnmatchedReward(report, height, blockHash, coinbase.TxID, out.N, addresses, atoms,
					"output address did not match accepted miner addresses or workers-file")
				report.UnmatchedRewardAtoms += atoms
				continue
			}
		}

		acc := ensureWorker(workers, address, workerLabels[address])
		event := RewardEvent{
			Height:        height,
			BlockHash:     blockHash,
			TxID:          coinbase.TxID,
			Vout:          out.N,
			MiningAddress: address,
			Worker:        acc.worker,
			RewardAtoms:   atoms,
			RewardDecimal: formatAtoms(atoms),
			Reason:        "direct coinbase output address match",
		}
		acc.rewardAtoms += atoms
		acc.rewardEvents = append(acc.rewardEvents, event)
		updateFirstLast(acc, height)
	}
}

func appendUnmatchedReward(report *Report, height int64, blockHash, txid string, vout uint32, addresses []string, atoms int64, reason string) {
	report.UnmatchedRewards = append(report.UnmatchedRewards, UnmatchedReward{
		Height:        height,
		BlockHash:     blockHash,
		TxID:          txid,
		Vout:          vout,
		Addresses:     addresses,
		RewardAtoms:   atoms,
		RewardDecimal: formatAtoms(atoms),
		Reason:        reason,
	})
}

func ensureWorker(workers map[string]*workerAccumulator, address, label string) *workerAccumulator {
	if acc, ok := workers[address]; ok {
		if acc.worker == "" {
			acc.worker = workerLabel(address, label)
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

func updateFirstLast(acc *workerAccumulator, height int64) {
	if acc.firstHeight == nil || height < *acc.firstHeight {
		h := height
		acc.firstHeight = &h
	}
	if acc.lastHeight == nil || height > *acc.lastHeight {
		h := height
		acc.lastHeight = &h
	}
}

func buildWorkerReports(workers map[string]*workerAccumulator) []WorkerReport {
	out := make([]WorkerReport, 0, len(workers))
	for _, acc := range workers {
		sort.Slice(acc.rewardEvents, func(i, j int) bool {
			return rewardEventLess(acc.rewardEvents[i], acc.rewardEvents[j])
		})
		sort.Slice(acc.acceptedAt, func(i, j int) bool { return acc.acceptedAt[i] < acc.acceptedAt[j] })
		out = append(out, WorkerReport{
			Worker:            acc.worker,
			MiningAddress:     acc.address,
			AcceptedMRBlocks:  acc.accepted,
			RewardAtoms:       acc.rewardAtoms,
			RewardDecimal:     formatAtoms(acc.rewardAtoms),
			FirstHeight:       acc.firstHeight,
			LastHeight:        acc.lastHeight,
			AcceptedMRHeights: acc.acceptedAt,
			RewardEvents:      acc.rewardEvents,
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

func sortUnmatched(events []UnmatchedReward) {
	sort.Slice(events, func(i, j int) bool {
		a, b := events[i], events[j]
		if a.Height != b.Height {
			return a.Height < b.Height
		}
		if a.BlockHash != b.BlockHash {
			return a.BlockHash < b.BlockHash
		}
		if a.TxID != b.TxID {
			return a.TxID < b.TxID
		}
		return a.Vout < b.Vout
	})
}

func rewardEventLess(a, b RewardEvent) bool {
	if a.Height != b.Height {
		return a.Height < b.Height
	}
	if a.BlockHash != b.BlockHash {
		return a.BlockHash < b.BlockHash
	}
	if a.TxID != b.TxID {
		return a.TxID < b.TxID
	}
	return a.Vout < b.Vout
}
