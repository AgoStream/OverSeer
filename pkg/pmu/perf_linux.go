//go:build linux

package pmu

// PerfBackend: Linux perf_event_open via golang.org/x/sys/unix.
//
// Backend choice rationale:
//   golang.org/x/sys/unix is preferred over cgo+libpfm4 because:
//   (a) it avoids a C toolchain dependency in the container build,
//   (b) unix.PerfEventOpen provides the correctly-sized perf_event_attr struct
//       and the syscall wrapper for every supported GOARCH without manual offsets,
//   (c) symbolic event name resolution — the only real value libpfm4 adds —
//       is handled by our own encodingTable which we control and version.
//
// Privilege: requires CAP_PERFMON (Linux ≥5.8) or CAP_SYS_ADMIN on older kernels.
// Group reads: all events for a core are opened in a single perf group (leader +
// members) so that one read(2) on the leader fd captures all counters atomically.

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/unix"
)

// PerfBackend implements CounterSource using Linux perf_event_open.
type PerfBackend struct {
	uarch  string
	mu     sync.Mutex
	nextID uint64
	groups map[Handle][]coreGroup
}

// coreGroup holds the open file descriptors for one core's event group.
type coreGroup struct {
	coreID    int
	leaderFD  *os.File
	memberFDs []*os.File
	events    []Event // order matches the group; index 0 is the leader event
}

// NewPerfBackend creates a PerfBackend for the given microarchitecture name.
// uarch must match a key in encodingTable (e.g. "sapphire_rapids").
func NewPerfBackend(uarch string) *PerfBackend {
	return &PerfBackend{
		uarch:  uarch,
		groups: make(map[Handle][]coreGroup),
	}
}

// Open implements CounterSource. It opens one perf event group per requested core.
func (p *PerfBackend) Open(coreIDs []int, events []Event) (Handle, error) {
	encs, err := lookupEncodings(p.uarch, events)
	if err != nil {
		return Handle{}, err
	}
	opened := make([]coreGroup, 0, len(coreIDs))
	for _, cid := range coreIDs {
		g, err := openCoreGroup(cid, events, encs)
		if err != nil {
			for _, g := range opened {
				closeCoreGroup(g) //nolint:errcheck
			}
			return Handle{}, fmt.Errorf("pmu perf: open core %d: %w", cid, err)
		}
		opened = append(opened, g)
	}
	h := Handle{id: atomic.AddUint64(&p.nextID, 1)}
	p.mu.Lock()
	p.groups[h] = opened
	p.mu.Unlock()
	return h, nil
}

// Read implements CounterSource. Each core group is read atomically via the leader fd.
func (p *PerfBackend) Read(h Handle) ([]RawSample, error) {
	p.mu.Lock()
	gs, ok := p.groups[h]
	p.mu.Unlock()
	if !ok {
		return nil, ErrHandleNotFound
	}
	samples := make([]RawSample, 0, len(gs))
	for _, g := range gs {
		counts, err := readCoreGroup(g)
		if err != nil {
			return nil, fmt.Errorf("pmu perf: read core %d: %w", g.coreID, err)
		}
		samples = append(samples, RawSample{CoreID: g.coreID, Counts: counts})
	}
	return samples, nil
}

// Close implements CounterSource.
func (p *PerfBackend) Close(h Handle) error {
	p.mu.Lock()
	gs, ok := p.groups[h]
	if ok {
		delete(p.groups, h)
	}
	p.mu.Unlock()
	if !ok {
		return ErrHandleNotFound
	}
	var firstErr error
	for _, g := range gs {
		if err := closeCoreGroup(g); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// openCoreGroup opens a perf event group for a single logical core.
// The first event becomes the group leader with PERF_FORMAT_GROUP so that a single
// read(2) on the leader fd returns all counter values atomically. Remaining events
// are opened as group members referencing the leader's fd.
func openCoreGroup(coreID int, events []Event, encs map[Event]perfEncoding) (coreGroup, error) {
	var leaderFD *os.File
	memberFDs := make([]*os.File, 0, len(events)-1)

	for i, ev := range events {
		enc := encs[ev]
		attr := unix.PerfEventAttr{
			Type:   enc.typ,
			Config: enc.config,
			// Start disabled; we enable the whole group with ioctl after all fds are open.
			Bits: unix.PerfBitDisabled,
		}
		if i == 0 {
			// Leader accumulates values for the whole group in one read.
			attr.Read_format = unix.PERF_FORMAT_GROUP
		}
		attr.Size = uint32(unsafe.Sizeof(attr))

		groupFD := -1
		if i > 0 {
			groupFD = int(leaderFD.Fd())
		}
		// pid=-1 means all processes on the given cpu.
		fd, err := unix.PerfEventOpen(&attr, -1, coreID, groupFD, unix.PERF_FLAG_FD_CLOEXEC)
		if err != nil {
			if leaderFD != nil {
				leaderFD.Close()
			}
			for _, f := range memberFDs {
				f.Close()
			}
			return coreGroup{}, fmt.Errorf("perf_event_open %s cpu%d: %w", ev, coreID, err)
		}
		f := os.NewFile(uintptr(fd), fmt.Sprintf("perf-cpu%d-%s", coreID, ev))
		if i == 0 {
			leaderFD = f
		} else {
			memberFDs = append(memberFDs, f)
		}
	}

	// Enable the group via ioctl so all counters start counting together.
	// PERF_IOC_FLAG_GROUP (1) propagates the enable to every fd in the group.
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL,
		leaderFD.Fd(), uintptr(unix.PERF_EVENT_IOC_ENABLE), 1); errno != 0 {
		leaderFD.Close()
		for _, f := range memberFDs {
			f.Close()
		}
		return coreGroup{}, fmt.Errorf("ioctl enable cpu%d: %w", coreID, errno)
	}
	return coreGroup{
		coreID:    coreID,
		leaderFD:  leaderFD,
		memberFDs: memberFDs,
		events:    events,
	}, nil
}

// readCoreGroup reads all events atomically from the group leader fd.
// The kernel serialises the read buffer as:
//
//	nr          uint64          number of events in the group
//	values[0]   uint64          count for leader event
//	values[1]   uint64          count for first member
//	…
//
// (PERF_FORMAT_GROUP without PERF_FORMAT_ID; event order matches Open order.)
func readCoreGroup(g coreGroup) (map[Event]uint64, error) {
	// Buffer: 8 bytes for nr + 8 bytes per event value.
	buf := make([]byte, 8*(1+len(g.events)))
	if _, err := g.leaderFD.Read(buf); err != nil {
		return nil, err
	}
	nr := binary.LittleEndian.Uint64(buf[0:8])
	if int(nr) != len(g.events) {
		return nil, fmt.Errorf("expected %d counter values, got %d", len(g.events), nr)
	}
	counts := make(map[Event]uint64, len(g.events))
	for i, ev := range g.events {
		counts[ev] = binary.LittleEndian.Uint64(buf[8+i*8 : 16+i*8])
	}
	return counts, nil
}

func closeCoreGroup(g coreGroup) error {
	var firstErr error
	if err := g.leaderFD.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	for _, f := range g.memberFDs {
		if err := f.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
