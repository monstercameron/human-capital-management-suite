package outage

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/secrets"
)

func trust032Policy() SealedPolicy {
	return SealedPolicy{MaxReferenceCacheAge: time.Hour}
}

func trust032Ref(id, version string) secrets.SecretReference {
	return secrets.SecretReference{
		ID: id, Kind: secrets.SymmetricKey, Version: version,
		Provider: "cell-kms", ProviderPath: "keys/" + id,
		Tenant: "recovery-tenant", Region: "cell-region", State: secrets.Active,
	}
}

func TestTodo_TRUST_032(t *testing.T) {
	policy := trust032Policy()
	// A reachable key service serves live.
	if mode, _ := ClassifyKeyOutage(policy, KeyServiceHealth{Reachable: true}); mode != KeyModeLive {
		t.Fatalf("live service mode=%v", mode)
	}
	// An unreachable service with a fresh cache serves bounded cached
	// references; the cache never silently outlives policy.
	if mode, _ := ClassifyKeyOutage(policy, KeyServiceHealth{Reachable: false, ReferenceCacheAge: 30 * time.Minute}); mode != KeyModeCachedBounded {
		t.Fatalf("fresh-cache mode=%v", mode)
	}
	if mode, _ := ClassifyKeyOutage(policy, KeyServiceHealth{Reachable: false, ReferenceCacheAge: 2 * time.Hour}); mode != KeyModeSealedOnly {
		t.Fatalf("expired-cache mode=%v", mode)
	}
	// Sealed recovery restores references and trust into an isolated
	// cell without copying secret material.
	bundle, err := SealReferences([]secrets.SecretReference{trust032Ref("backup-kek", "v3")})
	if err != nil {
		t.Fatalf("SealReferences: %v", err)
	}
	receipt, err := RestoreIsolated(bundle, IsolatedCell{CellID: "recovery-cell-a", AllowedIDs: []string{"backup-kek@v3"}})
	if err != nil {
		t.Fatalf("RestoreIsolated: %v", err)
	}
	if len(receipt.Restored) != 1 || receipt.Restored[0] != "backup-kek@v3" {
		t.Fatalf("receipt: %+v", receipt)
	}
	// A reference the cell does not trust stays out.
	if _, err := RestoreIsolated(bundle, IsolatedCell{CellID: "recovery-cell-a", AllowedIDs: []string{"other-kek@v1"}}); err == nil {
		t.Fatal("untrusted key reference admitted")
	}
}
