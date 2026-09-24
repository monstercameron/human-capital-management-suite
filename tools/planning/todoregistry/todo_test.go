package todoregistry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTodoRegistryMatchesMarkdown is the primary test for GOV-002.
// It verifies that:
//   - Parsing yields exactly the number of "- [ ] `ID`" blocks in the markdown
//   - No duplicate IDs exist
//   - Every phase and model label is valid
//   - Every dependency resolves to an existing ID (expanding ranges), except
//     edges explicitly allow-listed in definitions/planning/known-defects.yaml
//   - Every block has exactly one of each required field
//   - The checked-in JSON equals a fresh generation
func TestTodoRegistryMatchesMarkdown(t *testing.T) {
	// Read the markdown file
	markdownPath := filepath.Join("../../../planning/todos.md")
	content, err := os.ReadFile(markdownPath)
	if err != nil {
		t.Fatalf("failed to read todos.md: %v", err)
	}

	markdownStr := string(content)

	// Count todo blocks in markdown
	countInMarkdown := countTodoBlocks(markdownStr)
	if countInMarkdown == 0 {
		t.Fatal("expected at least one todo block in markdown")
	}

	// Parse todos
	todos, parseErrs := ParseTodos(markdownStr)
	if len(parseErrs) > 0 {
		t.Errorf("parse errors occurred:")
		for _, err := range parseErrs {
			t.Errorf("  %v", err)
		}
	}

	// Verify count matches
	if len(todos) != countInMarkdown {
		t.Errorf("parsed %d todos but markdown has %d blocks", len(todos), countInMarkdown)
	}

	// Check for duplicate IDs
	idMap := make(map[string]int)
	for _, td := range todos {
		idMap[td.ID]++
	}
	for id, count := range idMap {
		if count > 1 {
			t.Errorf("duplicate ID: %s (appears %d times)", id, count)
		}
	}

	// Check that every phase is valid
	for _, todo := range todos {
		if !validPhases[todo.Phase] {
			t.Errorf("todo %s has invalid phase: %s", todo.ID, todo.Phase)
		}
	}

	// Check that every model is valid
	for _, todo := range todos {
		if !validModels[todo.Model] {
			t.Errorf("todo %s has invalid model: %s", todo.ID, todo.Model)
		}
	}

	// Check dependency resolution against the known-defects allow-list.
	unresolved := ResolveDependencies(todos)
	knownDefects, err := LoadKnownDefects(filepath.Join("../../../definitions/planning/known-defects.yaml"))
	if err != nil {
		t.Fatalf("failed to load known-defects.yaml: %v", err)
	}
	allowed := make(map[string]bool, len(knownDefects))
	for _, d := range knownDefects {
		allowed[d.From+"|"+d.To] = true
	}

	var unexpectedUnresolved []UnresolvedDependency
	for _, u := range unresolved {
		if !allowed[u.From+"|"+u.To] {
			unexpectedUnresolved = append(unexpectedUnresolved, u)
		}
	}
	if len(unexpectedUnresolved) > 0 {
		t.Errorf("found %d unresolved dependencies not in known-defects.yaml:", len(unexpectedUnresolved))
		for _, u := range unexpectedUnresolved {
			t.Errorf("  %s depends on %s (not found)", u.From, u.To)
		}
	}

	// Every allow-listed defect must still be a genuine unresolved edge -
	// a stale entry would silently hide a regression once the underlying
	// dependency is added.
	actuallyUnresolved := make(map[string]bool, len(unresolved))
	for _, u := range unresolved {
		actuallyUnresolved[u.From+"|"+u.To] = true
	}
	for _, d := range knownDefects {
		if !actuallyUnresolved[d.From+"|"+d.To] {
			t.Errorf("known-defects.yaml entry %s -> %s no longer unresolved; remove it", d.From, d.To)
		}
	}

	// Check required fields (basic validation already done in parseErrs)
	for i, todo := range todos {
		if todo.ID == "" {
			t.Errorf("todo %d missing ID", i)
		}
		if todo.Phase == "" {
			t.Errorf("todo %s missing Phase", todo.ID)
		}
		if todo.Model == "" {
			t.Errorf("todo %s missing Model", todo.ID)
		}
		if todo.Title == "" {
			t.Errorf("todo %s missing Title", todo.ID)
		}
		if todo.Test == "" {
			t.Errorf("todo %s missing TEST field", todo.ID)
		}
		if len(todo.TestMatrix) == 0 {
			t.Errorf("todo %s missing TEST MATRIX field", todo.ID)
		}
		if todo.Red == "" {
			t.Errorf("todo %s missing RED field", todo.ID)
		}
		if todo.Green == "" {
			t.Errorf("todo %s missing GREEN field", todo.ID)
		}
		if todo.Refactor == "" {
			t.Errorf("todo %s missing REFACTOR field", todo.ID)
		}
		if todo.Refs == "" {
			t.Errorf("todo %s missing Refs field", todo.ID)
		}
		// Field values must be markdown-artifact free.
		if strings.Contains(todo.Test, "*") || strings.Contains(todo.Test, "`") {
			t.Errorf("todo %s TEST field retains markdown artifacts: %q", todo.ID, todo.Test)
		}
		for class, name := range todo.TestMatrix {
			if strings.ContainsAny(class, "*`") || strings.ContainsAny(name, "*`") {
				t.Errorf("todo %s TEST MATRIX entry %s=%s retains markdown artifacts", todo.ID, class, name)
			}
		}
		for _, dep := range todo.Depends {
			if strings.ContainsAny(dep, "*`") {
				t.Errorf("todo %s dependency %q retains markdown artifacts", todo.ID, dep)
			}
		}
	}

	// Check that generated JSON matches checked-in JSON.
	jsonPath := filepath.Join("../../../definitions/planning/todo-registry.json")
	currentJSON, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("failed to read current todo-registry.json: %v", err)
	}

	freshJSON, err := ToJSON(todos)
	if err != nil {
		t.Fatalf("failed to generate fresh JSON: %v", err)
	}

	if string(currentJSON) != string(freshJSON) {
		t.Errorf("checked-in todo-registry.json does not match fresh generation; run the generator (go run ./tools/planning/cmd/todoregistry)")
	}
}

