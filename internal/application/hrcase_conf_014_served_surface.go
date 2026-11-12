package application

import hrcaseconformance "github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/hrcase"

// ServedHRCaseConformanceSurface exposes the HR case and employee-relations
// conformance fixture (CONF-014) through the application boundary used by
// hcmnext serve, so the compartment, separation-of-duties and hold semantics
// are reachable from the shipped dependency closure. The fixture remains a
// pure semantic owner: environments and scenario parameters are supplied by
// the caller and no process-wide registry is created here.
type ServedHRCaseConformanceSurface struct {
	GoldenEnvironment                  func() *hrcaseconformance.Environment
	UnauthorizedParticipantEnvironment func() *hrcaseconformance.Environment
	MatterWallEnvironment              func() *hrcaseconformance.Environment
	LegalHoldEnvironment               func() *hrcaseconformance.Environment
	RetaliationSignalEnvironment       func() *hrcaseconformance.Environment
	AlreadyDisposedEnvironment         func() *hrcaseconformance.Environment
	DegradedObservationEnvironment     func() *hrcaseconformance.Environment
	NewSetup                           func(*hrcaseconformance.Environment) (*hrcaseconformance.Setup, error)
	NewSetupWithParams                 func(*hrcaseconformance.Environment, hrcaseconformance.Params) (*hrcaseconformance.Setup, error)
	CapabilityIDs                      func() []string
}

// NewServedHRCaseConformanceSurface returns the HR case conformance
// capabilities reachable from the served application.
func NewServedHRCaseConformanceSurface() ServedHRCaseConformanceSurface {
	return ServedHRCaseConformanceSurface{
		GoldenEnvironment:                  hrcaseconformance.GoldenEnvironment,
		UnauthorizedParticipantEnvironment: hrcaseconformance.UnauthorizedParticipantEnvironment,
		MatterWallEnvironment:              hrcaseconformance.MatterWallEnvironment,
		LegalHoldEnvironment:               hrcaseconformance.LegalHoldEnvironment,
		RetaliationSignalEnvironment:       hrcaseconformance.RetaliationSignalEnvironment,
		AlreadyDisposedEnvironment:         hrcaseconformance.AlreadyDisposedEnvironment,
		DegradedObservationEnvironment:     hrcaseconformance.DegradedObservationEnvironment,
		NewSetup:                           hrcaseconformance.NewSetup,
		NewSetupWithParams:                 hrcaseconformance.NewSetupWithParams,
		CapabilityIDs: func() []string {
			keys := hrcaseconformance.CapabilityIDs()
			ids := make([]string, 0, len(keys))
			for _, key := range keys {
				ids = append(ids, key.ID)
			}
			return ids
		},
	}
}

// HRCaseConformance returns the immutable HR case conformance surface exposed
// by a composed application. A nil application has no capabilities.
func (a *App) HRCaseConformance() ServedHRCaseConformanceSurface {
	if a == nil {
		return ServedHRCaseConformanceSurface{}
	}
	return NewServedHRCaseConformanceSurface()
}
