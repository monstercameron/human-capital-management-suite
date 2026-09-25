package projectstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	projectdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/project"
)

var (
	ErrNotFound         = errors.New("project record not found")
	ErrRevisionConflict = errors.New("project revision conflict")
	ErrInvalidRecord    = errors.New("invalid project record")
)

const (
	maxTaskTitleRunes       = 200
	maxTaskDescriptionRunes = 20_000
)

// validateTaskText bounds persisted task text and rejects malformed UTF-8 or
// control characters. Descriptions may retain ordinary whitespace so
// multiline text remains usable; this does not rewrite text already stored.
func validateTaskText(title, description string) bool {
	return validTaskText(title, maxTaskTitleRunes, false) && validTaskText(description, maxTaskDescriptionRunes, true)
}

func validTaskText(value string, maxRunes int, allowWhitespaceControls bool) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxRunes {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) && !(allowWhitespaceControls && (r == '\n' || r == '\r' || r == '\t')) {
			return false
		}
	}
	return true
}

// ProjectRecord is a persistence projection of the project domain value.
type ProjectRecord struct {
	ID, TenantID, OwnerID, Name, Timezone, Lifecycle string
	Revision                                         int64
	CreatedAt, UpdatedAt                             time.Time
}

// TaskRecord is a persistence projection of an ordinary project task.
type TaskRecord struct {
	ID, TenantID, ProjectID                                    string
	Title, Description, StatusID, TypeID, Priority, AssigneeID string
	Fields                                                     json.RawMessage
	DueDate                                                    *time.Time
	Revision                                                   int64
	Archived                                                   bool
	CreatedAt, UpdatedAt                                       time.Time
	// CreatedBy is the reporter; Labels, StartDate and StoryPoints are the
	// planning fields a project manager maintains.
	CreatedBy   string
	Labels      []string
	StartDate   *time.Time
	StoryPoints int32
}

func (s *Store) CreateProject(ctx context.Context, p ProjectRecord, actorID, origin, idempotencyKey string) error {
	return s.CreateProjectWith(ctx, p, actorID, origin, idempotencyKey, nil)
}

// CreateProjectWith runs the supplied starter configuration/view initializer
// in the same tenant transaction as the project, activity, outbox and
// idempotency records. Initializer failure rolls back the entire creation.
func (s *Store) CreateProjectWith(ctx context.Context, p ProjectRecord, actorID, origin, idempotencyKey string, initialize func(context.Context, dbport.Tx) error) error {
	if p.ID == "" || p.TenantID == "" || p.OwnerID == "" || p.Name == "" || p.Timezone == "" || actorID == "" || idempotencyKey == "" || !validOrigin(origin) {
		return ErrInvalidRecord
	}
	fingerprint, err := mutationFingerprint(struct {
		Project       ProjectRecord
		Actor, Origin string
	}{p, actorID, origin})
	if err != nil {
		return err
	}
	return s.RunTenantTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		replay, _, err := claimIdempotency(ctx, tx, p.TenantID, actorID, "project.create", idempotencyKey, fingerprint)
		if err != nil || replay {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO project(tenant_id,id,owner_id,name,project_timezone,lifecycle,revision) VALUES($1,$2,$3,$4,$5,COALESCE(NULLIF($6,''),'ACTIVE'),COALESCE(NULLIF($7,0),1))`, p.TenantID, p.ID, p.OwnerID, p.Name, p.Timezone, p.Lifecycle, p.Revision)
		if err != nil {
			return err
		}
		if err = appendProjectEvent(ctx, tx, p.TenantID, p.ID, p.ID, actorID, origin, "project.created", 0, valueRevision(p.Revision), 0, map[string]any{"name": p.Name, "timezone": p.Timezone}); err != nil {
			return err
		}
		if initialize != nil {
			if err = initialize(ctx, tx); err != nil {
				return err
			}
		}
		return finishIdempotency(ctx, tx, p.TenantID, actorID, "project.create", idempotencyKey, map[string]bool{"created": true})
	})
}

func (s *Store) GetProject(ctx context.Context, tenantID, projectID string) (ProjectRecord, error) {
	var out ProjectRecord
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT id,tenant_id,owner_id,name,project_timezone,lifecycle,revision,created_at,updated_at FROM project WHERE tenant_id=$1 AND id=$2`, tenantID, projectID).Scan(&out.ID, &out.TenantID, &out.OwnerID, &out.Name, &out.Timezone, &out.Lifecycle, &out.Revision, &out.CreatedAt, &out.UpdatedAt)
	})
	if err != nil {
		return ProjectRecord{}, mapNotFound(err)
	}
	return out, nil
}

