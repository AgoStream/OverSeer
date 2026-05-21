// Package pmu provides hardware performance counter collection behind a CounterSource
// interface so every other OverSeer package can be tested without real PMU hardware.
//
// Two backends are provided:
//   - PerfBackend  (perf_linux.go, build tag linux): perf_event_open via golang.org/x/sys/unix.
//   - ReplayBackend (replay.go): reads pre-recorded counter traces; the default for tests.
//
// Event names are symbolic ("LLC_MISS", "DRAM_STALL", …) and resolved to
// per-uarch perf_event_attr encodings via an internal table (events.go). Requesting
// an event that has no encoding for the detected uarch returns ErrUnknownEvents with
// the full list of unresolvable names — never silent zeros.
package pmu

import (
	"errors"
	"fmt"
	"strings"
)

// Event is a symbolic hardware performance counter name.
type Event string

// Supported symbolic event names. The mapping to hardware encodings lives in events.go.
const (
	EventInstructions Event = "INSTRUCTIONS"
	EventCycles       Event = "CYCLES"
	EventL1Miss       Event = "L1_MISS"
	EventLLCMiss      Event = "LLC_MISS"
	EventDRAMStall    Event = "DRAM_STALL"
)

// RawSample is a single core's counter snapshot returned by CounterSource.Read.
type RawSample struct {
	CoreID int
	Counts map[Event]uint64
}

// Handle is an opaque token issued by CounterSource.Open.
// It must be passed unchanged to Read and Close. The zero value is invalid.
type Handle struct{ id uint64 }

// CounterSource abstracts PMU counter collection. Implementations must be safe
// for concurrent use by a single goroutine driving Open/Read/Close sequentially;
// they need not be safe for concurrent calls on the same Handle.
type CounterSource interface {
	// Open arms the source for the given logical cores and symbolic events.
	// Returns ErrUnknownEvents if any event lacks an encoding for this uarch.
	// The caller must eventually call Close on the returned Handle.
	Open(coreIDs []int, events []Event) (Handle, error)

	// Read returns the current raw counter values for every core in h.
	// Within each core, counters are read as a group (atomically for PerfBackend).
	Read(h Handle) ([]RawSample, error)

	// Close releases kernel resources associated with h.
	Close(h Handle) error
}

// ErrUnknownEvents is returned by Open when one or more events have no encoding
// for the microarchitecture seen by this backend. Missing events are listed
// explicitly so operators can diagnose misconfiguration rather than receive zeros.
type ErrUnknownEvents struct {
	Uarch   string
	Missing []Event
}

func (e *ErrUnknownEvents) Error() string {
	names := make([]string, len(e.Missing))
	for i, ev := range e.Missing {
		names[i] = string(ev)
	}
	return fmt.Sprintf("pmu: uarch %q has no encoding for: %s",
		e.Uarch, strings.Join(names, ", "))
}

// ErrHandleNotFound is returned when a Handle passed to Read or Close is not
// known to the backend (already closed, or from a different backend instance).
var ErrHandleNotFound = errors.New("pmu: handle not found")
