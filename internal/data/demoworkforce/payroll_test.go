package demoworkforce

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/payrollstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll"
)

// TestPlanPayrollCoversEveryPeriodAndRespectsHireDates proves the planned
// history walks the real run lifecycle, that the frozen population agrees
// with each worker's pay basis and hire date, and that a worker hired inside
// a period is recorded as an explicit late entry rather than a full-period
// member.
func TestPlanPayrollCoversEveryPeriodAndRespectsHireDates(t *testing.T) {
	tenant := uuid.MustParse("0d5f5a8e-2b8b-4a6f-8f7e-1c4c2f2ad9b4")
	plan, err := PlanPayroll(tenant)
	if err != nil {
		t.Fatalf("PlanPayroll: %v", err)
	}
	if len(plan.Runs) != len(DemoPayPeriods) {
		t.Fatalf("runs = %d, want %d", len(plan.Runs), len(DemoPayPeriods))
	}
	for _, run := range plan.Runs {
		states := make([]payroll.PayrollRunState, 0, len(run.Revisions))
		for _, revision := range run.Revisions {
			if err := revision.Validate(); err != nil {
				t.Fatalf("%s revision %d: %v", revision.RunID, revision.Revision, err)
			}
			states = append(states, revision.State)
		}
		want := []payroll.PayrollRunState{payroll.PayrollRunStateDraft, payroll.PayrollRunStateCalculated,
			payroll.PayrollRunStateReleased, payroll.PayrollRunStateSettled}
		if len(states) != len(want) {
			t.Fatalf("%s lifecycle = %v", run.Period.ID, states)
		}
		for index := range want {
			if states[index] != want[index] {
				t.Fatalf("%s lifecycle = %v, want %v", run.Period.ID, states, want)
			}
		}
		if err := run.Population.Validate(); err != nil {
			t.Fatalf("%s population: %v", run.Period.ID, err)
		}
		if len(run.Population.Members) != NewWorkerCount {
			t.Fatalf("%s members = %d, want %d", run.Period.ID, len(run.Population.Members), NewWorkerCount)
		}
		if run.Population.RunRevision != 1 || run.Population.State != payroll.PopulationStateFrozen {
			t.Fatalf("%s population = revision %d state %s", run.Period.ID, run.Population.RunRevision, run.Population.State)
		}
	}

	// Every HarborCare worker was hired years before the seeded periods, so
	// the mid-period rule is proved on its own terms: a worker hired inside
	// a period is a late entry and never a full-period member, and one hired
	// after it is absent altogether.
	period := DemoPayPeriods[0]
	midPeriod := payrollRow("hc-900-mid-period", "hc-emp-90000", period.Start.AddDate(0, 0, 10))
	later := payrollRow("hc-901-not-yet", "hc-emp-90001", period.End.AddDate(0, 0, 3))
	hourly := payrollRow("hc-902-hourly", "hc-emp-90002", period.Start.AddDate(-1, 0, 0))
	hourly.PayBasis = "HOURLY_RATE"
	baseline := payrollRow("hc-903-established", "hc-emp-90003", period.Start.AddDate(-2, 0, 0))

	mixed, err := PlanPayrollFor([]workforce.WorkerRow{baseline, midPeriod, later, hourly})
	if err != nil {
		t.Fatalf("PlanPayrollFor: %v", err)
	}
	first := mixed.Runs[0]
	if len(first.Population.Members) != 1 || first.Population.Members[0].EmploymentRef != baseline.EmploymentID {
		t.Fatalf("frozen members = %+v; only the established salaried worker belongs", first.Population.Members)
	}
	if len(first.LateEntries) != 1 || first.LateEntries[0].Member.EmploymentRef != midPeriod.EmploymentID {
		t.Fatalf("late entries = %+v; the mid-period hire must be an explicit amendment", first.LateEntries)
	}
	if first.LateEntries[0].Kind != payroll.PopulationAmendmentLateEntry || first.LateEntries[0].Digest == "" {
		t.Fatalf("late entry = %+v", first.LateEntries[0])
	}
	// The next period covers the first mid-period hire in full, and treats
	// the worker who started inside it as that period's late entry.
	second := mixed.Runs[1]
	if len(second.Population.Members) != 2 || len(second.LateEntries) != 1 {
		t.Fatalf("the period after the hire = %d members, %d late entries", len(second.Population.Members), len(second.LateEntries))
	}
	if second.LateEntries[0].Member.EmploymentRef != later.EmploymentID {
		t.Fatalf("second period late entry = %+v, want %s", second.LateEntries[0].Member, later.EmploymentID)
	}
	// By the third period everybody salaried is established.
	third := mixed.Runs[2]
	if len(third.Population.Members) != 3 || len(third.LateEntries) != 0 {
		t.Fatalf("the third period = %d members, %d late entries", len(third.Population.Members), len(third.LateEntries))
	}
}

