package minerchain

import (
	"fmt"

	"btcd/chaincfg"
	"btcd/wire"
)

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
