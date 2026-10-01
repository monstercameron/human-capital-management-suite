package agenteval

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestStaticSyntheticTenantStorageScopeResolverUsesExactUniqueBindings(t *testing.T) {
	a, b := values.TenantId("suite-a"), values.TenantId("suite-b")
	ida, idb := uuid.New(), uuid.New()
	// Mutating caller input must not change the trusted map after construction.
	bindings := map[values.TenantId]uuid.UUID{a: ida, b: idb}
	resolve, err := NewStaticSyntheticTenantStorageScopeResolver(bindings)
	if err != nil {
		t.Fatal(err)
	}
	bindings[a] = uuid.New()
	got, err := resolve(context.Background(), a)
	if err != nil || got != ida {
		t.Fatalf("resolve(%q) = %s, %v; want %s", a, got, err, ida)
	}
	if _, err := resolve(context.Background(), "suite-a-extra"); !errors.Is(err, ErrSyntheticScopeMap) {
		t.Fatalf("unlisted tenant error = %v, want exact-map refusal", err)
	}
	if _, err := resolve(nil, a); !errors.Is(err, ErrSyntheticScopeMap) {
		t.Fatalf("nil context error = %v, want refusal", err)
	}
}

func TestStaticSyntheticTenantStorageScopeResolverRejectsSharedUUID(t *testing.T) {
	id := uuid.New()
	if _, err := NewStaticSyntheticTenantStorageScopeResolver(map[values.TenantId]uuid.UUID{
		"suite-a": id, "suite-b": id,
	}); !errors.Is(err, ErrSyntheticScopeMap) {
		t.Fatalf("duplicate storage UUID error = %v, want config refusal", err)
	}
}
