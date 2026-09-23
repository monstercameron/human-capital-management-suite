package truststore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/partnerapp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func machineFixture(t *testing.T) (*Store, *pgtest.DB, uuid.UUID) {
	t.Helper()
	db := pgtest.New(t)
	tenant := insertTrustTenant(t, db, "machine-primary")
	return New(trustAppConn(t, db)), db, tenant
}

// TestMachineTokenUseSingleUse proves the INTAPI-002 durable source: the
// first presentation records the identifier, its replay is refused, a
// revoked or expired client fails closed, and identifiers never cross
// tenants.
func TestMachineTokenUseSingleUse(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	store, db, tenant := machineFixture(t)
	byKey := map[string]uuid.UUID{"machine-primary": tenant, "machine-second": {}}
	other := insertTrustTenant(t, db, "machine-second")
	byKey["machine-second"] = other
	reg := store.Registry(func(key string) uuid.UUID { return byKey[key] })

	exp := now.Add(time.Hour).Truncate(time.Second)
	mustRegister := func(key, id string, expiry time.Time) {
		t.Helper()
		if err := reg.RegisterClient(ctx, key, partnerapp.MachineClient{
			ClientID: id, Owner: "owner-u", Status: partnerapp.MachineClientActive,
			Purpose: "sync", IPAllowlist: []string{"10.0.0.0/8"}, ExpiresAt: expiry,
		}); err != nil {
			t.Fatalf("register %s/%s: %v", key, id, err)
		}
	}
	mustRegister("machine-primary", "client-u", exp)

	check := func(key, client, jti string) error {
		return reg.CheckRevocation(ctx, trust.RevocationQuery{
			Tenant: key, ClientID: client, TokenID: jti, Session: "mcs-1", ExpiresAt: exp,
		})
	}

	t.Run("first presentation records, replay refuses", func(t *testing.T) {
		if err := check("machine-primary", "client-u", "jti-1"); err != nil {
			t.Fatalf("first use: %v", err)
		}
		if err := check("machine-primary", "client-u", "jti-1"); !errors.Is(err, trust.ErrTokenReplayed) {
			t.Fatalf("replay = %v, want replayed", err)
		}
		if err := check("machine-primary", "client-u", "jti-2"); err != nil {
			t.Fatalf("second identifier: %v", err)
		}
	})

	t.Run("revoked and expired clients fail closed", func(t *testing.T) {
		mustRegister("machine-primary", "client-v", exp)
		if _, err := store.SetMachineClientState(ctx, tenant, "client-v", MachineClientRevoked, true, now); err != nil {
			t.Fatal(err)
		}
		if err := check("machine-primary", "client-v", "jti-3"); !errors.Is(err, trust.ErrClientRevoked) {
			t.Fatalf("revoked client = %v, want revoked", err)
		}
		past := now.Add(-time.Minute).Truncate(time.Second)
		if err := reg.RegisterClient(ctx, "machine-primary", partnerapp.MachineClient{
			ClientID: "client-w", Owner: "owner-u", Status: partnerapp.MachineClientActive,
			Purpose: "sync", ExpiresAt: past,
		}); CodeOf(err) != CodeInvalid {
			t.Fatalf("already-expired registration = %v, want INVALID at write time", err)
		}
		if err := check("machine-primary", "ghost", "jti-5"); !errors.Is(err, trust.ErrClientRevoked) {
			t.Fatalf("unknown client = %v, want revoked", err)
		}
	})

	t.Run("identifiers never cross tenants", func(t *testing.T) {
		mustRegister("machine-second", "client-u", exp)
		if err := check("machine-second", "client-u", "jti-1"); err != nil {
			t.Fatalf("same identifier in another tenant: %v", err)
		}
	})
}

