package features_test

import (
	"math"
	"testing"
	"time"

	"github.com/overseer/overseer/pkg/features"
	"github.com/overseer/overseer/pkg/pmu"
)

// Hand-computed expected values for every case are derived in the comments below
// so a reader can verify correctness without running the code.

func checkApprox(t *testing.T, field string, want, got, eps float64) {
	t.Helper()
	if math.Abs(got-want) > eps {
		t.Errorf("%s: want %.15g got %.15g (diff %.3e)", field, want, got, math.Abs(got-want))
	}
}

func TestCompute(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	dur100ms := 100 * time.Millisecond // 0.1 s
	dur200ms := 200 * time.Millisecond // 0.2 s

	cases := []struct {
		name string
		in   features.WindowSamples
		want features.FeatureVector // only causal + diagnostic fields checked; RawCounters verified separately
	}{
		{
			// Single core, no multiplexing.
			// INSTRUCTIONS=1_000_000, CYCLES=2_000_000, L1_MISS=50_000, LLC_MISS=20_000, DRAM_STALL=300_000
			// LLCMissRate          = 20_000 / 1_000_000                = 0.02
			// DRAMStallRatio       = 300_000 / 2_000_000               = 0.15
			// L1ReplacementsPerKI  = 50_000 / 1_000_000 * 1000         = 50.0
			// IPC                  = 1_000_000 / 2_000_000             = 0.5
			// EffectiveFreqGHz     = 2_000_000 / (1 core * 0.1 s * 1e9) = 0.02
			name: "single_core_no_mux",
			in: features.WindowSamples{
				Start:    now,
				Duration: dur100ms,
				Cores: []features.CoreSample{
					{
						CoreID: 0,
						Counts: map[pmu.Event]uint64{
							pmu.EventInstructions: 1_000_000,
							pmu.EventCycles:       2_000_000,
							pmu.EventL1Miss:       50_000,
							pmu.EventLLCMiss:      20_000,
							pmu.EventDRAMStall:    300_000,
						},
					},
				},
			},
			want: features.FeatureVector{
				WindowStart:         now,
				WindowDuration:      dur100ms,
				LLCMissRate:         0.02,
				DRAMStallRatio:      0.15,
				L1ReplacementsPerKI: 50.0,
				IPC:                 0.5,
				EffectiveFreqGHz:    0.02,
			},
		},
		{
			// Two cores, no multiplexing.
			// Core 0: INST=1_000_000, CYC=1_500_000, L1=30_000, LLC=10_000, DRAM=150_000
			// Core 1: INST=2_000_000, CYC=2_500_000, L1=70_000, LLC=30_000, DRAM=250_000
			// Totals: INST=3_000_000, CYC=4_000_000, L1=100_000, LLC=40_000, DRAM=400_000
			// LLCMissRate          = 40_000 / 3_000_000               = 0.013333…
			// DRAMStallRatio       = 400_000 / 4_000_000              = 0.1
			// L1ReplacementsPerKI  = 100_000 / 3_000_000 * 1000       = 33.333…
			// IPC                  = 3_000_000 / 4_000_000            = 0.75
			// EffectiveFreqGHz     = 4_000_000 / (2 * 0.2 * 1e9)     = 0.01
			name: "two_cores_no_mux",
			in: features.WindowSamples{
				Start:    now,
				Duration: dur200ms,
				Cores: []features.CoreSample{
					{
						CoreID: 0,
						Counts: map[pmu.Event]uint64{
							pmu.EventInstructions: 1_000_000,
							pmu.EventCycles:       1_500_000,
							pmu.EventL1Miss:       30_000,
							pmu.EventLLCMiss:      10_000,
							pmu.EventDRAMStall:    150_000,
						},
					},
					{
						CoreID: 1,
						Counts: map[pmu.Event]uint64{
							pmu.EventInstructions: 2_000_000,
							pmu.EventCycles:       2_500_000,
							pmu.EventL1Miss:       70_000,
							pmu.EventLLCMiss:      30_000,
							pmu.EventDRAMStall:    250_000,
						},
					},
				},
			},
			want: features.FeatureVector{
				WindowStart:         now,
				WindowDuration:      dur200ms,
				LLCMissRate:         40_000.0 / 3_000_000.0,
				DRAMStallRatio:      0.1,
				L1ReplacementsPerKI: 100_000.0 / 3_000_000.0 * 1000.0,
				IPC:                 0.75,
				EffectiveFreqGHz:    0.01,
			},
		},
		{
			// Single core, PMU multiplexing: TimeRunning = 50 ms, TimeEnabled = 100 ms.
			// Scale factor = 100 / 50 = 2.0 applied to all raw counts.
			// Raw: INST=500_000, CYC=750_000, L1=10_000, LLC=5_000, DRAM=100_000
			// Scaled: INST=1_000_000, CYC=1_500_000, L1=20_000, LLC=10_000, DRAM=200_000
			// LLCMissRate          = 10_000 / 1_000_000               = 0.01
			// DRAMStallRatio       = 200_000 / 1_500_000              = 0.13333…
			// L1ReplacementsPerKI  = 20_000 / 1_000_000 * 1000        = 20.0
			// IPC                  = 1_000_000 / 1_500_000            = 0.66666…
			// EffectiveFreqGHz     = 1_500_000 / (1 * 0.1 * 1e9)     = 0.015
			name: "single_core_partial_window_mux",
			in: features.WindowSamples{
				Start:    now,
				Duration: dur100ms,
				Cores: []features.CoreSample{
					{
						CoreID: 0,
						Counts: map[pmu.Event]uint64{
							pmu.EventInstructions: 500_000,
							pmu.EventCycles:       750_000,
							pmu.EventL1Miss:       10_000,
							pmu.EventLLCMiss:      5_000,
							pmu.EventDRAMStall:    100_000,
						},
						TimeEnabled: 100_000_000, // 100 ms in ns
						TimeRunning: 50_000_000,  // 50 ms in ns  →  scale = 2.0
					},
				},
			},
			want: features.FeatureVector{
				WindowStart:         now,
				WindowDuration:      dur100ms,
				LLCMissRate:         0.01,
				DRAMStallRatio:      200_000.0 / 1_500_000.0,
				L1ReplacementsPerKI: 20.0,
				IPC:                 1_000_000.0 / 1_500_000.0,
				EffectiveFreqGHz:    0.015,
			},
		},
		{
			// Zero instructions: LLC miss rate and L1 RPKI must be 0, not NaN/Inf.
			// IPC and DRAMStallRatio are still computable from cycles.
			// INST=0, CYC=1_000_000, L1=0, LLC=0, DRAM=200_000
			// LLCMissRate=0, L1RPKI=0, DRAMStallRatio=0.2, IPC=0, EffFreq=0.01
			name: "zero_instructions_no_div_by_zero",
			in: features.WindowSamples{
				Start:    now,
				Duration: dur100ms,
				Cores: []features.CoreSample{
					{
						CoreID: 0,
						Counts: map[pmu.Event]uint64{
							pmu.EventInstructions: 0,
							pmu.EventCycles:       1_000_000,
							pmu.EventL1Miss:       0,
							pmu.EventLLCMiss:      0,
							pmu.EventDRAMStall:    200_000,
						},
					},
				},
			},
			want: features.FeatureVector{
				WindowStart:         now,
				WindowDuration:      dur100ms,
				LLCMissRate:         0,
				DRAMStallRatio:      0.2,
				L1ReplacementsPerKI: 0,
				IPC:                 0,
				EffectiveFreqGHz:    0.01,
			},
		},
		{
			// Completely empty core list: all derived values must be zero, no panic.
			name: "no_cores",
			in: features.WindowSamples{
				Start:    now,
				Duration: dur100ms,
				Cores:    nil,
			},
			want: features.FeatureVector{
				WindowStart:    now,
				WindowDuration: dur100ms,
			},
		},
		{
			// No-op multiplexing: TimeRunning == TimeEnabled → scale factor 1.0 (same as no mux).
			// INST=1_000_000, CYC=2_000_000, LLC=20_000 → LLCMissRate=0.02
			name: "mux_metadata_full_window_no_scaling",
			in: features.WindowSamples{
				Start:    now,
				Duration: dur100ms,
				Cores: []features.CoreSample{
					{
						CoreID: 0,
						Counts: map[pmu.Event]uint64{
							pmu.EventInstructions: 1_000_000,
							pmu.EventCycles:       2_000_000,
							pmu.EventL1Miss:       0,
							pmu.EventLLCMiss:      20_000,
							pmu.EventDRAMStall:    0,
						},
						TimeEnabled: 100_000_000,
						TimeRunning: 100_000_000, // equal → scale = 1.0
					},
				},
			},
			want: features.FeatureVector{
				WindowStart:         now,
				WindowDuration:      dur100ms,
				LLCMissRate:         0.02,
				DRAMStallRatio:      0,
				L1ReplacementsPerKI: 0,
				IPC:                 0.5,
				EffectiveFreqGHz:    0.02,
			},
		},
	}

	const epsilon = 1e-9

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := features.Compute(tc.in)

			// Window metadata.
			if !got.WindowStart.Equal(tc.want.WindowStart) {
				t.Errorf("WindowStart: want %v got %v", tc.want.WindowStart, got.WindowStart)
			}
			if got.WindowDuration != tc.want.WindowDuration {
				t.Errorf("WindowDuration: want %v got %v", tc.want.WindowDuration, got.WindowDuration)
			}

			// Causal features.
			checkApprox(t, "LLCMissRate", tc.want.LLCMissRate, got.LLCMissRate, epsilon)
			checkApprox(t, "DRAMStallRatio", tc.want.DRAMStallRatio, got.DRAMStallRatio, epsilon)
			checkApprox(t, "L1ReplacementsPerKI", tc.want.L1ReplacementsPerKI, got.L1ReplacementsPerKI, epsilon)
			checkApprox(t, "IPC", tc.want.IPC, got.IPC, epsilon)

			// Diagnostic field: effective frequency (recorded but excluded from model).
			checkApprox(t, "EffectiveFreqGHz", tc.want.EffectiveFreqGHz, got.EffectiveFreqGHz, epsilon)

			// NaN/Inf guards: no derived value may be non-finite.
			for _, v := range []float64{
				got.LLCMissRate,
				got.DRAMStallRatio,
				got.L1ReplacementsPerKI,
				got.IPC,
				got.EffectiveFreqGHz,
			} {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					t.Errorf("derived field is non-finite: %v", v)
				}
			}

			// ModelFeatures must exclude the diagnostic field.
			mf := got.ModelFeatures()
			if mf.EffectiveFreqGHz != 0 {
				t.Errorf("ModelFeatures: EffectiveFreqGHz must be 0, got %v", mf.EffectiveFreqGHz)
			}
			if mf.RawCounters != nil {
				t.Errorf("ModelFeatures: RawCounters must be nil, got %v", mf.RawCounters)
			}
		})
	}
}

