package application

import "github.com/monstercameron/human-capital-management-suite/internal/domains/availability"

// ServedAvailabilitySurface exposes the availability facts and pure
// simulations carried by a composed hcmnext application. The surface keeps
// authority with the availability domain: callers provide the tenant-bound
// facts, schedule revision and owning revision log explicitly.
type ServedAvailabilitySurface struct {
	ValidateWorkerAvailability func(availability.WorkerAvailability) error
	ValidateAbsenceImpact      func(availability.AbsenceImpact) error
	SimulateAbsence            func(availability.AbsenceSimulationRequest) (availability.AbsenceSimulationResult, error)
	SimulateVersionedAbsence   func(availability.AbsenceWindowRequest) (availability.AbsenceWindowResult, error)
	NewRevisionLog             func() *availability.RevisionLog
	PrepareRevision            func(*availability.RevisionLog, availability.Revision) (string, error)
	CommitRevision             func(*availability.RevisionLog, string, func(string) error) (availability.Revision, error)
	RecoverRevisions           func(*availability.RevisionLog) ([]availability.Revision, error)
}

// NewServedAvailabilitySurface returns the availability capabilities reachable
// from the serve composition. It constructs no process-wide or tenant-wide
// state; revision ownership remains with the log supplied by the caller.
func NewServedAvailabilitySurface() ServedAvailabilitySurface {
	return ServedAvailabilitySurface{
		ValidateWorkerAvailability: func(in availability.WorkerAvailability) error { return in.Validate() },
		ValidateAbsenceImpact:      func(in availability.AbsenceImpact) error { return in.Validate() },
		SimulateAbsence:            availability.SimulateAbsence,
		SimulateVersionedAbsence:   availability.SimulateVersionedAbsence,
		NewRevisionLog:             availability.NewRevisionLog,
		PrepareRevision: func(log *availability.RevisionLog, in availability.Revision) (string, error) {
			return log.Prepare(in)
		},
		CommitRevision: func(log *availability.RevisionLog, token string, inject func(string) error) (availability.Revision, error) {
			return log.Commit(token, inject)
		},
		RecoverRevisions: func(log *availability.RevisionLog) ([]availability.Revision, error) {
			return log.Recover()
		},
	}
}

// Availability returns the availability surface exposed by a composed app.
// The nil receiver form is deliberately empty, matching other application
// capability accessors and avoiding a hidden process-level fallback.
func (a *App) Availability() ServedAvailabilitySurface {
	if a == nil {
		return ServedAvailabilitySurface{}
	}
	return NewServedAvailabilitySurface()
}