func payrollRow(key, employment string, hire time.Time) workforce.WorkerRow {
	return workforce.WorkerRow{
		TenantID: uuid.MustParse("0d5f5a8e-2b8b-4a6f-8f7e-1c4c2f2ad9b4"),
		WorkerID: deterministicID("worker", key), WorkerKey: key,
		EmploymentID: employment, PayBasis: DemoPayBasis,
		HireDate: hire.UTC().Format(workforce.DateLayout),
	}
}

// TestSeedPayrollLandsTenantScopedRunsAndReplaysAsANoOp proves the payroll
// history lands through payrollstore, is scoped to the seeding tenant, and
// that a second seed adds nothing.
func TestSeedPayrollLandsTenantScopedRunsAndReplaysAsANoOp(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedAggregateTenant(t, db)
	ctx := context.Background()

	var first PayrollSummary
	inAggregateTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		first, err = SeedPayroll(ctx, tx, tenantID)
		return err
	})
	if first.Runs != len(DemoPayPeriods) || first.RunRevisions != 4*len(DemoPayPeriods) {
		t.Fatalf("first seed = %+v", first)
	}
	if first.Populations != len(DemoPayPeriods) || first.Members != NewWorkerCount*len(DemoPayPeriods) || first.SkippedRuns != 0 {
		t.Fatalf("first seed = %+v", first)
	}

	var second PayrollSummary
	inAggregateTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		second, err = SeedPayroll(ctx, tx, tenantID)
		return err
	})
	if second.Runs != 0 || second.RunRevisions != 0 || second.Populations != 0 || second.SkippedRuns != len(DemoPayPeriods) {
		t.Fatalf("replayed seed = %+v; every run should have been skipped", second)
	}

	var runs, populations, leaked int
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM payroll_run WHERE tenant_id = $1`, tenantID).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if runs != 4*len(DemoPayPeriods) {
		t.Fatalf("payroll_run rows = %d, want %d", runs, 4*len(DemoPayPeriods))
	}
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM payroll_frozen_population WHERE tenant_id = $1`, tenantID).Scan(&populations); err != nil {
		t.Fatal(err)
	}
	if populations != len(DemoPayPeriods) {
		t.Fatalf("frozen populations = %d, want %d", populations, len(DemoPayPeriods))
	}
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM payroll_run WHERE tenant_id <> $1`, tenantID).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatalf("%d payroll rows landed outside the seeded tenant", leaked)
	}

	// The stored history reads back through the same store the seed wrote it
	// with: the settled revision and the population it names.
	store := payrollstore.Store{}
	runID := PayrollRunID(DemoPayPeriods[len(DemoPayPeriods)-1])
	inAggregateTx(t, db, tenantID, func(tx dbport.Tx) error {
		settled, err := store.LoadRunTx(ctx, tx, tenantID, runID, 4)
		if err != nil || settled.State != payroll.PayrollRunStateSettled {
			t.Fatalf("settled revision = %+v, %v", settled, err)
		}
		population, err := store.LoadPopulationTx(ctx, tx, tenantID, runID, 1)
		if err != nil || len(population.Members) != NewWorkerCount {
			t.Fatalf("frozen population = %d members, %v", len(population.Members), err)
		}
		if population.PayGroupRef != DemoPayGroupRef {
			t.Fatalf("pay group = %q, want %q", population.PayGroupRef, DemoPayGroupRef)
		}
		return nil
	})

	if _, err := SeedPayroll(ctx, nil, tenantID); err == nil {
		t.Fatal("a payroll seed without a transaction was accepted")
	}
}
