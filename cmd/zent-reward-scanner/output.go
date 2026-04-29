package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

func writeJSON(w io.Writer, report *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func writeCSV(w io.Writer, report *Report) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{
		"worker",
		"mining_address",
		"accepted_mrblocks",
		"reward_atoms",
		"reward_decimal",
		"first_height",
		"last_height",
	}); err != nil {
		return err
	}
	for _, worker := range report.Workers {
		first := ""
		if worker.FirstHeight != nil {
			first = fmt.Sprintf("%d", *worker.FirstHeight)
		}
		last := ""
		if worker.LastHeight != nil {
			last = fmt.Sprintf("%d", *worker.LastHeight)
		}
		if err := cw.Write([]string{
			worker.Worker,
			worker.MiningAddress,
			fmt.Sprintf("%d", worker.AcceptedMRBlocks),
			fmt.Sprintf("%d", worker.RewardAtoms),
			worker.RewardDecimal,
			first,
			last,
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
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
