package productcorrelation

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ValidateServingContract exercises privacy-safe product correlation through
// the package boundary used by a composed application. Runtime callers still
// provide the tenant-scoped events and compiled telemetry allowlist.
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
		return fmt.Errorf("productcorrelation: serving principal: %w", err)
	}
	allowlist, err := telemetry.DefaultAllowlist()
	if err != nil {
		return fmt.Errorf("productcorrelation: serving allowlist: %w", err)
	}
	events := make([]Event, 0, len(Surfaces()))
	for _, surface := range Surfaces() {
		events = append(events, Event{Surface: surface, Name: "serving.event", Tenant: values.TenantId("serving-tenant"), CorrelationID: "serving-correlation", At: now})
	}
	report, err := Correlate(principal, allowlist, events)
	if err != nil {
		return fmt.Errorf("productcorrelation: serving correlation: %w", err)
	}
	if report.Tenant != values.TenantId("serving-tenant") || len(report.Timelines) != 1 || !report.Timelines[0].Complete {
		return fmt.Errorf("productcorrelation: serving correlation returned incomplete timeline")
	}
	return nil
}
