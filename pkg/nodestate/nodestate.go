// Package nodestate defines the NodeState schema: the per-node snapshot pushed by the
// agent to the Kubernetes API server and consumed by the scheduler plugin.
// This schema is a data contract; changes require cross-component review before any
// producer or consumer is modified.
package nodestate

import (
	"time"

	"github.com/overseer/overseer/pkg/regime"
	"github.com/overseer/overseer/pkg/topology"
)

// NodeState is the agent's view of a node's hardware contention state at a point in time.
// It is the primary artifact exchanged between the agent (producer) and the scheduler
// plugin (consumer).
type NodeState struct {
	// NodeName is the Kubernetes node name (matches node.metadata.name).
	NodeName string `json:"node_name"`
	// Timestamp is the wall-clock time at which this snapshot was taken (UTC).
	Timestamp time.Time `json:"timestamp"`
	// Topology summarises the node's physical layout as discovered by pkg/topology.
	Topology topology.NodeTopology `json:"topology"`
	// Sockets holds per-socket contention pressure and optional per-core detail.
	// Length must equal Topology.Sockets once the agent is wired up.
	Sockets []SocketState `json:"sockets"`
	// WorkloadRegimes maps workload identifier to its current regime label as inferred
	// by Component B (pkg/regime) from recent FeatureVectors.
	WorkloadRegimes map[string]regime.Label `json:"workload_regimes"`
}

// SocketState captures the contention pressure observed on one physical socket.
type SocketState struct {
	// SocketID is the zero-based socket index (matches kernel socket numbering).
	SocketID int `json:"socket_id"`
	// L3Contended is true when LLC miss pressure exceeds the configured saturation threshold,
	// indicating that the shared L3 cache is a bottleneck for at least one workload.
	L3Contended bool `json:"l3_contended"`
	// IMCBWSaturated is true when Integrated Memory Controller bandwidth utilisation
	// exceeds the saturation threshold on this socket.
	IMCBWSaturated bool `json:"imc_bw_saturated"`
	// Cores holds per-core detail. Omitted when core-level sampling is not enabled,
	// or when the agent is running in socket-granularity mode.
	Cores []CoreState `json:"cores,omitempty"`
}

// CoreState captures the instantaneous state of one logical core.
type CoreState struct {
	// CoreID is the zero-based logical core index within the containing socket.
	CoreID int `json:"core_id"`
	// SMTSiblingBusy is true when the SMT sibling thread on this physical core is
	// actively competing for front-end and execution resources.
	SMTSiblingBusy bool `json:"smt_sibling_busy"`
	// WorkloadID identifies the workload currently pinned to this core.
	// Empty when the core is idle or not tracked at core granularity.
	WorkloadID string `json:"workload_id,omitempty"`
}
