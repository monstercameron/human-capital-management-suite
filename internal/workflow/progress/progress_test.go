package progress

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/opsmeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var evalAt = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

func liveInstance(status string) Instance {
	return Instance{TenantID: uuid.NewString(), InstanceID: uuid.NewString(), WorkflowID: "wf.promotion", RuntimeStatus: status,
		CorrelationID: "corr-1", CreatedAt: evalAt.AddDate(0, -3, 0), StartedAt: evalAt.AddDate(0, -3, 0), LastRecordedAt: evalAt.AddDate(0, -3, 0)}
}

func kinds(findings []Finding) []Kind {
	out := make([]Kind, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Kind)
	}
	return out
}

func hasKind(findings []Finding, k Kind) bool {
	for _, f := range findings {
		if f.Kind == k {
			return true
		}
	}
	return false
}

// TestTodo_WF_RUN_020 is the PRIMARY. RED: a long legal wait flagged solely
// by age, or a poison node left invisible despite a missed lease, timer,
// signal, retry or SLA expectation. GREEN: detection uses each state's own
// expected progress, and a stuck instance opens exactly one incident that a
// repeated detection links to instead of duplicating.
func TestTodo_WF_RUN_020(t *testing.T) {
	p := DefaultPolicy()

	t.Run("a three-month-old instance legitimately waiting on a future timer is not stuck", func(t *testing.T) {
		s := Snapshot{Instance: liveInstance("RUNNING"), Timers: []Timer{{TimerID: "t1", NodeID: "wait_effective", Kind: "DEADLINE", State: "PENDING", FiresAt: evalAt.AddDate(0, 1, 0)}}}
		findings, err := Detect(s, p, evalAt)
		if err != nil || len(findings) != 0 {
			t.Fatalf("Detect = %v, %v; a legal long wait must not be flagged by age", kinds(findings), err)
		}
	})

	t.Run("a legitimately waiting human task inside its escalation window is not stuck", func(t *testing.T) {
		s := Snapshot{Instance: liveInstance("RUNNING"), HumanTasks: []HumanTask{{WorkItemID: "w1", NodeID: "approve", Status: "ASSIGNED", Open: true, HasSLA: true, BreachState: "WITHIN_TARGET", EscalateAt: evalAt.Add(48 * time.Hour)}}}
		if findings, _ := Detect(s, p, evalAt); len(findings) != 0 {
			t.Fatalf("Detect = %v, want none", kinds(findings))
		}
	})

	for _, tc := range []struct {
		name string
		snap Snapshot
		want Kind
	}{
		{"an overdue timer", Snapshot{Instance: liveInstance("RUNNING"), Timers: []Timer{{TimerID: "t1", NodeID: "wait", Kind: "DELAY", State: "PENDING", FiresAt: evalAt.Add(-time.Hour)}}}, KindTimerOverdue},
		{"an abandoned lease", Snapshot{Instance: liveInstance("RUNNING"), Leases: []Lease{{LeaseID: "l1", ResourceKind: "NODE_EXECUTION", ResourceID: "i/n", State: "HELD", ExpiresAt: evalAt.Add(-time.Hour), HeartbeatAt: evalAt.Add(-2 * time.Hour)}}}, KindLeaseAbandoned},
		{"undispatched ready work", Snapshot{Instance: liveInstance("RUNNING"), ReadyWork: []ReadyWork{{ReadyWorkID: "r1", NodeID: "n", State: "READY", Attempt: 1, EligibleAt: evalAt.Add(-time.Hour)}}}, KindReadyWorkUndispatched},
		{"an expired open signal subscription", Snapshot{Instance: liveInstance("WAITING"), Subscriptions: []Subscription{{SubscriptionID: "s1", NodeID: "await", SignalName: "hr.ack", State: "OPEN", ExpiresAt: evalAt.Add(-time.Hour)}}}, KindSignalExpiredUnclosed},
		{"a missed SLA escalation", Snapshot{Instance: liveInstance("RUNNING"), HumanTasks: []HumanTask{{WorkItemID: "w1", NodeID: "approve", Status: "ASSIGNED", Open: true, HasSLA: true, BreachState: "BREACHED", EscalateAt: evalAt.Add(-time.Hour)}}}, KindSLAEscalationMissed},
		{"a poison node that keeps failing while a retry is pending", Snapshot{Instance: liveInstance("RUNNING"),
			ReadyWork: []ReadyWork{{ReadyWorkID: "r4", NodeID: "commit", State: "READY", Attempt: 4, EligibleAt: evalAt.Add(time.Hour)}},
			Attempts:  []NodeAttempt{{NodeID: "commit", Attempt: 1, Status: "FAILED"}, {NodeID: "commit", Attempt: 2, Status: "FAILED"}, {NodeID: "commit", Attempt: 3, Status: "FAILED"}}}, KindPoisonNode},
		{"a running instance nothing can advance", Snapshot{Instance: liveInstance("RUNNING")}, KindNoProgressMechanism},
	} {
		t.Run("flags "+tc.name, func(t *testing.T) {
			findings, err := Detect(tc.snap, p, evalAt)
			if err != nil {
				t.Fatal(err)
			}
			if !hasKind(findings, tc.want) {
				t.Fatalf("Detect = %v, want %s", kinds(findings), tc.want)
			}
		})
	}

	t.Run("a paused or terminal instance holds no expectation", func(t *testing.T) {
		for _, status := range []string{"PAUSED", "COMPLETED", "CANCELLED", "REPAIR_REQUIRED", "QUARANTINED", "SUPERSEDED"} {
			s := Snapshot{Instance: liveInstance(status), Timers: []Timer{{TimerID: "t", State: "PENDING", FiresAt: evalAt.Add(-24 * time.Hour)}}}
			if findings, _ := Detect(s, p, evalAt); len(findings) != 0 {
				t.Errorf("%s instance flagged %v", status, kinds(findings))
			}
		}
	})

	t.Run("on the real runtime rows, one stuck instance opens one incident and a repeat links to it", func(t *testing.T) {
		h := newHarness(t)
		stuck := h.instance("RUNNING", evalAt.AddDate(0, -3, 0))
		h.timer(stuck, "wait_effective", evalAt.Add(-2*time.Hour))
		waiting := h.instance("RUNNING", evalAt.AddDate(0, -3, 0))
		h.timer(waiting, "wait_effective", evalAt.AddDate(0, 1, 0))
		before := h.businessFingerprint()

		first, err := h.sweeper().Sweep(context.Background(), h.tenant)
		if err != nil {
			t.Fatalf("first sweep: %v", err)
		}
		if first.Instances != 2 || first.Stuck != 1 || first.Opened != 1 || first.Linked != 0 {
			t.Fatalf("first sweep = %+v, want 2 read, 1 stuck, 1 opened", first)
		}
		second, err := h.sweeper().Sweep(context.Background(), h.tenant)
		if err != nil {
			t.Fatalf("second sweep: %v", err)
		}
		if second.Stuck != 1 || second.Opened != 0 || second.Linked != 1 {
			t.Fatalf("second sweep = %+v, want the same stuck instance linked, nothing opened", second)
		}
		if n := h.incidentCount(); n != 1 {
			t.Fatalf("%d operational incidents, want exactly 1", n)
		}
		if after := h.businessFingerprint(); after != before {
			t.Fatalf("the sweep changed runtime or business rows:\nbefore %s\nafter  %s", before, after)
		}
	})
}

