package dependencygraph

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

func TestTodo_GOV_016(t *testing.T) {
	const markdown = `## Dependency fixtures

- [ ] ` + "`A-001`" + ` **[GATE_A][LUNA] A.**
  - **Depends:** ` + "`B-001`" + `, ` + "`UNKNOWN-001`" + `.
  - **TEST:** ` + "`TestA`" + `.
  - **TEST MATRIX:** ` + "`PRIMARY=TestA`" + `.
  - **RED:** missing behavior.
  - **GREEN:** implemented behavior.
  - **REFACTOR:** keep it small.
  - **Refs:** [plan](plan.md).
- [ ] ` + "`B-001`" + ` **[GATE_B][LUNA] B.**
  - **Depends:** ` + "`A-001`" + `.
  - **TEST:** ` + "`TestB`" + `.
  - **TEST MATRIX:** ` + "`PRIMARY=TestB`" + `.
  - **RED:** missing behavior.
  - **GREEN:** implemented behavior.
  - **REFACTOR:** keep it small.
  - **Refs:** [plan](plan.md).
- [ ] ` + "`RANGE-001`" + ` **[P0][LUNA] A malformed range.**
  - **Depends:** ` + "`A-003`" + `–` + "`B-001`" + `.
  - **TEST:** ` + "`TestRange`" + `.
  - **TEST MATRIX:** ` + "`PRIMARY=TestRange`" + `.
  - **RED:** missing behavior.
  - **GREEN:** implemented behavior.
  - **REFACTOR:** keep it small.
  - **Refs:** [plan](plan.md).
- [ ] ` + "`PROSE-001`" + ` **[P0][LUNA] A prose dependency.**
  - **Depends:** database category.
  - **TEST:** ` + "`TestProse`" + `.
  - **TEST MATRIX:** ` + "`PRIMARY=TestProse`" + `.
  - **RED:** missing behavior.
  - **GREEN:** implemented behavior.
  - **REFACTOR:** keep it small.
  - **Refs:** [plan](plan.md).
`

	violations, err := CheckMarkdown(markdown)
	if err != nil {
		t.Fatalf("CheckMarkdown: %v", err)
	}
	assertCode(t, violations, "UNKNOWN_DEPENDENCY")
	assertCode(t, violations, "DEPENDENCY_CYCLE")
	assertCode(t, violations, "MALFORMED_RANGE")
	assertCode(t, violations, "PROSE_DEPENDENCY")
	if got := violations[0].String(); !strings.Contains(got, "line ") {
		t.Errorf("diagnostic lacks stable source location: %q", got)
	}
}

func TestTodo_GOV_016_Property(t *testing.T) {
	t.Run("dependency ranges expand without duplicating endpoints", func(t *testing.T) {
		for _, test := range []struct {
			name string
			raw  string
			want []string
		}{
			{
				name: "closed range",
				raw:  "`TASK-001`-`TASK-003`",
				want: []string{"TASK-001", "TASK-002", "TASK-003"},
			},
			{
				name: "range followed by list item",
				raw:  "`TASK-001`-`TASK-003`, `OTHER-001`",
				want: []string{"TASK-001", "TASK-002", "TASK-003", "OTHER-001"},
			},
		} {
			t.Run(test.name, func(t *testing.T) {
				got, findings := parseDependencyField("ROOT-001", 10, test.raw)
				if len(findings) != 0 {
					t.Fatalf("parseDependencyField findings: %v", findings)
				}
				if len(got) != len(test.want) {
					t.Fatalf("dependencies = %v, want %v", got, test.want)
				}
				for i := range test.want {
					if got[i] != test.want[i] {
						t.Fatalf("dependencies = %v, want %v", got, test.want)
					}
				}
			})
		}
	})

	todos := []todoregistry.Todo{
		{ID: "A-001", Phase: "P0", Line: 1, Depends: []string{"B-001"}},
		{ID: "B-001", Phase: "P0", Line: 2, Depends: nil},
	}
	if got := Check(todos); len(got) != 0 {
		t.Fatalf("acyclic resolved graph returned violations: %v", got)
	}

	// The checker must not mutate the parsed registry slice while normalizing
	// or traversing its graph.
	if todos[0].Depends[0] != "B-001" {
		t.Fatalf("Check mutated input dependency: %+v", todos[0])
	}

	t.Run("Gate A closure rejects an indirect later-gate implementation", func(t *testing.T) {
		violations := Check([]todoregistry.Todo{
			{ID: "A-001", Phase: "GATE_A", Line: 1, Depends: []string{"P-001"}},
			{ID: "P-001", Phase: "P0", Line: 2, Depends: []string{"B-001"}},
			{ID: "B-001", Phase: "GATE_B", Line: 3},
		})
		var found bool
		for _, violation := range violations {
			if violation.Code == "GATE_A_PHASE_INVERSION" && strings.Contains(violation.Message, "closure") {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing indirect phase-inversion diagnostic: %v", violations)
		}
	})
}

func TestTodo_GOV_016_Golden(t *testing.T) {
	todos := []todoregistry.Todo{
		{ID: "A-001", Phase: "P0", Line: 10, Depends: []string{"MISSING-001"}},
	}
	violations := Check(todos)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want one: %v", len(violations), violations)
	}
	const want = "line 10: A-001: UNKNOWN_DEPENDENCY: dependency MISSING-001 is not registered"
	if got := violations[0].String(); got != want {
		t.Errorf("diagnostic changed:\n got:  %s\n want: %s", got, want)
	}
}

func FuzzTodo_GOV_016(f *testing.F) {
	f.Add("A-001", "B-001")
	f.Add("A-001", "A-001")
	f.Add("not-an-id", "")
	f.Fuzz(func(t *testing.T, from, to string) {
		todos := []todoregistry.Todo{
			{ID: "A-001", Phase: "P0", Line: 1, Depends: []string{to}},
			{ID: from, Phase: "P0", Line: 2},
		}
		_ = Check(todos)
	})
}

func assertCode(t *testing.T, violations []Diagnostic, code string) {
	t.Helper()
	for _, violation := range violations {
		if violation.Code == code {
			return
		}
	}
	t.Errorf("missing %s diagnostic in %v", code, violations)
}
