package runtimemetrics

import (
	"sync"
	"testing"
)

func TestIncrementHelpersAreAtomic(t *testing.T) {
	ResetForTest()

	const goroutines = 8
	const iterations = 1000

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				IncIPMinerGapReject()
				IncAddrMinerGapReject()
				IncTemplateBuildSuccess()
				IncTemplateBuildFailure()
				IncMinerNonceTrials(1)
				IncCommitteeDialFailure()
				IncCommitteeParticipationSuccess()
			}
		}()
	}
	wg.Wait()

	want := uint64(goroutines * iterations)
	got := Read()
	if got.IPMinerGapRejectTotal != want ||
		got.AddrMinerGapRejectTotal != want ||
		got.TemplateBuildSuccessTotal != want ||
		got.TemplateBuildFailureTotal != want ||
		got.MinerNonceTrialsTotal != want ||
		got.CommitteeDialFailureTotal != want ||
		got.CommitteeParticipationSuccessTotal != want {
		t.Fatalf("snapshot = %#v, want all counters %d", got, want)
	}
}

func TestReadReturnsSnapshot(t *testing.T) {
	ResetForTest()

	IncIPMinerGapReject()
	IncTemplateBuildSuccess()
	IncMinerNonceTrials(7)

	got := Read()
	if got.IPMinerGapRejectTotal != 1 {
		t.Fatalf("IPMinerGapRejectTotal = %d, want 1", got.IPMinerGapRejectTotal)
	}
	if got.TemplateBuildSuccessTotal != 1 {
		t.Fatalf("TemplateBuildSuccessTotal = %d, want 1", got.TemplateBuildSuccessTotal)
	}
	if got.MinerNonceTrialsTotal != 7 {
		t.Fatalf("MinerNonceTrialsTotal = %d, want 7", got.MinerNonceTrialsTotal)
	}
	if got.AddrMinerGapRejectTotal != 0 ||
		got.TemplateBuildFailureTotal != 0 ||
		got.CommitteeDialFailureTotal != 0 ||
		got.CommitteeParticipationSuccessTotal != 0 {
		t.Fatalf("unexpected non-zero counters: %#v", got)
	}
}

func TestResetForTestZeroesAll(t *testing.T) {
	IncIPMinerGapReject()
	IncAddrMinerGapReject()
	IncTemplateBuildFailure()
	IncCommitteeDialFailure()

	ResetForTest()

	if got := Read(); got != (Snapshot{}) {
		t.Fatalf("snapshot after reset = %#v, want zero", got)
	}
}
