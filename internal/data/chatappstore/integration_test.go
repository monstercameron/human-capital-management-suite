package chatappstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func appStoreFixture(t *testing.T) (*Store, context.Context) {
	t.Helper()
	db := pgtest.NewEmpty(t)
	data, err := chatstore.Migrations.ReadFile("migrations/00004_chatapps.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.SplitN(strings.SplitN(string(data), "-- +goose Down", 2)[0], "-- +goose Up", 2)[1]
	if _, err := db.SQL.ExecContext(context.Background(), up); err != nil {
		t.Fatalf("app migration: %v", err)
	}
	// The application role has no BYPASSRLS and cannot own these tables.
	db.Exec(t, `DO $$ BEGIN CREATE ROLE hcmnext_app NOLOGIN NOBYPASSRLS; EXCEPTION WHEN duplicate_object THEN NULL; END $$`)
	db.Exec(t, `GRANT USAGE ON SCHEMA `+db.Schema+` TO hcmnext_app`)
	db.Exec(t, `GRANT SELECT,INSERT,UPDATE ON ALL TABLES IN SCHEMA `+db.Schema+` TO hcmnext_app`)
	if _, err := db.Conn.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatalf("set non-superuser role: %v", err)
	}
	return New(db.Conn), WithTenant(context.Background(), "tenant-a")
}

func TestTodo_CHAT_040_Integration_DurableInstallAndTenantIsolation(t *testing.T) {
	store, ctx := appStoreFixture(t)
	now := time.Now().UTC()
	v := chatapps.Installation{ID: "tenant-a:conversation:app", Tenant: "tenant-a", Conversation: "conversation", AppID: "app", Version: 1, Manifest: chatapps.Manifest{AppID: "app", Version: 1}, GrantedScopes: []string{}, Status: chatapps.Active, Approver: "manager", Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := store.Put(ctx, v); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, v.ID)
	if err != nil || got.Version != 1 || got.Revision != 1 || got.Status != chatapps.Active {
		t.Fatalf("durable installation=%+v err=%v", got, err)
	}
	other := WithTenant(context.Background(), "tenant-b")
	if _, err := store.Get(other, v.ID); !errors.Is(err, chatapps.ErrNotFound) {
		t.Fatalf("foreign tenant read: %v", err)
	}
	if rows, err := store.ByConversation(other, "tenant-b", v.Conversation); err != nil || len(rows) != 0 {
		t.Fatalf("foreign tenant list=%+v err=%v", rows, err)
	}
	v.Revision = 2
	v.Status = chatapps.Revoked
	if err := store.Put(ctx, v); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, v); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision: %v", err)
	}
}