// ListProjects returns an ID-ordered bounded page. afterID is exclusive.
func (s *Store) ListProjects(ctx context.Context, tenantID, afterID string, limit int32) ([]ProjectRecord, error) {
	if err := validLimit(limit); err != nil {
		return nil, err
	}
	out := make([]ProjectRecord, 0, limit)
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,owner_id,name,project_timezone,lifecycle,revision,created_at,updated_at FROM project WHERE tenant_id=$1 AND id>$2 ORDER BY id LIMIT $3`, tenantID, afterID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p ProjectRecord
			if err := rows.Scan(&p.ID, &p.TenantID, &p.OwnerID, &p.Name, &p.Timezone, &p.Lifecycle, &p.Revision, &p.CreatedAt, &p.UpdatedAt); err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Store) CreateTask(ctx context.Context, task TaskRecord, actorID, origin, idempotencyKey string) error {
	return s.createTask(ctx, task, 0, actorID, origin, idempotencyKey, "", nil)
}

// CreateTaskWithConfig pins task creation to the active workflow version in
// the same transaction that inserts the task and its outbox event.
func (s *Store) CreateTaskWithConfig(ctx context.Context, task TaskRecord, expectedConfigVersion int64, actorID, origin, idempotencyKey string) error {
	return s.createTask(ctx, task, expectedConfigVersion, actorID, origin, idempotencyKey, "", nil)
}

// CreateTaskWith pins creation to the published workflow version and invokes
// an optional typed-link initializer in the same transaction as the task,
// activity, outbox, and idempotency evidence.
func (s *Store) CreateTaskWith(ctx context.Context, task TaskRecord, expectedConfigVersion int64, actorID, origin, idempotencyKey, initializerFingerprint string, initialize func(context.Context, dbport.Tx) error) error {
	if expectedConfigVersion <= 0 {
		return ErrInvalidRecord
	}
	return s.createTask(ctx, task, expectedConfigVersion, actorID, origin, idempotencyKey, initializerFingerprint, initialize)
}

func (s *Store) createTask(ctx context.Context, task TaskRecord, expectedConfigVersion int64, actorID, origin, idempotencyKey, initializerFingerprint string, initialize func(context.Context, dbport.Tx) error) error {
	if task.ID == "" || task.TenantID == "" || task.ProjectID == "" || strings.TrimSpace(task.Title) == "" || !validateTaskText(task.Title, task.Description) || task.StatusID == "" || actorID == "" || idempotencyKey == "" || !validOrigin(origin) {
		return ErrInvalidRecord
	}
	if expectedConfigVersion < 0 {
		return ErrInvalidRecord
	}
	if (initialize == nil) != (initializerFingerprint == "") {
		return ErrInvalidRecord
	}
	if task.TypeID == "" {
		task.TypeID = "task_default"
	}
	if task.Priority == "" {
		task.Priority = "NORMAL"
	}
	if len(task.Fields) == 0 {
		task.Fields = json.RawMessage(`{}`)
	}
	fingerprint, err := mutationFingerprint(struct {
		Task                   TaskRecord
		ConfigVersion          int64
		InitializerFingerprint string
		Actor, Origin          string
	}{task, expectedConfigVersion, initializerFingerprint, actorID, origin})
	if err != nil {
		return err
	}
	return s.RunTenantTx(ctx, task.TenantID, func(tx dbport.Tx) error {
		replay, _, err := claimIdempotency(ctx, tx, task.TenantID, actorID, "task.create", idempotencyKey, fingerprint)
		if err != nil {
			return err
		}
		if replay {
			// Initializers such as source-link insertion are required to verify
			// their existing stable-ID row on replay; AddTx treats exact values
			// as a no-op and changed values as an idempotency conflict.
			if initialize != nil {
				return initialize(ctx, tx)
			}
			return nil
		}
		var lifecycle string
		if err := tx.QueryRow(ctx, `SELECT lifecycle FROM project WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, task.TenantID, task.ProjectID).Scan(&lifecycle); err != nil {
			return mapNotFound(err)
		}
		if lifecycle != "ACTIVE" {
			return ErrInvalidRecord
		}
		if expectedConfigVersion > 0 {
			var currentVersion int64
			if err := tx.QueryRow(ctx, `SELECT version FROM project_workflow_current WHERE tenant_id=$1 AND project_id=$2`, task.TenantID, task.ProjectID).Scan(&currentVersion); err != nil {
				return mapNotFound(err)
			}
			if currentVersion != expectedConfigVersion {
				return ErrRevisionConflict
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO project_task(tenant_id,id,project_id,title,description,status_id,type_id,priority,fields_json,assignee_id,due_date,revision,archived,created_by,labels,start_date,story_points) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,COALESCE(NULLIF($12,0),1),$13,$14,COALESCE($15::text[],'{}'),$16,$17)`, task.TenantID, task.ID, task.ProjectID, task.Title, task.Description, task.StatusID, task.TypeID, task.Priority, task.Fields, task.AssigneeID, task.DueDate, task.Revision, task.Archived, actorID, task.Labels, task.StartDate, task.StoryPoints)
		if err != nil {
			return err
		}
		if err = appendProjectEvent(ctx, tx, task.TenantID, task.ProjectID, task.ID, actorID, origin, "task.created", 0, valueRevision(task.Revision), expectedConfigVersion, taskEventValue(task)); err != nil {
			return err
		}
		if initialize != nil {
			if err := initialize(ctx, tx); err != nil {
				return err
			}
		}
		return finishIdempotency(ctx, tx, task.TenantID, actorID, "task.create", idempotencyKey, map[string]bool{"created": true})
	})
}

func (s *Store) GetTask(ctx context.Context, tenantID, projectID, taskID string) (TaskRecord, error) {
	var out TaskRecord
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return scanTask(tx.QueryRow(ctx, taskSelect+` WHERE tenant_id=$1 AND project_id=$2 AND id=$3`, tenantID, projectID, taskID), &out)
	})
	if err != nil {
		return TaskRecord{}, mapNotFound(err)
	}
	return out, nil
}

// MoveTask performs the application-validated status transition and persists
// typed edits, revision, activity, outbox, and idempotency result atomically.
func (s *Store) MoveTask(ctx context.Context, tenantID, projectID, taskID, targetStatus string, expectedRevision, configVersion int64, fieldEdits []projectdomain.TaskFieldEdit, actorID, origin, idempotencyKey string) (TaskRecord, error) {
	if tenantID == "" || projectID == "" || taskID == "" || targetStatus == "" || expectedRevision <= 0 || configVersion <= 0 || actorID == "" || idempotencyKey == "" || !validOrigin(origin) {
		return TaskRecord{}, ErrInvalidRecord
	}
	fingerprint, err := mutationFingerprint(struct {
		TenantID, ProjectID, TaskID, TargetStatus string
		ExpectedRevision, ConfigVersion           int64
		Edits                                     []projectdomain.TaskFieldEdit
		Actor, Origin                             string
	}{tenantID, projectID, taskID, targetStatus, expectedRevision, configVersion, fieldEdits, actorID, origin})
	if err != nil {
		return TaskRecord{}, err
	}
	var out TaskRecord
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		replay, result, err := claimIdempotency(ctx, tx, tenantID, actorID, "task.move", idempotencyKey, fingerprint)
		if err != nil {
			return err
		}
		if replay {
			return json.Unmarshal(result, &out)
		}
		var lifecycle string
		if err := tx.QueryRow(ctx, `SELECT lifecycle FROM project WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, projectID).Scan(&lifecycle); err != nil {
			return mapNotFound(err)
		}
		if lifecycle != "ACTIVE" {
			return ErrInvalidRecord
		}
		var currentConfigVersion int64
		if err := tx.QueryRow(ctx, `SELECT version FROM project_workflow_current WHERE tenant_id=$1 AND project_id=$2`, tenantID, projectID).Scan(&currentConfigVersion); err != nil {
			return mapNotFound(err)
		}
		if currentConfigVersion != configVersion {
			return ErrRevisionConflict
		}
		var prior TaskRecord
		if err := scanTask(tx.QueryRow(ctx, taskSelect+` WHERE tenant_id=$1 AND project_id=$2 AND id=$3 FOR UPDATE`, tenantID, projectID, taskID), &prior); err != nil {
			return mapNotFound(err)
		}
		if prior.Revision != expectedRevision || prior.Archived {
			return ErrRevisionConflict
		}
		fields, err := applyFieldEdits(prior.Fields, fieldEdits)
		if err != nil {
			return err
		}
		if err := scanTask(tx.QueryRow(ctx, `UPDATE project_task SET status_id=$1,fields_json=$2::jsonb,revision=revision+1,updated_at=now() WHERE tenant_id=$3 AND project_id=$4 AND id=$5 AND revision=$6 RETURNING `+taskColumns, targetStatus, fields, tenantID, projectID, taskID, expectedRevision), &out); err != nil {
			return err
		}
		out.Fields = fields
		if err := appendProjectEvent(ctx, tx, tenantID, projectID, taskID, actorID, origin, "task.moved", expectedRevision, out.Revision, configVersion, map[string]any{"from": prior.StatusID, "to": targetStatus, "fieldEdits": fieldEdits}); err != nil {
			return err
		}
		return finishIdempotency(ctx, tx, tenantID, actorID, "task.move", idempotencyKey, out)
	})
	if err != nil {
		return TaskRecord{}, err
	}
	return out, nil
}

