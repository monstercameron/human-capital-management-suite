package workflowconformance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

func TestTodo_REV_048_01(t *testing.T) {
	root := t.TempDir()
	workflowPath := filepath.Join(root, "planning", "workflows", "people", "sample.md")
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflowPath, []byte("# Sample\n\n## Identity and scope\n\n```text\nworkflow_id: sample/v1\nstate: EXTRACTED + EXPLORED\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	todo := todoregistry.Todo{ID: "CONF-TEST", Phase: "CONFORMANCE", Role: "CONFORMANCE", Done: true, Refs: "[sample](workflows/people/sample.md)"}
	findings, err := Check(root, []todoregistry.Todo{todo})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %v, want one unpromoted workflow finding", findings)
	}
	unsafeRef := todo
	unsafeRef.Refs = "[escaped](workflows/../missing.md)"
	findings, err = Check(root, []todoregistry.Todo{unsafeRef})
	if err != nil || len(findings) != 0 {
		t.Fatalf("escaped workflow ref findings = %v, err = %v", findings, err)
	}
	conflicted := todo
	conflicted.PrePromotionExploratory = true
	conflicted.IntentContextConflict = true
	findings, err = Check(root, []todoregistry.Todo{conflicted})
	if err != nil || len(findings) != 1 || !strings.Contains(findings[0].Issue, "conflicting ROLE") {
		t.Fatalf("conflicting context findings = %v, err = %v", findings, err)
	}
	todo.PrePromotionExploratory = true
	findings, err = Check(root, []todoregistry.Todo{todo})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("explicit exploratory todo findings = %v, want none", findings)
	}
	todo.PrePromotionExploratory = false
	if err := os.WriteFile(workflowPath, []byte("# Sample\n\n## Identity and scope\n\n```text\nworkflow_id: sample/v1\nstate: CONTRACTED\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err = Check(root, []todoregistry.Todo{todo})
	if err != nil || len(findings) != 1 || !strings.Contains(findings[0].String(), "trusted workflow-bound promotion receipt") {
		t.Fatalf("unreceipted contracted workflow findings = %v, err = %v", findings, err)
	}
	contracted := Finding{TodoID: "CONF-TEST", Path: "planning/workflows/people/sample.md", State: "CONTRACTED"}
	wantContracted := `CONF-TEST: workflow planning/workflows/people/sample.md declares CONTRACTED without a trusted workflow-bound promotion receipt`
	if got := contracted.String(); got != wantContracted {
		t.Fatalf("contracted finding = %q, want %q", got, wantContracted)
	}
}

func TestTodo_REV_048_01_MetadataEvasion(t *testing.T) {
	root := t.TempDir()
	workflowPath := filepath.Join(root, "planning", "workflows", "people", "sample.md")
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		t.Fatal(err)
	}
	todo := todoregistry.Todo{ID: "CONF-EVASION", Phase: "CONFORMANCE", Role: "CONFORMANCE", Done: true, Refs: "[workflow](<./workflows/people/sample.md>)"}
	cases := []struct {
		name    string
		content string
		issue   string
	}{
		{"workflow id in prose only", "# Sample\n\nworkflow_id: sample/v1\nstate: CONTRACTED\n", "workflow_id must appear exactly once"},
		{"state in prose only", "# Sample\n\n## Identity and scope\n\n```text\nworkflow_id: sample/v1\n```\n\nstate: CONTRACTED\n", "state must appear exactly once"},
		{"duplicate state", "# Sample\n\n## Identity and scope\n\n```text\nworkflow_id: sample/v1\nstate: EXTRACTED\nstate: CONTRACTED\n```\n", "state must appear exactly once"},
		{"missing state", "# Sample\n\n## Identity and scope\n\n```text\nworkflow_id: sample/v1\n```\n", "state must appear exactly once"},
		{"empty state", "# Sample\n\n## Identity and scope\n\n```text\nworkflow_id: sample/v1\nstate:\n```\n", "state must appear exactly once"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(workflowPath, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			findings, err := Check(root, []todoregistry.Todo{todo})
			if err != nil {
				t.Fatal(err)
			}
			if len(findings) != 1 || !strings.Contains(findings[0].Issue, tc.issue) {
				t.Fatalf("findings = %v, want issue %q", findings, tc.issue)
			}
		})
	}
}

