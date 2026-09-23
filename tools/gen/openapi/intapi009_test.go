package openapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAlignmentFixture(t *testing.T, contract, generated, todos string) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"schema/openapi", "planning"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, data := range map[string]string{
		IntegrationContractPath: contract,
		DefaultOutputPath:       generated,
		TodosPath:               todos,
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const alignmentGenerated = `openapi: 3.1.0
info: {title: generated, version: 0.0.1}
paths:
  /hcmnext.widgets.v1.WidgetService/GetWidget:
    post: {}
`

const alignmentTodos = "- [ ] `INTAPI-010` read widgets\n- [ ] `INTAPI-011` write widgets\n- [x] `INTAPI-012` retired widgets\n"

// TestTodo_INTAPI_009 is the PRIMARY: the alignment check fails exactly
// the three GREEN cases (an operation without an existing owning todo, a
// SERVED operation with no matching generated RPC, a PLANNED operation
// whose todo is closed) and passes the aligned contract.
func TestTodo_INTAPI_009(t *testing.T) {
	aligned := `openapi: 3.1.0
info: {title: design, version: 0.0.1}
paths:
  /v1/widgets:
    get:
      x-hcmnext-todo: INTAPI-010
      x-hcmnext-status: SERVED
      x-hcmnext-rpc: hcmnext.widgets.v1.WidgetService/GetWidget
    post:
      x-hcmnext-todo: INTAPI-011
      x-hcmnext-status: PLANNED
`
	for _, tc := range []struct {
		name     string
		contract string
		todos    string
		want     []string
	}{
		{"aligned contract passes", aligned, alignmentTodos, nil},
		{"missing todo extension fails", "openapi: 3.1.0\ninfo: {title: d, version: 0.0.1}\npaths:\n  /v1/widgets:\n    get:\n      x-hcmnext-status: SERVED\n      x-hcmnext-rpc: hcmnext.widgets.v1.WidgetService/GetWidget\n", alignmentTodos,
			[]string{"GET /v1/widgets: operation names no x-hcmnext-todo backlog owner"}},
		{"unknown todo fails", strings.Replace(aligned, "INTAPI-011", "INTAPI-999", 1), alignmentTodos,
			[]string{"POST /v1/widgets: x-hcmnext-todo \"INTAPI-999\" names no backlog item in planning/todos.md"}},
		{"served without rpc fails", strings.Replace(aligned, "      x-hcmnext-rpc: hcmnext.widgets.v1.WidgetService/GetWidget\n", "", 1), alignmentTodos,
			[]string{"GET /v1/widgets: operation is SERVED but names no x-hcmnext-rpc backing call"}},
		{"served rpc missing from generated fails", strings.Replace(aligned, "WidgetService/GetWidget", "WidgetService/ListWidgets", 1), alignmentTodos,
			[]string{"GET /v1/widgets: x-hcmnext-rpc \"hcmnext.widgets.v1.WidgetService/ListWidgets\" is SERVED but matches no operation in schema/openapi/rpcs.openapi.yaml"}},
		{"planned with closed todo fails", strings.Replace(aligned, "INTAPI-011", "INTAPI-012", 1), alignmentTodos,
			[]string{"POST /v1/widgets: x-hcmnext-todo \"INTAPI-012\" is closed while the operation is still PLANNED"}},
		{"unknown status fails", strings.Replace(aligned, "PLANNED", "SOON", 1), alignmentTodos,
			[]string{"POST /v1/widgets: x-hcmnext-status \"SOON\" is neither SERVED nor PLANNED"}},
		{"missing status fails", strings.Replace(aligned, "      x-hcmnext-status: PLANNED\n", "", 1), alignmentTodos,
			[]string{"POST /v1/widgets: operation names no x-hcmnext-status (SERVED or PLANNED)"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := writeAlignmentFixture(t, tc.contract, alignmentGenerated, tc.todos)
			findings, err := CheckIntegrationAlignment(root)
			if err != nil {
				t.Fatalf("CheckIntegrationAlignment: %v", err)
			}
			var got []string
			for _, f := range findings {
				got = append(got, f.String())
			}
			if len(got) != len(tc.want) {
				t.Fatalf("findings = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("findings = %v, want %v", got, tc.want)
				}
			}
		})
	}

	if _, err := CheckIntegrationAlignment(t.TempDir()); err == nil {
		t.Fatal("missing contract files passed the check")
	}
	if _, err := CheckIntegrationAlignment(filepath.Join("testdata", "does-not-exist")); err == nil {
		t.Fatal("missing repository root passed the check")
	}
}

// TestTodo_INTAPI_009_Golden pins the live tree: the checked-in design
// contract names an existing backlog todo on every operation, every
// SERVED operation matches a generated RPC, and no PLANNED operation
// hides behind a closed todo.
func TestTodo_INTAPI_009_Golden(t *testing.T) {
	findings, err := CheckIntegrationAlignment(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		t.Errorf("alignment finding: %s", f)
	}
}
