package governance

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/masking"
)

// MaskingRuntime is the stateless test-data boundary carried by a composed
// governance result. Callers supply the scoped source, profile and evaluation
// time for every operation; no dataset is retained by the runtime.
type MaskingRuntime struct{}

// NewMaskingRuntime returns the masking boundary for one composed process.
func NewMaskingRuntime() MaskingRuntime { return MaskingRuntime{} }

// MaskingRuntime returns the masking boundary selected by the governance
// composition. The returned value has no mutable state of its own.
func (ComposeResult) MaskingRuntime() MaskingRuntime { return NewMaskingRuntime() }

// GenerateMaskedDataset exposes the test-data transformation through the
// composed governance boundary.
func (MaskingRuntime) GenerateMaskedDataset(
	source masking.Dataset,
	profile masking.Profile,
	now time.Time,
) (masking.Result, error) {
	return masking.Generate(source, profile, now)
}

// DestroyMaskedDataset forwards the irreversible in-memory cleanup through
// the same serving boundary that created the result.
func (MaskingRuntime) DestroyMaskedDataset(result *masking.Result) error {
	return masking.Destroy(result)
}
