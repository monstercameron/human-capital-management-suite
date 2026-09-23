package application

import (
	"context"
	"errors"
	"testing"

	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
)

// TestServedThresholdsProjectsTheReferenceTable proves the GetThresholdTable
// driver serves the compiled RULE-003 table, not a restatement of it: every
// identity field, input, row, condition and outcome below is read off the
// rules engine's own PromotionApprovalThresholdTable, so the served contract
// cannot drift from what the engine evaluates.
func TestServedThresholdsProjectsTheReferenceTable(t *testing.T) {
	port := newServedThresholds()
	table, err := port.GetThresholdTable(context.Background(), "tenant-acme", "")
	if err != nil {
		t.Fatalf("GetThresholdTable: %v", err)
	}
	if table.TableID != "hcmnext.rules.promotion_approval_threshold" {
		t.Fatalf("table id = %q", table.TableID)
	}
	if table.VersionRef != "2026.1" {
		t.Fatalf("version_ref = %q, want the rules release the rows come from", table.VersionRef)
	}
	if table.Version != thresholdTableContractVersion {
		t.Fatalf("version = %d, want the projection contract revision %d", table.Version, thresholdTableContractVersion)
	}
	if table.TenantID != "tenant-acme" {
		t.Fatalf("tenant = %q, want the caller's tenant stamped on the answer", table.TenantID)
	}
	if table.HitPolicy != transporthumanwork.ThresholdHitPolicyFirst {
		t.Fatalf("hit policy = %v, want FIRST", table.HitPolicy)
	}
	wantInputs := []string{"increase_percent", "band_position", "budget_authority", "grade_change"}
	if len(table.InputNames) != len(wantInputs) {
		t.Fatalf("inputs = %v, want %v", table.InputNames, wantInputs)
	}
	for i, name := range wantInputs {
		if table.InputNames[i] != name {
			t.Fatalf("inputs = %v, want %v", table.InputNames, wantInputs)
		}
	}
	if len(table.Rows) != 9 {
		t.Fatalf("rows = %d, want the reference table's 9", len(table.Rows))
	}
	// Outcomes in declared order: priority IS the contract under FIRST.
	wantOutcomes := []string{
		"UNKNOWN_BLOCKED", "UNKNOWN_BLOCKED", "FINANCE_REQUIRED",
		"EXECUTIVE_REQUIRED", "EXECUTIVE_REQUIRED", "FINANCE_REQUIRED",
		"FINANCE_REQUIRED", "FINANCE_REQUIRED", "STANDARD",
	}
	for i, want := range wantOutcomes {
		if table.Rows[i].Outcome != want {
			t.Fatalf("row %d outcome = %q, want %q", i+1, table.Rows[i].Outcome, want)
		}
	}
	first := table.Rows[0]
	assertConditions(t, "row 1", first.Conditions, []transporthumanwork.ThresholdCondition{
		{InputName: "budget_authority", Comparison: "EQUAL(UNKNOWN)"},
	})
	unknownBand := table.Rows[1]
	assertConditions(t, "row 2", unknownBand.Conditions, []transporthumanwork.ThresholdCondition{
		{InputName: "band_position", Comparison: "EQUAL(UNKNOWN)"},
	})
	largeRaise := table.Rows[4]
	assertConditions(t, "row 5", largeRaise.Conditions, []transporthumanwork.ThresholdCondition{
		{InputName: "increase_percent", Comparison: "GREATER_THAN(20.0000)"},
	})
	gradeChange := table.Rows[7]
	assertConditions(t, "row 8", gradeChange.Conditions, []transporthumanwork.ThresholdCondition{
		{InputName: "grade_change", Comparison: "EQUAL(true)"},
	})
	last := table.Rows[len(table.Rows)-1]
	if last.Outcome != "STANDARD" || len(last.Conditions) != 0 {
		t.Fatalf("otherwise row = %+v, want the empty catch-all", last)
	}

	// The default table answers under its own id too; anything else is an
	// absence, never an empty table.
	named, err := port.GetThresholdTable(context.Background(), "tenant-acme", "hcmnext.rules.promotion_approval_threshold")
	if err != nil {
		t.Fatalf("GetThresholdTable by id: %v", err)
	}
	if len(named.Rows) != len(table.Rows) {
		t.Fatal("the named and the default reads disagree")
	}
	if _, err := port.GetThresholdTable(context.Background(), "tenant-acme", "no-such-table"); !errors.Is(err, transporthumanwork.ErrThresholdNotFound) {
		t.Fatalf("unknown table err = %v, want ErrThresholdNotFound", err)
	}
	// A tenant's answer is its own: the table carries the requesting tenant.
	other, err := port.GetThresholdTable(context.Background(), "tenant-other", "")
	if err != nil {
		t.Fatalf("GetThresholdTable: %v", err)
	}
	if other.TenantID != "tenant-other" || other.TableID != table.TableID {
		t.Fatalf("tenant scoping leaked: %+v", other)
	}
}

func assertConditions(t *testing.T, name string, got, want []transporthumanwork.ThresholdCondition) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s conditions = %v, want %v", name, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s conditions = %v, want %v", name, got, want)
		}
	}
}
