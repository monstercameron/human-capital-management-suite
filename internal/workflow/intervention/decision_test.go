package intervention

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// store is one tenant with one stored instance and an app-role connection.
type store struct {
	t      *testing.T
	db     *pgtest.DB
	tenant uuid.UUID
	inst   runtime.Instance
	plan   *workflow.CompiledWorkflow
}

func newStore(t *testing.T) *store {
	t.Helper()
	db := pgtest.New(t)
	s := &store{t: t, db: db, tenant: uuid.New(), plan: execPlan(t)}
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'intervention', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, s.tenant, "iv-"+s.tenant.String()[:8])
	inst, err := runtime.NewInstance(s.tenant, uuid.New(), "cell-local", s.plan, workflow.ModeExecute, "sha256:input", "corr", interventionInstant)
	if err != nil {
		t.Fatal(err)
	}
	s.tx(s.conn(), func(tx dbport.Tx) error {
		stored, err := (runtime.Store{}).CreateInstance(context.Background(), tx, inst)
		s.inst = stored
		return err
	})
	return s
}

func (s *store) conn() *pgxadapter.Conn {
	s.t.Helper()
	conn := s.db.NewConn(s.t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		s.t.Fatal(err)
	}
	return conn
}

func (s *store) txErr(conn *pgxadapter.Conn, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, s.tenant); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *store) tx(conn *pgxadapter.Conn, fn func(tx dbport.Tx) error) {
	s.t.Helper()
	if err := s.txErr(conn, fn); err != nil {
		s.t.Fatalf("tenant tx: %v", err)
	}
}

// decision seals a PAUSED -> RUNNING resume decision for the stored instance.
func (s *store) decision(key string) Decision {
	s.t.Helper()
	f := Facts{Plan: s.plan, Instance: s.inst}
	f.Instance.RuntimeStatus = runtime.InstancePaused
	req := request(f, Resume)
	p, err := Evaluate(req, f)
	if err != nil {
		s.t.Fatal(err)
	}
	d, err := NewDecision(req, p, Binding{TenantID: s.tenant, OperatorKind: "WORKFLOW_RESUME", IntentInstanceID: "intent:operator:" + key, IdempotencyKey: key},
		Observed{InstanceStatus: runtime.InstanceRunning, InstanceVersion: s.inst.InstanceVersion + 1})
	if err != nil {
		s.t.Fatal(err)
	}
	return d
}

// TestTodo_WF_RUN_015_DecisionKindsMatchTheMigration keeps the ledger's kind
// constraint and the taxonomy in step: a kind the table refuses could never be
// decided, and one it accepts but the package does not declare would be a
// free-form intervention by the back door.
func TestTodo_WF_RUN_015_DecisionKindsMatchTheMigration(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "00307_workflow_intervention_decision.sql"))
	if err != nil {
		t.Fatal(err)
	}
	constraint := regexp.MustCompile(`(?s)workflow_intervention_decision_kind CHECK \(kind IN\s*\((.*?)\)\)`).FindSubmatch(src)
	if constraint == nil {
		t.Fatal("the migration declares no kind constraint")
	}
	declared := map[string]bool{}
	for _, token := range regexp.MustCompile(`'([A-Z_]+)'`).FindAllStringSubmatch(string(constraint[1]), -1) {
		declared[token[1]] = true
	}
	for _, k := range Kinds() {
		if !declared[string(k)] {
			t.Errorf("the decision table refuses kind %s", k)
		}
		delete(declared, string(k))
	}
	for k := range declared {
		t.Errorf("the decision table accepts %s, which is not a declared kind", k)
	}
}

