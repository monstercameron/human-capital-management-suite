package projectworkflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

const (
	MaxAffectedTasksPerPublish = 1000
	MaxTaskSnapshotsPerPreview = 10000
)

var (
	ErrMigrationLimitExceeded = errors.New("projectworkflow: migration exceeds affected task limit")
	ErrSnapshotLimitExceeded  = errors.New("projectworkflow: migration preview snapshot limit exceeded")
	ErrUnsafeMigration        = errors.New("projectworkflow: migration is unsafe")
)

// TaskSnapshot is the bounded, active-task projection needed to evaluate a
// configuration migration. Field values are consumed only for validation and
// are never copied into the preview result.
type TaskSnapshot struct {
	ID       string                     `json:"id"`
	TypeID   string                     `json:"type_id"`
	StatusID string                     `json:"status_id"`
	Revision int64                      `json:"revision,omitempty"`
	Fields   map[string]json.RawMessage `json:"fields,omitempty"`
}

type MigrationRequest struct {
	Current        Config            `json:"current"`
	Pending        Config            `json:"pending"`
	Tasks          []TaskSnapshot    `json:"tasks"`
	StatusMappings map[string]string `json:"status_mappings,omitempty"`
	FieldMappings  map[string]string `json:"field_mappings,omitempty"`
}

// MigrationMappings contains explicit operator-approved source to target IDs.
// Status and field namespaces are separate so identical IDs in each namespace
// cannot collide.
type MigrationMappings struct {
	Statuses map[string]string `json:"statuses,omitempty"`
	Fields   map[string]string `json:"fields,omitempty"`
}

