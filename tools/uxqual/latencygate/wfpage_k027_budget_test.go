package latencygate

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestTodo_WFPAGE_027(t *testing.T) {
	budget := DefaultWorkflowPageBudget()
	if err := ValidateWorkflowPageBudgetDefinition(budget); err != nil {
		t.Fatal(err)
	}
	if budget.InitialGzipBytes != 150*1024 || budget.WarmWorkflowInteractive != 1500*time.Millisecond || budget.WarmDesignerOpen != 2500*time.Millisecond {
		t.Fatalf("budget = %+v, want governed limits", budget)
	}
}

func TestTodo_WFPAGE_027_Performance(t *testing.T) {
	budget := DefaultWorkflowPageBudget()
	if err := CheckWorkflowPageBudget(budget, WorkflowPageBundleMeasurement{InitialGzipBytes: budget.InitialGzipBytes + 1, WorkflowInteractive: time.Second, DesignerOpen: 2 * time.Second}); err == nil {
		t.Fatal("over-size initial bundle passed the budget")
	}
	if err := CheckWorkflowPageBudget(budget, WorkflowPageBundleMeasurement{InitialGzipBytes: 100 * 1024, WorkflowInteractive: 1501 * time.Millisecond, DesignerOpen: 2 * time.Second}); err == nil {
		t.Fatal("slow workflow page passed the budget")
	}
	compressed, err := GzipBytes([]byte(strings.Repeat("workflow-page-rule", 1000)))
	if err != nil || compressed <= 0 {
		t.Fatalf("GzipBytes() = %d, %v", compressed, err)
	}
}

func TestTodo_WFPAGE_027_Golden(t *testing.T) {
	budget := DefaultWorkflowPageBudget()
	golden := "150KB-gzip|1.5s|2.5s|" + strings.Join(budget.InitialSurfaces, ",") + "|lazy:" + strings.Join(budget.DesignerRouteSurfaces, ",")
	digest := sha256.Sum256([]byte(golden))
	if got := hex.EncodeToString(digest[:]); got != "57f612d94ed14faedbafe0a07736e1eeb0491030499d6c6b85f05845a8eeefd1" {
		// The assertion is intentionally computed from the contract string; the
		// failure message keeps a changed budget reviewable without accepting it.
		t.Fatalf("workflow page budget golden = %s", got)
	}
}

func TestTodo_WFPAGE_027_Conformance(t *testing.T) {
	budget := DefaultWorkflowPageBudget()
	if err := CheckWorkflowPageBudget(budget, WorkflowPageBundleMeasurement{InitialGzipBytes: 149 * 1024, WorkflowInteractive: 1499 * time.Millisecond, DesignerOpen: 2499 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	if err := CheckWorkflowPageBudget(budget, WorkflowPageBundleMeasurement{InitialGzipBytes: 149 * 1024, WorkflowInteractive: time.Second, DesignerOpen: 2 * time.Second, DesignerLoadedOnInitial: true}); err == nil {
		t.Fatal("designer assets loaded in the initial bundle passed conformance")
	}
}
