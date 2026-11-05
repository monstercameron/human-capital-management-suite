package application

import terminationconformance "github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/termination"

// ServedTerminationConformanceSurface exposes the shared Termination
// conformance oracle through the application boundary used by hcmnext serve.
// The oracle remains a pure semantic owner: environments and scenario
// parameters are supplied by the caller and no process-wide registry is
// created here.
type ServedTerminationConformanceSurface struct {
	GoldenEnvironment              func() *terminationconformance.Environment
	LegalHoldEnvironment           func() *terminationconformance.Environment
	AlreadyEndedEnvironment        func() *terminationconformance.Environment
	DegradedObservationEnvironment func() *terminationconformance.Environment
	NewSetup                       func(*terminationconformance.Environment) (*terminationconformance.Setup, error)
	NewSetupWithParams             func(*terminationconformance.Environment, terminationconformance.Params) (*terminationconformance.Setup, error)
	ReferenceWorkflowID            func() string
	CapabilityIDs                  func() []string
}

// NewServedTerminationConformanceSurface returns the Termination conformance
// capabilities reachable from the served application.
func NewServedTerminationConformanceSurface() ServedTerminationConformanceSurface {
	return ServedTerminationConformanceSurface{
		GoldenEnvironment:              terminationconformance.GoldenEnvironment,
		LegalHoldEnvironment:           terminationconformance.LegalHoldEnvironment,
		AlreadyEndedEnvironment:        terminationconformance.AlreadyEndedEnvironment,
		DegradedObservationEnvironment: terminationconformance.DegradedObservationEnvironment,
		NewSetup:                       terminationconformance.NewSetup,
		NewSetupWithParams:             terminationconformance.NewSetupWithParams,
		ReferenceWorkflowID:            func() string { return terminationconformance.WorkflowID },
		CapabilityIDs: func() []string {
			keys := terminationconformance.CapabilityIDs()
			ids := make([]string, 0, len(keys))
			for _, key := range keys {
				ids = append(ids, key.ID)
			}
			return ids
		},
	}
}

// TerminationConformance returns the immutable Termination conformance surface
// exposed by a composed application. A nil application has no capabilities.
func (a *App) TerminationConformance() ServedTerminationConformanceSurface {
	if a == nil {
		return ServedTerminationConformanceSurface{}
	}
	return NewServedTerminationConformanceSurface()
}