// TestComputeRawCountersMux verifies that RawCounters in the output holds the scaled
// (estimated) aggregate counts, not the raw unscaled values.
func TestComputeRawCountersMux(t *testing.T) {
	t.Parallel()

	// Scale factor = 4: TimeEnabled=200ms, TimeRunning=50ms.
	// Raw CYCLES=250_000 → scaled estimate = 1_000_000.
	w := features.WindowSamples{
		Start:    time.Now(),
		Duration: 200 * time.Millisecond,
		Cores: []features.CoreSample{
			{
				CoreID: 0,
				Counts: map[pmu.Event]uint64{
					pmu.EventInstructions: 250_000,
					pmu.EventCycles:       250_000,
					pmu.EventL1Miss:       0,
					pmu.EventLLCMiss:      0,
					pmu.EventDRAMStall:    0,
				},
				TimeEnabled: 200_000_000,
				TimeRunning: 50_000_000, // scale = 4.0
			},
		},
	}
	got := features.Compute(w)
	wantCycles := uint64(1_000_000)
	if got.RawCounters[string(pmu.EventCycles)] != wantCycles {
		t.Errorf("RawCounters[CYCLES]: want %d got %d (scaling by 4 not applied)",
			wantCycles, got.RawCounters[string(pmu.EventCycles)])
	}
}
