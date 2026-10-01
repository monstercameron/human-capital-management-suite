package agentinvocationstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_AGENTP_008_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	a, b := uuid.New(), uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1),($2)`, a, b); err != nil {
		t.Fatal(err)
	}
	login, password := roleName("invocation"), uuid.NewString()
	if err := createLogin(ctx, db.SQL, login, password); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.SQL.ExecContext(context.Background(), "DROP ROLE "+login) })
	cfg := agentstore.Config{DSN: testDSN(t, db.URL, db.Schema, login, password, "postgres"), CoreDSN: "postgres://core:pw@127.0.0.1:5432/core", MaxConns: 4}
	pool, err := agentstore.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	s, err := New(pool)
	if err != nil {
		t.Fatal(err)
	}
	logical, err := NewWithTenantUUID(pool, func(value string) uuid.UUID {
		switch value {
		case "harborcare-demo":
			return a
		case "ironridge-demo":
			return b
		default:
			return uuid.Nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	opaque := invocation("harborcare-demo", "opaque", "opaque-persona", "opaque-post", "opaque-user")
	if got, created, err := logical.Claim(ctx, opaque); err != nil || !created || got.TenantID != opaque.TenantID {
		t.Fatalf("opaque claim=%+v created=%v error=%v", got, created, err)
	}
	if got, created, err := logical.Claim(ctx, opaque); err != nil || created || got.TenantID != opaque.TenantID {
		t.Fatalf("opaque replay=%+v created=%v error=%v", got, created, err)
	}
	opaqueGrant := agentinvoke.DelegationGrant{ID: "opaque-grant", UserID: opaque.InvokerID, TenantID: opaque.TenantID, TaskID: opaque.ID, TargetAgentID: "agent:opaque", Skills: opaque.Skills}
	if got, err := logical.SetGrant(ctx, opaque.TenantID, opaque.ID, opaqueGrant); err != nil || got.Grant.TenantID != opaque.TenantID || got.TenantID != opaque.TenantID {
		t.Fatalf("opaque grant=%+v error=%v", got, err)
	}
	if started, err := logical.MarkStarted(ctx, opaque.TenantID, opaque.ID); err != nil || !started {
		t.Fatalf("opaque start=%v error=%v", started, err)
	}
	if got, err := logical.Lookup(ctx, opaque.TenantID, opaque.InvokerID, opaque.ID); err != nil || got.TenantID != opaque.TenantID || got.Grant.TenantID != opaque.TenantID {
		t.Fatalf("opaque lookup=%+v error=%v", got, err)
	}
	if rows, err := logical.ListPersonaInvocations(ctx, opaque.TenantID, opaque.InvokerID, opaque.ConversationID); err != nil || len(rows) != 1 || rows[0].TenantID != opaque.TenantID {
		t.Fatalf("opaque list=%+v error=%v", rows, err)
	}
	if _, err := logical.Lookup(ctx, "ironridge-demo", opaque.InvokerID, opaque.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("opaque foreign tenant error=%v", err)
	}
	if _, err := logical.Lookup(ctx, "unregistered", opaque.InvokerID, opaque.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unregistered mapping error=%v", err)
	}
	forged := opaque
	forged.Actor.UserID = "different-user"
	if _, _, err := logical.Claim(ctx, forged); !errors.Is(err, ErrInvalid) {
		t.Fatalf("forged actor binding persisted: %v", err)
	}
	narrow := invocation("harborcare-demo", "narrow", "narrow-persona", "narrow-post", "narrow-user")
	narrow.Skills = agentinvoke.SkillScopes{"read": {"self", "team"}}
	if _, _, err := logical.Claim(ctx, narrow); err != nil {
		t.Fatal(err)
	}
	narrowGrant := agentinvoke.DelegationGrant{ID: "narrow-grant", UserID: narrow.InvokerID, TenantID: narrow.TenantID, TaskID: narrow.ID, TargetAgentID: "agent:narrow", Skills: agentinvoke.SkillScopes{"read": {"self"}}}
	if got, err := logical.SetGrant(ctx, narrow.TenantID, narrow.ID, narrowGrant); err != nil || len(got.Skills["read"]) != 1 {
		t.Fatalf("narrowed grant=%+v error=%v", got, err)
	}
	reopened, err := NewWithTenantUUID(pool, func(value string) uuid.UUID {
		if value == "harborcare-demo" {
			return a
		}
		return uuid.Nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, created, err := reopened.Claim(ctx, narrow); err != nil || created || got.Grant.ID != narrowGrant.ID || len(got.Skills["read"]) != 1 {
		t.Fatalf("restart narrowed claim=%+v created=%v error=%v", got, created, err)
	}
	if got, err := reopened.Lookup(ctx, narrow.TenantID, narrow.InvokerID, narrow.ID); err != nil || len(got.Skills["read"]) != 1 {
		t.Fatalf("narrowed lookup=%+v error=%v", got, err)
	}
	candidate := invocation(a.String(), "p1", "i1", "post-1", "u1")
	got, created, err := s.Claim(ctx, candidate)
	if err != nil || !created {
		t.Fatalf("claim created=%v err=%v", created, err)
	}
	if got.ID != candidate.ID {
		t.Fatalf("claim id=%q", got.ID)
	}
	replay, created, err := s.Claim(ctx, candidate)
	if err != nil || created || replay.ID != got.ID {
		t.Fatalf("replay=%+v created=%v err=%v", replay, created, err)
	}
	grant := agentinvoke.DelegationGrant{ID: "grant-1", UserID: "u1", TenantID: a.String(), TaskID: candidate.ID, TargetAgentID: "agent:comp-analyst", Skills: agentinvoke.SkillScopes{"read": {"self"}}}
	if _, err := s.SetGrant(ctx, a.String(), candidate.ID, grant); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.MarkStarted(ctx, a.String(), candidate.ID); err != nil || !ok {
		t.Fatalf("start=%v err=%v", ok, err)
	}
	if ok, err := s.MarkStarted(ctx, a.String(), candidate.ID); err != nil || ok {
		t.Fatalf("start replay=%v err=%v", ok, err)
	}
	loaded, err := s.Lookup(ctx, a.String(), "u1", candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Grant.TargetAgentID != "agent:comp-analyst" || loaded.Grant.TaskID != candidate.ID {
		t.Fatalf("durable invocation grant lost target/run binding: %+v", loaded.Grant)
	}
	wrongScope := invocation(a.String(), "p2", "i2", "post-2", "u1")
	if _, _, err := s.Claim(ctx, wrongScope); err != nil {
		t.Fatal(err)
	}
	wrongScopeGrant := grant
	wrongScopeGrant.ID = "grant-wrong-run"
	wrongScopeGrant.TaskID = candidate.ID
	if _, err := s.SetGrant(ctx, a.String(), wrongScope.ID, wrongScopeGrant); err == nil {
		t.Fatal("grant for another invocation was attached")
	}
	if _, err := s.Lookup(ctx, b.String(), "u1", candidate.ID); err == nil {
		t.Fatal("cross-tenant lookup succeeded")
	}
	listed, err := s.ListPersonaInvocations(ctx, a.String(), "u1", candidate.ConversationID)
	if err != nil || len(listed) != 2 || listed[1].ID != candidate.ID || listed[1].Grant.ID != grant.ID || listed[1].State != agentinvoke.InvocationStarted {
		t.Fatalf("owned durable invocation list = %+v, %v", listed, err)
	}
	for _, scope := range []struct{ tenant, owner, conversation string }{
		{b.String(), "u1", candidate.ConversationID}, {a.String(), "other", candidate.ConversationID}, {a.String(), "u1", "other-conversation"},
	} {
		listed, err := s.ListPersonaInvocations(ctx, scope.tenant, scope.owner, scope.conversation)
		if err != nil || len(listed) != 0 {
			t.Fatalf("invocation list leaked scope %+v: %+v, %v", scope, listed, err)
		}
	}
	if _, err := s.SetGrant(ctx, b.String(), candidate.ID, grant); err == nil {
		t.Fatal("cross-tenant grant succeeded")
	}
}

func TestTodo_AGENTP_008_Race(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1)`, tenant); err != nil {
		t.Fatal(err)
	}
	login, password := roleName("invocationrace"), uuid.NewString()
	if err := createLogin(ctx, db.SQL, login, password); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.SQL.ExecContext(context.Background(), "DROP ROLE "+login) })
	pool, err := agentstore.New(ctx, agentstore.Config{DSN: testDSN(t, db.URL, db.Schema, login, password, "postgres"), CoreDSN: "postgres://core:pw@127.0.0.1:5432/core", MaxConns: 4})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	s, _ := New(pool)
	c := invocation(tenant.String(), "p", "i", "post-race", "u")
	var wg sync.WaitGroup
	var mu sync.Mutex
	created := 0
	errs := 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok, err := s.Claim(ctx, c)
			mu.Lock()
			defer mu.Unlock()
			if ok {
				created++
			}
			if err != nil {
				errs++
			}
		}()
	}
	wg.Wait()
	if created != 1 || errs != 0 {
		t.Fatalf("created=%d errs=%d", created, errs)
	}
}

func invocation(tenant, post, persona, postID, user string) agentinvoke.Invocation {
	id := "inv-" + postID
	return agentinvoke.Invocation{ID: id, TenantID: tenant, ConversationID: "conversation", ThreadID: "thread", PostID: postID, InvokerID: user, PersonaID: persona, PersonaVersion: "v1", InstallationID: "install", Mode: agentinvoke.OnBehalfOf, Skills: agentinvoke.SkillScopes{"read": {"self"}}, Actor: agentinvoke.ActorChain{UserID: user, PersonaID: persona, PersonaVersion: "v1", InstallationID: "install", ConversationID: "conversation", InvokingPostID: postID, InvocationID: id}, State: agentinvoke.InvocationClaimed}
}

func createLogin(ctx context.Context, db *sql.DB, name, password string) error {
	if _, err := db.ExecContext(ctx, fmt.Sprintf("CREATE ROLE %s LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS PASSWORD '%s'", name, password)); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, "GRANT "+agentstore.AppRole+" TO "+name)
	return err
}
func roleName(prefix string) string { return prefix + strings.ReplaceAll(uuid.NewString(), "-", "") }
func testDSN(t *testing.T, base, schema, user, password, database string) string {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(user, password)
	u.Path = "/" + database
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}
