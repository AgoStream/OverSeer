// Package interference defines DegradationScore: Component B's predicted KPI impact
// for a workload placed on a node in its current contention state.
//
// Combined hardware contention is non-additive and can sign-flip under saturation.
// DegradationScores must never be summed across workloads or contention sources.
package interference

// SourceKind describes how a DegradationScore was derived.
type SourceKind string

const (
	// SourceSingle indicates the score was looked up from a single isolated contention source.
	SourceSingle SourceKind = "single"
	// SourceStacked indicates the score was synthesised from multiple overlapping contention
	// sources and is an intentional upper-bound (conservative) estimate.
	// Stacked scores must not be further composed — contention effects are non-additive.
	SourceStacked SourceKind = "stacked"
)

// DegradationScore is Component B's predicted KPI impact of placing a specific workload
// on a node in its current contention state.
//
// KPIMultiplier is applied multiplicatively to a reference KPI baseline (e.g. p99 latency).
// 1.0 means no predicted change; 1.5 means 50% predicted degradation. Scores are always
// conservative (upper-bound); callers must not add them across workloads — combined
// contention is non-additive and can produce sign-flipping effects.
type DegradationScore struct {
	// WorkloadID identifies the target workload this score applies to.
	WorkloadID string `json:"workload_id"`
	// KPIMultiplier is the predicted multiplicative factor on a reference KPI.
	// Values above 1.0 indicate degradation; values below 1.0 indicate improvement.
	// The field is always a conservative (worst-case) estimate.
	KPIMultiplier float64 `json:"kpi_multiplier"`
	// Conservative is true when the score was derived from stacked contention sources
	// and intentionally represents an upper bound rather than a point estimate.
	Conservative bool `json:"conservative"`
	// Source describes whether the score originated from a single lookup or stacked inference.
	Source SourceKind `json:"source"`
}