// TestMachineRegistryPort proves the partnerapp.ClientRegistry port reads
// the same durable rows through tenant keys: registration, client and key
// loads, use recording, and unknown-tenant refusal.
func TestMachineRegistryPort(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	store, _, tenant := machineFixture(t)
	byKey := map[string]uuid.UUID{"machine-primary": tenant}
	reg := store.Registry(func(key string) uuid.UUID { return byKey[key] })

	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	err := reg.RegisterClient(ctx, "machine-primary", partnerapp.MachineClient{
		ClientID: "client-p", Owner: "owner-p", Status: partnerapp.MachineClientActive,
		Scopes: []string{"intents.read"}, Purpose: "sync", IPAllowlist: []string{"10.0.0.0/8"},
		ExpiresAt: exp,
	})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	client, err := reg.LoadClient(ctx, "machine-primary", "client-p")
	if err != nil {
		t.Fatalf("LoadClient: %v", err)
	}
	if client.ClientID != "client-p" || client.Tenant != "machine-primary" || client.Owner != "owner-p" {
		t.Fatalf("client = %+v", client)
	}
	if err := reg.RegisterClientKey(ctx, "machine-primary", partnerapp.MachineClientKey{
		ClientID: "client-p", KID: "kp", Alg: "EdDSA",
		JWK:       []byte(`{"kty":"OKP","crv":"Ed25519","x":"11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"}`),
		NotBefore: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("RegisterClientKey: %v", err)
	}
	keys, err := reg.LoadClientKeys(ctx, "machine-primary", "client-p")
	if err != nil {
		t.Fatalf("LoadClientKeys: %v", err)
	}
	if len(keys) != 1 || keys[0].KID != "kp" || keys[0].Alg != "EdDSA" {
		t.Fatalf("keys = %+v", keys)
	}
	if err := reg.RecordClientUse(ctx, "machine-primary", "client-p", now); err != nil {
		t.Fatalf("RecordClientUse: %v", err)
	}
	loaded, err := reg.LoadClient(ctx, "machine-primary", "client-p")
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.LastUsedAt.Equal(now) {
		t.Fatalf("last use = %v, want %v", loaded.LastUsedAt, now)
	}
	if _, err := reg.LoadClient(ctx, "no-such-tenant", "client-p"); CodeOf(err) != CodeNotFound {
		t.Fatalf("unknown tenant = %v, want NOT_FOUND", err)
	}
}

func sampleMachineClient(tenant uuid.UUID, id string) MachineClientRecord {
	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	return MachineClientRecord{
		TenantID: tenant, RowID: uuid.New(), ClientID: id, Owner: "integration-owner",
		Status: MachineClientActive, Scopes: []string{"intents.read", "workers.read"},
		Purpose: "nightly_sync", DataDomains: []string{"workforce"},
		FieldSubset: []string{"worker.display_name"}, IPAllowlist: []string{"10.0.0.0/8"},
		ExpiresAt: &exp,
	}
}

func sampleMachineKey(tenant uuid.UUID, client, kid string) MachineClientKeyRecord {
	return MachineClientKeyRecord{
		TenantID: tenant, RowID: uuid.New(), ClientID: client, KID: kid, Alg: "EdDSA",
		PublicJWK: []byte(`{"kty":"OKP","crv":"Ed25519","x":"11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"}`),
		NotBefore: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestTodo_INTAPI_001 is the INTAPI-001 registry PRIMARY: the durable
// registry stores every coordinate the token endpoint joins a client
// against (owner, scopes, purpose, data domains, field subset, IP
// allowlist, expiry, keys, revocation, last use), rotates keys, revokes
// clients and keys, and isolates tenants.
func TestTodo_INTAPI_001(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)

	t.Run("register and load round-trips every registry field", func(t *testing.T) {
		store, _, tenant := machineFixture(t)
		in := sampleMachineClient(tenant, "client-a")
		if err := store.RegisterMachineClient(ctx, tenant, in); err != nil {
			t.Fatalf("RegisterMachineClient: %v", err)
		}
		got, err := store.LoadMachineClient(ctx, tenant, "client-a")
		if err != nil {
			t.Fatalf("LoadMachineClient: %v", err)
		}
		if got.Owner != "integration-owner" || got.Status != MachineClientActive || got.Purpose != "nightly_sync" {
			t.Fatalf("identity fields = %+v", got)
		}
		if len(got.Scopes) != 2 || len(got.DataDomains) != 1 || len(got.FieldSubset) != 1 || len(got.IPAllowlist) != 1 {
			t.Fatalf("grant fields = %+v", got)
		}
		wantExp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
		if got.ExpiresAt == nil || !got.ExpiresAt.UTC().Equal(wantExp) {
			t.Fatalf("expiry = %v, want %v", got.ExpiresAt, wantExp)
		}
		if got.Revoked || got.LastUsedAt != nil {
			t.Fatalf("fresh client carries state: %+v", got)
		}
	})

	t.Run("client identity cannot be silently replaced", func(t *testing.T) {
		store, _, tenant := machineFixture(t)
		if err := store.RegisterMachineClient(ctx, tenant, sampleMachineClient(tenant, "client-b")); err != nil {
			t.Fatal(err)
		}
		if err := store.RegisterMachineClient(ctx, tenant, sampleMachineClient(tenant, "client-b")); CodeOf(err) != CodeDuplicate {
			t.Fatalf("repeat register = %v, want DUPLICATE", err)
		}
	})

	t.Run("unknown client loads as not found", func(t *testing.T) {
		store, _, tenant := machineFixture(t)
		if _, err := store.LoadMachineClient(ctx, tenant, "nobody"); CodeOf(err) != CodeNotFound {
			t.Fatalf("load unknown = %v, want NOT_FOUND", err)
		}
	})

	t.Run("keys rotate and revoke per kid", func(t *testing.T) {
		store, _, tenant := machineFixture(t)
		if err := store.RegisterMachineClient(ctx, tenant, sampleMachineClient(tenant, "client-c")); err != nil {
			t.Fatal(err)
		}
		if err := store.RegisterMachineClientKey(ctx, tenant, sampleMachineKey(tenant, "client-c", "k1")); err != nil {
			t.Fatalf("register k1: %v", err)
		}
		rotated := sampleMachineKey(tenant, "client-c", "k2")
		if err := store.RegisterMachineClientKey(ctx, tenant, rotated); err != nil {
			t.Fatalf("register k2: %v", err)
		}
		keys, err := store.LoadMachineClientKeys(ctx, tenant, "client-c")
		if err != nil {
			t.Fatalf("load keys: %v", err)
		}
		if len(keys) != 2 || keys[0].KID != "k1" || keys[1].KID != "k2" {
			t.Fatalf("keys = %+v, want k1 then k2", keys)
		}
		if keys[0].Alg != "EdDSA" || len(keys[0].PublicJWK) == 0 || keys[0].NotBefore.IsZero() {
			t.Fatalf("key row = %+v, want alg, jwk and window", keys[0])
		}
		if err := store.RevokeMachineClientKey(ctx, tenant, "client-c", "k1"); err != nil {
			t.Fatalf("revoke k1: %v", err)
		}
		keys, err = store.LoadMachineClientKeys(ctx, tenant, "client-c")
		if err != nil {
			t.Fatal(err)
		}
		if !keys[0].Revoked || keys[1].Revoked {
			t.Fatalf("revocation = %+v, want only k1 revoked", keys)
		}
		if err := store.RegisterMachineClientKey(ctx, tenant, sampleMachineKey(tenant, "client-c", "k1")); CodeOf(err) != CodeDuplicate {
			t.Fatalf("repeat kid = %v, want DUPLICATE", err)
		}
	})

	t.Run("key for an unregistered client is refused", func(t *testing.T) {
		store, _, tenant := machineFixture(t)
		if err := store.RegisterMachineClientKey(ctx, tenant, sampleMachineKey(tenant, "ghost", "k1")); CodeOf(err) != CodeInvalid && CodeOf(err) != CodeDatabase {
			t.Fatalf("orphan key = %v, want INVALID or DATABASE (foreign key)", err)
		}
	})

	t.Run("state changes suspend, revoke and record use", func(t *testing.T) {
		store, _, tenant := machineFixture(t)
		if err := store.RegisterMachineClient(ctx, tenant, sampleMachineClient(tenant, "client-d")); err != nil {
			t.Fatal(err)
		}
		updated, err := store.SetMachineClientState(ctx, tenant, "client-d", MachineClientSuspended, false, now)
		if err != nil {
			t.Fatalf("suspend: %v", err)
		}
		if updated.Status != MachineClientSuspended || updated.Revoked {
			t.Fatalf("suspended = %+v", updated)
		}
		updated, err = store.SetMachineClientState(ctx, tenant, "client-d", MachineClientRevoked, true, now)
		if err != nil {
			t.Fatalf("revoke: %v", err)
		}
		if updated.Status != MachineClientRevoked || !updated.Revoked {
			t.Fatalf("revoked = %+v", updated)
		}
		if err := store.TouchMachineClientUse(ctx, tenant, "client-d", now); err != nil {
			t.Fatalf("touch: %v", err)
		}
		loaded, err := store.LoadMachineClient(ctx, tenant, "client-d")
		if err != nil {
			t.Fatal(err)
		}
		if loaded.LastUsedAt == nil || !loaded.LastUsedAt.Equal(now) {
			t.Fatalf("last use = %v, want %v", loaded.LastUsedAt, now)
		}
		if _, err := store.SetMachineClientState(ctx, tenant, "client-d", "bogus", false, now); CodeOf(err) != CodeInvalid {
			t.Fatalf("bogus status = %v, want INVALID", err)
		}
	})

	t.Run("one tenant cannot read or reference another tenant's clients", func(t *testing.T) {
		store, db, tenantA := machineFixture(t)
		tenantB := insertTrustTenant(t, db, "machine-other")
		if err := store.RegisterMachineClient(ctx, tenantA, sampleMachineClient(tenantA, "client-e")); err != nil {
			t.Fatal(err)
		}
		if _, err := store.LoadMachineClient(ctx, tenantB, "client-e"); CodeOf(err) != CodeNotFound {
			t.Fatalf("cross-tenant load = %v, want NOT_FOUND", err)
		}
		// A registration smuggling the other tenant's id is refused before
		// any write.
		smuggled := sampleMachineClient(tenantB, "client-e")
		if err := store.RegisterMachineClient(ctx, tenantA, smuggled); CodeOf(err) != CodeTenantRequired {
			t.Fatalf("tenant-mismatched register = %v, want TENANT_REQUIRED", err)
		}
		keys, err := store.LoadMachineClientKeys(ctx, tenantB, "client-e")
		if err != nil || len(keys) != 0 {
			t.Fatalf("cross-tenant keys = %+v, %v; want none", keys, err)
		}
	})
}
