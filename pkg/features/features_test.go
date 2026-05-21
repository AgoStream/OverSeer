package features_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/overseer/overseer/pkg/features"
)

func TestFeatureVectorRoundTrip(t *testing.T) {
	t.Parallel()

	// Fixed wall time with no monotonic reading — safe for DeepEqual after JSON round-trip.
	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		in   features.FeatureVector
	}{
		{
			name: "fully_populated",
			in: features.FeatureVector{
				WindowStart:         now,
				WindowDuration:      100 * time.Millisecond,
				LLCMissRate:         0.042,
				DRAMStallRatio:      0.18,
				L1ReplacementsPerKI: 3.7,
				IPC:                 1.23,
				EffectiveFreqGHz:    3.1,
				RawCounters: map[string]uint64{
					"LLC_MISSES":   9_000_000,
					"INST_RETIRED": 210_000_000,
					"CPU_CLK":      270_000_000,
				},
			},
		},
		{
			name: "no_freq_no_raw_counters",
			in: features.FeatureVector{
				WindowStart:         now,
				WindowDuration:      50 * time.Millisecond,
				LLCMissRate:         0.01,
				DRAMStallRatio:      0.05,
				L1ReplacementsPerKI: 1.2,
				IPC:                 2.4,
			},
		},
		{
			// Verify omitempty fields survive the zero case without spurious keys.
			name: "zero_value",
			in:   features.FeatureVector{},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var got features.FeatureVector
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if !reflect.DeepEqual(tc.in, got) {
				t.Errorf("round-trip mismatch\n want: %+v\n  got: %+v", tc.in, got)
			}
		})
	}
}

func TestModelFeaturesExcludesNonCausal(t *testing.T) {
	t.Parallel()

	v := features.FeatureVector{
		LLCMissRate:         0.05,
		DRAMStallRatio:      0.12,
		L1ReplacementsPerKI: 2.1,
		IPC:                 1.8,
		EffectiveFreqGHz:    3.2,
		RawCounters:         map[string]uint64{"LLC_MISSES": 1_000},
	}
	mf := v.ModelFeatures()

	if mf.EffectiveFreqGHz != 0 {
		t.Errorf("EffectiveFreqGHz must be zeroed in model view, got %v", mf.EffectiveFreqGHz)
	}
	if mf.RawCounters != nil {
		t.Errorf("RawCounters must be nil in model view, got %v", mf.RawCounters)
	}

	// Causal features must be preserved unchanged.
	if mf.LLCMissRate != v.LLCMissRate {
		t.Errorf("LLCMissRate changed: want %v got %v", v.LLCMissRate, mf.LLCMissRate)
	}
	if mf.DRAMStallRatio != v.DRAMStallRatio {
		t.Errorf("DRAMStallRatio changed: want %v got %v", v.DRAMStallRatio, mf.DRAMStallRatio)
	}
	if mf.IPC != v.IPC {
		t.Errorf("IPC changed: want %v got %v", v.IPC, mf.IPC)
	}

	// ModelFeatures must not mutate the original.
	if v.EffectiveFreqGHz != 3.2 {
		t.Error("ModelFeatures must not mutate the receiver")
	}
	if v.RawCounters == nil {
		t.Error("ModelFeatures must not mutate receiver RawCounters")
	}
}

func TestModelFeaturesJSONOmitsNonCausal(t *testing.T) {
	t.Parallel()

	v := features.FeatureVector{
		LLCMissRate:      0.03,
		EffectiveFreqGHz: 3.5,
		RawCounters:      map[string]uint64{"X": 42},
	}
	data, err := json.Marshal(v.ModelFeatures())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}
	if _, ok := m["effective_freq_ghz"]; ok {
		t.Error("effective_freq_ghz must be absent from model JSON (omitempty + zero)")
	}
	if _, ok := m["raw_counters"]; ok {
		t.Error("raw_counters must be absent from model JSON (omitempty + nil)")
	}
}
