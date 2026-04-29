package minerchain

import (
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"

	"btcd/chaincfg"
	"btcd/wire"
)

func TestEffectiveH1CollateralScheduleParity(t *testing.T) {
	const (
		startHeight = int32(100)
		stopHeight  = int32(120)
		iterations  = 5000
	)

	params := h1ScheduleParams(startHeight, stopHeight)
	bits := uint32(0x207fffff)
	factorPOW := int64(1)
	h2 := int64(1)
	powLimit := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

	intervals := []struct {
		name               string
		height             int32
		wantParentSelected bool
	}{
		{name: "pre-start", height: startHeight - 1},
		{name: "active", height: startHeight + 1, wantParentSelected: true},
		{name: "post-stop", height: stopHeight},
	}

	directions := []struct {
		name             string
		parentCollateral uint32
		headerCollateral uint32
		collateralValue  uint32
	}{
		{name: "case-a-header-lt-parent", parentCollateral: 100000, headerCollateral: 87500, collateralValue: 180000},
		{name: "case-b-header-gt-parent", parentCollateral: 50000, headerCollateral: 87500, collateralValue: 180000},
		{name: "case-c-header-eq-parent", parentCollateral: 80000, headerCollateral: 80000, collateralValue: 180000},
	}

	for _, interval := range intervals {
		for _, direction := range directions {
			name := interval.name + "/" + direction.name
			header := h1ScheduleHeader(direction.headerCollateral)
			parent := h1ScheduleHeader(direction.parentCollateral)

			acceptC, err := EffectiveH1Collateral(header, parent, interval.height, params)
			if err != nil {
				t.Fatalf("%s: accept helper returned error: %v", name, err)
			}
			solverC, err := EffectiveH1Collateral(header, parent, interval.height, params)
			if err != nil {
				t.Fatalf("%s: solver helper returned error: %v", name, err)
			}
			if acceptC != solverC {
				t.Fatalf("%s: accept denominator %d != solver denominator %d", name, acceptC, solverC)
			}

			wantC := direction.headerCollateral
			if interval.wantParentSelected {
				wantC = direction.parentCollateral
			}
			if wantC == 0 {
				wantC = 1
			}
			if acceptC != wantC {
				t.Fatalf("%s: denominator = %d, want %d", name, acceptC, wantC)
			}

			acceptH1 := h1ScheduleFactor(direction.collateralValue, acceptC)
			solverH1 := h1ScheduleFactor(direction.collateralValue, solverC)
			acceptHits := h1ScheduleHits(header, bits, factorPOW, acceptH1+h2, powLimit, iterations)
			solverHits := h1ScheduleHits(header, bits, factorPOW, solverH1+h2, powLimit, iterations)
			if !reflect.DeepEqual(acceptHits, solverHits) {
				t.Fatalf("%s: helper accept hits differ from helper solver hits: %v != %v", name, acceptHits, solverHits)
			}
		}
	}
}

func TestEffectiveH1CollateralMissingContext(t *testing.T) {
	const (
		startHeight = int32(100)
		stopHeight  = int32(120)
	)

	params := h1ScheduleParams(startHeight, stopHeight)
	header := h1ScheduleHeader(75)
	zeroHeader := h1ScheduleHeader(0)
	parent := h1ScheduleHeader(50)
	zeroParent := h1ScheduleHeader(0)

	tests := []struct {
		name        string
		header      *wire.MingingRightBlock
		parent      *wire.MingingRightBlock
		height      int32
		want        uint32
		wantErrText string
	}{
		{
			name:        "active-missing-parent-errors",
			header:      header,
			height:      startHeight,
			wantErrText: "missing parent header for active interval",
		},
		{
			name:   "active-parent-zero-falls-back-to-one",
			header: header,
			parent: zeroParent,
			height: startHeight,
			want:   1,
		},
		{
			name:   "active-parent-collateral",
			header: header,
			parent: parent,
			height: startHeight,
			want:   parent.Collateral,
		},
		{
			name:        "pre-start-missing-current-header-errors",
			parent:      parent,
			height:      startHeight - 1,
			wantErrText: "missing current header for legacy interval",
		},
		{
			name:   "pre-start-missing-parent-uses-current-header",
			header: header,
			height: startHeight - 1,
			want:   header.Collateral,
		},
		{
			name:   "post-stop-current-zero-falls-back-to-one",
			header: zeroHeader,
			parent: parent,
			height: stopHeight,
			want:   1,
		},
	}

	for _, test := range tests {
		got, err := EffectiveH1Collateral(test.header, test.parent, test.height, params)
		if test.wantErrText != "" {
			if err == nil {
				t.Fatalf("%s: expected error containing %q", test.name, test.wantErrText)
			}
			if !strings.Contains(err.Error(), test.wantErrText) {
				t.Fatalf("%s: error = %q, want substring %q", test.name, err.Error(), test.wantErrText)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}
		if got != test.want {
			t.Fatalf("%s: collateral = %d, want %d", test.name, got, test.want)
		}
	}
}

func TestH1DenominatorScheduleBoundaries(t *testing.T) {
	params := h1ScheduleParams(100, 120)

	tests := []struct {
		name   string
		height int32
		active bool
	}{
		{name: "before-start", height: 99},
		{name: "at-start", height: 100, active: true},
		{name: "before-stop", height: 119, active: true},
		{name: "at-stop", height: 120},
		{name: "after-stop", height: 121},
	}

	for _, test := range tests {
		if got := h1DenominatorScheduleActive(test.height, params); got != test.active {
			t.Fatalf("%s: active = %v, want %v", test.name, got, test.active)
		}
	}

	forever := h1ScheduleParams(100, 0)
	if !h1DenominatorScheduleActive(100000, forever) {
		t.Fatalf("stop=0 should keep the schedule active after start")
	}

	disabled := h1ScheduleParams(0, 0)
	if h1DenominatorScheduleActive(100000, disabled) {
		t.Fatalf("start=0 should disable the active interval")
	}
}

func h1ScheduleParams(start, stop int32) *chaincfg.Params {
	params := chaincfg.MainNetParams
	params.H1DenominatorActivationStartHeight = start
	params.H1DenominatorActivationStopHeight = stop
	return &params
}

func h1ScheduleHeader(collateral uint32) *wire.MingingRightBlock {
	return &wire.MingingRightBlock{
		Version:    0x20000,
		Timestamp:  time.Unix(1700000000, 0),
		Bits:       0x207fffff,
		Collateral: collateral,
	}
}

func h1ScheduleFactor(collateralValue, denominator uint32) int64 {
	h1 := int64(collateralValue / denominator)
	if h1 < 1 {
		return 1
	}
	return h1
}

func h1ScheduleHits(header *wire.MingingRightBlock, bits uint32, factorPOW, h int64, powLimit *big.Int, iterations int) []int32 {
	hits := make([]int32, 0)
	for nonce := int32(1); nonce <= int32(iterations); nonce++ {
		if TryMinerNonce(header, nonce, bits, factorPOW, h, powLimit) {
			hits = append(hits, nonce)
		}
	}
	return hits
}
