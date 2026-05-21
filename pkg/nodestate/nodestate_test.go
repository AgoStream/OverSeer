package nodestate_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/overseer/overseer/pkg/nodestate"
	"github.com/overseer/overseer/pkg/regime"
	"github.com/overseer/overseer/pkg/topology"
)

func TestNodeStateRoundTrip(t *testing.T) {
	t.Parallel()

	// Fixed wall time with no monotonic reading — safe for DeepEqual after JSON round-trip.
	ts := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		in   nodestate.NodeState
	}{
		{
			name: "two_socket_two_workloads",
			in: nodestate.NodeState{
				NodeName:  "worker-1",
				Timestamp: ts,
				Topology: topology.NodeTopology{
					Sockets:        2,
					NUMANodes:      2,
					CoresPerSocket: 24,
				},
				Sockets: []nodestate.SocketState{
					{
						SocketID:       0,
						L3Contended:    true,
						IMCBWSaturated: false,
						Cores: []nodestate.CoreState{
							{CoreID: 0, SMTSiblingBusy: true, WorkloadID: "cnf-a"},
							{CoreID: 1, SMTSiblingBusy: false, WorkloadID: "cnf-b"},
						},
					},
					{
						SocketID:       1,
						L3Contended:    false,
						IMCBWSaturated: true,
						// No cores — socket-granularity mode.
					},
				},
				WorkloadRegimes: map[string]regime.Label{
					"cnf-a": regime.LabelMemBound,
					"cnf-b": regime.LabelContended,
				},
			},
		},
		{
			name: "single_socket_idle_no_core_detail",
			in: nodestate.NodeState{
				NodeName:  "worker-2",
				Timestamp: ts,
				Topology:  topology.NodeTopology{Sockets: 1, NUMANodes: 1, CoresPerSocket: 8},
				Sockets: []nodestate.SocketState{
					{SocketID: 0, L3Contended: false, IMCBWSaturated: false},
				},
				WorkloadRegimes: map[string]regime.Label{},
			},
		},
		{
			name: "all_pressure_flags_set",
			in: nodestate.NodeState{
				NodeName:  "worker-3",
				Timestamp: ts,
				Topology:  topology.NodeTopology{Sockets: 1, NUMANodes: 1, CoresPerSocket: 4},
				Sockets: []nodestate.SocketState{
					{
						SocketID:       0,
						L3Contended:    true,
						IMCBWSaturated: true,
						Cores: []nodestate.CoreState{
							{CoreID: 0, SMTSiblingBusy: true, WorkloadID: "cnf-x"},
							{CoreID: 1, SMTSiblingBusy: true, WorkloadID: ""},
						},
					},
				},
				WorkloadRegimes: map[string]regime.Label{
					"cnf-x": regime.LabelComputeBound,
				},
			},
		},
		{
			// Nil slices and nil map must survive the round-trip as nil (not empty).
			name: "zero_value",
			in:   nodestate.NodeState{},
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
			var got nodestate.NodeState
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if !reflect.DeepEqual(tc.in, got) {
				t.Errorf("round-trip mismatch\n want: %+v\n  got: %+v", tc.in, got)
			}
		})
	}
}

func TestSocketStateJSONFieldNames(t *testing.T) {
	t.Parallel()

	s := nodestate.SocketState{
		SocketID:       1,
		L3Contended:    true,
		IMCBWSaturated: true,
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}
	for _, want := range []string{"socket_id", "l3_contended", "imc_bw_saturated"} {
		if _, ok := m[want]; !ok {
			t.Errorf("expected JSON key %q not found in %s", want, data)
		}
	}
	// cores omitempty — must be absent when nil.
	if _, ok := m["cores"]; ok {
		t.Errorf("cores must be omitted when nil, but found in %s", data)
	}
}

func TestCoreStateWorkloadIDOmitempty(t *testing.T) {
	t.Parallel()

	c := nodestate.CoreState{CoreID: 0, SMTSiblingBusy: false}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}
	if _, ok := m["workload_id"]; ok {
		t.Errorf("workload_id must be omitted when empty, but found in %s", data)
	}
}
