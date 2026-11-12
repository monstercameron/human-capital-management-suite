package connectorconfig

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
)

type Field string

const (
	FieldStatus        Field = "status"
	FieldHealth        Field = "health"
	FieldConfiguration Field = "configuration"
	FieldEvidence      Field = "evidence"
)

// Decision is the already-evaluated disclosure decision supplied by the
// caller. A missing field ruling is a denial; secrets are never a field.
type Decision struct {
	SubjectDisclosable bool
	Fields             map[Field]bool
}

func (d *Decision) allows(f Field) bool { return d == nil || (d.SubjectDisclosable && d.Fields[f]) }

type Scope struct{ TenantID, Environment string }
type EvidenceRef struct{ ID, Kind string }

type StatusView struct {
	ConnectionID, TenantID, ConnectorID, ConnectorVersion, Environment, State string
	StateVersion                                                              uint64
	CredentialRef                                                             string // always the stable reference, never its secret material
	Scopes                                                                    []string
	EvidenceRefs                                                              []EvidenceRef
	Withheld                                                                  bool
}

// BuildStatus renders only the connection metadata that is safe for the
// operations center. CredentialRef is an opaque reference by contract.
func BuildStatus(c *connectivity.ConnectorConnection, scope Scope, d *Decision, evidence []EvidenceRef) (StatusView, error) {
	if c == nil {
		return StatusView{}, errors.New("connectorconfig: connection is required")
	}
	if scope.TenantID != "" && c.TenantID() != scope.TenantID {
		return StatusView{}, errors.New("connectorconfig: tenant scope mismatch")
	}
	if scope.Environment != "" && string(c.Environment()) != scope.Environment {
		return StatusView{}, errors.New("connectorconfig: environment scope mismatch")
	}
	v := StatusView{ConnectionID: c.ID(), TenantID: c.TenantID(), ConnectorID: c.ConnectorID(), ConnectorVersion: c.ConnectorVersion().String(), Environment: string(c.Environment()), State: string(c.State()), StateVersion: c.StateVersion()}
	if d != nil && !d.SubjectDisclosable {
		v.Withheld = true
		v.TenantID = ""
		v.ConnectorID = ""
		v.ConnectorVersion = ""
		v.Environment = ""
		v.State = "UNKNOWN"
		v.StateVersion = 0
		v.CredentialRef = ""
		return v, nil
	}
	if d == nil || d.allows(FieldConfiguration) {
		v.Scopes = c.Scopes()
		v.CredentialRef = c.CredentialRef().String()
	}
	if d != nil && !d.allows(FieldStatus) {
		v.State = "UNKNOWN"
		v.StateVersion = 0
	}
	if d == nil || d.allows(FieldEvidence) {
		v.EvidenceRefs = cloneEvidence(evidence)
	}
	return v, nil
}

func cloneEvidence(in []EvidenceRef) []EvidenceRef { return append([]EvidenceRef(nil), in...) }

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
)

var actionOrder = []Action{ActionInspect, ActionTest, ActionRedrive, ActionReconcile, ActionDiff, ActionSimulate, ActionPromote, ActionRollback}

func Actions() []Action { return append([]Action(nil), actionOrder...) }
func (a Action) Valid() bool {
	for _, x := range actionOrder {
		if a == x {
			return true
		}
	}
	return false
}

type ActionRequest struct {
	Action                                    Action
	Scope                                     Scope
	TenantID                                  string
	Environment                               string
	Production                                bool
	ParentWorkflowID                          string
	OperationID                               string
	ExpectedStateVersion                      uint64
	CurrentStateVersion                       uint64
	ExpectedConfigDigest, CurrentConfigDigest string
	EvidenceRefs                              []EvidenceRef
	Authorized                                bool
}
type ActionPlan struct {
	Action           Action
	ConnectionID     string
	EvidenceRefs     []EvidenceRef
	RequiresApproval bool
	Mutates          bool
}

var (
	ErrInvalidAction    = errors.New("connectorconfig: invalid action")
	ErrUnauthorized     = errors.New("connectorconfig: action is not authorized")
	ErrProductionTest   = errors.New("connectorconfig: production connector tests are prohibited")
	ErrParentRerun      = errors.New("connectorconfig: parent workflow rerun is prohibited")
	ErrStaleConfig      = errors.New("connectorconfig: stale configuration activation")
	ErrEvidenceRequired = errors.New("connectorconfig: evidence references are required")
)

// PlanAction validates the safety boundary and returns a description only;
// callers must hand the plan to the governed runtime to create effects.
func PlanAction(r ActionRequest) (ActionPlan, error) {
	if !r.Action.Valid() {
		return ActionPlan{}, ErrInvalidAction
	}
	if !r.Authorized {
		return ActionPlan{}, ErrUnauthorized
	}
	if r.Action == ActionTest && (r.Production || strings.EqualFold(r.Environment, "production") || strings.EqualFold(r.Scope.Environment, "production")) {
		return ActionPlan{}, ErrProductionTest
	}
	if r.ParentWorkflowID != "" && r.Action == ActionRedrive {
		return ActionPlan{}, ErrParentRerun
	}
	if r.ExpectedStateVersion != 0 && r.CurrentStateVersion != r.ExpectedStateVersion {
		return ActionPlan{}, fmt.Errorf("%w: expected state version %d, got %d", ErrStaleConfig, r.ExpectedStateVersion, r.CurrentStateVersion)
	}
	if r.ExpectedConfigDigest != "" && r.CurrentConfigDigest != r.ExpectedConfigDigest {
		return ActionPlan{}, fmt.Errorf("%w: expected digest %q, got %q", ErrStaleConfig, r.ExpectedConfigDigest, r.CurrentConfigDigest)
	}
	if len(r.EvidenceRefs) == 0 {
		return ActionPlan{}, ErrEvidenceRequired
	}
	p := ActionPlan{Action: r.Action, EvidenceRefs: cloneEvidence(r.EvidenceRefs), RequiresApproval: r.Action == ActionPromote || r.Action == ActionRollback, Mutates: r.Action == ActionRedrive || r.Action == ActionReconcile || r.Action == ActionPromote || r.Action == ActionRollback}
	return p, nil
}

// Registry returns the canonical operation-center action registry.
func Registry() map[Action]struct{} {
	m := make(map[Action]struct{}, len(actionOrder))
	for _, a := range actionOrder {
		m[a] = struct{}{}
	}
	return m
}
func SortedActions(m map[Action]struct{}) []Action {
	out := make([]Action, 0, len(m))
	for a := range m {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
