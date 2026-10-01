// Package incidentrepair owns the ADMIN-005 operations-center contract.
// Adapters provide authoritative incidents and execute plans; this package
// only validates transitions, authority, evidence, and safe disclosure.
package incidentrepair

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const RegistryID = "ADMIN-005"
const ContractVersion = "hcmnext.admincenter.incident-repair/1"

type State string

const (
	StateDetected      State = "DETECTED"
	StateTriaged       State = "TRIAGED"
	StateDeclared      State = "DECLARED"
	StateMitigating    State = "MITIGATING"
	StateMonitoring    State = "MONITORING"
	StateResolved      State = "RESOLVED"
	StateReviewed      State = "REVIEWED"
	StateReopened      State = "REOPENED"
	StateFalsePositive State = "FALSE_POSITIVE"
	StateMerged        State = "MERGED"
	StateSplit         State = "SPLIT"
	StateDuplicate     State = "DUPLICATE"
)

type Action string

const (
	ActionInspect   Action = "inspect"
	ActionTest      Action = "test"
	ActionRedrive   Action = "redrive"
	ActionReconcile Action = "reconcile"
	ActionDiff      Action = "diff"
	ActionSimulate  Action = "simulate"
	ActionPromote   Action = "promote"
	ActionRollback  Action = "rollback"
	ActionReopen    Action = "reopen"
	ActionMerge     Action = "merge"
	ActionSplit     Action = "split"
)

var actionOrder = []Action{ActionInspect, ActionTest, ActionRedrive, ActionReconcile, ActionDiff, ActionSimulate, ActionPromote, ActionRollback, ActionReopen, ActionMerge, ActionSplit}

func Actions() []Action { return append([]Action(nil), actionOrder...) }
func (a Action) Valid() bool {
	for _, x := range actionOrder {
		if a == x {
			return true
		}
	}
	return false
}

type RegistryEntry struct {
	ID, Version                    string
	Actions                        []Action
	RedactionRequired, RequiresSoD bool
}

func Registry() RegistryEntry {
	return RegistryEntry{RegistryID, ContractVersion, Actions(), true, true}
}

type Evidence struct {
	ID, Kind, Summary, TenantID, Compartment string
	ObservedAt                               time.Time
}
type TimelineEvent struct {
	ID, Type, Summary, TenantID, Compartment string
	At                                       time.Time
	EvidenceID                               string
}
type Incident struct {
	ID, TenantID, Service string
	State                 State
	Version               uint64
	Severity              string
	AffectedKnown         bool
	Evidence              []Evidence
	Timeline              []TimelineEvent
}
type Authorization struct {
	ActorID, TenantID, Role string
	AllowedActions          []Action
	ApproverID              string
	CanCustomerView         bool
}

func (a Authorization) Allows(x Action) bool {
	for _, y := range a.AllowedActions {
		if x == y {
			return true
		}
	}
	return false
}

type ActionRequest struct {
	Action                 Action
	Incident               Incident
	Authorization          Authorization
	ExpectedVersion        uint64
	Reason, IdempotencyKey string
	ChildIDs               []string
}
type ActionPlan struct {
	Action                    Action
	IncidentID                string
	From, To                  State
	Version                   uint64
	RequiresApproval, Mutates bool
	Evidence                  []Evidence
	IdempotencyKey            string
}
type CenterView struct {
	Incidents   []IncidentView
	GeneratedAt time.Time
	Stale       bool
}
type IncidentView struct {
	Incident Incident
	Timeline []TimelineEvent
	Actions  []Action
	Withheld bool
}

var (
	ErrInvalidAction     = errors.New("incident repair: invalid action")
	ErrUnauthorized      = errors.New("incident repair: unauthorized")
	ErrSelfApproval      = errors.New("incident repair: segregation of duties violation")
	ErrStale             = errors.New("incident repair: stale action plan")
	ErrInvalidTransition = errors.New("incident repair: invalid lifecycle transition")
	ErrEvidenceRequired  = errors.New("incident repair: evidence is required")
	ErrUnsafeRecovery    = errors.New("incident repair: unsafe recovery")
)

