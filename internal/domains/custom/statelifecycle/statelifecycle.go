// Package statelifecycle implements WF-DATA-025: tenant types declare
// lifecycles with guarded transitions that can start workflows.
//
// A type declares its states and the transitions between them. Each
// transition is guarded by roles, a named access rule and an optional
// approval, and may start one pinned workflow. A transition is a governed
// operation: it validates, checks every guard, applies, starts the
// workflow, and records evidence — or it changes nothing at all.
//
// The record's state is privately held: ApplyTransition is the only state
// mutator, and the generic data-update path cannot move it, so a guard
// cannot be bypassed through a plain update.
package statelifecycle

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Lifecycle errors. All are matchable with errors.Is.
var (
	// ErrLifecycleInvalid is returned for a malformed lifecycle.
	ErrLifecycleInvalid = errors.New("statelifecycle: lifecycle is malformed")
	// ErrUndeclaredTransition is returned for a transition the lifecycle
	// does not declare. No record reaches a state through one.
	ErrUndeclaredTransition = errors.New("statelifecycle: transition is not declared")
	// ErrBadState is returned for a declared transition taken from the
	// wrong state.
	ErrBadState = errors.New("statelifecycle: record is not in the transition's from state")
	// ErrGuardDenied is returned when any guard denies the transition.
	ErrGuardDenied = errors.New("statelifecycle: transition guard denied")
)

// State is one declared lifecycle state.
type State string

// Guard gates one transition: the actor must hold one of Roles (when
// set), satisfy the named AccessRule (when set) and hold an approval
// (when required). StartWorkflow names the one pinned workflow started
// when the transition applies.
type Guard struct {
	Roles           []string
	AccessRule      string
	RequireApproval bool
	StartWorkflow   string
}

// Transition is one declared state edge.
type Transition struct {
	Name  string
	From  State
	To    State
	Guard Guard
}

// Lifecycle is a tenant type's declared state model.
type Lifecycle struct {
	TypeID      string
	Initial     State
	States      []State
	Transitions []Transition
}

// Validate fails closed on any malformed lifecycle: blank type, initial
// outside the declared states, blank or duplicate states, and
// transitions with blank or duplicate names, undeclared endpoints or no
// movement.
func (l Lifecycle) Validate() error {
	if strings.TrimSpace(l.TypeID) == "" {
		return fmt.Errorf("%w: type is required", ErrLifecycleInvalid)
	}
	known := make(map[State]bool, len(l.States))
	for _, s := range l.States {
		if strings.TrimSpace(string(s)) == "" || known[s] {
			return fmt.Errorf("%w: blank or duplicate state %q", ErrLifecycleInvalid, s)
		}
		known[s] = true
	}
	if !known[l.Initial] {
		return fmt.Errorf("%w: initial %q is not declared", ErrLifecycleInvalid, l.Initial)
	}
	names := make(map[string]bool, len(l.Transitions))
	for _, tr := range l.Transitions {
		if strings.TrimSpace(tr.Name) == "" || names[tr.Name] {
			return fmt.Errorf("%w: blank or duplicate transition %q", ErrLifecycleInvalid, tr.Name)
		}
		names[tr.Name] = true
		if !known[tr.From] || !known[tr.To] {
			return fmt.Errorf("%w: transition %q names an undeclared state", ErrLifecycleInvalid, tr.Name)
		}
		if tr.From == tr.To {
			return fmt.Errorf("%w: transition %q moves nowhere", ErrLifecycleInvalid, tr.Name)
		}
	}
	return nil
}

// find returns the declared transition by name.
func (l Lifecycle) find(name string) (Transition, bool) {
	for _, tr := range l.Transitions {
		if tr.Name == name {
			return tr, true
		}
	}
	return Transition{}, false
}

// Actor attempts a transition under these roles.
type Actor struct {
	ID    string
	Roles []string
}

func (a Actor) hasRole(roles []string) bool {
	for _, need := range roles {
		for _, have := range a.Roles {
			if have == need {
				return true
			}
		}
	}
	return false
}

// AccessChecker evaluates the transition's named access rule.
type AccessChecker interface {
	CheckAccess(rule string, actor Actor, rec Record) error
}

// ApprovalChecker proves the transition holds its required approval.
type ApprovalChecker interface {
	CheckApproved(transition string, actor Actor, rec Record) error
}

// WorkflowStarter starts the transition's pinned workflow, returning the
// run id.
type WorkflowStarter interface {
	StartWorkflow(pinnedID string, actor Actor, rec Record) (string, error)
}