func TestTodo_CHAT_047_Integration_AppMutationAuditAtomicity(t *testing.T) {
	db := pgtest.NewEmpty(t)
	for _, name := range []string{"00001_chat.sql", "00002_chat_records.sql", "00004_chatapps.sql", "00006_chat_event_sequence.sql", "00007_chat_audit_immutable.sql"} {
		data, err := chatstore.Migrations.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		up := strings.SplitN(strings.SplitN(string(data), "-- +goose Down", 2)[0], "-- +goose Up", 2)[1]
		// These fixtures use the same SQL as the independent chat migration tree.
		if _, err := db.SQL.ExecContext(context.Background(), up); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	db.Exec(t, `DO $$ BEGIN CREATE ROLE hcmnext_app NOLOGIN NOBYPASSRLS; EXCEPTION WHEN duplicate_object THEN NULL; END $$`)
	db.Exec(t, `GRANT USAGE ON SCHEMA `+db.Schema+` TO hcmnext_app`)
	db.Exec(t, `GRANT SELECT,INSERT,UPDATE ON ALL TABLES IN SCHEMA `+db.Schema+` TO hcmnext_app`)
	db.Exec(t, `GRANT USAGE ON ALL SEQUENCES IN SCHEMA `+db.Schema+` TO hcmnext_app`)
	// Force projection failure after the installation CAS has run.
	db.Exec(t, `CREATE FUNCTION fail_app_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='chat.app.install' THEN RAISE EXCEPTION 'injected audit fault'; END IF; RETURN NEW; END $$`)
	db.Exec(t, `CREATE TRIGGER fail_app_audit BEFORE INSERT ON chat_audit_event FOR EACH ROW EXECUTE FUNCTION fail_app_audit()`)
	if _, err := db.Conn.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	store := New(db.Conn)
	ctx := WithTenant(context.Background(), "tenant-a")
	actor := chatapps.Actor{Tenant: "tenant-a", Conversation: "conv", Principal: "owner"}
	now := time.Now().UTC()
	v := chatapps.Installation{ID: "tenant-a:conv:app", Tenant: "tenant-a", Conversation: "conv", AppID: "app", Version: 1, Manifest: chatapps.Manifest{AppID: "app", Version: 1}, GrantedScopes: []string{}, Status: chatapps.Active, Approver: "owner", Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := store.PutAudited(ctx, v, actor, "tenant-a", "chat.app.install", 2); err == nil {
		t.Fatal("fault did not reject installation")
	}
	if _, err := store.Get(ctx, v.ID); !errors.Is(err, chatapps.ErrNotFound) {
		t.Fatalf("installation survived audit failure: %v", err)
	}
	if _, err := db.Conn.Exec(context.Background(), `RESET ROLE`); err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `DROP TRIGGER fail_app_audit ON chat_audit_event`)
	if _, err := db.Conn.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	if err := store.PutAudited(ctx, v, actor, "tenant-a", "chat.app.install", 2); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if _, err := db.Conn.Exec(context.Background(), `RESET ROLE`); err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `CREATE OR REPLACE FUNCTION fail_app_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='chat.app.status' THEN RAISE EXCEPTION 'injected status audit fault'; END IF; RETURN NEW; END $$`)
	db.Exec(t, `CREATE TRIGGER fail_app_audit BEFORE INSERT ON chat_audit_event FOR EACH ROW EXECUTE FUNCTION fail_app_audit()`)
	if _, err := db.Conn.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	v.Revision = 2
	v.Status = chatapps.Suspended
	if err := store.PutAudited(ctx, v, actor, "tenant-a", "chat.app.status", 2); err == nil {
		t.Fatal("status fault did not reject mutation")
	}
	if unchanged, err := store.Get(ctx, v.ID); err != nil || unchanged.Revision != 1 || unchanged.Status != chatapps.Active {
		t.Fatalf("status survived audit fault: %+v err=%v", unchanged, err)
	}
	if _, err := db.Conn.Exec(context.Background(), `RESET ROLE`); err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `DROP TRIGGER fail_app_audit ON chat_audit_event`)
	if _, err := db.Conn.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	if err := store.PutAudited(ctx, v, actor, "tenant-a", "chat.app.status", 2); err != nil {
		t.Fatalf("status: %v", err)
	}
	if err := store.PutAudited(ctx, v, actor, "tenant-a", "chat.app.status", 2); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale write: %v", err)
	}
	var audits, outbox, inventory int
	tx, err := db.Conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(context.Background(), `SELECT set_config('hcmnext.tenant_id','tenant-a',true)`); err != nil {
		t.Fatal(err)
	}
	for query, target := range map[string]*int{
		`SELECT count(*) FROM chat_audit_event WHERE tenant_id='tenant-a' AND actor_id='owner' AND prior_revision IN (0,1)`:          &audits,
		`SELECT count(*) FROM chat_outbox WHERE tenant_id='tenant-a' AND event_type LIKE 'chat.app.%'`:                               &outbox,
		`SELECT count(*) FROM chat_record_inventory WHERE tenant_id='tenant-a' AND record_id='app:tenant-a:conv:app' AND revision=2`: &inventory,
	} {
		if err := tx.QueryRow(context.Background(), query).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if audits != 2 || outbox != 2 || inventory != 1 {
		t.Fatalf("audit=%d outbox=%d inventory=%d", audits, outbox, inventory)
	}
}

