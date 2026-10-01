package application

import programconformance "github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/program"

// ServedProgramConformanceSurface exposes the shared Program conformance
// oracle through the application boundary used by hcmnext serve. The oracle
// remains a pure semantic owner: fixtures and tenant scope are supplied by the
// caller and no process-wide registry is created here.
type ServedProgramConformanceSurface struct {
	ValidateFixture        func(programconformance.Fixture) error
	CheckSharedAbstraction func([]programconformance.Fixture, string) (programconformance.Report, error)
}

// NewServedProgramConformanceSurface returns the Program conformance
// capabilities reachable from the served application.
func NewServedProgramConformanceSurface() ServedProgramConformanceSurface {
	return ServedProgramConformanceSurface{
		ValidateFixture:        programconformance.ValidateFixture,
		CheckSharedAbstraction: programconformance.CheckSharedAbstraction,
	}
}

// ProgramConformance returns the immutable Program conformance surface
// exposed by a composed application. A nil application has no capabilities.
func (a *App) ProgramConformance() ServedProgramConformanceSurface {
	if a == nil {
		return ServedProgramConformanceSurface{}
	}
	return NewServedProgramConformanceSurface()
}
