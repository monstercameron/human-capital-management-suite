package application

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	kernelvalues "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestComposedDemoTenantCarriesItsPerformancePayrollAndRoleData proves the
// serve composition actually seeds the three datasets and binds the
// calibrated-rating lookup, so the high-performer routing predicate has
// something to read in a served cell instead of always taking its "no
// lookup" branch.
func TestComposedDemoTenantCarriesItsPerformancePayrollAndRoleData(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	pool, err := pgxadapter.NewPool(ctx, db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	cfg := ServeConfig{
		GRPCListen: "127.0.0.1:0", HTTPListen: "127.0.0.1:0", DatabaseURL: db.URL,
		DevHMACKey: "demo-data-composition-signing-key-000000", PageCursorKey: integrationPageCursorKey,
		Issuer: DefaultIssuer, Audience: DefaultAudience,
		Tenant: demoworkforce.CompanyKey, CellID: "cell-demo-data", MaxDeadline: 60 * time.Second,
		Workspace: true, DevBrowserLogin: true, OTelExporter: OTelExporterNone,
		WorkflowPlan: WorkflowPlanExecute, TimerTzdbVersion: DefaultTimerTzdbVersion,
		TimerCalendarVersion: DefaultTimerCalendarVersion,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("configuration: %v", err)
	}
	if _, err := ComposeServe(ctx, ServeInput{Config: cfg, Pool: pool, Identity: "demo-data"}); err != nil {
		t.Fatalf("ComposeServe: %v", err)
	}
	tenantID := pgstore.TenantID(cfg.Tenant)

	for _, expectation := range []struct {
		table string
		want  int
	}{
		{"performance_final_rating", 2 * demoworkforce.NewWorkerCount},
		{"payroll_run", 4 * len(demoworkforce.DemoPayPeriods)},
		{"payroll_frozen_population", len(demoworkforce.DemoPayPeriods)},
		{"worker_access_role_set", demoworkforce.NewWorkerCount},
	} {
		var rows int
		if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM `+expectation.table+` WHERE tenant_id = $1`, tenantID).Scan(&rows); err != nil {
			t.Fatalf("count %s: %v", expectation.table, err)
		}
		if rows != expectation.want {
			t.Fatalf("%s rows = %d, want %d", expectation.table, rows, expectation.want)
		}
	}

	// The composed lookup resolves the demo promotion subject's calibrated
	// rating, and that rating is top-band, which is the whole point of
	// binding it.
	lookup := composeCalibratedRatings(pool)
	if lookup == nil {
		t.Fatal("the serve composition binds no calibrated-rating lookup")
	}
	employees, err := demoworkforce.Plan(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	subject := ""
	for _, employee := range employees {
		if employee.Row.WorkerNumber == "HC-21051" {
			subject = employee.Row.WorkerID.String()
		}
	}
	rating, err := lookup.LookupCalibratedRating(ctx, kernelvalues.TenantId(cfg.Tenant), subject)
	if err != nil {
		t.Fatalf("LookupCalibratedRating: %v", err)
	}
	if !performance.IsHighPerformer(rating) {
		t.Fatalf("the demo promotion subject's rating %s is not top-band", rating.FinalRating)
	}

	// A composition with no pool reads no ratings and seeds no assignments,
	// so a cell without a database behaves exactly as it did before.
	if composeCalibratedRatings(nil) != nil {
		t.Fatal("a poolless composition bound a calibrated-rating lookup")
	}
	empty, err := bootstrapLocalDevRoleAssignments(ctx, nil, cfg.Tenant)
	if err != nil || empty.Workers != 0 {
		t.Fatalf("a poolless role assignment seed = %+v, %v", empty, err)
	}
	other, err := bootstrapLocalDevRoleAssignments(ctx, pool, "some-other-tenant")
	if err != nil || other.Workers != 0 {
		t.Fatalf("a non-demo tenant role assignment seed = %+v, %v", other, err)
	}

	// The seed is replay-safe through the composition: composing twice against
	// the same database adds nothing.
	replayed, err := bootstrapLocalDevRoleAssignments(ctx, pool, cfg.Tenant)
	if err != nil {
		t.Fatalf("replay the role assignment seed: %v", err)
	}
	if replayed.Workers != 0 || replayed.Skipped != demoworkforce.NewWorkerCount {
		t.Fatalf("replayed role assignment seed = %+v", replayed)
	}
}
