package minerchain

/*
This test exercises the production builders `acceptH1Bindings` and `solverH1Bindings`.
Do not copy logic out of either builder into this file; if a future change tempts
you to, fix the builder instead.
*/

import (
	"strings"
	"testing"
	"time"

	"btcd/blockchain/chainutil"
	"btcd/chaincfg"
	"btcd/mining"
	"btcd/wire"
)

func TestH1CallSiteBindingParity(t *testing.T) {
	const (
		startHeight = int32(100)
		stopHeight  = int32(120)
	)

	scheduleCells := []struct {
		name         string
		parentHeight int32
		params       *chaincfg.Params
	}{
		{name: "disabled", parentHeight: 50, params: h1CallSiteParams(0, 0)},
		{name: "pre_start", parentHeight: startHeight - 2, params: h1CallSiteParams(startHeight, stopHeight)},
		{name: "at_start", parentHeight: startHeight - 1, params: h1CallSiteParams(startHeight, stopHeight)},
		{name: "active_middle", parentHeight: startHeight + 5, params: h1CallSiteParams(startHeight, stopHeight)},
		{name: "before_stop", parentHeight: stopHeight - 2, params: h1CallSiteParams(startHeight, stopHeight)},
		{name: "at_stop", parentHeight: stopHeight - 1, params: h1CallSiteParams(startHeight, stopHeight)},
		{name: "after_stop", parentHeight: stopHeight, params: h1CallSiteParams(startHeight, stopHeight)},
		{name: "forever_at_start", parentHeight: startHeight - 1, params: h1CallSiteParams(startHeight, 0)},
		{name: "forever_late", parentHeight: startHeight + 50, params: h1CallSiteParams(startHeight, 0)},
	}

	cases := []struct {
		name             string
		parentCollateral uint32
		headerCollateral uint32
	}{
		{name: "case_a_header_lt_parent", parentCollateral: 100000, headerCollateral: 87500},
		{name: "case_b_header_gt_parent", parentCollateral: 50000, headerCollateral: 87500},
		{name: "case_c_header_eq_parent", parentCollateral: 80000, headerCollateral: 80000},
	}

	for cellIndex, cell := range scheduleCells {
		for caseIndex, testCase := range cases {
			t.Run(cell.name+"/"+testCase.name, func(t *testing.T) {
				parentHeader := h1CallSiteHeader(testCase.parentCollateral, int32(cellIndex+1))
				parentNode := h1CallSiteNode(cell.parentHeight, parentHeader)
				header := h1CallSiteHeader(testCase.headerCollateral, int32(100+caseIndex))
				header.PrevBlock = parentNode.Hash
				template := &mining.BlockTemplate{
					Block:  header,
					Height: parentNode.Height + 1,
				}

				acceptBindings, err := acceptH1Bindings(header, parentNode, cell.params)
				if err != nil {
					t.Fatalf("acceptH1Bindings returned error: %v", err)
				}
				solverBindings, err := solverH1Bindings(template, parentNode, cell.params)
				if err != nil {
					t.Fatalf("solverH1Bindings returned error: %v", err)
				}

				if mismatches := h1BindingMismatches(acceptBindings, solverBindings); len(mismatches) != 0 {
					t.Fatalf("h1 binding mismatches: %s", strings.Join(mismatches, ", "))
				}

				acceptCollateral, err := acceptBindings.Resolve()
				if err != nil {
					t.Fatalf("accept binding Resolve returned error: %v", err)
				}
				solverCollateral, err := solverBindings.Resolve()
				if err != nil {
					t.Fatalf("solver binding Resolve returned error: %v", err)
				}
				if acceptCollateral != solverCollateral {
					t.Fatalf("resolved collateral mismatch: accept=%d solver=%d", acceptCollateral, solverCollateral)
				}
			})
		}
	}

	t.Run("chainChoice_drift_fails", func(t *testing.T) {
		params := h1CallSiteParams(startHeight, stopHeight)
		parentA := h1CallSiteHeader(100000, 1)
		parentB := h1CallSiteHeader(100000, 2)
		parentNodeForAccept := h1CallSiteNode(startHeight-1, parentA)
		chainChoiceForSolver := h1CallSiteNode(startHeight-1, parentB)
		header := h1CallSiteHeader(87500, 10)
		header.PrevBlock = parentNodeForAccept.Hash
		template := &mining.BlockTemplate{Block: header, Height: parentNodeForAccept.Height + 1}

		acceptBindings, err := acceptH1Bindings(header, parentNodeForAccept, params)
		if err != nil {
			t.Fatalf("acceptH1Bindings returned error: %v", err)
		}
		solverBindings, err := solverH1Bindings(template, chainChoiceForSolver, params)
		if err != nil {
			t.Fatalf("solverH1Bindings returned error: %v", err)
		}

		h1RequireMismatch(t, h1BindingMismatches(acceptBindings, solverBindings), "ParentHeader.BlockHash")
	})

	t.Run("parent_collateral_mismatch_fails", func(t *testing.T) {
		params := h1CallSiteParams(startHeight, stopHeight)
		parentA := h1CallSiteHeader(100000, 1)
		parentB := h1CallSiteHeader(200000, 1)
		parentNodeForAccept := h1CallSiteNode(startHeight-1, parentA)
		chainChoiceForSolver := h1CallSiteNode(startHeight-1, parentB)
		chainChoiceForSolver.Hash = parentNodeForAccept.Hash
		header := h1CallSiteHeader(87500, 10)
		header.PrevBlock = parentNodeForAccept.Hash
		template := &mining.BlockTemplate{Block: header, Height: parentNodeForAccept.Height + 1}

		acceptBindings, err := acceptH1Bindings(header, parentNodeForAccept, params)
		if err != nil {
			t.Fatalf("acceptH1Bindings returned error: %v", err)
		}
		solverBindings, err := solverH1Bindings(template, chainChoiceForSolver, params)
		if err != nil {
			t.Fatalf("solverH1Bindings returned error: %v", err)
		}

		h1RequireMismatch(t, h1BindingMismatches(acceptBindings, solverBindings), "ParentHeader.Collateral")
	})

	t.Run("height_off_by_one_fails", func(t *testing.T) {
		params := h1CallSiteParams(startHeight, stopHeight)
		parentHeader := h1CallSiteHeader(100000, 1)
		parentNode := h1CallSiteNode(startHeight-1, parentHeader)
		header := h1CallSiteHeader(87500, 10)
		header.PrevBlock = parentNode.Hash
		template := &mining.BlockTemplate{Block: header, Height: parentNode.Height + 2}

		acceptBindings, err := acceptH1Bindings(header, parentNode, params)
		if err != nil {
			t.Fatalf("acceptH1Bindings returned error: %v", err)
		}
		solverBindings, err := solverH1Bindings(template, parentNode, params)
		if err != nil {
			t.Fatalf("solverH1Bindings returned error: %v", err)
		}

		h1RequireMismatch(t, h1BindingMismatches(acceptBindings, solverBindings), "Height")
	})

	t.Run("params_schedule_field_mismatch_fails", func(t *testing.T) {
		acceptParams := h1CallSiteParams(startHeight, stopHeight)
		solverParams := h1CallSiteParams(startHeight+1, stopHeight)
		parentHeader := h1CallSiteHeader(100000, 1)
		parentNode := h1CallSiteNode(startHeight-1, parentHeader)
		header := h1CallSiteHeader(87500, 10)
		header.PrevBlock = parentNode.Hash
		template := &mining.BlockTemplate{Block: header, Height: parentNode.Height + 1}

		acceptBindings, err := acceptH1Bindings(header, parentNode, acceptParams)
		if err != nil {
			t.Fatalf("acceptH1Bindings returned error: %v", err)
		}
		solverBindings, err := solverH1Bindings(template, parentNode, solverParams)
		if err != nil {
			t.Fatalf("solverH1Bindings returned error: %v", err)
		}

		h1RequireMismatch(t, h1BindingMismatches(acceptBindings, solverBindings), "Params")
	})

	t.Run("solver_builder_error_paths", func(t *testing.T) {
		params := h1CallSiteParams(startHeight, stopHeight)
		parentHeader := h1CallSiteHeader(100000, 1)
		parentNode := h1CallSiteNode(startHeight-1, parentHeader)
		header := h1CallSiteHeader(87500, 10)
		header.PrevBlock = parentNode.Hash
		template := &mining.BlockTemplate{Block: header, Height: parentNode.Height + 1}

		t.Run("nil_template", func(t *testing.T) {
			got, err := solverH1Bindings(nil, parentNode, params)
			h1RequireBuilderError(t, "nil template", got, err)
		})

		t.Run("nil_chainChoice", func(t *testing.T) {
			got, err := solverH1Bindings(template, nil, params)
			h1RequireBuilderError(t, "nil chainChoice", got, err)
		})

		t.Run("wrong_chainChoice_data", func(t *testing.T) {
			wrongDataNode := &chainutil.BlockNode{Data: h1WrongNodeData{}}
			got, err := solverH1Bindings(template, wrongDataNode, params)
			h1RequireBuilderError(t, "wrong chainChoice data", got, err)
		})

		t.Run("wrong_template_block", func(t *testing.T) {
			wrongTemplate := &mining.BlockTemplate{Block: "not a miner block", Height: parentNode.Height + 1}
			got, err := solverH1Bindings(wrongTemplate, parentNode, params)
			h1RequireBuilderError(t, "wrong template block", got, err)
		})
	})

	t.Run("accept_builder_error_paths", func(t *testing.T) {
		params := h1CallSiteParams(startHeight, stopHeight)
		header := h1CallSiteHeader(87500, 10)

		t.Run("nil_parentNode", func(t *testing.T) {
			got, err := acceptH1Bindings(header, nil, params)
			h1RequireBuilderError(t, "nil parentNode", got, err)
		})

		t.Run("wrong_parent_data", func(t *testing.T) {
			wrongDataNode := &chainutil.BlockNode{Data: h1WrongNodeData{}}
			got, err := acceptH1Bindings(header, wrongDataNode, params)
			h1RequireBuilderError(t, "wrong parent data", got, err)
		})
	})
}