func TestTodo_CHAT_042_Recovery_AtomicReplayAcrossStoreInstances(t *testing.T) {
	store, ctx := appStoreFixture(t)
	e := chatapps.Event{ID: "event-1", Tenant: "tenant-a", Conversation: "conversation", InstallationID: "tenant-a:conversation:app", Type: "post", Sequence: 1, Payload: []byte(`{"value":1}`), At: time.Now().UTC(), Signature: "signed"}
	if err := store.AppendOnce(ctx, e, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	restarted := New(store.DB)
	if err := restarted.AppendOnce(ctx, e, time.Now().UTC()); !errors.Is(err, chatapps.ErrReplay) {
		t.Fatalf("replayed after restart: %v", err)
	}
	events, err := restarted.Events(ctx, e.Conversation, 0, 10)
	if err != nil || len(events) != 1 || events[0].ID != e.ID {
		t.Fatalf("replayed events=%+v err=%v", events, err)
	}
	foreign := WithTenant(context.Background(), "tenant-b")
	if events, err := restarted.Events(foreign, e.Conversation, 0, 10); err != nil || len(events) != 0 {
		t.Fatalf("foreign tenant events=%+v err=%v", events, err)
	}
}

func TestTodo_CHAT_040_Integration_ListIsScopedToConversation(t *testing.T) {
	store, ctx := appStoreFixture(t)
	now := time.Now().UTC()
	for _, conversation := range []string{"first", "second"} {
		v := chatapps.Installation{ID: "tenant-a:" + conversation + ":app", Tenant: "tenant-a", Conversation: conversation, AppID: "app", Version: 1, Manifest: chatapps.Manifest{AppID: "app", Version: 1}, GrantedScopes: []string{}, Status: chatapps.Active, Approver: "manager", Revision: 1, CreatedAt: now, UpdatedAt: now}
		if err := store.Put(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	installations, err := store.ByConversation(ctx, "tenant-a", "first")
	if err != nil || len(installations) != 1 || installations[0].Conversation != "first" || installations[0].Manifest.AppID != "app" {
		t.Fatalf("conversation listing=%+v err=%v", installations, err)
	}
	if _, err := store.Get(context.Background(), installations[0].ID); err == nil {
		t.Fatal("Get without tenant context admitted")
	}
}

func TestTodo_CHAT_042_Integration_ReceiptAndAppendAreTenantScoped(t *testing.T) {
	store, ctx := appStoreFixture(t)
	now := time.Now().UTC()
	seen, err := store.Seen(ctx, "receipt-1")
	if err != nil || seen {
		t.Fatalf("new receipt seen=%v err=%v", seen, err)
	}
	if err := store.MarkSeen(ctx, "receipt-1", now); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSeen(ctx, "receipt-1", now); !errors.Is(err, chatapps.ErrReplay) {
		t.Fatalf("duplicate receipt: %v", err)
	}
	seen, err = New(store.DB).Seen(ctx, "receipt-1")
	if err != nil || !seen {
		t.Fatalf("durable receipt seen=%v err=%v", seen, err)
	}
	foreign := WithTenant(context.Background(), "tenant-b")
	seen, err = store.Seen(foreign, "receipt-1")
	if err != nil || seen {
		t.Fatalf("foreign receipt seen=%v err=%v", seen, err)
	}
	e := chatapps.Event{ID: "event-2", Tenant: "tenant-a", Conversation: "first", InstallationID: "tenant-a:first:app", Type: "post", Sequence: 2, Payload: []byte(`{"value":2}`), At: now, Signature: "signed"}
	if err := store.AppendEvent(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvent(ctx, e); err != nil {
		t.Fatalf("idempotent append: %v", err)
	}
	events, err := New(store.DB).Events(ctx, "first", 0, 10)
	if err != nil || len(events) != 1 || events[0].ID != e.ID {
		t.Fatalf("durable append=%+v err=%v", events, err)
	}
	events, err = store.Events(foreign, "first", 0, 10)
	if err != nil || len(events) != 0 {
		t.Fatalf("foreign events=%+v err=%v", events, err)
	}
}
