// Package features defines the FeatureVector schema: the per-workload PMU-derived
// observation produced by the node agent and consumed by the training pipeline and
// Component B (pkg/regime). This schema is a data contract; changes require
// cross-component review before any producer or consumer is modified.
package features

import "time"

// FeatureVector holds PMU-derived measurements for a single workload over one sampling window.
//
// Effective CPU frequency is captured for diagnostic traceability but is NOT a model input —
// it is a utilization confound rather than a causal regime signal. Use ModelFeatures to
// obtain the inference-safe projection that excludes it.
type FeatureVector struct {
	// WindowStart is the wall-clock start of the measurement window (UTC).
	WindowStart time.Time `json:"window_start"`
	// WindowDuration is the length of the sampling window expressed as nanoseconds.
	// Use time.Duration arithmetic; divide by time.Millisecond etc. for display.
	WindowDuration time.Duration `json:"window_dur_ns"`

	// LLCMissRate is LLC misses divided by retired instructions over the window.
	LLCMissRate float64 `json:"llc_miss_rate"`
	// DRAMStallRatio is DRAM-stall cycles divided by total cycles over the window.
	DRAMStallRatio float64 `json:"dram_stall_ratio"`
	// L1ReplacementsPerKI is L1D cache line replacements per 1 000 retired instructions.
	L1ReplacementsPerKI float64 `json:"l1_repl_per_ki"`
	// IPC is instructions retired per cycle.
	IPC float64 `json:"ipc"`

	// EffectiveFreqGHz is the observed effective CPU frequency in GHz.
	// DIAGNOSTIC ONLY — excluded from the model feature surface by ModelFeatures.
	// Frequency is a utilization confound and must not be used as a regime predictor.
	EffectiveFreqGHz float64 `json:"effective_freq_ghz,omitempty"`

	// RawCounters maps perf_event counter name to the raw 64-bit hardware value sampled
	// during the window. Retained for auditability and counter-derivation verification;
	// not part of the model feature surface.
	RawCounters map[string]uint64 `json:"raw_counters,omitempty"`
}

// ModelFeatures returns a copy of v with all non-causal fields zeroed so that training
// and inference code cannot accidentally consume them. Specifically:
//   - EffectiveFreqGHz is zeroed (utilization confound, not a regime signal).
//   - RawCounters is cleared (provenance data, not a model input).
//
// ModelFeatures does not mutate the receiver.
func (v FeatureVector) ModelFeatures() FeatureVector {
	v.EffectiveFreqGHz = 0
	v.RawCounters = nil
	return v
}
