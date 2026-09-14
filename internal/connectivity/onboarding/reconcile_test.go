package onboarding_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/onboarding"
)

// reconcileFixture has equal aggregate counts on both sides while hiding a
// swapped title, a shifted effective date, an observation disagreement, a
// missing and an unexpected subject.
func reconcileFixture() ([]onboarding.SourceRow, []onboarding.TargetRecord) {
	source := []onboarding.SourceRow{
		{RowID: "r1", SubjectID: "emp-1", EffectiveDate: "2026-10-01", Fields: map[string]string{"title": "Engineer", "grade": "P2"}},
		{RowID: "r2", SubjectID: "emp-2", EffectiveDate: "2026-10-01", Fields: map[string]string{"title": "Manager", "grade": "M1"}},
		{RowID: "r3", SubjectID: "emp-3", EffectiveDate: "2026-10-01", Fields: map[string]string{"title": "Analyst", "grade": "P1"}},
		{RowID: "r4", SubjectID: "emp-4", EffectiveDate: "2026-10-01", Fields: map[string]string{"title": "Designer", "grade": "P2"}},
		{RowID: "r5", SubjectID: "emp-5", EffectiveDate: "2026-10-01", Fields: map[string]string{"title": "Lead", "grade": "P3"}},
	}
	target := []onboarding.TargetRecord{
		{SubjectID: "emp-1", EffectiveDate: "2026-10-01", Fields: map[string]string{"title": "Engineer", "grade": "P2"}, LedgerEventRefs: []string{"ev-1"}},
		{SubjectID: "emp-2", EffectiveDate: "2026-10-01", Fields: map[string]string{"title": "Analyst", "grade": "M1"}, LedgerEventRefs: []string{"ev-2"}},
		{SubjectID: "emp-3", EffectiveDate: "2026-11-01", Fields: map[string]string{"title": "Analyst", "grade": "P1"}, LedgerEventRefs: []string{"ev-3"}},
		{SubjectID: "emp-4", EffectiveDate: "2026-10-01", Fields: map[string]string{"title": "Designer", "grade": "P2"}, LedgerEventRefs: []string{"ev-4"},
			Observed: map[string]string{"title": "Designer", "grade": "P1"}, IrreversibleEffects: []string{"payroll:release-77"}},
		{SubjectID: "emp-9", EffectiveDate: "2026-10-01", Fields: map[string]string{"title": "Ghost", "grade": "P9"}, LedgerEventRefs: []string{"ev-9"}},
	}
	return source, target
}

// TestTodo_ONBOARD_006 proves row-level reconciliation cannot be hidden by
// equal aggregate counts: every row is classified by its own drift, every
// drift gets a governed compensation that appends after the committed ledger
// events and deletes nothing, and a released irreversible effect is reported.
func TestTodo_ONBOARD_006(t *testing.T) {
	source, target := reconcileFixture()
	if len(source) != len(target) {
		t.Fatal("fixture must have equal aggregate counts")
	}
	report, err := onboarding.Reconcile("import-1", source, target)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{onboarding.RowMatched: 1, onboarding.RowFieldDrift: 1, onboarding.RowEffectiveDateDrift: 1,
		onboarding.RowObservationDrift: 1, onboarding.RowMissingInTarget: 1, onboarding.RowUnexpectedInTarget: 1}
	for k, v := range want {
		if report.Counts[k] != v {
			t.Errorf("count[%s] = %d, want %d", k, report.Counts[k], v)
		}
	}
	if len(report.Rows) != 6 || len(report.Compensations) != 5 {
		t.Fatalf("rows %d compensations %d", len(report.Rows), len(report.Compensations))
	}
	for _, c := range report.Compensations {
		if c.DeletesHistory || c.IdempotencyKey == "" || len(c.Corrections) == 0 {
			t.Errorf("compensation %+v is not a governed append-only correction", c)
		}
		if c.Classification != onboarding.RowMissingInTarget && len(c.AppendsAfter) == 0 {
			t.Errorf("compensation for %s does not append after its ledger history", c.SubjectID)
		}
	}
	if len(report.Irreversible) != 1 || report.Irreversible[0] != "emp-4" {
		t.Fatalf("irreversible = %v", report.Irreversible)
	}
	var drift onboarding.Compensation
	for _, c := range report.Compensations {
		if c.SubjectID == "emp-2" {
			drift = c
		}
	}
	if len(drift.Corrections) != 1 || drift.Corrections[0].Field != "title" || drift.Corrections[0].Target != "Manager" {
		t.Fatalf("field drift correction = %+v", drift)
	}
}

// TestTodo_ONBOARD_006_Golden pins the complete report.
func TestTodo_ONBOARD_006_Golden(t *testing.T) {
	source, target := reconcileFixture()
	report, err := onboarding.Reconcile("import-1", source, target)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.MarshalIndent(report, "", "  ")
	path := filepath.Join("testdata", "onboard006_reconciliation.golden.json")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with HCMNEXT_UPDATE_GOLDEN=1 to create): %v", err)
	}
	if string(append(got, '\n')) != strings.ReplaceAll(string(want), "\r", "") {
		t.Fatalf("reconciliation report changed:\n%s", got)
	}
}

// TestTodo_ONBOARD_006_Mutation proves each classification is load-bearing:
// fixing one drift at a time moves exactly that row to MATCHED, and malformed
// input is refused rather than reconciled partially.
func TestTodo_ONBOARD_006_Mutation(t *testing.T) {
	fixes := map[string]func([]onboarding.SourceRow, []onboarding.TargetRecord) ([]onboarding.SourceRow, []onboarding.TargetRecord){
		"emp-2": func(s []onboarding.SourceRow, tg []onboarding.TargetRecord) ([]onboarding.SourceRow, []onboarding.TargetRecord) {
			tg[1].Fields["title"] = "Manager"
			return s, tg
		},
		"emp-3": func(s []onboarding.SourceRow, tg []onboarding.TargetRecord) ([]onboarding.SourceRow, []onboarding.TargetRecord) {
			tg[2].EffectiveDate = "2026-10-01"
			return s, tg
		},
		"emp-4": func(s []onboarding.SourceRow, tg []onboarding.TargetRecord) ([]onboarding.SourceRow, []onboarding.TargetRecord) {
			tg[3].Observed = map[string]string{"title": "Designer", "grade": "P2"}
			return s, tg
		},
	}
	for subject, fix := range fixes {
		source, target := reconcileFixture()
		source, target = fix(source, target)
		report, err := onboarding.Reconcile("import-1", source, target)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range report.Rows {
			if row.SubjectID == subject && row.Classification != onboarding.RowMatched {
				t.Errorf("fixing %s left it %s", subject, row.Classification)
			}
		}
		if report.Counts[onboarding.RowMatched] != 2 {
			t.Errorf("fixing %s matched %d rows, want 2", subject, report.Counts[onboarding.RowMatched])
		}
	}
	source, target := reconcileFixture()
	for name, call := range map[string]func() error{
		"no import id": func() error { _, err := onboarding.Reconcile("", source, target); return err },
		"dup target": func() error {
			_, err := onboarding.Reconcile("i", source, append(target, target[0]))
			return err
		},
		"blank target": func() error {
			_, err := onboarding.Reconcile("i", source, []onboarding.TargetRecord{{}})
			return err
		},
		"dup source": func() error {
			_, err := onboarding.Reconcile("i", append(source, source[0]), target)
			return err
		},
		"blank source": func() error {
			_, err := onboarding.Reconcile("i", []onboarding.SourceRow{{}}, target)
			return err
		},
	} {
		if err := call(); !errors.Is(err, onboarding.ErrReconcileInput) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
