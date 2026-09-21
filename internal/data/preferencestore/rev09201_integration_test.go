package preferencestore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_REV_092_01_Integration proves against real PostgreSQL that a
// density choice is one principal's own: it persists for that principal,
// another principal in the same tenant and organization still inherits the
// organization density, the organization theme is not written, and an
// unadmitted value is stored as "inherit".
func TestTodo_REV_092_01_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-test','Test','ACTIVE',$3)`, tenantID, "density-test", time.Now().UTC())
	store := New(db.Conn, func(values.TenantId) uuid.UUID { return tenantID })
	ctx := context.Background()
	tenant := values.TenantId("density-test")
	const organization = "org:test:north"

	theme := preferences.DefaultSnapshot().Theme
	theme.Theme.Density = preferences.DensitySpacious
	theme, err := store.SaveTheme(ctx, tenant, organization, "admin", theme)
	if err != nil {
		t.Fatalf("save organization theme: %v", err)
	}

	alice, err := store.Load(ctx, tenant, organization, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if alice.User.Density != "" {
		t.Fatalf("a principal who never chose a density has %q, want inherit", alice.User.Density)
	}
	user := alice.User
	user.Density = preferences.DensityCompact
	if _, err := store.SaveUser(ctx, tenant, "alice", user); err != nil {
		t.Fatalf("save alice density: %v", err)
	}

	alice, err = store.Load(ctx, tenant, organization, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if alice.User.Density != preferences.DensityCompact {
		t.Fatalf("alice's density did not persist: %q", alice.User.Density)
	}
	if alice.Theme.Density != preferences.DensitySpacious || alice.Theme.Version != theme.Version {
		t.Fatalf("a personal choice changed the organization theme: density=%q version=%d", alice.Theme.Density, alice.Theme.Version)
	}

	bob, err := store.Load(ctx, tenant, organization, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if bob.User.Density != "" || bob.Theme.Density != preferences.DensitySpacious {
		t.Fatalf("bob sees personal=%q organization=%q, want inherit and spacious", bob.User.Density, bob.Theme.Density)
	}

	// An unadmitted value is never stored as a density.
	user = alice.User
	user.Density = "ultra-compact"
	saved, err := store.SaveUser(ctx, tenant, "alice", user)
	if err != nil {
		t.Fatalf("save unadmitted density: %v", err)
	}
	if saved.Density != "" {
		t.Fatalf("unadmitted density stored as %q", saved.Density)
	}
	reloaded, err := store.Load(ctx, tenant, organization, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.User.Density != "" {
		t.Fatalf("unadmitted density reloaded as %q", reloaded.User.Density)
	}
}
