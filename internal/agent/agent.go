// Package agent drives the collect→compute→publish loop.
package agent

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/overseer/overseer/internal/publisher"
	"github.com/overseer/overseer/pkg/features"
	"github.com/overseer/overseer/pkg/nodestate"
	"github.com/overseer/overseer/pkg/pmu"
	"github.com/overseer/overseer/pkg/topology"
)

// Saturation thresholds for socket-level contention classification.
// These are conservative lower bounds; regime inference (P2.2) will refine them.
const (
	llcSaturationThreshold  = 0.05 // LLCMissRate fraction
	dramSaturationThreshold = 0.20 // DRAMStallRatio fraction
)

// Config holds agent tuning parameters. Source and topology are injected via New.
type Config struct {
	NodeName   string
	Interval   time.Duration
	Events     []pmu.Event
	Publishers []publisher.Publisher
}

// Agent drives the PMU collect→feature compute→NodeState publish loop.
type Agent struct {
	cfg  Config
	src  pmu.CounterSource
	topo *topology.TopologySummary
}

func New(cfg Config, src pmu.CounterSource, topo *topology.TopologySummary) *Agent {
	return &Agent{cfg: cfg, src: src, topo: topo}
}

// Run blocks until ctx is cancelled. It opens a pmu.Collector over all cores
// discovered in topo, samples on cfg.Interval, and calls each Publisher after
// every tick. Returns ctx.Err() on clean shutdown; any other error is fatal.
func (a *Agent) Run(ctx context.Context) error {
	coreIDs := make([]int, len(a.topo.Cores))
	for i, c := range a.topo.Cores {
		coreIDs[i] = c.LogicalID
	}

	col, err := pmu.NewCollector(a.src, coreIDs, a.cfg.Events, a.cfg.Interval, 2)
	if err != nil {
		return fmt.Errorf("agent: open collector: %w", err)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- col.Run(ctx) }()

	for {
		select {
		case batch, ok := <-col.C():
			if !ok {
				return <-errCh
			}
			ns := a.buildNodeState(batch)
			for _, p := range a.cfg.Publishers {
				if err := p.Publish(ctx, ns); err != nil {
					log.Printf("agent: publish: %v", err)
				}
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (a *Agent) buildNodeState(batch []pmu.RawSample) nodestate.NodeState {
	cores := make([]features.CoreSample, len(batch))
	for i, s := range batch {
		cores[i] = features.CoreSample{
			CoreID: s.CoreID,
			Counts: s.Counts,
		}
	}
	now := time.Now().UTC()
	fv := features.Compute(features.WindowSamples{
		Start:    now,
		Duration: a.cfg.Interval,
		Cores:    cores,
	})
	return assembleNodeState(a.cfg.NodeName, a.topo, fv)
}

// assembleNodeState converts a FeatureVector and topology into a NodeState.
// Per-socket contention flags are derived from node-wide averages; per-core
// SMT busy detection requires per-workload CPU data and is deferred to P2.2.
func assembleNodeState(nodeName string, topo *topology.TopologySummary, fv features.FeatureVector) nodestate.NodeState {
	type entry struct {
		id    int
		cores []topology.CoreInfo
	}
	socketMap := make(map[int]*entry)
	for _, c := range topo.Cores {
		if socketMap[c.SocketID] == nil {
			socketMap[c.SocketID] = &entry{id: c.SocketID}
		}
		socketMap[c.SocketID].cores = append(socketMap[c.SocketID].cores, c)
	}

	socketIDs := make([]int, 0, len(socketMap))
	for id := range socketMap {
		socketIDs = append(socketIDs, id)
	}
	sort.Ints(socketIDs)

	sockets := make([]nodestate.SocketState, 0, len(socketIDs))
	for _, sid := range socketIDs {
		e := socketMap[sid]
		cs := make([]nodestate.CoreState, 0, len(e.cores))
		for _, ci := range e.cores {
			cs = append(cs, nodestate.CoreState{
				CoreID: ci.LogicalID,
				// SMTSiblingBusy requires per-workload CPU tracking; populated in P2.2.
				SMTSiblingBusy: false,
			})
		}
		sockets = append(sockets, nodestate.SocketState{
			SocketID:       sid,
			L3Contended:    fv.LLCMissRate > llcSaturationThreshold,
			IMCBWSaturated: fv.DRAMStallRatio > dramSaturationThreshold,
			Cores:          cs,
		})
	}

	return nodestate.NodeState{
		NodeName:  nodeName,
		Timestamp: fv.WindowStart,
		Topology:  topo.NodeTopology,
		Sockets:   sockets,
		// WorkloadRegimes populated by regime inference in P2.2.
	}
}
