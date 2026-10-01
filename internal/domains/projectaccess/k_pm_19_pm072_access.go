package projectaccess

import (
	"errors"
	"sort"
	"strings"
)

var (
	ErrInvalidTaskGrant   = errors.New("invalid project task grant")
	ErrInvalidFieldPolicy = errors.New("invalid project field policy")
)

// FieldPolicy is the current, server-resolved policy for a project field.
// Project grants can narrow this policy but can never widen it. Keeping this
// as a port lets the project domain compose with the organization AuthZ
// decision without importing a policy implementation into the domain.
type FieldPolicy interface {
	AllowsField(string) bool
}

// FieldPolicyFunc adapts a current policy decision to FieldPolicy.
type FieldPolicyFunc func(string) bool

func (f FieldPolicyFunc) AllowsField(fieldID string) bool {
	return f != nil && f(fieldID)
}

// TaskGrant is an optional later sharing grant. A nil FieldIDs slice means
// that the grant does not narrow fields; a non-nil slice is an explicit
// allowlist. The project role and current field policy are still required.
type TaskGrant struct {
	TenantID  TenantID
	ProjectID ProjectID
	TaskID    string
	UserID    UserID
	Allowed   bool
	FieldIDs  []string
	Revision  uint64
}

type FieldDecision struct {
	Allowed bool
	Reason  string
}

// TaskAccessDecision is the composed result used by every derived surface.
// A denied task has no field rulings: callers must omit the task entirely so
// counts, cursors, activity, notifications and prompts cannot reveal it.
type TaskAccessDecision struct {
	TaskID  string
	Allowed bool
	Fields  map[string]FieldDecision
}

