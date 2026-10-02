package agentpersonastore

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func iconFixture(t *testing.T) (*fixture, *TenantStore) {
	t.Helper()
	f := newFixture(t, "icon-a", "icon-b")
	if err := MigrateIcons(context.Background(), f.db.SQL); err != nil {
		t.Fatal(err)
	}
	if err := MigrateIcons(context.Background(), f.db.SQL); err != nil {
		t.Fatal("idempotent migration", err)
	}
	return f, f.store(t, "icon-a")
}

func createIconDraft(t *testing.T, s *TenantStore, id string) PersonaIcon {
	t.Helper()
	v := version(s.tenant, id, 1)
	v.Profile = json.RawMessage(`{"owner":"user:business-owner","purpose":"policy guide","instructions":"Read the policy handbook","skill_pins":[{"id":"policy.search"}]}`)
	owner, steward := draftOwners(v)
	if err := s.CreateDraftWithIcon(context.Background(), v, owner, steward, "user:business-owner", v.CreatedAt); err != nil {
		t.Fatal(err)
	}
	icon, err := s.GetIcon(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return icon
}

func TestAgentUXIcon_Generated(t *testing.T) {
	f, s := iconFixture(t)
	ctx := context.Background()
	a := createIconDraft(t, s, "a")
	b := createIconDraft(t, s, "b")
	// Two agents with the same name and instructions are told apart by their
	// picture: the second takes the next glyph nobody in the workspace uses.
	if a.Value.Glyph != "book" || a.Revision != 1 || a.Value == b.Value || a.Value.Glyph == b.Value.Glyph || !b.Value.Valid() {
		t.Fatal("generation/collision failed", a, b)
	}
	if err := s.PutVersion(ctx, version(s.tenant, "a", 2)); err != nil {
		t.Fatal(err)
	}
	if current, err := s.GetIcon(ctx, "a"); err != nil || current != a {
		t.Fatal("version changed identity", current, err)
	}
	if _, err := f.store(t, "icon-b").GetIcon(ctx, "a"); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross tenant icon visible", err)
	}
	var count int
	if err := f.db.SQL.QueryRow(`SELECT count(*) FROM persona_icon_events`).Scan(&count); err != nil || count != 2 {
		t.Fatal("creation audit missing", count, err)
	}
}

func TestAgentUXIcon_Generated_Integration(t *testing.T) {
	f, s := iconFixture(t)
	ctx := context.Background()
	original := createIconDraft(t, s, "commands")
	preview, err := s.PreviewIcon(ctx, "commands", 1, "shuffle", "user:business-owner", f.when, nil)
	if err != nil || preview.Revision != 1 || preview.Value == original.Value {
		t.Fatal("preview", preview, err)
	}
	if stored, err := s.GetIcon(ctx, "commands"); err != nil || stored != original {
		t.Fatal("preview wrote state", stored, err)
	}
	shuffled, err := s.ChangeIcon(ctx, "commands", 1, "shuffle", "user:business-owner", f.when, nil)
	if err != nil || shuffled.Value != preview.Value || shuffled.Revision != 2 {
		t.Fatal("shuffle", shuffled, err)
	}
	if _, err = s.ChangeIcon(ctx, "commands", 1, "shuffle", "user:business-owner", f.when, nil); !errors.Is(err, ErrConflict) {
		t.Fatal("replay accepted", err)
	}
	undone, err := s.ChangeIcon(ctx, "commands", 2, "undo", "user:technical-steward", f.when, nil)
	if err != nil || undone.Value != original.Value || undone.Revision != 3 {
		t.Fatal("undo", undone, err)
	}
	v := version(s.tenant, "commands", 2)
	v.DisplayName = "Birthday coordinator"
	v.Profile = json.RawMessage(`{"owner":"user:business-owner","instructions":"Celebrate birthdays and cake"}`)
	if err = s.PutVersion(ctx, v); err != nil {
		t.Fatal(err)
	}
	stable, err := s.ChangeIcon(ctx, "commands", 3, "shuffle", "user:business-owner", f.when, nil)
	if err != nil || stable.Value.Glyph != "book" {
		t.Fatal("shuffle reinterpreted new instructions", stable, err)
	}
	regenerated, err := s.ChangeIcon(ctx, "commands", 4, "regenerate", "user:business-owner", f.when, nil)
	if err != nil || regenerated.Value.Glyph != "cake" {
		t.Fatal("regenerate ignored current instructions", regenerated, err)
	}
	reset, err := s.ChangeIcon(ctx, "commands", 5, "reset", "user:business-owner", f.when, nil)
	if err != nil || reset.Value != original.Value || reset.Revision != 6 {
		t.Fatal("reset", reset, err)
	}
	var count int
	if err = f.db.SQL.QueryRow(`SELECT count(*) FROM persona_icon_events WHERE persona_id='commands'`).Scan(&count); err != nil || count != 6 {
		t.Fatal("audit count", count, err)
	}
	if _, err = f.db.SQL.Exec(`UPDATE persona_icon_events SET action='shuffle' WHERE persona_id='commands'`); err == nil {
		t.Fatal("audit mutable")
	}
	if _, err = f.db.SQL.Exec(`DELETE FROM persona_icon_events WHERE persona_id='commands'`); err == nil {
		t.Fatal("audit deletable")
	}
}