// TestTodo_WF_RUN_015_DecisionStore records a sealed decision once and reads
// it back verified; the row is tenant-isolated and immutable to the app role.
func TestTodo_WF_RUN_015_DecisionStore(t *testing.T) {
	s := newStore(t)
	conn := s.conn()
	d := s.decision("resume-1")
	s.tx(conn, func(tx dbport.Tx) error { return DecisionStore{}.Record(context.Background(), tx, d) })

	var got []Decision
	s.tx(conn, func(tx dbport.Tx) (err error) {
		got, err = DecisionStore{}.Load(context.Background(), tx, s.tenant, s.inst.InstanceID)
		return err
	})
	if len(got) != 1 || got[0].Digest != d.Digest || got[0].Verify() != nil || got[0].Plan.InstanceTo != runtime.InstanceRunning {
		t.Fatalf("loaded %+v, want the recorded decision %s", got, d.Digest)
	}

	// A second decision for the same instance version records nothing.
	if err := s.txErr(conn, func(tx dbport.Tx) error {
		return DecisionStore{}.Record(context.Background(), tx, s.decision("resume-2"))
	}); CodeOf(err) != CodeStaleVersion {
		t.Fatalf("second decision for one version = %v, want %s", err, CodeStaleVersion)
	}
	// A tampered decision is refused before any statement.
	tampered := d
	tampered.Reason = "forged"
	if err := s.txErr(conn, func(tx dbport.Tx) error { return DecisionStore{}.Record(context.Background(), tx, tampered) }); CodeOf(err) != CodeDecisionMutated {
		t.Fatalf("tampered record = %v", err)
	}
	// The app role can neither update nor delete a decision.
	for _, stmt := range []string{
		`UPDATE workflow_intervention_decision SET reason = 'rewritten'`,
		`DELETE FROM workflow_intervention_decision`,
	} {
		if err := s.txErr(conn, func(tx dbport.Tx) error { _, err := tx.Exec(context.Background(), stmt); return err }); err == nil {
			t.Fatalf("%s succeeded for the app role", stmt)
		}
	}
	// Another tenant sees nothing.
	other := uuid.New()
	s.db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'other', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, other, "iv-other-"+other.String()[:8])
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, other); err != nil {
		t.Fatal(err)
	}
	if rows, err := (DecisionStore{}).Load(ctx, tx, s.tenant, s.inst.InstanceID); err != nil || len(rows) != 0 {
		t.Fatalf("cross-tenant load = %d rows, %v", len(rows), err)
	}
}

// TestTodo_WF_RUN_015_DecisionStoreRace records decisions for one instance
// version from many connections at once: exactly one lands, every other is a
// typed stale-version refusal.
func TestTodo_WF_RUN_015_DecisionStoreRace(t *testing.T) {
	s := newStore(t)
	const workers = 8
	conns := make([]*pgxadapter.Conn, workers)
	decisions := make([]Decision, workers)
	for i := range conns {
		conns[i] = s.conn()
		decisions[i] = s.decision("race-" + string(rune('a'+i)))
	}
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range conns {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = s.txErr(conns[i], func(tx dbport.Tx) error { return DecisionStore{}.Record(context.Background(), tx, decisions[i]) })
		}(i)
	}
	wg.Wait()
	landed := 0
	for i, err := range errs {
		switch {
		case err == nil:
			landed++
		case CodeOf(err) != CodeStaleVersion:
			t.Fatalf("worker %d: %v, want nil or %s", i, err, CodeStaleVersion)
		}
	}
	var n int
	if err := s.db.QueryRow(context.Background(), `SELECT count(*) FROM workflow_intervention_decision WHERE instance_id = $1`, s.inst.InstanceID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if landed != 1 || n != 1 {
		t.Fatalf("%d decisions landed, %d rows; want exactly one", landed, n)
	}
}

// TestTodo_WF_RUN_015_DecisionStoreMutation proves a row altered underneath
// the ledger (the trigger disabled by a superuser) is refused on read.
func TestTodo_WF_RUN_015_DecisionStoreMutation(t *testing.T) {
	s := newStore(t)
	conn := s.conn()
	s.tx(conn, func(tx dbport.Tx) error {
		return DecisionStore{}.Record(context.Background(), tx, s.decision("mutate-1"))
	})
	for name, stmt := range map[string]string{
		"sealed reason":  `UPDATE workflow_intervention_decision SET reason = 'rewritten'`,
		"lifted version": `UPDATE workflow_intervention_decision SET resulting_instance_version = resulting_instance_version + 5, reason = reason`,
	} {
		s.db.Exec(t, `ALTER TABLE workflow_intervention_decision DISABLE TRIGGER workflow_intervention_decision_forbid_mutation`)
		s.db.Exec(t, `CREATE TEMP TABLE IF NOT EXISTS iv_backup AS SELECT * FROM workflow_intervention_decision`)
		s.db.Exec(t, stmt)
		err := s.txErr(conn, func(tx dbport.Tx) error {
			_, err := DecisionStore{}.Load(context.Background(), tx, s.tenant, s.inst.InstanceID)
			return err
		})
		if CodeOf(err) != CodeDecisionMutated {
			t.Fatalf("%s: load = %v, want %s", name, err, CodeDecisionMutated)
		}
		s.db.Exec(t, `DELETE FROM workflow_intervention_decision`)
		s.db.Exec(t, `INSERT INTO workflow_intervention_decision SELECT * FROM iv_backup`)
		s.db.Exec(t, `ALTER TABLE workflow_intervention_decision ENABLE TRIGGER workflow_intervention_decision_forbid_mutation`)
	}
}
