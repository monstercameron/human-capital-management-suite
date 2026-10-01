package agentbudgetstore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type env struct {
	db      *pgtest.DB
	tenants map[string]uuid.UUID
}

func newEnv(t *testing.T, keys ...string) *env {
	t.Helper()
	e := &env{db: pgtest.New(t), tenants: map[string]uuid.UUID{}}
	for _, key := range keys {
		id := uuid.New()
		e.tenants[key] = id
		e.db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, key)
	}
	return e
}

func (e *env) mapper(key values.TenantId) uuid.UUID { return e.tenants[string(key)] }

// conn returns a fresh connection running as the RLS-bound application role.
func (e *env) conn(t *testing.T) *pgxadapter.Conn {
	t.Helper()
	c := e.db.NewConn(t)
	if _, err := c.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume app role: %v", err)
	}
	return c
}

// store opens a Store on its own connection: a "process".
func (e *env) store(t *testing.T) *Store {
	t.Helper()
	s, err := New(e.conn(t), e.mapper)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) now() time.Time  { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *clock) set(t time.Time) { c.mu.Lock(); c.at = t; c.mu.Unlock() }

func policy() agentbudget.Policy {
	return agentbudget.Policy{
		TaskDefault:     agentbudget.Limits{Steps: 50, Tokens: 100_000, WallClock: time.Hour, SpendMicros: 100_000},
		UserDaily:       agentbudget.Limits{Steps: 1_000, Tokens: 1_000_000, WallClock: 24 * time.Hour, SpendMicros: 1_000_000},
		TenantMonthly:   agentbudget.Limits{Steps: 100_000, Tokens: 100_000_000, WallClock: 720 * time.Hour, SpendMicros: 100_000_000},
		ExtensionPolicy: agentbudget.ExtensionPolicy{MaxAdditional: agentbudget.Limits{Steps: 100_000, Tokens: 100_000_000, WallClock: 720 * time.Hour, SpendMicros: 100_000_000}},
	}
}

func call() agentbudget.Usage {
	return agentbudget.Usage{Steps: 1, Tokens: 100, WallClock: time.Second, SpendMicros: 100}
}

