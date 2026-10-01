package application

import "github.com/monstercameron/human-capital-management-suite/internal/domains/program"

// ServedProgramSurface exposes program outcome and reconciliation capabilities
// through the application boundary used by hcmnext serve. Catalog state and
// caller authorization remain explicit inputs owned by the program domain.
type ServedProgramSurface struct {
	ContractID         string
	NewCatalog         func() *program.Catalog
	Calculate          func(*program.Catalog, program.OutcomeInput) (program.OutcomeResult, error)
	Reconcile          func(*program.Catalog, []program.Expectation, []program.Observation) ([]program.Discrepancy, []program.RepairPlan, error)
	SealReconciliation func(*program.Catalog, []program.Expectation, []program.Observation) (string, error)
}

// NewServedProgramSurface returns the program capabilities reachable from a
// served application without creating process-wide or tenant-wide state.
func NewServedProgramSurface() ServedProgramSurface {
	return ServedProgramSurface{
		ContractID: program.ServingContractID,
		NewCatalog: program.NewCatalog,
		Calculate: func(c *program.Catalog, in program.OutcomeInput) (program.OutcomeResult, error) {
			return c.Calculate(in)
		},
		Reconcile: func(c *program.Catalog, expected []program.Expectation, observed []program.Observation) ([]program.Discrepancy, []program.RepairPlan, error) {
			return c.Reconcile(expected, observed)
		},
		SealReconciliation: func(c *program.Catalog, expected []program.Expectation, observed []program.Observation) (string, error) {
			return c.SealReconciliation(expected, observed)
		},
	}
}

// Program returns the program surface exposed by a composed application.
// A nil receiver is deliberately empty, matching other application surfaces.
func (a *App) Program() ServedProgramSurface {
	if a == nil {
		return ServedProgramSurface{}
	}
	return NewServedProgramSurface()
}