// Deps bundles the guard ports a transition evaluates.
type Deps struct {
	Access   AccessChecker
	Approval ApprovalChecker
	Starter  WorkflowStarter
}

// Record is one governed lifecycle record. The state is privately held:
// ApplyTransition is the only mutator, and UpdateData cannot move it.
type Record struct {
	ID      string
	TypeID  string
	state   State
	Version int
	Data    map[string]string
}

// NewRecord creates a record in the lifecycle's initial state.
func NewRecord(l Lifecycle, id string, data map[string]string) (Record, error) {
	if err := l.Validate(); err != nil {
		return Record{}, err
	}
	if strings.TrimSpace(id) == "" {
		return Record{}, fmt.Errorf("%w: record id is required", ErrLifecycleInvalid)
	}
	cp := make(map[string]string, len(data))
	for k, v := range data {
		cp[k] = v
	}
	return Record{ID: id, TypeID: l.TypeID, state: l.Initial, Data: cp}, nil
}

// State returns the record's current state.
func (r Record) State() State { return r.state }

// UpdateData is the generic update path: it merges data and bumps the
// version, and never moves state — not even when the patch names one.
func UpdateData(rec Record, patch map[string]string) Record {
	out := rec
	out.Data = make(map[string]string, len(rec.Data)+len(patch))
	for k, v := range rec.Data {
		out.Data[k] = v
	}
	for k, v := range patch {
		out.Data[k] = v
	}
	out.Version++
	return out
}

// TransitionEvidence is the governed record of one applied transition.
type TransitionEvidence struct {
	RecordID      string
	TypeID        string
	Transition    string
	From          State
	To            State
	Actor         string
	Version       int
	WorkflowRunID string
	At            time.Time
}

// ApplyTransition applies one declared transition: validate, check every
// guard, start the pinned workflow, apply, record evidence. Any failure
// changes nothing.
func ApplyTransition(l Lifecycle, rec Record, name string, actor Actor, deps Deps, at time.Time) (Record, TransitionEvidence, error) {
	if err := l.Validate(); err != nil {
		return rec, TransitionEvidence{}, err
	}
	if rec.TypeID != l.TypeID {
		return rec, TransitionEvidence{}, fmt.Errorf("%w: record of %q is not a %q", ErrLifecycleInvalid, rec.TypeID, l.TypeID)
	}
	tr, ok := l.find(name)
	if !ok {
		return rec, TransitionEvidence{}, fmt.Errorf("%w: %q", ErrUndeclaredTransition, name)
	}
	if rec.state != tr.From {
		return rec, TransitionEvidence{}, fmt.Errorf("%w: %q from %s", ErrBadState, name, rec.state)
	}
	if len(tr.Guard.Roles) > 0 && !actor.hasRole(tr.Guard.Roles) {
		return rec, TransitionEvidence{}, fmt.Errorf("%w: role", ErrGuardDenied)
	}
	if tr.Guard.AccessRule != "" {
		if deps.Access == nil {
			return rec, TransitionEvidence{}, fmt.Errorf("%w: no access checker", ErrGuardDenied)
		}
		if err := deps.Access.CheckAccess(tr.Guard.AccessRule, actor, rec); err != nil {
			return rec, TransitionEvidence{}, fmt.Errorf("%w: access: %v", ErrGuardDenied, err)
		}
	}
	if tr.Guard.RequireApproval {
		if deps.Approval == nil {
			return rec, TransitionEvidence{}, fmt.Errorf("%w: no approval checker", ErrGuardDenied)
		}
		if err := deps.Approval.CheckApproved(name, actor, rec); err != nil {
			return rec, TransitionEvidence{}, fmt.Errorf("%w: approval: %v", ErrGuardDenied, err)
		}
	}
	runID := ""
	if tr.Guard.StartWorkflow != "" {
		if deps.Starter == nil {
			return rec, TransitionEvidence{}, fmt.Errorf("%w: no workflow starter", ErrGuardDenied)
		}
		var err error
		runID, err = deps.Starter.StartWorkflow(tr.Guard.StartWorkflow, actor, rec)
		if err != nil {
			return rec, TransitionEvidence{}, fmt.Errorf("%w: start %s: %v", ErrGuardDenied, tr.Guard.StartWorkflow, err)
		}
	}
	out := rec
	out.state = tr.To
	out.Version++
	ev := TransitionEvidence{
		RecordID: rec.ID, TypeID: rec.TypeID, Transition: name,
		From: tr.From, To: tr.To, Actor: actor.ID,
		Version: out.Version, WorkflowRunID: runID, At: at.UTC(),
	}
	return out, ev, nil
}
