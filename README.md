# OverSeer

Kubernetes scheduler extension that places CNFs using PMU-derived hardware telemetry.
Targets Sapphire Rapids nodes; the agent tolerates per-node PMU-event differences.

## Components

- **Node Agent**  Go (`cmd/agent`)  Collects PMU counters, builds feature vectors, pushes NodeState to the API server 
- **Component A — Training** Python (`training/`) Offline pipeline: regime labelling, interference model training, artifact export
- **Component B — Regime Inference**  Go (`pkg/regime`)  In-process inference of hardware contention regime from live feature vectors 
- **Scheduler Plugin**  Go (`cmd/scheduler`)  Kubernetes scheduling-framework plugin; scores nodes using regime + interference predictions 
- **Eval Harness**  Go + Python Holds out workloads, measures held-out accuracy, drives continuous retraining

## Build Order

Data contracts must be stable before any component writes code that depends on them.

1. **Freeze data contracts** — `pkg/features` (Vector schema) and `pkg/nodestate` (NodeState schema).
2. **Node Agent** (`cmd/agent`) — reads PMU events, emits NodeState.
3. **Training pipeline** (`training/`) — offline; consumes labelled NodeState traces, exports model artifacts.
4. **Regime inference** (`pkg/regime`) — loads exported artifacts, runs in-process on the node.
5. **Scheduler Plugin** (`cmd/scheduler`) — consumes NodeState + regime labels, scores nodes.
6. **Eval Harness** — closes the loop; triggers retraining when held-out accuracy degrades.

## Quick Start

```bash
make build        # compile agent + scheduler binaries → bin/
make test         # Go unit tests
make lint         # golangci-lint
make fmt          # gofumpt
```

Python (training only):

```bash
cd training
pip install -e ".[dev]"
make python-lint  # ruff
make python-test  # pytest
```

Or run everything:

```bash
make all
```

## Key Design Invariants

Short form:

- **Single µarch** — Sapphire Rapids characterised; agent tolerates per-node event differences.
- **Frequency excluded** — it is a utilization confound, not a regime signal.
- **Non-additive contention** — predictions are conservative/worst-case; never summed across workloads.
- **Small datasets** — report held-out-workload accuracy; design for continuous retraining.
- **Language split** — Go everywhere except `training/`; Python must not appear outside that directory.
- **Data contracts first** — `pkg/features` and `pkg/nodestate` are frozen before downstream components are written.