func TestWorkflowRefsAcceptMarkdownPathFormsAndDeduplicate(t *testing.T) {
	forms := []string{
		"[plain](workflows/people/a.md)",
		"[relative](./workflows/people/a.md)",
		"[angle](<./workflows/people/a.md>)",
		"[backtick](`workflows/people/a.md`)",
		"`workflows/people/a.md`",
		"[rooted](planning/workflows/people/a.md)",
	}
	for _, form := range forms {
		got := workflowRefs(form)
		if len(got) != 1 || got[0] != "planning/workflows/people/a.md" {
			t.Errorf("workflowRefs(%q) = %v, want the normalized workflow path", form, got)
		}
	}
	refs := strings.Join(forms, " ")
	got := workflowRefs(refs)
	if len(got) != 1 {
		t.Fatalf("workflowRefs(%q) = %v, want one deduplicated path", refs, got)
	}
	if got[0] != "planning/workflows/people/a.md" {
		t.Fatalf("normalized ref = %q", got[0])
	}
}

func TestTodo_REV_048_01_SupportReferencesAreNotCatalogWorkflows(t *testing.T) {
	root := t.TempDir()
	supportPath := filepath.Join(root, "planning", "workflows", "_engine", "step-types.md")
	if err := os.MkdirAll(filepath.Dir(supportPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(supportPath, []byte("state: EXTRACTED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	todo := todoregistry.Todo{ID: "CONF-SUPPORT", Phase: "CONFORMANCE", Role: "CONFORMANCE", Done: true, Refs: "[support](<./workflows/_engine/step-types.md>)"}
	findings, err := Check(root, []todoregistry.Todo{todo})
	if err != nil || len(findings) != 0 {
		t.Fatalf("support reference findings = %v, err = %v", findings, err)
	}
}

func TestTodo_REV_048_01_Golden(t *testing.T) {
	finding := Finding{TodoID: "CONF-TEST", Path: "planning/workflows/people/sample.md", State: "EXTRACTED + EXPLORED"}
	want := `CONF-TEST: ticked CONFORMANCE todo cites workflow planning/workflows/people/sample.md in state "EXTRACTED + EXPLORED" without PRE_PROMOTION_EXPLORATORY=true`
	if got := finding.String(); got != want {
		t.Fatalf("finding = %q, want %q", got, want)
	}
}

func TestTodo_REV_048_01_Conformance(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	refs := []string{
		"workflows/people/contact-information-update.md",
		"workflows/people/emergency-contact-update.md",
		"workflows/people/legal-name-change.md",
		"workflows/rewards/compensation-change.md",
		"workflows/workforce/headcount-requisition.md",
	}
	todos := make([]todoregistry.Todo, 0, len(refs))
	ids := []string{"CONF-017", "CONF-018", "CONF-019", "CONF-020", "CONF-021"}
	for i, ref := range refs {
		todos = append(todos, todoregistry.Todo{ID: ids[i], Phase: "CONFORMANCE", Role: "CONFORMANCE", Done: true, Refs: "[workflow](" + ref + ")"})
	}
	findings, err := Check(root, todos)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != len(refs) {
		t.Fatalf("live exploratory workflow findings = %v, want %d", findings, len(refs))
	}
	for i, finding := range findings {
		if !strings.Contains(finding.Path, filepath.Base(refs[i])) || finding.State != "EXTRACTED + EXPLORED" {
			t.Errorf("finding %d = %+v, does not match source %s", i, finding, refs[i])
		}
	}
}
