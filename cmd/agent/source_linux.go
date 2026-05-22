//go:build linux

package main

import "github.com/overseer/overseer/pkg/pmu"

func newPerfSource(uarch string) (pmu.CounterSource, error) {
	return pmu.NewPerfBackend(uarch), nil
}