func (s *Store) ListTasks(ctx context.Context, tenantID, projectID, afterID string, limit int32) ([]TaskRecord, error) {
	if err := validLimit(limit); err != nil {
		return nil, err
	}
	out := make([]TaskRecord, 0, limit)
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, taskSelect+` WHERE tenant_id=$1 AND project_id=$2 AND id>$3 ORDER BY id LIMIT $4`, tenantID, projectID, afterID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var task TaskRecord
			if err := scanTask(rows, &task); err != nil {
				return err
			}
			out = append(out, task)
		}
		return rows.Err()
	})
	return out, err
}

// taskColumns is the one column list every task read and RETURNING clause
// scans, in scanTask's order.
const taskColumns = `id,tenant_id,project_id,title,description,status_id,type_id,priority,assignee_id,due_date,revision,archived,created_at,updated_at,fields_json,created_by,labels,start_date,story_points`

const taskSelect = `SELECT ` + taskColumns + ` FROM project_task`

func appendProjectEvent(ctx context.Context, tx dbport.Tx, tenantID, projectID, aggregateID, actorID, origin, eventType string, priorRevision, newRevision, configVersion int64, value any) error {
	var taskSequence int64
	if strings.HasPrefix(eventType, "task.") {
		if err := tx.QueryRow(ctx, `UPDATE project_task SET activity_sequence=GREATEST(activity_sequence,
 (SELECT count(*) FROM project_activity WHERE tenant_id=$1 AND project_id=$2 AND aggregate_id=$3 AND event_type LIKE 'task.%' AND task_sequence=0) +
 (SELECT count(*) FROM project_task_comment_activity WHERE tenant_id=$1 AND project_id=$2 AND task_id=$3 AND task_sequence=0)) + 1
 WHERE tenant_id=$1 AND project_id=$2 AND id=$3 RETURNING activity_sequence`, tenantID, projectID, aggregateID).Scan(&taskSequence); err != nil {
			return err
		}
	}
	event, err := AppendOutboxEventTx(ctx, tx, AppendOutboxEvent{TenantID: tenantID, ProjectID: projectID, AggregateID: aggregateID, EventType: eventType, SourceRevision: newRevision, Classification: "INTERNAL", Value: value})
	if err != nil {
		return err
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO project_activity(tenant_id,id,project_id,aggregate_id,actor_id,origin,event_type,prior_revision,new_revision,config_version,classification,payload,task_sequence) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'INTERNAL',$11,$12)`, tenantID, event.EventID, projectID, aggregateID, actorID, origin, eventType, priorRevision, newRevision, configVersion, payload, taskSequence); err != nil {
		return err
	}
	return nil
}

func applyFieldEdits(initial json.RawMessage, edits []projectdomain.TaskFieldEdit) (json.RawMessage, error) {
	fields := map[string]projectdomain.TaskFieldEdit{}
	if len(initial) > 0 {
		if err := json.Unmarshal(initial, &fields); err != nil {
			return nil, err
		}
	}
	for _, edit := range edits {
		if strings.TrimSpace(edit.FieldID) == "" || strings.TrimSpace(edit.Type) == "" {
			return nil, ErrInvalidRecord
		}
		fields[edit.FieldID] = edit
	}
	return json.Marshal(fields)
}

func taskEventValue(t TaskRecord) map[string]any {
	return map[string]any{"taskId": t.ID, "title": t.Title, "statusId": t.StatusID, "typeId": t.TypeID, "priority": t.Priority}
}
func validOrigin(origin string) bool {
	return origin == "HUMAN" || origin == "APP" || origin == "AGENT"
}
func valueRevision(revision int64) int64 {
	if revision <= 0 {
		return 1
	}
	return revision
}
func validLimit(limit int32) error {
	if limit < 1 || limit > 100 {
		return ErrInvalidRecord
	}
	return nil
}
func mapNotFound(err error) error {
	if errors.Is(err, dbport.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

type rowScanner interface{ Scan(...any) error }

func scanTask(row rowScanner, out *TaskRecord) error {
	return row.Scan(&out.ID, &out.TenantID, &out.ProjectID, &out.Title, &out.Description, &out.StatusID, &out.TypeID, &out.Priority, &out.AssigneeID, &out.DueDate, &out.Revision, &out.Archived, &out.CreatedAt, &out.UpdatedAt, &out.Fields, &out.CreatedBy, &out.Labels, &out.StartDate, &out.StoryPoints)
}
