// Package topology models NUMA topology and core-to-socket mapping for SPR nodes.
package topology

// NodeTopology describes the physical layout of a single Kubernetes node.
// It is embedded in NodeState and serialised as part of every agent snapshot.
type NodeTopology struct {
	Sockets        int `json:"sockets"`
	NUMANodes      int `json:"numa_nodes"`
	CoresPerSocket int `json:"cores_per_socket"`
}
