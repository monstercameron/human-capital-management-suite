package application

import (
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/versionexplain"
)

// ServedVersionExplanationSurface exposes version explanation through the
// composed application. The versionexplain package remains the semantic
// owner; this projection only makes its read-only resolver reachable from a
// running hcmnext serve composition.
type ServedVersionExplanationSurface struct {
	NewExplainer func(versionexplain.Recorder) *versionexplain.Explainer
	Explain      func(versionexplain.Recorder, versionexplain.Snapshot, string, values.Instant) (versionexplain.Explanation, error)
	IsRejected   func(error) bool
}

// NewServedVersionExplanationSurface returns the version explanation
// capability available through a served application composition.
func NewServedVersionExplanationSurface() ServedVersionExplanationSurface {
	return ServedVersionExplanationSurface{
		NewExplainer: versionexplain.NewExplainer,
		Explain: func(recorder versionexplain.Recorder, snapshot versionexplain.Snapshot, subject string, at values.Instant) (versionexplain.Explanation, error) {
			return versionexplain.NewExplainer(recorder).Explain(snapshot, subject, at)
		},
		IsRejected: versionexplain.IsRejected,
	}
}

// VersionExplanation returns the immutable explanation surface exposed by a
// composed application. A nil application has no served capabilities.
func (a *App) VersionExplanation() ServedVersionExplanationSurface {
	if a == nil {
		return ServedVersionExplanationSurface{}
	}
	return NewServedVersionExplanationSurface()
}
