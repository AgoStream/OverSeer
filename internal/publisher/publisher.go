// Package publisher defines the Publisher interface and lightweight adapters
// for delivering NodeState snapshots to different output channels.
package publisher

import (
	"context"

	"github.com/overseer/overseer/pkg/nodestate"
)

// Publisher accepts a NodeState snapshot for delivery to one output channel.
// Implementations must be safe for concurrent calls.
type Publisher interface {
	Publish(ctx context.Context, ns nodestate.NodeState) error
}

// Func is a function that satisfies Publisher, useful in tests.
type Func func(ctx context.Context, ns nodestate.NodeState) error

func (f Func) Publish(ctx context.Context, ns nodestate.NodeState) error {
	return f(ctx, ns)
}