// TestTodo_WF_RUN_020_Race runs concurrent sweeps over the same stuck
// instance and proves the operations store's unique incident key still leaves
// exactly one incident.
func TestTodo_WF_RUN_020_Race(t *testing.T) {
	h := newHarness(t)
	id := h.instance("RUNNING", evalAt.AddDate(0, -1, 0))
	h.lease(id, "commit", evalAt.Add(-time.Hour))
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		w := h.sweeper()
		w.Begin = h.appConn().Begin
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := w.Sweep(context.Background(), h.tenant); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent sweep: %v", err)
	}
	if n := h.incidentCount(); n != 1 {
		t.Fatalf("%d incidents after 8 concurrent sweeps, want 1", n)
	}
}

// TestTodo_WF_RUN_020_Fault proves the sweep fails loudly rather than
// silently: an invalid policy, a missing evaluation instant, an unscoped
// tenant, a failed transaction and an incident storm each surface as an
// error, and a storm-refused incident does not hide other stuck instances.
func TestTodo_WF_RUN_020_Fault(t *testing.T) {
	if _, err := Detect(Snapshot{Instance: liveInstance("RUNNING")}, Policy{PoisonAttempts: 0}, evalAt); err == nil {
		t.Fatal("Detect accepted a poison threshold of 0")
	}
	if _, err := Detect(Snapshot{Instance: liveInstance("RUNNING")}, Policy{TimerGrace: -time.Second, PoisonAttempts: 1}, evalAt); err == nil {
		t.Fatal("Detect accepted a negative grace")
	}
	if _, err := Detect(Snapshot{Instance: liveInstance("RUNNING")}, DefaultPolicy(), time.Time{}); err == nil {
		t.Fatal("Detect accepted a zero evaluation instant")
	}
	if _, err := (Sweeper{}).Sweep(context.Background(), uuid.New()); err == nil {
		t.Fatal("a sweeper without a transaction opener swept")
	}
	boom := errors.New("database unavailable")
	if _, err := (Sweeper{Begin: func(context.Context) (dbport.Tx, error) { return nil, boom }, Policy: DefaultPolicy()}).Sweep(context.Background(), uuid.New()); !errors.Is(err, boom) {
		t.Fatalf("a failed transaction = %v, want the database error", err)
	}
	if _, err := RaiseIncident(context.Background(), nil, uuid.New(), Snapshot{}, nil, Route{}); !errors.Is(err, ErrNoFindings) {
		t.Fatalf("raising a healthy instance = %v, want ErrNoFindings", err)
	}

	h := newHarness(t)
	for range 3 {
		h.timer(h.instance("RUNNING", evalAt.AddDate(0, -1, 0)), "wait", evalAt.Add(-time.Hour))
	}
	w := h.sweeper()
	w.Route.StormLimit = 1
	result, err := w.Sweep(context.Background(), h.tenant)
	if !errors.Is(err, opsmeta.ErrAlertStorm) {
		t.Fatalf("storm-limited sweep error = %v, want ErrAlertStorm", err)
	}
	if result.Stuck != 3 || result.Opened != 1 || result.Failed != 2 {
		t.Fatalf("storm-limited sweep = %+v, want 3 stuck, 1 opened, 2 refused and reported", result)
	}
}