func h1BindingMismatches(a, b effectiveH1Bindings) []string {
	var mismatches []string
	if a.Header == nil || b.Header == nil {
		if a.Header != b.Header {
			mismatches = append(mismatches, "Header.PrevBlock", "Header.Collateral")
		}
	} else {
		if a.Header.PrevBlock != b.Header.PrevBlock {
			mismatches = append(mismatches, "Header.PrevBlock")
		}
		if a.Header.Collateral != b.Header.Collateral {
			mismatches = append(mismatches, "Header.Collateral")
		}
	}

	if a.ParentHeader == nil || b.ParentHeader == nil {
		if a.ParentHeader != b.ParentHeader {
			mismatches = append(mismatches, "ParentHeader.BlockHash", "ParentHeader.Collateral")
		}
	} else {
		if a.ParentHeader.BlockHash() != b.ParentHeader.BlockHash() {
			mismatches = append(mismatches, "ParentHeader.BlockHash")
		}
		if a.ParentHeader.Collateral != b.ParentHeader.Collateral {
			mismatches = append(mismatches, "ParentHeader.Collateral")
		}
	}

	if a.Height != b.Height {
		mismatches = append(mismatches, "Height")
	}
	if !h1BindingParamsMatch(a.Params, b.Params) {
		mismatches = append(mismatches, "Params")
	}
	return mismatches
}

