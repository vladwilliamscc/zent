package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

func loadRewardReport(path string) (*RewardAttributionReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var report RewardAttributionReport
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	if err := dec.Decode(&report); err != nil {
		return nil, err
	}
	return &report, nil
}

func loadWorkersFile(path string) (map[string]string, error) {
	workers := make(map[string]string)
	if path == "" {
		return workers, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return workers, nil
	}

	if err := json.Unmarshal(data, &workers); err == nil {
		return workers, nil
	}

	reader := csv.NewReader(strings.NewReader(string(data)))
	reader.TrimLeadingSpace = true
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	for i, record := range records {
		if len(record) == 0 {
			continue
		}
		if i == 0 && len(record) >= 2 && strings.EqualFold(record[0], "mining_address") {
			continue
		}
		if len(record) != 2 {
			return nil, fmt.Errorf("workers CSV row %d has %d fields, want 2", i+1, len(record))
		}
		address := strings.TrimSpace(record[0])
		label := strings.TrimSpace(record[1])
		if address == "" {
			return nil, fmt.Errorf("workers CSV row %d has empty mining address", i+1)
		}
		workers[address] = label
	}
	return workers, nil
}

func writeJSON(w io.Writer, report *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func writePrometheus(w io.Writer, report *Report) error {
	lines := make([]string, 0)
	addMetric := func(name string, value interface{}) {
		lines = append(lines, fmt.Sprintf("%s %v", name, value))
	}
	addMetric("scan_start_miner_height", report.ScanStartMinerHeight)
	addMetric("scan_end_miner_height", report.ScanEndMinerHeight)
	addMetric("scanned_miner_blocks_total", report.ScannedMinerBlocksTotal)
	addMetric("connection_empty_blocks_total", report.ConnectionEmptyBlocksTotal)
	addMetric("connection_nonempty_blocks_total", report.ConnectionNonemptyBlocksTotal)
	addMetric("route_a_accepted_share", report.RouteAAcceptedShare)
	addMetric("route_b_accepted_share", report.RouteBAcceptedShare)
	addMetric("unmatched_reward_atoms", report.UnmatchedRewardAtoms)

	for _, worker := range report.Workers {
		labels := prometheusLabels(map[string]string{
			"worker":         worker.Worker,
			"mining_address": worker.MiningAddress,
		})
		lines = append(lines,
			fmt.Sprintf("per_worker_accepted_mrblocks%s %d", labels, worker.PerWorkerAcceptedMRBlocks),
			fmt.Sprintf("per_worker_route_a_accepted_mrblocks%s %d", labels, worker.PerWorkerRouteAAcceptedMRBlocks),
			fmt.Sprintf("per_worker_route_b_accepted_mrblocks%s %d", labels, worker.PerWorkerRouteBAcceptedMRBlocks),
			fmt.Sprintf("per_worker_attributed_reward_atoms%s %d", labels, worker.PerWorkerAttributedRewardAtoms),
		)
	}

	for _, metric := range report.UnavailableMetrics {
		labels := prometheusLabels(map[string]string{
			"name":   metric.Name,
			"reason": metric.Reason,
		})
		lines = append(lines, fmt.Sprintf("unavailable_metric_info%s 1", labels))
	}

	sort.Strings(lines)
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

func prometheusLabels(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, key, prometheusEscape(labels[key])))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func prometheusEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return strings.ReplaceAll(s, `"`, `\"`)
}
