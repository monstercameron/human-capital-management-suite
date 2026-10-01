package agentrunstore

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var t0 = time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)

type env struct {
	db     *pgtest.DB
	one    uuid.UUID
	two    uuid.UUID
	mapper func(values.TenantId) uuid.UUID
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db := pgtest.New(t)
	e := &env{db: db, one: uuid.New(), two: uuid.New()}
	for _, item := range []struct {
		id  uuid.UUID
		key string
	}{{e.one, "run-one"}, {e.two, "run-two"}} {
		db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, item.id, item.key, item.key)
	}
	e.mapper = func(key values.TenantId) uuid.UUID {
		switch key {
		case "run-one":
			return e.one
		case "run-two":
			return e.two
		}
		return uuid.Nil
	}
	return e
}

// store opens a store on its own connection running as the RLS-bound app role.
func (e *env) store(t *testing.T) *Store {
	t.Helper()
	conn := e.db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume app role: %v", err)
	}
	s, err := New(conn, e.mapper)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (e *env) tenantStore(t *testing.T, tenant string) *TenantStore {
	t.Helper()
	ts, err := e.store(t).Scoped(values.TenantId(tenant))
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func testPlan(t *testing.T) agentrun.AgentPlan {
	t.Helper()
	plan, err := agentrun.NewPlan([]agentrun.PlanStep{
		{ID: "read", Type: agentrun.StepRead, SkillID: "skill.read", SkillVersion: 1, ExpectedOutput: "facts", Tier: agentrun.TierRead,
			Inputs: []agentrun.InputRef{{Name: "doc", Ref: "doc:1", SourceID: "src-1", Taint: []string{"EXTERNAL", "UNTRUSTED"}}}},
		{ID: "send", Type: agentrun.StepCommunicate, SkillID: "skill.send", SkillVersion: 2, ExpectedOutput: "sent", Tier: agentrun.TierCommunicate,
			Destination: "mailto:a@example.com", DestinationConfirmed: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func newRuntimeTask(t *testing.T, store agentrun.TaskStore, tenant, id string) (*agentrun.Runtime, agentrun.AgentTask) {
	t.Helper()
	rt, err := agentrun.NewRuntime(store)
	if err != nil {
		t.Fatal(err)
	}
	task, err := rt.CreateTask(context.Background(), agentrun.CreateRequest{ID: id, TenantID: tenant, UserID: "user-1", Goal: "reconcile the ledger",
		Constraints: []string{"no external sends", "stay under budget"}, Plan: testPlan(t), Now: t0, ExpiresAt: t0.Add(48 * time.Hour)})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return rt, task
}

// TestTodo_AGENT2_010_RoundTrip drives the real runtime over the store and
// requires every stored revision to read back deep-equal, including ledger
// taint labels, revoked flags, the confirmed plan digest and wake conditions.
func TestTodo_AGENT2_010_RoundTrip(t *testing.T) {
	e := newEnv(t)
	store := e.tenantStore(t, "run-one")
	ctx := context.Background()
	rt, created := newRuntimeTask(t, store, "run-one", "task-1")

	same := func(step string, want agentrun.AgentTask) {
		t.Helper()
		got, err := store.Get(ctx, want.ID)
		if err != nil {
			t.Fatalf("%s: Get: %v", step, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: round trip differs\n got: %+v\nwant: %+v", step, got, want)
		}
	}
	same("created", created)
	if len(created.Ledger.Entries) != 3 || created.Ledger.Entries[0].Taint[0] != "USER_AUTHORED" {
		t.Fatalf("fixture ledger = %+v", created.Ledger.Entries)
	}

	confirmed, err := rt.ConfirmPlan(ctx, "task-1", "user-1", 1, t0.Add(time.Minute))
	if err != nil || !confirmed.Plan.Confirmed || confirmed.Plan.Digest == "" {
		t.Fatalf("ConfirmPlan = %+v, %v", confirmed, err)
	}
	same("confirmed", confirmed)

	noted, err := rt.AddModelNote(ctx, "task-1", confirmed.Version, "note:1", "sha256:aa", t0.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	same("model note", noted)
	noted.Ledger.AnswerText = "sanitized answer"
	noted.Version++
	if err := store.Save(ctx, noted, noted.Version-1); err != nil {
		t.Fatalf("save answer text: %v", err)
	}
	same("answer text", noted)
	// a source revocation must survive the trip as Revoked=true.
	withSource := noted
	withSource.Ledger.Entries = append(withSource.Ledger.Entries, agentrun.LedgerEntry{Sequence: 5, Kind: "STEP_RESULT", Ref: "res:1", SourceID: "src-9", Taint: []string{"EXTERNAL"}})
	withSource.Version++
	if err := store.Save(ctx, withSource, noted.Version); err != nil {
		t.Fatal(err)
	}
	revoked, err := rt.RevokeSource(ctx, "task-1", "src-9", withSource.Version, t0.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	same("revoked", revoked)
	got, _ := store.Get(ctx, "task-1")
	if last := got.Ledger.Entries[len(got.Ledger.Entries)-1]; !last.Revoked || last.SourceID != "src-9" || len(last.Taint) != 1 || last.Taint[0] != "EXTERNAL" {
		t.Fatalf("revoked entry = %+v", last)
	}

	// Park with every wake field set, then pause and resume: both wake copies must survive.
	cond := agentrun.WakeCondition{Kind: agentrun.WakePolling, Key: "poll-key", Correlation: "corr-1", DueAt: t0.Add(time.Hour), PollAfter: 90 * time.Second, StaleAfter: t0.Add(2 * time.Hour)}
	parked, err := rt.Park(ctx, "task-1", revoked.Version, cond, t0.Add(4*time.Minute))
	if err != nil || parked.Wake == nil {
		t.Fatalf("Park = %+v, %v", parked, err)
	}
	same("parked", parked)
	paused, err := rt.Pause(ctx, "task-1", parked.Version, t0.Add(5*time.Minute))
	if err != nil || paused.PausedWake == nil || paused.PausedState != agentrun.StateWaiting {
		t.Fatalf("Pause = %+v, %v", paused, err)
	}
	same("paused", paused)
	resumed, err := rt.Resume(ctx, "task-1", paused.Version, t0.Add(6*time.Minute))
	if err != nil || !reflect.DeepEqual(resumed.Wake, &cond) {
		t.Fatalf("Resume = %+v, %v", resumed, err)
	}
	same("resumed", resumed)

	// A non-UTC task time reads back normalised to the same instant in UTC.
	zone := time.FixedZone("plus5", 5*3600)
	moved := resumed
	moved.Version++
	moved.UpdatedAt = t0.Add(7 * time.Minute).In(zone)
	moved.Plan.ConfirmedAt = t0.In(zone)
	if err := store.Save(ctx, moved, resumed.Version); err != nil {
		t.Fatal(err)
	}
	moved.UpdatedAt, moved.Plan.ConfirmedAt = moved.UpdatedAt.UTC(), moved.Plan.ConfirmedAt.UTC()
	same("zone normalised", moved)

	// A failed task keeps its failure fields.
	failed := moved
	failed.Version++
	failed.State, failed.FailureCode, failed.FailureDetail, failed.Wake = agentrun.StateFailed, "STEP_FAILED", "boom", nil
	if err := store.Save(ctx, failed, moved.Version); err != nil {
		t.Fatal(err)
	}
	same("failed", failed)
}

func TestTodo_AGENT2_010_StoreSemantics(t *testing.T) {
	e := newEnv(t)
	store := e.tenantStore(t, "run-one")
	ctx := context.Background()
	_, task := newRuntimeTask(t, store, "run-one", "task-a")

	if err := store.Create(ctx, task); !errors.Is(err, agentrun.ErrConflict) {
		t.Fatalf("duplicate Create = %v, want ErrConflict", err)
	}
	if _, err := store.Get(ctx, "missing"); !errors.Is(err, agentrun.ErrNotFound) {
		t.Fatalf("Get missing = %v", err)
	}
	next := task
	next.Version = 2
	if err := store.Save(ctx, agentrun.AgentTask{ID: "missing", TenantID: "run-one", UserID: "u", State: agentrun.StateRunning, Version: 2}, 1); !errors.Is(err, agentrun.ErrNotFound) {
		t.Fatalf("Save missing = %v", err)
	}
	if err := store.Save(ctx, next, 5); !errors.Is(err, agentrun.ErrConflict) {
		t.Fatalf("Save stale expected = %v, want ErrConflict", err)
	}
	skip := task
	skip.Version = 9
	if err := store.Save(ctx, skip, 1); !errors.Is(err, agentrun.ErrInvalid) {
		t.Fatalf("Save version skip = %v, want ErrInvalid", err)
	}
	if got, _ := store.Get(ctx, "task-a"); got.Version != 1 {
		t.Fatalf("rejected saves changed the task: version %d", got.Version)
	}
	if err := store.Save(ctx, next, 1); err != nil {
		t.Fatalf("Save ok: %v", err)
	}
	if err := store.Save(ctx, next, 1); !errors.Is(err, agentrun.ErrConflict) {
		t.Fatalf("replayed Save = %v, want ErrConflict", err)
	}

	// List is ordered by id in byte order like MemoryStore.
	for _, id := range []string{"task-B", "task-Z", "task-b"} {
		clone := task
		clone.ID = id
		if err := store.Create(ctx, clone); err != nil {
			t.Fatal(err)
		}
	}
	list, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, item := range list {
		ids = append(ids, item.ID)
	}
	if want := []string{"task-B", "task-Z", "task-a", "task-b"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("List order = %v, want %v", ids, want)
	}

	// The runtime reports the same sentinels through the store.
	rt, _ := agentrun.NewRuntime(store)
	if _, err := rt.ConfirmPlan(ctx, "task-a", "user-1", 1, t0); !errors.Is(err, agentrun.ErrConflict) {
		t.Fatalf("runtime stale ConfirmPlan = %v", err)
	}
}

func TestTodo_AGENT2_010_Validation(t *testing.T) {
	e := newEnv(t)
	store := e.tenantStore(t, "run-one")
	ctx := context.Background()
	good := agentrun.AgentTask{ID: "t", TenantID: "run-one", UserID: "u", State: agentrun.StateRunning, Version: 1, Plan: agentrun.AgentPlan{Digest: "d"}}
	for name, mutate := range map[string]func(*agentrun.AgentTask){
		"empty id":         func(x *agentrun.AgentTask) { x.ID = " " },
		"empty user":       func(x *agentrun.AgentTask) { x.UserID = "" },
		"unknown state":    func(x *agentrun.AgentTask) { x.State = "BOGUS" },
		"unknown paused":   func(x *agentrun.AgentTask) { x.PausedState = "BOGUS" },
		"zero version":     func(x *agentrun.AgentTask) { x.Version = 0 },
		"huge version":     func(x *agentrun.AgentTask) { x.Version = 1 << 63 },
		"negative cursor":  func(x *agentrun.AgentTask) { x.CurrentStep = -1 },
		"bad wake":         func(x *agentrun.AgentTask) { x.Wake = &agentrun.WakeCondition{Kind: "NOPE", Key: "k"} },
		"bad paused wake":  func(x *agentrun.AgentTask) { x.PausedWake = &agentrun.WakeCondition{Kind: agentrun.WakeSignal} },
		"foreign tenant":   func(x *agentrun.AgentTask) { x.TenantID = "run-two" },
		"blank tenant ref": func(x *agentrun.AgentTask) { x.TenantID = "" },
	} {
		task := good
		mutate(&task)
		err := store.Create(ctx, task)
		if !errors.Is(err, agentrun.ErrInvalid) {
			t.Fatalf("%s: Create = %v, want ErrInvalid", name, err)
		}
		if name == "foreign tenant" && !errors.Is(err, ErrTenantMismatch) {
			t.Fatalf("foreign tenant error = %v, want ErrTenantMismatch", err)
		}
	}
	if err := store.Save(ctx, agentrun.AgentTask{ID: "t", TenantID: "run-two", UserID: "u", State: agentrun.StateRunning, Version: 2}, 1); !errors.Is(err, ErrTenantMismatch) {
		t.Fatalf("Save foreign tenant = %v", err)
	}
	if list, _ := store.List(ctx); len(list) != 0 {
		t.Fatalf("refused tasks were stored: %+v", list)
	}
	// Storage-level CHECKs back the validation: a raw insert with a bad state is refused.
	conn := e.db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	tx, _ := conn.Begin(ctx)
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, e.one); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO agent_task (tenant_id,task_id,tenant_ref,user_id,goal,constraints,plan,plan_digest,plan_confirmed,state,version,current_step,ledger,created_at,updated_at,expires_at)
		VALUES ($1,'x','run-one','u','g','[]','{}','d',false,'BOGUS',1,0,'{}',now(),now(),now())`, e.one); err == nil {
		t.Fatal("CHECK on state accepted BOGUS")
	}
}

func TestTodo_AGENT2_011_CrossTenantIsolation(t *testing.T) {
	e := newEnv(t)
	one := e.tenantStore(t, "run-one")
	two := e.tenantStore(t, "run-two")
	ctx := context.Background()
	_, task := newRuntimeTask(t, one, "run-one", "shared-id")

	if _, err := two.Get(ctx, "shared-id"); !errors.Is(err, agentrun.ErrNotFound) {
		t.Fatalf("tenant two Get of tenant one task = %v, want ErrNotFound", err)
	}
	list, err := two.List(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("tenant two List = %+v, %v", list, err)
	}
	next := task
	next.Version = 2
	next.Goal = "hijacked"
	if err := two.Save(ctx, next, 1); !errors.Is(err, ErrTenantMismatch) {
		t.Fatalf("tenant two Save of tenant one task = %v, want ErrTenantMismatch", err)
	}
	// Even relabelled as tenant two, the row of tenant one is unreachable.
	next.TenantID = "run-two"
	if err := two.Save(ctx, next, 1); !errors.Is(err, agentrun.ErrNotFound) {
		t.Fatalf("relabelled cross-tenant Save = %v, want ErrNotFound", err)
	}
	if err := two.Create(ctx, task); !errors.Is(err, ErrTenantMismatch) {
		t.Fatalf("tenant two Create of tenant one task = %v", err)
	}
	if _, err := two.ClaimWake(ctx, "shared-id", agentrun.WakeEvent{ID: "e1", Kind: agentrun.WakeSignal, Key: "k", OccurredAt: t0}); !errors.Is(err, agentrun.ErrNotFound) {
		t.Fatalf("tenant two ClaimWake = %v, want ErrNotFound", err)
	}
	if got, err := one.Get(ctx, "shared-id"); err != nil || got.Goal != task.Goal || got.Version != 1 {
		t.Fatalf("tenant one task changed: %+v, %v", got, err)
	}
	// The same id is independent per tenant.
	twin := task
	twin.TenantID = "run-two"
	if err := two.Create(ctx, twin); err != nil {
		t.Fatalf("tenant two Create with same id: %v", err)
	}
	if list, _ := one.List(ctx); len(list) != 1 || list[0].TenantID != "run-one" {
		t.Fatalf("tenant one List = %+v", list)
	}
	// Raw SQL under the app role sees nothing without a tenant binding.
	var n int
	if err := rawConn(t, e).QueryRow(ctx, `SELECT count(*) FROM agent_task`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("unbound app-role count = %d, %v; want 0", n, err)
	}
	if _, err := e.store(t).ForTenant(ctx, "run-unknown"); !errors.Is(err, agentrun.ErrInvalid) {
		t.Fatalf("ForTenant unknown tenant = %v", err)
	}
}

// TestTodo_AGENT2_011_Race proves two workers saving from the same version
// cannot both win, across independent connections.
func TestTodo_AGENT2_011_Race(t *testing.T) {
	e := newEnv(t)
	seed := e.tenantStore(t, "run-one")
	ctx := context.Background()
	_, task := newRuntimeTask(t, seed, "run-one", "race-task")

	const workers = 8
	stores := make([]*TenantStore, workers)
	for i := range stores {
		stores[i] = e.tenantStore(t, "run-one")
	}
	results := make(chan error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, s := range stores {
		wg.Add(1)
		go func() {
			defer wg.Done()
			next := task
			next.Version = 2
			next.Goal = "winner-" + string(rune('a'+i))
			<-start
			results <- s.Save(ctx, next, 1)
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, agentrun.ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected race error: %v", err)
		}
	}
	if wins != 1 || conflicts != workers-1 {
		t.Fatalf("race wins=%d conflicts=%d, want 1 and %d", wins, conflicts, workers-1)
	}
	got, _ := seed.Get(ctx, "race-task")
	if got.Version != 2 || got.Goal[:7] != "winner-" {
		t.Fatalf("stored task = version %d goal %q", got.Version, got.Goal)
	}
}

func parkedSignalTask(t *testing.T, store agentrun.TaskStore, id string) agentrun.AgentTask {
	t.Helper()
	rt, _ := newRuntimeTask(t, store, "run-one", id)
	ctx := context.Background()
	confirmed, err := rt.ConfirmPlan(ctx, id, "user-1", 1, t0)
	if err != nil {
		t.Fatal(err)
	}
	parked, err := rt.Park(ctx, id, confirmed.Version, agentrun.WakeCondition{Kind: agentrun.WakeSignal, Key: "inbound", Correlation: "thread-1"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return parked
}

func receiptCount(t *testing.T, e *env, taskID string) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_wake_receipt WHERE task_id=$1`, taskID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestTodo_AGENT2_013_WakeDedupe proves ClaimWake is atomic: the same event
// delivered concurrently resumes the task exactly once.
func TestTodo_AGENT2_013_WakeDedupe(t *testing.T) {
	e := newEnv(t)
	seed := e.tenantStore(t, "run-one")
	ctx := context.Background()
	parked := parkedSignalTask(t, seed, "wake-task")
	event := agentrun.WakeEvent{ID: "evt-1", Kind: agentrun.WakeSignal, Key: "inbound", Correlation: "thread-1", PayloadRef: "payload:1", OccurredAt: t0.Add(time.Minute)}

	const workers = 8
	results := make(chan agentrun.WakeResult, workers)
	errs := make(chan error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		s := e.tenantStore(t, "run-one")
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res, err := s.ClaimWake(ctx, "wake-task", event)
			if err != nil {
				errs <- err
				return
			}
			results <- res
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("ClaimWake: %v", err)
	}
	accepted, duplicates := 0, 0
	for res := range results {
		if res.Accepted {
			accepted++
			if res.Task.State != agentrun.StateRunning || res.Task.Wake != nil || res.Task.Version != parked.Version+1 {
				t.Fatalf("accepted task = %+v", res.Task)
			}
		}
		if res.Duplicate {
			duplicates++
			if res.Accepted || res.Ignored {
				t.Fatalf("duplicate flagged accepted/ignored: %+v", res)
			}
		}
	}
	if accepted != 1 || duplicates != workers-1 {
		t.Fatalf("accepted=%d duplicates=%d, want 1 and %d", accepted, duplicates, workers-1)
	}
	got, _ := seed.Get(ctx, "wake-task")
	if got.State != agentrun.StateRunning || got.Wake != nil || got.Version != parked.Version+1 || !got.UpdatedAt.Equal(event.OccurredAt) {
		t.Fatalf("stored task after wake = %+v", got)
	}
	if n := receiptCount(t, e, "wake-task"); n != 1 {
		t.Fatalf("receipts = %d, want 1", n)
	}
	// Redelivery after the fact is still a duplicate and changes nothing.
	again, err := seed.ClaimWake(ctx, "wake-task", event)
	if err != nil || !again.Duplicate || again.Task.Version != got.Version {
		t.Fatalf("redelivery = %+v, %v", again, err)
	}
}

func TestTodo_AGENT2_013_WakeRules(t *testing.T) {
	e := newEnv(t)
	store := e.tenantStore(t, "run-one")
	ctx := context.Background()
	parked := parkedSignalTask(t, store, "rules-task")
	claim := func(ev agentrun.WakeEvent) agentrun.WakeResult {
		t.Helper()
		res, err := store.ClaimWake(ctx, "rules-task", ev)
		if err != nil {
			t.Fatalf("ClaimWake %s: %v", ev.ID, err)
		}
		return res
	}
	for id, ev := range map[string]agentrun.WakeEvent{
		"wrong key":  {ID: "w1", Kind: agentrun.WakeSignal, Key: "other", Correlation: "thread-1", OccurredAt: t0},
		"wrong kind": {ID: "w2", Kind: agentrun.WakeTimer, Key: "inbound", Correlation: "thread-1", OccurredAt: t0},
		"wrong corr": {ID: "w3", Kind: agentrun.WakeSignal, Key: "inbound", Correlation: "thread-2", OccurredAt: t0},
	} {
		res := claim(ev)
		if !res.Ignored || res.Accepted || res.Duplicate || res.Task.State != agentrun.StateWaiting || res.Task.Version != parked.Version {
			t.Fatalf("%s: result = %+v", id, res)
		}
	}
	// An ignored event still counts as delivered: its redelivery is a duplicate.
	if res := claim(agentrun.WakeEvent{ID: "w1", Kind: agentrun.WakeSignal, Key: "inbound", Correlation: "thread-1", OccurredAt: t0}); !res.Duplicate || res.Accepted {
		t.Fatalf("redelivered ignored id must dedupe, got %+v", res)
	}
	for name, ev := range map[string]agentrun.WakeEvent{
		"no id":   {Kind: agentrun.WakeSignal, Key: "inbound"},
		"no key":  {ID: "x", Kind: agentrun.WakeSignal},
		"no kind": {ID: "x", Key: "inbound"},
	} {
		if _, err := store.ClaimWake(ctx, "rules-task", ev); !errors.Is(err, agentrun.ErrInvalidWake) {
			t.Fatalf("%s: err = %v, want ErrInvalidWake", name, err)
		}
	}
	if _, err := store.ClaimWake(ctx, "nope", agentrun.WakeEvent{ID: "x", Kind: agentrun.WakeSignal, Key: "k", OccurredAt: t0}); !errors.Is(err, agentrun.ErrNotFound) {
		t.Fatalf("ClaimWake missing task = %v", err)
	}

	// A paused task ignores the matching event; resuming restores the wake.
	rt, _ := agentrun.NewRuntime(store)
	paused, err := rt.Pause(ctx, "rules-task", parked.Version, t0)
	if err != nil {
		t.Fatal(err)
	}
	if res := claim(agentrun.WakeEvent{ID: "p1", Kind: agentrun.WakeSignal, Key: "inbound", Correlation: "thread-1", OccurredAt: t0}); !res.Ignored || res.Task.Version != paused.Version {
		t.Fatalf("paused task claim = %+v", res)
	}
	resumed, err := rt.Resume(ctx, "rules-task", paused.Version, t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimWake(ctx, "rules-task", agentrun.WakeEvent{ID: "zero-time", Kind: agentrun.WakeSignal, Key: "inbound"}); !errors.Is(err, agentrun.ErrInvalidWake) {
		t.Fatalf("zero-time wake was accepted: %v", err)
	}
	event := agentrun.WakeEvent{ID: "ok", Kind: agentrun.WakeSignal, Key: "inbound", Correlation: "thread-1", PayloadRef: "source:result", OccurredAt: t0.Add(time.Minute)}
	res := claim(event)
	if !res.Accepted || !res.Task.UpdatedAt.Equal(event.OccurredAt) || res.Task.State != agentrun.StateRunning {
		t.Fatalf("accepted claim = %+v", res)
	}
	if resumed.State != agentrun.StateWaiting {
		t.Fatalf("resume lost parked state: %+v", resumed)
	}
	restored, err := e.tenantStore(t, "run-one").Get(ctx, "rules-task")
	if err != nil || restored.LastWake == nil || !reflect.DeepEqual(*restored.LastWake, event) {
		t.Fatalf("restart lost exact accepted wake: %+v err=%v", restored.LastWake, err)
	}
	// Terminal tasks ignore wake events.
	cancelled, err := rt.Cancel(ctx, "rules-task", res.Task.Version, t0)
	if err != nil {
		t.Fatal(err)
	}
	if res := claim(agentrun.WakeEvent{ID: "t1", Kind: agentrun.WakeSignal, Key: "inbound", OccurredAt: t0}); !res.Ignored || res.Task.State != cancelled.State {
		t.Fatalf("terminal claim = %+v", res)
	}
	// Receipts are append-only: the trigger refuses UPDATE and DELETE even for the owner.
	if err := e.db.ExecErr(`UPDATE agent_task_wake_receipt SET outcome='IGNORED' WHERE task_id='rules-task'`); err == nil {
		t.Fatal("receipt UPDATE was allowed")
	}
	if err := e.db.ExecErr(`DELETE FROM agent_task_wake_receipt WHERE task_id='rules-task'`); err == nil {
		t.Fatal("receipt DELETE was allowed")
	}
}

// TestTodo_AGENT2_011_RestartDurability replaces every in-memory object and
// checks a parked task, its wake and the sweep behave as before the restart.
func TestTodo_AGENT2_011_RestartDurability(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	first := e.tenantStore(t, "run-one")
	rt, _ := newRuntimeTask(t, first, "run-one", "durable")
	confirmed, err := rt.ConfirmPlan(ctx, "durable", "user-1", 1, t0)
	if err != nil {
		t.Fatal(err)
	}
	parked, err := rt.Park(ctx, "durable", confirmed.Version, agentrun.WakeCondition{Kind: agentrun.WakeTimer, Key: "timer-1", DueAt: t0.Add(time.Hour), StaleAfter: t0.Add(3 * time.Hour)}, t0)
	if err != nil {
		t.Fatal(err)
	}

	second := e.tenantStore(t, "run-one") // "restarted process": new connection, new store
	got, err := second.Get(ctx, "durable")
	if err != nil || !reflect.DeepEqual(got, parked) {
		t.Fatalf("after restart: %+v, %v; want %+v", got, err, parked)
	}
	rt2, _ := agentrun.NewRuntime(second)
	// Not yet due: nothing to do.
	if due, err := second.DueWaits(ctx, t0.Add(30*time.Minute)); err != nil || len(due) != 0 {
		t.Fatalf("early DueWaits = %+v, %v", due, err)
	}
	if swept, err := rt2.SweepStaleTasks(ctx, t0.Add(30*time.Minute)); err != nil || len(swept) != 0 {
		t.Fatalf("early sweep = %+v, %v", swept, err)
	}
	// Timer due: reported by DueWaits, and the timer wake resumes it.
	due, err := second.DueWaits(ctx, t0.Add(time.Hour))
	if err != nil || len(due) != 1 || due[0].ID != "durable" {
		t.Fatalf("DueWaits at timer = %+v, %v", due, err)
	}
	woke, err := rt2.Wake(ctx, "durable", agentrun.WakeEvent{ID: "timer-fire", Kind: agentrun.WakeTimer, Key: "timer-1", OccurredAt: t0.Add(time.Hour)})
	if err != nil || !woke.Accepted {
		t.Fatalf("timer wake = %+v, %v", woke, err)
	}
	if due, _ := second.DueWaits(ctx, t0.Add(time.Hour)); len(due) != 0 {
		t.Fatalf("woken task still due: %+v", due)
	}

	// A lost wake is found by its stale_after and failed by the sweep.
	lost, err := rt2.Park(ctx, "durable", woke.Task.Version, agentrun.WakeCondition{Kind: agentrun.WakeSignal, Key: "never", StaleAfter: t0.Add(2 * time.Hour)}, t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if due, _ := second.DueWaits(ctx, t0.Add(2*time.Hour)); len(due) != 1 {
		t.Fatalf("stale wait not due: %+v", due)
	}
	swept, err := rt2.SweepStaleTasks(ctx, t0.Add(2*time.Hour))
	if err != nil || len(swept) != 1 || swept[0].State != agentrun.StateFailed || swept[0].FailureCode != "LOST_WAKE" || swept[0].Version != lost.Version+1 {
		t.Fatalf("sweep = %+v, %v", swept, err)
	}

	// An open task past its lifetime expires; a completed task past it does not show as due.
	_, open := newRuntimeTask(t, second, "run-one", "aging")
	if due, _ := second.DueWaits(ctx, open.ExpiresAt.Add(time.Second)); len(due) != 1 || due[0].ID != "aging" {
		t.Fatalf("expiry DueWaits = %+v", due)
	}
	swept, err = rt2.SweepStaleTasks(ctx, open.ExpiresAt)
	if err != nil || len(swept) != 1 || swept[0].State != agentrun.StateExpired {
		t.Fatalf("expiry sweep = %+v, %v", swept, err)
	}
	// A restart preserves the expired state too.
	third := e.tenantStore(t, "run-one")
	final, _ := third.Get(ctx, "aging")
	if final.State != agentrun.StateExpired || final.FailureCode != "TASK_EXPIRED" {
		t.Fatalf("after second restart: %+v", final)
	}
}

type failingDB struct{ err error }

func (f failingDB) Begin(context.Context) (dbport.Tx, error) { return nil, f.err }

func TestTodo_AGENT2_010_ConstructorsAndDBFailures(t *testing.T) {
	if _, err := New(nil, nil); !errors.Is(err, agentrun.ErrInvalid) {
		t.Fatalf("New(nil) = %v", err)
	}
	mapper := func(values.TenantId) uuid.UUID { return uuid.New() }
	if _, err := New(failingDB{}, nil); err == nil {
		t.Fatal("New without mapper succeeded")
	}
	var nilStore *Store
	if _, err := nilStore.ForTenant(context.Background(), "t"); !errors.Is(err, agentrun.ErrInvalid) {
		t.Fatalf("nil store ForTenant = %v", err)
	}
	s, err := New(failingDB{err: errors.New("db down")}, mapper)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []values.TenantId{"", " padded "} {
		if _, err := s.ForTenant(context.Background(), bad); !errors.Is(err, agentrun.ErrInvalid) {
			t.Fatalf("ForTenant(%q) = %v", bad, err)
		}
	}
	ts, err := s.Scoped("t")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	task := agentrun.AgentTask{ID: "a", TenantID: "t", UserID: "u", State: agentrun.StateRunning, Version: 1}
	if err := ts.Create(ctx, task); err == nil {
		t.Fatalf("Create on failing db = %v", err)
	}
	if _, err := ts.Get(ctx, "a"); err == nil {
		t.Fatal("Get on failing db succeeded")
	}
	task.Version = 2
	if err := ts.Save(ctx, task, 1); err == nil {
		t.Fatal("Save on failing db succeeded")
	}
	if _, err := ts.ClaimWake(ctx, "a", agentrun.WakeEvent{ID: "e", Kind: agentrun.WakeSignal, Key: "k"}); err == nil {
		t.Fatal("ClaimWake on failing db succeeded")
	}
	if _, err := ts.List(ctx); err == nil {
		t.Fatal("List on failing db succeeded")
	}
	if _, err := ts.DueWaits(ctx, t0); err == nil {
		t.Fatal("DueWaits on failing db succeeded")
	}
}

func rawConn(t *testing.T, e *env) dbport.Conn {
	t.Helper()
	conn := e.db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	return conn
}
