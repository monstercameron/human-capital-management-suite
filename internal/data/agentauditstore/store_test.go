package agentauditstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type fixture struct {
	db      *pgtest.DB
	ids     map[string]uuid.UUID
	mapper  func(values.TenantId) uuid.UUID
	store   *Store
	newConn func() *pgxadapter.Conn
}

func newFixture(t *testing.T, tenants ...string) *fixture {
	t.Helper()
	db := pgtest.New(t)
	ids := map[string]uuid.UUID{}
	for _, name := range tenants {
		id := uuid.New()
		ids[name] = id
		db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, strings.ReplaceAll(name, ":", "-"), name)
	}
	mapper := func(key values.TenantId) uuid.UUID { return ids[string(key)] }
	f := &fixture{db: db, ids: ids, mapper: mapper}
	f.newConn = func() *pgxadapter.Conn {
		conn := db.NewConn(t)
		if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
			t.Fatalf("assume app role: %v", err)
		}
		return conn
	}
	store, err := New(f.newConn(), mapper)
	if err != nil {
		t.Fatal(err)
	}
	f.store = store
	return f
}

func testEntry(id, tenant, user, task string, kind agentaudit.EventKind) agentaudit.Entry {
	actor := agentaudit.ActorChain{UserID: user, AgentVersion: "agent:v3", InstallationID: "install:7", TaskID: task, PlanRevision: "plan:4", StepID: "step:2", DelegationGrantID: "grant:9"}
	return agentaudit.Entry{EventID: id, TenantID: tenant, Kind: kind, Actor: actor, Action: "people.lookup", ArgumentsDigest: "sha256:args", ResultDigest: "sha256:result",
		Fields:     []agentaudit.Field{{Name: "subject", Value: "worker-7 <ok>", Classification: agentaudit.ClassificationPublic}, {Name: "salary", Value: "125000", Classification: agentaudit.ClassificationRestricted}, {Name: "note", Value: "n", Classification: agentaudit.ClassificationInternal}},
		Edges:      []agentaudit.Edge{{Kind: agentaudit.EdgeTask, From: task, To: id}, {Kind: agentaudit.EdgeIntent, From: task, To: "intent:" + id}, {Kind: agentaudit.EdgeApproval, From: task, To: "approval:7"}, {Kind: agentaudit.EdgeWorkflowRun, From: task, To: "workflow:7"}, {Kind: agentaudit.EdgeConnectorOp, From: task, To: "connector-op:7"}},
		OccurredAt: time.Date(2026, 9, 28, 12, 0, 0, 123456789, time.UTC)}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTodo_AGENT2_014_NewRequiresDependencies(t *testing.T) {
	if _, err := New(nil, nil); !errors.Is(err, agentaudit.ErrInvalidEntry) {
		t.Fatalf("New(nil) err = %v", err)
	}
	var nilStore *Store
	ctx := context.Background()
	if _, err := nilStore.Append(ctx, agentaudit.Entry{}); !errors.Is(err, agentaudit.ErrInvalidEntry) {
		t.Fatalf("nil Append err = %v", err)
	}
	if _, err := nilStore.Query(ctx, agentaudit.Query{}); !errors.Is(err, agentaudit.ErrInvalidEntry) {
		t.Fatalf("nil Query err = %v", err)
	}
	if err := nilStore.Verify(ctx, "t"); !errors.Is(err, agentaudit.ErrInvalidEntry) {
		t.Fatalf("nil Verify err = %v", err)
	}
	if _, err := nilStore.FindByEdge(ctx, "t", agentaudit.EdgeTask, "", ""); !errors.Is(err, agentaudit.ErrInvalidEntry) {
		t.Fatalf("nil FindByEdge err = %v", err)
	}
}

func TestTodo_AGENT2_014_Integration(t *testing.T) {
	f := newFixture(t, "tenant:a")
	ctx := context.Background()
	entry := testEntry("event:1", "tenant:a", "user:a", "task:1", agentaudit.EventSkillCall)
	first, err := f.store.Append(ctx, entry)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if !first.Created || first.Sequence != 1 || first.PrevHash != "" || !strings.HasPrefix(first.ChainHash, "sha256:") {
		t.Fatalf("first = %+v", first)
	}
	// Idempotent, reordered-but-identical retry.
	retryEntry := entry
	retryEntry.Edges = []agentaudit.Edge{entry.Edges[4], entry.Edges[3], entry.Edges[2], entry.Edges[1], entry.Edges[0]}
	retry, err := f.store.Append(ctx, retryEntry)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if retry.Created || retry.Sequence != 1 || retry.ChainHash != first.ChainHash || !retry.RecordedAt.Equal(first.RecordedAt) {
		t.Fatalf("retry = %+v, want same record Created=false", retry)
	}
	changed := entry
	changed.Action = "people.delete"
	if _, err := f.store.Append(ctx, changed); !errors.Is(err, agentaudit.ErrDuplicateConflict) {
		t.Fatalf("changed duplicate err = %v, want ErrDuplicateConflict", err)
	}
	second, err := f.store.Append(ctx, testEntry("event:2", "tenant:a", "user:a", "task:1", agentaudit.EventApproval))
	if err != nil || second.Sequence != 2 || second.PrevHash != first.ChainHash {
		t.Fatalf("second = %+v, %v", second, err)
	}
	var count int
	if err := f.db.SQL.QueryRow(`SELECT count(*) FROM agent_audit_record`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("rows after retry/conflict = %d, %v; want 2", count, err)
	}
	if err := f.db.SQL.QueryRow(`SELECT count(*) FROM agent_audit_edge`).Scan(&count); err != nil || count != 10 {
		t.Fatalf("edge rows = %d, %v; want 10", count, err)
	}
	if err := f.store.Verify(ctx, "tenant:a"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if err := f.store.Verify(ctx, "tenant:a"); err != nil {
		t.Fatalf("Verify again: %v", err)
	}
	// The memory store agrees on the chain material.
	mem := agentaudit.NewMemoryStore()
	m1, _ := mem.Append(ctx, entry)
	if m1.ChainHash != first.ChainHash {
		t.Fatalf("durable hash %s != memory hash %s", first.ChainHash, m1.ChainHash)
	}
}

func TestTodo_AGENT2_014_Validation(t *testing.T) {
	f := newFixture(t, "tenant:a")
	ctx := context.Background()
	bad := testEntry("event:x", "tenant:a", "user:a", "task:1", agentaudit.EventSkillCall)
	bad.Action = ""
	if _, err := f.store.Append(ctx, bad); !errors.Is(err, agentaudit.ErrInvalidEntry) {
		t.Fatalf("invalid entry err = %v", err)
	}
	if _, err := f.store.Append(ctx, testEntry("event:x", "tenant:unknown", "u", "task:1", agentaudit.EventSkillCall)); !errors.Is(err, agentaudit.ErrTenantBoundary) {
		t.Fatalf("unknown tenant err = %v", err)
	}
	if _, err := f.store.Append(ctx, testEntry("event:x", " tenant:a", "u", "task:1", agentaudit.EventSkillCall)); !errors.Is(err, agentaudit.ErrInvalidEntry) {
		t.Fatalf("padded tenant err = %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := f.store.Append(cancelled, testEntry("event:x", "tenant:a", "u", "task:1", agentaudit.EventSkillCall)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Append err = %v", err)
	}
	if _, err := f.store.Query(cancelled, agentaudit.Query{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Query err = %v", err)
	}
	if err := f.store.Verify(cancelled, "tenant:a"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Verify err = %v", err)
	}
	if err := f.store.Verify(ctx, ""); !errors.Is(err, agentaudit.ErrInvalidEntry) {
		t.Fatalf("empty tenant Verify err = %v", err)
	}
	if err := f.store.Verify(ctx, "tenant:unknown"); !errors.Is(err, agentaudit.ErrTenantBoundary) {
		t.Fatalf("unknown tenant Verify err = %v", err)
	}
	if err := f.store.Verify(ctx, "tenant:a"); err != nil {
		t.Fatalf("empty chain must verify: %v", err)
	}
	if _, err := f.store.Query(ctx, agentaudit.Query{}); !errors.Is(err, agentaudit.ErrUnauthorized) {
		t.Fatalf("empty viewer err = %v", err)
	}
	viewer := agentaudit.Viewer{TenantID: "tenant:a", UserID: "u", Role: agentaudit.ViewerAuditor}
	if _, err := f.store.Query(ctx, agentaudit.Query{Viewer: viewer, Kinds: []agentaudit.EventKind{"BOGUS"}}); !errors.Is(err, agentaudit.ErrInvalidEntry) {
		t.Fatalf("bad kind err = %v", err)
	}
	if _, err := f.store.FindByEdge(ctx, "tenant:a", "", "", ""); !errors.Is(err, agentaudit.ErrInvalidEntry) {
		t.Fatalf("FindByEdge without kind err = %v", err)
	}
	if _, err := f.store.FindByEdge(ctx, "tenant:unknown", agentaudit.EdgeTask, "", ""); !errors.Is(err, agentaudit.ErrTenantBoundary) {
		t.Fatalf("FindByEdge unknown tenant err = %v", err)
	}
	var rows int
	if err := f.db.SQL.QueryRow(`SELECT count(*) FROM agent_audit_record`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("rejected appends left %d rows, %v", rows, err)
	}
}

func TestTodo_AGENT2_014_ViewerParityWithMemoryStore(t *testing.T) {
	f := newFixture(t, "tenant:a", "tenant:b")
	ctx := context.Background()
	mem := agentaudit.NewMemoryStore()
	entries := []agentaudit.Entry{
		testEntry("event:1", "tenant:a", "user:a", "task:1", agentaudit.EventSkillCall),
		testEntry("event:2", "tenant:a", "user:b", "task:2", agentaudit.EventApproval),
		testEntry("event:3", "tenant:a", "user:a", "task:1", agentaudit.EventIntentOrigin),
		testEntry("event:4", "tenant:a", "user:a", "task:3", agentaudit.EventWorkflowRun),
		testEntry("event:5", "tenant:b", "user:a", "task:1", agentaudit.EventConnectorOperation),
	}
	for _, e := range entries {
		dr, err := f.store.Append(ctx, e)
		if err != nil {
			t.Fatalf("durable Append %s: %v", e.EventID, err)
		}
		mr, err := mem.Append(ctx, e)
		if err != nil {
			t.Fatalf("memory Append %s: %v", e.EventID, err)
		}
		if dr.Sequence != mr.Sequence || dr.PrevHash != mr.PrevHash || dr.ChainHash != mr.ChainHash {
			t.Fatalf("chain diverged for %s: %+v vs %+v", e.EventID, dr, mr)
		}
	}
	cases := map[string]agentaudit.Query{
		"auditor all":         {Viewer: agentaudit.Viewer{TenantID: "tenant:a", UserID: "aud", Role: agentaudit.ViewerAuditor}},
		"auditor allowed":     {Viewer: agentaudit.Viewer{TenantID: "tenant:a", UserID: "aud", Role: agentaudit.ViewerAuditor, AllowedFields: map[string]bool{"salary": true}}},
		"auditor task":        {Viewer: agentaudit.Viewer{TenantID: "tenant:a", UserID: "aud", Role: agentaudit.ViewerAuditor}, TaskID: "task:1"},
		"auditor kinds":       {Viewer: agentaudit.Viewer{TenantID: "tenant:a", UserID: "aud", Role: agentaudit.ViewerAuditor}, Kinds: []agentaudit.EventKind{agentaudit.EventApproval, agentaudit.EventWorkflowRun}},
		"auditor task+kind":   {Viewer: agentaudit.Viewer{TenantID: "tenant:a", UserID: "aud", Role: agentaudit.ViewerAuditor}, TaskID: "task:1", Kinds: []agentaudit.EventKind{agentaudit.EventIntentOrigin}},
		"user a":              {Viewer: agentaudit.Viewer{TenantID: "tenant:a", UserID: "user:a", Role: agentaudit.ViewerUser}},
		"user b":              {Viewer: agentaudit.Viewer{TenantID: "tenant:a", UserID: "user:b", Role: agentaudit.ViewerUser, AllowedFields: map[string]bool{"note": true}}},
		"user a task of b":    {Viewer: agentaudit.Viewer{TenantID: "tenant:a", UserID: "user:a", Role: agentaudit.ViewerUser}, TaskID: "task:2"},
		"user nobody":         {Viewer: agentaudit.Viewer{TenantID: "tenant:a", UserID: "user:z", Role: agentaudit.ViewerUser}},
		"other tenant":        {Viewer: agentaudit.Viewer{TenantID: "tenant:b", UserID: "aud", Role: agentaudit.ViewerAuditor}},
		"unknown tenant":      {Viewer: agentaudit.Viewer{TenantID: "tenant:none", UserID: "aud", Role: agentaudit.ViewerAuditor}},
		"user a in b":         {Viewer: agentaudit.Viewer{TenantID: "tenant:b", UserID: "user:a", Role: agentaudit.ViewerUser}},
		"user a kinds filter": {Viewer: agentaudit.Viewer{TenantID: "tenant:a", UserID: "user:a", Role: agentaudit.ViewerUser}, Kinds: []agentaudit.EventKind{agentaudit.EventSkillCall}},
	}
	nonEmpty := 0
	for name, q := range cases {
		got, err := f.store.Query(ctx, q)
		if err != nil {
			t.Fatalf("%s: durable Query: %v", name, err)
		}
		want, err := mem.Query(ctx, q)
		if err != nil {
			t.Fatalf("%s: memory Query: %v", name, err)
		}
		if mustJSON(t, got) != mustJSON(t, want) {
			t.Fatalf("%s: views differ\n got %s\nwant %s", name, mustJSON(t, got), mustJSON(t, want))
		}
		if len(got) > 0 {
			nonEmpty++
		}
	}
	if nonEmpty < 8 {
		t.Fatalf("only %d cases returned rows; parity table is not exercising the filters", nonEmpty)
	}
	// Spot-check expectations independent of the memory store.
	userA, _ := f.store.Query(ctx, cases["user a"])
	if len(userA) != 3 {
		t.Fatalf("user a sees %d records, want 3", len(userA))
	}
	for _, v := range userA {
		if v.Actor.UserID != "user:a" {
			t.Fatalf("user viewer saw %s's record", v.Actor.UserID)
		}
		for _, fv := range v.Fields {
			if fv.Name == "salary" && (!fv.Redacted || fv.Value != "" || fv.Digest == "") {
				t.Fatalf("salary not redacted: %+v", fv)
			}
			if fv.Name == "subject" && (fv.Redacted || fv.Value != "worker-7 <ok>") {
				t.Fatalf("public field mangled: %+v", fv)
			}
		}
	}
	allowed, _ := f.store.Query(ctx, cases["auditor allowed"])
	for _, fv := range allowed[0].Fields {
		if fv.Name == "salary" && (fv.Redacted || fv.Value != "125000") {
			t.Fatalf("allowed field still redacted: %+v", fv)
		}
	}
	if other, _ := f.store.Query(ctx, cases["other tenant"]); len(other) != 1 || other[0].EventID != "event:5" {
		t.Fatalf("tenant b viewer = %+v", other)
	}
}

func TestTodo_AGENT2_014_Race(t *testing.T) {
	f := newFixture(t, "tenant:a", "tenant:b")
	ctx := context.Background()
	const workers, each = 8, 5
	var wg sync.WaitGroup
	errs := make(chan error, workers*each+workers)
	for w := 0; w < workers; w++ {
		store, err := New(f.newConn(), f.mapper)
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func(w int, store *Store) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				tenant := "tenant:a"
				if i == 0 {
					tenant = "tenant:b"
				}
				if _, err := store.Append(ctx, testEntry(fmt.Sprintf("event:%d:%d", w, i), tenant, "user:a", "task:1", agentaudit.EventSkillCall)); err != nil {
					errs <- err
				}
			}
		}(w, store)
	}
	// All workers race to record the same event; exactly one may create it.
	created := make(chan bool, workers)
	for w := 0; w < workers; w++ {
		store, err := New(f.newConn(), f.mapper)
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func(store *Store) {
			defer wg.Done()
			rec, err := store.Append(ctx, testEntry("event:shared", "tenant:a", "user:a", "task:1", agentaudit.EventSkillCall))
			if err != nil {
				errs <- err
				return
			}
			created <- rec.Created
		}(store)
	}
	wg.Wait()
	close(errs)
	close(created)
	for err := range errs {
		t.Fatalf("concurrent Append: %v", err)
	}
	creators := 0
	for c := range created {
		if c {
			creators++
		}
	}
	if creators != 1 {
		t.Fatalf("%d goroutines created the shared event, want exactly 1", creators)
	}
	wantA := workers*(each-1) + 1
	views, err := f.store.Query(ctx, agentaudit.Query{Viewer: agentaudit.Viewer{TenantID: "tenant:a", UserID: "aud", Role: agentaudit.ViewerAuditor}})
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != wantA {
		t.Fatalf("tenant a has %d records, want %d", len(views), wantA)
	}
	for i, v := range views {
		if v.Sequence != uint64(i+1) {
			t.Fatalf("gap or duplicate: position %d has sequence %d", i, v.Sequence)
		}
	}
	if err := f.store.Verify(ctx, "tenant:a"); err != nil {
		t.Fatalf("Verify a: %v", err)
	}
	if err := f.store.Verify(ctx, "tenant:b"); err != nil {
		t.Fatalf("Verify b: %v", err)
	}
	var b int
	if err := f.db.SQL.QueryRow(`SELECT count(*) FROM agent_audit_record WHERE tenant_id=$1`, f.ids["tenant:b"]).Scan(&b); err != nil || b != workers {
		t.Fatalf("tenant b rows = %d, %v; want %d", b, err, workers)
	}
}

func TestTodo_AGENT2_014_RestartDurability(t *testing.T) {
	f := newFixture(t, "tenant:a")
	ctx := context.Background()
	first, err := f.store.Append(ctx, testEntry("event:1", "tenant:a", "user:a", "task:1", agentaudit.EventSkillCall))
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := New(f.newConn(), f.mapper)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Verify(ctx, "tenant:a"); err != nil {
		t.Fatalf("Verify after restart: %v", err)
	}
	retry, err := restarted.Append(ctx, testEntry("event:1", "tenant:a", "user:a", "task:1", agentaudit.EventSkillCall))
	if err != nil || retry.Created || retry.ChainHash != first.ChainHash {
		t.Fatalf("retry after restart = %+v, %v", retry, err)
	}
	next, err := restarted.Append(ctx, testEntry("event:2", "tenant:a", "user:a", "task:1", agentaudit.EventSkillCall))
	if err != nil || next.Sequence != 2 || next.PrevHash != first.ChainHash {
		t.Fatalf("append after restart = %+v, %v; want to extend the persisted chain", next, err)
	}
	if err := restarted.Verify(ctx, "tenant:a"); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_AGENT2_014_FindByEdge(t *testing.T) {
	f := newFixture(t, "tenant:a", "tenant:b")
	ctx := context.Background()
	for _, e := range []agentaudit.Entry{
		testEntry("event:1", "tenant:a", "user:a", "task:1", agentaudit.EventSkillCall),
		testEntry("event:2", "tenant:a", "user:a", "task:2", agentaudit.EventApproval),
		testEntry("event:3", "tenant:b", "user:a", "task:1", agentaudit.EventApproval),
	} {
		if _, err := f.store.Append(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := f.store.FindByEdge(ctx, "tenant:a", agentaudit.EdgeApproval, "", "approval:7")
	if err != nil || strings.Join(got, ",") != "event:1,event:2" {
		t.Fatalf("approval lookup = %v, %v", got, err)
	}
	got, err = f.store.FindByEdge(ctx, "tenant:a", agentaudit.EdgeApproval, "task:2", "")
	if err != nil || strings.Join(got, ",") != "event:2" {
		t.Fatalf("from lookup = %v, %v", got, err)
	}
	got, err = f.store.FindByEdge(ctx, "tenant:a", agentaudit.EdgeIntent, "task:1", "intent:event:1")
	if err != nil || strings.Join(got, ",") != "event:1" {
		t.Fatalf("intent lookup = %v, %v", got, err)
	}
	got, err = f.store.FindByEdge(ctx, "tenant:a", agentaudit.EdgeCaused, "", "")
	if err != nil || len(got) != 0 {
		t.Fatalf("absent kind = %v, %v", got, err)
	}
	if got, _ = f.store.FindByEdge(ctx, "tenant:b", agentaudit.EdgeApproval, "", ""); strings.Join(got, ",") != "event:3" {
		t.Fatalf("tenant b lookup = %v", got)
	}
}

func TestTodo_AGENT2_014_RLS(t *testing.T) {
	f := newFixture(t, "tenant:a", "tenant:b")
	ctx := context.Background()
	if _, err := f.store.Append(ctx, testEntry("event:1", "tenant:a", "user:a", "task:1", agentaudit.EventSkillCall)); err != nil {
		t.Fatal(err)
	}
	conn := f.newConn()
	if _, err := conn.Exec(ctx, `SELECT set_config('app.tenant_id', $1, false)`, f.ids["tenant:b"].String()); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM agent_audit_record`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("tenant b context sees %d tenant a rows, %v", n, err)
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM agent_audit_edge`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("tenant b context sees %d tenant a edge rows, %v", n, err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO agent_audit_record (tenant_id,sequence,event_id,kind,user_id,task_id,entry,prev_hash,chain_hash,recorded_at)
		VALUES ($1,1,'e','SKILL_CALL','u','t','{}'::jsonb,'','sha256:`+strings.Repeat("0", 64)+`',now())`, f.ids["tenant:a"]); err == nil {
		t.Fatal("tenant b context inserted a tenant a row")
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM agent_audit_record`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("count = %d, %v", n, err)
	}
}

func TestTodo_AGENT2_014_AppendOnly(t *testing.T) {
	f := newFixture(t, "tenant:a")
	ctx := context.Background()
	if _, err := f.store.Append(ctx, testEntry("event:1", "tenant:a", "user:a", "task:1", agentaudit.EventSkillCall)); err != nil {
		t.Fatal(err)
	}
	conn := f.newConn()
	if _, err := conn.Exec(ctx, `SELECT set_config('app.tenant_id', $1, false)`, f.ids["tenant:a"].String()); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`UPDATE agent_audit_record SET task_id = 'task:evil'`,
		`DELETE FROM agent_audit_record`,
		`UPDATE agent_audit_edge SET to_ref = 'x'`,
		`DELETE FROM agent_audit_edge`,
	} {
		if _, err := conn.Exec(ctx, stmt); err == nil {
			t.Fatalf("app role executed %q", stmt)
		}
	}
	// Even a superuser is stopped by the trigger.
	for _, stmt := range []string{
		`UPDATE agent_audit_record SET task_id = 'task:evil'`,
		`DELETE FROM agent_audit_record`,
		`UPDATE agent_audit_edge SET to_ref = 'x'`,
		`DELETE FROM agent_audit_edge`,
	} {
		if _, err := f.db.SQL.Exec(stmt); err == nil {
			t.Fatalf("admin connection executed %q despite forbid_mutation", stmt)
		}
	}
	if err := f.store.Verify(ctx, "tenant:a"); err != nil {
		t.Fatalf("chain changed by refused mutations: %v", err)
	}
}

func TestTodo_AGENT2_014_TamperDetection(t *testing.T) {
	ctx := context.Background()
	admin := func(f *fixture, stmts ...string) {
		t.Helper()
		for _, stmt := range stmts {
			if _, err := f.db.SQL.Exec(stmt); err != nil {
				t.Fatalf("admin %q: %v", stmt, err)
			}
		}
	}
	const off, on = `ALTER TABLE agent_audit_record DISABLE TRIGGER agent_audit_record_immutable`, `ALTER TABLE agent_audit_record ENABLE TRIGGER agent_audit_record_immutable`
	const edgeOff, edgeOn = `ALTER TABLE agent_audit_edge DISABLE TRIGGER agent_audit_edge_immutable`, `ALTER TABLE agent_audit_edge ENABLE TRIGGER agent_audit_edge_immutable`
	seed := func(f *fixture) {
		for i := 1; i <= 3; i++ {
			if _, err := f.store.Append(ctx, testEntry(fmt.Sprintf("event:%d", i), "tenant:a", "user:a", "task:1", agentaudit.EventSkillCall)); err != nil {
				t.Fatal(err)
			}
		}
		if err := f.store.Verify(ctx, "tenant:a"); err != nil {
			t.Fatalf("untampered chain must verify: %v", err)
		}
	}
	cases := map[string]func(f *fixture){
		"entry content edited": func(f *fixture) {
			admin(f, off, `UPDATE agent_audit_record SET entry = jsonb_set(entry, '{Action}', '"people.delete"') WHERE sequence = 2`, on)
		},
		"chain hash replaced": func(f *fixture) {
			admin(f, off, `UPDATE agent_audit_record SET chain_hash = 'sha256:`+strings.Repeat("a", 64)+`' WHERE sequence = 3`, on)
		},
		"prev hash relinked": func(f *fixture) {
			admin(f, off, `UPDATE agent_audit_record SET prev_hash = 'sha256:`+strings.Repeat("b", 64)+`' WHERE sequence = 2`, on)
		},
		"row deleted from middle": func(f *fixture) {
			admin(f, edgeOff, `DELETE FROM agent_audit_edge WHERE sequence = 2`, edgeOn, off, `DELETE FROM agent_audit_record WHERE sequence = 2`, on)
		},
		"indexed column disagrees": func(f *fixture) {
			admin(f, off, `UPDATE agent_audit_record SET user_id = 'user:evil' WHERE sequence = 1`, on)
		},
		"entry no longer decodes": func(f *fixture) {
			admin(f, off, `UPDATE agent_audit_record SET entry = '{"Actor": "bad"}'::jsonb WHERE sequence = 1`, on)
		},
	}
	for name, tamper := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, "tenant:a")
			seed(f)
			tamper(f)
			if err := f.store.Verify(ctx, "tenant:a"); !errors.Is(err, agentaudit.ErrChainTampered) {
				t.Fatalf("Verify err = %v, want ErrChainTampered", err)
			}
		})
	}
}
