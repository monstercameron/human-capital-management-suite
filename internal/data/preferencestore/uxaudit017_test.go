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

// TestTodo_UXAUDIT_017_Integration is the INTEGRATION matrix entry: it
// reaches a real store (embedded PostgreSQL via pgtest), not a fake.
//
// UXAUDIT-017's GREEN requires My Work to "retain filters on return", and
// the plan says to reuse preferences.TablePreferences -- the mechanism
// UXAUDIT-008 built for the People and Workflow History tables -- rather
// than inventing a new retention path. The store already persists
// User.Tables as one generic map (see store.go's json.Marshal of the whole
// User value), so a new "work" key needs no schema or store change; this
// test is the proof that claim is actually true against a real database,
// not just plausible from reading the code.
func TestTodo_UXAUDIT_017_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-test','Test','ACTIVE',$3)`, tenantID, "uxaudit017-test", time.Now().UTC())
	store := New(db.Conn, func(values.TenantId) uuid.UUID { return tenantID })
	ctx := context.Background()
	const organization = "org:test:uxaudit017"

	snapshot, err := store.Load(ctx, values.TenantId("uxaudit017-test"), organization, "priya")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snapshot.User.Tables["work"]; ok {
		t.Fatalf("a fresh principal must not already carry a My Work filter preference: %+v", snapshot.User.Tables)
	}

	user := snapshot.User
	if user.Tables == nil {
		user.Tables = map[string]preferences.TablePreferences{}
	}
	user.Tables["work"] = preferences.TablePreferences{Filters: map[string]string{"filter": "blocked"}}
	saved, err := store.SaveUser(ctx, values.TenantId("uxaudit017-test"), "priya", user)
	if err != nil {
		t.Fatalf("save the My Work filter preference: %v", err)
	}
	if saved.Version != 1 {
		t.Fatalf("save version = %d, want 1", saved.Version)
	}

	// The round trip the GREEN clause actually describes: leave and come
	// back with a fresh Load, as a real return to the page would.
	loaded, err := store.Load(ctx, values.TenantId("uxaudit017-test"), organization, "priya")
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.User.Tables["work"].Filters["filter"]; got != "blocked" {
		t.Fatalf("My Work filter did not survive a real store round trip: tables = %+v", loaded.User.Tables)
	}

	// The preference is scoped to the principal who set it, exactly like
	// every other table preference this store already carries.
	other, err := store.Load(ctx, values.TenantId("uxaudit017-test"), organization, "sam")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := other.User.Tables["work"]; ok {
		t.Fatalf("priya's My Work filter leaked to another principal: %+v", other.User.Tables)
	}
}
