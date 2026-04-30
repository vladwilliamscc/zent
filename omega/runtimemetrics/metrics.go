// Package runtimemetrics holds process-global counters for runtime events that
// do not produce on-chain artifacts and therefore cannot be inferred by a
// scanner.
//
// Layering: this package is a neutral sibling of omega/minerchain and
// omega/consensus. Do not make it depend on either package.
package runtimemetrics

import "sync/atomic"

// Snapshot is a value-type read of all runtime counters. Individual fields are
// loaded atomically; the snapshot is not cross-counter consistent.
type Snapshot struct {
	IPMinerGapRejectTotal              uint64
	AddrMinerGapRejectTotal            uint64
	TemplateBuildSuccessTotal          uint64
	TemplateBuildFailureTotal          uint64
	MinerNonceTrialsTotal              uint64
	CommitteeDialFailureTotal          uint64
	CommitteeParticipationSuccessTotal uint64
}

var (
	ipMinerGapRejectTotal              uint64
	addrMinerGapRejectTotal            uint64
	templateBuildSuccessTotal          uint64
	templateBuildFailureTotal          uint64
	minerNonceTrialsTotal              uint64
	committeeDialFailureTotal          uint64
	committeeParticipationSuccessTotal uint64
)

// Read returns a point-in-time snapshot of all counters.
func Read() Snapshot {
	return Snapshot{
		IPMinerGapRejectTotal:              atomic.LoadUint64(&ipMinerGapRejectTotal),
		AddrMinerGapRejectTotal:            atomic.LoadUint64(&addrMinerGapRejectTotal),
		TemplateBuildSuccessTotal:          atomic.LoadUint64(&templateBuildSuccessTotal),
		TemplateBuildFailureTotal:          atomic.LoadUint64(&templateBuildFailureTotal),
		MinerNonceTrialsTotal:              atomic.LoadUint64(&minerNonceTrialsTotal),
		CommitteeDialFailureTotal:          atomic.LoadUint64(&committeeDialFailureTotal),
		CommitteeParticipationSuccessTotal: atomic.LoadUint64(&committeeParticipationSuccessTotal),
	}
}

func IncIPMinerGapReject() {
	atomic.AddUint64(&ipMinerGapRejectTotal, 1)
}

func IncAddrMinerGapReject() {
	atomic.AddUint64(&addrMinerGapRejectTotal, 1)
}

func IncTemplateBuildSuccess() {
	atomic.AddUint64(&templateBuildSuccessTotal, 1)
}

func IncTemplateBuildFailure() {
	atomic.AddUint64(&templateBuildFailureTotal, 1)
}

func IncMinerNonceTrials(n uint64) {
	atomic.AddUint64(&minerNonceTrialsTotal, n)
}

func IncCommitteeDialFailure() {
	atomic.AddUint64(&committeeDialFailureTotal, 1)
}

func IncCommitteeParticipationSuccess() {
	atomic.AddUint64(&committeeParticipationSuccessTotal, 1)
}

// ResetForTest zeroes every counter atomically. It exists only for test
// isolation; production code must not call it.
func ResetForTest() {
	atomic.StoreUint64(&ipMinerGapRejectTotal, 0)
	atomic.StoreUint64(&addrMinerGapRejectTotal, 0)
	atomic.StoreUint64(&templateBuildSuccessTotal, 0)
	atomic.StoreUint64(&templateBuildFailureTotal, 0)
	atomic.StoreUint64(&minerNonceTrialsTotal, 0)
	atomic.StoreUint64(&committeeDialFailureTotal, 0)
	atomic.StoreUint64(&committeeParticipationSuccessTotal, 0)
}
