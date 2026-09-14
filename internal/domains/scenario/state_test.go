package scenario

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_SCENARIO_002(t *testing.T) {
	revision := baseScenario(t)
	baseline := testBaseline(revision, map[string]TypedValue{
		"headcount.current": DecimalValue(values.MustDecimal("10", 0, values.RoundingHalfEven)),
		"secret.salary":     DecimalValue(values.MustDecimal("999", 0, values.RoundingHalfEven)),
	}, "headcount.current", "headcount.target")
	got, err := Evaluate(revision, baseline, Delta{Field: "headcount.target", Value: DecimalValue(values.MustDecimal("14", 0, values.RoundingHalfEven))})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Fields) != 2 || got.Fields[0].Field != "headcount.current" || got.Fields[0].Known == false || got.Fields[1].Field != "headcount.target" || !got.Fields[1].Known || got.Fields[1].Value.Number.String() != "14" {
		t.Fatalf("evaluation = %+v", got)
	}
	if len(got.Unknowns) != 0 {
		t.Fatalf("unknowns = %+v", got.Unknowns)
	}
	if got.BaselineSnapshotRef != baseline.SnapshotRef || got.Scope != baseline.Scope || got.PolicySnapshotRef != baseline.PolicySnapshotRef || got.AccessDecisionRef != baseline.AccessDecisionRef {
		t.Fatalf("baseline evidence = %+v", got)
	}
}

func TestTodo_SCENARIO_002_Property(t *testing.T) {
	revision := baseScenario(t)
	baseline := testBaseline(revision, map[string]TypedValue{}, "missing")
	got, err := Evaluate(revision, baseline)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Unknowns) != 1 || got.Unknowns[0].Field != "missing" || got.Fields[0].Known {
		t.Fatalf("missing input was not explicit: %+v", got)
	}
	if len(baseline.Fields) != 0 {
		t.Fatal("evaluation changed baseline")
	}
	baseline.Fields["missing"] = TextValue("late mutation")
	if got.Fields[0].Known || got.Fields[0].Value.Validate() == nil {
		t.Fatalf("result aliases baseline state: %+v", got)
	}
}

func TestTodo_SCENARIO_002_Mutation(t *testing.T) {
	revision := baseScenario(t)
	baseline := testBaseline(revision, map[string]TypedValue{"private": TextValue("do-not-read")}, "public")
	if _, err := Evaluate(revision, baseline, Delta{Field: "private", Value: TextValue("write")}); !errors.Is(err, ErrUnapprovedField) {
		t.Fatalf("unapproved delta error = %v", err)
	}
	stale := testBaseline(revision, nil, "public")
	stale.SnapshotRef = "other"
	if _, err := Evaluate(revision, stale); !errors.Is(err, ErrBaselineMismatch) {
		t.Fatalf("stale baseline error = %v", err)
	}
	crossScope := testBaseline(revision, nil, "public")
	crossScope.Scope = "other-tenant-scope"
	if _, err := Evaluate(revision, crossScope); !errors.Is(err, ErrBaselineScope) {
		t.Fatalf("cross-scope baseline error = %v", err)
	}
	missingEvidence := testBaseline(revision, nil, "public")
	missingEvidence.AccessDecisionRef = ""
	if _, err := Evaluate(revision, missingEvidence); !errors.Is(err, ErrAccessEvidence) {
		t.Fatalf("missing access evidence error = %v", err)
	}
	duplicateApproval := testBaseline(revision, nil, "public", "public")
	if _, err := Evaluate(revision, duplicateApproval); !errors.Is(err, ErrInvalidScenario) {
		t.Fatalf("duplicate approved field error = %v", err)
	}
	if _, err := Evaluate(revision, testBaseline(revision, map[string]TypedValue{"private": TextValue("do-not-read")}, "public"), Delta{Field: "public", Value: TextValue("ok")}); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_SCENARIO_002_Security: unapproved baseline values never
// influence the result and never leak through fields, unknowns or
// refusal errors — the allowlist is a DLP boundary, not a filter.
func TestTodo_SCENARIO_002_Security(t *testing.T) {
	const secret = "salary-999-confidential"
	revision := baseScenario(t)
	baseline := testBaseline(revision, map[string]TypedValue{
		"comp.secret":      TextValue(secret),
		"headcount.target": DecimalValue(values.MustDecimal("14", 0, values.RoundingHalfEven)),
	}, "headcount.target")
	got, err := Evaluate(revision, baseline)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range got.Fields {
		if field.Field == "comp.secret" {
			t.Fatal("unapproved field evaluated")
		}
		if field.Value.Kind == ValueText && strings.Contains(field.Value.Text, secret) {
			t.Fatal("unapproved value leaked through fields")
		}
	}
	for _, unknown := range got.Unknowns {
		if unknown.Field == "comp.secret" || strings.Contains(unknown.Reason, secret) {
			t.Fatal("unapproved value leaked through unknowns")
		}
	}
	_, err = Evaluate(revision, baseline, Delta{Field: "comp.secret", Value: TextValue("overwrite")})
	if !errors.Is(err, ErrUnapprovedField) {
		t.Fatalf("unapproved delta error = %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("refusal leaked protected value: %v", err)
	}
}

func testBaseline(revision ScenarioRevision, fields map[string]TypedValue, approved ...string) Baseline {
	return Baseline{
		SnapshotRef:       revision.BaselineSnapshotRef,
		Scope:             revision.Scope,
		PolicySnapshotRef: "policy:dlp-2026-01",
		AccessDecisionRef: "decision:read-1",
		Fields:            fields,
		ApprovedFields:    approved,
	}
}
