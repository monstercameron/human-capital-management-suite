package todogovernance

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

// Paths to the real, checked-in corpus, relative to this package directory.
const (
	realMarkdownPath = "../../../planning/archive/todos-2026-10-22.md"
	realRegistryPath = "../../../definitions/planning/todo-registry.json"
	realCatalogPath  = "../../../planning/specs/business-intent-catalog.md"
)

// loadRealMarkdown parses the live planning/archive/todos-2026-10-22.md and fails the test if
// it cannot be read or if todoregistry reports structural parse errors
// (which would themselves indicate a live-corpus regression, not a fixture
// concern).
func loadRealMarkdown(t *testing.T) []Record {
	t.Helper()
	_, records, parseErrs, err := LoadMarkdown(realMarkdownPath)
	if err != nil {
		t.Fatalf("load %s: %v", realMarkdownPath, err)
	}
	if len(parseErrs) != 0 {
		t.Fatalf("planning/archive/todos-2026-10-22.md has %d structural parse errors, e.g. %v", len(parseErrs), parseErrs[0])
	}
	return records
}

func loadRealRegistry(t *testing.T) []todoregistry.Todo {
	t.Helper()
	todos, err := LoadRegistry(realRegistryPath)
	if err != nil {
		t.Fatalf("load %s: %v", realRegistryPath, err)
	}
	return todos
}

func loadRealCatalog(t *testing.T) []string {
	t.Helper()
	defs, err := LoadCatalogDefinitions(realCatalogPath)
	if err != nil {
		t.Fatalf("load %s: %v", realCatalogPath, err)
	}
	return defs
}

// requireFinding asserts that findings contains at least one entry matching
// id/rule/code (Detail is not compared since fixtures usually produce
// exactly one finding of interest per assertion).
func requireFinding(t *testing.T, findings []Finding, id, rule, code string) {
	t.Helper()
	for _, f := range findings {
		if f.TodoID == id && f.Rule == rule && f.Code == code {
			return
		}
	}
	t.Errorf("expected a %s/%s finding for %s, got: %v", rule, code, id, findings)
}

// requireNoFinding asserts findings contains no entry for id/rule/code.
func requireNoFinding(t *testing.T, findings []Finding, id, rule, code string) {
	t.Helper()
	for _, f := range findings {
		if f.TodoID == id && f.Rule == rule && f.Code == code {
			t.Errorf("did not expect a %s/%s finding for %s, got: %v", rule, code, id, f)
		}
	}
}

// assertOnlyAllowlisted fails the test for every finding whose Key() is not
// present in allowlist, so a NEW live-corpus violation breaks the build
// while every already-reviewed entry stays silently green.
func assertOnlyAllowlisted(t *testing.T, findings []Finding, allowlist map[string]bool) {
	t.Helper()
	var unexpected []Finding
	for _, f := range findings {
		if !allowlist[f.Key()] {
			unexpected = append(unexpected, f)
		}
	}
	if len(unexpected) != 0 {
		t.Errorf("%d NEW governance violation(s) not covered by the reviewed allowlist:", len(unexpected))
		for i, f := range unexpected {
			if i >= 25 {
				t.Errorf("  ... and %d more", len(unexpected)-25)
				break
			}
			t.Errorf("  %s", f.String())
		}
	}
}
