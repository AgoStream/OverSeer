package pmu

// perfEncoding holds the perf_event_attr Type and Config values for a single event
// on a specific microarchitecture.
type perfEncoding struct {
	typ    uint32
	config uint64
}

// perf_event_attr Type field constants (linux/perf_event.h).
const (
	perfTypeHardware uint32 = 0
	perfTypeRaw      uint32 = 4
)

// Config constants for PERF_TYPE_HARDWARE.
const (
	perfHWCPUCycles    uint64 = 0
	perfHWInstructions uint64 = 1
)

// encodingTable maps microarchitecture name → event name → perf encoding.
// Extend this table when characterising a new uarch; DO NOT add a new uarch
// until the encoding has been validated against hardware (heterogeneity caveat).
//
// Sapphire Rapids (SPR) event codes follow Intel SDM vol 3B and
// linux/arch/x86/events/intel/core.c. Raw config layout: bits[7:0]=event select,
// bits[15:8]=umask, bits[31:24]=cmask.
var encodingTable = map[string]map[Event]perfEncoding{
	"sapphire_rapids": {
		EventInstructions: {typ: perfTypeHardware, config: perfHWInstructions},
		EventCycles:       {typ: perfTypeHardware, config: perfHWCPUCycles},
		// MEM_LOAD_RETIRED.L1_MISS  event=0xd1 umask=0x08
		EventL1Miss: {typ: perfTypeRaw, config: 0x0800d1},
		// MEM_LOAD_RETIRED.L3_MISS  event=0xd1 umask=0x20
		EventLLCMiss: {typ: perfTypeRaw, config: 0x2000d1},
		// CYCLE_ACTIVITY.STALLS_L3_MISS  event=0xa3 umask=0x06 cmask=0x06
		EventDRAMStall: {typ: perfTypeRaw, config: 0x0600a3},
	},
}

// lookupEncodings resolves symbolic event names to uarch-specific perf encodings.
// Returns ErrUnknownEvents if the uarch is unrecognised or any individual event
// has no entry in the table for that uarch.
func lookupEncodings(uarch string, events []Event) (map[Event]perfEncoding, error) {
	table, ok := encodingTable[uarch]
	if !ok {
		return nil, &ErrUnknownEvents{
			Uarch:   uarch,
			Missing: append([]Event(nil), events...),
		}
	}
	out := make(map[Event]perfEncoding, len(events))
	var missing []Event
	for _, ev := range events {
		enc, ok := table[ev]
		if !ok {
			missing = append(missing, ev)
			continue
		}
		out[ev] = enc
	}
	if len(missing) > 0 {
		return nil, &ErrUnknownEvents{Uarch: uarch, Missing: missing}
	}
	return out, nil
}
