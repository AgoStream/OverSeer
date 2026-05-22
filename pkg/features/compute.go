package features

import (
	"time"

	"github.com/overseer/overseer/pkg/pmu"
)

// CoreSample holds accumulated PMU counter deltas for one logical core over a
// measurement window, along with the perf_event_open group multiplexing fields
// that allow scaling when the kernel time-multiplexed the counter group.
type CoreSample struct {
	CoreID int
	// Counts holds the raw counter deltas (end − start) for this core over the window.
	Counts map[pmu.Event]uint64
	// TimeEnabled is the duration in nanoseconds the event group was enabled by the kernel.
	// Zero means multiplexing metadata is unavailable; no scaling is applied.
	TimeEnabled uint64
	// TimeRunning is the duration in nanoseconds the event group was actually counting.
	// When TimeRunning < TimeEnabled the kernel was multiplexing; counts are scaled up
	// by TimeEnabled/TimeRunning before aggregation.
	TimeRunning uint64
}

// WindowSamples is the input to Compute: per-core PMU counter deltas accumulated
// over a single sampling window. Cores may carry multiplexing metadata for scaling.
type WindowSamples struct {
	Start    time.Time
	Duration time.Duration
	Cores    []CoreSample
}

// Compute derives a FeatureVector from raw per-core PMU counter samples collected
// over w. Each core's counts are scaled by TimeEnabled/TimeRunning when the kernel
// reports PMU multiplexing; cores without that metadata (TimeRunning == 0, or equal
// to TimeEnabled) are aggregated as-is. All derived ratios guard against zero
// denominators by returning 0 rather than NaN or ±Inf.
func Compute(w WindowSamples) FeatureVector {
	var (
		instructions float64
		cycles       float64
		l1Miss       float64
		llcMiss      float64
		dramStall    float64
	)

	numCores := len(w.Cores)
	for _, c := range w.Cores {
		s := coreSF(c)
		instructions += s * float64(c.Counts[pmu.EventInstructions])
		cycles += s * float64(c.Counts[pmu.EventCycles])
		l1Miss += s * float64(c.Counts[pmu.EventL1Miss])
		llcMiss += s * float64(c.Counts[pmu.EventLLCMiss])
		dramStall += s * float64(c.Counts[pmu.EventDRAMStall])
	}

	fv := FeatureVector{
		WindowStart:    w.Start,
		WindowDuration: w.Duration,
		// RawCounters stores the scaled aggregate estimates for auditability.
		RawCounters: map[string]uint64{
			string(pmu.EventInstructions): uint64(instructions),
			string(pmu.EventCycles):       uint64(cycles),
			string(pmu.EventL1Miss):       uint64(l1Miss),
			string(pmu.EventLLCMiss):      uint64(llcMiss),
			string(pmu.EventDRAMStall):    uint64(dramStall),
		},
	}

	if instructions > 0 {
		fv.LLCMissRate = llcMiss / instructions
		fv.L1ReplacementsPerKI = l1Miss / instructions * 1000
	}
	if cycles > 0 {
		fv.DRAMStallRatio = dramStall / cycles
		fv.IPC = instructions / cycles
	}

	// Effective frequency: cycles per core per second expressed in GHz.
	// Captured for diagnostic traceability only — excluded from ModelFeatures.
	windowSec := w.Duration.Seconds()
	if numCores > 0 && windowSec > 0 {
		fv.EffectiveFreqGHz = cycles / float64(numCores) / windowSec / 1e9
	}

	return fv
}

// coreSF returns the multiplexing scale factor for a CoreSample.
// Returns 1.0 when metadata is absent or when the counter group ran without interruption.
func coreSF(c CoreSample) float64 {
	if c.TimeRunning == 0 || c.TimeRunning >= c.TimeEnabled {
		return 1.0
	}
	return float64(c.TimeEnabled) / float64(c.TimeRunning)
}
