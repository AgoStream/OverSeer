package pmu_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/overseer/overseer/pkg/pmu"
)

// testFrames provides two recorded snapshots — one per core — used across multiple tests.
var testFrames = []pmu.TraceFrame{
	{CoreID: 0, Counts: map[string]uint64{
		"INSTRUCTIONS": 1_000_000,
		"CYCLES":       800_000,
		"LLC_MISS":     5_000,
		"L1_MISS":      20_000,
		"DRAM_STALL":   15_000,
	}},
	{CoreID: 1, Counts: map[string]uint64{
		"INSTRUCTIONS": 900_000,
		"CYCLES":       750_000,
		"LLC_MISS":     3_000,
		"L1_MISS":      18_000,
		"DRAM_STALL":   12_000,
	}},
}

var allEvents = []pmu.Event{
	pmu.EventInstructions,
	pmu.EventCycles,
	pmu.EventLLCMiss,
	pmu.EventL1Miss,
	pmu.EventDRAMStall,
}

// newSPR returns a ReplayBackend configured for Sapphire Rapids with testFrames.
func newSPR(frames []pmu.TraceFrame) *pmu.ReplayBackend {
	return pmu.NewReplayBackend("sapphire_rapids", frames, 0)
}

// --- Basic open / read / close ---

func TestReplayOpenAndRead(t *testing.T) {
	t.Parallel()
	src := newSPR(testFrames)

	h, err := src.Open([]int{0, 1}, allEvents)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer src.Close(h)

	samples, err := src.Read(h)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(samples) != 2 {
		t.Fatalf("want 2 samples, got %d", len(samples))
	}

	// Frame 0 maps to core 0 (first frame consumed for the first core).
	wantInstr := testFrames[0].Counts["INSTRUCTIONS"]
	if got := samples[0].Counts[pmu.EventInstructions]; got != wantInstr {
		t.Errorf("core 0 INSTRUCTIONS: want %d got %d", wantInstr, got)
	}
	// Frame 1 maps to core 1.
	wantLLC := testFrames[1].Counts["LLC_MISS"]
	if got := samples[1].Counts[pmu.EventLLCMiss]; got != wantLLC {
		t.Errorf("core 1 LLC_MISS: want %d got %d", wantLLC, got)
	}
}

func TestReplayAllEventsPresent(t *testing.T) {
	t.Parallel()
	src := newSPR(testFrames)
	h, err := src.Open([]int{0}, allEvents)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer src.Close(h)

	samples, err := src.Read(h)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	for _, ev := range allEvents {
		if _, ok := samples[0].Counts[ev]; !ok {
			t.Errorf("event %s missing from sample", ev)
		}
	}
}

// --- Frame cycling ---

func TestReplayFramesCycle(t *testing.T) {
	t.Parallel()
	frames := []pmu.TraceFrame{
		{CoreID: 0, Counts: map[string]uint64{"INSTRUCTIONS": 100, "CYCLES": 90}},
		{CoreID: 0, Counts: map[string]uint64{"INSTRUCTIONS": 200, "CYCLES": 180}},
	}
	src := pmu.NewReplayBackend("sapphire_rapids", frames, 0)
	h, err := src.Open([]int{0}, []pmu.Event{pmu.EventInstructions, pmu.EventCycles})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer src.Close(h)

	// Reads 0, 1, 2 should give frame 0, frame 1, frame 0 (wrap).
	wantInstr := []uint64{100, 200, 100}
	for i, want := range wantInstr {
		samples, err := src.Read(h)
		if err != nil {
			t.Fatalf("Read %d: %v", i, err)
		}
		got := samples[0].Counts[pmu.EventInstructions]
		if got != want {
			t.Errorf("Read %d: INSTRUCTIONS want %d got %d", i, want, got)
		}
	}
}

func TestReplayNoFramesReturnsZeros(t *testing.T) {
	t.Parallel()
	src := pmu.NewReplayBackend("sapphire_rapids", nil, 0)
	h, err := src.Open([]int{0, 1}, []pmu.Event{pmu.EventCycles})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer src.Close(h)

	samples, err := src.Read(h)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	for _, s := range samples {
		if v := s.Counts[pmu.EventCycles]; v != 0 {
			t.Errorf("core %d: expected 0 cycles with no frames, got %d", s.CoreID, v)
		}
	}
}

// --- Unknown uarch error path ---

func TestUnknownUarchReturnsErrUnknownEvents(t *testing.T) {
	t.Parallel()
	src := pmu.NewReplayBackend("arm_neoverse_n1", testFrames, 0)
	_, err := src.Open([]int{0}, allEvents)
	if err == nil {
		t.Fatal("expected ErrUnknownEvents, got nil")
	}
	var ue *pmu.ErrUnknownEvents
	if !errors.As(err, &ue) {
		t.Fatalf("expected *ErrUnknownEvents, got %T: %v", err, err)
	}
	if ue.Uarch != "arm_neoverse_n1" {
		t.Errorf("Uarch: want arm_neoverse_n1 got %q", ue.Uarch)
	}
	// All requested events must be listed because the uarch itself is unknown.
	if len(ue.Missing) == 0 {
		t.Error("Missing must be non-empty for an unknown uarch")
	}
}

