package application

import talentconformance "github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/talent"

// ServedTalentConformanceSurface exposes the shared Talent
// conformance oracle through the application boundary used by hcmnext serve.
// The oracle remains a pure semantic owner: environments and scenario
// parameters are supplied by the caller and no process-wide registry is
// created here.
type ServedTalentConformanceSurface struct {
	GoldenEnvironment              func() *talentconformance.Environment
	StalePopulationEnvironment     func() *talentconformance.Environment
	CalibrationConflictEnvironment func() *talentconformance.Environment
	DegradedObservationEnvironment func() *talentconformance.Environment
	NewSetup                       func(*talentconformance.Environment) (*talentconformance.Setup, error)
	NewSetupWithParams             func(*talentconformance.Environment, talentconformance.Params) (*talentconformance.Setup, error)
	ReferenceWorkflowID            func() string
	CapabilityIDs                  func() []string
}

// NewServedTalentConformanceSurface returns the Talent conformance
// capabilities reachable from the served application.
func NewServedTalentConformanceSurface() ServedTalentConformanceSurface {
	return ServedTalentConformanceSurface{
		GoldenEnvironment:              talentconformance.GoldenEnvironment,
		StalePopulationEnvironment:     talentconformance.StalePopulationEnvironment,
		CalibrationConflictEnvironment: talentconformance.CalibrationConflictEnvironment,
		DegradedObservationEnvironment: talentconformance.DegradedObservationEnvironment,
		NewSetup:                       talentconformance.NewSetup,
		NewSetupWithParams:             talentconformance.NewSetupWithParams,
		ReferenceWorkflowID:            func() string { return talentconformance.WorkflowID },
		CapabilityIDs: func() []string {
			keys := talentconformance.CapabilityIDs()
			ids := make([]string, 0, len(keys))
			for _, key := range keys {
				ids = append(ids, key.ID)
			}
			return ids
		},
	}
}

// TalentConformance returns the immutable Talent conformance surface
// exposed by a composed application. A nil application has no capabilities.
func (a *App) TalentConformance() ServedTalentConformanceSurface {
	if a == nil {
		return ServedTalentConformanceSurface{}
	}
	return NewServedTalentConformanceSurface()
}
