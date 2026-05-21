package pmu

import (
	"context"
	"time"
)

// Collector drives a CounterSource with a periodic ticker and publishes raw
// per-core snapshots on a channel. It owns the Handle returned by Open and
// closes it when Run exits.
//
// Typical usage:
//
//	col, err := pmu.NewCollector(src, []int{0,1,2,3}, events, time.Second, 8)
//	go func() { log.Fatal(col.Run(ctx)) }()
//	for batch := range col.C() { ... }
type Collector struct {
	src      CounterSource
	handle   Handle
	interval time.Duration
	out      chan []RawSample
}

// NewCollector opens coreIDs/events on src and returns a ready Collector.
// interval is the ticker period; bufSize is the output channel buffer depth
// (use ≥1 so a slow consumer does not block the ticker).
func NewCollector(src CounterSource, coreIDs []int, events []Event, interval time.Duration, bufSize int) (*Collector, error) {
	h, err := src.Open(coreIDs, events)
	if err != nil {
		return nil, err
	}
	return &Collector{
		src:      src,
		handle:   h,
		interval: interval,
		out:      make(chan []RawSample, bufSize),
	}, nil
}

// C returns the read-only channel on which sample batches are published.
// The channel is closed when Run returns.
func (c *Collector) C() <-chan []RawSample { return c.out }

// Run starts the sampling loop. It blocks until ctx is cancelled or Read returns
// an error. On exit it closes the CounterSource handle and the output channel.
// Call Run in its own goroutine.
func (c *Collector) Run(ctx context.Context) error {
	defer func() {
		c.src.Close(c.handle)
		close(c.out)
	}()

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			samples, err := c.src.Read(c.handle)
			if err != nil {
				return err
			}
			select {
			case c.out <- samples:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}
