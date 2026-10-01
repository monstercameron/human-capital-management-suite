package application

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/enterprisegate"
	"github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/authn/outage"
	"github.com/monstercameron/human-capital-management-suite/internal/authn/subjectlink"
	trustoutage "github.com/monstercameron/human-capital-management-suite/internal/trust/outage"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

// ServedAuthnSurface is the read-only adapter set exposed by the application
// composition boundary for authentication capabilities that are otherwise
// easy to leave as library-only code. It carries constructors and pure
// decisions, not mutable registries or authority. Stores, issuer policy,
// tenant scope and evidence remain supplied by their owning composition.
//
// Keeping these edges in application makes the capabilities part of the
// hcmnext serve dependency closure while preserving each package's semantic
// ownership and fail-closed behavior.
type ServedAuthnSurface struct {
	NewSubjectLinkService   func(subjectlink.Store, issuerregistry.Store) *subjectlink.Service
	NewWorkloadIssuer       func(workload.MTLSIssuerConfig) (*workload.MTLSIssuer, error)
	NewWorkloadVerifier     func(workload.MTLSVerifierConfig) (*workload.MTLSVerifier, error)
	DecideIdentityOutage    func(outage.Profile, outage.Phase, trustoutage.Health, outage.Request, time.Time) (outage.Decision, error)
	ReconcileIdentityOutage func(outage.Profile, trustoutage.Health, []outage.RecordedUse, time.Time) ([]outage.ReconciliationResult, error)
	EvaluateEnterpriseGate  func(enterprisegate.Record, time.Time) (enterprisegate.Decision, error)
	EnterpriseSchema        func() enterprisegate.SchemaDescriptor
}

// NewServedAuthnSurface returns the authentication capabilities available to
// a served composition. The returned value contains no process or tenant
// state, so callers must still provide current request inputs and stores.
func NewServedAuthnSurface() ServedAuthnSurface {
	return ServedAuthnSurface{
		NewSubjectLinkService:   subjectlink.New,
		NewWorkloadIssuer:       workload.NewMTLSIssuer,
		NewWorkloadVerifier:     workload.NewMTLSVerifier,
		DecideIdentityOutage:    outage.Decide,
		ReconcileIdentityOutage: outage.Reconcile,
		EvaluateEnterpriseGate:  enterprisegate.EvaluateAt,
		EnterpriseSchema:        enterprisegate.Schema,
	}
}

// Authn returns the immutable authentication surface exposed by a composed
// application. It does not create or retain authority; every call still
// supplies the current tenant-scoped stores and evidence inputs.
func (a *App) Authn() ServedAuthnSurface {
	if a == nil {
		return ServedAuthnSurface{}
	}
	return NewServedAuthnSurface()
}
