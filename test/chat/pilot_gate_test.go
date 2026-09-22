package chat_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type pilotGateFixture struct {
	PilotOwner                      string   `json:"pilot_owner"`
	Cohorts                         []string `json:"cohorts"`
	RequiredMetrics                 []string `json:"required_metrics"`
	KillSwitch                      string   `json:"kill_switch"`
	RollbackPlan                    string   `json:"rollback_plan"`
	WorkflowRegressionBudgetPercent int      `json:"workflow_regression_budget_percent"`
	Evidence                        struct {
		SignedWorkflowBaseline bool `json:"signed_workflow_baseline"`
		ObservedEmployeePilot  bool `json:"observed_employee_pilot"`
		IncidentDrill          bool `json:"incident_drill"`
	} `json:"evidence"`
}

func TestTodo_CHAT_053(t *testing.T) {
	f := loadPilotFixture(t)
	if f.PilotOwner == "" || len(f.Cohorts) == 0 || len(f.RequiredMetrics) == 0 {
		t.Fatal("pilot gate must name an owner, cohorts, and measurable metrics")
	}
	if f.KillSwitch == "" || f.RollbackPlan == "" {
		t.Fatal("pilot gate must name both a kill switch and rollback plan")
	}
	if f.WorkflowRegressionBudgetPercent <= 0 || f.WorkflowRegressionBudgetPercent >= 100 {
		t.Fatalf("workflow regression budget = %d, want a bounded percentage", f.WorkflowRegressionBudgetPercent)
	}
	if ready, reasons := pilotReady(f); ready || len(reasons) == 0 {
		t.Fatalf("pilot with no real evidence ready=%v reasons=%v; external pilot must remain open", ready, reasons)
	}
}

func TestTodo_CHAT_053_Conformance(t *testing.T) {
	f := loadPilotFixture(t)
	f.Evidence.SignedWorkflowBaseline = true
	f.Evidence.ObservedEmployeePilot = true
	f.Evidence.IncidentDrill = true
	if ready, reasons := pilotReady(f); !ready || len(reasons) != 0 {
		t.Fatalf("complete evidence ready=%v reasons=%v", ready, reasons)
	}
}

func loadPilotFixture(t *testing.T) pilotGateFixture {
	t.Helper()
	path := filepath.Join("testdata", "pilot_gate.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pilot fixture: %v", err)
	}
	var f pilotGateFixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("decode pilot fixture: %v", err)
	}
	return f
}

func pilotReady(f pilotGateFixture) (bool, []string) {
	var reasons []string
	if f.PilotOwner == "" {
		reasons = append(reasons, "pilot_owner")
	}
	if len(f.Cohorts) == 0 {
		reasons = append(reasons, "cohorts")
	}
	if !f.Evidence.SignedWorkflowBaseline {
		reasons = append(reasons, "signed_workflow_baseline")
	}
	if !f.Evidence.ObservedEmployeePilot {
		reasons = append(reasons, "observed_employee_pilot")
	}
	if !f.Evidence.IncidentDrill {
		reasons = append(reasons, "incident_drill")
	}
	if f.KillSwitch == "" {
		reasons = append(reasons, "kill_switch")
	}
	if f.RollbackPlan == "" {
		reasons = append(reasons, "rollback_plan")
	}
	return len(reasons) == 0, reasons
}
