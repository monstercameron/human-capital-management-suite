package projectservice

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/projectconfigstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
)

type migrationSnapshotFake struct {
	tasks []projectworkflow.TaskSnapshot
}

func (f migrationSnapshotFake) ActiveTaskSnapshots(context.Context, string, string) ([]projectworkflow.TaskSnapshot, error) {
	return f.tasks, nil
}

type migrationConfigFake struct {
	current projectworkflow.Config
	draft   projectworkflow.Config
	version int64
}

func (f migrationConfigFake) GetPublished(context.Context, string, string) (projectconfigstore.Published, error) {
	p, err := projectworkflow.Publish(f.current, 1)
	if err != nil {
		return projectconfigstore.Published{}, err
	}
	return projectconfigstore.Published{Config: p.Snapshot(), Version: 1}, nil
}
func (f migrationConfigFake) GetDraft(context.Context, string, string) (projectworkflow.Config, uint64, error) {
	return f.draft, uint64(f.version), nil
}

func TestStoreMigrationPreviewerReturnsImpactWithoutTaskValues(t *testing.T) {
	current := transitionConfig()
	pending := transitionConfig()
	pending.Statuses = []projectworkflow.Status{
		{ID: "todo", Name: "To do", Category: projectworkflow.CategoryNotStarted, AllowedNextStatusIDs: []string{"doing_v2"}},
		{ID: "doing_v2", Name: "Doing", Category: projectworkflow.CategoryActive},
	}
	pending.Transitions[0].To = "doing_v2"
	pending.Columns[1].ID = "doing_v2"
	pending.Columns[1].StatusIDs = []string{"doing_v2"}
	configs := migrationConfigFake{current: current, draft: pending, version: 3}
	snapshots := migrationSnapshotFake{tasks: []projectworkflow.TaskSnapshot{{
		ID: "task-1", TypeID: "task_default", StatusID: "doing",
		Fields: map[string]json.RawMessage{"secret": json.RawMessage(`"private value"`)},
	}}}
	preview, err := (StoreMigrationPreviewer{Projects: snapshots, Configs: configs}).Preview(context.Background(), "tenant", "project", pending, 3, projectworkflow.MigrationMappings{})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Digest == "" || preview.AffectedTaskCount != 1 || preview.Safe || len(preview.Errors) == 0 {
		t.Fatalf("unexpected migration preview: %+v", preview)
	}
	for _, diagnostic := range preview.Errors {
		if diagnostic.Message == `"private value"` || diagnostic.Path == "secret" {
			t.Fatalf("preview leaked a task field value: %+v", diagnostic)
		}
	}
}

func TestStoreMigrationPreviewerRejectsStaleDraftRevision(t *testing.T) {
	cfg := transitionConfig()
	_, err := (StoreMigrationPreviewer{
		Projects: migrationSnapshotFake{},
		Configs:  migrationConfigFake{current: cfg, draft: cfg, version: 4},
	}).Preview(context.Background(), "tenant", "project", cfg, 3, projectworkflow.MigrationMappings{})
	if err != projectconfigstore.ErrRevisionConflict {
		t.Fatalf("expected stale draft revision conflict, got %v", err)
	}
}

func TestStoreMigrationPreviewerBindsExplicitMappingsToPlanDigest(t *testing.T) {
	current := transitionConfig()
	pending := transitionConfig()
	pending.Statuses = []projectworkflow.Status{
		{ID: "todo", Name: "To do", Category: projectworkflow.CategoryNotStarted, AllowedNextStatusIDs: []string{"doing_v2"}},
		{ID: "doing_v2", Name: "Doing", Category: projectworkflow.CategoryActive},
	}
	pending.Transitions[0].To = "doing_v2"
	pending.Columns[1].ID = "doing_v2"
	pending.Columns[1].StatusIDs = []string{"doing_v2"}
	mappings := projectworkflow.MigrationMappings{Statuses: map[string]string{"doing": "doing_v2"}}
	configs := migrationConfigFake{current: current, draft: pending, version: 3}
	tasks := migrationSnapshotFake{tasks: []projectworkflow.TaskSnapshot{{ID: "task-1", TypeID: "task_default", StatusID: "doing", Revision: 8}}}
	preview, err := (StoreMigrationPreviewer{Projects: tasks, Configs: configs}).Preview(context.Background(), "tenant", "project", pending, 3, mappings)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Safe || preview.Digest == "" || preview.PlanDigest == "" || preview.AffectedTaskCount != 1 || len(preview.AffectedTaskIDs) != 1 || preview.AffectedTaskIDs[0] != "task-1" {
		t.Fatalf("mapped status migration preview=%+v", preview)
	}
	differentMapping := projectworkflow.MigrationMappings{Statuses: map[string]string{"doing": "todo"}}
	other, err := (StoreMigrationPreviewer{Projects: tasks, Configs: configs}).Preview(context.Background(), "tenant", "project", pending, 3, differentMapping)
	if err != nil {
		t.Fatal(err)
	}
	if other.PlanDigest == preview.PlanDigest {
		t.Fatal("changing the reviewed status mapping did not change the plan digest")
	}
}