// TestTodo_GOV_002_Golden pins the deterministic JSON representation of a
// representative registry entry, including ordered dependencies and matrix
// keys. This catches serialization drift independently of the full corpus.
func TestTodo_GOV_002_Golden(t *testing.T) {
	todos := []Todo{{
		ID: "GOV-002", Phase: "P0", Model: "TERRA", Title: "Register every todo",
		Depends: []string{"GOV-001"}, Test: "TestRegistry", TestMatrix: map[string]string{"GOLDEN": "TestGolden", "PRIMARY": "TestRegistry"},
		Red: "missing entry", Green: "complete registry", Refactor: "single source", Refs: "plan.md", Done: true,
	}}
	got, err := ToJSON(todos)
	if err != nil {
		t.Fatal(err)
	}
	const want = `[
  {
    "id": "GOV-002",
    "phase": "P0",
    "model": "TERRA",
    "title": "Register every todo",
    "section": "",
    "depends": [
      "GOV-001"
    ],
    "test": "TestRegistry",
    "test_matrix": {
      "GOLDEN": "TestGolden",
      "PRIMARY": "TestRegistry"
    },
    "red": "missing entry",
    "green": "complete registry",
    "refactor": "single source",
    "refs": "plan.md",
    "retired": false,
    "done": true
  }
]
`
	if string(got) != want {
		t.Fatalf("registry JSON changed:\n got: %s\nwant: %s", got, want)
	}
}

// countTodoBlocks counts the number of "- [ ] `ID`" lines in the markdown.
func countTodoBlocks(content string) int {
	count := 0
	for line := range strings.SplitSeq(content, "\n") {
		if strings.HasPrefix(line, "- [ ] `") || strings.HasPrefix(line, "- [x] `") {
			count++
		}
	}
	return count
}

