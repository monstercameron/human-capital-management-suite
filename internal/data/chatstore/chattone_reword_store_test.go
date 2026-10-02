package chatstore

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

// TestTodo_CHATTONE_002_Integration: the administrator's reword choice and the
// channel override are kept by the real chat store, revise in place with a
// revision that only moves forward, and removing an override returns the
// channel to the workspace.
func TestTodo_CHATTONE_002_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	if got, err := s.LoadRewordSettings(ctx, "tenant-a"); err != nil || len(got) != 0 {
		t.Fatalf("a workspace with no setting: %+v %v", got, err)
	}
	if rev, err := s.SaveRewordSetting(ctx, RewordSetting{Tenant: "tenant-a", Mode: RewordModeOffered, MembersMayViewOriginal: true, UpdatedBy: "admin-1"}); err != nil || rev != 1 {
		t.Fatalf("workspace save: %d %v", rev, err)
	}
	if _, err := s.SaveRewordSetting(ctx, RewordSetting{Tenant: "tenant-a", Channel: "incident", Mode: RewordModeOn, MembersMayViewOriginal: false, UpdatedBy: "admin-1"}); err != nil {
		t.Fatal(err)
	}
	if rev, err := s.SaveRewordSetting(ctx, RewordSetting{Tenant: "tenant-a", Mode: RewordModeOn, MembersMayViewOriginal: true, UpdatedBy: "admin-2"}); err != nil || rev != 2 {
		t.Fatalf("workspace revision: %d %v", rev, err)
	}
	got, err := s.LoadRewordSettings(ctx, "tenant-a")
	if err != nil || len(got) != 2 {
		t.Fatalf("settings: %+v %v", got, err)
	}
	if got[0].Channel != "" || got[0].Mode != RewordModeOn || !got[0].MembersMayViewOriginal || got[0].Revision != 2 || got[0].UpdatedBy != "admin-2" {
		t.Fatalf("workspace row: %+v", got[0])
	}
	if got[1].Channel != "incident" || got[1].Mode != RewordModeOn || got[1].MembersMayViewOriginal {
		t.Fatalf("channel override: %+v", got[1])
	}
	if err := s.DeleteRewordOverride(ctx, "tenant-a", "incident"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.LoadRewordSettings(ctx, "tenant-a"); len(got) != 1 || got[0].Channel != "" {
		t.Fatalf("after removing the override: %+v", got)
	}
	// The workspace's own row cannot be removed through the override call.
	if err := s.DeleteRewordOverride(ctx, "tenant-a", ""); err == nil {
		t.Fatal("the workspace row was removable as an override")
	}
	for name, in := range map[string]RewordSetting{
		"no tenant":   {Mode: RewordModeOn, UpdatedBy: "a"},
		"no writer":   {Tenant: "t", Mode: RewordModeOn, UpdatedBy: " "},
		"unknown":     {Tenant: "t", Mode: "sometimes", UpdatedBy: "a"},
		"padded id":   {Tenant: "t", Channel: " x", Mode: RewordModeOn, UpdatedBy: "a"},
		"empty mode":  {Tenant: "t", UpdatedBy: "a"},
		"long id":     {Tenant: "t", Channel: strings.Repeat("c", 201), Mode: RewordModeOn, UpdatedBy: "a"},
		"upper-cased": {Tenant: "t", Mode: "ON", UpdatedBy: "a"},
	} {
		if _, err := s.SaveRewordSetting(ctx, in); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

// TestTodo_CHATTONE_003_Security: settings and a person's choices never cross
// workspaces or people, the database refuses a write for another workspace, and
// in a cell whose serving role holds only the default SELECT, INSERT and UPDATE
// the migration lets it remove an override.
func TestTodo_CHATTONE_003_Security(t *testing.T) {
	s, schema := chatFixture(t)
	ctx := context.Background()
	if _, err := s.SaveRewordSetting(ctx, RewordSetting{Tenant: "tenant-a", Mode: RewordModeOn, UpdatedBy: "admin"}); err != nil {
		t.Fatal(err)
	}
	if other, err := s.LoadRewordSettings(ctx, "tenant-b"); err != nil || len(other) != 0 {
		t.Fatalf("a setting crossed workspaces: %+v %v", other, err)
	}
	if err := s.SaveReaderChoice(ctx, "tenant-a", "dana", "", "as-written"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveReaderChoice(ctx, "tenant-a", "dana", "incident", "reworded"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveReaderChoice(ctx, "tenant-a", "dana", "incident", "as-written"); err != nil {
		t.Fatal(err)
	}
	choices, err := s.LoadReaderChoices(ctx, "tenant-a", "dana")
	if err != nil || len(choices) != 2 || choices[0] != (ReaderChoice{"", "as-written"}) || choices[1] != (ReaderChoice{"incident", "as-written"}) {
		t.Fatalf("dana's choices: %+v %v", choices, err)
	}
	if other, err := s.LoadReaderChoices(ctx, "tenant-a", "morgan"); err != nil || len(other) != 0 {
		t.Fatalf("a choice crossed people: %+v %v", other, err)
	}
	if other, err := s.LoadReaderChoices(ctx, "tenant-b", "dana"); err != nil || len(other) != 0 {
		t.Fatalf("a choice crossed workspaces: %+v %v", other, err)
	}
	if err := s.DeleteReaderChoice(ctx, "tenant-a", "dana", "incident"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.LoadReaderChoices(ctx, "tenant-a", "dana"); len(got) != 1 || got[0].Channel != "" {
		t.Fatalf("after returning a channel to the general choice: %+v", got)
	}
	for name, err := range map[string]error{
		"tone":            s.SaveReaderChoice(ctx, "tenant-a", "dana", "", "angry"),
		"person":          s.SaveReaderChoice(ctx, "tenant-a", " ", "", "reworded"),
		"delete general":  s.DeleteReaderChoice(ctx, "tenant-a", "dana", ""),
		"delete override": s.DeleteReaderChoice(ctx, "", "dana", "x"),
	} {
		if err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	role := createChatRLSRole(t, s, schema)
	err = runChatAsTenantRole(ctx, s, role, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chattone_reword_setting (tenant_id, channel_id, mode, members_may_view_original, updated_by) VALUES ('tenant-c','', 'on', true, 'x')`)
		return err
	})
	if err == nil {
		t.Fatal("row level security allowed a reword setting for another workspace")
	}
	err = runChatAsTenantRole(ctx, s, role, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chattone_reader_choice (tenant_id, person_id, tone) VALUES ('tenant-c','dana','reworded')`)
		return err
	})
	if err == nil {
		t.Fatal("row level security allowed a reader choice for another workspace")
	}
}

// TestTodo_CHATTONE_003_DeleteGrant reproduces a cell with separated roles:
// the serving role holds only the default table privileges. Migration 43 must
// let it remove a channel override and a person's override.
func TestTodo_CHATTONE_003_DeleteGrant(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	role := "chattone003_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	db.Exec(t, `CREATE ROLE `+role+` NOLOGIN`)
	t.Cleanup(func() {
		_ = db.ExecErr(`DROP OWNED BY ` + role)
		_ = db.ExecErr(`DROP ROLE ` + role)
	})
	db.Exec(t, `GRANT USAGE ON SCHEMA `+db.Schema+` TO `+role)
	db.Exec(t, `ALTER DEFAULT PRIVILEGES IN SCHEMA `+db.Schema+` GRANT SELECT, INSERT, UPDATE ON TABLES TO `+role)
	migrations, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `INSERT INTO chattone_reword_setting (tenant_id, channel_id, mode, members_may_view_original, updated_by) VALUES ('t','incident','on',false,'admin')`)
	db.Exec(t, `INSERT INTO chattone_reader_choice (tenant_id, person_id, channel_id, tone) VALUES ('t','dana','incident','as-written')`)
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, `SET ROLE `+role); err != nil {
		t.Fatal(err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT set_config('hcmnext.tenant_id','t',true)`); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`DELETE FROM chattone_reword_setting WHERE tenant_id='t' AND channel_id='incident'`,
		`DELETE FROM chattone_reader_choice WHERE tenant_id='t' AND person_id='dana' AND channel_id='incident'`,
	} {
		if changed, err := tx.Exec(ctx, statement); err != nil || changed != 1 {
			t.Fatalf("the serving role could not run %q: %d %v", statement, changed, err)
		}
	}
}
