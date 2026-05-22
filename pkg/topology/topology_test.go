package topology_test

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/overseer/overseer/pkg/topology"
)

// ---- fixture builders ----

// cpuSpec describes one logical CPU for fixture construction.
type cpuSpec struct {
	id     int
	socket int
	// core is the physical core_id within socket.
	core    int
	numa    int
	sibs    []int // SMT siblings including self, sorted
	l3Peers []int // all CPUs sharing L3 with this one, sorted
}

// fmtList formats an int slice as a comma-separated string ("0,8").
func fmtList(ids []int) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, ",")
}

func ffile(content string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(content + "\n")}
}

// addCPUFiles writes all sysfs files for one CPU into m.
// Cache hierarchy: index0=L1d, index1=L1i (both shared by SMT siblings),
// index2=L2 (shared by SMT siblings), index3=L3 (shared by l3Peers).
func addCPUFiles(m fstest.MapFS, c cpuSpec) {
	base := fmt.Sprintf("sys/devices/system/cpu/cpu%d", c.id)
	topo := base + "/topology"
	cache := base + "/cache"

	m[topo+"/physical_package_id"] = ffile(strconv.Itoa(c.socket))
	m[topo+"/core_id"] = ffile(strconv.Itoa(c.core))
	m[topo+"/thread_siblings_list"] = ffile(fmtList(c.sibs))

	sibList := fmtList(c.sibs)
	l3List := fmtList(c.l3Peers)

	for _, idx := range []struct{ name, level, typ, shared string }{
		{"index0", "1", "Data", sibList},
		{"index1", "1", "Instruction", sibList},
		{"index2", "2", "Unified", sibList},
		{"index3", "3", "Unified", l3List},
	} {
		p := cache + "/" + idx.name
		m[p+"/level"] = ffile(idx.level)
		m[p+"/type"] = ffile(idx.typ)
		m[p+"/shared_cpu_list"] = ffile(idx.shared)
	}
}

// makeSPRFixture builds an fstest.MapFS representing a simplified Sapphire Rapids
// layout: 2 sockets × 4 physical cores × 2-way SMT = 16 logical CPUs.
//
// CPU numbering:
//
//	Socket 0, thread 0: CPUs 0-3  (physical cores 0-3)
//	Socket 1, thread 0: CPUs 4-7  (physical cores 0-3)
//	Socket 0, thread 1: CPUs 8-11 (physical cores 0-3, SMT siblings of 0-3)
//	Socket 1, thread 1: CPUs 12-15 (physical cores 0-3, SMT siblings of 4-7)
//
// L2: one instance per physical core (SMT pair shares L2, as on SPR).
// L3: one instance per socket (all 8 CPUs on a socket share the LLC).
// NUMA: node 0 = socket 0 (CPUs 0-3,8-11), node 1 = socket 1 (CPUs 4-7,12-15).
func makeSPRFixture() fstest.MapFS {
	m := make(fstest.MapFS)
	m["sys/devices/system/cpu/online"] = ffile("0-15")
	m["sys/devices/system/node/node0/cpulist"] = ffile("0-3,8-11")
	m["sys/devices/system/node/node1/cpulist"] = ffile("4-7,12-15")

	sock0L3 := []int{0, 1, 2, 3, 8, 9, 10, 11}
	sock1L3 := []int{4, 5, 6, 7, 12, 13, 14, 15}

	for phys := 0; phys < 4; phys++ {
		// Socket 0: thread-0 CPU = phys, thread-1 CPU = phys+8
		t0, t1 := phys, phys+8
		addCPUFiles(m, cpuSpec{t0, 0, phys, 0, []int{t0, t1}, sock0L3})
		addCPUFiles(m, cpuSpec{t1, 0, phys, 0, []int{t0, t1}, sock0L3})
		// Socket 1: thread-0 CPU = phys+4, thread-1 CPU = phys+12
		t0, t1 = phys+4, phys+12
		addCPUFiles(m, cpuSpec{t0, 1, phys, 1, []int{t0, t1}, sock1L3})
		addCPUFiles(m, cpuSpec{t1, 1, phys, 1, []int{t0, t1}, sock1L3})
	}
	return m
}