func TestTodoRegistryValidPhases(t *testing.T) {
	phases := []string{"P0", "GATE_A", "GATE_B", "GATE_C", "PHASE_2", "PHASE_3", "PHASE_4", "PHASE_5", "CONFORMANCE", "DESIGN", "RETIRED", "OUT"}
	for _, phase := range phases {
		if !validPhases[phase] {
			t.Errorf("phase %s should be valid", phase)
		}
	}
}

func TestTodoRegistryValidModels(t *testing.T) {
	models := []string{"LUNA", "TERRA", "SOL_LOW", "SOL_HIGH"}
	for _, model := range models {
		if !validModels[model] {
			t.Errorf("model %s should be valid", model)
		}
	}
}

// Test dependency range expansion
func TestExpandIDRange(t *testing.T) {
	tests := []struct {
		start    string
		end      string
		expected []string
	}{
		{"TOOL-010", "TOOL-015", []string{"TOOL-010", "TOOL-011", "TOOL-012", "TOOL-013", "TOOL-014", "TOOL-015"}},
		{"WF-STEP-001", "WF-STEP-003", []string{"WF-STEP-001", "WF-STEP-002", "WF-STEP-003"}},
		// Different prefixes cannot be numerically expanded; both endpoints
		// are returned as literal dependencies.
		{"PEOPLE-001", "COMP-003", []string{"PEOPLE-001", "COMP-003"}},
	}

	for _, tc := range tests {
		result := expandIDRange(tc.start, tc.end)
		if len(result) != len(tc.expected) {
			t.Errorf("expandIDRange(%s, %s) = %v, want %v", tc.start, tc.end, result, tc.expected)
			continue
		}
		for i, id := range result {
			if id != tc.expected[i] {
				t.Errorf("expandIDRange(%s, %s) element %d = %s, want %s", tc.start, tc.end, i, id, tc.expected[i])
			}
		}
	}
}

func TestParseDependencies(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"none", []string{}},
		{"None", []string{}},
		{"`GOV-001`", []string{"GOV-001"}},
		{"`TOOL-010`–`TOOL-015`", []string{"TOOL-010", "TOOL-011", "TOOL-012", "TOOL-013", "TOOL-014", "TOOL-015"}},
		{"`TOOL-010`-`TOOL-015`", []string{"TOOL-010", "TOOL-011", "TOOL-012", "TOOL-013", "TOOL-014", "TOOL-015"}},
		{"`TOOL-010` to `TOOL-015`", []string{"TOOL-010", "TOOL-011", "TOOL-012", "TOOL-013", "TOOL-014", "TOOL-015"}},
		{"`GOV-001`, `GOV-002`", []string{"GOV-001", "GOV-002"}},
		{"`MSG-005`, hostile-content/integration ingress", []string{"MSG-005"}},
	}

	for _, tc := range tests {
		result := parseDependencies(tc.input)
		if len(result) != len(tc.expected) {
			t.Errorf("parseDependencies(%q) = %v (len %d), want %v (len %d)", tc.input, result, len(result), tc.expected, len(tc.expected))
			continue
		}
		for i, id := range result {
			if id != tc.expected[i] {
				t.Errorf("parseDependencies(%q) element %d = %s, want %s", tc.input, i, id, tc.expected[i])
			}
		}
	}
}

func TestParseTestMatrix(t *testing.T) {
	got := parseTestMatrix("`PRIMARY=TestFoo`; `GOLDEN=TestTodo_FOO_001_Golden`")
	want := map[string]string{
		"PRIMARY": "TestFoo",
		"GOLDEN":  "TestTodo_FOO_001_Golden",
	}
	if len(got) != len(want) {
		t.Fatalf("parseTestMatrix() = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("parseTestMatrix()[%s] = %q, want %q", k, got[k], v)
		}
	}
}

