package atomicity

import (
	"strings"
	"testing"
)

// TestTodoAtomicity is the primary red/green test for GOV-007.
func TestTodoAtomicity(t *testing.T) {
	t.Run("two independently shippable verbs is rejected", func(t *testing.T) {
		v := CheckAtomicity("X-001", "Implement the ledger writer and deploy the outbox consumer.", "writer emits one event; consumer drains the outbox")
		assertHasReasonContaining(t, v, "bundles two independently shippable verbs")
	})
	t.Run("declared action pair with todo tags is rejected", func(t *testing.T) {
		v := CheckAtomicity("X-001A", "[PHASE_2][SOL_HIGH] Research and publish the federal baseline.", "the report lists each source and finding")
		assertHasReasonContaining(t, v, "bundles two independently shippable verbs")
	})
	t.Run("noun-phrase and is not rejected", func(t *testing.T) {
		v := CheckAtomicity("X-002", "Establish trust and assurance boundaries.", "boundary fixture returns the expected decision")
		for _, viol := range v {
			if strings.Contains(viol.Reason, "bundles two independently shippable verbs") {
				t.Errorf("noun-phrase 'and' incorrectly flagged: %v", v)
			}
		}
	})
	t.Run("noun-led title with incidental action is not rejected", func(t *testing.T) {
		v := CheckAtomicity("X-002A", "Ledger and publish schema behavior.", "the returned schema contains the expected fields")
		for _, viol := range v {
			if strings.Contains(viol.Reason, "bundles two independently shippable verbs") {
				t.Errorf("noun-led title incorrectly flagged: %v", v)
			}
		}
	})
	t.Run("empty GREEN is rejected", func(t *testing.T) {
		v := CheckAtomicity("X-003", "Implement the ledger writer.", "")
		assertHasReasonContaining(t, v, "no observable expected result")
	})
	t.Run("vague placeholder GREEN is rejected", func(t *testing.T) {
		for _, vague := range []string{"TBD", "N/A", "it works", "Done.", "works as expected", "the feature is implemented successfully"} {
			v := CheckAtomicity("X-004", "Implement the ledger writer.", vague)
			assertHasReasonContaining(t, v, "no observable expected result")
		}
	})
	t.Run("a single verb with a concrete GREEN has zero violations", func(t *testing.T) {
		v := CheckAtomicity("X-005", "Implement the ledger writer.", "a fixture with three events replays deterministically to the same digest")
		if len(v) != 0 {
			t.Errorf("expected zero violations, got %v", v)
		}
	})
	t.Run("a fixture with both defects reports both", func(t *testing.T) {
		v := CheckAtomicity("X-006", "Implement the writer and publish the schema.", "TBD")
		if len(v) != 2 {
			t.Fatalf("expected 2 violations, got %d: %v", len(v), v)
		}
	})
}

func assertHasReasonContaining(t *testing.T, violations []Violation, substr string) {
	t.Helper()
	for _, v := range violations {
		if strings.Contains(v.Reason, substr) {
			return
		}
	}
	t.Errorf("expected a violation containing %q, got %v", substr, violations)
}

// TestTodo_GOV_007_Golden pins the exact violation message format.
func TestTodo_GOV_007_Golden(t *testing.T) {
	v := CheckAtomicity("GOLDEN-007", "Implement the writer and publish the schema.", "")
	if len(v) != 2 {
		t.Fatalf("expected 2 violations, got %d: %v", len(v), v)
	}
	want := []string{
		`GOLDEN-007: Title bundles two independently shippable verbs (second verb "publish"); split into separate todos`,
		"GOLDEN-007: GREEN names no observable expected result (empty or a vague placeholder)",
	}
	for i, w := range want {
		if got := v[i].String(); got != w {
			t.Errorf("violation %d changed:\n got:  %s\n want: %s", i, got, w)
		}
	}
}