func TestUnknownEventsOnKnownUarch(t *testing.T) {
	t.Parallel()
	src := newSPR(testFrames)
	badEvents := []pmu.Event{"BRANCH_MISPRED", "STORE_FORWARD_BLOCK"}
	_, err := src.Open([]int{0}, badEvents)
	if err == nil {
		t.Fatal("expected ErrUnknownEvents, got nil")
	}
	var ue *pmu.ErrUnknownEvents
	if !errors.As(err, &ue) {
		t.Fatalf("expected *ErrUnknownEvents, got %T: %v", err, err)
	}
	if ue.Uarch != "sapphire_rapids" {
		t.Errorf("Uarch: want sapphire_rapids got %q", ue.Uarch)
	}
	if len(ue.Missing) != 2 {
		t.Errorf("want 2 missing events, got %d: %v", len(ue.Missing), ue.Missing)
	}
	// Error message must name the unknown events.
	msg := err.Error()
	for _, ev := range badEvents {
		if !containsStr(msg, string(ev)) {
			t.Errorf("error message %q does not mention event %s", msg, ev)
		}
	}
}

func TestMixedKnownAndUnknownEvents(t *testing.T) {
	t.Parallel()
	src := newSPR(testFrames)
	mixed := []pmu.Event{pmu.EventInstructions, "MYSTERY_COUNTER", pmu.EventCycles, "ANOTHER_MISSING"}
	_, err := src.Open([]int{0}, mixed)
	if err == nil {
		t.Fatal("expected ErrUnknownEvents, got nil")
	}
	var ue *pmu.ErrUnknownEvents
	if !errors.As(err, &ue) {
		t.Fatalf("expected *ErrUnknownEvents, got %T", err)
	}
	if len(ue.Missing) != 2 {
		t.Errorf("want 2 missing events, got %d: %v", len(ue.Missing), ue.Missing)
	}
}

// --- Handle lifecycle ---

func TestDoubleCloseReturnsErrHandleNotFound(t *testing.T) {
	t.Parallel()
	src := newSPR(testFrames)
	h, err := src.Open([]int{0}, []pmu.Event{pmu.EventCycles})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := src.Close(h); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := src.Close(h); !errors.Is(err, pmu.ErrHandleNotFound) {
		t.Errorf("second Close: want ErrHandleNotFound, got %v", err)
	}
}

func TestReadAfterCloseReturnsErrHandleNotFound(t *testing.T) {
	t.Parallel()
	src := newSPR(testFrames)
	h, err := src.Open([]int{0}, []pmu.Event{pmu.EventCycles})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	src.Close(h)
	if _, err := src.Read(h); !errors.Is(err, pmu.ErrHandleNotFound) {
		t.Errorf("Read after Close: want ErrHandleNotFound, got %v", err)
	}
}

func TestMultipleIndependentHandles(t *testing.T) {
	t.Parallel()
	src := newSPR(testFrames)
	events := []pmu.Event{pmu.EventInstructions}

	h1, err := src.Open([]int{0}, events)
	if err != nil {
		t.Fatalf("Open h1: %v", err)
	}
	h2, err := src.Open([]int{1}, events)
	if err != nil {
		t.Fatalf("Open h2: %v", err)
	}

	// Close h1; h2 must still be usable.
	src.Close(h1)
	if _, err := src.Read(h2); err != nil {
		t.Errorf("Read h2 after closing h1: %v", err)
	}
	src.Close(h2)
}

// --- Collector ---

func TestCollectorEmitsSamples(t *testing.T) {
	t.Parallel()
	frames := []pmu.TraceFrame{
		{CoreID: 0, Counts: map[string]uint64{"INSTRUCTIONS": 500_000, "CYCLES": 400_000}},
	}
	src := pmu.NewReplayBackend("sapphire_rapids", frames, 0)
	events := []pmu.Event{pmu.EventInstructions, pmu.EventCycles}

	col, err := pmu.NewCollector(src, []int{0}, events, 5*time.Millisecond, 8)
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- col.Run(ctx) }()

	const wantSamples = 3
	got := 0
	for range col.C() {
		got++
		if got >= wantSamples {
			cancel()
			break
		}
	}
	// drain so Run can close the channel
	for range col.C() {
	}
	if err := <-errc; err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("Run: %v", err)
	}
	if got < wantSamples {
		t.Errorf("want ≥%d samples, got %d", wantSamples, got)
	}
}

func TestCollectorChannelClosedAfterCancel(t *testing.T) {
	t.Parallel()
	src := newSPR(testFrames)
	col, err := pmu.NewCollector(src, []int{0}, []pmu.Event{pmu.EventCycles}, 5*time.Millisecond, 4)
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go col.Run(ctx) //nolint:errcheck
	cancel()
	// C() must eventually close after cancellation.
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case _, open := <-col.C():
		if open {
			// drain remaining, wait for close
			for range col.C() {
			}
		}
	case <-timer.C:
		t.Error("channel not closed within 2s of context cancellation")
	}
}

func TestCollectorOpenError(t *testing.T) {
	t.Parallel()
	src := pmu.NewReplayBackend("unknown_uarch", testFrames, 0)
	_, err := pmu.NewCollector(src, []int{0}, allEvents, time.Second, 1)
	if err == nil {
		t.Fatal("expected error from unknown uarch, got nil")
	}
	var ue *pmu.ErrUnknownEvents
	if !errors.As(err, &ue) {
		t.Errorf("expected *ErrUnknownEvents, got %T: %v", err, err)
	}
}

// containsStr is a dependency-free substring check for error message assertions.
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
