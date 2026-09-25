// Package project owns ordinary project execution state. Human Work remains
// the authority for governed WorkItem assignment, claims, and outcomes.
package project

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type ProjectID string
type TaskID string
type TypeID string
type Priority string

const (
	PriorityLow    Priority = "LOW"
	PriorityNormal Priority = "NORMAL"
	PriorityHigh   Priority = "HIGH"
	PriorityUrgent Priority = "URGENT"
)

type Lifecycle string

const (
	LifecycleActive    Lifecycle = "ACTIVE"
	LifecycleSuspended Lifecycle = "SUSPENDED"
	LifecycleArchived  Lifecycle = "ARCHIVED"
)

type ActorRole string

const (
	RoleOwner    ActorRole = "OWNER"
	RoleOperator ActorRole = "SCOPED_OPERATOR"
)

var (
	ErrInvalidIdentity    = errors.New("project: invalid identity")
	ErrInvalidState       = errors.New("project: invalid state")
	ErrRevisionConflict   = errors.New("project: revision conflict")
	ErrNotAuthorized      = errors.New("project: actor not authorized")
	ErrWriteUnavailable   = errors.New("project: writes unavailable")
	ErrInvalidWorkItemRef = errors.New("project: invalid WorkItem reference")
)

type Project struct {
	ID       ProjectID
	TenantID string
	OwnerID  string
	Name     string
	Timezone string
	State    Lifecycle
	Revision uint64
	History  []ProjectEvent
}

type ProjectEvent struct {
	From, To Lifecycle
	ActorID  string
	Role     ActorRole
	Reason   string
	Revision uint64
}

// UpdateSettings revises project-owned display and timezone metadata without
// changing its lifecycle. Timezone values must be IANA locations.
func (p Project) UpdateSettings(name, timezone string, expected uint64) (Project, error) {
	if expected != p.Revision {
		return Project{}, ErrRevisionConflict
	}
	if p.State != LifecycleActive || strings.TrimSpace(name) == "" || strings.TrimSpace(timezone) == "" {
		return Project{}, ErrInvalidState
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return Project{}, fmt.Errorf("%w: invalid project timezone %q", ErrInvalidState, timezone)
	}
	p.Name, p.Timezone = strings.TrimSpace(name), timezone
	p.Revision++
	return p, nil
}

func NewProject(id ProjectID, tenantID, ownerID, name, timezone string) (Project, error) {
	if strings.TrimSpace(string(id)) == "" || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(ownerID) == "" || strings.TrimSpace(name) == "" {
		return Project{}, ErrInvalidIdentity
	}
	if strings.TrimSpace(timezone) == "" {
		return Project{}, fmt.Errorf("%w: timezone required", ErrInvalidState)
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return Project{}, fmt.Errorf("%w: invalid project timezone %q", ErrInvalidState, timezone)
	}
	return Project{ID: id, TenantID: tenantID, OwnerID: ownerID, Name: name, Timezone: timezone, State: LifecycleActive, Revision: 1}, nil
}

// TransitionProject applies the contract's lifecycle graph with optimistic
// concurrency and role checks. It retains a revisioned history of transitions.
func (p Project) TransitionProject(target Lifecycle, expected uint64, actor string, role ActorRole, reason string) (Project, error) {
	if p.Revision != expected {
		return Project{}, ErrRevisionConflict
	}
	if strings.TrimSpace(actor) == "" || !allowedTransition(p.State, target) {
		return Project{}, ErrInvalidState
	}
	switch target {
	case LifecycleSuspended:
		if role != RoleOperator || strings.TrimSpace(reason) == "" {
			return Project{}, ErrNotAuthorized
		}
	case LifecycleArchived, LifecycleActive:
		if role != RoleOwner || actor != p.OwnerID {
			return Project{}, ErrNotAuthorized
		}
	default:
		return Project{}, ErrInvalidState
	}
	p.History = append(append([]ProjectEvent(nil), p.History...), ProjectEvent{From: p.State, To: target, ActorID: actor, Role: role, Reason: reason, Revision: p.Revision + 1})
	p.State = target
	p.Revision++
	return p, nil
}

func allowedTransition(from, to Lifecycle) bool {
	switch from {
	case LifecycleActive:
		return to == LifecycleSuspended || to == LifecycleArchived
	case LifecycleSuspended:
		return to == LifecycleActive || to == LifecycleArchived
	case LifecycleArchived:
		return to == LifecycleActive
	default:
		return false
	}
}

