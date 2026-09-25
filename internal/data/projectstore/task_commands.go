package projectstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
)

var ErrRestoreWorkflowMismatch = errors.New("archived task requires workflow migration before restore")

// TaskPatch is sparse: nil preserves a value; an empty optional string clears it.
type TaskPatch struct {
	Title, Description, AssigneeID, DueDate, Priority *string
	// StartDate is a civil date like DueDate; an empty value clears it.
	StartDate   *string
	StoryPoints *int32
	// Labels replaces the whole label set; an empty slice clears it.
	Labels *[]string
}

func (p TaskPatch) empty() bool {
	return p.Title == nil && p.Description == nil && p.AssigneeID == nil && p.DueDate == nil && p.Priority == nil && p.StartDate == nil && p.StoryPoints == nil && p.Labels == nil
}

const (
	maxTaskLabels     = 20
	maxTaskLabelRunes = 40
	maxStoryPoints    = 1000
)

// validTaskLabels accepts at most maxTaskLabels distinct, trimmed, non-empty
// labels of at most maxTaskLabelRunes printable runes each.
func validTaskLabels(labels []string) bool {
	if len(labels) > maxTaskLabels {
		return false
	}
	seen := make(map[string]bool, len(labels))
	for _, label := range labels {
		if label == "" || label != strings.TrimSpace(label) || seen[strings.ToLower(label)] || !validTaskText(label, maxTaskLabelRunes, false) {
			return false
		}
		seen[strings.ToLower(label)] = true
	}
	return true
}

func parseCivilDate(value string) (*time.Time, bool) {
	if value == "" {
		return nil, true
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return nil, false
	}
	return &parsed, true
}

// PatchTask commits a project-owned task revision, activity, outbox, and
// idempotency receipt under the same project and workflow version fences used
// by task creation and moves.
func (s *Store) PatchTask(ctx context.Context, tenantID, projectID, taskID string, expectedRevision, configVersion int64, patch TaskPatch, actorID, origin, key string) (TaskRecord, error) {
	if tenantID == "" || projectID == "" || taskID == "" || expectedRevision <= 0 || configVersion <= 0 || actorID == "" || key == "" || !validOrigin(origin) || patch.empty() {
		return TaskRecord{}, ErrInvalidRecord
	}
	if patch.Title != nil && strings.TrimSpace(*patch.Title) == "" {
		return TaskRecord{}, ErrInvalidRecord
	}
	if (patch.Title != nil && !validTaskText(*patch.Title, maxTaskTitleRunes, false)) || (patch.Description != nil && !validTaskText(*patch.Description, maxTaskDescriptionRunes, true)) {
		return TaskRecord{}, ErrInvalidRecord
	}
	if patch.Priority != nil && *patch.Priority != "LOW" && *patch.Priority != "NORMAL" && *patch.Priority != "HIGH" && *patch.Priority != "URGENT" {
		return TaskRecord{}, ErrInvalidRecord
	}
	var dueDate *time.Time
	if patch.DueDate != nil && *patch.DueDate != "" {
		parsed, err := time.Parse("2006-01-02", *patch.DueDate)
		if err != nil || parsed.Format("2006-01-02") != *patch.DueDate {
			return TaskRecord{}, ErrInvalidRecord
		}
		dueDate = &parsed
	}
	var startDate *time.Time
	if patch.StartDate != nil {
		parsed, ok := parseCivilDate(*patch.StartDate)
		if !ok {
			return TaskRecord{}, ErrInvalidRecord
		}
		startDate = parsed
	}
	if patch.StoryPoints != nil && (*patch.StoryPoints < 0 || *patch.StoryPoints > maxStoryPoints) {
		return TaskRecord{}, ErrInvalidRecord
	}
	if patch.Labels != nil && !validTaskLabels(*patch.Labels) {
		return TaskRecord{}, ErrInvalidRecord
	}
	fingerprint, err := mutationFingerprint(struct {
		TenantID, ProjectID, TaskID     string
		ExpectedRevision, ConfigVersion int64
		Patch                           TaskPatch
		Actor, Origin                   string
	}{tenantID, projectID, taskID, expectedRevision, configVersion, patch, actorID, origin})
	if err != nil {
		return TaskRecord{}, err
	}
	var out TaskRecord
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		replay, result, err := claimIdempotency(ctx, tx, tenantID, actorID, "task.patch", key, fingerprint)
		if err != nil {
			return err
		}
		if replay {
			return json.Unmarshal(result, &out)
		}
		if err := lockActiveProjectWorkflow(ctx, tx, tenantID, projectID, configVersion); err != nil {
			return err
		}
		var prior TaskRecord
		if err := scanTask(tx.QueryRow(ctx, taskSelect+` WHERE tenant_id=$1 AND project_id=$2 AND id=$3 FOR UPDATE`, tenantID, projectID, taskID), &prior); err != nil {
			return mapNotFound(err)
		}
		if prior.Revision != expectedRevision || prior.Archived {
			return ErrRevisionConflict
		}
		updated := prior
		if patch.Title != nil {
			updated.Title = *patch.Title
		}
		if patch.Description != nil {
			updated.Description = *patch.Description
		}
		if patch.AssigneeID != nil {
			updated.AssigneeID = *patch.AssigneeID
		}
		if patch.Priority != nil {
			updated.Priority = *patch.Priority
		}
		if patch.DueDate != nil {
			updated.DueDate = dueDate
		}
		if patch.StartDate != nil {
			updated.StartDate = startDate
		}
		if patch.StoryPoints != nil {
			updated.StoryPoints = *patch.StoryPoints
		}
		if patch.Labels != nil {
			updated.Labels = append([]string{}, (*patch.Labels)...)
		}
		if updated.Labels == nil {
			updated.Labels = []string{}
		}
		if err := scanTask(tx.QueryRow(ctx, `UPDATE project_task SET title=$1,description=$2,assignee_id=$3,due_date=$4,priority=$5,start_date=$10,story_points=$11,labels=$12::text[],revision=revision+1,updated_at=now() WHERE tenant_id=$6 AND project_id=$7 AND id=$8 AND revision=$9 RETURNING `+taskColumns, updated.Title, updated.Description, updated.AssigneeID, updated.DueDate, updated.Priority, tenantID, projectID, taskID, expectedRevision, updated.StartDate, updated.StoryPoints, updated.Labels), &out); err != nil {
			return err
		}
		if err := appendProjectEvent(ctx, tx, tenantID, projectID, taskID, actorID, origin, "task.revised", expectedRevision, out.Revision, configVersion, taskEventValue(out)); err != nil {
			return err
		}
		return finishIdempotency(ctx, tx, tenantID, actorID, "task.patch", key, out)
	})
	if err != nil {
		return TaskRecord{}, err
	}
	return out, nil
}

