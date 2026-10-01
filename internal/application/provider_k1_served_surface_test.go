package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providercontract"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerdrift"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerexit"
)

func TestTodo_PROVIDER_001_Served(t *testing.T) {
	surface := application.NewServedProviderSurface()
	if surface.NewContractFixture == nil || surface.CompileContractEvidence == nil {
		t.Fatal("served provider contract surface is incomplete")
	}
	fixture, err := surface.NewContractFixture(providercontract.PlaceholderTopology())
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := surface.CompileContractEvidence(context.Background(), fixture)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.ProbeHealth != "HEALTHY" || !evidence.IndependentObservation || !evidence.RecoveryVerified {
		t.Fatalf("served contract evidence=%+v", evidence)
	}
}

func TestTodo_PROVIDER_002_Served(t *testing.T) {
	surface := application.NewServedProviderSurface()
	if surface.ContractFromTopology == nil || surface.CompareProviderDrift == nil || surface.ReconcileProviderDrift == nil {
		t.Fatal("served provider drift surface is incomplete")
	}
	signed, err := surface.ContractFromTopology(providercontract.PlaceholderTopology(), "sha256:served-manifest")
	if err != nil {
		t.Fatal(err)
	}
	observed := signed
	observed.SchemaVersions = cloneProviderSchemaVersions(signed.SchemaVersions)
	observed.SchemaVersions["WORKER"] = "PLACEHOLDER_SCHEMA_SERVED_V2"
	report, err := surface.CompareProviderDrift(signed, observed)
	if err != nil {
		t.Fatal(err)
	}
	if report.State != providerdrift.StateQuarantined || report.NewDispatchAllowed {
		t.Fatalf("served drift report=%+v", report)
	}
	reviewed := surface.ReconcileProviderDrift(report, providerdrift.Review{
		ReviewID:       "served-review",
		Compatible:     true,
		ProviderID:     observed.ProviderID,
		AdapterVersion: observed.AdapterVersion,
		ManifestDigest: observed.ManifestDigest,
		ConfigDigest:   "sha256:served-config",
		ApprovedAt:     time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC),
	})
	if reviewed.State != providerdrift.StateResumed || !reviewed.NewDispatchAllowed {
		t.Fatalf("served reviewed drift report=%+v", reviewed)
	}
}

func TestTodo_PROVIDER_003_Served(t *testing.T) {
	surface := application.NewServedProviderSurface()
	if surface.ReconcileProviderExit == nil {
		t.Fatal("served provider exit surface is incomplete")
	}
	plan, err := surface.ReconcileProviderExit(providerexit.Request{
		Tenant:      "served-tenant",
		Provider:    "served-provider",
		RequestedBy: "served-operator",
		At:          time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC),
		Reachable:   true,
		Resources: []providerexit.Resource{{
			ID:          "resource-1",
			Kind:        providerexit.Object,
			ExternalRef: "object/1",
			Enumerated:  true,
			Reachable:   true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != providerexit.StatusCertifiable || plan.ResourceCount != 1 {
		t.Fatalf("served exit plan=%+v", plan)
	}
}

func cloneProviderSchemaVersions(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