type iconAdmin struct{ allow bool }

func (a iconAdmin) AuthorizeAgentIconAdministrator(context.Context, values.TenantId, string) error {
	if a.allow {
		return nil
	}
	return ErrIconDenied
}

func TestAgentUXIcon_Generated_Security(t *testing.T) {
	f, s := iconFixture(t)
	ctx := context.Background()
	initial := createIconDraft(t, s, "secure")
	for _, admin := range []IconAdministrator{nil, iconAdmin{}} {
		if _, err := s.ChangeIcon(ctx, "secure", 1, "shuffle", "intruder", f.when, admin); !errors.Is(err, ErrIconDenied) {
			t.Fatal("unauthorized write", err)
		}
		if _, err := s.PreviewIcon(ctx, "secure", 1, "regenerate", "intruder", f.when, admin); !errors.Is(err, ErrIconDenied) {
			t.Fatal("unauthorized preview", err)
		}
	}
	if got, err := s.GetIcon(ctx, "secure"); err != nil || got != initial {
		t.Fatal("denial mutated state", got, err)
	}
	if changed, err := s.ChangeIcon(ctx, "secure", 1, "shuffle", "administrator", f.when, iconAdmin{allow: true}); err != nil || changed.Revision != 2 {
		t.Fatal("trusted administrator", changed, err)
	}
	if _, err := s.ChangeIcon(ctx, "secure", 2, "upload", "user:business-owner", f.when, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("unknown operation", err)
	}
	// A failed audit insert must roll back the identity update.
	f.db.Exec(t, `ALTER TABLE persona_icon_events ADD CONSTRAINT reject_revision CHECK (revision < 3)`)
	if _, err := s.ChangeIcon(ctx, "secure", 2, "shuffle", "user:business-owner", f.when, nil); err == nil {
		t.Fatal("failed audit accepted")
	}
	if got, err := s.GetIcon(ctx, "secure"); err != nil || got.Revision != 2 {
		t.Fatal("failed audit advanced revision", got, err)
	}
	other := f.store(t, "icon-b")
	if _, err := other.ChangeIcon(ctx, "secure", 2, "shuffle", "user:business-owner", f.when, nil); !errors.Is(err, ErrIconDenied) {
		t.Fatal("tenant scope", err)
	}
	var policies int
	if err := f.db.SQL.QueryRow(`SELECT count(*) FROM pg_policies WHERE schemaname=current_schema() AND tablename IN ('persona_icons','persona_icon_events') AND policyname='tenant_isolation'`).Scan(&policies); err != nil || policies != 2 {
		t.Fatal("RLS absent", policies, err)
	}
}

