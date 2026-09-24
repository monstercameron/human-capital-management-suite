package ownershipboundaries

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func hasCode(fs []Finding, code string) bool {
	for _, f := range fs {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestHumanInteractionBoundaries(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/workflow/bad.go":  "package workflow\nimport _ \"net/smtp\"\ntype Queue struct{}\nfunc Claim() {}\n",
		"internal/forms/bad.go":     "package forms\ntype Approval struct{}\n",
		"internal/messaging/bad.go": "package messaging\nfunc EscalateBusiness() {}\n",
		"internal/humanwork/bad.go": "package humanwork\nimport _ \"github.com/twilio/twilio-go\"\n",
	})
	findings := Check(root)
	for _, code := range []string{"workflow-owns-human-work", "forms-owns-approval", "messaging-owns-escalation", "human-work-provider-import"} {
		if !hasCode(findings, code) {
			t.Fatalf("missing %s in %#v", code, findings)
		}
	}
}

func TestHumanInteractionBoundariesAllowOwnerPackages(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/work/items.go":    "package work\ntype Queue struct{}\nfunc Claim() {}\n",
		"internal/forms/forms.go":   "package forms\ntype FormDefinition struct{}\nfunc ValidateSubmission() {}\n",
		"internal/messaging/msg.go": "package messaging\ntype MessageIntent struct{}\nfunc ReconcileDelivery() {}\n",
	})
	if findings := Check(root); len(findings) != 0 {
		t.Fatalf("unexpected findings: %#v", findings)
	}
}

func TestCheckIsDeterministic(t *testing.T) {
	root := fixture(t, map[string]string{"internal/workflow/bad.go": "package workflow\ntype Queue struct{}\n"})
	a, b := Check(root), Check(root)
	if len(a) != len(b) || len(a) == 0 || a[0].String() != b[0].String() {
		t.Fatalf("results differ: %#v %#v", a, b)
	}
}

func TestTodo_ARCH_GO_022_Browser(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/workflow/screen.go": "package workflow\nfunc RenderScreen() {}\n",
	})
	if findings := Check(root); len(findings) != 0 {
		t.Fatalf("workflow presentation entry point should be allowed: %#v", findings)
	}
}
func TestTodo_ARCH_GO_022_Conformance(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/forms/approval.go": "package forms\ntype approvalDraft struct{}\nfunc validateApproval() {}\n",
	})
	if findings := Check(root); len(findings) != 0 {
		t.Fatalf("unexported form implementation details should be allowed: %#v", findings)
	}
}
func TestTodo_ARCH_GO_022_Golden(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/workflow/bad.go":  "package workflow\ntype Queue struct{}\n",
		"internal/forms/bad.go":     "package forms\ntype Approval struct{}\n",
		"internal/messaging/bad.go": "package messaging\nfunc EscalateBusiness() {}\n",
	})
	findings := Check(root)
	for _, code := range []string{"workflow-owns-human-work", "forms-owns-approval", "messaging-owns-escalation"} {
		if !hasCode(findings, code) {
			t.Errorf("missing boundary finding %q in %#v", code, findings)
		}
	}
}
func TestTodo_ARCH_GO_022_Integration(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/humanwork/provider.go": "package humanwork\nimport _ \"github.com/twilio/twilio-go\"\n",
	})
	findings := Check(root)
	if len(findings) != 1 || findings[0].Code != "human-work-provider-import" {
		t.Fatalf("provider integration boundary findings = %#v", findings)
	}
}
func TestTodo_ARCH_GO_022_Mutation(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/workflow/private.go": "package workflow\ntype queue struct{}\nfunc claim() {}\n",
	})
	if findings := Check(root); len(findings) != 0 {
		t.Fatalf("private workflow helpers should not create exported ownership: %#v", findings)
	}
}
func TestTodo_ARCH_GO_022_Race(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/workflow/bad.go": "package workflow\ntype Queue struct{}\n",
		"internal/forms/bad.go":    "package forms\ntype Approval struct{}\n",
	})
	type result struct{ findings []Finding }
	const workers = 8
	results := make(chan result, workers)
	for i := 0; i < workers; i++ {
		go func() { results <- result{findings: Check(root)} }()
	}
	for i := 0; i < workers; i++ {
		got := <-results
		if len(got.findings) != 2 || !hasCode(got.findings, "workflow-owns-human-work") || !hasCode(got.findings, "forms-owns-approval") {
			t.Fatalf("concurrent ownership scan = %#v", got.findings)
		}
	}
}