// TestTodo_WF_RUN_020_Security runs the sweep under the application role and
// proves row-level security confines it to its own tenant: another tenant's
// stuck instance is neither read nor raised, and an incident cannot be raised
// against a tenant the snapshot does not belong to.
func TestTodo_WF_RUN_020_Security(t *testing.T) {
	h := newHarness(t)
	other := h.otherTenant()
	h.timer(h.instanceFor(other, "RUNNING", evalAt.AddDate(0, -1, 0)), "wait", evalAt.Add(-time.Hour))
	mine := h.instance("RUNNING", evalAt.AddDate(0, -1, 0))
	h.timer(mine, "wait", evalAt.Add(-time.Hour))

	result, err := h.sweeper().Sweep(context.Background(), h.tenant)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if result.Instances != 1 || result.Stuck != 1 {
		t.Fatalf("sweep of one tenant = %+v, want only its own one instance", result)
	}
	if n := h.incidentCountFor(other); n != 0 {
		t.Fatalf("the sweep raised %d incidents in another tenant", n)
	}

	s := Snapshot{Instance: liveInstance("RUNNING")}
	if _, err := RaiseIncident(context.Background(), nil, uuid.New(), s, []Finding{{Kind: KindNoProgressMechanism, Ref: "x"}}, Route{}); err == nil {
		t.Fatal("RaiseIncident accepted a snapshot from a different tenant")
	}
}

