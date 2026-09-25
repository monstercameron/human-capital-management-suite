package projectstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
)

func TestTodo_PM_008_Integration_ReviseArchiveRestore(t *testing.T) {
	s, _ := projectFixture(t)
	ctx := context.Background()
	project := ProjectRecord{ID: "task-lifecycle", TenantID: "tenant-a", OwnerID: "owner", Name: "Tasks", Timezone: "UTC", Lifecycle: "ACTIVE", Revision: 1}
	if err := s.CreateProject(ctx, project, "owner", "HUMAN", "create-project"); err != nil {
		t.Fatal(err)
	}
	config := projectworkflow.Config{
		TaskTypes: []projectworkflow.TaskType{{ID: "task_default", Name: "Task", InitialStatus: "todo"}},
		Statuses: []projectworkflow.Status{
			{ID: "todo", Name: "To do", Category: projectworkflow.CategoryNotStarted, AllowedNextStatusIDs: []string{"doing"}},
			{ID: "doing", Name: "In progress", Category: projectworkflow.CategoryActive, AllowedNextStatusIDs: []string{"done"}},
			{ID: "done", Name: "Done", Category: projectworkflow.CategoryDone},
		},
		Transitions: []projectworkflow.Transition{{From: "todo", To: "doing"}, {From: "doing", To: "done"}},
		Columns:     []projectworkflow.Column{{ID: "todo", Name: "To do", StatusIDs: []string{"todo"}}, {ID: "doing", Name: "In progress", StatusIDs: []string{"doing"}}, {ID: "done", Name: "Done", StatusIDs: []string{"done"}}},
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, project.TenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO project_workflow_version(tenant_id,project_id,version,source_revision,config_json,digest,publisher_id) VALUES($1,$2,1,1,$3::jsonb,repeat('a',64),'owner')`, project.TenantID, project.ID, configJSON); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_workflow_current(tenant_id,project_id,version) VALUES($1,$2,1)`, project.TenantID, project.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	task := TaskRecord{ID: "task-1", TenantID: project.TenantID, ProjectID: project.ID, Title: "Original", StatusID: "todo", TypeID: "task_default", Priority: "NORMAL", Revision: 1}
	if err := s.CreateTaskWithConfig(ctx, task, 1, "owner", "HUMAN", "create-task"); err != nil {
		t.Fatal(err)
	}
	newTitle, due := "Revised", "2026-10-02"
	patch := TaskPatch{Title: &newTitle, DueDate: &due}
	revised, err := s.PatchTask(ctx, project.TenantID, project.ID, task.ID, 1, 1, patch, "owner", "HUMAN", "revise-1")
	if err != nil || revised.Revision != 2 || revised.Title != newTitle || revised.DueDate == nil || revised.DueDate.Format("2006-01-02") != due {
		t.Fatalf("revise=%+v err=%v", revised, err)
	}
	replay, err := s.PatchTask(ctx, project.TenantID, project.ID, task.ID, 1, 1, patch, "owner", "HUMAN", "revise-1")
	if err != nil || replay.Revision != 2 {
		t.Fatalf("revise replay=%+v err=%v", replay, err)
	}
	changed := "Different"
	if _, err := s.PatchTask(ctx, project.TenantID, project.ID, task.ID, 1, 1, TaskPatch{Title: &changed}, "owner", "HUMAN", "revise-1"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed replay=%v", err)
	}
	if _, err := s.PatchTask(ctx, project.TenantID, project.ID, task.ID, 1, 1, TaskPatch{Title: &changed}, "owner", "HUMAN", "revise-stale"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale revision=%v", err)
	}
	archived, err := s.SetTaskArchived(ctx, project.TenantID, project.ID, task.ID, 2, 1, true, "owner", "HUMAN", "archive-1")
	if err != nil || archived.Revision != 3 || !archived.Archived {
		t.Fatalf("archive=%+v err=%v", archived, err)
	}
	if _, err := s.PatchTask(ctx, project.TenantID, project.ID, task.ID, 3, 1, patch, "owner", "HUMAN", "revise-archived"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("archived edit=%v", err)
	}
	if got, err := s.GetTask(ctx, project.TenantID, project.ID, task.ID); err != nil || !got.Archived || got.Title != newTitle {
		t.Fatalf("archive lost task: %+v err=%v", got, err)
	}
	restored, err := s.SetTaskArchived(ctx, project.TenantID, project.ID, task.ID, 3, 1, false, "owner", "HUMAN", "restore-1")
	if err != nil || restored.Revision != 4 || restored.Archived {
		t.Fatalf("restore=%+v err=%v", restored, err)
	}
	if replay, err := s.SetTaskArchived(ctx, project.TenantID, project.ID, task.ID, 3, 1, false, "owner", "HUMAN", "restore-1"); err != nil || replay.Revision != 4 {
		t.Fatalf("restore replay=%+v err=%v", replay, err)
	}
	if _, err := s.SetTaskArchived(ctx, "tenant-b", project.ID, task.ID, 4, 1, true, "owner", "HUMAN", "cross-tenant"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant archive=%v", err)
	}
	var activity, outbox int
	if err := s.RunTenantTx(ctx, project.TenantID, func(tx dbport.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_activity WHERE tenant_id=$1 AND project_id=$2 AND aggregate_id=$3`, project.TenantID, project.ID, task.ID).Scan(&activity); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM project_outbox WHERE tenant_id=$1 AND project_id=$2 AND payload->>'aggregateId'=$3`, project.TenantID, project.ID, task.ID).Scan(&outbox)
	}); err != nil {
		t.Fatal(err)
	}
	if activity != 4 || outbox != 4 {
		t.Fatalf("activity=%d outbox=%d want create/revise/archive/restore", activity, outbox)
	}
	if _, err := s.SetTaskArchived(ctx, project.TenantID, project.ID, task.ID, 4, 1, true, "owner", "HUMAN", "archive-before-workflow-change"); err != nil {
		t.Fatal(err)
	}
	newConfig := projectworkflow.Config{
		TaskTypes:   []projectworkflow.TaskType{{ID: "task_default", Name: "Task", InitialStatus: "doing"}},
		Statuses:    []projectworkflow.Status{{ID: "doing", Name: "In progress", Category: projectworkflow.CategoryActive, AllowedNextStatusIDs: []string{"done"}}, {ID: "done", Name: "Done", Category: projectworkflow.CategoryDone}},
		Transitions: []projectworkflow.Transition{{From: "doing", To: "done"}},
		Columns:     []projectworkflow.Column{{ID: "doing", Name: "In progress", StatusIDs: []string{"doing"}}, {ID: "done", Name: "Done", StatusIDs: []string{"done"}}},
	}
	newJSON, err := json.Marshal(newConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, project.TenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO project_workflow_version(tenant_id,project_id,version,source_revision,config_json,digest,publisher_id) VALUES($1,$2,2,2,$3::jsonb,repeat('b',64),'owner')`, project.TenantID, project.ID, newJSON); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE project_workflow_current SET version=2 WHERE tenant_id=$1 AND project_id=$2`, project.TenantID, project.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetTaskArchived(ctx, project.TenantID, project.ID, task.ID, 5, 2, false, "owner", "HUMAN", "restore-obsolete-status"); !errors.Is(err, ErrRestoreWorkflowMismatch) {
		t.Fatalf("obsolete archived status restored: %v", err)
	}
	if got, err := s.GetTask(ctx, project.TenantID, project.ID, task.ID); err != nil || !got.Archived || got.Revision != 5 {
		t.Fatalf("failed restore changed task: %+v err=%v", got, err)
	}
}

func TestTodo_PM_024(t *testing.T) {
	valid := "line one\nline two\t✓"
	if !validateTaskText("Launch plan", valid) {
		t.Fatal("bounded UTF-8 text with ordinary whitespace was rejected")
	}
	invalidUTF8 := string([]byte{0xff})
	cases := []struct {
		name, title, description string
	}{
		{name: "title too long", title: strings.Repeat("x", maxTaskTitleRunes+1)},
		{name: "description too long", title: "Valid", description: strings.Repeat("x", maxTaskDescriptionRunes+1)},
		{name: "invalid title UTF-8", title: invalidUTF8},
		{name: "invalid description UTF-8", title: "Valid", description: invalidUTF8},
		{name: "title control", title: "bad\x00title"},
		{name: "description control", title: "Valid", description: "bad\x00text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if validateTaskText(tc.title, tc.description) {
				t.Fatalf("accepted title/description lengths %d/%d", utf8.RuneCountInString(tc.title), utf8.RuneCountInString(tc.description))
			}
		})
	}
}