func TestAgentUXIcon_Generated_Backfill(t *testing.T) {
	f, s := iconFixture(t)
	ctx := context.Background()
	// Every store write now gives a new agent its icon, so the rows a backfill
	// exists for are the ones written before the icon tables: seed them as the
	// table owner, the way they were stored then.
	seed := func(id string, n int64) {
		t.Helper()
		v := version(s.tenant, id, n)
		f.db.Exec(t, `INSERT INTO persona_versions
			(tenant_id,persona_id,version,agent_version,handle,display_name,profile,content_digest,created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9)`,
			f.ids[s.tenant], v.PersonaID, v.Version, v.AgentVersion, v.Handle, v.DisplayName, string(v.Profile), v.ContentDigest, v.CreatedAt)
	}
	for _, id := range []string{"one", "two", "three"} {
		seed(id, 1)
	}
	seed("one", 2)
	if _, err := s.GetIcon(ctx, "one"); !errors.Is(err, ErrNotFound) {
		t.Fatal("seeded agent already has an icon", err)
	}
	if count, err := s.BackfillIcons(ctx, "preparation", f.when); err != nil || count != 3 {
		t.Fatal("backfill", count, err)
	} else {
		t.Logf("backfill count=%d", count)
	}
	if count, err := s.BackfillIcons(ctx, "preparation", f.when); err != nil || count != 0 {
		t.Fatal("repeat backfill", count, err)
	} else {
		t.Logf("repeat backfill count=%d", count)
	}
	for _, id := range []string{"one", "two", "three"} {
		got, err := s.GetIcon(ctx, id)
		if err != nil || got.Revision != 1 || !got.Value.Valid() {
			t.Fatal("missing icon", id, got, err)
		}
	}
}

func TestAgentUXIcon_Generated_Property(t *testing.T) {
	f, s := iconFixture(t)
	ctx := context.Background()
	stores := []*TenantStore{s, f.store(t, "icon-a")}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i, id := range []string{"first", "second"} {
		wg.Add(1)
		go func(s *TenantStore, id string) {
			defer wg.Done()
			v := version(s.tenant, id, 1)
			owner, steward := draftOwners(v)
			errs <- s.CreateDraftWithIcon(ctx, v, owner, steward, "user:business-owner", f.when)
		}(stores[i], id)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	a, err := s.GetIcon(ctx, "first")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.GetIcon(ctx, "second")
	if err != nil || a.Value.Glyph == b.Value.Glyph {
		t.Fatal("concurrent collision", a, b, err)
	}
	// The draft writer's rollback must also remove the profile and owner rows.
	f.db.Exec(t, `ALTER TABLE persona_icon_events ADD CONSTRAINT reject_creation CHECK (persona_id <> 'rollback')`)
	v := version(s.tenant, "rollback", 1)
	owner, steward := draftOwners(v)
	if err = s.CreateDraftWithIcon(ctx, v, owner, steward, "user:business-owner", f.when); err == nil {
		t.Fatal("audit failure did not fail creation")
	}
	if _, err = s.GetVersion(ctx, "rollback", 1); !errors.Is(err, ErrNotFound) {
		t.Fatal("profile escaped transaction", err)
	}
	if _, err = s.GetIcon(ctx, "rollback"); !errors.Is(err, ErrNotFound) {
		t.Fatal("icon escaped transaction", err)
	}
	if err = s.CreateDraftWithIcon(ctx, version("icon-b", "bad", 1), owner, steward, "user:business-owner", time.Time{}); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid creation", err)
	}
	if got := IconInput(PersonaVersion{DisplayName: "safe", Profile: json.RawMessage(`{"skill_pins":[{"id":"birthday"}]}`)}); agenticon.Generate(got).Glyph != "cake" {
		t.Fatal("skill names ignored", got)
	}
	if err = MigrateIcons(ctx, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("nil migration DB", err)
	}
}
