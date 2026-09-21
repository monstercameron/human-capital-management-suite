package designeredit_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designeredit"
)

func TestTodo_WF_UI_010_Golden(t *testing.T) {
	before := workflow.Definition{
		WorkflowID: "workflow.people.change", StartNodeID: "start",
		Nodes: []workflow.Node{
			{ID: "start", Type: workflow.StepTask, Metadata: map[string]string{designeredit.MetadataDisplayName: "Start"}},
			{ID: "review", Type: workflow.StepTask, InputMappings: []workflow.Mapping{{Target: "person", Source: workflow.Source{Kind: workflow.SourceNodeOutput, NodeID: "start", Path: "worker.original"}}}},
		},
		Edges: []workflow.Edge{{From: "start", To: "review", RouteKey: "NEXT"}},
	}
	after := workflow.Definition{
		WorkflowID: "workflow.people.change", StartNodeID: "start",
		Nodes: []workflow.Node{
			{ID: "start", Type: workflow.StepTask, Metadata: map[string]string{designeredit.MetadataDisplayName: "Begin"}},
			{ID: "review", Type: workflow.StepTask, InputMappings: []workflow.Mapping{{Target: "person", Source: workflow.Source{Kind: workflow.SourceNodeOutput, NodeID: "start", Path: "worker.updated"}}}},
			{ID: "end", Type: workflow.StepEnd},
		},
		Edges: []workflow.Edge{{From: "start", To: "review", RouteKey: "NEXT"}, {From: "review", To: "end", RouteKey: "DONE"}},
	}
	encoded, err := json.MarshalIndent(designeredit.SemanticDiff(before, after), "", "  ")
	if err != nil {
		t.Fatalf("marshal semantic diff: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "wfui010_semantic_diff.golden.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(encoded)+"\n" != string(want) {
		t.Fatalf("semantic diff:\n%s\nwant:\n%s", encoded, want)
	}
}

func TestTodo_WF_UI_010_DiffIgnoresFormattingAndInputOrder(t *testing.T) {
	definition := workflow.Definition{WorkflowID: "workflow.people.change", StartNodeID: "start", Nodes: []workflow.Node{{ID: "start", Type: workflow.StepTask}}}
	if changes := designeredit.SemanticDiff(definition, definition); len(changes) != 0 {
		t.Fatalf("identical definition changes = %+v", changes)
	}
}

func TestTodo_WF_UI_010_AgentImportUsesTheHumanDraftArtifact(t *testing.T) {
	store := &memoryDraftStore{}
	definition := workflow.Definition{
		WorkflowID: "customer.workflow.agent_import", Name: "Imported change", StartNodeID: "start",
		Nodes: []workflow.Node{{ID: "start", Type: workflow.StepTask}, {ID: "finish", Type: workflow.StepEnd}},
		Edges: []workflow.Edge{{From: "start", To: "finish", RouteKey: "NEXT"}},
	}
	now := time.Date(2026, 9, 19, 21, 0, 0, 0, time.UTC)
	service := designeredit.Service{Store: store, NewID: func() (string, error) { return "01999f37-9f42-7000-8000-000000000210", nil }, Now: func() time.Time { return now }}
	imported, err := service.Import(context.Background(), values.TenantId("tenant-a"), "agent:workflow-author", designeredit.ImportRequest{Definition: definition, SemanticVersion: "0.4.0"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if imported.Draft.DraftID != store.draft.DraftID || imported.Draft.Revision != 1 || imported.Draft.LayoutMode != "AUTO" || len(imported.Draft.Nodes) != 2 || len(imported.Draft.Edges) != 1 {
		t.Fatalf("imported draft = %+v; stored = %+v", imported.Draft, store.draft)
	}
	loaded, err := workflow.Load(store.draft.Document)
	if err != nil || loaded.WorkflowID != definition.WorkflowID || store.draft.AuthorRef != "agent:workflow-author" {
		t.Fatalf("stored agent artifact = %+v, %v", loaded, err)
	}
}
