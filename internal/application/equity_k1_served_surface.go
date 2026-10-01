package application

import (
	"github.com/monstercameron/human-capital-management-suite/internal/domains/equity"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ServedEquitySurface exposes the equity plan, grant, vesting, and acceptance
// contract through the application boundary used by hcmnext serve. The equity
// domain remains the semantic owner; this value only makes its immutable
// operations reachable from a composed serving process.
type ServedEquitySurface struct {
	Version         func() int
	NewPlanRevision func(equity.EquityPlanRevision) (equity.EquityPlanRevision, error)
	NewGrant        func(equity.EquityGrant) (equity.EquityGrant, error)
	NewAcceptance   func(equity.AcceptanceEvent) (equity.AcceptanceEvent, error)
	ValidateGrant   func(equity.EquityPlanRevision, equity.EquityGrant) error
	ComputeVesting  func(equity.VestingSchedule, values.LocalDate, values.Decimal) ([]equity.VestingTranche, error)
	Accept          func(equity.EquityGrant, equity.AcceptanceEvent) (equity.EquityGrant, error)
	Activate        func(equity.EquityGrant, string) (equity.EquityGrant, error)
	Cancel          func(equity.EquityGrant, string) (equity.EquityGrant, error)
	Settle          func(equity.EquityGrant, string) (equity.EquityGrant, error)
	Explain         func(equity.EquityGrant) (equity.GrantExplanation, error)
}

// NewServedEquitySurface returns the equity capabilities reachable from a
// served application without creating process-wide or tenant-wide state.
func NewServedEquitySurface() ServedEquitySurface {
	return ServedEquitySurface{
		Version:         equity.Version,
		NewPlanRevision: equity.NewEquityPlanRevision,
		NewGrant:        equity.NewEquityGrant,
		NewAcceptance:   equity.NewAcceptanceEvent,
		ValidateGrant:   func(plan equity.EquityPlanRevision, grant equity.EquityGrant) error { return plan.ValidateGrant(grant) },
		ComputeVesting:  equity.ComputeVesting,
		Accept: func(grant equity.EquityGrant, event equity.AcceptanceEvent) (equity.EquityGrant, error) {
			return grant.Accept(event)
		},
		Activate: func(grant equity.EquityGrant, evidence string) (equity.EquityGrant, error) {
			return grant.Activate(evidence)
		},
		Cancel: func(grant equity.EquityGrant, evidence string) (equity.EquityGrant, error) {
			return grant.Cancel(evidence)
		},
		Settle: func(grant equity.EquityGrant, evidence string) (equity.EquityGrant, error) {
			return grant.Settle(evidence)
		},
		Explain: equity.Explain,
	}
}

// Equity returns the equity surface exposed by a composed application. A nil
// application has no served capabilities.
func (a *App) Equity() ServedEquitySurface {
	if a == nil {
		return ServedEquitySurface{}
	}
	return NewServedEquitySurface()
}
