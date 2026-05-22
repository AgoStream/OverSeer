package topology

import (
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Discover reads the CPU topology from fsys, which must be rooted so that
// sysfs lives at "sys/devices/system/...".  On a real Linux node pass
// os.DirFS("/"); in tests pass a testing/fstest.MapFS fixture.
func Discover(fsys fs.FS) (*TopologySummary, error) {
	// Step 1: enumerate online CPUs.
	onlineStr, err := sysReadStr(fsys, "sys/devices/system/cpu/online")
	if err != nil {
		return nil, fmt.Errorf("topology: read online cpus: %w", err)
	}
	online, err := parseCPUList(onlineStr)
	if err != nil {
		return nil, fmt.Errorf("topology: parse online cpulist: %w", err)
	}
	if len(online) == 0 {
		return nil, fmt.Errorf("topology: no online CPUs found")
	}

	// Step 2: per-CPU sysfs topology attributes.
	type rawAttrs struct {
		socketID   int
		physicalID int
		siblings   []int
	}
	raw := make(map[int]rawAttrs, len(online))
	for _, cpu := range online {
		base := fmt.Sprintf("sys/devices/system/cpu/cpu%d/topology", cpu)

		socketID, err := sysReadInt(fsys, base+"/physical_package_id")
		if err != nil {
			return nil, fmt.Errorf("topology: cpu%d physical_package_id: %w", cpu, err)
		}
		physID, err := sysReadInt(fsys, base+"/core_id")
		if err != nil {
			return nil, fmt.Errorf("topology: cpu%d core_id: %w", cpu, err)
		}
		sibStr, err := sysReadStr(fsys, base+"/thread_siblings_list")
		if err != nil {
			return nil, fmt.Errorf("topology: cpu%d thread_siblings_list: %w", cpu, err)
		}
		sibs, err := parseCPUList(sibStr)
		if err != nil {
			return nil, fmt.Errorf("topology: cpu%d thread_siblings_list parse: %w", cpu, err)
		}
		raw[cpu] = rawAttrs{socketID: socketID, physicalID: physID, siblings: sibs}
	}

	// Step 3: NUMA node membership → per-CPU NUMA ID + ordered node list.
	numaMap, numaNodes, err := discoverNUMA(fsys, online)
	if err != nil {
		return nil, err
	}

	// Step 4: cache groups (L2 and L3).
	l2raw, l3raw, err := discoverCacheGroups(fsys, online)
	if err != nil {
		return nil, err
	}
	l2Groups := buildCacheGroups(l2raw)
	l3Groups := buildCacheGroups(l3raw)

	// Step 5: assemble CoreInfo, preserving ascending LogicalID order.
	sort.Ints(online)
	cores := make([]CoreInfo, 0, len(online))
	for _, cpu := range online {
		a := raw[cpu]
		cores = append(cores, CoreInfo{
			LogicalID:      cpu,
			PhysicalID:     a.physicalID,
			SocketID:       a.socketID,
			NUMANodeID:     numaMap[cpu],
			ThreadSiblings: a.siblings,
		})
	}

	// Step 6: IMC groups — one per NUMA node (SPR invariant).
	imcGroups := make([]IMCGroup, len(numaNodes))
	for i, nn := range numaNodes {
		imcGroups[i] = IMCGroup{ID: i, NUMANode: nn.nodeID, CPUs: nn.cpus}
	}

	// Step 7: NodeTopology summary counts.
	socketSet := make(map[int]struct{})
	physPerSocket := make(map[int]map[int]struct{})
	for _, c := range cores {
		socketSet[c.SocketID] = struct{}{}
		if physPerSocket[c.SocketID] == nil {
			physPerSocket[c.SocketID] = make(map[int]struct{})
		}
		physPerSocket[c.SocketID][c.PhysicalID] = struct{}{}
	}
	maxPhys := 0
	for _, m := range physPerSocket {
		if len(m) > maxPhys {
			maxPhys = len(m)
		}
	}

	return &TopologySummary{
		NodeTopology: NodeTopology{
			Sockets:        len(socketSet),
			NUMANodes:      len(numaNodes),
			CoresPerSocket: maxPhys,
		},
		Cores:     cores,
		L2Groups:  l2Groups,
		L3Groups:  l3Groups,
		IMCGroups: imcGroups,
	}, nil
}

// DiscoverFromSysfs reads topology from the real /sys filesystem on Linux.
func DiscoverFromSysfs() (*TopologySummary, error) {
	return Discover(os.DirFS("/"))
}

// ---- NUMA discovery ----

type numaEntry struct {
	nodeID int
	cpus   []int
}

func discoverNUMA(fsys fs.FS, online []int) (cpuToNUMA map[int]int, nodes []numaEntry, err error) {
	cpuToNUMA = make(map[int]int, len(online))

	entries, err := fs.ReadDir(fsys, "sys/devices/system/node")
	if err != nil {
		// No NUMA support in kernel: treat all CPUs as node 0.
		cpus := make([]int, len(online))
		copy(cpus, online)
		sort.Ints(cpus)
		return cpuToNUMA, []numaEntry{{nodeID: 0, cpus: cpus}}, nil
	}

	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "node") {
			continue
		}
		nodeID, err := strconv.Atoi(name[4:])
		if err != nil {
			continue // not a nodeN entry
		}
		cpuListStr, err := sysReadStr(fsys, "sys/devices/system/node/"+name+"/cpulist")
		if err != nil {
			return nil, nil, fmt.Errorf("topology: NUMA node%d cpulist: %w", nodeID, err)
		}
		cpus, err := parseCPUList(cpuListStr)
		if err != nil {
			return nil, nil, fmt.Errorf("topology: NUMA node%d cpulist parse: %w", nodeID, err)
		}
		sort.Ints(cpus)
		nodes = append(nodes, numaEntry{nodeID: nodeID, cpus: cpus})
		for _, c := range cpus {
			cpuToNUMA[c] = nodeID
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].nodeID < nodes[j].nodeID })
	return cpuToNUMA, nodes, nil
}