func (p Project) CanWrite() bool { return p.State == LifecycleActive }

type WorkItemProjection struct {
	ID              string
	ObservedVersion uint64
	SafeStatus      string
	Freshness       string
}

type Task struct {
	ID          TaskID
	ProjectID   ProjectID
	TenantID    string
	Title       string
	Description string
	Status      string
	TypeID      TypeID
	Priority    Priority
	AssigneeID  string
	DueDate     string
	Revision    uint64
	Archived    bool
	WorkItem    *WorkItemProjection
	Fields      map[string]TaskFieldEdit
}

func NewTask(id TaskID, project Project, title, initialStatusID string) (Task, error) {
	return NewTaskWithDetails(id, project, title, initialStatusID, "task_default", PriorityNormal)
}

// NewTaskWithDetails creates a project task using stable configured type and
// initial status IDs. Type membership is validated by the workflow policy.
func NewTaskWithDetails(id TaskID, project Project, title, initialStatusID string, typeID TypeID, priority Priority) (Task, error) {
	if strings.TrimSpace(string(id)) == "" || project.ID == "" || project.TenantID == "" || strings.TrimSpace(initialStatusID) == "" || !ValidateTaskText(title, "") {
		return Task{}, ErrInvalidIdentity
	}
	if strings.TrimSpace(string(typeID)) == "" || !priority.Valid() {
		return Task{}, ErrInvalidState
	}
	if !project.CanWrite() {
		return Task{}, ErrWriteUnavailable
	}
	return Task{ID: id, ProjectID: project.ID, TenantID: project.TenantID, Title: title, Status: initialStatusID, TypeID: typeID, Priority: priority, Revision: 1}, nil
}

// Planning field bounds; the store enforces the same limits.
const (
	MaxStoryPoints = 1000
	MaxTaskLabels  = 20
)

func (p Priority) Valid() bool {
	return p == PriorityLow || p == PriorityNormal || p == PriorityHigh || p == PriorityUrgent
}

type TaskUpdate struct{ Title, Description, AssigneeID, DueDate string }

// TaskPatch represents sparse edits: nil leaves the field unchanged, while a
// pointer to an empty optional value clears it.
type TaskPatch struct {
	Title       *string
	Description *string
	AssigneeID  *string
	DueDate     *string
	TypeID      *TypeID
	Priority    *Priority
	// Planning fields: StartDate is a civil date (empty clears it), and
	// Labels replaces the whole set.
	StartDate   *string
	StoryPoints *uint32
	Labels      *[]string
}

// ReviseTask changes only project-owned fields; the owning project is immutable.
func (t Task) ReviseTask(project Project, update TaskUpdate, expected uint64) (Task, error) {
	if expected != t.Revision {
		return Task{}, ErrRevisionConflict
	}
	if t.ProjectID != project.ID || t.TenantID != project.TenantID {
		return Task{}, ErrInvalidIdentity
	}
	if !project.CanWrite() || t.Archived {
		return Task{}, ErrWriteUnavailable
	}
	if !ValidateTaskText(update.Title, update.Description) {
		return Task{}, ErrInvalidState
	}
	t.Title, t.Description, t.AssigneeID, t.DueDate = update.Title, update.Description, update.AssigneeID, update.DueDate
	t.Revision++
	return t, nil
}

// PatchTask applies only supplied fields and requires the expected task
// revision. Empty description, assignee, and due date values clear those fields.
func (t Task) PatchTask(project Project, patch TaskPatch, expected uint64) (Task, error) {
	if expected != t.Revision {
		return Task{}, ErrRevisionConflict
	}
	if t.ProjectID != project.ID || t.TenantID != project.TenantID {
		return Task{}, ErrInvalidIdentity
	}
	if !project.CanWrite() || t.Archived {
		return Task{}, ErrWriteUnavailable
	}
	if patch.Title != nil {
		if !ValidateTaskText(*patch.Title, "") {
			return Task{}, ErrInvalidState
		}
		t.Title = *patch.Title
	}
	if patch.Description != nil {
		if !validTaskText(*patch.Description, MaxTaskDescriptionRunes, true) {
			return Task{}, ErrInvalidState
		}
		t.Description = *patch.Description
	}
	if patch.AssigneeID != nil {
		t.AssigneeID = *patch.AssigneeID
	}
	if patch.DueDate != nil {
		t.DueDate = *patch.DueDate
	}
	if patch.TypeID != nil {
		if strings.TrimSpace(string(*patch.TypeID)) == "" {
			return Task{}, ErrInvalidState
		}
		t.TypeID = *patch.TypeID
	}
	if patch.Priority != nil {
		if !patch.Priority.Valid() {
			return Task{}, ErrInvalidState
		}
		t.Priority = *patch.Priority
	}
	if patch.StoryPoints != nil && *patch.StoryPoints > MaxStoryPoints {
		return Task{}, ErrInvalidState
	}
	if patch.Labels != nil && len(*patch.Labels) > MaxTaskLabels {
		return Task{}, ErrInvalidState
	}
	t.Revision++
	return t, nil
}

