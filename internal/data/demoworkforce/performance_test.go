package demoworkforce

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/performancestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestPlanPerformanceIsADeterministicRealisticRecord proves the planned
// performance record is arithmetic over real reviews rather than asserted
// ratings: every finalized rating validates against its own proposed rating
// and adjustment history, the distribution has a few top performers, mostly
// solid ratings and a couple below expectation, a worker's standing is the
// same in both completed cycles, and the current cycle is open with no
// finalized rating at all.
func TestPlanPerformanceIsADeterministicRealisticRecord(t *testing.T) {
	tenant := uuid.MustParse("6f0f7d0e-5f4c-4f1a-9a4e-0f6a6d2f9c31")
	plan, err := PlanPerformance(tenant)
	if err != nil {
		t.Fatalf("PlanPerformance: %v", err)
	}
	if len(plan.Cycles) != len(PerformanceCycles) {
		t.Fatalf("cycles = %d, want %d", len(plan.Cycles), len(PerformanceCycles))
	}
	employees, err := Plan(tenant)
	if err != nil {
		t.Fatal(err)
	}

	standings := make([]map[string]string, 0, 2)
	for _, cycle := range plan.Cycles {
		if len(cycle.Reviews) != 2*NewWorkerCount {
			t.Fatalf("%s reviews = %d, want %d", cycle.Spec.CycleID, len(cycle.Reviews), 2*NewWorkerCount)
		}
		if !cycle.Spec.Closed {
			if len(cycle.Revisions) != 2 || cycle.Revisions[1].State != performance.PerformanceCycleOpen {
				t.Fatalf("open cycle %s revisions = %+v", cycle.Spec.CycleID, cycle.Revisions)
			}
			if len(cycle.Sessions) != 0 {
				t.Fatalf("open cycle %s carries %d calibration sessions", cycle.Spec.CycleID, len(cycle.Sessions))
			}
			continue
		}
		if len(cycle.Revisions) != 4 || cycle.Revisions[3].State != performance.PerformanceCycleClosed {
			t.Fatalf("closed cycle %s revisions = %+v", cycle.Spec.CycleID, cycle.Revisions)
		}
		standing := map[string]string{}
		top, below, adjusted := 0, 0, 0
		for _, session := range cycle.Sessions {
			for _, rating := range session.Ratings {
				if err := rating.Validate(); err != nil {
					t.Fatalf("%s rating for %s does not validate: %v", cycle.Spec.CycleID, rating.ParticipantID, err)
				}
				standing[rating.ParticipantID] = rating.FinalRating.String()
				if performance.IsHighPerformer(rating) {
					top++
				}
				if rating.FinalRating.Cmp(mustRating(t, "3.00")) < 0 {
					below++
				}
				if len(rating.Adjustments) > 0 {
					adjusted++
				}
			}
		}
		if len(standing) != NewWorkerCount {
			t.Fatalf("%s finalized %d ratings, want %d", cycle.Spec.CycleID, len(standing), NewWorkerCount)
		}
		if top < 3 || top > 12 {
			t.Fatalf("%s high performers = %d, want a few out of %d", cycle.Spec.CycleID, top, NewWorkerCount)
		}
		if below < 1 || below > 6 {
			t.Fatalf("%s below expectation = %d, want a couple out of %d", cycle.Spec.CycleID, below, NewWorkerCount)
		}
		if adjusted != len(calibratedOrdinals) {
			t.Fatalf("%s calibration adjustments = %d, want %d", cycle.Spec.CycleID, adjusted, len(calibratedOrdinals))
		}
		standings = append(standings, standing)
	}
	if len(standings) != 2 {
		t.Fatalf("completed cycles = %d, want 2", len(standings))
	}
	for participant, rating := range standings[0] {
		if standings[1][participant] != rating {
			t.Fatalf("%s moved from %s to %s between cycles; standings must be stable", participant, rating, standings[1][participant])
		}
	}

	// HC-21051 is the demo promotion subject; the routing predicate needs a
	// subject it actually fires for.
	subject := ""
	for _, employee := range employees {
		if employee.Row.WorkerNumber == "HC-21051" {
			subject = employee.Row.WorkerID.String()
		}
	}
	if subject == "" {
		t.Fatal("the demo workforce has no HC-21051")
	}
	if standings[1][subject] != "5.00" {
		t.Fatalf("HC-21051 final rating = %q, want 5.00", standings[1][subject])
	}

	again, err := PlanPerformance(tenant)
	if err != nil {
		t.Fatal(err)
	}
	if again.Cycles[1].Sessions[0].Session.CanonicalDigest != plan.Cycles[1].Sessions[0].Session.CanonicalDigest {
		t.Fatal("PlanPerformance is not deterministic: the same tenant produced a different session digest")
	}
}