// ---- cache group discovery ----

func discoverCacheGroups(fsys fs.FS, cpuIDs []int) (l2, l3 map[string][]int, err error) {
	l2 = make(map[string][]int)
	l3 = make(map[string][]int)

	for _, cpu := range cpuIDs {
		cacheDir := fmt.Sprintf("sys/devices/system/cpu/cpu%d/cache", cpu)
		entries, err := fs.ReadDir(fsys, cacheDir)
		if err != nil {
			return nil, nil, fmt.Errorf("topology: cpu%d cache dir: %w", cpu, err)
		}
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), "index") {
				continue
			}
			base := cacheDir + "/" + e.Name()
			level, err := sysReadInt(fsys, base+"/level")
			if err != nil {
				continue
			}
			cacheType, err := sysReadStr(fsys, base+"/type")
			if err != nil {
				continue
			}
			sharedStr, err := sysReadStr(fsys, base+"/shared_cpu_list")
			if err != nil {
				continue
			}
			sharedCPUs, err := parseCPUList(sharedStr)
			if err != nil {
				continue
			}
			key := canonicalCPUKey(sharedCPUs)
			switch {
			case level == 2 && cacheType == "Unified":
				l2[key] = sharedCPUs
			case level == 3:
				// Accept both "Unified" and "Data" (some kernels report Data for LLC).
				l3[key] = sharedCPUs
			}
		}
	}
	return l2, l3, nil
}

// buildCacheGroups converts a key→cpus map into a sorted, ID-assigned slice.
// Groups are ordered by the smallest CPU ID they contain for determinism.
func buildCacheGroups(raw map[string][]int) []CacheGroup {
	groups := make([]CacheGroup, 0, len(raw))
	for _, cpus := range raw {
		sorted := make([]int, len(cpus))
		copy(sorted, cpus)
		sort.Ints(sorted)
		groups = append(groups, CacheGroup{CPUs: sorted})
	}
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].CPUs) == 0 {
			return true
		}
		if len(groups[j].CPUs) == 0 {
			return false
		}
		return groups[i].CPUs[0] < groups[j].CPUs[0]
	})
	for i := range groups {
		groups[i].ID = i
	}
	return groups
}

// ---- cpulist parsing ----

// parseCPUList parses a Linux cpulist string such as "0-3,8-11" into a slice of ints.
func parseCPUList(s string) ([]int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var cpus []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if dash := strings.Index(part, "-"); dash >= 0 {
			lo, err := strconv.Atoi(part[:dash])
			if err != nil {
				return nil, fmt.Errorf("invalid cpulist segment %q", part)
			}
			hi, err := strconv.Atoi(part[dash+1:])
			if err != nil {
				return nil, fmt.Errorf("invalid cpulist segment %q", part)
			}
			for i := lo; i <= hi; i++ {
				cpus = append(cpus, i)
			}
		} else {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid cpulist segment %q", part)
			}
			cpus = append(cpus, n)
		}
	}
	return cpus, nil
}

// canonicalCPUKey returns a sorted, comma-separated string used as a dedup key.
func canonicalCPUKey(cpus []int) string {
	sorted := make([]int, len(cpus))
	copy(sorted, cpus)
	sort.Ints(sorted)
	parts := make([]string, len(sorted))
	for i, c := range sorted {
		parts[i] = strconv.Itoa(c)
	}
	return strings.Join(parts, ",")
}

// ---- low-level sysfs readers ----

func sysReadStr(fsys fs.FS, path string) (string, error) {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func sysReadInt(fsys fs.FS, path string) (int, error) {
	s, err := sysReadStr(fsys, path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}
