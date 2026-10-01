package application_test

import (
	"crypto/ed25519"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/residency"
)

func servedResidencyPolicy(t *testing.T) residency.Policy {
	t.Helper()
	privateKey := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	location := residency.Location{ID: "store-served", Jurisdiction: "US-CA"}
	processor := residency.Processor{ID: "processor-served", Kind: "primary"}
	policy, err := application.NewServedResidencySurface().NewPolicy(
		"served-residency-policy", "revision-1", residency.JurisdictionSet{IDs: []string{"US-CA"}, Revision: "jurisdiction-1"},
		[]residency.Rule{{
			DataClass: "WORKFORCE_RECORD", Purpose: "serving",
			Processing: []residency.Location{location}, Storage: []residency.Location{location},
			Support: []residency.Location{location}, TransferTargets: []residency.Location{location},
			Processors: []residency.Processor{processor},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	policy, err = application.NewServedResidencySurface().Sign(policy, "served-key", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func servedResidencyInventory(observed bool) residency.Inventory {
	location := residency.Location{ID: "store-served", Jurisdiction: "US-CA"}
	processor := residency.Processor{ID: "processor-served", Kind: "primary"}
	return residency.Inventory{
		Copies: []residency.Copy{{ID: "copy-1", TenantID: "tenant-1", Class: "WORKFORCE_RECORD", Purpose: "serving", Kind: residency.CopyPrimary, Location: location, Processor: processor, Observed: observed}},
	}
}

func TestTodo_RESIDENCY_001_Served(t *testing.T) {
	var app application.App
	surface := app.Residency()
	if surface.Version == nil || surface.NewPolicy == nil || surface.Sign == nil || surface.Verify == nil || surface.Evaluate == nil || surface.Explain == nil {
		t.Fatal("served residency surface is incomplete")
	}
	if surface.Version() != 1 {
		t.Fatalf("served residency version=%d, want 1", surface.Version())
	}

	policy := servedResidencyPolicy(t)
	if err := surface.Verify(policy); err != nil {
		t.Fatalf("served signed policy verification: %v", err)
	}
	decision, err := surface.Evaluate(policy, servedResidencyInventory(true))
	if err != nil || !decision.Admitted() || decision.CheckedCopies != 1 {
		t.Fatalf("served residency decision=%+v err=%v", decision, err)
	}
	if got := surface.Explain(decision); got == "" {
		t.Fatal("served residency explanation is empty")
	}

	unknown, err := surface.Evaluate(policy, servedResidencyInventory(false))
	if err != nil || unknown.State != residency.Unknown || unknown.Admitted() {
		t.Fatalf("served unknown residency decision=%+v err=%v", unknown, err)
	}

	var nilApp *application.App
	if exposed := nilApp.Residency(); exposed.Version != nil || exposed.Evaluate != nil {
		t.Fatal("nil application exposed residency capabilities")
	}
}
