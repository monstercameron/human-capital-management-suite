package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// TestTodo_AGENT2_017_ClientProjection walks the task detail the server
// derives (saved progress, what the task produced, what an approval rests on,
// submissions, plan changes) from the page document to the page: the client
// must read every field the server writes, and read it the way the server's
// own first render does. The two sides keep separate copies of the document
// shape, so a field added to one and not the other shows up here.
func TestTodo_AGENT2_017_ClientProjection(t *testing.T) {
	island := workspace.AgentsConfig{Enabled: true, Service: "available", StartAvailable: true, Tasks: []workspace.AgentTaskConfig{{
		ID: "task-promote", Version: 7, Title: "Prepare the promotion request", Goal: "Prepare the promotion request\nUse the calibration notes.", State: "awaiting_approval", PlanRevision: "2",
		Actions:   workspace.AgentTaskActionPolicy{Pause: true, Cancel: true},
		Steps:     []workspace.AgentStepConfig{{Name: "agent.read_handbook", State: "completed", Tier: "T0"}, {Name: "people.promote_worker", State: "awaiting_approval", Tier: "T3"}},
		Approvals: []workspace.AgentApprovalConfig{{ID: "task-promote/submit", Digest: "sha256:approve-submit", Summary: "people.promote_worker", Sources: []string{"document:Calibration notes", "records", "request"}, Taint: "AGENT_DERIVED,USER_AUTHORED"}},
		Detail: &workspace.AgentTaskDetailConfig{
			Checkpoints: []workspace.AgentCheckpointConfig{
				{Kind: productui.AgentCheckpointPlanConfirmed, At: "2026-10-02T09:01:00Z"},
				{Kind: productui.AgentCheckpointStepFinished, Step: "agent.read_handbook", At: "2026-10-02T09:03:00Z"},
				{Kind: productui.AgentCheckpointWaitingApproval, At: "2026-10-02T09:10:00Z"},
			},
			Artifacts:        []workspace.AgentArtifactConfig{{Kind: productui.AgentArtifactDocument, Name: "Calibration notes", Href: "/workspace/app/docs?document=doc-calibration"}, {Kind: productui.AgentArtifactDraft}},
			SubmittedIntents: []workspace.AgentIntentConfig{{Name: "people.promote_worker", Status: productui.AgentIntentAwaitingApproval}},
			PlanChanges:      []workspace.AgentPlanChangeConfig{{Change: productui.AgentPlanChangeAdded, Step: "agent.read_handbook", Tier: "T0"}, {Change: productui.AgentPlanChangeRemoved, Step: "skill.lookup", Tier: "T0"}},
		},
	}, {ID: "task-quick", Version: 1, Title: "A quick answer", State: "completed"}}}
	raw, err := json.Marshal(island)
	if err != nil {
		t.Fatal(err)
	}
	var received journeyclient.Agents
	if err := json.Unmarshal(raw, &received); err != nil {
		t.Fatal(err)
	}
	projection := projectAgents(&received)
	if projection == nil || len(projection.Snapshot.Tasks) != 2 {
		t.Fatalf("projection lost tasks: %+v", projection)
	}
	client, server := projection.Snapshot.Tasks[0], workspace.ProductAgentsAvailability(&island).Snapshot.Tasks[0]
	if len(client.Checkpoints) != 3 || len(client.Artifacts) != 2 || len(client.SubmittedIntents) != 1 || len(client.PlanChanges) != 2 || len(client.Approvals) != 1 {
		t.Fatalf("the client dropped task detail: %+v", client)
	}
	for name, pair := range map[string][2]any{
		"checkpoints":       {client.Checkpoints, server.Checkpoints},
		"artifacts":         {client.Artifacts, server.Artifacts},
		"approvals":         {client.Approvals, server.Approvals},
		"submitted intents": {client.SubmittedIntents, server.SubmittedIntents},
		"plan changes":      {client.PlanChanges, server.PlanChanges},
	} {
		if !reflect.DeepEqual(pair[0], pair[1]) {
			t.Errorf("%s differ between the browser and the server render:\n browser %+v\n server  %+v", name, pair[0], pair[1])
		}
	}
	if got := client.Approvals[0]; got.Taint != "AGENT_DERIVED,USER_AUTHORED" || len(got.Sources) != 3 || got.Sources[0] != "document:Calibration notes" {
		t.Errorf("what the approval rests on did not reach the page: %+v", got)
	}
	if got := client.Checkpoints[1]; got.Kind != productui.AgentCheckpointStepFinished || got.Step != "agent.read_handbook" || got.At != "2026-10-02T09:03:00Z" {
		t.Errorf("checkpoint = %+v", got)
	}
	// A task with no detail stays without one.
	if quick := projection.Snapshot.Tasks[1]; len(quick.Checkpoints)+len(quick.Artifacts)+len(quick.SubmittedIntents)+len(quick.PlanChanges) != 0 {
		t.Errorf("a quick answer was given task detail: %+v", quick)
	}
	// A status refresh that knows less than the page document keeps the
	// detail the document supplied; the next document replaces it.
	refreshed := mergeAgentTask(client, productui.AgentTask{ID: client.ID, Version: 8, State: productui.AgentTaskAwaitingApproval, Title: client.Title})
	if len(refreshed.Checkpoints) != 3 || len(refreshed.Approvals) != 1 || refreshed.Version != 8 {
		t.Errorf("a status refresh dropped the task detail: %+v", refreshed)
	}
}
