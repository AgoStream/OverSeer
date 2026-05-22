package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/overseer/overseer/internal/agent"
	"github.com/overseer/overseer/internal/publisher"
	"github.com/overseer/overseer/pkg/nodestate"
	"github.com/overseer/overseer/pkg/pmu"
)

// testFrames supplies two cores with healthy counter values.
var testFrames = []pmu.TraceFrame{
	{CoreID: 0, Counts: map[string]uint64{
		"INSTRUCTIONS": 1_000_000,
		"CYCLES":       2_000_000,
		"L1_MISS":      50_000,
		"LLC_MISS":     20_000,
		"DRAM_STALL":   300_000,
	}},
	{CoreID: 1, Counts: map[string]uint64{
		"INSTRUCTIONS": 800_000,
		"CYCLES":       1_600_000,
		"L1_MISS":      40_000,
		"LLC_MISS":     16_000,
		"DRAM_STALL":   240_000,
	}},
}

// TestReplayRecorder verifies that the agent publishes a well-formed NodeState
// after at least one sampling tick when driven by the replay backend.
func TestReplayRecorder(t *testing.T) {
	t.Parallel()

	src := pmu.NewReplayBackend("sapphire_rapids", testFrames, 0)
	topo := replayTopology()

	var mu sync.Mutex
	var captured []nodestate.NodeState
	rec := publisher.Func(func(_ context.Context, ns nodestate.NodeState) error {
		mu.Lock()
		captured = append(captured, ns)
		mu.Unlock()
		return nil
	})

	cfg := agent.Config{
		NodeName: "test-node",
		Interval: 10 * time.Millisecond,
		Events: []pmu.Event{
			pmu.EventInstructions,
			pmu.EventCycles,
			pmu.EventL1Miss,
			pmu.EventLLCMiss,
			pmu.EventDRAMStall,
		},
		Publishers: []publisher.Publisher{rec},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	ag := agent.New(cfg, src, topo)
	ag.Run(ctx) //nolint:errcheck // context.Canceled is expected

	mu.Lock()
	got := captured
	mu.Unlock()

	if len(got) == 0 {
		t.Fatal("no NodeState snapshots published within 300 ms")
	}
	ns := got[0]
	assertNodeState(t, ns)
}

// TestReplayHTTP verifies the end-to-end path including the HTTP /state endpoint.
func TestReplayHTTP(t *testing.T) {
	t.Parallel()

	src := pmu.NewReplayBackend("sapphire_rapids", testFrames, 0)
	topo := replayTopology()

	// Bind to a random port before constructing the publisher so we know the address.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()

	httpPub := publisher.NewHTTP(addr)
	cfg := agent.Config{
		NodeName: "test-node",
		Interval: 10 * time.Millisecond,
		Events: []pmu.Event{
			pmu.EventInstructions,
			pmu.EventCycles,
			pmu.EventL1Miss,
			pmu.EventLLCMiss,
			pmu.EventDRAMStall,
		},
		Publishers: []publisher.Publisher{httpPub},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go httpPub.Serve(ctx, ln) //nolint:errcheck
	ag := agent.New(cfg, src, topo)
	go ag.Run(ctx) //nolint:errcheck

	// Poll until /state returns 200 or the deadline expires.
	url := fmt.Sprintf("http://%s/state", addr)
	var body []byte
	for {
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for /state to return 200")
		default:
		}
		resp, err := http.Get(url) //nolint:noctx
		if err != nil {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		if resp.StatusCode == http.StatusServiceUnavailable {
			resp.Body.Close()
			time.Sleep(5 * time.Millisecond)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("/state returned unexpected status %d", resp.StatusCode)
		}
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		break
	}

	var ns nodestate.NodeState
	if err := json.Unmarshal(body, &ns); err != nil {
		t.Fatalf("unmarshal /state response: %v", err)
	}
	assertNodeState(t, ns)
}

// assertNodeState checks structural invariants on a NodeState snapshot.
func assertNodeState(t *testing.T, ns nodestate.NodeState) {
	t.Helper()
	if ns.NodeName != "test-node" {
		t.Errorf("NodeName: want test-node got %q", ns.NodeName)
	}
	if ns.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
	if ns.Topology.Sockets != 1 {
		t.Errorf("Topology.Sockets: want 1 got %d", ns.Topology.Sockets)
	}
	if ns.Topology.CoresPerSocket != 2 {
		t.Errorf("Topology.CoresPerSocket: want 2 got %d", ns.Topology.CoresPerSocket)
	}
	if len(ns.Sockets) != 1 {
		t.Fatalf("Sockets: want 1 got %d", len(ns.Sockets))
	}
	sock := ns.Sockets[0]
	if sock.SocketID != 0 {
		t.Errorf("Sockets[0].SocketID: want 0 got %d", sock.SocketID)
	}
	if len(sock.Cores) != 2 {
		t.Errorf("Sockets[0].Cores: want 2 got %d", len(sock.Cores))
	}
}
