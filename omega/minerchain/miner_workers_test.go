package minerchain

import (
	"testing"

	"btcd/chaincfg"
)

func TestMinerWorkerCount(t *testing.T) {
	tests := []struct {
		name               string
		minerWorkers       int
		sigVeriConcurrency int
		want               int
	}{
		{
			name:               "explicit workers override sigverify concurrency",
			minerWorkers:       4,
			sigVeriConcurrency: 8,
			want:               4,
		},
		{
			name:               "explicit one worker",
			minerWorkers:       1,
			sigVeriConcurrency: 8,
			want:               1,
		},
		{
			name:               "zero uses legacy fallback",
			minerWorkers:       0,
			sigVeriConcurrency: 8,
			want:               7,
		},
		{
			name:               "negative uses legacy fallback",
			minerWorkers:       -1,
			sigVeriConcurrency: 8,
			want:               7,
		},
		{
			name:               "fallback clamps low concurrency to one",
			minerWorkers:       0,
			sigVeriConcurrency: 1,
			want:               1,
		},
		{
			name:               "fallback clamps zero concurrency to one",
			minerWorkers:       0,
			sigVeriConcurrency: 0,
			want:               1,
		},
	}

	for _, test := range tests {
		params := &chaincfg.Params{SigVeriConcurrency: test.sigVeriConcurrency}
		cfg := &Config{
			ChainParams:  params,
			MinerWorkers: test.minerWorkers,
		}

		got := minerWorkerCount(cfg)
		if got != test.want {
			t.Fatalf("%s: got %d workers, want %d", test.name, got, test.want)
		}
	}
}
