package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("zent-route-ab-metrics", flag.ContinueOnError)
	fs.SetOutput(stderr)

	rpcURL := fs.String("rpc-url", "http://127.0.0.1:8789", "omgd JSON-RPC URL")
	rpcUser := fs.String("rpc-user", "", "omgd JSON-RPC username")
	rpcPass := fs.String("rpc-pass", "", "omgd JSON-RPC password")
	startMinerHeight := fs.Int64("start-miner-height", -1, "first miner-chain height to scan")
	endMinerHeight := fs.Int64("end-miner-height", -1, "last miner-chain height to scan")
	rewardReportPath := fs.String("reward-report", "", "path to PR-9 zent-reward-scanner JSON output")
	workersFile := fs.String("workers-file", "", "optional JSON or CSV mapping mining_address to worker label")
	format := fs.String("format", "json", "output format: json or prometheus")
	outputPath := fs.String("output", "", "output path; defaults to stdout")
	timeout := fs.Duration("timeout", 30*time.Second, "per-RPC HTTP timeout")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rewardReportPath == "" {
		return fmt.Errorf("--reward-report is required")
	}

	workers, err := loadWorkersFile(*workersFile)
	if err != nil {
		return fmt.Errorf("load workers-file: %w", err)
	}
	rewardReport, err := loadRewardReport(*rewardReportPath)
	if err != nil {
		return fmt.Errorf("load reward-report: %w", err)
	}

	client := NewRPCClient(*rpcURL, *rpcUser, *rpcPass, *timeout)
	report, err := Scan(context.Background(), client, ScanConfig{
		StartMinerHeight: *startMinerHeight,
		EndMinerHeight:   *endMinerHeight,
		Workers:          workers,
		RewardReport:     rewardReport,
	})
	if err != nil {
		return err
	}

	out := stdout
	var file *os.File
	if *outputPath != "" {
		file, err = os.Create(*outputPath)
		if err != nil {
			return err
		}
		defer file.Close()
		out = file
	}

	switch *format {
	case "json":
		return writeJSON(out, report)
	case "prometheus":
		return writePrometheus(out, report)
	default:
		return fmt.Errorf("unsupported format %q", *format)
	}
}
