package agentcontextstore

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_AGENTP_008_Recovery_BoundedContextSurvivesRestartAndTenantIsolation(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenant, other := uuid.New(), uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1),($2)`, tenant, other); err != nil {
		t.Fatal(err)
	}
	login := "context" + strings.ReplaceAll(uuid.NewString(), "-", "")
	password := uuid.NewString()
	if _, err := db.SQL.ExecContext(ctx, fmt.Sprintf("CREATE ROLE %s LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS PASSWORD '%s'", login, password)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.ExecContext(ctx, "GRANT "+agentstore.AppRole+" TO "+login); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.SQL.ExecContext(context.Background(), "DROP ROLE "+login) })
	u, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(login, password)
	query := u.Query()
	query.Set("search_path", db.Schema)
	u.RawQuery = query.Encode()
	cfg := agentstore.Config{DSN: u.String(), CoreDSN: "postgres://core:pw@127.0.0.1:5432/core", MaxConns: 2}
	first, err := agentstore.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := testSnapshot(tenant.String())
	store, err := New(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	first.Close()
	restarted, err := agentstore.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	restored, _ := New(restarted)
	got, err := restored.Get(ctx, tenant.String(), snapshot.SnapshotID)
	if err != nil || !reflect.DeepEqual(got, snapshot) {
		t.Fatalf("restored=%+v err=%v", got, err)
	}
	if err := restored.Put(ctx, snapshot); err != nil {
		t.Fatalf("same-context replay: %v", err)
	}
	if _, err := restored.Get(ctx, other.String(), snapshot.SnapshotID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign tenant: %v", err)
	}
	var count int
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM persona_thread_context`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("contexts=%d err=%v", count, err)
	}
	changed := snapshot
	changed.Posts = append([]chat.Post(nil), snapshot.Posts...)
	changed.Posts[0].SourceAttribution = &chat.SourceAttribution{OriginalAuthorID: "new source"}
	if err := restored.Put(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("same canonical digest with changed attribution: %v", err)
	}
	if _, err := db.SQL.ExecContext(ctx, `UPDATE persona_thread_context SET digest=$1 WHERE tenant_id=$2`, snapshot.Digest, tenant); err == nil {
		t.Fatal("immutable context updated")
	}
	logical := testSnapshot("harborcare-demo")
	mapper := func(ref string) uuid.UUID {
		if ref == "harborcare-demo" || ref == "alias" {
			return tenant
		}
		return uuid.Nil
	}
	logicalStore, err := NewWithTenantUUID(restarted, mapper)
	if err != nil {
		t.Fatal(err)
	}
	if err = logicalStore.Put(ctx, logical); err != nil {
		t.Fatal(err)
	}
	logicalRestored, err := NewWithTenantUUID(restarted, mapper)
	if err != nil {
		t.Fatal(err)
	}
	got, err = logicalRestored.Get(ctx, "harborcare-demo", logical.SnapshotID)
	if err != nil || !reflect.DeepEqual(got, logical) {
		t.Fatalf("logical tenant source changed: %+v %v", got, err)
	}
	if _, err = logicalRestored.Get(ctx, "alias", logical.SnapshotID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("logical alias widened source: %v", err)
	}
	if _, err = logicalRestored.Get(ctx, "unknown", logical.SnapshotID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown logical tenant: %v", err)
	}
}

func testSnapshot(tenant string) chat.ThreadSnapshot {
	s := chat.ThreadSnapshot{TenantID: tenant, ConversationID: "room", ThreadID: "post", InvokingPostID: "post", PrincipalTenantID: tenant, PrincipalID: "alice", PrincipalRoles: []string{"employee"}, Revision: 1, AuthorityRevision: 1, Posts: []chat.Post{{TenantID: tenant, ConversationID: "room", ID: "post", AuthorID: "alice", AuthorHomeTenantID: tenant, Body: "question", Sequence: 1, Revision: 1}}}
	s.Digest, _ = chat.ThreadSnapshotDigest(s)
	s.SnapshotID = "chat-thread-" + s.Digest
	return s
}

func TestTodo_AGENTP_008_Security_ContextRejectsMalformedAndChangedImages(t *testing.T) {
	if _, err := New(nil); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := NewWithTenantUUID(nil, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	s := testSnapshot(uuid.NewString())
	cases := []chat.ThreadSnapshot{s}
	cases[0].Posts[0].Body = "injected"
	wrong := testSnapshot(uuid.NewString())
	wrong.PrincipalTenantID = uuid.NewString()
	cases = append(cases, wrong)
	deleted := testSnapshot(uuid.NewString())
	deleted.Posts[0].Deleted = true
	cases = append(cases, deleted)
	duplicate := testSnapshot(uuid.NewString())
	duplicate.Posts = append(duplicate.Posts, duplicate.Posts[0])
	cases = append(cases, duplicate)
	for _, bad := range cases {
		if _, _, err := encode(bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted malformed context: %+v", bad)
		}
	}
	var nilStore *Store
	if _, err := nilStore.Get(context.Background(), uuid.NewString(), "id"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if err := nilStore.Put(context.Background(), s); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
