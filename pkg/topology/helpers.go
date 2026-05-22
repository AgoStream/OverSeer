package topology

// CoresInL3WithCore returns all logical CPU IDs that share the L3 (LLC) cache
// with cpu, including cpu itself. The returned slice is sorted. Returns nil if
// cpu is not present in any L3 group.
func (t *TopologySummary) CoresInL3WithCore(cpu int) []int {
	for _, g := range t.L3Groups {
		for _, c := range g.CPUs {
			if c == cpu {
				result := make([]int, len(g.CPUs))
				copy(result, g.CPUs)
				return result
			}
		}
	}
	return nil
}

// SMTSiblingOf returns the logical CPU ID of the SMT peer that shares the same
// physical core as cpu. Returns -1, false if cpu has no peer (single-threaded
// core or cpu not found in the topology).
func (t *TopologySummary) SMTSiblingOf(cpu int) (int, bool) {
	for _, c := range t.Cores {
		if c.LogicalID != cpu {
			continue
		}
		for _, sib := range c.ThreadSiblings {
			if sib != cpu {
				return sib, true
			}
		}
		return -1, false // sole thread on this physical core
	}
	return -1, false // cpu not found
}

// IsOnSaturatedIMC reports whether cpu is served by a memory-controller domain
// that appears as true in saturated. The saturated map is keyed by IMCGroup.ID.
// Returns false if cpu is not found in any IMC group.
func (t *TopologySummary) IsOnSaturatedIMC(cpu int, saturated map[int]bool) bool {
	for _, g := range t.IMCGroups {
		for _, c := range g.CPUs {
			if c == cpu {
				return saturated[g.ID]
			}
		}
	}
	return false
}
