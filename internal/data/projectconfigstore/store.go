// Package projectconfigstore persists revisioned project workflow drafts and
// immutable published workflow versions in the project database.
package projectconfigstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	projectdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
)

var (
	ErrNotFound                    = errors.New("project workflow configuration not found")
	ErrRevisionConflict            = errors.New("project workflow revision conflict")
	ErrDigestConflict              = errors.New("reviewed workflow digest does not match draft")
	ErrInvalidRequest              = errors.New("invalid project workflow configuration request")
	ErrReviewRequired              = errors.New("independent review evidence required")
	ErrIdempotencyReuse            = errors.New("workflow idempotency key reused with different request")
	ErrMigrationRequired           = errors.New("workflow publication requires an atomic task migration")
	ErrMigrationPlanDigestConflict = errors.New("reviewed workflow migration plan does not match current impacts or mappings")
)

// Store shares the project store's schema, tenant scope, and transaction pool.
type Store struct{ projects *projectstore.Store }

func New(projects *projectstore.Store) *Store { return &Store{projects: projects} }

type Draft struct {
	TenantID, ProjectID, ActorID string
	Revision                     int64
	Config                       projectworkflow.Config
	Digest                       string
	Validation                   projectworkflow.ValidationErrors
}

type ReviewEvidence struct {
	Required           bool            `json:"required"`
	ReviewerID         string          `json:"reviewer_id,omitempty"`
	DecisionID         string          `json:"decision_id,omitempty"`
	ReviewedDigest     string          `json:"reviewed_digest,omitempty"`
	PolicyReference    string          `json:"policy_reference,omitempty"`
	ReviewedPlanDigest string          `json:"reviewed_plan_digest,omitempty"`
	Details            json.RawMessage `json:"details,omitempty"`
}

type Published struct {
	TenantID, ProjectID, PublisherID, ReviewerID string
	Version, SourceRevision                      int64
	Config                                       projectworkflow.Config
	Digest                                       string
}

// MigrationBlockedError carries the authoritative transaction-time impact
// preview returned when a publication cannot safely proceed without a task
// migration.
type MigrationBlockedError struct {
	Preview projectworkflow.MigrationPreview
	Cause   error
}

func (e *MigrationBlockedError) Error() string {
	if e.Cause == nil {
		return ErrMigrationRequired.Error()
	}
	return fmt.Sprintf("%s: %v", ErrMigrationRequired, e.Cause)
}
func (e *MigrationBlockedError) Unwrap() error { return e.Cause }
func (e *MigrationBlockedError) Is(target error) bool {
	return target == ErrMigrationRequired || errors.Is(e.Cause, target)
}

// Snapshot joins draft and active version for workflow ports that read a
// project's configuration as one value.
type Snapshot struct {
	Config           projectworkflow.Config
	DraftRevision    int64
	PublishedVersion int64
	PublishedConfig  projectworkflow.Config
	PublishedDigest  string
}

func (s *Store) GetDraft(ctx context.Context, tenantID, projectID string) (projectworkflow.Config, uint64, error) {
	draft, _, err := s.Get(ctx, tenantID, projectID)
	return draft.Config, uint64(draft.Revision), err
}

func (s *Store) GetPublished(ctx context.Context, tenantID, projectID string) (Published, error) {
	_, published, err := s.Get(ctx, tenantID, projectID)
	if err != nil {
		return Published{}, err
	}
	if published == nil {
		return Published{}, ErrNotFound
	}
	return *published, nil
}

// InitializeProject installs a usable starter board in one tenant transaction.
// It is idempotent so a caller can retry after project creation or a timeout.
func (s *Store) InitializeProject(ctx context.Context, tenantID, projectID, actorID string) error {
	if s == nil || s.projects == nil || tenantID == "" || projectID == "" || actorID == "" {
		return ErrInvalidRequest
	}
	return s.projects.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error { return InitializeProjectTx(ctx, tx, tenantID, projectID, actorID) })
}

