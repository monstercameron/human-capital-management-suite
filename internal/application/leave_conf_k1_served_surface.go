package application

import (
	"github.com/monstercameron/human-capital-management-suite/internal/commercial"
	leave "github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/leave"
)

// ServedLeaveConformanceSurface exposes the shared Leave conformance oracle
// through the application boundary used by hcmnext serve. The oracle remains
// a pure semantic owner: the tenant entitlement snapshot is supplied by the
// caller and no process-wide registry is created here.
type ServedLeaveConformanceSurface struct {
	ServingContractID       func() string
	ValidateServingContract func() error
	NewEntitlementGate      func(commercial.EntitlementSnapshot) (leave.EntitlementGate, error)
}

// NewServedLeaveConformanceSurface returns the Leave conformance capabilities
// reachable from the served application.
func NewServedLeaveConformanceSurface() ServedLeaveConformanceSurface {
	return ServedLeaveConformanceSurface{
		ServingContractID:       func() string { return leave.ServingContractID },
		ValidateServingContract: leave.ValidateServingContract,
		NewEntitlementGate:      leave.NewEntitlementGate,
	}
}

// LeaveConformance returns the immutable Leave conformance surface exposed by
// a composed application. A nil application has no capabilities.
func (a *App) LeaveConformance() ServedLeaveConformanceSurface {
	if a == nil {
		return ServedLeaveConformanceSurface{}
	}
	return NewServedLeaveConformanceSurface()
}
