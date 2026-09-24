package pgstore_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/oidc"
	"github.com/monstercameron/human-capital-management-suite/internal/authn/oidc/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_REV_033_01_StateStore_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := values.TenantId("tenant-a")
	storageTenant := oidcTestTenant()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1::uuid,$2,'cell-local','OIDC tenant','ACTIVE',now())`, storageTenant.String(), tenant.String())
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	otherConn := db.NewConn(t)
	if _, err := otherConn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	mapper := func(values.TenantId) values.TenantId { return storageTenant }
	first, replica := pgstore.NewWithTenantMapper(conn, mapper), pgstore.NewWithTenantMapper(otherConn, mapper)
	if _, _, err := replica.Take(ctx, "unknown"); err != nil {
		t.Fatalf("unknown Take: %v", err)
	}
	pending := oidc.PendingAuthorization{Tenant: tenant, IssuerURL: "https://idp.example/tenant", ClientID: "client", RedirectURI: "https://app.example/callback", State: "state-very-random", Nonce: "nonce-value", CodeChallengeDigest: "challenge-digest", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(10 * time.Minute)}
	if err := first.Put(ctx, pending); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := first.Put(ctx, pending); err == nil {
		t.Fatal("duplicate state Put succeeded")
	}
	got, found, err := replica.Take(ctx, pending.State)
	if err != nil || !found {
		t.Fatalf("replica Take = %+v, %t, %v", got, found, err)
	}
	if got.Tenant != pending.Tenant || got.IssuerURL != pending.IssuerURL || got.ClientID != pending.ClientID || got.RedirectURI != pending.RedirectURI || got.Nonce != pending.Nonce || got.CodeChallengeDigest != pending.CodeChallengeDigest || got.State != pending.State {
		t.Fatalf("replica Take returned %+v, want %+v", got, pending)
	}
	if _, found, err := first.Take(ctx, pending.State); err != nil || found {
		t.Fatalf("replayed state Take found=%t err=%v", found, err)
	}
}

func oidcTestTenant() values.TenantId {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	s := hex.EncodeToString(b[:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" + hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" + hex.EncodeToString(b[10:])
	return values.TenantId(s)
}
