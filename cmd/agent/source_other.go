//go:build !linux

package main

import (
	"errors"

	"github.com/overseer/overseer/pkg/pmu"
)

func newPerfSource(_ string) (pmu.CounterSource, error) {
	return nil, errors.New("perf_event_open is only available on Linux; use --source=replay")
}
