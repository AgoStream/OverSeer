// Package pmu collects and normalizes PMU hardware counter telemetry.
// Per-node event availability differs; callers must check EventSupported before use.
package pmu

// EventSupported reports whether the named PMU event is available on this node.
// Real implementation will probe the perf_event subsystem; placeholder always returns false.
func EventSupported(_ string) bool { return false }