// TestTodo_WF_RUN_020_Mutation plants the defects this detector exists to
// catch and proves each is caught, and that a planted age-only rule would be.
func TestTodo_WF_RUN_020_Mutation(t *testing.T) {
	p := DefaultPolicy()
	ancient := liveInstance("RUNNING")
	ancient.CreatedAt = evalAt.AddDate(-2, 0, 0)
	legal := Snapshot{Instance: ancient, Timers: []Timer{{TimerID: "t", NodeID: "wait", Kind: "DEADLINE", State: "PENDING", FiresAt: evalAt.AddDate(0, 0, 10)}}}
	if findings, _ := Detect(legal, p, evalAt); len(findings) != 0 {
		t.Fatalf("a two-year-old legal wait was flagged %v: the detector is age-based", kinds(findings))
	}

	for name, mutate := range map[string]func(*Snapshot){
		"timer moved into the past": func(s *Snapshot) { s.Timers[0].FiresAt = evalAt.Add(-time.Hour) },
		"timer removed":             func(s *Snapshot) { s.Timers = nil },
		"node failing three times": func(s *Snapshot) {
			s.Attempts = []NodeAttempt{{NodeID: "n", Status: "FAILED"}, {NodeID: "n", Status: "FAILED"}, {NodeID: "n", Status: "FAILED"}}
		},
		"lease abandoned alongside": func(s *Snapshot) {
			s.Leases = []Lease{{LeaseID: "l", State: "HELD", ExpiresAt: evalAt.Add(-time.Hour)}}
		},
		"escalation missed alongside": func(s *Snapshot) {
			s.HumanTasks = []HumanTask{{WorkItemID: "w", Open: true, HasSLA: true, BreachState: "AT_RISK", EscalateAt: evalAt.Add(-time.Hour)}}
		},
	} {
		mutated := legal
		mutated.Timers = append([]Timer(nil), legal.Timers...)
		mutate(&mutated)
		if findings, _ := Detect(mutated, p, evalAt); len(findings) == 0 {
			t.Errorf("planted defect %q was not detected", name)
		}
	}

	a := []Finding{{Kind: KindTimerOverdue, NodeID: "wait", Ref: "workflow_timer:1", Detail: "one wording"}}
	b := []Finding{{Kind: KindTimerOverdue, NodeID: "wait", Ref: "workflow_timer:1", Detail: "other wording"}}
	c := []Finding{{Kind: KindLeaseAbandoned, Ref: "workflow_lease:1"}}
	if IncidentKey("i", a) != IncidentKey("i", b) {
		t.Error("the incident key depends on finding wording, so a repeated detection would open a duplicate")
	}
	if IncidentKey("i", a) == IncidentKey("i", c) {
		t.Error("the incident key ignores which expectation was missed")
	}
	if IncidentKey("i", a) == IncidentKey("j", a) {
		t.Error("the incident key ignores the instance")
	}
}

// --- harness -------------------------------------------------------------

type harness struct {
	t      *testing.T
	db     *pgtest.DB
	app    *pgxadapter.Conn
	tenant uuid.UUID
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	db := pgtest.New(t)
	h := &harness{t: t, db: db, tenant: uuid.New()}
	h.insertTenant(h.tenant)
	h.app = h.appConn()
	return h
}

