package application_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/authn/enterprisegate"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

// TestTodo_AUTHN_003_Served proves the governed subject-link service is
// reachable through the application boundary used by hcmnext serve.
func TestTodo_AUTHN_003_Served(t *testing.T) {
	surface := application.NewServedAuthnSurface()
	if surface.NewSubjectLinkService == nil {
		t.Fatal("served authn surface omitted subject-link service")
	}
	if surface.NewSubjectLinkService(nil, nil) == nil {
		t.Fatal("served subject-link factory did not create a service")
	}
}

// TestTodo_AUTHN_006_Served proves workload mTLS construction is reachable
// from the served application surface and still delegates validation.
func TestTodo_AUTHN_006_Served(t *testing.T) {
	surface := application.NewServedAuthnSurface()
	if surface.NewWorkloadIssuer == nil || surface.NewWorkloadVerifier == nil {
		t.Fatal("served authn surface omitted workload mTLS constructors")
	}
	if _, err := surface.NewWorkloadIssuer(workload.MTLSIssuerConfig{}); err == nil {
		t.Fatal("invalid workload issuer configuration was accepted")
	}
}

// TestTodo_AUTHN_007_Served proves outage decisions are on the served
// application surface rather than test-only imports.
func TestTodo_AUTHN_007_Served(t *testing.T) {
	surface := application.NewServedAuthnSurface()
	if surface.DecideIdentityOutage == nil || surface.ReconcileIdentityOutage == nil {
		t.Fatal("served authn surface omitted identity-outage decisions")
	}
}

// TestTodo_AUTHN_008_Served proves the enterprise gate schema is available on
// the served surface without enabling either optional protocol.
func TestTodo_AUTHN_008_Served(t *testing.T) {
	surface := application.NewServedAuthnSurface()
	if surface.EvaluateEnterpriseGate == nil || surface.EnterpriseSchema == nil {
		t.Fatal("served authn surface omitted enterprise gate")
	}
	if schema := surface.EnterpriseSchema(); schema.Version != enterprisegate.Version() || len(schema.Protocols) != 2 {
		t.Fatalf("enterprise schema = %+v, want version %d with both protocols", schema, enterprisegate.Version())
	}
}

// TestTodo_AUTHN_ServedApp proves the composed application exposes the same
// surface without a package-level registry.
func TestTodo_AUTHN_ServedApp(t *testing.T) {
	var app application.App
	got := app.Authn()
	if got.NewSubjectLinkService == nil || got.NewWorkloadIssuer == nil || got.DecideIdentityOutage == nil || got.EnterpriseSchema == nil {
		t.Fatal("composed application did not expose the served authentication surface")
	}
}
