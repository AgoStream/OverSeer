package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/overseer/overseer/internal/agent"
	"github.com/overseer/overseer/internal/publisher"
	"github.com/overseer/overseer/pkg/pmu"
	"github.com/overseer/overseer/pkg/topology"
)

var (
	flagSource   = flag.String("source", "perf", "counter source: perf or replay")
	flagTrace    = flag.String("trace", "", "path to trace NDJSON file (--source=replay only)")
	flagNode     = flag.String("node", defaultNodeName(), "kubernetes node name")
	flagInterval = flag.Duration("interval", time.Second, "PMU sampling interval")
	flagUarch    = flag.String("uarch", "sapphire_rapids", "microarchitecture name")
	flagAddr     = flag.String("addr", ":9100", "HTTP /state endpoint listen address")
)

func defaultNodeName() string {
	if n := os.Getenv("NODE_NAME"); n != "" {
		return n
	}
	n, _ := os.Hostname()
	return n
}

func main() {
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var (
		src  pmu.CounterSource
		topo *topology.TopologySummary
	)

	switch *flagSource {
	case "replay":
		var frames []pmu.TraceFrame
		if *flagTrace != "" {
			var err error
			frames, err = pmu.LoadTraceFile(*flagTrace)
			if err != nil {
				log.Fatalf("agent: load trace: %v", err)
			}
		}
		src = pmu.NewReplayBackend(*flagUarch, frames, 0)
		topo = replayTopology()

	case "perf":
		var err error
		src, err = newPerfSource(*flagUarch)
		if err != nil {
			log.Fatalf("agent: perf backend: %v", err)
		}
		topo, err = topology.DiscoverFromSysfs()
		if err != nil {
			log.Fatalf("agent: topology: %v", err)
		}

	default:
		log.Fatalf("agent: unknown --source %q (want perf or replay)", *flagSource)
	}

	httpPub := publisher.NewHTTP(*flagAddr)
	pubs := []publisher.Publisher{httpPub}

	crdPub, err := publisher.NewCRD()
	if err != nil {
		log.Printf("agent: CRD publisher init failed: %v", err)
	} else if crdPub != nil {
		pubs = append(pubs, crdPub)
	}

	go func() {
		if err := httpPub.ListenAndServe(ctx); err != nil {
			log.Printf("agent: HTTP server: %v", err)
		}
	}()

	events := []pmu.Event{
		pmu.EventInstructions,
		pmu.EventCycles,
		pmu.EventL1Miss,
		pmu.EventLLCMiss,
		pmu.EventDRAMStall,
	}

	ag := agent.New(agent.Config{
		NodeName:   *flagNode,
		Interval:   *flagInterval,
		Events:     events,
		Publishers: pubs,
	}, src, topo)

	if err := ag.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("agent: %v", err)
	}
}

// replayTopology returns a minimal two-core, one-socket, one-NUMA topology
// for use with --source=replay on hardware without sysfs.
func replayTopology() *topology.TopologySummary {
	return &topology.TopologySummary{
		NodeTopology: topology.NodeTopology{
			Sockets:        1,
			NUMANodes:      1,
			CoresPerSocket: 2,
		},
		Cores: []topology.CoreInfo{
			{LogicalID: 0, PhysicalID: 0, SocketID: 0, NUMANodeID: 0, ThreadSiblings: []int{0, 1}},
			{LogicalID: 1, PhysicalID: 0, SocketID: 0, NUMANodeID: 0, ThreadSiblings: []int{0, 1}},
		},
		L3Groups: []topology.CacheGroup{
			{ID: 0, CPUs: []int{0, 1}},
		},
		IMCGroups: []topology.IMCGroup{
			{ID: 0, NUMANode: 0, CPUs: []int{0, 1}},
		},
	}
}
