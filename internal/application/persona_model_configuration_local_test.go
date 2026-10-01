package application

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestTodo_AGENT_021_ModelDeploymentLocalPersistentKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "signing.json")
	first, err := LoadOrCreateLocalPersonaModelSigningMaterial(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateLocalPersonaModelSigningMaterial(path)
	if err != nil || first != second {
		t.Fatalf("local authority rotated on restart: %v", err)
	}
	if first.PolicySeed == "" || first.PolicySeed == first.OutputSeed || first.PolicySeed == first.WorkloadSeed || first.PolicySeed == first.PricingSeed || first.OutputSeed == first.WorkloadSeed || first.OutputSeed == first.PricingSeed || first.WorkloadSeed == first.PricingSeed {
		t.Fatal("local authority reuses key material")
	}
	cfg := modelDeploymentFixture(t)
	if err := SignLocalPersonaModelPricing(&cfg, first); err != nil {
		t.Fatal(err)
	}
	if _, _, err := cfg.validate(); err != nil {
		t.Fatalf("local schedule signature failed: %v", err)
	}
	aliased := first
	aliased.PricingSeed = first.OutputSeed + "\n"
	if err := SignLocalPersonaModelPricing(&cfg, aliased); !errors.Is(err, ErrPersonaModelConfiguration) {
		t.Fatalf("same decoded signing key accepted under alternate base64 formatting: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"output_seed":"corrupt"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateLocalPersonaModelSigningMaterial(path); !errors.Is(err, ErrPersonaModelConfiguration) {
		t.Fatalf("corrupt keys silently rotated: %v", err)
	}
}

func TestTodo_AGENT_021_ModelDeploymentLocalPolicyKeyMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-signing.json")
	prior, err := LoadOrCreateLocalPersonaModelSigningMaterial(path)
	if err != nil {
		t.Fatal(err)
	}
	prior.PolicySeed = ""
	raw, err := json.Marshal(prior)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	migrated, err := LoadOrCreateLocalPersonaModelSigningMaterial(path)
	if err != nil || migrated.PolicySeed == "" || migrated.OutputSeed != prior.OutputSeed || migrated.WorkloadSeed != prior.WorkloadSeed || migrated.PricingSeed != prior.PricingSeed {
		t.Fatalf("legacy key migration changed existing authority: %v", err)
	}
	second, err := LoadOrCreateLocalPersonaModelSigningMaterial(path)
	if err != nil || second != migrated {
		t.Fatalf("policy sidecar rotated on restart: %v", err)
	}
	if err := os.WriteFile(path+".policy-key", []byte(prior.OutputSeed), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateLocalPersonaModelSigningMaterial(path); !errors.Is(err, ErrPersonaModelConfiguration) {
		t.Fatalf("policy key reuses output authority: %v", err)
	}
}