// makeAltFixture builds an fstest.MapFS for a deliberately different layout:
// 1 socket × 3 physical cores × 2-way SMT = 6 logical CPUs.
//
// CPU numbering:
//
//	Thread 0: CPUs 0, 1, 2  (physical cores 0, 1, 2)
//	Thread 1: CPUs 3, 4, 5  (physical cores 0, 1, 2)
//
// L3: single instance — all 6 CPUs share the LLC.
// NUMA: one node (node 0).
func makeAltFixture() fstest.MapFS {
	m := make(fstest.MapFS)
	m["sys/devices/system/cpu/online"] = ffile("0-5")
	m["sys/devices/system/node/node0/cpulist"] = ffile("0-5")

	allCPUs := []int{0, 1, 2, 3, 4, 5}
	for phys := 0; phys < 3; phys++ {
		t0, t1 := phys, phys+3
		addCPUFiles(m, cpuSpec{t0, 0, phys, 0, []int{t0, t1}, allCPUs})
		addCPUFiles(m, cpuSpec{t1, 0, phys, 0, []int{t0, t1}, allCPUs})
	}
	return m
}

// ---- structural count tests (table-driven) ----

func TestDiscover_StructuralCounts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		fsys           fstest.MapFS
		wantSockets    int
		wantNUMANodes  int
		wantCoresPerSk int
		wantL2Groups   int
		wantL3Groups   int
		wantIMCGroups  int
		wantCores      int
	}{
		{
			name:           "spr_2s4c2t",
			fsys:           makeSPRFixture(),
			wantSockets:    2,
			wantNUMANodes:  2,
			wantCoresPerSk: 4,
			wantL2Groups:   8,  // 4 phys cores × 2 sockets
			wantL3Groups:   2,  // one LLC per socket
			wantIMCGroups:  2,  // one IMC domain per NUMA node
			wantCores:      16, // 16 logical CPUs total
		},
		{
			name:           "alt_1s3c2t",
			fsys:           makeAltFixture(),
			wantSockets:    1,
			wantNUMANodes:  1,
			wantCoresPerSk: 3,
			wantL2Groups:   3, // 3 phys cores × 1 socket
			wantL3Groups:   1, // single LLC
			wantIMCGroups:  1,
			wantCores:      6,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ts, err := topology.Discover(tc.fsys)
			if err != nil {
				t.Fatalf("Discover: %v", err)
			}

			check := func(field string, want, got int) {
				t.Helper()
				if got != want {
					t.Errorf("%s: want %d got %d", field, want, got)
				}
			}
			check("Sockets", tc.wantSockets, ts.Sockets)
			check("NUMANodes", tc.wantNUMANodes, ts.NUMANodes)
			check("CoresPerSocket", tc.wantCoresPerSk, ts.CoresPerSocket)
			check("len(L2Groups)", tc.wantL2Groups, len(ts.L2Groups))
			check("len(L3Groups)", tc.wantL3Groups, len(ts.L3Groups))
			check("len(IMCGroups)", tc.wantIMCGroups, len(ts.IMCGroups))
			check("len(Cores)", tc.wantCores, len(ts.Cores))
		})
	}
}

// ---- detailed field checks for SPR fixture ----

func TestDiscover_SPR_L3Groups(t *testing.T) {
	t.Parallel()
	ts, err := topology.Discover(makeSPRFixture())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	// Groups are ordered by smallest CPU ID: group 0 = socket 0, group 1 = socket 1.
	wantGroups := [][]int{
		{0, 1, 2, 3, 8, 9, 10, 11},
		{4, 5, 6, 7, 12, 13, 14, 15},
	}
	if len(ts.L3Groups) != len(wantGroups) {
		t.Fatalf("L3Groups: want %d groups got %d", len(wantGroups), len(ts.L3Groups))
	}
	for i, want := range wantGroups {
		got := ts.L3Groups[i].CPUs
		if !intSliceEqual(got, want) {
			t.Errorf("L3Groups[%d].CPUs: want %v got %v", i, want, got)
		}
		if ts.L3Groups[i].ID != i {
			t.Errorf("L3Groups[%d].ID: want %d got %d", i, i, ts.L3Groups[i].ID)
		}
	}
}