func TestTodo_PM_008_Race_ConcurrentTaskPatch(t *testing.T) {
	s, _ := projectFixture(t)
	ctx := context.Background()
	project := ProjectRecord{ID: "race-project", TenantID: "tenant-race", OwnerID: "owner", Name: "Race", Timezone: "UTC", Lifecycle: "ACTIVE", Revision: 1}
	if err := s.CreateProject(ctx, project, "owner", "HUMAN", "create-race-project"); err != nil {
		t.Fatal(err)
	}
	seedCurrentWorkflow(t, s, project.TenantID, project.ID, "owner", 1)
	task := TaskRecord{ID: "race-task", TenantID: project.TenantID, ProjectID: project.ID, Title: "Original", StatusID: "todo", TypeID: "task_default", Priority: "NORMAL", Revision: 1}
	if err := s.CreateTaskWithConfig(ctx, task, 1, "owner", "HUMAN", "create-race-task"); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, title := range []string{"First", "Second"} {
		wg.Add(1)
		go func(title string) {
			defer wg.Done()
			<-start
			_, err := s.PatchTask(ctx, project.TenantID, project.ID, task.ID, 1, 1, TaskPatch{Title: &title}, "owner", "HUMAN", "patch-"+title)
			results <- err
		}(title)
	}
	close(start)
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrRevisionConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent patch error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent patches: successes=%d conflicts=%d", successes, conflicts)
	}
	got, err := s.GetTask(ctx, project.TenantID, project.ID, task.ID)
	if err != nil || got.Revision != 2 || (got.Title != "First" && got.Title != "Second") {
		t.Fatalf("concurrent result=%+v err=%v", got, err)
	}
}
