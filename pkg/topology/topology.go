// Package topology discovers and models the shared-resource layout of a Kubernetes
// node so that the scheduler can reason about cache, memory-controller, and SMT
// pressure domains.
package topology

// NodeTopology describes the physical layout of a single Kubernetes node.
// It is embedded in NodeState and serialised as part of every agent snapshot.
// Kept as a standalone type so that existing consumers (nodestate, tests) are
// unchanged; TopologySummary embeds it for the full detail view.
type NodeTopology struct {
	Sockets        int `json:"sockets"`
	NUMANodes      int `json:"numa_nodes"`
	CoresPerSocket int `json:"cores_per_socket"`
}

// CoreInfo holds the physical placement of one logical CPU as reported by sysfs.
type CoreInfo struct {
	// LogicalID is the OS CPU number (e.g. the N in /sys/.../cpuN).
	LogicalID int `json:"logical_id"`
	// PhysicalID is the kernel core_id within the socket (not globally unique).
	PhysicalID int `json:"physical_id"`
	// SocketID is the kernel physical_package_id.
	SocketID int `json:"socket_id"`
	// NUMANodeID is the NUMA node this CPU belongs to.
	NUMANodeID int `json:"numa_node_id"`
	// ThreadSiblings lists all logical CPU IDs (including this one) that share
	// the same physical core via SMT, as reported by thread_siblings_list.
	ThreadSiblings []int `json:"thread_siblings"`
}

// CacheGroup is a set of logical CPUs sharing a single cache instance.
type CacheGroup struct {
	// ID is a stable zero-based index within the containing L2Groups or L3Groups slice.
	ID int `json:"id"`
	// CPUs is the sorted list of logical CPU IDs that share this cache.
	CPUs []int `json:"cpus"`
}

// IMCGroup is the set of logical CPUs served by one integrated memory-controller
// domain. On Sapphire Rapids, IMC domains align one-to-one with NUMA nodes.
type IMCGroup struct {
	// ID is a stable zero-based index within TopologySummary.IMCGroups.
	ID int `json:"id"`
	// NUMANode is the NUMA node ID this controller serves.
	NUMANode int `json:"numa_node"`
	// CPUs is the sorted list of logical CPU IDs served by this controller.
	CPUs []int `json:"cpus"`
}

// TopologySummary is the full shared-resource layout produced by Discover.
// It embeds NodeTopology (for the aggregate counts expected by NodeState) and
// extends it with per-core detail and cache/IMC group membership.
type TopologySummary struct {
	NodeTopology
	// Cores holds one entry per online logical CPU, in ascending LogicalID order.
	Cores []CoreInfo `json:"cores,omitempty"`
	// L2Groups lists the sets of CPUs sharing each L2 cache instance, ordered by
	// the smallest CPU ID in the group (deterministic across calls).
	L2Groups []CacheGroup `json:"l2_groups,omitempty"`
	// L3Groups lists the sets of CPUs sharing each L3 (LLC) instance, same ordering.
	L3Groups []CacheGroup `json:"l3_groups,omitempty"`
	// IMCGroups lists the sets of CPUs sharing each memory-controller domain.
	// Derived from NUMA node membership; ordered by NUMANode ID.
	IMCGroups []IMCGroup `json:"imc_groups,omitempty"`
}