// AuthorizeTask composes current project-role access, an optional task grant,
// and the current field policy. A task grant can only reduce access. The
// function does not return protected values or a field list for a denied task.
func AuthorizeTask(p Project, user UserID, tenant TenantID, taskID string, capability Capability, grant *TaskGrant, fields []string, policy FieldPolicy) (TaskAccessDecision, error) {
	if p.ID == "" || p.Tenant == "" || user == "" || tenant == "" || taskID == "" {
		return TaskAccessDecision{}, ErrInvalidTaskGrant
	}
	if grant != nil {
		if grant.TenantID != p.Tenant || grant.ProjectID != p.ID || grant.TaskID != taskID || grant.UserID != user || grant.Revision == 0 {
			return TaskAccessDecision{}, ErrInvalidTaskGrant
		}
		if err := validateFieldIDs(grant.FieldIDs); err != nil {
			return TaskAccessDecision{}, err
		}
	}
	role := Authorize(p, user, tenant, capability, nil)
	decision := TaskAccessDecision{TaskID: taskID, Allowed: role.Allowed, Fields: make(map[string]FieldDecision)}
	if !role.Allowed {
		return decision, nil
	}
	if grant != nil && !grant.Allowed {
		decision.Allowed = false
		return decision, nil
	}
	if len(fields) == 0 {
		return decision, nil
	}
	if policy == nil {
		return TaskAccessDecision{}, ErrInvalidFieldPolicy
	}
	allowlisted := make(map[string]struct{}, len(grantFieldIDs(grant)))
	if grant != nil && grant.FieldIDs != nil {
		for _, fieldID := range grant.FieldIDs {
			allowlisted[fieldID] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(fields))
	for _, fieldID := range fields {
		if strings.TrimSpace(fieldID) == "" {
			return TaskAccessDecision{}, ErrInvalidTaskGrant
		}
		if _, duplicate := seen[fieldID]; duplicate {
			return TaskAccessDecision{}, ErrInvalidTaskGrant
		}
		seen[fieldID] = struct{}{}
		if grant != nil && grant.FieldIDs != nil {
			if _, ok := allowlisted[fieldID]; !ok {
				decision.Fields[fieldID] = FieldDecision{Reason: "task_field_grant"}
				continue
			}
		}
		if !policy.AllowsField(fieldID) {
			decision.Fields[fieldID] = FieldDecision{Reason: "current_policy"}
			continue
		}
		decision.Fields[fieldID] = FieldDecision{Allowed: true, Reason: "composed_grant"}
	}
	return decision, nil
}

func grantFieldIDs(grant *TaskGrant) []string {
	if grant == nil {
		return nil
	}
	return grant.FieldIDs
}

func validateFieldIDs(fields []string) error {
	seen := make(map[string]struct{}, len(fields))
	for _, fieldID := range fields {
		if strings.TrimSpace(fieldID) == "" {
			return ErrInvalidTaskGrant
		}
		if _, ok := seen[fieldID]; ok {
			return ErrInvalidTaskGrant
		}
		seen[fieldID] = struct{}{}
	}
	return nil
}

type DerivedSurface string

const (
	DerivedSurfaceBoard        DerivedSurface = "BOARD"
	DerivedSurfaceSearch       DerivedSurface = "SEARCH"
	DerivedSurfaceActivity     DerivedSurface = "ACTIVITY"
	DerivedSurfaceNotification DerivedSurface = "NOTIFICATION"
	DerivedSurfaceExport       DerivedSurface = "EXPORT"
	DerivedSurfaceAgentPrompt  DerivedSurface = "AGENT_PROMPT"
	DerivedSurfaceIntegration  DerivedSurface = "INTEGRATION"
)

// TaskSurfaceRecord is the raw record handed to the shared projection
// boundary. Values must not be serialized before ProjectSurface is called.
type TaskSurfaceRecord struct {
	TaskID      string
	Title       string
	StatusID    string
	Revision    uint64
	FieldValues map[string]string
}

// SurfaceView is deliberately uniform across derived surfaces. FieldValues
// contains only values allowed by the composed decision; no hidden field ID,
// count, or placeholder is emitted.
type SurfaceView struct {
	Surface     DerivedSurface
	TaskID      string
	Title       string
	StatusID    string
	Revision    uint64
	FieldValues map[string]string
}

// ProjectSurface applies the same task/field boundary to board, search,
// activity, notification, export, agent, and integration projections.
func ProjectSurface(surface DerivedSurface, record TaskSurfaceRecord, decision TaskAccessDecision) (SurfaceView, bool, error) {
	if record.TaskID == "" || record.Revision == 0 || decision.TaskID != record.TaskID {
		return SurfaceView{}, false, ErrInvalidTaskGrant
	}
	if !validPM072Surface(surface) {
		return SurfaceView{}, false, ErrInvalidTaskGrant
	}
	if !decision.Allowed {
		return SurfaceView{}, false, nil
	}
	view := SurfaceView{Surface: surface, TaskID: record.TaskID, Revision: record.Revision}
	if surface == DerivedSurfaceActivity || surface == DerivedSurfaceNotification {
		// Task revisions include mutations to fields that this viewer may not
		// see. Activity and notification projections therefore carry no source
		// revision that could disclose a hidden-field change.
		view.Revision = 0
	}
	if surface == DerivedSurfaceBoard || surface == DerivedSurfaceSearch || surface == DerivedSurfaceExport || surface == DerivedSurfaceAgentPrompt || surface == DerivedSurfaceIntegration {
		view.Title, view.StatusID = record.Title, record.StatusID
	}
	view.FieldValues = make(map[string]string)
	for fieldID, value := range record.FieldValues {
		field, ok := decision.Fields[fieldID]
		if ok && field.Allowed {
			view.FieldValues[fieldID] = value
		}
	}
	return view, true, nil
}

// ProjectSurfaceBatch filters before returning the slice, making the returned
// length safe to use as a board count or notification/export cardinality.
func ProjectSurfaceBatch(surface DerivedSurface, records []TaskSurfaceRecord, decisions map[string]TaskAccessDecision) ([]SurfaceView, error) {
	if !validPM072Surface(surface) {
		return nil, ErrInvalidTaskGrant
	}
	out := make([]SurfaceView, 0, len(records))
	for _, record := range records {
		decision, present := decisions[record.TaskID]
		if !present {
			// A missing current decision is fail-closed and indistinguishable
			// from a task the caller cannot disclose.
			continue
		}
		view, visible, err := ProjectSurface(surface, record, decision)
		if err != nil {
			return nil, err
		}
		if visible {
			out = append(out, view)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	return out, nil
}

func validPM072Surface(surface DerivedSurface) bool {
	switch surface {
	case DerivedSurfaceBoard, DerivedSurfaceSearch, DerivedSurfaceActivity, DerivedSurfaceNotification, DerivedSurfaceExport, DerivedSurfaceAgentPrompt, DerivedSurfaceIntegration:
		return true
	default:
		return false
	}
}
