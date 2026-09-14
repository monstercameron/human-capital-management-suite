package preferencestore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestStorePersistsPrincipalSettingsAndOrganizationAppearanceWithCAS(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-test','Test','ACTIVE',$3)`, tenantID, "prefs-test", time.Now().UTC())
	store := New(db.Conn, func(values.TenantId) uuid.UUID { return tenantID })
	ctx := context.Background()

	const organizationNorth = "org:test:north"
	const organizationSouth = "org:test:south"
	snapshot, err := store.Load(ctx, values.TenantId("prefs-test"), organizationNorth, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.User.Version != 0 || snapshot.User.Tables[preferences.TablePeople].PageSize != 20 {
		t.Fatalf("unexpected defaults: %+v", snapshot.User)
	}

	user := snapshot.User
	user.Locale = "de-DE"
	user.Tables[preferences.TablePeople] = preferences.TablePreferences{PageSize: 50, Filters: map[string]string{"team": "Care Operations"}, Sort: "name", Direction: "asc"}
	user, err = store.SaveUser(ctx, values.TenantId("prefs-test"), "alice", user)
	if err != nil || user.Version != 1 {
		t.Fatalf("save user: version=%d err=%v", user.Version, err)
	}
	stale := user
	stale.Version = 0
	if _, err = store.SaveUser(ctx, values.TenantId("prefs-test"), "alice", stale); !errors.Is(err, preferences.ErrVersionConflict) {
		t.Fatalf("stale save err=%v", err)
	}

	theme := preferences.DefaultSnapshot().Theme
	theme.Theme.Palette = "ocean"
	theme, err = store.SaveTheme(ctx, values.TenantId("prefs-test"), organizationNorth, "alice", theme)
	if err != nil || theme.Version != 1 {
		t.Fatalf("save theme: version=%d err=%v", theme.Version, err)
	}
	staleTheme := theme
	staleTheme.Version = 0
	if _, err = store.SaveTheme(ctx, values.TenantId("prefs-test"), organizationNorth, "alice", staleTheme); !errors.Is(err, preferences.ErrVersionConflict) {
		t.Fatalf("stale organization appearance save err=%v", err)
	}

	loaded, err := store.Load(ctx, values.TenantId("prefs-test"), organizationNorth, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.User.Locale != "de-DE" || loaded.User.Tables[preferences.TablePeople].PageSize != 50 || loaded.Theme.Palette != "ocean" {
		t.Fatalf("round trip lost preferences: %+v", loaded)
	}

	// A second principal in the same organization receives the shared theme
	// but not Alice's personal locale, navigation, or table configuration.
	bob, err := store.Load(ctx, values.TenantId("prefs-test"), organizationNorth, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if bob.Theme.Palette != "ocean" || bob.Theme.OrganizationScopeID != organizationNorth {
		t.Fatalf("organization appearance was not shared: %+v", bob.Theme)
	}
	if bob.User.Locale != "" || bob.User.Version != 0 || bob.User.Tables[preferences.TablePeople].PageSize != 20 {
		t.Fatalf("Alice's user settings leaked to Bob: %+v", bob.User)
	}

	// A principal in a different organization under the same tenant receives
	// neither organization's appearance; the default remains honest.
	carol, err := store.Load(ctx, values.TenantId("prefs-test"), organizationSouth, "carol")
	if err != nil {
		t.Fatal(err)
	}
	if carol.Theme.Palette != preferences.DefaultSnapshot().Theme.Palette || carol.Theme.Version != 0 || carol.Theme.OrganizationScopeID != organizationSouth {
		t.Fatalf("organization appearance crossed scopes: %+v", carol.Theme)
	}
	carolTheme := carol.Theme
	carolTheme.Theme.Palette = "violet"
	carolTheme, err = store.SaveTheme(ctx, values.TenantId("prefs-test"), organizationSouth, "carol", carolTheme)
	if err != nil || carolTheme.Version != 1 {
		t.Fatalf("independent organization appearance stream: version=%d err=%v", carolTheme.Version, err)
	}
	northAgain, err := store.Load(ctx, values.TenantId("prefs-test"), organizationNorth, "alice")
	if err != nil || northAgain.Theme.Palette != "ocean" || northAgain.Theme.Version != 1 {
		t.Fatalf("south organization changed north appearance: theme=%+v err=%v", northAgain.Theme, err)
	}
	authored := northAgain.Theme
	authored.Palette = "custom"
	authored.TokenOverrides = map[string]string{"color.brand.primary": "#4d1f78", "color.canvas": "#fbf9fd"}
	authored.DarkTokenOverrides = map[string]string{"color.brand.primary": "#ba9ce7", "color.canvas": "#101019"}
	if _, err := store.SaveTheme(ctx, values.TenantId("prefs-test"), organizationNorth, "alice", authored); err != nil {
		t.Fatalf("save authored colors: %v", err)
	}
	bobAuthored, err := store.Load(ctx, values.TenantId("prefs-test"), organizationNorth, "bob")
	if err != nil || bobAuthored.Theme.TokenOverrides["color.brand.primary"] != "#4d1f78" || bobAuthored.Theme.DarkTokenOverrides["color.canvas"] != "#101019" {
		t.Fatalf("organization light/dark palette was not shared intact: theme=%+v err=%v", bobAuthored.Theme, err)
	}

	visibility := northAgain.OrganizationVisibility
	visibility.Mode = preferences.OrganizationVisibilityAllowlist
	visibility.OrganizationUnits = []string{"People", "Finance"}
	visibility, err = store.SaveOrganizationVisibility(ctx, values.TenantId("prefs-test"), organizationNorth, "alice", visibility)
	if err != nil || visibility.Version != 1 {
		t.Fatalf("save organization visibility: version=%d err=%v", visibility.Version, err)
	}
	staleVisibility := visibility
	staleVisibility.Version = 0
	if _, err = store.SaveOrganizationVisibility(ctx, values.TenantId("prefs-test"), organizationNorth, "alice", staleVisibility); !errors.Is(err, preferences.ErrVersionConflict) {
		t.Fatalf("stale organization visibility err=%v", err)
	}
	bobAgain, err := store.Load(ctx, values.TenantId("prefs-test"), organizationNorth, "bob")
	if err != nil || bobAgain.OrganizationVisibility.Mode != preferences.OrganizationVisibilityAllowlist || len(bobAgain.OrganizationVisibility.OrganizationUnits) != 2 {
		t.Fatalf("organization visibility was not shared in scope: %+v err=%v", bobAgain.OrganizationVisibility, err)
	}
	southAgain, err := store.Load(ctx, values.TenantId("prefs-test"), organizationSouth, "carol")
	if err != nil || southAgain.OrganizationVisibility.Mode != preferences.OrganizationVisibilityAll || southAgain.OrganizationVisibility.Version != 0 {
		t.Fatalf("organization visibility crossed scopes: %+v err=%v", southAgain.OrganizationVisibility, err)
	}

	bob.User.Locale = "ar"
	if _, err = store.SaveUser(ctx, values.TenantId("prefs-test"), "bob", bob.User); err != nil {
		t.Fatal(err)
	}
	aliceAgain, err := store.Load(ctx, values.TenantId("prefs-test"), organizationNorth, "alice")
	if err != nil || aliceAgain.User.Locale != "de-DE" {
		t.Fatalf("Bob's settings changed Alice: locale=%q err=%v", aliceAgain.User.Locale, err)
	}

	used, err := store.RecordWorkflowUse(ctx, values.TenantId("prefs-test"), "alice", "promotion")
	if err != nil || used.WorkflowUses["promotion"] != 1 {
		t.Fatalf("record workflow use: %+v err=%v", used.WorkflowUses, err)
	}
}

func TestStoreRejectsMissingAuthenticatedCoordinates(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-test','Test','ACTIVE',$3)`, tenantID, "prefs-invalid-test", time.Now().UTC())
	store := New(db.Conn, func(values.TenantId) uuid.UUID { return tenantID })
	tenant := values.TenantId("prefs-invalid-test")

	if _, err := store.Load(context.Background(), tenant, "", "alice"); !errors.Is(err, preferences.ErrInvalid) {
		t.Fatalf("load without organization scope err=%v", err)
	}
	if _, err := store.Load(context.Background(), tenant, "org:test:north", ""); !errors.Is(err, preferences.ErrInvalid) {
		t.Fatalf("load without principal err=%v", err)
	}
	if _, err := store.SaveTheme(context.Background(), tenant, "", "alice", preferences.DefaultSnapshot().Theme); !errors.Is(err, preferences.ErrInvalid) {
		t.Fatalf("save appearance without organization scope err=%v", err)
	}
	if _, err := store.SaveOrganizationVisibility(context.Background(), tenant, "", "alice", preferences.OrganizationVisibility{}); !errors.Is(err, preferences.ErrInvalid) {
		t.Fatalf("save visibility without organization scope err=%v", err)
	}
}
