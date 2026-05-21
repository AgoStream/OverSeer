package pmu

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// TraceFrame is one recorded counter snapshot, typically for a single core.
// It is the unit of storage in replay trace files (newline-delimited JSON).
type TraceFrame struct {
	CoreID int               `json:"core_id"`
	Counts map[string]uint64 `json:"counts"` // keyed by Event string (e.g. "LLC_MISS")
}

// ReplayBackend implements CounterSource by replaying pre-recorded TraceFrames.
// It validates event names against the same encodingTable as PerfBackend so that
// unknown-uarch and unknown-event errors are exercisable in tests without hardware.
//
// Frames are served round-robin: each Read call consumes one frame per requested
// core, advancing a per-handle cursor that wraps when the frame slice is exhausted.
// With cadence == 0 (recommended for unit tests), Read returns immediately.
type ReplayBackend struct {
	uarch   string
	frames  []TraceFrame
	cadence time.Duration

	mu      sync.Mutex
	nextID  uint64
	handles map[Handle]*replayState
}

type replayState struct {
	coreIDs []int
	events  []Event
	mu      sync.Mutex
	pos     int
}

// NewReplayBackend creates a ReplayBackend for the given microarchitecture name.
// frames is the ordered sequence of snapshots to replay; it may be nil (zeros are
// returned). cadence adds an artificial delay per Read; use 0 in tests.
func NewReplayBackend(uarch string, frames []TraceFrame, cadence time.Duration) *ReplayBackend {
	return &ReplayBackend{
		uarch:   uarch,
		frames:  append([]TraceFrame(nil), frames...),
		cadence: cadence,
		handles: make(map[Handle]*replayState),
	}
}

// LoadTraceFile reads newline-delimited JSON TraceFrames from path.
// Use the result as the frames argument to NewReplayBackend.
func LoadTraceFile(path string) ([]TraceFrame, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("pmu replay: %w", err)
	}
	defer f.Close()
	var frames []TraceFrame
	dec := json.NewDecoder(f)
	for dec.More() {
		var fr TraceFrame
		if err := dec.Decode(&fr); err != nil {
			return nil, fmt.Errorf("pmu replay: decode %s: %w", path, err)
		}
		frames = append(frames, fr)
	}
	return frames, nil
}

// Open implements CounterSource. It validates events against the encoding table
// for r.uarch and returns ErrUnknownEvents on mismatch — identical behaviour to
// PerfBackend.Open so tests cover the real error path.
func (r *ReplayBackend) Open(coreIDs []int, events []Event) (Handle, error) {
	if _, err := lookupEncodings(r.uarch, events); err != nil {
		return Handle{}, err
	}
	state := &replayState{
		coreIDs: append([]int(nil), coreIDs...),
		events:  append([]Event(nil), events...),
	}
	h := Handle{id: atomic.AddUint64(&r.nextID, 1)}
	r.mu.Lock()
	r.handles[h] = state
	r.mu.Unlock()
	return h, nil
}

// Read returns the next round of frames, one per requested core, advancing the
// per-handle cursor. Frames cycle when exhausted. If no frames were provided,
// all counters read as zero.
func (r *ReplayBackend) Read(h Handle) ([]RawSample, error) {
	r.mu.Lock()
	state, ok := r.handles[h]
	r.mu.Unlock()
	if !ok {
		return nil, ErrHandleNotFound
	}
	if r.cadence > 0 {
		time.Sleep(r.cadence)
	}
	state.mu.Lock()
	defer state.mu.Unlock()

	samples := make([]RawSample, len(state.coreIDs))
	for i, cid := range state.coreIDs {
		counts := make(map[Event]uint64, len(state.events))
		if len(r.frames) > 0 {
			frame := r.frames[state.pos%len(r.frames)]
			state.pos++
			for _, ev := range state.events {
				counts[ev] = frame.Counts[string(ev)]
			}
		}
		// When frames is empty, map access returns zero — intentional.
		samples[i] = RawSample{CoreID: cid, Counts: counts}
	}
	return samples, nil
}

// Close implements CounterSource.
func (r *ReplayBackend) Close(h Handle) error {
	r.mu.Lock()
	_, ok := r.handles[h]
	if ok {
		delete(r.handles, h)
	}
	r.mu.Unlock()
	if !ok {
		return ErrHandleNotFound
	}
	return nil
}