func mutates(a Action) bool {
	return a == ActionRedrive || a == ActionReconcile || a == ActionPromote || a == ActionRollback || a == ActionReopen || a == ActionMerge || a == ActionSplit
}
func needsApproval(a Action) bool {
	return a == ActionPromote || a == ActionRollback || a == ActionMerge || a == ActionSplit || a == ActionReopen
}
func target(s State, a Action) State {
	switch a {
	case ActionReopen:
		return StateReopened
	case ActionMerge:
		return StateMerged
	case ActionSplit:
		return StateSplit
	}
	return s
}
func transitionOK(s State, a Action) bool {
	if a == ActionInspect || a == ActionTest || a == ActionDiff || a == ActionSimulate || a == ActionReconcile || a == ActionRedrive || a == ActionPromote || a == ActionRollback {
		return s != StateReviewed && s != StateFalsePositive && s != StateMerged && s != StateSplit
	}
	switch a {
	case ActionReopen:
		return s == StateResolved || s == StateReviewed
	case ActionMerge:
		return s == StateDeclared || s == StateMitigating || s == StateMonitoring
	case ActionSplit:
		return s == StateDeclared || s == StateMitigating || s == StateMonitoring
	}
	return false
}
func Plan(r ActionRequest) (ActionPlan, error) {
	if !r.Action.Valid() {
		return ActionPlan{}, ErrInvalidAction
	}
	if r.Incident.ID == "" || r.Incident.TenantID == "" {
		return ActionPlan{}, ErrInvalidTransition
	}
	if r.Authorization.TenantID != r.Incident.TenantID || !r.Authorization.Allows(r.Action) {
		return ActionPlan{}, ErrUnauthorized
	}
	if r.ExpectedVersion != 0 && r.ExpectedVersion != r.Incident.Version {
		return ActionPlan{}, fmt.Errorf("%w: expected %d got %d", ErrStale, r.ExpectedVersion, r.Incident.Version)
	}
	if !transitionOK(r.Incident.State, r.Action) {
		return ActionPlan{}, ErrInvalidTransition
	}
	if strings.TrimSpace(r.IdempotencyKey) == "" {
		return ActionPlan{}, errors.New("incident repair: idempotency key required")
	}
	if mutates(r.Action) && strings.TrimSpace(r.Reason) == "" {
		return ActionPlan{}, errors.New("incident repair: reason required")
	}
	if mutates(r.Action) && len(r.Incident.Evidence) == 0 {
		return ActionPlan{}, ErrEvidenceRequired
	}
	if needsApproval(r.Action) && (r.Authorization.ApproverID == "" || r.Authorization.ApproverID == r.Authorization.ActorID) {
		return ActionPlan{}, ErrSelfApproval
	}
	if r.Action == ActionRedrive && r.Incident.State == StateResolved {
		return ActionPlan{}, ErrUnsafeRecovery
	}
	return ActionPlan{Action: r.Action, IncidentID: r.Incident.ID, From: r.Incident.State, To: target(r.Incident.State, r.Action), Version: r.Incident.Version, RequiresApproval: needsApproval(r.Action), Mutates: mutates(r.Action), Evidence: append([]Evidence(nil), r.Incident.Evidence...), IdempotencyKey: r.IdempotencyKey}, nil
}

// View redacts customer-unsafe timeline entries and never claims a complete
// affected population unless the authoritative affected set was verified.
func View(incidents []Incident, auth Authorization, now time.Time) CenterView {
	out := CenterView{GeneratedAt: now}
	for _, in := range incidents {
		v := IncidentView{Incident: in}
		if in.TenantID != auth.TenantID {
			v.Withheld = true
			v.Incident = Incident{ID: in.ID, State: in.State}
			out.Incidents = append(out.Incidents, v)
			continue
		}
		if !auth.CanCustomerView {
			v.Withheld = true
			v.Incident = Incident{ID: in.ID, TenantID: in.TenantID, State: in.State, Version: in.Version}
			out.Incidents = append(out.Incidents, v)
			continue
		}
		v.Incident.Evidence = customerEvidence(in)
		v.Incident.Timeline = customerTimeline(in)
		for _, e := range in.Timeline {
			if customerVisible(in.TenantID, e.TenantID, e.Compartment) {
				v.Timeline = append(v.Timeline, e)
			}
		}
		for _, a := range Actions() {
			if auth.Allows(a) {
				v.Actions = append(v.Actions, a)
			}
		}
		out.Incidents = append(out.Incidents, v)
	}
	sort.SliceStable(out.Incidents, func(i, j int) bool { return out.Incidents[i].Incident.ID < out.Incidents[j].Incident.ID })
	return out
}

func customerEvidence(in Incident) []Evidence {
	var out []Evidence
	for _, e := range in.Evidence {
		if customerVisible(in.TenantID, e.TenantID, e.Compartment) {
			out = append(out, e)
		}
	}
	return out
}

func customerTimeline(in Incident) []TimelineEvent {
	var out []TimelineEvent
	for _, e := range in.Timeline {
		if customerVisible(in.TenantID, e.TenantID, e.Compartment) {
			out = append(out, e)
		}
	}
	return out
}

func customerVisible(incidentTenant, entryTenant, compartment string) bool {
	return entryTenant == incidentTenant && (compartment == "" || compartment == "customer")
}