// boot builds a ledger over a fresh store on its own connection and restores
// the tenant's durable state into it, exactly as a restarted process would.
func boot(t *testing.T, e *env, p agentbudget.Policy, c *clock, tenants ...string) *agentbudget.Ledger {
	t.Helper()
	s := e.store(t)
	ledger, err := agentbudget.NewWithPersistence(p, c.now, s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tenant := range tenants {
		state, err := s.Load(context.Background(), values.TenantId(tenant), c.now())
		if err != nil {
			t.Fatalf("Load %s: %v", tenant, err)
		}
		if err := ledger.Restore(state); err != nil {
			t.Fatalf("Restore %s: %v", tenant, err)
		}
	}
	return ledger
}

func open(t *testing.T, l *agentbudget.Ledger, id, tenant, user string, limit agentbudget.Limits) {
	t.Helper()
	if err := l.OpenTask(agentbudget.TaskSpec{ID: id, TenantID: tenant, UserID: user, Limit: limit}); err != nil {
		t.Fatalf("OpenTask %s: %v", id, err)
	}
}

func reserve(l *agentbudget.Ledger, task, step, fp string, est agentbudget.Usage) (*agentbudget.Reservation, error) {
	return l.Reserve(context.Background(), agentbudget.Request{TaskID: task, StepID: step, Fingerprint: fp, Estimate: est})
}

func mustReserve(t *testing.T, l *agentbudget.Ledger, task, step, fp string) *agentbudget.Reservation {
	t.Helper()
	r, err := reserve(l, task, step, fp, call())
	if err != nil {
		t.Fatalf("Reserve %s/%s: %v", task, step, err)
	}
	return r
}

func taskSnap(t *testing.T, l *agentbudget.Ledger, id string) agentbudget.TaskSnapshot {
	t.Helper()
	for _, s := range l.Snapshot().Tasks {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("task %s not in snapshot", id)
	return agentbudget.TaskSnapshot{}
}

var day1 = time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC)

// TestTodo_AGENT2_012_RestartDurability checks a restarted ledger keeps
// specs, settled usage, counters and the pause card, and forgets in-flight
// reservations.
func TestTodo_AGENT2_012_RestartDurability(t *testing.T) {
	e := newEnv(t, "bud-one")
	c := &clock{at: day1}
	first := boot(t, e, policy(), c, "bud-one")
	limit := agentbudget.Limits{Steps: 2, Tokens: 1_000, WallClock: time.Hour, SpendMicros: 1_000}
	open(t, first, "task-a", "bud-one", "user-a", limit)

	settled := mustReserve(t, first, "task-a", "s1", "f1")
	actual := agentbudget.Usage{Steps: 1, Tokens: 70, WallClock: 900 * time.Millisecond, SpendMicros: 80}
	if err := settled.Settle(actual); err != nil {
		t.Fatal(err)
	}
	_ = mustReserve(t, first, "task-a", "s2", "f2") // in flight when the process dies
	_, err := reserve(first, "task-a", "s3", "f3", agentbudget.Usage{Steps: 5, Tokens: 1, WallClock: 1, SpendMicros: 1})
	var pause *agentbudget.PauseError
	if !errors.As(err, &pause) || pause.Reason != agentbudget.PauseTaskSteps || !pause.Card.CanAccept {
		t.Fatalf("ceiling Reserve = %v", err)
	}
	before := taskSnap(t, first, "task-a")
	if before.Reserved != call() {
		t.Fatalf("fixture: in-flight reserved = %+v", before.Reserved)
	}

	second := boot(t, e, policy(), c, "bud-one") // restart
	after := taskSnap(t, second, "task-a")
	want := before
	want.Reserved = agentbudget.Usage{} // the restart released the in-flight reservation
	if after != want {
		t.Fatalf("restored task = %+v\nwant          %+v", after, want)
	}
	if after.Limit != limit || after.Used != actual || after.Paused != agentbudget.PauseTaskSteps || after.Attempts != 2 {
		t.Fatalf("restored details = %+v", after)
	}
	if got, w := second.Snapshot().UserPeriodTotals, []agentbudget.Usage{actual}; !reflect.DeepEqual(got, w) {
		t.Fatalf("restored user day = %+v, want %+v", got, w)
	}
	if got, w := second.Snapshot().TenantPeriodTotals, []agentbudget.Usage{actual}; !reflect.DeepEqual(got, w) {
		t.Fatalf("restored tenant month = %+v, want %+v", got, w)
	}
	// Still paused, with the same card, not silently reset by the restart.
	_, err = reserve(second, "task-a", "s4", "f4", call())
	var stillPaused *agentbudget.PauseError
	if !errors.As(err, &stillPaused) || !reflect.DeepEqual(stillPaused.Card, pause.Card) || stillPaused.Reason != pause.Reason {
		t.Fatalf("paused task after restart: err=%v card=%+v want %+v", err, stillPaused, pause.Card)
	}
	if err := second.OpenTask(agentbudget.TaskSpec{ID: "task-a", TenantID: "bud-one", UserID: "user-a"}); !errors.Is(err, agentbudget.ErrTaskExists) {
		t.Fatalf("re-open after restart = %v", err)
	}

	// Accept the extension, restart again: the new limit and cleared pause persist.
	if err := second.AcceptExtension(agentbudget.ExtensionRequest{TaskID: "task-a", RequestID: "extend-restart", ExpectedRevision: taskSnap(t, second, "task-a").Revision, Additional: agentbudget.Limits{Steps: 3}}); err != nil {
		t.Fatal(err)
	}
	third := boot(t, e, policy(), c, "bud-one")
	extended := taskSnap(t, third, "task-a")
	if extended.Limit.Steps != 5 || extended.Paused != "" {
		t.Fatalf("extension lost across restart: %+v", extended)
	}
	if _, err := reserve(third, "task-a", "s5", "f5", call()); err != nil {
		t.Fatalf("Reserve after persisted extension: %v", err)
	}
}

// TestTodo_AGENT2_012_CountersSurviveRestart proves the retry ceiling cannot
// be dodged by crashing: an interrupted try still counts, and failure keys
// (which contain a NUL byte) round-trip so loop detection resumes.
func TestTodo_AGENT2_012_CountersSurviveRestart(t *testing.T) {
	e := newEnv(t, "bud-one")
	c := &clock{at: day1}
	p := policy()
	p.MaxRetries, p.LoopThreshold = 2, 3

	ledger := boot(t, e, p, c, "bud-one")
	open(t, ledger, "crashy", "bud-one", "user-a", agentbudget.Limits{})
	for attempt := 1; attempt <= 3; attempt++ {
		r := mustReserve(t, ledger, "crashy", "step", "fp-"+string(rune('a'+attempt)))
		if r.Attempt != attempt {
			t.Fatalf("attempt after %d restarts = %d", attempt-1, r.Attempt)
		}
		ledger = boot(t, e, p, c, "bud-one") // crash with the reservation in flight
	}
	_, err := reserve(ledger, "crashy", "step", "fp-z", call())
	var pause *agentbudget.PauseError
	if !errors.As(err, &pause) || pause.Reason != agentbudget.PauseRetryLimit {
		t.Fatalf("4th attempt after crashes = %v, want retry-limit pause", err)
	}
	ledger = boot(t, e, p, c, "bud-one")
	if _, err := reserve(ledger, "crashy", "step", "fp-z", call()); !errors.Is(err, agentbudget.ErrPaused) {
		t.Fatalf("retry pause did not survive restart: %v", err)
	}

	open(t, ledger, "loopy", "bud-one", "user-a", agentbudget.Limits{})
	for i := 0; i < 2; i++ {
		if err := mustReserve(t, ledger, "loopy", "s", "same").Fail(); err != nil {
			t.Fatalf("Fail %d: %v", i, err)
		}
	}
	ledger = boot(t, e, p, c, "bud-one")
	err = mustReserve(t, ledger, "loopy", "s", "same").Fail()
	if !errors.As(err, &pause) || pause.Reason != agentbudget.PauseLoopDetected {
		t.Fatalf("third identical failure after restart = %v, want loop pause", err)
	}
	if got := taskSnap(t, boot(t, e, p, c, "bud-one"), "loopy"); got.Paused != agentbudget.PauseLoopDetected {
		t.Fatalf("loop pause not durable: %+v", got)
	}
}

// TestTodo_AGENT2_012_PeriodRollover proves period keys roll with the clock,
// old periods stop counting, and Load returns only the current month's rows.
func TestTodo_AGENT2_012_PeriodRollover(t *testing.T) {
	e := newEnv(t, "bud-one")
	p := policy()
	p.UserDaily.Steps = 2
	c := &clock{at: time.Date(2026, time.January, 31, 23, 0, 0, 0, time.UTC)}
	ledger := boot(t, e, p, c, "bud-one")
	open(t, ledger, "night", "bud-one", "user-a", agentbudget.Limits{})
	for i := 0; i < 2; i++ {
		if err := mustReserve(t, ledger, "night", "s"+string(rune('1'+i)), "f").Settle(call()); err != nil {
			t.Fatal(err)
		}
	}
	// Day one's user ceiling is spent.
	open(t, ledger, "night-2", "bud-one", "user-a", agentbudget.Limits{})
	if _, err := reserve(ledger, "night-2", "s", "f", call()); !errors.Is(err, agentbudget.ErrPaused) {
		t.Fatalf("Reserve past the daily ceiling = %v", err)
	}

	// After midnight the same user has a fresh day; the month rolled too.
	c.set(time.Date(2026, time.February, 1, 1, 0, 0, 0, time.UTC))
	open(t, ledger, "morning", "bud-one", "user-a", agentbudget.Limits{})
	if err := mustReserve(t, ledger, "morning", "s1", "f").Settle(call()); err != nil {
		t.Fatalf("first call of the new day: %v", err)
	}

	s := e.store(t)
	jan, err := s.Load(context.Background(), "bud-one", time.Date(2026, time.January, 31, 23, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]agentbudget.Usage{}
	for _, period := range jan.Periods {
		keys[string(period.Scope)+"/"+period.Key] = period.Used
	}
	twoCalls := agentbudget.Usage{Steps: 2, Tokens: 200, WallClock: 2 * time.Second, SpendMicros: 200}
	wantJan := map[string]agentbudget.Usage{
		"USER_DAILY/user-a|2026-01-31":   twoCalls,
		"TENANT_MONTHLY/bud-one|2026-01": twoCalls,
		"USER_DAILY/user-a|2026-02-01":   call(),
		"TENANT_MONTHLY/bud-one|2026-02": call(),
	}
	if !reflect.DeepEqual(keys, wantJan) {
		t.Fatalf("periods loaded in January = %+v\nwant %+v", keys, wantJan)
	}
	feb, err := s.Load(context.Background(), "bud-one", c.now())
	if err != nil {
		t.Fatal(err)
	}
	if len(feb.Periods) != 2 {
		t.Fatalf("periods loaded in February = %+v; January rows must be excluded", feb.Periods)
	}
	for _, period := range feb.Periods {
		if period.Used != call() || period.Start.Before(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("february period = %+v", period)
		}
	}

	// A restart on day two: yesterday's spend no longer counts, today's does.
	restarted := boot(t, e, p, c, "bud-one")
	open(t, restarted, "evening", "bud-one", "user-a", agentbudget.Limits{})
	if err := mustReserve(t, restarted, "evening", "s1", "f").Settle(call()); err != nil {
		t.Fatalf("second call of day two after restart: %v", err)
	}
	if _, err := reserve(restarted, "evening", "s2", "f", call()); !errors.Is(err, agentbudget.ErrPaused) {
		t.Fatalf("third call of day two (ceiling 2, one used before restart) = %v", err)
	}
}

func bound(t *testing.T, e *env, tenant string) (dbport.Tx, func()) {
	t.Helper()
	ctx := context.Background()
	tx, err := e.conn(t).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(ctx, tx, e.tenants[tenant]); err != nil {
		t.Fatal(err)
	}
	return tx, func() { _ = tx.Rollback(ctx) }
}

func count(t *testing.T, tx dbport.Tx, table string) int {
	t.Helper()
	var n int
	if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestTodo_AGENT2_012_CrossTenantIsolation(t *testing.T) {
	e := newEnv(t, "bud-one", "bud-two")
	c := &clock{at: day1}
	ledger := boot(t, e, policy(), c, "bud-one", "bud-two")
	open(t, ledger, "one-task", "bud-one", "user-a", agentbudget.Limits{})
	open(t, ledger, "two-task", "bud-two", "user-a", agentbudget.Limits{})
	if err := mustReserve(t, ledger, "one-task", "s", "f").Settle(call()); err != nil {
		t.Fatal(err)
	}
	if _, err := reserve(ledger, "two-task", "s", "f", agentbudget.Usage{Steps: 60, Tokens: 1, WallClock: 1, SpendMicros: 1}); !errors.Is(err, agentbudget.ErrPaused) {
		t.Fatal(err)
	}

	s := e.store(t)
	one, err := s.Load(context.Background(), "bud-one", c.now())
	if err != nil {
		t.Fatal(err)
	}
	if len(one.Tasks) != 1 || one.Tasks[0].Spec.ID != "one-task" || one.Tasks[0].Paused != nil || one.Tasks[0].Used != call() || len(one.Periods) != 2 {
		t.Fatalf("tenant one state = %+v", one)
	}
	two, err := s.Load(context.Background(), "bud-two", c.now())
	if err != nil {
		t.Fatal(err)
	}
	if len(two.Tasks) != 1 || two.Tasks[0].Spec.ID != "two-task" || two.Tasks[0].Paused == nil || two.Tasks[0].Used.Steps != 0 || len(two.Periods) != 0 {
		t.Fatalf("tenant two state = %+v", two)
	}
	if _, err := s.Load(context.Background(), "bud-ghost", c.now()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Load unknown tenant = %v", err)
	}

	// A task of an unmapped tenant is refused durably, so the ledger refuses it too.
	if err := ledger.OpenTask(agentbudget.TaskSpec{ID: "ghost-task", TenantID: "bud-ghost", UserID: "u"}); !errors.Is(err, agentbudget.ErrPersistence) || !errors.Is(err, ErrInvalid) {
		t.Fatalf("OpenTask for unknown tenant = %v", err)
	}
	if _, err := reserve(ledger, "ghost-task", "s", "f", call()); !errors.Is(err, agentbudget.ErrTaskNotFound) {
		t.Fatalf("ghost task admitted work: %v", err)
	}

	// Storage level: each binding sees only its own rows and cannot write the other's.
	txOne, done := bound(t, e, "bud-one")
	defer done()
	for table, want := range map[string]int{"agent_budget_task": 1, "agent_budget_period": 2, "agent_budget_event": 2} {
		if n := count(t, txOne, table); n != want {
			t.Fatalf("%s rows visible to tenant one = %d, want %d", table, n, want)
		}
	}
	if _, err := txOne.Exec(context.Background(), `INSERT INTO agent_budget_task (tenant_id, task_id, tenant_ref, user_id, limit_steps, limit_tokens, limit_wall_ns, limit_spend, attempts, failures, created_at, updated_at)
		VALUES ($1,'smuggled','bud-two','u',1,1,1,1,'{}','{}',now(),now())`, e.tenants["bud-two"]); err == nil {
		t.Fatal("tenant one binding wrote a tenant two row")
	}
	unbound, err := e.conn(t).Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer unbound.Rollback(context.Background())
	if n := count(t, unbound, "agent_budget_task"); n != 0 {
		t.Fatalf("unbound app-role connection sees %d task rows", n)
	}
}

// TestTodo_AGENT2_012_Race runs concurrent admissions through one ledger and
// requires the durable rows to match the in-memory totals exactly.
func TestTodo_AGENT2_012_Race(t *testing.T) {
	e := newEnv(t, "bud-one")
	c := &clock{at: day1}
	ledger := boot(t, e, policy(), c, "bud-one")
	const tasks, workers, calls = 4, 16, 5
	for i := 0; i < tasks; i++ {
		open(t, ledger, "race-"+string(rune('a'+i)), "bud-one", "user-a", agentbudget.Limits{Steps: 500, Tokens: 1_000_000, WallClock: time.Hour, SpendMicros: 1_000_000})
	}
	var wg sync.WaitGroup
	errs := make(chan error, workers*calls)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task := "race-" + string(rune('a'+w%tasks))
			for i := 0; i < calls; i++ {
				r, err := reserve(ledger, task, fmt.Sprintf("step-%d-%d", w, i), "fp", call())
				if err != nil {
					errs <- err
					return
				}
				if err := r.Settle(call()); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent admission: %v", err)
	}
	live := ledger.Snapshot()
	restored := boot(t, e, policy(), c, "bud-one").Snapshot()
	if !reflect.DeepEqual(live, restored) {
		t.Fatalf("durable state differs from memory\nlive:     %+v\nrestored: %+v", live, restored)
	}
	total := agentbudget.Usage{}
	for _, task := range restored.Tasks {
		total.Steps += task.Used.Steps
	}
	if total.Steps != workers*calls || restored.UserPeriodTotals[0].Steps != workers*calls || restored.TenantPeriodTotals[0].Steps != workers*calls {
		t.Fatalf("settled steps task=%d user=%+v tenant=%+v, want %d", total.Steps, restored.UserPeriodTotals, restored.TenantPeriodTotals, workers*calls)
	}
}

func TestTodo_AGENT2_012_JournalIsAppendOnly(t *testing.T) {
	e := newEnv(t, "bud-one")
	c := &clock{at: day1}
	ledger := boot(t, e, policy(), c, "bud-one")
	open(t, ledger, "journaled", "bud-one", "user-a", agentbudget.Limits{Steps: 2, Tokens: 1_000, WallClock: time.Hour, SpendMicros: 1_000})
	r := mustReserve(t, ledger, "journaled", "s1", "f")
	if err := r.Settle(call()); err != nil {
		t.Fatal(err)
	}
	if err := mustReserve(t, ledger, "journaled", "s2", "g").Release(); err != nil {
		t.Fatal(err)
	}
	if err := mustReserve(t, ledger, "journaled", "s3", "h").Fail(); err != nil {
		t.Fatal(err)
	}
	if _, err := reserve(ledger, "journaled", "s4", "i", agentbudget.Usage{Steps: 5, Tokens: 1, WallClock: 1, SpendMicros: 1}); !errors.Is(err, agentbudget.ErrPaused) {
		t.Fatal(err)
	}
	if err := ledger.AcceptExtension(agentbudget.ExtensionRequest{TaskID: "journaled", RequestID: "extend-journal", ExpectedRevision: taskSnap(t, ledger, "journaled").Revision, Additional: agentbudget.Limits{Steps: 4}}); err != nil {
		t.Fatal(err)
	}

	tx, done := bound(t, e, "bud-one")
	defer done()
	rows, err := tx.Query(context.Background(), `SELECT kind, reservation_id, steps, pause_reason FROM agent_budget_event ORDER BY event_seq`)
	if err != nil {
		t.Fatal(err)
	}
	type entry struct {
		kind, reservation string
		steps             int64
		pause             string
	}
	var got []entry
	for rows.Next() {
		var en entry
		if err := rows.Scan(&en.kind, &en.reservation, &en.steps, &en.pause); err != nil {
			t.Fatal(err)
		}
		got = append(got, en)
	}
	rows.Close()
	want := []entry{
		{"OPEN_TASK", "", 2, ""},
		{"SETTLE", r.ID, 1, ""},
		{"FAIL", "reservation-00000003", 0, ""},
		{"PAUSE", "", 0, "TASK_STEP_CEILING"},
		{"EXTENSION", "", 4, ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("journal = %+v\nwant     %+v", got, want)
	}
	if err := e.db.ExecErr(`UPDATE agent_budget_event SET steps=0`); err == nil {
		t.Fatal("journal UPDATE was allowed")
	}
	if err := e.db.ExecErr(`DELETE FROM agent_budget_event`); err == nil {
		t.Fatal("journal DELETE was allowed")
	}
}

type failingDB struct{ err error }

func (f failingDB) Begin(context.Context) (dbport.Tx, error) { return nil, f.err }

func TestTodo_AGENT2_012_StoreErrors(t *testing.T) {
	if _, err := New(nil, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("New(nil) = %v", err)
	}
	mapper := func(values.TenantId) uuid.UUID { return uuid.New() }
	down, err := New(failingDB{err: errors.New("db down")}, mapper)
	if err != nil {
		t.Fatal(err)
	}
	spec := agentbudget.TaskSpec{ID: "t", TenantID: "any", UserID: "u", Limit: call()}
	open := agentbudget.Transition{Kind: agentbudget.TransitionOpenTask, Task: agentbudget.DurableTask{Spec: spec}}
	if err := down.Persist(open); err == nil {
		t.Fatal("Persist on a failing database succeeded")
	}
	if _, err := down.Load(context.Background(), "any", day1); err == nil {
		t.Fatal("Load on a failing database succeeded")
	}
	if err := down.Persist(agentbudget.Transition{Kind: agentbudget.TransitionRelease, Task: open.Task}); err == nil {
		t.Fatal("Release persistence failure was ignored")
	}
	var nilStore *Store
	if err := nilStore.Persist(open); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil store Persist = %v", err)
	}
	if err := down.Persist(agentbudget.Transition{Kind: agentbudget.TransitionOpenTask}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Persist without a tenant = %v", err)
	}

	// Real database: refusals that the ledger cannot produce on its own.
	e := newEnv(t, "bud-one")
	s := e.store(t)
	spec.TenantID = "bud-one"
	spec.Limit = agentbudget.Limits{Steps: 1, Tokens: 1, WallClock: time.Second, SpendMicros: 1}
	task := agentbudget.DurableTask{Spec: spec, Revision: 1, Attempts: map[string]int{}, Failures: map[string]int{}}
	if err := s.Persist(agentbudget.Transition{Kind: agentbudget.TransitionReserve, Task: task}); !errors.Is(err, ErrTaskMissing) {
		t.Fatalf("update of an unstored task = %v", err)
	}
	if err := s.Persist(agentbudget.Transition{Kind: agentbudget.TransitionOpenTask, Task: task}); err != nil {
		t.Fatal(err)
	}
	if err := s.Persist(agentbudget.Transition{Kind: agentbudget.TransitionOpenTask, Task: task}); !errors.Is(err, ErrTaskExists) {
		t.Fatalf("second OPEN_TASK = %v", err)
	}
	badScopeTask := task
	badScopeTask.Revision = 2
	badScope := agentbudget.Transition{Kind: agentbudget.TransitionSettle, Task: badScopeTask, ExpectedRevision: 1, ResultRevision: 2, Periods: []agentbudget.DurablePeriod{{Scope: agentbudget.ScopeTask, Key: "k"}}}
	if err := s.Persist(badScope); !errors.Is(err, ErrInvalid) {
		t.Fatalf("period scope TASK = %v", err)
	}
	reasonless := task
	reasonless.Revision = 2
	reasonless.Paused = &agentbudget.PauseError{TaskID: "t"}
	if err := s.Persist(agentbudget.Transition{Kind: agentbudget.TransitionPause, Task: reasonless, ExpectedRevision: 1, ResultRevision: 2}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("pause without reason = %v", err)
	}
	// NUL-bearing failure keys and a pause card round-trip through Load.
	paused := task
	paused.Revision = 2
	paused.Failures = map[string]int{"step\x00fp": 2}
	paused.Attempts = map[string]int{"step": 3}
	paused.Paused = &agentbudget.PauseError{Reason: agentbudget.PauseLoopDetected, Scope: agentbudget.ScopeTask, TaskID: "t",
		Card: agentbudget.ExtensionCard{ID: "extension-t", TaskID: "t", Scope: agentbudget.ScopeTask, Reason: agentbudget.PauseLoopDetected, Message: "m",
			MaxAdditional: agentbudget.Limits{Steps: 1, WallClock: time.Minute}}}
	if err := s.Persist(agentbudget.Transition{Kind: agentbudget.TransitionFail, Task: paused, ExpectedRevision: 1, ResultRevision: 2}); err != nil {
		t.Fatal(err)
	}
	state, err := s.Load(context.Background(), "bud-one", day1)
	if err != nil || len(state.Tasks) != 1 {
		t.Fatalf("Load = %+v, %v", state, err)
	}
	got := state.Tasks[0]
	if !reflect.DeepEqual(got.Failures, paused.Failures) || !reflect.DeepEqual(got.Attempts, paused.Attempts) || !reflect.DeepEqual(got.Paused, paused.Paused) || got.Spec != spec {
		t.Fatalf("round trip = %+v\nwant       %+v", got, paused)
	}
}