// DigestMigrationPlan binds the exact config pair, workflow revision fences,
// explicit mappings, projected impact and task snapshot revisions reviewed by
// the publisher. Task field values are never included.
func DigestMigrationPlan(currentDigest, pendingDigest string, currentVersion, draftRevision uint64, mappings MigrationMappings, preview MigrationPreview, tasks []TaskSnapshot) string {
	statuses := cloneMappings(mappings.Statuses)
	fields := cloneMappings(mappings.Fields)
	taskFences := make([]struct {
		ID       string `json:"id"`
		TypeID   string `json:"type_id"`
		StatusID string `json:"status_id"`
		Revision int64  `json:"revision"`
	}, 0, len(tasks))
	ordered := append([]TaskSnapshot(nil), tasks...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	for _, task := range ordered {
		taskFences = append(taskFences, struct {
			ID       string `json:"id"`
			TypeID   string `json:"type_id"`
			StatusID string `json:"status_id"`
			Revision int64  `json:"revision"`
		}{task.ID, task.TypeID, task.StatusID, task.Revision})
	}
	canonical, _ := json.Marshal(struct {
		CurrentDigest  string            `json:"current_digest"`
		PendingDigest  string            `json:"pending_digest"`
		CurrentVersion uint64            `json:"current_version"`
		DraftRevision  uint64            `json:"draft_revision"`
		Mappings       MigrationMappings `json:"mappings"`
		Preview        MigrationPreview  `json:"preview"`
		Tasks          any               `json:"tasks"`
	}{currentDigest, pendingDigest, currentVersion, draftRevision, MigrationMappings{Statuses: statuses, Fields: fields}, preview, taskFences})
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

func cloneMappings(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for from, to := range in {
		out[from] = to
	}
	return out
}

type MigrationIssue struct {
	Code     string `json:"code"`
	TaskID   string `json:"task_id,omitempty"`
	FieldID  string `json:"field_id,omitempty"`
	StatusID string `json:"status_id,omitempty"`
	Message  string `json:"message"`
}

type MigrationIssues []MigrationIssue

func (e MigrationIssues) Error() string {
	if len(e) == 0 {
		return ""
	}
	return fmt.Sprintf("projectworkflow: unsafe migration: %s", e[0].Code)
}
func (e MigrationIssues) Is(target error) bool { return target == ErrUnsafeMigration }

type StatusChange struct {
	StatusID    string         `json:"status_id"`
	OldCategory StatusCategory `json:"old_category"`
	NewCategory StatusCategory `json:"new_category"`
}

type FieldChange struct {
	FieldID           string    `json:"field_id"`
	TargetID          string    `json:"target_id,omitempty"`
	OldType           FieldType `json:"old_type"`
	NewType           FieldType `json:"new_type,omitempty"`
	Removed           bool      `json:"removed,omitempty"`
	Added             bool      `json:"added,omitempty"`
	Retired           bool      `json:"retired,omitempty"`
	DefinitionChanged bool      `json:"definition_changed,omitempty"`
}

type TaskMapping struct {
	TaskID          string           `json:"task_id"`
	SourceStatusID  string           `json:"source_status_id"`
	TargetStatusID  string           `json:"target_status_id"`
	FieldMappings   []FieldIDMapping `json:"field_mappings,omitempty"`
	DefaultsApplied []string         `json:"defaults_applied,omitempty"`
}

type FieldIDMapping struct {
	SourceID string `json:"source_id"`
	TargetID string `json:"target_id"`
}

type InvalidValue struct {
	TaskID  string `json:"task_id"`
	FieldID string `json:"field_id"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type MigrationPreview struct {
	TaskCount         int             `json:"task_count"`
	AffectedTaskIDs   []string        `json:"affected_task_ids"`
	AffectedTaskCount int             `json:"affected_task_count"`
	StatusChanges     []StatusChange  `json:"status_changes"`
	FieldChanges      []FieldChange   `json:"field_changes"`
	TaskMappings      []TaskMapping   `json:"task_mappings"`
	InvalidValues     []InvalidValue  `json:"invalid_values"`
	Issues            MigrationIssues `json:"issues"`
	Safe              bool            `json:"safe"`
}

// PreviewMigration compares two validated configurations and evaluates every
// supplied active task. It rejects snapshots above the atomic publish cap and
// returns structured task impacts even when the migration is unsafe.
func PreviewMigration(req MigrationRequest) (MigrationPreview, error) {
	preview := MigrationPreview{TaskCount: len(req.Tasks)}
	if len(req.Tasks) > MaxTaskSnapshotsPerPreview {
		preview.Issues = MigrationIssues{{Code: "TASK_SNAPSHOT_LIMIT_EXCEEDED", Message: fmt.Sprintf("preview contains %d task snapshots; the scan limit is %d", len(req.Tasks), MaxTaskSnapshotsPerPreview)}}
		return preview, ErrSnapshotLimitExceeded
	}
	addIssue := func(code, taskID, fieldID, statusID, message string) {
		preview.Issues = append(preview.Issues, MigrationIssue{Code: code, TaskID: taskID, FieldID: fieldID, StatusID: statusID, Message: message})
	}
	for _, item := range []struct {
		name   string
		config Config
	}{{"current", req.Current}, {"pending", req.Pending}} {
		for _, e := range Validate(item.config) {
			addIssue("INVALID_"+strings.ToUpper(item.name)+"_CONFIG_"+e.Code, "", "", "", item.name+" configuration: "+e.Error())
		}
	}
	if len(preview.Issues) != 0 {
		return preview, MigrationIssues(preview.Issues)
	}

	oldStatuses, newStatuses := statusIndex(req.Current), statusIndex(req.Pending)
	oldFields, newFields := fieldIndex(req.Current), fieldIndex(req.Pending)
	newTypes := taskTypeIndex(req.Pending)
	oldTypes := taskTypeIndex(req.Current)
	for sourceID, targetID := range req.StatusMappings {
		source, sourceOK := oldStatuses[sourceID]
		target, targetOK := newStatuses[targetID]
		if strings.TrimSpace(sourceID) == "" || !sourceOK || source.Retired {
			addIssue("INVALID_STATUS_MAPPING_SOURCE", "", "", sourceID, "status mapping source must be active in the current configuration")
		}
		if strings.TrimSpace(targetID) == "" || !targetOK || target.Retired {
			addIssue("INVALID_STATUS_MAPPING_TARGET", "", "", sourceID, "status mapping target must be active in the pending configuration")
		}
		if sourceID == targetID {
			addIssue("INVALID_STATUS_MAPPING", "", "", sourceID, "status mapping must name a different target status")
		}
	}
	for sourceID, targetID := range req.FieldMappings {
		source, sourceOK := oldFields[sourceID]
		target, targetOK := newFields[targetID]
		if strings.TrimSpace(sourceID) == "" || !sourceOK || source.Retired {
			addIssue("INVALID_FIELD_MAPPING_SOURCE", "", sourceID, "", "field mapping source must be active in the current configuration")
		}
		if strings.TrimSpace(targetID) == "" || !targetOK || target.Retired {
			addIssue("INVALID_FIELD_MAPPING_TARGET", "", sourceID, "", "field mapping target must be active in the pending configuration")
		}
		if sourceID == targetID {
			addIssue("INVALID_FIELD_MAPPING", "", sourceID, "", "field mapping must name a different target field")
		}
	}
	if len(preview.Issues) != 0 {
		return preview, MigrationIssues(preview.Issues)
	}
	for id, old := range oldStatuses {
		if old.Retired {
			continue
		}
		updated, ok := newStatuses[id]
		if !ok || updated.Retired {
			continue
		}
		if old.Category != updated.Category {
			preview.StatusChanges = append(preview.StatusChanges, StatusChange{StatusID: id, OldCategory: old.Category, NewCategory: updated.Category})
		}
	}
	for id, old := range oldFields {
		updated, ok := newFields[id]
		if !ok || updated.Retired {
			preview.FieldChanges = append(preview.FieldChanges, FieldChange{FieldID: id, OldType: old.Type, Removed: true, Retired: ok && updated.Retired})
			continue
		}
		if old.Type != updated.Type {
			preview.FieldChanges = append(preview.FieldChanges, FieldChange{FieldID: id, TargetID: req.FieldMappings[id], OldType: old.Type, NewType: updated.Type, DefinitionChanged: true})
		} else if !reflect.DeepEqual(old, updated) {
			preview.FieldChanges = append(preview.FieldChanges, FieldChange{FieldID: id, TargetID: id, OldType: old.Type, NewType: updated.Type, DefinitionChanged: true})
		}
	}
	for id, field := range newFields {
		if !field.Retired {
			if old, ok := oldFields[id]; !ok || old.Retired {
				preview.FieldChanges = append(preview.FieldChanges, FieldChange{FieldID: id, TargetID: id, NewType: field.Type, Added: true})
			}
		}
	}
	sort.Slice(preview.StatusChanges, func(i, j int) bool { return preview.StatusChanges[i].StatusID < preview.StatusChanges[j].StatusID })
	sort.Slice(preview.FieldChanges, func(i, j int) bool { return preview.FieldChanges[i].FieldID < preview.FieldChanges[j].FieldID })

	seenTasks := map[string]bool{}
	affected := map[string]bool{}
	for _, task := range req.Tasks {
		before := len(preview.Issues)
		if strings.TrimSpace(task.ID) == "" {
			addIssue("INVALID_TASK_ID", task.ID, "", "", "task ID is required")
		}
		if seenTasks[task.ID] {
			addIssue("DUPLICATE_TASK_ID", task.ID, "", "", "task ID appears more than once in the snapshot")
		}
		seenTasks[task.ID] = true
		if _, ok := oldTypes[task.TypeID]; !ok {
			addIssue("UNKNOWN_CURRENT_TASK_TYPE", task.ID, "", "", "task type is not in the current configuration")
		}
		if _, ok := newTypes[task.TypeID]; !ok {
			addIssue("TASK_TYPE_REMOVED", task.ID, "", "", "task type has no destination in the pending configuration")
		}
		sourceStatus, ok := oldStatuses[task.StatusID]
		if !ok || sourceStatus.Retired {
			addIssue("UNKNOWN_CURRENT_STATUS", task.ID, "", task.StatusID, "task status is not active in the current configuration")
		}
		targetStatusID := task.StatusID
		targetStatus, targetExists := newStatuses[targetStatusID]
		if mapped, hasMapping := req.StatusMappings[task.StatusID]; hasMapping {
			targetStatusID = mapped
			targetStatus, targetExists = newStatuses[targetStatusID]
			if !targetExists || targetStatus.Retired {
				addIssue("INVALID_STATUS_MAPPING", task.ID, "", task.StatusID, "status mapping target must be active in the pending configuration")
			}
		} else if !targetExists || targetStatus.Retired {
			addIssue("STATUS_MAPPING_REQUIRED", task.ID, "", task.StatusID, "removed status requires an explicit destination mapping")
		}
		mapping := TaskMapping{TaskID: task.ID, SourceStatusID: task.StatusID, TargetStatusID: targetStatusID}
		targetValues := map[string]json.RawMessage{}
		for fieldID, raw := range task.Fields {
			oldField, oldOK := oldFields[fieldID]
			if !oldOK || oldField.Retired {
				addIssue("UNKNOWN_CURRENT_FIELD", task.ID, fieldID, "", "task has a field absent from the current active configuration")
				continue
			}
			if oldType, exists := oldTypes[task.TypeID]; exists && !contains(oldType.FieldIDs, fieldID) {
				addIssue("FIELD_NOT_ALLOWED_FOR_CURRENT_TASK_TYPE", task.ID, fieldID, "", "field is not enabled for this task type in the current configuration")
			}
			if message := validateValue(oldField, raw); message != "" {
				preview.InvalidValues = append(preview.InvalidValues, InvalidValue{TaskID: task.ID, FieldID: fieldID, Code: "INVALID_CURRENT_VALUE", Message: message})
				addIssue("INVALID_CURRENT_VALUE", task.ID, fieldID, "", message)
			}
			targetID := fieldID
			updated, exists := newFields[targetID]
			newType, typeExists := newTypes[task.TypeID]
			targetAllowed := typeExists && contains(newType.FieldIDs, targetID)
			requiresMapping := !exists || updated.Retired || oldField.Type != updated.Type || !targetAllowed
			mappedID, hasMapping := req.FieldMappings[fieldID]
			if hasMapping {
				targetID = mappedID
				updated, exists = newFields[targetID]
				if !exists || updated.Retired {
					addIssue("INVALID_FIELD_MAPPING", task.ID, fieldID, "", "field mapping target must be active in the pending configuration")
					continue
				}
				mapping.FieldMappings = append(mapping.FieldMappings, FieldIDMapping{SourceID: fieldID, TargetID: targetID})
			} else if requiresMapping {
				addIssue("FIELD_MAPPING_REQUIRED", task.ID, fieldID, "", "removed or retyped field requires an explicit mapping to a new field ID")
				continue
			}
			if newType, typeExists := newTypes[task.TypeID]; typeExists && !contains(newType.FieldIDs, targetID) {
				addIssue("FIELD_NOT_ALLOWED_FOR_PENDING_TASK_TYPE", task.ID, targetID, "", "field is not enabled for this task type in the pending configuration")
			}
			if previous, collision := targetValues[targetID]; collision && string(previous) != string(raw) {
				addIssue("FIELD_MAPPING_COLLISION", task.ID, targetID, "", "multiple source fields map different values to the same target field")
				continue
			}
			targetValues[targetID] = append(json.RawMessage(nil), raw...)
			if message := validateValue(updated, raw); message != "" {
				preview.InvalidValues = append(preview.InvalidValues, InvalidValue{TaskID: task.ID, FieldID: targetID, Code: "INVALID_PENDING_VALUE", Message: message})
				addIssue("INVALID_PENDING_VALUE", task.ID, targetID, "", message)
			}
		}
		if newType, ok := newTypes[task.TypeID]; ok {
			requiredIDs := append([]string(nil), newType.RequiredFields...)
			for _, fieldID := range newType.FieldIDs {
				if field, exists := newFields[fieldID]; exists && field.Required {
					requiredIDs = append(requiredIDs, fieldID)
				}
			}
			if target, exists := newStatuses[targetStatusID]; exists {
				requiredIDs = append(requiredIDs, target.RequiredFieldIDs...)
			}
			for _, requiredID := range requiredIDs {
				value, present := targetValues[requiredID]
				if present && hasValue(value) {
					continue
				}
				field, exists := newFields[requiredID]
				if exists && len(field.Default) > 0 && validateValue(field, field.Default) == "" {
					targetValues[requiredID] = append(json.RawMessage(nil), field.Default...)
					mapping.DefaultsApplied = append(mapping.DefaultsApplied, requiredID)
					continue
				}
				addIssue("NEW_REQUIRED_FIELD_UNSATISFIED", task.ID, requiredID, "", "required field has no task value or valid default")
			}
		}
		if len(preview.Issues) > before || task.StatusID != targetStatusID || (ok && targetExists && sourceStatus.Category != targetStatus.Category) || len(mapping.FieldMappings) > 0 || len(mapping.DefaultsApplied) > 0 || taskHasChangedField(task, oldFields, newFields) {
			affected[task.ID] = true
		}
		if task.StatusID != targetStatusID || len(mapping.FieldMappings) > 0 || len(mapping.DefaultsApplied) > 0 {
			preview.TaskMappings = append(preview.TaskMappings, mapping)
		}
	}
	for _, invalid := range preview.InvalidValues {
		affected[invalid.TaskID] = true
	}
	for id := range affected {
		if id != "" {
			preview.AffectedTaskIDs = append(preview.AffectedTaskIDs, id)
		}
	}
	sort.Strings(preview.AffectedTaskIDs)
	preview.AffectedTaskCount = len(preview.AffectedTaskIDs)
	if preview.AffectedTaskCount > MaxAffectedTasksPerPublish {
		addIssue("AFFECTED_TASK_LIMIT_EXCEEDED", "", "", "", fmt.Sprintf("%d tasks are affected; the publish limit is %d", preview.AffectedTaskCount, MaxAffectedTasksPerPublish))
		preview.Safe = false
		return preview, ErrMigrationLimitExceeded
	}
	sort.Slice(preview.TaskMappings, func(i, j int) bool { return preview.TaskMappings[i].TaskID < preview.TaskMappings[j].TaskID })
	for i := range preview.TaskMappings {
		sort.Slice(preview.TaskMappings[i].FieldMappings, func(a, b int) bool {
			return preview.TaskMappings[i].FieldMappings[a].SourceID < preview.TaskMappings[i].FieldMappings[b].SourceID
		})
		sort.Strings(preview.TaskMappings[i].DefaultsApplied)
	}
	sort.Slice(preview.InvalidValues, func(i, j int) bool {
		if preview.InvalidValues[i].TaskID == preview.InvalidValues[j].TaskID {
			return preview.InvalidValues[i].FieldID < preview.InvalidValues[j].FieldID
		}
		return preview.InvalidValues[i].TaskID < preview.InvalidValues[j].TaskID
	})
	sort.Slice(preview.Issues, func(i, j int) bool {
		if preview.Issues[i].TaskID == preview.Issues[j].TaskID {
			if preview.Issues[i].FieldID == preview.Issues[j].FieldID {
				return preview.Issues[i].Code < preview.Issues[j].Code
			}
			return preview.Issues[i].FieldID < preview.Issues[j].FieldID
		}
		return preview.Issues[i].TaskID < preview.Issues[j].TaskID
	})
	preview.Safe = len(preview.Issues) == 0 && len(preview.InvalidValues) == 0
	if !preview.Safe {
		return preview, MigrationIssues(preview.Issues)
	}
	return preview, nil
}

func statusIndex(c Config) map[string]Status {
	out := map[string]Status{}
	for _, v := range c.Statuses {
		out[v.ID] = v
	}
	return out
}
func fieldIndex(c Config) map[string]Field {
	out := map[string]Field{}
	for _, v := range c.Fields {
		out[v.ID] = v
	}
	return out
}
func taskTypeIndex(c Config) map[string]TaskType {
	out := map[string]TaskType{}
	for _, v := range c.TaskTypes {
		out[v.ID] = v
	}
	return out
}
func taskHasChangedField(task TaskSnapshot, oldFields, newFields map[string]Field) bool {
	for id := range task.Fields {
		old, oldOK := oldFields[id]
		updated, newOK := newFields[id]
		if !oldOK || !newOK || old.Retired != updated.Retired || old.Required != updated.Required || old.Type != updated.Type || !reflect.DeepEqual(old.Validation, updated.Validation) || old.Classification != updated.Classification {
			return true
		}
	}
	return false
}
