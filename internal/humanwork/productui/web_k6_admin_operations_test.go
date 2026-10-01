package productui

import (
	"strings"
	"testing"
)

func web6ReadyView(page PageID) View {
	return testView(page)
}

func TestTodo_WEB_233_Integration(t *testing.T) {
	view := web6ReadyView(PageIntegrationOperations)
	view.IntegrationOperations = &IntegrationOperationsProjection{
		Ready: true, ContractVersion: 1, ServiceVersion: "integration/v1", AsOf: "2026-09-29T12:00:00Z",
		Operations: []IntegrationOperationProjection{{ID: "op-7", Connector: "incumbent", Operation: "READ", State: "OBSERVED", StartedAt: "2026-09-29T11:59:00Z", FinishedAt: "2026-09-29T12:00:00Z", EvidenceRef: "evidence:op-7"}},
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"data-service-state=\"ready\"", "op-7", "incumbent", "OBSERVED", "evidence:op-7"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("integration operations missing %q in live projection: %s", want, doc)
		}
	}
	if strings.Contains(doc, "integration_operations.unavailable_title") {
		t.Fatal("live integration projection rendered the unavailable state")
	}
}

func TestTodo_WEB_233_Security(t *testing.T) {
	view := web6ReadyView(PageIntegrationOperations)
	view.IntegrationOperations = &IntegrationOperationsProjection{Ready: true, Operations: []IntegrationOperationProjection{{ID: "op-authorized", Connector: "connector-a", Operation: "READ", State: "SUCCEEDED", EvidenceRef: "sha256:authorized"}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "tenant-secret") || strings.Contains(doc, "client-secret") {
		t.Fatal("integration page rendered credential-like data")
	}
	if !strings.Contains(doc, "op-authorized") {
		t.Fatal("authorized operation was not rendered")
	}
}

func TestTodo_WEB_233_Fault(t *testing.T) {
	view := web6ReadyView(PageIntegrationOperations)
	view.IntegrationOperations = &IntegrationOperationsProjection{Ready: false, Operations: []IntegrationOperationProjection{{ID: "must-not-render", Connector: "connector-a"}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "must-not-render") || !strings.Contains(doc, view.Locale.Text("integration_operations.unavailable_title")) {
		t.Fatal("integration fault did not fail closed")
	}
}

func TestTodo_WEB_234_Integration(t *testing.T) {
	view := web6ReadyView(PageReconciliationWorkbench)
	view.ReconciliationWorkbench = &ReconciliationWorkbenchProjection{Ready: true, ContractVersion: 1, Slice: "workforce", Items: []ReconciliationFindingProjection{{ID: "finding-3", Kind: "RECONCILIATION_MISMATCH", Severity: "MAJOR", Resource: "worker:opaque", TargetCount: 2, EvidenceDigest: "sha256:finding-3", Actions: []string{"DATABASE_REPAIR"}}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"data-service-state=\"ready\"", "finding-3", "RECONCILIATION_MISMATCH", "DATABASE_REPAIR", "sha256:finding-3"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("reconciliation workbench missing %q: %s", want, doc)
		}
	}
}

func TestTodo_WEB_234_Security(t *testing.T) {
	view := web6ReadyView(PageReconciliationWorkbench)
	view.ReconciliationWorkbench = &ReconciliationWorkbenchProjection{Ready: true, Items: []ReconciliationFindingProjection{{ID: "finding-authorized", Resource: "resource-authorized", Kind: "OUTBOX_STUCK", Severity: "MINOR"}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "worker-private") || strings.Contains(doc, "raw-payload") {
		t.Fatal("reconciliation workbench rendered protected payload data")
	}
}

func TestTodo_WEB_234_Fault(t *testing.T) {
	view := web6ReadyView(PageReconciliationWorkbench)
	view.ReconciliationWorkbench = &ReconciliationWorkbenchProjection{Ready: false, Items: []ReconciliationFindingProjection{{ID: "must-not-render", Resource: "resource"}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "must-not-render") {
		t.Fatal("reconciliation fault did not fail closed")
	}
}

func TestTodo_WEB_235_Integration(t *testing.T) {
	view := web6ReadyView(PagePrivacyTelemetry)
	view.PrivacyTelemetry = &PrivacyTelemetryProjection{Ready: true, PolicyVersion: 1, PolicyDigest: "sha256:policy", SuccessSampleRate: "0.1", MetricCardinalityLimit: 256, CriticalFailureClasses: []string{"security_denial", "correctness_failure"}, EvidenceRef: "evidence:telemetry-policy"}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"data-service-state=\"ready\"", "sha256:policy", "0.1", "256", "security_denial", "correctness_failure", "evidence:telemetry-policy"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("privacy telemetry missing %q: %s", want, doc)
		}
	}
}

func TestTodo_WEB_235_Security(t *testing.T) {
	view := web6ReadyView(PagePrivacyTelemetry)
	view.PrivacyTelemetry = &PrivacyTelemetryProjection{Ready: true, PolicyVersion: 1, PolicyDigest: "sha256:public-policy", CriticalFailureClasses: []string{"security_denial"}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "person@example.com") || strings.Contains(doc, "tenant-secret") || strings.Contains(doc, "raw-field-value") {
		t.Fatal("privacy telemetry page rendered a raw customer or field value")
	}
}

func TestTodo_WEB_235_Fault(t *testing.T) {
	view := web6ReadyView(PagePrivacyTelemetry)
	view.PrivacyTelemetry = &PrivacyTelemetryProjection{Ready: false, PolicyDigest: "must-not-render"}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "must-not-render") {
		t.Fatal("privacy telemetry fault did not fail closed")
	}
}

func TestTodo_WEB_236_Integration(t *testing.T) {
	view := web6ReadyView(PagePerformanceBudgets)
	view.PerformanceBudgets = &PerformanceBudgetsProjection{Ready: true, ContractVersion: 1, ServiceVersion: "performance/v1", AsOf: "2026-09-29T12:00:00Z", Budgets: []PerformanceBudgetProjection{{ID: "budget-1", Name: "interaction-latency", Scope: "workspace", Target: "p95 <= 800ms", Observed: "p95 640ms", Status: "WITHIN_BUDGET", Version: "v3"}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"data-service-state=\"ready\"", "interaction-latency", "p95 &lt;= 800ms", "p95 640ms", "WITHIN_BUDGET", "v3"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("performance budgets missing %q: %s", want, doc)
		}
	}
}

func TestTodo_WEB_236_Security(t *testing.T) {
	view := web6ReadyView(PagePerformanceBudgets)
	view.PerformanceBudgets = &PerformanceBudgetsProjection{Ready: true, Budgets: []PerformanceBudgetProjection{{ID: "budget-safe", Name: "startup", Scope: "workspace", Target: "bounded", Status: "UNKNOWN"}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "worker-salary") || strings.Contains(doc, "tenant-secret") || strings.Contains(doc, "raw-payload") {
		t.Fatal("performance page rendered protected business data")
	}
}

func TestTodo_WEB_236_Fault(t *testing.T) {
	view := web6ReadyView(PagePerformanceBudgets)
	view.PerformanceBudgets = &PerformanceBudgetsProjection{Ready: false, Budgets: []PerformanceBudgetProjection{{ID: "must-not-render", Name: "fake"}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "must-not-render") {
		t.Fatal("performance budget fault did not fail closed")
	}
}