// TransitionPolicy is the boundary to the project-owned workflow configuration
// domain. It must validate a configured transition at the supplied revision.
type TransitionPolicy interface {
	ValidateTaskTransition(projectID ProjectID, fromStatusID, toStatusID string, expectedConfigRevision uint64, fieldEdits []TaskFieldEdit) error
}

// TaskFieldEdit carries a typed field identifier and its canonical value to
// the project workflow policy. The workflow domain validates the field type.
type TaskFieldEdit struct {
	FieldID        string
	Type           string
	CanonicalValue string
}

// TransitionTask applies a policy-approved status change under the task's
// expected revision. Optional field edits are validated in the same policy
// call. Policy validation occurs before the returned copy changes.
func (t Task) TransitionTask(project Project, policy TransitionPolicy, targetStatusID string, expectedTaskRevision, expectedConfigRevision uint64, fieldEdits []TaskFieldEdit) (Task, error) {
	if expectedTaskRevision != t.Revision {
		return Task{}, ErrRevisionConflict
	}
	if t.ProjectID != project.ID || t.TenantID != project.TenantID {
		return Task{}, ErrInvalidIdentity
	}
	if t.Archived || !project.CanWrite() {
		return Task{}, ErrWriteUnavailable
	}
	if policy == nil || strings.TrimSpace(targetStatusID) == "" || expectedConfigRevision == 0 {
		return Task{}, ErrInvalidState
	}
	edits := append([]TaskFieldEdit(nil), fieldEdits...)
	if err := policy.ValidateTaskTransition(t.ProjectID, t.Status, targetStatusID, expectedConfigRevision, edits); err != nil {
		return Task{}, err
	}
	t.Status = targetStatusID
	if len(edits) > 0 {
		fields := make(map[string]TaskFieldEdit, len(t.Fields)+len(edits))
		for id, field := range t.Fields {
			fields[id] = field
		}
		for _, edit := range edits {
			fields[edit.FieldID] = edit
		}
		t.Fields = fields
	}
	t.Revision++
	return t, nil
}

func (t Task) Archive(project Project, expected uint64) (Task, error) {
	if expected != t.Revision {
		return Task{}, ErrRevisionConflict
	}
	if t.ProjectID != project.ID || t.TenantID != project.TenantID {
		return Task{}, ErrInvalidIdentity
	}
	if !project.CanWrite() {
		return Task{}, ErrWriteUnavailable
	}
	if t.Archived {
		return Task{}, ErrInvalidState
	}
	t.Archived = true
	t.Revision++
	return t, nil
}

func (t Task) Restore(project Project, expected uint64) (Task, error) {
	if expected != t.Revision {
		return Task{}, ErrRevisionConflict
	}
	if t.ProjectID != project.ID || t.TenantID != project.TenantID {
		return Task{}, ErrInvalidIdentity
	}
	if !project.CanWrite() {
		return Task{}, ErrWriteUnavailable
	}
	if !t.Archived {
		return Task{}, ErrInvalidState
	}
	t.Archived = false
	t.Revision++
	return t, nil
}

// WithWorkItemProjection attaches a safe, read-only observed projection. It
// provides no operation that can issue a command to or mutate a Human WorkItem.
func (t Task) WithWorkItemProjection(ref WorkItemProjection) (Task, error) {
	if strings.TrimSpace(ref.ID) == "" || ref.ObservedVersion == 0 || strings.TrimSpace(ref.SafeStatus) == "" || strings.TrimSpace(ref.Freshness) == "" {
		return Task{}, ErrInvalidWorkItemRef
	}
	copy := ref
	t.WorkItem = &copy
	return t, nil
}
