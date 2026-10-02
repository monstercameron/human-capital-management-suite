package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// agent2017Client answers with one task that carries every detail section.
type agent2017Client struct{}

func (agent2017Client) Snapshot(_ context.Context, req productui.AgentSnapshotRequest) (productui.AgentSnapshot, error) {
	return productui.AgentSnapshot{Availability: productui.AgentsAvailable, Tasks: []productui.AgentTask{{
		ID: "task-promote", Version: 7, Title: "Prepare the promotion request", Goal: "Prepare the promotion request\nUse the calibration notes.", State: productui.AgentTaskAwaitingApproval, PlanRevision: "2",
		Steps:            []productui.AgentTaskStep{{Name: "agent.read_handbook", State: "completed", Tier: "T0"}, {Name: "people.promote_worker", State: "awaiting_approval", Tier: "T3"}},
		Checkpoints:      []productui.AgentCheckpoint{{Kind: productui.AgentCheckpointPlanConfirmed, At: "2026-10-02T09:01:00Z"}, {Kind: productui.AgentCheckpointStepFinished, Step: "agent.read_handbook", At: "2026-10-02T09:03:00Z"}},
		Artifacts:        []productui.AgentArtifact{{Kind: productui.AgentArtifactDocument, Name: "Calibration notes", Href: "/workspace/app/docs?document=doc-calibration"}},
		Approvals:        []productui.AgentApproval{{ID: "task-promote/submit", Digest: "sha256:approve-submit", Summary: "people.promote_worker", Sources: []string{"document:Calibration notes", "request"}, Taint: "AGENT_DERIVED"}},
		SubmittedIntents: []productui.AgentIntentStatus{{Name: "people.promote_worker", Status: productui.AgentIntentAwaitingApproval}},
		PlanChanges:      []productui.AgentPlanChange{{Change: productui.AgentPlanChangeAdded, Step: productui.AgentTaskStep{Name: "agent.read_handbook", Tier: "T0"}}},
	}, {ID: "task-quick", Version: 1, Title: "A quick answer for " + req.Principal, State: productui.AgentTaskCompleted}}}, nil
}

// TestTodo_AGENT2_017_PageDocument checks the page document the server
// writes: the task detail the agent client derived is in it, survives the
// trip through JSON, and is what the server's own first paint reads.
func TestTodo_AGENT2_017_PageDocument(t *testing.T) {
	h := &Handler{agentSettings: &memoryAgentSettings{enabled: map[values.TenantId]bool{shellTenant: true}}, agents: agent2017Client{}}
	config := h.resolveAgents(context.Background(), uxblind122Principal(t), productAccess{roles: []string{"worker_self"}})
	if config == nil || !config.Enabled || len(config.Tasks) != 2 {
		t.Fatalf("agents config = %+v", config)
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"detail":{`, `"checkpoints":[{"kind":"plan_confirmed","at":"2026-10-02T09:01:00Z"}`, `"artifacts":[{"kind":"document","name":"Calibration notes"`,
		`"submitted_intents":[{"name":"people.promote_worker","status":"awaiting_approval"}]`, `"plan_changes":[{"change":"added","step":"agent.read_handbook","tier":"T0"}]`,
		`"sources":["document:Calibration notes","request"]`, `"taint":"AGENT_DERIVED"`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the page document is missing %s: %s", want, raw)
		}
	}
	if strings.Count(string(raw), `"detail"`) != 1 {
		t.Errorf("a task with no detail was given an empty one: %s", raw)
	}
	var decoded AgentsConfig
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	task := ProductAgentsAvailability(&decoded).Snapshot.Tasks[0]
	if len(task.Checkpoints) != 2 || task.Checkpoints[1].Step != "agent.read_handbook" || len(task.Artifacts) != 1 || task.Artifacts[0].Href != "/workspace/app/docs?document=doc-calibration" ||
		len(task.SubmittedIntents) != 1 || len(task.PlanChanges) != 1 || task.PlanChanges[0].Step.Tier != "T0" ||
		len(task.Approvals) != 1 || task.Approvals[0].Taint != "AGENT_DERIVED" || len(task.Approvals[0].Sources) != 2 {
		t.Fatalf("the detail did not survive the page document: %+v", task)
	}
}
