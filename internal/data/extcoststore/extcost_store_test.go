package extcoststore

import (
	"context"
	"errors"
	"fmt"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/extcost"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type extcostAuthority struct{}

func (extcostAuthority) AuthorizeExternalUsage(_ context.Context, tenant, actor, action string, _ extcost.Filter) error {
	if tenant == "" || actor == "" || (action != "call" && actor != "admin") {
		return extcost.ErrDenied
	}
	return nil
}
func extcostDate() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
func extcostSchedule() extcost.Schedule {
	return extcost.Schedule{Version: "fixture-v1", Provider: "simulator", Operation: "request", Currency: "USD", Authority: "admin", Source: "fixture-only", EffectiveFrom: extcostDate().Add(-time.Hour), ReviewAt: extcostDate().Add(time.Hour), Rates: []extcost.Rate{{Unit: extcost.Requests, Tiers: []extcost.Tier{{Micros: 10, Per: 1}}}}}
}
func extcostCall(k string) extcost.Call {
	return extcost.Call{Tenant: "tenant-a", LegalEntity: "entity", Feature: "answers", Provider: "simulator", Operation: "request", Purpose: "answers", DataClasses: []string{"internal"}, Key: k, Cause: "cause", RequestDigest: strings.Repeat("a", 64), Attribution: extcost.Attribution{Actor: "person", Agent: "agent", AgentRun: "run", Step: "step", WorkflowDefinition: "workflow", WorkflowRun: "workflow-run", Node: "node"}}
}
func extcostConnection(t *testing.T, db *pgtest.DB) *Store {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE hcmnext_app"); err != nil {
		t.Fatal(err)
	}
	s, err := New(conn)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type extcostWarningSink struct{}

func (extcostWarningSink) NotifyExternalBudgetWarning(_ context.Context, c extcost.Call, b extcost.BudgetStatus) error {
	if !b.Warning || b.Budget.Tenant != c.Tenant {
		return extcost.ErrInvalid
	}
	return nil
}
func extcostGateway(t *testing.T, s *Store) *extcost.Gateway {
	t.Helper()
	c, err := extcost.NewCatalog([]extcost.Schedule{extcostSchedule()})
	if err != nil {
		t.Fatal(err)
	}
	g, err := extcost.NewGatewayWithWarnings(s, c, extcostAuthority{}, []extcost.Operation{{Provider: "simulator", Name: "request"}}, extcostDate, extcostWarningSink{})
	if err != nil {
		t.Fatal(err)
	}
	return g
}
func extcostEnv(t *testing.T) (*pgtest.DB, *Store, *extcost.Gateway) {
	t.Helper()
	db := pgtest.New(t)
	raw, err := Migrations.ReadFile("migrations/00001_extcost_ledger.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.Split(strings.Split(string(raw), "-- +goose Up")[1], "-- +goose Down")[0]
	db.Exec(t, up)
	db.Exec(t, fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO hcmnext_app", db.Schema))
	s := extcostConnection(t, db)
	g := extcostGateway(t, s)
	for _, scope := range []extcost.Scope{{Kind: "tenant", ID: "tenant-a", Period: "month"}, {Kind: "feature", ID: "answers", Period: "month"}} {
		if err := g.ChangeBudget(context.Background(), extcost.BudgetChange{Budget: extcost.Budget{Tenant: "tenant-a", Scope: scope, Currency: "USD", LimitMicros: 100, WarningBasisPoints: 8000}, Actor: "admin", Reason: "test approval"}); err != nil {
			t.Fatal(err)
		}
	}
	return db, s, g
}
func extcostInvoke(n int64) extcost.Invoke {
	return func(context.Context) (extcost.Measurement, error) {
		return extcost.Measurement{Units: extcost.Units{extcost.Requests: n}, Billed: true, Outcome: "succeeded", ResultRef: "result-id"}, nil
	}
}
func TestTodo_EXTCOST_002_Integration(t *testing.T) {
	db, s, g := extcostEnv(t)
	l, err := g.Run(context.Background(), extcostCall("a"), extcost.Units{extcost.Requests: 3}, extcostInvoke(2))
	if err != nil || l.CostMicros != 20 {
		t.Fatalf("record %+v %v", l, err)
	}
	r, err := g.Read(context.Background(), "tenant-a", "admin", extcost.Filter{})
	if err != nil || len(r.Lines) != 1 || r.Lines[0].Measurement.ResultRef != "result-id" {
		t.Fatal("journal result", err)
	}
	for _, b := range r.Budgets {
		if b.SpentMicros != 20 || b.ReservedMicros != 0 {
			t.Fatal("settle failed to release", b)
		}
	}
	if db.ExecErr("UPDATE extcost_usage SET cost_micros=0") == nil || db.ExecErr("DELETE FROM extcost_usage") == nil {
		t.Fatal("append-only journal changed")
	}
	pending, err := s.Pending(context.Background(), "tenant-a")
	if err != nil || len(pending) != 0 {
		t.Fatal("pending", err)
	}
	t.Log("record, result journal and append-only enforcement PASS")
}
func TestTodo_EXTCOST_002_Property(t *testing.T) {
	_, _, g := extcostEnv(t)
	calls := 0
	invoke := func(ctx context.Context) (extcost.Measurement, error) { calls++; return extcostInvoke(2)(ctx) }
	for _, key := range []string{"a", "a", "b", "b"} {
		_, err := g.Run(context.Background(), extcostCall(key), extcost.Units{extcost.Requests: 2}, invoke)
		if err != nil && !errors.Is(err, extcost.ErrReplay) {
			t.Fatal(err)
		}
	}
	r, err := g.Read(context.Background(), "tenant-a", "admin", extcost.Filter{Cause: "cause"})
	if err != nil || calls != 2 || len(r.Lines) != 2 {
		t.Fatal("replay", calls, len(r.Lines), err)
	}
	sum := int64(0)
	for _, l := range r.Lines {
		sum += l.CostMicros
	}
	if sum != 40 {
		t.Fatal("cause sum", sum)
	}
	c := extcostCall("a")
	c.Attribution.Actor = "forged"
	if _, err := g.Run(context.Background(), c, extcost.Units{extcost.Requests: 2}, invoke); !errors.Is(err, extcost.ErrConflict) {
		t.Fatal("forged replay")
	}
	t.Log("idempotent retry, separate billed attempts and cause sum PASS")
}
func TestTodo_EXTCOST_005_Integration(t *testing.T) {
	_, _, g := extcostEnv(t)
	if _, err := g.Run(context.Background(), extcostCall("warning"), extcost.Units{extcost.Requests: 8}, extcostInvoke(8)); err != nil {
		t.Fatal(err)
	}
	r, err := g.Read(context.Background(), "tenant-a", "admin", extcost.Filter{From: extcostDate()})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range r.Budgets {
		if !b.Warning || b.SpentMicros != 80 || b.ReservedMicros != 0 {
			t.Fatal("warning", b)
		}
	}
	called := false
	_, err = g.Run(context.Background(), extcostCall("stop"), extcost.Units{extcost.Requests: 3}, func(context.Context) (extcost.Measurement, error) { called = true; return extcost.Measurement{}, nil })
	if !errors.Is(err, extcost.ErrBudget) || called {
		t.Fatal("hard stop", err)
	}
	t.Log("budget warning and hard stop before provider dispatch PASS")
}
func TestTodo_EXTCOST_005_Property(t *testing.T) {
	db, s, g := extcostEnv(t)
	g2 := extcostGateway(t, extcostConnection(t, db))
	started, finish := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	var first error
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, first = g.Run(context.Background(), extcostCall("a"), extcost.Units{extcost.Requests: 7}, func(context.Context) (extcost.Measurement, error) {
			close(started)
			<-finish
			return extcostInvoke(4)(context.Background())
		})
	}()
	<-started
	called := false
	_, err := g2.Run(context.Background(), extcostCall("b"), extcost.Units{extcost.Requests: 4}, func(context.Context) (extcost.Measurement, error) { called = true; return extcost.Measurement{}, nil })
	close(finish)
	wg.Wait()
	if first != nil || !errors.Is(err, extcost.ErrBudget) || called {
		t.Fatal("concurrent reserve", first, err)
	}
	if _, err := g2.Run(context.Background(), extcostCall("b"), extcost.Units{extcost.Requests: 4}, extcostInvoke(4)); err != nil {
		t.Fatal(err)
	}
	pending, err := s.Pending(context.Background(), "tenant-a")
	if err != nil || len(pending) != 0 {
		t.Fatal("reservation leaked", err)
	}
	t.Log("concurrent reservations and release of unused maximum PASS")
}
func TestTodo_EXTCOST_005_Fault(t *testing.T) {
	db, s, g := extcostEnv(t)
	_, err := g.Run(context.Background(), extcostCall("crash"), extcost.Units{extcost.Requests: 6}, func(context.Context) (extcost.Measurement, error) {
		return extcost.Measurement{}, errors.New("reply lost")
	})
	if !errors.Is(err, extcost.ErrPending) {
		t.Fatal(err)
	}
	boot := extcostConnection(t, db)
	pending, err := boot.Pending(context.Background(), "tenant-a")
	if err != nil || len(pending) != 1 {
		t.Fatal("crash lost reservation", err)
	}
	restarted := extcostGateway(t, boot)
	called := false
	_, err = restarted.Run(context.Background(), extcostCall("crash"), extcost.Units{extcost.Requests: 6}, func(context.Context) (extcost.Measurement, error) { called = true; return extcost.Measurement{}, nil })
	if !errors.Is(err, extcost.ErrPending) || called {
		t.Fatal("uncertain attempt replayed")
	}
	l, err := restarted.Recover(context.Background(), "admin", pending[0], extcost.Measurement{Units: extcost.Units{extcost.Requests: 2}, Billed: true, Outcome: "succeeded"})
	if err != nil || l.CostMicros != 20 {
		t.Fatal("recover", err)
	}
	pending, err = s.Pending(context.Background(), "tenant-a")
	if err != nil || len(pending) != 0 {
		t.Fatal("recover leaked", err)
	}
	t.Log("durable crash recovery PASS")
}
func TestTodo_EXTCOST_002_Security(t *testing.T) {
	db, s, g := extcostEnv(t)
	if _, err := g.Run(context.Background(), extcostCall("a"), extcost.Units{extcost.Requests: 1}, extcostInvoke(1)); err != nil {
		t.Fatal(err)
	}
	err := s.transaction(context.Background(), "tenant-b", false, func(tx dbport.Tx) error {
		var n int64
		if err := tx.QueryRow(context.Background(), "SELECT COUNT(*) FROM extcost_usage").Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Fatal("RLS disclosed tenant-a")
		}
		_, err := tx.Exec(context.Background(), `INSERT INTO extcost_budget (tenant_id,kind,scope_id,period,currency,limit_micros,warning_basis_points) VALUES ('tenant-a','agent','forged','month','USD',100,8000)`)
		if err == nil {
			t.Fatal("cross-tenant write accepted")
		}
		return err
	})
	if err == nil {
		t.Fatal("RLS write committed")
	}
	if _, err := g.Read(context.Background(), "tenant-a", "person", extcost.Filter{}); !errors.Is(err, extcost.ErrDenied) {
		t.Fatal("unrestricted admin read")
	}
	var n int64
	if err := db.QueryRow(context.Background(), `SELECT COUNT(*) FROM pg_policies WHERE schemaname=$1 AND tablename LIKE 'extcost_%' AND policyname='tenant_isolation'`, db.Schema).Scan(&n); err != nil || n != 6 {
		t.Fatal("RLS policies", n, err)
	}
}
func TestTodo_EXTCOST_004_Integration(t *testing.T) {
	_, s, g := extcostEnv(t)
	schedule := extcostSchedule()
	if err := s.SaveSchedule(context.Background(), "tenant-a", schedule); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSchedule(context.Background(), "tenant-a", schedule); err != nil {
		t.Fatal("schedule replay", err)
	}
	changed := schedule
	changed.MinimumMicros = 5
	if err := s.SaveSchedule(context.Background(), "tenant-a", changed); !errors.Is(err, extcost.ErrConflict) {
		t.Fatal("old price changed")
	}
	catalog, err := s.LoadCatalog(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	q, err := catalog.Quote(extcostCall("a"), extcost.Units{extcost.Requests: 2}, extcostDate())
	if err != nil || q.CostMicros != 20 {
		t.Fatal("loaded price", err)
	}
	if _, err := g.Run(context.Background(), extcostCall("a"), extcost.Units{extcost.Requests: 2}, extcostInvoke(2)); err != nil {
		t.Fatal(err)
	}
	report, err := g.Read(context.Background(), "tenant-a", "admin", extcost.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	correction := report.Lines[0]
	correction.Call.Key = "correction"
	correction.CorrectionOf = "a"
	correction.CostMicros = -5
	correction.Measurement.Billed = false
	r := extcost.Reconciliation{Tenant: "tenant-a", Provider: "simulator", Operation: "request", Currency: "USD", ReportID: "bill-day", Day: extcostDate(), LedgerMicros: 20, ProviderMicros: 15, CorrectionKey: "correction"}
	for i := 0; i < 2; i++ {
		if err := g.Reconcile(context.Background(), "admin", r, &correction); err != nil {
			t.Fatal("reconcile", err)
		}
	}
	after, err := g.Read(context.Background(), "tenant-a", "admin", extcost.Filter{})
	if err != nil || len(after.Lines) != 2 || len(after.Reconciliations) != 1 {
		t.Fatal("reconcile result", err)
	}
	for _, group := range after.Groups {
		if group.Kind == "total" && (group.CostMicros != 15 || group.Calls != 1) {
			t.Fatal("corrected total", group)
		}
	}
	scoped, err := g.Read(context.Background(), "tenant-a", "admin", extcost.Filter{Agent: "agent"})
	if err != nil || len(scoped.Reconciliations) != 0 || !scoped.ReconciledAt["a"].Equal(extcostDate()) {
		t.Fatal("scoped reconciliation status lost or provider-wide billing exposed", scoped.ReconciledAt, err)
	}
	t.Log("immutable versioned prices and append-only reconciliation PASS")
}
func TestTodo_EXTCOST_006(t *testing.T) {
	_, _, g := extcostEnv(t)
	for _, key := range []string{"a", "b"} {
		c := extcostCall(key)
		if key == "b" {
			c.Attribution.Agent = "another-agent"
			c.Attribution.WorkflowRun = "other-run"
		}
		if _, err := g.Run(context.Background(), c, extcost.Units{extcost.Requests: 2}, extcostInvoke(2)); err != nil {
			t.Fatal(err)
		}
	}
	r, err := g.Read(context.Background(), "tenant-a", "admin", extcost.Filter{Agent: "agent", Search: "simulator", From: extcostDate().Add(-time.Hour), Until: extcostDate().Add(time.Hour)})
	if err != nil || len(r.Lines) != 1 {
		t.Fatal("filtered read", err)
	}
	for _, kind := range []string{"day", "feature", "agent", "person", "cause"} {
		found := false
		for _, group := range r.Groups {
			if group.Kind == kind && group.CostMicros == 20 && group.Calls == 1 {
				found = true
			}
		}
		if !found {
			t.Fatal("missing aggregate", kind)
		}
	}
	all, err := g.Read(context.Background(), "tenant-a", "admin", extcost.Filter{Limit: 1})
	if err != nil || !all.Truncated || len(all.Lines) != 1 {
		t.Fatal("truncation", err)
	}
	for _, group := range all.Groups {
		if group.Kind == "total" && group.CostMicros != 40 {
			t.Fatal("total truncated with list")
		}
	}
	if len(all.Statistics) != 2 || all.Statistics[0].MedianMicros != 20 || all.Statistics[0].P95Micros != 20 {
		t.Fatal("workflow percentiles", all.Statistics)
	}
	empty, err := g.Read(context.Background(), "tenant-b", "admin", extcost.Filter{})
	if err != nil || len(empty.Lines) != 0 {
		t.Fatal("tenant isolation", err)
	}
	t.Log("cost by feature/day/agent, top callers, complete totals and workflow percentiles PASS")
}
func TestTodo_EXTCOST_006_Overflow(t *testing.T) {
	line := extcost.Line{Call: extcostCall("a"), Currency: "USD", CostMicros: math.MaxInt64, At: extcostDate()}
	other := line
	other.Call.Key = "b"
	other.CostMicros = 1
	if _, err := Groups([]extcost.Line{line, other}); !errors.Is(err, extcost.ErrOverflow) {
		t.Fatal("read sum overflow")
	}
	other.Currency = "EUR"
	groups, err := Groups([]extcost.Line{line, other})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, g := range groups {
		if g.Kind == "total" {
			n++
		}
	}
	if n != 2 {
		t.Fatal("currencies silently combined")
	}
}

func TestTodo_EXTCOST_005_Scopes(t *testing.T) {
	_, _, g := extcostEnv(t)
	ctx := context.Background()
	for _, scope := range []extcost.Scope{{Kind: "agent", ID: "agent", Period: "month"}, {Kind: "run", ID: "run", Period: "run"}, {Kind: "node", ID: "workflow-run:node", Period: "day"}, {Kind: "step", ID: "run:step", Period: "run"}, {Kind: "workflow", ID: "workflow", Period: "month"}} {
		if err := g.ChangeBudget(ctx, extcost.BudgetChange{Budget: extcost.Budget{Tenant: "tenant-a", Scope: scope, Currency: "USD", LimitMicros: 20, WarningBasisPoints: 8000}, Actor: "admin", Reason: "scoped ceiling"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := g.Run(ctx, extcostCall("first"), extcost.Units{extcost.Requests: 1}, extcostInvoke(1)); err != nil {
		t.Fatal(err)
	}
	called := false
	_, err := g.Run(ctx, extcostCall("second"), extcost.Units{extcost.Requests: 2}, func(context.Context) (extcost.Measurement, error) { called = true; return extcost.Measurement{}, nil })
	if !errors.Is(err, extcost.ErrBudget) || called {
		t.Fatal("nested budget failed", err)
	}
	if err := g.ChangeBudget(ctx, extcost.BudgetChange{Budget: extcost.Budget{Tenant: "tenant-a", Scope: extcost.Scope{Kind: "agent", ID: "agent", Period: "month"}, Currency: "USD", LimitMicros: 5, WarningBasisPoints: 8000}, Actor: "admin", Reason: "invalid reduction"}); !errors.Is(err, extcost.ErrBudget) {
		t.Fatal("budget below settled spend")
	}
	t.Log("tenant/feature/agent/workflow/run/node/step reservations and daily/monthly/run periods PASS")
}
func TestTodo_EXTCOST_005_Overrun(t *testing.T) {
	_, _, g := extcostEnv(t)
	l, err := g.Run(context.Background(), extcostCall("overrun"), extcost.Units{extcost.Requests: 1}, extcostInvoke(2))
	if !errors.Is(err, extcost.ErrOverrun) || l.CostMicros != 20 || l.Finding != "reserved_maximum_exceeded" {
		t.Fatal("provider overrun was hidden", err)
	}
	called := false
	_, err = g.Run(context.Background(), extcostCall("blocked"), extcost.Units{extcost.Requests: 1}, func(context.Context) (extcost.Measurement, error) { called = true; return extcost.Measurement{}, nil })
	if !errors.Is(err, extcost.ErrOverrun) || called {
		t.Fatal("overrun did not fail closed", err)
	}
	t.Log("actual provider overrun retained and further dispatch refused PASS")
}