// SetTaskArchived preserves the task row and its full event history. Each
// archive or restore is a distinct, revision-fenced command.
func (s *Store) SetTaskArchived(ctx context.Context, tenantID, projectID, taskID string, expectedRevision, configVersion int64, archived bool, actorID, origin, key string) (TaskRecord, error) {
	if tenantID == "" || projectID == "" || taskID == "" || expectedRevision <= 0 || configVersion <= 0 || actorID == "" || key == "" || !validOrigin(origin) {
		return TaskRecord{}, ErrInvalidRecord
	}
	operation, eventType := "task.restore", "task.restored"
	if archived {
		operation, eventType = "task.archive", "task.archived"
	}
	fingerprint, err := mutationFingerprint(struct {
		TenantID, ProjectID, TaskID     string
		ExpectedRevision, ConfigVersion int64
		Archived                        bool
		Actor, Origin                   string
	}{tenantID, projectID, taskID, expectedRevision, configVersion, archived, actorID, origin})
	if err != nil {
		return TaskRecord{}, err
	}
	var out TaskRecord
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		replay, result, err := claimIdempotency(ctx, tx, tenantID, actorID, operation, key, fingerprint)
		if err != nil {
			return err
		}
		if replay {
			return json.Unmarshal(result, &out)
		}
		if err := lockActiveProjectWorkflow(ctx, tx, tenantID, projectID, configVersion); err != nil {
			return err
		}
		var prior TaskRecord
		if err := scanTask(tx.QueryRow(ctx, taskSelect+` WHERE tenant_id=$1 AND project_id=$2 AND id=$3 FOR UPDATE`, tenantID, projectID, taskID), &prior); err != nil {
			return mapNotFound(err)
		}
		if prior.Revision != expectedRevision || prior.Archived == archived {
			return ErrRevisionConflict
		}
		if !archived {
			if err := validateRestorableTask(ctx, tx, prior, configVersion); err != nil {
				return err
			}
		}
		if err := scanTask(tx.QueryRow(ctx, `UPDATE project_task SET archived=$1,revision=revision+1,updated_at=now() WHERE tenant_id=$2 AND project_id=$3 AND id=$4 AND revision=$5 RETURNING `+taskColumns, archived, tenantID, projectID, taskID, expectedRevision), &out); err != nil {
			return err
		}
		if err := appendProjectEvent(ctx, tx, tenantID, projectID, taskID, actorID, origin, eventType, expectedRevision, out.Revision, configVersion, taskEventValue(out)); err != nil {
			return err
		}
		return finishIdempotency(ctx, tx, tenantID, actorID, operation, key, out)
	})
	if err != nil {
		return TaskRecord{}, err
	}
	return out, nil
}

// Archived tasks are deliberately excluded from bounded publish migrations.
// Revalidate before reactivation so an old status, type, or field definition
// cannot silently return to a board under a newer workflow.
func validateRestorableTask(ctx context.Context, tx dbport.Tx, task TaskRecord, configVersion int64) error {
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT config_json FROM project_workflow_version WHERE tenant_id=$1 AND project_id=$2 AND version=$3`, task.TenantID, task.ProjectID, configVersion).Scan(&raw); err != nil {
		return mapNotFound(err)
	}
	var config projectworkflow.Config
	if err := json.Unmarshal(raw, &config); err != nil {
		return err
	}
	fields, err := DecodeTaskFieldValues(task.Fields)
	if err != nil {
		return err
	}
	preview, err := projectworkflow.PreviewMigration(projectworkflow.MigrationRequest{Current: config, Pending: config, Tasks: []projectworkflow.TaskSnapshot{{ID: task.ID, TypeID: task.TypeID, StatusID: task.StatusID, Fields: fields, Revision: task.Revision}}})
	if err != nil || !preview.Safe || preview.AffectedTaskCount != 0 {
		return ErrRestoreWorkflowMismatch
	}
	return nil
}

func lockActiveProjectWorkflow(ctx context.Context, tx dbport.Tx, tenantID, projectID string, configVersion int64) error {
	var lifecycle string
	if err := tx.QueryRow(ctx, `SELECT lifecycle FROM project WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, projectID).Scan(&lifecycle); err != nil {
		return mapNotFound(err)
	}
	if lifecycle != "ACTIVE" {
		return ErrInvalidRecord
	}
	var current int64
	if err := tx.QueryRow(ctx, `SELECT version FROM project_workflow_current WHERE tenant_id=$1 AND project_id=$2`, tenantID, projectID).Scan(&current); err != nil {
		return mapNotFound(err)
	}
	if current != configVersion {
		return ErrRevisionConflict
	}
	return nil
}
