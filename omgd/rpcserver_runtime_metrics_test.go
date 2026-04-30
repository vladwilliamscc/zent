package main

import (
	"testing"

	"omega/runtimemetrics"
)

func TestGetMinerRuntimeMetricsReturnsAllSevenKeys(t *testing.T) {
	runtimemetrics.ResetForTest()
	runtimemetrics.IncIPMinerGapReject()
	runtimemetrics.IncMinerNonceTrials(3)

	result, err := handleGetMinerRuntimeMetrics(nil, nil, nil)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	got, ok := result.(map[string]uint64)
	if !ok {
		t.Fatalf("handler result type = %T, want map[string]uint64", result)
	}

	wantKeys := []string{
		"ip_minergap_reject_total",
		"addr_minergap_reject_total",
		"template_build_success_total",
		"template_build_failure_total",
		"miner_nonce_trials_total",
		"committee_dial_failure_total",
		"committee_participation_success_total",
	}
	if len(got) != len(wantKeys) {
		t.Fatalf("handler returned %d keys, want %d: %#v", len(got), len(wantKeys), got)
	}
	for _, key := range wantKeys {
		if _, ok := got[key]; !ok {
			t.Fatalf("handler missing key %q in %#v", key, got)
		}
	}
	if got["ip_minergap_reject_total"] != 1 {
		t.Fatalf("ip_minergap_reject_total = %d, want 1", got["ip_minergap_reject_total"])
	}
	if got["miner_nonce_trials_total"] != 3 {
		t.Fatalf("miner_nonce_trials_total = %d, want 3", got["miner_nonce_trials_total"])
	}
}