func h1BindingParamsMatch(a, b *chaincfg.Params) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.H1DenominatorActivationStartHeight == b.H1DenominatorActivationStartHeight &&
		a.H1DenominatorActivationStopHeight == b.H1DenominatorActivationStopHeight
}

func h1RequireMismatch(t *testing.T, mismatches []string, field string) {
	t.Helper()
	for _, mismatch := range mismatches {
		if mismatch == field {
			return
		}
	}
	t.Fatalf("mismatches %v did not include %s", mismatches, field)
}

func h1RequireBuilderError(t *testing.T, name string, got effectiveH1Bindings, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected builder error", name)
	}
	var zero effectiveH1Bindings
	if got != zero {
		t.Fatalf("%s: got non-zero bindings after error: %+v", name, got)
	}
}

func h1CallSiteParams(start, stop int32) *chaincfg.Params {
	params := chaincfg.MainNetParams
	params.H1DenominatorActivationStartHeight = start
	params.H1DenominatorActivationStopHeight = stop
	return &params
}

func h1CallSiteHeader(collateral uint32, nonce int32) *wire.MingingRightBlock {
	return &wire.MingingRightBlock{
		Version:    0x20000,
		Timestamp:  time.Unix(1700000000+int64(nonce), 0),
		Bits:       0x207fffff,
		Nonce:      nonce,
		Collateral: collateral,
	}
}

func h1CallSiteNode(height int32, header *wire.MingingRightBlock) *chainutil.BlockNode {
	return &chainutil.BlockNode{
		Hash:   header.BlockHash(),
		Height: height,
		Data:   &blockchainNodeData{block: header},
	}
}

type h1WrongNodeData struct{}

func (h1WrongNodeData) TimeStamp() int64 {
	return 0
}

func (h1WrongNodeData) GetNonce() int32 {
	return 0
}

func (h1WrongNodeData) GetBits() uint32 {
	return 0
}

func (h1WrongNodeData) SetBits(uint32) {}

func (h1WrongNodeData) GetVersion() uint32 {
	return 0
}

func (h1WrongNodeData) GetContractExec() uint32 {
	return 0
}