// InitializeProjectTx installs the starter board inside a caller's existing
// tenant transaction, allowing project creation and its usable workflow to
// commit atomically.
func InitializeProjectTx(ctx context.Context, tx dbport.Tx, tenantID, projectID, actorID string) error {
	if tx == nil || tenantID == "" || projectID == "" || actorID == "" {
		return ErrInvalidRequest
	}
	c := StarterConfig()
	canonical, err := json.Marshal(c)
	if err != nil {
		return err
	}
	digest := digestJSON(canonical)
	var lifecycle string
	if err := tx.QueryRow(ctx, `SELECT lifecycle FROM project WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, projectID).Scan(&lifecycle); err != nil {
		return mapNoRows(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO project_workflow_draft(tenant_id,project_id,revision,config_json,config_digest,validation_json,actor_id) VALUES($1,$2,1,$3::jsonb,$4,'[]'::jsonb,$5) ON CONFLICT DO NOTHING`, tenantID, projectID, canonical, digest, actorID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO project_workflow_version(tenant_id,project_id,version,source_revision,config_json,digest,publisher_id) VALUES($1,$2,1,1,$3::jsonb,$4,$5) ON CONFLICT DO NOTHING`, tenantID, projectID, canonical, digest, actorID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO project_workflow_current(tenant_id,project_id,version) VALUES($1,$2,1) ON CONFLICT DO NOTHING`, tenantID, projectID)
	return err
}

func StarterConfig() projectworkflow.Config {
	return projectworkflow.Config{
		TaskTypes:   []projectworkflow.TaskType{{ID: "task_default", Name: "Task", InitialStatus: "todo"}},
		Statuses:    []projectworkflow.Status{{ID: "todo", Name: "To do", Category: projectworkflow.CategoryNotStarted, AllowedNextStatusIDs: []string{"doing"}}, {ID: "doing", Name: "In progress", Category: projectworkflow.CategoryActive, AllowedNextStatusIDs: []string{"done"}}, {ID: "done", Name: "Done", Category: projectworkflow.CategoryDone}},
		Transitions: []projectworkflow.Transition{{From: "todo", To: "doing"}, {From: "doing", To: "done"}},
		Columns:     []projectworkflow.Column{{ID: "todo", Name: "To do", StatusIDs: []string{"todo"}}, {ID: "doing", Name: "In progress", StatusIDs: []string{"doing"}}, {ID: "done", Name: "Done", StatusIDs: []string{"done"}}},
	}
}

// Get returns the mutable draft together with the immutable version currently
// selected for the project. A missing draft is ErrNotFound.
func (s *Store) Get(ctx context.Context, tenantID, projectID string) (Draft, *Published, error) {
	if s == nil || s.projects == nil || tenantID == "" || projectID == "" {
		return Draft{}, nil, ErrInvalidRequest
	}
	var draft Draft
	var current *Published
	err := s.projects.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var raw []byte
		var validation []byte
		err := tx.QueryRow(ctx, `SELECT tenant_id,project_id,revision,config_json,config_digest,validation_json,actor_id FROM project_workflow_draft WHERE tenant_id=$1 AND project_id=$2`, tenantID, projectID).Scan(&draft.TenantID, &draft.ProjectID, &draft.Revision, &raw, &draft.Digest, &validation, &draft.ActorID)
		if err != nil {
			return mapNoRows(err)
		}
		if err := json.Unmarshal(raw, &draft.Config); err != nil {
			return err
		}
		if err := json.Unmarshal(validation, &draft.Validation); err != nil {
			return err
		}
		var published Published
		var config []byte
		err = tx.QueryRow(ctx, `SELECT v.tenant_id,v.project_id,v.version,v.source_revision,v.config_json,v.digest,v.publisher_id,v.reviewer_id FROM project_workflow_current c JOIN project_workflow_version v USING (tenant_id,project_id,version) WHERE c.tenant_id=$1 AND c.project_id=$2`, tenantID, projectID).Scan(&published.TenantID, &published.ProjectID, &published.Version, &published.SourceRevision, &config, &published.Digest, &published.PublisherID, &published.ReviewerID)
		if err == nil {
			if err := json.Unmarshal(config, &published.Config); err != nil {
				return err
			}
			current = &published
		} else if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		return nil
	})
	if err != nil {
		return Draft{}, nil, err
	}
	return draft, current, nil
}

