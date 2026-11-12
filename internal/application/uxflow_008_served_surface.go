package application

import "github.com/monstercameron/human-capital-management-suite/internal/experience/continuity"

// ServedExperienceContinuitySurface exposes the complete-flow continuity
// evidence contract (UXFLOW-008) through the application boundary used by
// hcmnext serve, so route validation, normalized intent digests and route
// equivalence are reachable from the shipped dependency closure. The
// contract is a pure semantic owner: flows and requests are supplied by the
// caller, and the surface performs no business mutation or authority grant.
type ServedExperienceContinuitySurface struct {
	Execute              func(continuity.FlowSpec, continuity.Request) (continuity.Outcome, error)
	ValidateCompleteFlow func(continuity.FlowSpec, continuity.Request) error
	NormalizeIntent      func(map[string]string) (map[string]string, string, error)
	Equivalent           func(continuity.Outcome, continuity.Outcome) bool
}

// NewServedExperienceContinuitySurface returns the continuity capabilities
// reachable from the served application.
func NewServedExperienceContinuitySurface() ServedExperienceContinuitySurface {
	return ServedExperienceContinuitySurface{
		Execute:              continuity.Execute,
		ValidateCompleteFlow: continuity.ValidateCompleteFlow,
		NormalizeIntent:      continuity.NormalizeIntent,
		Equivalent:           continuity.Equivalent,
	}
}

// ExperienceContinuity returns the continuity surface exposed by a composed
// application. A nil application exposes no capabilities.
func (a *App) ExperienceContinuity() ServedExperienceContinuitySurface {
	if a == nil {
		return ServedExperienceContinuitySurface{}
	}
	return NewServedExperienceContinuitySurface()
}