func TestCleanFieldValue(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"** `TestFoo`.", "`TestFoo`"},
		{"** none.", "none"},
		{"plain value.", "plain value"},
	}
	for _, tc := range tests {
		if got := cleanFieldValue(tc.input); got != tc.want {
			t.Errorf("cleanFieldValue(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestUnresolvedDependencyMessage(t *testing.T) {
	u := UnresolvedDependency{From: "A-001", To: "B-002"}
	msg := fmt.Sprintf("%s depends on %s (not found)", u.From, u.To)
	if msg != "A-001 depends on B-002 (not found)" {
		t.Errorf("unexpected message: %s", msg)
	}
}

func TestParseEvidenceAcceptsAnyDatedForm(t *testing.T) {
	md := "- [x] `X-001` **[GATE_A][LUNA] Title.**\n" +
		"  - **Evidence (2026-09-06):** `TestX` in `internal/x`; `go test -count=1 ./internal/x/` PASS on windows/arm64 (Go 1.26.3).\n" +
		"  - **Depends:** none.\n" +
		"  - **INTENT CONTEXT:** `ROLE=SUBSTRATE; SETS=BI.ALL; DIRECT=none; WHY=w`.\n" +
		"  - **TEST:** `TestX`.\n" +
		"  - **TEST MATRIX:** `PRIMARY=TestX`.\n" +
		"  - **RED:** r.\n" +
		"  - **GREEN:** g.\n" +
		"  - **REFACTOR:** f.\n" +
		"  - **Refs:** [p](plan.md).\n"
	todos, errs := ParseTodos(md)
	if len(errs) != 0 || len(todos) != 1 {
		t.Fatalf("parse: %v %d", errs, len(todos))
	}
	if !strings.Contains(todos[0].Evidence, "TestX") {
		t.Fatalf("dated evidence must populate Evidence, got %q", todos[0].Evidence)
	}
	partial, _ := ParseTodos(strings.Replace(md, "Evidence (2026-09-06)", "Evidence (partial, 2026-09-06)", 1))
	if len(partial) != 1 || !strings.Contains(partial[0].Evidence, "TestX") {
		t.Fatal("partial dated evidence must populate Evidence too")
	}
}

func TestParseTodosPreservesRepeatedRefs(t *testing.T) {
	md := "- [x] `X-001` **[CONFORMANCE][LUNA] repeated refs.**\n" +
		"  - **Depends:** none.\n" +
		"  - **INTENT CONTEXT:** `ROLE=CONFORMANCE; SETS=BI.ALL; DIRECT=none; WHY=fixture`.\n" +
		"  - **TEST:** `TestX`.\n" +
		"  - **TEST MATRIX:** `PRIMARY=TestX`.\n" +
		"  - **RED:** r.\n" +
		"  - **GREEN:** g.\n" +
		"  - **REFACTOR:** f.\n" +
		"  - **Refs:** [first](./workflows/one.md).\n" +
		"  - **Refs:** [second](<workflows/two.md>).\n"
	todos, errs := ParseTodos(md)
	if len(errs) != 0 || len(todos) != 1 {
		t.Fatalf("parse: %v %d", errs, len(todos))
	}
	if !strings.Contains(todos[0].Refs, "workflows/one.md") || !strings.Contains(todos[0].Refs, "workflows/two.md") {
		t.Fatalf("repeated Refs were shadowed: %q", todos[0].Refs)
	}
}

func TestParseIntentContextConflictsFailClosed(t *testing.T) {
	md := "- [x] `X-001` **[CONFORMANCE][LUNA] conflicting context.**\n" +
		"  - **Depends:** none.\n" +
		"  - **INTENT CONTEXT:** `ROLE=CONFORMANCE; PRE_PROMOTION_EXPLORATORY=true`.\n" +
		"  - **INTENT CONTEXT:** `ROLE=SUBSTRATE; PRE_PROMOTION_EXPLORATORY=false`.\n" +
		"  - **TEST:** `TestX`.\n" +
		"  - **TEST MATRIX:** `PRIMARY=TestX`.\n" +
		"  - **RED:** r.\n" +
		"  - **GREEN:** g.\n" +
		"  - **REFACTOR:** f.\n" +
		"  - **Refs:** [workflow](workflows/sample.md).\n"
	todos, errs := ParseTodos(md)
	if len(errs) != 0 || len(todos) != 1 {
		t.Fatalf("parse: %v %d", errs, len(todos))
	}
	if !todos[0].IntentContextConflict || todos[0].Role != "CONFORMANCE" || !todos[0].PrePromotionExploratory {
		t.Fatalf("conflicting intent context parsed unsafely: %+v", todos[0])
	}
}

func TestTodoCapabilityMetadataParsesAndSerializes(t *testing.T) {
	markdown := "- [x] `X-001` **[GATE_B][LUNA] explicit capability.**\n" +
		"  - **Depends:** none.\n" +
		"  - **INTENT CONTEXT:** `ROLE=GOVERNANCE; SETS=BI.ALL; DIRECT=none; WHY=fixture; CAPABILITY=LIBRARY; OWNER=ARCHITECTURE_COUNCIL`.\n" +
		"  - **TEST:** `TestX`.\n" +
		"  - **TEST MATRIX:** `PRIMARY=TestX`.\n" +
		"  - **RED:** red.\n" +
		"  - **GREEN:** green.\n" +
		"  - **REFACTOR:** refactor.\n" +
		"  - **Refs:** [fixture](fixture.md).\n"
	todos, errs := ParseTodos(markdown)
	if len(errs) != 0 || len(todos) != 1 {
		t.Fatalf("ParseTodos() = %d todos, errors %v", len(todos), errs)
	}
	if todos[0].CapabilityClass != CapabilityLibrary || todos[0].Owner != "ARCHITECTURE_COUNCIL" {
		t.Fatalf("metadata parsed as capability=%q owner=%q", todos[0].CapabilityClass, todos[0].Owner)
	}
	encoded, err := ToJSON(todos)
	if err != nil {
		t.Fatal(err)
	}
	var registry []map[string]any
	if err := json.Unmarshal(encoded, &registry); err != nil {
		t.Fatalf("registry JSON is invalid: %v", err)
	}
	if len(registry) != 1 || registry[0]["capability_class"] != CapabilityLibrary || registry[0]["owner"] != "ARCHITECTURE_COUNCIL" {
		t.Fatalf("registry omitted explicit metadata: %s", encoded)
	}
}

func TestTodoCapabilityMetadataRejectsInvalidDeclarations(t *testing.T) {
	base := "- [x] `X-001` **[GATE_B][LUNA] explicit capability.**\n" +
		"  - **Depends:** none.\n" +
		"  - **INTENT CONTEXT:** `ROLE=GOVERNANCE; SETS=BI.ALL; DIRECT=none; WHY=fixture; %s`.\n" +
		"  - **TEST:** `TestX`.\n" +
		"  - **TEST MATRIX:** `PRIMARY=TestX`.\n" +
		"  - **RED:** red.\n" +
		"  - **GREEN:** green.\n" +
		"  - **REFACTOR:** refactor.\n" +
		"  - **Refs:** [fixture](fixture.md).\n"
	for _, declaration := range []string{
		"CAPABILITY=OTHER; OWNER=ARCHITECTURE_COUNCIL",
		"CAPABILITY=RUNTIME",
		"CAPABILITY=RUNTIME; OWNER=lowercase",
		"CAPABILITY=RUNTIME; OWNER=ONE; CAPABILITY=LIBRARY",
	} {
		t.Run(declaration, func(t *testing.T) {
			todos, errs := ParseTodos(fmt.Sprintf(base, declaration))
			if len(errs) == 0 || len(todos) != 0 {
				t.Fatalf("invalid declaration accepted: todos=%d errors=%v", len(todos), errs)
			}
		})
	}
}
