package minerchain

import (
	"fmt"

	"btcd/blockchain/chainutil"
	"btcd/chaincfg"
	"btcd/mining"
	"btcd/wire"
)

type effectiveH1Bindings struct {
	Header       *wire.MingingRightBlock
	ParentHeader *wire.MingingRightBlock
	Height       int32
	Params       *chaincfg.Params
}

func (b effectiveH1Bindings) Resolve() (uint32, error) {
	return EffectiveH1Collateral(b.Header, b.ParentHeader, b.Height, b.Params)
}

func acceptH1Bindings(
	header *wire.MingingRightBlock,
	parentNode *chainutil.BlockNode,
	params *chaincfg.Params,
) (effectiveH1Bindings, error) {
	var zero effectiveH1Bindings
	if parentNode == nil {
		return zero, fmt.Errorf("acceptH1Bindings: nil parent node")
	}
	data, ok := parentNode.Data.(*blockchainNodeData)
	if !ok || data == nil || data.block == nil {
		return zero, fmt.Errorf("acceptH1Bindings: parent node has unexpected data shape")
	}
	return effectiveH1Bindings{
		Header:       header,
		ParentHeader: data.block,
		Height:       parentNode.Height + 1,
		Params:       params,
	}, nil
}

func solverH1Bindings(
	template *mining.BlockTemplate,
	chainChoice *chainutil.BlockNode,
	params *chaincfg.Params,
) (effectiveH1Bindings, error) {
	var zero effectiveH1Bindings
	if template == nil {
		return zero, fmt.Errorf("solverH1Bindings: nil template")
	}
	header, ok := template.Block.(*wire.MingingRightBlock)
	if !ok || header == nil {
		return zero, fmt.Errorf("solverH1Bindings: template.Block is not *wire.MingingRightBlock")
	}
	if chainChoice == nil {
		return zero, fmt.Errorf("solverH1Bindings: nil chainChoice")
	}
	data, ok := chainChoice.Data.(*blockchainNodeData)
	if !ok || data == nil || data.block == nil {
		return zero, fmt.Errorf("solverH1Bindings: chainChoice has unexpected data shape")
	}
	return effectiveH1Bindings{
		Header:       header,
		ParentHeader: data.block,
		Height:       template.Height,
		Params:       params,
	}, nil
}

func h1DenominatorScheduleActive(height int32, params *chaincfg.Params) bool {
	if params == nil {
		return false
	}

	start := params.H1DenominatorActivationStartHeight
	stop := params.H1DenominatorActivationStopHeight
	return start > 0 && height >= start && (stop == 0 || height < stop)
}

// EffectiveH1Collateral returns the denominator used for the miner-chain h1
// factor under the configured non-retroactive start/stop schedule.
func EffectiveH1Collateral(header, parentHeader *wire.MingingRightBlock, height int32, params *chaincfg.Params) (uint32, error) {
	if params == nil {
		return 0, fmt.Errorf("h1_schedule: missing chain params at height %d", height)
	}

	var c uint32
	if h1DenominatorScheduleActive(height, params) {
		if parentHeader == nil {
			return 0, fmt.Errorf("h1_schedule: missing parent header for active interval at height %d", height)
		}
		c = parentHeader.Collateral
	} else {
		if header == nil {
			return 0, fmt.Errorf("h1_schedule: missing current header for legacy interval at height %d", height)
		}
		c = header.Collateral
	}

	if c == 0 {
		c = 1
	}
	return c, nil
}
