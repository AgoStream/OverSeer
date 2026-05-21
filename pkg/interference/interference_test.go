package interference_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/overseer/overseer/pkg/interference"
)

func TestDegradationScoreRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   interference.DegradationScore
	}{
		{
			name: "single_source_moderate",
			in: interference.DegradationScore{
				WorkloadID:    "cnf-a",
				KPIMultiplier: 1.35,
				Conservative:  false,
				Source:        interference.SourceSingle,
			},
		},
		{
			name: "stacked_conservative",
			in: interference.DegradationScore{
				WorkloadID:    "cnf-b",
				KPIMultiplier: 1.80,
				Conservative:  true,
				Source:        interference.SourceStacked,
			},
		},
		{
			name: "no_degradation_single",
			in: interference.DegradationScore{
				WorkloadID:    "cnf-c",
				KPIMultiplier: 1.0,
				Conservative:  false,
				Source:        interference.SourceSingle,
			},
		},
		{
			// A score below 1.0 is theoretically possible (e.g. cache-warming side-effect).
			name: "sub_one_multiplier",
			in: interference.DegradationScore{
				WorkloadID:    "cnf-d",
				KPIMultiplier: 0.95,
				Conservative:  false,
				Source:        interference.SourceSingle,
			},
		},
		{
			// Zero value must round-trip without error.
			name: "zero_value",
			in:   interference.DegradationScore{},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var got interference.DegradationScore
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if !reflect.DeepEqual(tc.in, got) {
				t.Errorf("round-trip mismatch\n want: %+v\n  got: %+v", tc.in, got)
			}
		})
	}
}

func TestSourceKindJSONValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind interference.SourceKind
		want string
	}{
		{interference.SourceSingle, `"single"`},
		{interference.SourceStacked, `"stacked"`},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(tc.kind)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(data) != tc.want {
				t.Errorf("SourceKind JSON: want %s got %s", tc.want, data)
			}
		})
	}
}

func TestDegradationScoreJSONFieldNames(t *testing.T) {
	t.Parallel()

	s := interference.DegradationScore{
		WorkloadID:    "cnf-x",
		KPIMultiplier: 1.5,
		Conservative:  true,
		Source:        interference.SourceStacked,
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}
	for _, want := range []string{"workload_id", "kpi_multiplier", "conservative", "source"} {
		if _, ok := m[want]; !ok {
			t.Errorf("expected JSON key %q not found in %s", want, data)
		}
	}
}
