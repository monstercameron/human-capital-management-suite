package project

import (
	"errors"
	"strings"
	"testing"
)

func pm038Proposal() BoardProposal {
	return BoardProposal{
		SchemaVersion:       BoardProposalSchemaVersion1,
		EnabledCapabilities: []BoardProposalCapability{BoardCapabilityStatuses, BoardCapabilityTransitions, BoardCapabilityTaskTypes, BoardCapabilityBoardViews, BoardCapabilitySampleTasks},
		Workflow: BoardWorkflowProposal{
			Statuses:    []BoardProposalStatus{{ID: "intake", Name: "Intake"}, {ID: "done", Name: "Done"}},
			Transitions: []BoardProposalTransition{{From: "intake", To: "done"}},
			TaskTypes:   []BoardProposalTaskType{{ID: "task", Name: "Task", InitialStatus: "intake"}},
		},
		Views:       []BoardViewProposal{{ID: "board", Kind: "BOARD", StatusIDs: []string{"intake", "done"}}},
		SampleTasks: []BoardSampleTask{{ID: "sample-1", Title: "Synthetic example", StatusID: "intake", Synthetic: true}},
		Rationale:   "Make recurring operations visible.",
		Assumptions: []string{"The team uses one intake queue."},
		Unknowns:    []string{"Whether a list view is needed."},
	}
}

func TestTodo_PM_038(t *testing.T) {
	proposal := pm038Proposal()
	if err := proposal.Validate(); err != nil {
		t.Fatalf("valid proposal rejected: %v", err)
	}
	if proposal.SampleTasks[0].Synthetic != true || len(proposal.Assumptions) != 1 || len(proposal.Unknowns) != 1 {
		t.Fatalf("proposal lost safety metadata: %+v", proposal)
	}
}

func TestTodo_PM_038_Golden(t *testing.T) {
	got, err := pm038Proposal().CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schema_version":1,"enabled_capabilities":["WORKFLOW_STATUSES","WORKFLOW_TRANSITIONS","TASK_TYPES","BOARD_VIEWS","SAMPLE_TASKS"],"workflow":{"statuses":[{"id":"intake","name":"Intake"},{"id":"done","name":"Done"}],"transitions":[{"from":"intake","to":"done"}],"task_types":[{"id":"task","name":"Task","initial_status":"intake"}],"fields":null},"views":[{"id":"board","kind":"BOARD","status_ids":["intake","done"]}],"sample_tasks":[{"id":"sample-1","title":"Synthetic example","status_id":"intake","synthetic":true}],"rationale":"Make recurring operations visible.","assumptions":["The team uses one intake queue."],"unknowns":["Whether a list view is needed."]}`
	if string(got) != want {
		t.Fatalf("canonical proposal changed\n got: %s\nwant: %s", got, want)
	}
}

func TestTodo_PM_038_Security(t *testing.T) {
	tests := []struct {
		name string
		edit func(*BoardProposal)
		want error
	}{
		{"unsupported capability", func(p *BoardProposal) { p.EnabledCapabilities = append(p.EnabledCapabilities, "SCRIPT_EXECUTION") }, ErrUnsupportedProposal},
		{"cycle", func(p *BoardProposal) {
			p.Workflow.Transitions = append(p.Workflow.Transitions, BoardProposalTransition{From: "done", To: "intake"})
		}, ErrInvalidBoardProposal},
		{"live sample", func(p *BoardProposal) { p.SampleTasks[0].Synthetic = false }, ErrLiveSampleTask},
		{"script", func(p *BoardProposal) { p.Scripts = []string{"run arbitrary code"} }, ErrUnsafeProposalMaterial},
		{"permission", func(p *BoardProposal) { p.Permissions = []string{"grant owner"} }, ErrUnsafeProposalMaterial},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proposal := pm038Proposal()
			before := proposal
			tt.edit(&proposal)
			if err := proposal.Validate(); !errors.Is(err, tt.want) {
				t.Fatalf("Validate() = %v, want %v", err, tt.want)
			}
			if before.SchemaVersion != proposal.SchemaVersion || strings.TrimSpace(proposal.Rationale) == "" {
				t.Fatal("validation unexpectedly changed proposal material")
			}
		})
	}
}
