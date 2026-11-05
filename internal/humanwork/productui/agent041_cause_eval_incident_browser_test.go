package productui

import (
	"strings"
	"testing"
)

// TestTodo_AGENT_041_CauseEvalIncident renders a run's trace facts: what
// caused it, the evaluation outcome of the version it executed, and whether
// stop evidence links it to an incident. Causes and evaluations print only
// catalog words for known codes in the viewer's language; the incident
// reference itself never reaches the page, only that one is recorded.
func TestTodo_AGENT_041_CauseEvalIncident(t *testing.T) {
	traced := AgentControlRun{ID: "task-traced", AgentID: "policy-helper", Name: "Policy Helper", Revision: 5, Version: "4", State: "COMPLETED", Started: "2026-10-22T09:00:00Z", Since: "2026-10-22T09:00:09Z", Duration: "9s", RequestedBy: "Ana Flores", Cause: "SCHEDULE", Evals: []string{"passed"}, Incident: "INC-1042"}
	hostile := AgentControlRun{ID: "task-hostile", AgentID: "policy-helper", Name: "Policy Helper", Revision: 3, Version: "4", State: "COMPLETED", Started: "2026-10-22T09:00:00Z", Since: "2026-10-22T09:00:09Z", Duration: "9s", RequestedBy: "Ana Flores", Cause: "<script>alert(1)</script>", Evals: []string{"<img src=x onerror=alert(1)>"}, Incident: "incident-secret"}
	plain := AgentControlRun{ID: "task-plain", AgentID: "policy-helper", Name: "Policy Helper", Revision: 2, Version: "4", State: "COMPLETED", Started: "2026-10-22T09:00:00Z", Since: "2026-10-22T09:00:09Z", Duration: "9s", RequestedBy: "Ana Flores"}
	snapshot := AgentControlsSnapshot{Available: true, OwnerName: "Walt Brennan", UpdatedAt: "2026-10-22T09:20:00Z", Runs: []AgentControlRun{traced, hostile, plain}}
	want := map[string][3]string{
		"en-US": {"Run cause: </strong>Schedule", "Evaluations: </strong>Passed", "Incident reference: </strong>Recorded"},
		"de-DE": {"Auslöser: </strong>Zeitplan", "Bewertungen: </strong>Bestanden", "Vorfallsreferenz: </strong>Erfasst"},
		"ar":    {"سبب التشغيل: </strong>جدول زمني", "التقييمات: </strong>ناجح", "مرجع الحادث: </strong>مسجّل"},
	}
	for localeID, facts := range want {
		locale := ResolveProductLocale(localeID)
		activity := agentMatrixRender(t, RenderAgentControls(locale, snapshot, "done"))
		tracedBlock := agentMatrixBlock(t, activity, `<tr data-control-run="task-traced"`, "tr")
		for _, fact := range facts {
			if !strings.Contains(tracedBlock, fact) {
				t.Errorf("%s traced run does not show %q: %s", localeID, fact, tracedBlock)
			}
		}
		if strings.Contains(tracedBlock, "INC-1042") {
			t.Errorf("%s traced run prints its incident reference: %s", localeID, tracedBlock)
		}
		hostileBlock := agentMatrixBlock(t, activity, `<tr data-control-run="task-hostile"`, "tr")
		for _, raw := range []string{"<script>", "onerror", "secret"} {
			if strings.Contains(hostileBlock, raw) {
				t.Errorf("%s hostile run prints %q: %s", localeID, raw, hostileBlock)
			}
		}
		if !strings.Contains(hostileBlock, facts[2]) {
			t.Errorf("%s hostile run hides that an incident is recorded: %s", localeID, hostileBlock)
		}
		plainBlock := agentMatrixBlock(t, activity, `<tr data-control-run="task-plain"`, "tr")
		for _, fact := range facts {
			if strings.Contains(plainBlock, fact) {
				t.Errorf("%s plain run shows %q: %s", localeID, fact, plainBlock)
			}
		}
	}
}

// TestTodo_AGENT_041_ExpiredEval renders a lapsed version evaluation as its
// own outcome, distinct from a passing one and from no evaluation at all.
func TestTodo_AGENT_041_ExpiredEval(t *testing.T) {
	run := AgentControlRun{ID: "task-expired", AgentID: "policy-helper", Name: "Policy Helper", Revision: 5, Version: "4", State: "COMPLETED", Started: "2026-10-22T09:00:00Z", Since: "2026-10-22T09:00:09Z", Duration: "9s", RequestedBy: "Ana Flores", Evals: []string{"expired"}}
	snapshot := AgentControlsSnapshot{Available: true, OwnerName: "Walt Brennan", UpdatedAt: "2026-10-22T09:20:00Z", Runs: []AgentControlRun{run}}
	locale := ResolveProductLocale("en-US")
	activity := agentMatrixRender(t, RenderAgentControls(locale, snapshot, "done"))
	block := agentMatrixBlock(t, activity, `<tr data-control-run="task-expired"`, "tr")
	if !strings.Contains(block, "Evaluations: </strong>Expired") {
		t.Errorf("expired evaluation does not render: %s", block)
	}
}