func mustRating(t *testing.T, text string) values.Decimal {
	t.Helper()
	rating, err := values.NewDecimal(text, PerformanceRatingScale, values.RoundingExactRequired)
	if err != nil {
		t.Fatalf("decimal %q: %v", text, err)
	}
	return rating
}

// TestSeedPerformanceLandsTenantScopedRowsAndReplaysAsANoOp proves the
// performance record lands in migration 00108's tables, that every row is
// scoped to the seeding tenant, and that a second seed writes nothing.
func TestSeedPerformanceLandsTenantScopedRowsAndReplaysAsANoOp(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedAggregateTenant(t, db)
	otherTenant := seedAggregateTenant(t, db)
	ctx := context.Background()

	var first PerformanceSummary
	inAggregateTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		first, err = SeedPerformance(ctx, tx, tenantID)
		return err
	})
	closedCycles := 0
	for _, spec := range PerformanceCycles {
		if spec.Closed {
			closedCycles++
		}
	}
	wantRatings := closedCycles * NewWorkerCount
	if first.FinalRatings != wantRatings || first.RatingCases != wantRatings || first.RatingEvents != wantRatings {
		t.Fatalf("first seed = %+v, want %d final ratings, cases and events", first, wantRatings)
	}
	if first.Reviews != len(PerformanceCycles)*2*NewWorkerCount || first.Skipped != 0 {
		t.Fatalf("first seed = %+v", first)
	}
	// A closed cycle records PLANNED, OPEN, CALIBRATING and CLOSED; an open
	// one records PLANNED and OPEN.
	if want := 4*closedCycles + 2*(len(PerformanceCycles)-closedCycles); first.CycleRevisions != want {
		t.Fatalf("cycle revisions = %d, want %d", first.CycleRevisions, want)
	}

	var second PerformanceSummary
	inAggregateTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		second, err = SeedPerformance(ctx, tx, tenantID)
		return err
	})
	if second.FinalRatings != 0 || second.CycleRevisions != 0 || second.Reviews != 0 || second.Sessions != 0 {
		t.Fatalf("replayed seed wrote rows: %+v", second)
	}
	if second.Skipped != first.CycleRevisions+first.Reviews+first.Sessions+first.RatingCases+first.RatingEvents+first.FinalRatings {
		t.Fatalf("replayed seed = %+v; every planned row should have been skipped", second)
	}

	var scoped, leaked int
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM performance_final_rating WHERE tenant_id = $1`, tenantID).Scan(&scoped); err != nil {
		t.Fatal(err)
	}
	if scoped != wantRatings {
		t.Fatalf("final ratings for the seeded tenant = %d, want %d", scoped, wantRatings)
	}
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM performance_final_rating WHERE tenant_id <> $1`, tenantID).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatalf("%d performance rows landed outside the seeded tenant", leaked)
	}

	// The seeded record is readable as a calibrated rating through the
	// production lookup path, for the seeding tenant only.
	employees, err := Plan(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	subject := ""
	for _, employee := range employees {
		if employee.Row.WorkerNumber == "HC-21051" {
			subject = employee.Row.WorkerID.String()
		}
	}
	inAggregateTx(t, db, tenantID, func(tx dbport.Tx) error {
		rating, err := performancestore.LookupCalibratedRatingTx(ctx, tx, tenantID, subject)
		if err != nil {
			t.Fatalf("lookup the seeded subject: %v", err)
		}
		if !performance.IsHighPerformer(rating) {
			t.Fatalf("seeded subject rating %s is not a high performer", rating.FinalRating)
		}
		return nil
	})
	inAggregateTx(t, db, otherTenant, func(tx dbport.Tx) error {
		if _, err := performancestore.LookupCalibratedRatingTx(ctx, tx, otherTenant, subject); err == nil {
			t.Fatal("another tenant read the seeded subject's calibrated rating")
		}
		return nil
	})

	if _, err := SeedPerformance(ctx, nil, tenantID); err == nil {
		t.Fatal("a performance seed without a transaction was accepted")
	}
}