// appConn is a fresh connection under the application role, so row-level
// security applies exactly as it does in production.
func (h *harness) appConn() *pgxadapter.Conn {
	conn := h.db.NewConn(h.t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		h.t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func (h *harness) insertTenant(id uuid.UUID) {
	h.db.Exec(h.t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, id, "t-"+id.String()[:8], "tenant "+id.String()[:8])
}

func (h *harness) otherTenant() uuid.UUID {
	id := uuid.New()
	h.insertTenant(id)
	return id
}

func (h *harness) sweeper() Sweeper {
	return Sweeper{
		Begin:  h.app.Begin,
		Policy: DefaultPolicy(),
		Route:  Route{PrimaryOwner: "team:workflow-runtime", SecondaryRoute: "team:platform-oncall", StormLimit: 50, StormWindow: time.Hour},
		Clock:  func() time.Time { return evalAt },
	}
}

func (h *harness) instance(status string, created time.Time) uuid.UUID {
	return h.instanceFor(h.tenant, status, created)
}

func (h *harness) instanceFor(tenant uuid.UUID, status string, created time.Time) uuid.UUID {
	id := uuid.New()
	h.db.Exec(h.t, `INSERT INTO workflow_instance (tenant_id, instance_id, cell_id, workflow_id, workflow_version,
			compiled_plan_hash, execution_mode, runtime_status, input_ref, correlation_id, created_at, started_at)
		VALUES ($1, $2, 'cell-local', 'wf.promotion', 1, repeat('a', 64), 'EXECUTE', $3, 'sha256:input', $4, $5, $5)`,
		tenant, id, status, "corr-"+id.String()[:8], created)
	return id
}

func (h *harness) tenantOf(instance uuid.UUID) uuid.UUID {
	var tenant uuid.UUID
	if err := h.db.QueryRow(context.Background(), `SELECT tenant_id FROM workflow_instance WHERE instance_id = $1`, instance).Scan(&tenant); err != nil {
		h.t.Fatalf("tenant of %s: %v", instance, err)
	}
	return tenant
}

func (h *harness) timer(instance uuid.UUID, node string, firesAt time.Time) {
	h.db.Exec(h.t, `INSERT INTO workflow_timer (tenant_id, timer_id, instance_id, node_id, timer_key, timer_kind, timer_state, fires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, 'DEADLINE', 'PENDING', $6, $7)`,
		h.tenantOf(instance), uuid.New(), instance, node, "timer-"+node, firesAt, firesAt.Add(-24*time.Hour))
}

func (h *harness) lease(instance uuid.UUID, node string, expires time.Time) {
	h.db.Exec(h.t, `INSERT INTO workflow_lease (tenant_id, lease_id, resource_kind, resource_id, lease_state, fence_token, holder_id, acquired_at, expires_at, heartbeat_at)
		VALUES ($1, $2, 'NODE_EXECUTION', $3, 'HELD', 1, 'worker-gone', $4, $5, $4)`,
		h.tenantOf(instance), uuid.New(), instance.String()+"/"+node, expires.Add(-time.Minute), expires)
}

func (h *harness) incidentCount() int { return h.incidentCountFor(h.tenant) }

func (h *harness) incidentCountFor(tenant uuid.UUID) int {
	var n int
	if err := h.db.QueryRow(context.Background(), `SELECT count(*) FROM operational_incident WHERE tenant_id = $1 AND incident_key LIKE 'workflow-progress:v1:%'`, tenant).Scan(&n); err != nil {
		h.t.Fatalf("count incidents: %v", err)
	}
	return n
}

// businessFingerprint is every runtime and business row the detector must
// never write, rendered with its version columns.
func (h *harness) businessFingerprint() string {
	var parts []string
	for _, q := range []string{
		`SELECT coalesce(string_agg(instance_id::text || ':' || runtime_status || ':' || instance_version, ',' ORDER BY instance_id), '') FROM workflow_instance`,
		`SELECT coalesce(string_agg(timer_id::text || ':' || timer_state || ':' || timer_version, ',' ORDER BY timer_id), '') FROM workflow_timer`,
		`SELECT coalesce(string_agg(lease_id::text || ':' || lease_state || ':' || lease_version, ',' ORDER BY lease_id), '') FROM workflow_lease`,
		`SELECT count(*)::text FROM workflow_node_execution`,
		`SELECT count(*)::text FROM work_item`,
		`SELECT count(*)::text FROM ledger_event`,
		`SELECT count(*)::text FROM outbox`,
	} {
		var s string
		if err := h.db.QueryRow(context.Background(), q).Scan(&s); err != nil {
			h.t.Fatalf("fingerprint %q: %v", q, err)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " | ")
}