func TestDiscover_SPR_CoreInfo(t *testing.T) {
	t.Parallel()
	ts, err := topology.Discover(makeSPRFixture())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	// Spot-check a selection of cores. Cores are in ascending LogicalID order.
	type expectCore struct {
		logicalID  int
		physicalID int
		socketID   int
		numaNodeID int
		siblings   []int
	}
	cases := []expectCore{
		{0, 0, 0, 0, []int{0, 8}},
		{3, 3, 0, 0, []int{3, 11}},
		{4, 0, 1, 1, []int{4, 12}},
		// CPU 8 is the SMT sibling of CPU 0 (same physical core, socket 0).
		{8, 0, 0, 0, []int{0, 8}},
		// CPU 12 is the SMT sibling of CPU 4 (same physical core, socket 1).
		{12, 0, 1, 1, []int{4, 12}},
		{15, 3, 1, 1, []int{7, 15}},
	}

	coreByID := make(map[int]topology.CoreInfo, len(ts.Cores))
	for _, c := range ts.Cores {
		coreByID[c.LogicalID] = c
	}

	for _, ec := range cases {
		c, ok := coreByID[ec.logicalID]
		if !ok {
			t.Errorf("CPU %d not found in Cores", ec.logicalID)
			continue
		}
		if c.PhysicalID != ec.physicalID {
			t.Errorf("CPU %d PhysicalID: want %d got %d", ec.logicalID, ec.physicalID, c.PhysicalID)
		}
		if c.SocketID != ec.socketID {
			t.Errorf("CPU %d SocketID: want %d got %d", ec.logicalID, ec.socketID, c.SocketID)
		}
		if c.NUMANodeID != ec.numaNodeID {
			t.Errorf("CPU %d NUMANodeID: want %d got %d", ec.logicalID, ec.numaNodeID, c.NUMANodeID)
		}
		got := make([]int, len(c.ThreadSiblings))
		copy(got, c.ThreadSiblings)
		sort.Ints(got)
		if !intSliceEqual(got, ec.siblings) {
			t.Errorf("CPU %d ThreadSiblings: want %v got %v", ec.logicalID, ec.siblings, got)
		}
	}
}

// ---- helper-method tests: SPR ----

func TestSMTSiblingOf_SPR(t *testing.T) {
	t.Parallel()
	ts, err := topology.Discover(makeSPRFixture())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	cases := []struct {
		cpu    int
		sib    int
		wantOK bool
	}{
		{0, 8, true},
		{8, 0, true},
		{3, 11, true},
		{11, 3, true},
		{4, 12, true},
		{12, 4, true},
		{7, 15, true},
		{15, 7, true},
		{99, -1, false}, // unknown CPU
	}

	for _, tc := range cases {
		sib, ok := ts.SMTSiblingOf(tc.cpu)
		if ok != tc.wantOK {
			t.Errorf("SMTSiblingOf(%d): wantOK=%v got=%v", tc.cpu, tc.wantOK, ok)
			continue
		}
		if ok && sib != tc.sib {
			t.Errorf("SMTSiblingOf(%d): want sibling %d got %d", tc.cpu, tc.sib, sib)
		}
	}
}

func TestCoresInL3WithCore_SPR(t *testing.T) {
	t.Parallel()
	ts, err := topology.Discover(makeSPRFixture())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	sock0Expected := []int{0, 1, 2, 3, 8, 9, 10, 11}
	sock1Expected := []int{4, 5, 6, 7, 12, 13, 14, 15}

	// Every CPU in socket 0 should report the same L3 peer set.
	for _, cpu := range sock0Expected {
		got := ts.CoresInL3WithCore(cpu)
		sort.Ints(got)
		if !intSliceEqual(got, sock0Expected) {
			t.Errorf("CoresInL3WithCore(%d): want %v got %v", cpu, sock0Expected, got)
		}
	}

	// Every CPU in socket 1 should report the socket-1 set.
	for _, cpu := range sock1Expected {
		got := ts.CoresInL3WithCore(cpu)
		sort.Ints(got)
		if !intSliceEqual(got, sock1Expected) {
			t.Errorf("CoresInL3WithCore(%d): want %v got %v", cpu, sock1Expected, got)
		}
	}

	// Unknown CPU returns nil.
	if got := ts.CoresInL3WithCore(99); got != nil {
		t.Errorf("CoresInL3WithCore(99): want nil got %v", got)
	}
}