// SaveDraft persists even invalid configurations so editors can correct them.
// Validation errors are stored and returned in deterministic domain order.
func (s *Store) SaveDraft(ctx context.Context, tenantID, projectID, actorID string, expectedRevision int64, config projectworkflow.Config, idempotencyKey string) (Draft, error) {
	if s == nil || s.projects == nil || tenantID == "" || projectID == "" || actorID == "" || expectedRevision < 0 || idempotencyKey == "" {
		return Draft{}, ErrInvalidRequest
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		return Draft{}, err
	}
	digest := digestJSON(configJSON)
	validation := projectworkflow.Validate(config)
	validationJSON, err := json.Marshal(validation)
	if err != nil {
		return Draft{}, err
	}
	fingerprint, err := fingerprint(struct {
		Project  string
		Expected int64
		Config   json.RawMessage
	}{projectID, expectedRevision, configJSON})
	if err != nil {
		return Draft{}, err
	}
	var out Draft
	err = s.projects.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if replay, result, err := claim(ctx, tx, tenantID, actorID, "workflow.draft.save", idempotencyKey, fingerprint); err != nil {
			return err
		} else if replay {
			return json.Unmarshal(result, &out)
		}
		var lockedProject string
		if err := tx.QueryRow(ctx, `SELECT id FROM project WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, projectID).Scan(&lockedProject); err != nil {
			return mapNoRows(err)
		}
		var exists bool
		var revision int64
		err := tx.QueryRow(ctx, `SELECT revision FROM project_workflow_draft WHERE tenant_id=$1 AND project_id=$2 FOR UPDATE`, tenantID, projectID).Scan(&revision)
		if err == nil {
			exists = true
		} else if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		if (!exists && expectedRevision != 0) || (exists && revision != expectedRevision) {
			return ErrRevisionConflict
		}
		if exists {
			revision++
		} else {
			revision = 1
		}
		if exists {
			_, err = tx.Exec(ctx, `UPDATE project_workflow_draft SET revision=$1,config_json=$2::jsonb,config_digest=$3,validation_json=$4::jsonb,actor_id=$5,updated_at=now() WHERE tenant_id=$6 AND project_id=$7`, revision, configJSON, digest, validationJSON, actorID, tenantID, projectID)
		} else {
			_, err = tx.Exec(ctx, `INSERT INTO project_workflow_draft(tenant_id,project_id,revision,config_json,config_digest,validation_json,actor_id) VALUES($1,$2,$3,$4::jsonb,$5,$6::jsonb,$7)`, tenantID, projectID, revision, configJSON, digest, validationJSON, actorID)
		}
		if err != nil {
			return err
		}
		out = Draft{TenantID: tenantID, ProjectID: projectID, ActorID: actorID, Revision: revision, Config: config, Digest: digest, Validation: validation}
		if _, err := projectstore.AppendOutboxEventTx(ctx, tx, projectstore.AppendOutboxEvent{
			TenantID: tenantID, ProjectID: projectID, AggregateID: projectID, EventType: "workflow.draft_saved",
			SourceRevision: revision, Classification: "INTERNAL",
			Value: map[string]any{"draftRevision": revision, "digest": digest},
		}); err != nil {
			return err
		}
		return finish(ctx, tx, tenantID, actorID, "workflow.draft.save", idempotencyKey, out)
	})
	if err != nil {
		return Draft{}, err
	}
	return out, nil
}

// Publish activates an immutable configuration only when the reviewed digest
// and optimistic draft revision still match. Preview/migration evidence is
// retained verbatim as review evidence for audit and application policy.
func (s *Store) Publish(ctx context.Context, tenantID, projectID, publisherID string, expectedRevision, expectedCurrentPublishedVersion int64, reviewedDigest, idempotencyKey string, evidence ReviewEvidence) (Published, error) {
	return s.publish(ctx, tenantID, projectID, publisherID, expectedRevision, expectedCurrentPublishedVersion, reviewedDigest, "", idempotencyKey, evidence, projectworkflow.MigrationMappings{}, false)
}

// PublishWithMigration requires the exact plan digest returned during review
// and rechecks it against fresh task snapshots while holding the project row
// lock. The mappings are part of the request fingerprint and are applied only
// after the digest and planner result match.
func (s *Store) PublishWithMigration(ctx context.Context, tenantID, projectID, publisherID string, expectedRevision, expectedCurrentPublishedVersion int64, reviewedDigest, reviewedPlanDigest, idempotencyKey string, evidence ReviewEvidence, mappings projectworkflow.MigrationMappings) (Published, error) {
	if reviewedPlanDigest == "" {
		return Published{}, ErrInvalidRequest
	}
	return s.publish(ctx, tenantID, projectID, publisherID, expectedRevision, expectedCurrentPublishedVersion, reviewedDigest, reviewedPlanDigest, idempotencyKey, evidence, mappings, true)
}

func (s *Store) publish(ctx context.Context, tenantID, projectID, publisherID string, expectedRevision, expectedCurrentPublishedVersion int64, reviewedDigest, reviewedPlanDigest, idempotencyKey string, evidence ReviewEvidence, mappings projectworkflow.MigrationMappings, requirePlanDigest bool) (Published, error) {
	if s == nil || s.projects == nil || tenantID == "" || projectID == "" || publisherID == "" || expectedRevision <= 0 || expectedCurrentPublishedVersion < 0 || reviewedDigest == "" || idempotencyKey == "" {
		return Published{}, ErrInvalidRequest
	}
	if evidence.Required && (evidence.ReviewerID == "" || evidence.ReviewerID == publisherID || evidence.DecisionID == "" || evidence.ReviewedDigest != reviewedDigest || evidence.ReviewedPlanDigest != reviewedPlanDigest) {
		return Published{}, ErrReviewRequired
	}
	fingerprint, err := fingerprint(struct {
		Project, Digest, PlanDigest string
		Revision, CurrentVersion    int64
		Evidence                    ReviewEvidence
		Mappings                    projectworkflow.MigrationMappings
	}{projectID, reviewedDigest, reviewedPlanDigest, expectedRevision, expectedCurrentPublishedVersion, evidence, mappings})
	if err != nil {
		return Published{}, err
	}
	var out Published
	err = s.projects.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if replay, result, err := claim(ctx, tx, tenantID, publisherID, "workflow.publish", idempotencyKey, fingerprint); err != nil {
			return err
		} else if replay {
			return json.Unmarshal(result, &out)
		}
		var lifecycle string
		if err := tx.QueryRow(ctx, `SELECT lifecycle FROM project WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, projectID).Scan(&lifecycle); err != nil {
			return mapNoRows(err)
		}
		if lifecycle != "ACTIVE" {
			return ErrInvalidRequest
		}
		var currentVersion int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT version FROM project_workflow_current WHERE tenant_id=$1 AND project_id=$2),0)`, tenantID, projectID).Scan(&currentVersion); err != nil {
			return err
		}
		if currentVersion != expectedCurrentPublishedVersion {
			return ErrRevisionConflict
		}
		var currentConfig projectworkflow.Config
		currentDigest := ""
		if currentVersion > 0 {
			var currentJSON []byte
			if err := tx.QueryRow(ctx, `SELECT config_json,digest FROM project_workflow_version WHERE tenant_id=$1 AND project_id=$2 AND version=$3`, tenantID, projectID, currentVersion).Scan(&currentJSON, &currentDigest); err != nil {
				return err
			}
			if err := json.Unmarshal(currentJSON, &currentConfig); err != nil {
				return err
			}
		}
		var revision int64
		var raw []byte
		var storedDigest string
		if err := tx.QueryRow(ctx, `SELECT revision,config_json,config_digest FROM project_workflow_draft WHERE tenant_id=$1 AND project_id=$2 FOR UPDATE`, tenantID, projectID).Scan(&revision, &raw, &storedDigest); err != nil {
			return mapNoRows(err)
		}
		if revision != expectedRevision {
			return ErrRevisionConflict
		}
		if storedDigest != reviewedDigest {
			return ErrDigestConflict
		}
		var config projectworkflow.Config
		if err := json.Unmarshal(raw, &config); err != nil {
			return err
		}
		var next int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM project_workflow_version WHERE tenant_id=$1 AND project_id=$2`, tenantID, projectID).Scan(&next); err != nil {
			return err
		}
		sealed, err := projectworkflow.Publish(config, uint64(next))
		if err != nil {
			return err
		}
		canonical, err := json.Marshal(sealed.Snapshot())
		if err != nil {
			return err
		}
		if digestJSON(canonical) != reviewedDigest {
			return ErrDigestConflict
		}
		if err := checkPublishMigration(ctx, tx, tenantID, projectID, currentConfig, sealed.Snapshot(), currentVersion > 0, next, currentVersion, revision, currentDigest, reviewedDigest, publisherID, mappings, reviewedPlanDigest, requirePlanDigest); err != nil {
			return err
		}
		evidenceJSON, err := json.Marshal(evidence)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO project_workflow_version(tenant_id,project_id,version,source_revision,config_json,digest,publisher_id,reviewer_id,review_evidence) VALUES($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9::jsonb)`, tenantID, projectID, next, revision, canonical, reviewedDigest, publisherID, evidence.ReviewerID, evidenceJSON); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO project_workflow_current(tenant_id,project_id,version) VALUES($1,$2,$3) ON CONFLICT(tenant_id,project_id) DO UPDATE SET version=EXCLUDED.version,updated_at=now()`, tenantID, projectID, next)
		if err != nil {
			return err
		}
		out = Published{TenantID: tenantID, ProjectID: projectID, PublisherID: publisherID, ReviewerID: evidence.ReviewerID, Version: next, SourceRevision: revision, Config: sealed.Snapshot(), Digest: sealed.Digest()}
		if _, err := projectstore.AppendOutboxEventTx(ctx, tx, projectstore.AppendOutboxEvent{
			TenantID: tenantID, ProjectID: projectID, AggregateID: projectID, EventType: "workflow.published",
			SourceRevision: revision, Classification: "INTERNAL",
			Value: map[string]any{"version": next, "sourceRevision": revision, "digest": sealed.Digest(), "publisherId": publisherID},
		}); err != nil {
			return err
		}
		return finish(ctx, tx, tenantID, publisherID, "workflow.publish", idempotencyKey, out)
	})
	if err != nil {
		return Published{}, err
	}
	return out, nil
}

