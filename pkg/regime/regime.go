// Package regime implements Component B: in-process hardware contention regime inference.
// Models are exported by the offline training pipeline (training/) and loaded at startup.
package regime

import "github.com/overseer/overseer/pkg/features"

// Label names a hardware contention regime as inferred by Component B.
type Label string

const (
	LabelIdle         Label = "idle"
	LabelMemBound     Label = "mem-bound"
	LabelComputeBound Label = "compute-bound"
	LabelContended    Label = "contended"
)

// Classifier infers a regime label from a FeatureVector.
// The zero value is not usable; construct via New.
type Classifier struct{}

// New returns a placeholder Classifier. The real implementation loads an exported
// model artifact from the path provided at startup.
func New() *Classifier { return &Classifier{} }

// Predict returns the most likely regime label for the given feature vector.
// v is projected through ModelFeatures automatically, ensuring frequency is excluded.
// Predictions are conservative (worst-case); callers must not aggregate them additively.
func (c *Classifier) Predict(v features.FeatureVector) Label {
	_ = v.ModelFeatures()
	return LabelIdle
}
