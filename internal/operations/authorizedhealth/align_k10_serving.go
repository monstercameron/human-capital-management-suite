package authorizedhealth

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ValidateServingContract exercises the operator-health projection through the
// same package boundary used by a composed application. It is intentionally
// deterministic and in-memory; data/workflow owners remain responsible for
// supplying observations to Project at runtime.
func ValidateServingContract() error {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "serving-tenant", Subject: "serving-operator", SubjectKind: trust.SubjectKindHuman,
		Roles: []string{admin.OperatorRole}, Purposes: []string{"operator_diagnostics"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "serving-session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute),
		CredentialDigest: "serving-credential",
	})
	if err != nil {
		return fmt.Errorf("authorizedhealth: serving principal: %w", err)
	}
	view, err := Project(principal, Request{
		Tenant: "serving-tenant", Now: now,
		Projections:    []ProjectionObservation{{Name: "serving-projection", SchemaVersion: "v1", DefinitionVersion: "v1", SourceSequence: 1, AppliedSequence: 1, ObservedAt: now, MaxAge: time.Minute, Status: "CURRENT"}},
		Outbox:         &OutboxObservation{SchemaVersion: "v1", ObservedAt: now, MaxAge: time.Minute, MaxPendingAge: time.Minute, Status: "HEALTHY"},
		Reconciliation: &ReconciliationObservation{SchemaVersion: "v1", ObservedAt: now, MaxAge: time.Minute, Status: "HEALTHY"},
	})
	if err != nil {
		return fmt.Errorf("authorizedhealth: serving projection: %w", err)
	}
	if view.State != StateHealthy || len(view.Projections) != 1 || view.Outbox == nil || view.Reconciliation == nil {
		return fmt.Errorf("authorizedhealth: serving projection returned incomplete healthy view")
	}
	if view.PolicyVersion == "" {
		return fmt.Errorf("authorizedhealth: serving projection omitted policy version")
	}
	return nil
}