func digestJSON(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

// checkPublishMigration re-reads the complete bounded active-task set after the
// project row lock is held. Create and move operations take that same lock, so
// a task committed after an application preview is included here.
func checkPublishMigration(ctx context.Context, tx dbport.Tx, tenantID, projectID string, current, pending projectworkflow.Config, hasCurrent bool, nextVersion, currentVersion, draftRevision int64, currentDigest, pendingDigest string, publisherID string, mappings projectworkflow.MigrationMappings, reviewedPlanDigest string, requirePlanDigest bool) error {
	tasks, err := loadActiveTaskSnapshots(ctx, tx, tenantID, projectID)
	if err != nil {
		return err
	}
	if len(tasks) > projectworkflow.MaxTaskSnapshotsPerPreview {
		return &MigrationBlockedError{Preview: projectworkflow.MigrationPreview{TaskCount: len(tasks), Safe: false}, Cause: projectworkflow.ErrSnapshotLimitExceeded}
	}
	if !hasCurrent {
		if len(tasks) != 0 {
			return &MigrationBlockedError{Preview: projectworkflow.MigrationPreview{TaskCount: len(tasks), Safe: false}, Cause: projectworkflow.ErrUnsafeMigration}
		}
		return nil
	}
	preview, err := projectworkflow.PreviewMigration(projectworkflow.MigrationRequest{Current: current, Pending: pending, Tasks: tasks, StatusMappings: mappings.Statuses, FieldMappings: mappings.Fields})
	if err != nil {
		return &MigrationBlockedError{Preview: preview, Cause: err}
	}
	computedPlanDigest := projectworkflow.DigestMigrationPlan(currentDigest, pendingDigest, uint64(currentVersion), uint64(draftRevision), mappings, preview, tasks)
	if requirePlanDigest && computedPlanDigest != reviewedPlanDigest {
		return &MigrationBlockedError{Preview: preview, Cause: ErrMigrationPlanDigestConflict}
	}
	if preview.AffectedTaskCount != 0 {
		if preview.AffectedTaskCount > projectworkflow.MaxAffectedTasksPerPublish {
			return &MigrationBlockedError{Preview: preview, Cause: projectworkflow.ErrMigrationLimitExceeded}
		}
		if err := migrateAffectedTasks(ctx, tx, tenantID, projectID, tasks, preview, pending, nextVersion, publisherID); err != nil {
			return &MigrationBlockedError{Preview: preview, Cause: err}
		}
	}
	return nil
}

// migrateAffectedTasks applies only per-task changes returned by the validated
// planner after the caller's explicit mappings are bound to the reviewed plan.
func migrateAffectedTasks(ctx context.Context, tx dbport.Tx, tenantID, projectID string, tasks []projectworkflow.TaskSnapshot, preview projectworkflow.MigrationPreview, pending projectworkflow.Config, version int64, actorID string) error {
	byTask := make(map[string]projectworkflow.TaskSnapshot, len(tasks))
	for _, task := range tasks {
		byTask[task.ID] = task
	}
	mappings := make(map[string]projectworkflow.TaskMapping, len(preview.TaskMappings))
	for _, mapping := range preview.TaskMappings {
		mappings[mapping.TaskID] = mapping
	}
	fieldIndex := make(map[string]projectworkflow.Field, len(pending.Fields))
	for _, field := range pending.Fields {
		if !field.Retired {
			fieldIndex[field.ID] = field
		}
	}
	typeIndex := make(map[string]projectworkflow.TaskType, len(pending.TaskTypes))
	for _, taskType := range pending.TaskTypes {
		typeIndex[taskType.ID] = taskType
	}
	for _, taskID := range preview.AffectedTaskIDs {
		task, ok := byTask[taskID]
		if !ok {
			return fmt.Errorf("%w: affected task %q disappeared from the publish snapshot", ErrMigrationRequired, taskID)
		}
		if task.Revision <= 0 {
			return fmt.Errorf("%w: affected task %q has no revision in the publish snapshot", ErrMigrationRequired, taskID)
		}
		mapping := mappings[taskID]
		fieldTargets := make(map[string]string, len(mapping.FieldMappings))
		for _, fieldMapping := range mapping.FieldMappings {
			fieldTargets[fieldMapping.SourceID] = fieldMapping.TargetID
		}
		nextFields := make(map[string]projectdomain.TaskFieldEdit, len(task.Fields)+len(mapping.DefaultsApplied))
		changedIDs := make([]string, 0, len(fieldTargets)+len(mapping.DefaultsApplied))
		for sourceID, raw := range task.Fields {
			targetID := sourceID
			if mapped, exists := fieldTargets[sourceID]; exists {
				targetID = mapped
				changedIDs = append(changedIDs, targetID)
			}
			field, exists := fieldIndex[targetID]
			if !exists {
				return fmt.Errorf("%w: task %q field %q has no planner-approved destination", ErrMigrationRequired, taskID, sourceID)
			}
			value, err := fieldCanonicalValue(task.ID, sourceID, raw)
			if err != nil {
				return err
			}
			nextFields[targetID] = projectdomain.TaskFieldEdit{FieldID: targetID, Type: string(field.Type), CanonicalValue: value}
		}
		for _, fieldID := range mapping.DefaultsApplied {
			if _, exists := nextFields[fieldID]; exists {
				continue
			}
			field, exists := fieldIndex[fieldID]
			if !exists || len(field.Default) == 0 {
				return fmt.Errorf("%w: planner default for task %q field %q is unavailable", ErrMigrationRequired, taskID, fieldID)
			}
			value, err := fieldCanonicalValue(task.ID, fieldID, field.Default)
			if err != nil {
				return err
			}
			nextFields[fieldID] = projectdomain.TaskFieldEdit{FieldID: fieldID, Type: string(field.Type), CanonicalValue: value}
			changedIDs = append(changedIDs, fieldID)
		}
		if _, exists := typeIndex[task.TypeID]; !exists {
			return fmt.Errorf("%w: task %q has no target type", ErrMigrationRequired, taskID)
		}
		sort.Strings(changedIDs)
		fieldsJSON, err := json.Marshal(nextFields)
		if err != nil {
			return err
		}
		targetStatus := task.StatusID
		if mapping.TargetStatusID != "" {
			targetStatus = mapping.TargetStatusID
		}
		if err := projectstore.MigrateTaskTx(ctx, tx, tenantID, projectID, taskID, task.Revision, targetStatus, fieldsJSON, version, actorID, changedIDs); err != nil {
			return fmt.Errorf("migrate task %q: %w", taskID, err)
		}
	}
	return nil
}

func fieldCanonicalValue(taskID, fieldID string, raw json.RawMessage) (string, error) {
	var existing projectdomain.TaskFieldEdit
	if json.Unmarshal(raw, &existing) == nil && existing.FieldID == fieldID && existing.CanonicalValue != "" {
		return existing.CanonicalValue, nil
	}
	if !json.Valid(raw) {
		return "", fmt.Errorf("%w: invalid field data for task %q field %q", ErrMigrationRequired, taskID, fieldID)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "null", nil
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value, nil
	}
	return string(raw), nil
}

func loadActiveTaskSnapshots(ctx context.Context, tx dbport.Tx, tenantID, projectID string) ([]projectworkflow.TaskSnapshot, error) {
	limit := projectworkflow.MaxTaskSnapshotsPerPreview + 1
	rows, err := tx.Query(ctx, `SELECT id,type_id,status_id,revision,fields_json FROM project_task WHERE tenant_id=$1 AND project_id=$2 AND archived=false ORDER BY id LIMIT $3`, tenantID, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := make([]projectworkflow.TaskSnapshot, 0, limit)
	for rows.Next() {
		var task projectworkflow.TaskSnapshot
		var fields []byte
		if err := rows.Scan(&task.ID, &task.TypeID, &task.StatusID, &task.Revision, &fields); err != nil {
			return nil, err
		}
		task.Fields, err = projectstore.DecodeTaskFieldValues(fields)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tasks, nil
}

func fingerprint(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return digestJSON(b), nil
}
func mapNoRows(err error) error {
	if errors.Is(err, dbport.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
func claim(ctx context.Context, tx dbport.Tx, tenantID, actorID, operation, key, fp string) (bool, []byte, error) {
	var claimed int
	err := tx.QueryRow(ctx, `INSERT INTO project_workflow_idempotency(tenant_id,actor_id,operation,client_key,fingerprint) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING RETURNING 1`, tenantID, actorID, operation, key, fp).Scan(&claimed)
	if err == nil {
		return false, nil, nil
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return false, nil, err
	}
	var prior string
	var result []byte
	if err := tx.QueryRow(ctx, `SELECT fingerprint,result_json FROM project_workflow_idempotency WHERE tenant_id=$1 AND actor_id=$2 AND operation=$3 AND client_key=$4`, tenantID, actorID, operation, key).Scan(&prior, &result); err != nil {
		return false, nil, err
	}
	if prior != fp {
		return false, nil, ErrIdempotencyReuse
	}
	return true, result, nil
}
func finish(ctx context.Context, tx dbport.Tx, tenantID, actorID, operation, key string, result any) error {
	b, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE project_workflow_idempotency SET result_json=$1::jsonb WHERE tenant_id=$2 AND actor_id=$3 AND operation=$4 AND client_key=$5`, b, tenantID, actorID, operation, key)
	if err != nil {
		return fmt.Errorf("persist workflow idempotency result: %w", err)
	}
	return nil
}