func TestIsOnSaturatedIMC_SPR(t *testing.T) {
	t.Parallel()
	ts, err := topology.Discover(makeSPRFixture())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	// IMC group 0 = NUMA node 0 (CPUs 0-3, 8-11)
	// IMC group 1 = NUMA node 1 (CPUs 4-7, 12-15)

	saturateGroup1 := map[int]bool{1: true}
	saturateBoth := map[int]bool{0: true, 1: true}
	saturateNone := map[int]bool{}

	cases := []struct {
		cpu       int
		saturated map[int]bool
		want      bool
	}{
		// Group 0 CPUs — not on saturated IMC when only group 1 is saturated.
		{0, saturateGroup1, false},
		{3, saturateGroup1, false},
		{8, saturateGroup1, false},
		{11, saturateGroup1, false},
		// Group 1 CPUs — saturated when group 1 is saturated.
		{4, saturateGroup1, true},
		{7, saturateGroup1, true},
		{12, saturateGroup1, true},
		{15, saturateGroup1, true},
		// Both groups saturated — all CPUs return true.
		{0, saturateBoth, true},
		{4, saturateBoth, true},
		// Nothing saturated.
		{0, saturateNone, false},
		{4, saturateNone, false},
		// Unknown CPU.
		{99, saturateGroup1, false},
	}

	for _, tc := range cases {
		got := ts.IsOnSaturatedIMC(tc.cpu, tc.saturated)
		if got != tc.want {
			t.Errorf("IsOnSaturatedIMC(%d, %v): want %v got %v",
				tc.cpu, tc.saturated, tc.want, got)
		}
	}
}

// ---- helper-method tests: alt fixture (proves parser is not hardcoded) ----

func TestSMTSiblingOf_Alt(t *testing.T) {
	t.Parallel()
	ts, err := topology.Discover(makeAltFixture())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	cases := []struct{ cpu, sib int }{
		{0, 3},
		{3, 0},
		{1, 4},
		{4, 1},
		{2, 5},
		{5, 2},
	}
	for _, tc := range cases {
		sib, ok := ts.SMTSiblingOf(tc.cpu)
		if !ok {
			t.Errorf("SMTSiblingOf(%d): expected ok=true", tc.cpu)
			continue
		}
		if sib != tc.sib {
			t.Errorf("SMTSiblingOf(%d): want %d got %d", tc.cpu, tc.sib, sib)
		}
	}
}

func TestCoresInL3WithCore_Alt(t *testing.T) {
	t.Parallel()
	ts, err := topology.Discover(makeAltFixture())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	allCPUs := []int{0, 1, 2, 3, 4, 5}
	for _, cpu := range allCPUs {
		got := ts.CoresInL3WithCore(cpu)
		sort.Ints(got)
		if !intSliceEqual(got, allCPUs) {
			t.Errorf("CoresInL3WithCore(%d): want %v got %v", cpu, allCPUs, got)
		}
	}
}

func TestIsOnSaturatedIMC_Alt(t *testing.T) {
	t.Parallel()
	ts, err := topology.Discover(makeAltFixture())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	// Single IMC group (ID 0) — all CPUs share the one memory controller.
	for _, cpu := range []int{0, 1, 2, 3, 4, 5} {
		if ts.IsOnSaturatedIMC(cpu, map[int]bool{0: true}) != true {
			t.Errorf("IsOnSaturatedIMC(%d): want true with group 0 saturated", cpu)
		}
		if ts.IsOnSaturatedIMC(cpu, map[int]bool{}) != false {
			t.Errorf("IsOnSaturatedIMC(%d): want false with nothing saturated", cpu)
		}
	}
}

// ---- error path ----

func TestDiscover_MissingOnline(t *testing.T) {
	t.Parallel()
	_, err := topology.Discover(make(fstest.MapFS))
	if err == nil {
		t.Error("Discover on empty FS: expected error, got nil")
	}
}

func TestDiscover_NoNUMAFallback(t *testing.T) {
	t.Parallel()

	// Fixture with no sys/devices/system/node directory — should fall back to NUMA node 0.
	m := make(fstest.MapFS)
	m["sys/devices/system/cpu/online"] = ffile("0-1")
	allCPUs := []int{0, 1}
	addCPUFiles(m, cpuSpec{0, 0, 0, 0, []int{0, 1}, allCPUs})
	addCPUFiles(m, cpuSpec{1, 0, 0, 0, []int{0, 1}, allCPUs})

	ts, err := topology.Discover(m)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if ts.NUMANodes != 1 {
		t.Errorf("NUMANodes: want 1 got %d", ts.NUMANodes)
	}
	if len(ts.IMCGroups) != 1 {
		t.Errorf("IMCGroups: want 1 got %d", len(ts.IMCGroups))
	}
	// Both CPUs should be reachable via the IMC helper.
	for _, cpu := range []int{0, 1} {
		if !ts.IsOnSaturatedIMC(cpu, map[int]bool{0: true}) {
			t.Errorf("IsOnSaturatedIMC(%d): want true in fallback NUMA-0 mode", cpu)
		}
	}
}

// ---- utility ----

func intSliceEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
