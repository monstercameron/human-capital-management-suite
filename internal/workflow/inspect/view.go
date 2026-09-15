package inspect

import (
	"encoding/json"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// Ref is one reference a view renders, together with why it does or does not
// carry a value.
//
// It wraps values.Presence[string] rather than using a bare string because the
// two failure modes WF-RUN-019 forbids are not distinguishable in a string: a
// reference that was never recorded and a reference the caller may not see
// both render as "". A Ref says which, in the state, and never carries the
// value in the redacted case — the value is not stored in the presence at all.
type Ref struct {
	presence values.Presence[string]
}

// RefValue builds a Ref carrying a reference, or an absent Ref for "".
func RefValue(s string) Ref {
	if s == "" {
		return Ref{presence: values.Absent[string]()}
	}
	return Ref{presence: values.Value(s)}
}

// RefAbsent builds a Ref for a reference nothing recorded.
func RefAbsent() Ref { return Ref{presence: values.Absent[string]()} }

// RefRedacted builds a Ref for a reference the caller may not see. The value
// is not stored, so it cannot reach a log or an error message by accident.
func RefRedacted(reason string) Ref { return Ref{presence: values.Redacted[string](reason)} }

// State returns the presence state.
func (r Ref) State() values.PresenceState { return r.presence.State() }

// Get returns the reference and whether one is readable.
func (r Ref) Get() (string, bool) { return r.presence.Get() }

// Reason returns the policy token recorded for a non-value state.
func (r Ref) Reason() string { return r.presence.Reason() }

// IsRedacted reports whether the reference was withheld.
func (r Ref) IsRedacted() bool { return r.presence.State() == values.PresenceRedacted }

// refWire is the rendered JSON shape of a Ref.
type refWire struct {
	State  string `json:"state"`
	Value  string `json:"value,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// MarshalJSON renders the state and, only for a VALUE, the reference itself.
func (r Ref) MarshalJSON() ([]byte, error) {
	w := refWire{State: r.presence.State().String()}
	if v, ok := r.presence.Get(); ok {
		w.Value = v
	} else {
		w.Reason = r.presence.Reason()
	}
	return json.Marshal(w)
}

// RefList is a list of references rendered under one presence, so a denied
// list is withheld as a list rather than silently rendered empty.
type RefList struct {
	State  values.PresenceState `json:"state"`
	Values []string             `json:"values,omitempty"`
	Reason string               `json:"reason,omitempty"`
}

// RefListValue builds a readable list. A nil slice renders as an empty list in
// the VALUE state, which says "nothing was recorded", not "you may not look".
func RefListValue(v []string) RefList {
	out := append([]string(nil), v...)
	if out == nil {
		out = []string{}
	}
	return RefList{State: values.PresenceValue, Values: out}
}

// RefListRedacted builds a withheld list.
func RefListRedacted(reason string) RefList {
	return RefList{State: values.PresenceRedacted, Reason: reason}
}

// MarshalJSON renders the state token rather than the numeric presence value.
func (l RefList) MarshalJSON() ([]byte, error) {
	type wire struct {
		State  string   `json:"state"`
		Values []string `json:"values,omitempty"`
		Reason string   `json:"reason,omitempty"`
	}
	return json.Marshal(wire{State: l.State.String(), Values: l.Values, Reason: l.Reason})
}

// DefinitionView is the first traversal stage: what is running.
type DefinitionView struct {
	Disclosed        bool   `json:"disclosed"`
	WorkflowID       string `json:"workflow_id,omitempty"`
	WorkflowVersion  uint32 `json:"workflow_version,omitempty"`
	CompiledPlanHash string `json:"compiled_plan_hash,omitempty"`
	// DeniedReason is the policy token when the stage was withheld.
	DeniedReason string `json:"denied_reason,omitempty"`
}

// Lifecycle is the five-dimension intent state, rendered in full. There is no
// collapsed status field: an instance may be COMPLETED with a DEGRADED
// consistency state, and a single "status" would hide exactly that.
type Lifecycle struct {
	RequestState     string `json:"request_state"`
	ExecutionState   string `json:"execution_state"`
	BusinessState    string `json:"business_state"`
	ConsistencyState string `json:"consistency_state"`
	ObligationState  string `json:"obligation_state"`
}

// InstanceView is the second stage: where execution is now.
type InstanceView struct {
	Disclosed    bool   `json:"disclosed"`
	DeniedReason string `json:"denied_reason,omitempty"`

	InstanceID    string `json:"instance_id,omitempty"`
	CellID        string `json:"cell_id,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
	ExecutionMode string `json:"execution_mode,omitempty"`
	RuntimeStatus string `json:"runtime_status,omitempty"`
	// InstanceVersion is the optimistic token an intervention would have to
	// hold. It is shown because an operator who cannot see it cannot tell a
	// stuck instance from one that is being written to constantly.
	InstanceVersion      int64     `json:"instance_version,omitempty"`
	VariableRevisionHead int64     `json:"variable_revision_head"`
	Lifecycle            Lifecycle `json:"lifecycle"`

	InputRef            Ref     `json:"input_ref"`
	EffectiveContextRef Ref     `json:"effective_context_ref"`
	LastCheckpointRef   Ref     `json:"last_checkpoint_ref"`
	BusinessSubjectRefs RefList `json:"business_subject_refs"`

	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// FrontierEntry is one node the instance is currently at, with the attempt
// that is standing there.
//
// AttemptRecorded is false when the frontier names a node no execution row
// covers. That is a gap, not a blank: it is reported in [Completeness.Gaps]
// too, because a frontier the inspector cannot explain is the single most
// important thing an operator needs to be told.
type FrontierEntry struct {
	NodeID          string `json:"node_id"`
	AttemptRecorded bool   `json:"attempt_recorded"`
	Attempt         int    `json:"attempt,omitempty"`
	Status          string `json:"status,omitempty"`
	StepType        string `json:"step_type,omitempty"`
}

// GovernanceView is the fourth stage: the decisions behind one node.
type GovernanceView struct {
	Disclosed               bool   `json:"disclosed"`
	DeniedReason            string `json:"denied_reason,omitempty"`
	AuthorizationDecisionID Ref    `json:"authorization_decision_id"`
	DecisionID              Ref    `json:"decision_id"`
	PolicyRef               Ref    `json:"policy_ref"`
	ProposalRef             Ref    `json:"proposal_ref"`
	BaselineRef             Ref    `json:"baseline_ref"`
	HumanTaskID             Ref    `json:"human_task_id"`
	AgentExecutionID        Ref    `json:"agent_execution_id"`
}

// TransactionView is the fifth stage: the business transaction the node's work
// belongs to.
type TransactionView struct {
	Disclosed             bool   `json:"disclosed"`
	DeniedReason          string `json:"denied_reason,omitempty"`
	BusinessTransactionID Ref    `json:"business_transaction_id"`
}

// ConnectorView is the sixth stage: what left the boundary.
type ConnectorView struct {
	Disclosed             bool    `json:"disclosed"`
	DeniedReason          string  `json:"denied_reason,omitempty"`
	CapabilityExecutionID Ref     `json:"capability_execution_id"`
	EffectRefs            RefList `json:"effect_refs"`
}

// ObservationView is the seventh stage: what came back, and what it obliges.
type ObservationView struct {
	Disclosed    bool   `json:"disclosed"`
	DeniedReason string `json:"denied_reason,omitempty"`
	ErrorClass   Ref    `json:"error_class"`
	RepairRef    Ref    `json:"repair_ref"`
}

// TraceView is the eighth stage: the telemetry correlation, which explains
// software behavior and is never business evidence.
type TraceView struct {
	Disclosed    bool   `json:"disclosed"`
	DeniedReason string `json:"denied_reason,omitempty"`
	TraceID      Ref    `json:"trace_id"`
}

// NodeView is the third stage for one attempt, carrying the four stages that
// hang off it.
type NodeView struct {
	NodeID   string `json:"node_id"`
	Attempt  int    `json:"attempt"`
	StepType string `json:"step_type"`
	Status   string `json:"status"`
	// Current reports whether this attempt is on the instance's frontier.
	Current bool `json:"current"`
	// RetryPolicyRef is the retry policy the compiled plan declared for this
	// node. It is a policy reference, not a scheduled retry: the scheduled
	// retry is a durable RETRY_BACKOFF timer, which [Load] renders in
	// [DurableView.Attempts] from the timer rows themselves.
	RetryPolicyRef Ref `json:"retry_policy_ref"`

	InputSnapshotRef  Ref `json:"input_snapshot_ref"`
	OutputArtifactRef Ref `json:"output_artifact_ref"`

	Governance  GovernanceView  `json:"governance"`
	Transaction TransactionView `json:"transaction"`
	Connector   ConnectorView   `json:"connector"`
	Observation ObservationView `json:"observation"`
	Trace       TraceView       `json:"trace"`

	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	RecordedAt  time.Time  `json:"recorded_at"`
}

// Completeness states what the view does not say, and why.
//
// Complete is true only when nothing was denied and nothing was missing. It is
// what stops a partial view being read as a full one, which is the failure
// WF-RUN-019's RED clause is about.
type Completeness struct {
	Complete bool `json:"complete"`
	// Redactions names every stage and field withheld from this caller,
	// sorted.
	Redactions []string `json:"redactions"`
	// Gaps names every datum the projection expected and did not receive,
	// sorted. A frontier node with no recorded execution is the main one.
	Gaps []string `json:"gaps"`
}

// View is the whole rendered inspection.
//
// Traversal is the declared stage order, and every stage appears whether or
// not it was disclosed: a denied stage renders with Disclosed false and a
// policy token, never as a missing field.
type View struct {
	PolicyVersion string    `json:"policy_version"`
	Purpose       string    `json:"purpose"`
	Subject       string    `json:"subject,omitempty"`
	Traversal     []Section `json:"traversal"`

	Definition DefinitionView  `json:"definition"`
	Instance   InstanceView    `json:"instance"`
	Frontier   []FrontierEntry `json:"frontier"`
	Nodes      []NodeView      `json:"nodes"`

	Completeness Completeness `json:"completeness"`
}

// FrontierNodeIDs returns the node ids the instance is currently at.
func (v View) FrontierNodeIDs() []string {
	out := make([]string, 0, len(v.Frontier))
	for _, f := range v.Frontier {
		out = append(out, f.NodeID)
	}
	return out
}

// Node returns the rendered attempt for a node id, preferring the highest
// attempt, and whether one was rendered.
func (v View) Node(nodeID string) (NodeView, bool) {
	var (
		found NodeView
		ok    bool
	)
	for _, n := range v.Nodes {
		if n.NodeID == nodeID && (!ok || n.Attempt > found.Attempt) {
			found, ok = n, true
		}
	}
	return found, ok
}

// JSON renders the view as indented, deterministic JSON: struct field order is
// declaration order, every slice is emitted in the order Build produced it,
// and Build sorts the two slices whose order is not otherwise defined.
func (v View) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// dimensionsOf renders the stored dimensions, substituting the explicit
// UNSPECIFIED token for a dimension nothing has recorded yet rather than an
// empty string that would read as a missing field.
func dimensionsOf(d runtime.Dimensions) Lifecycle {
	unspecified := func(s string) string {
		if s == "" {
			return "UNSPECIFIED"
		}
		return s
	}
	return Lifecycle{
		RequestState:     unspecified(d.RequestState),
		ExecutionState:   unspecified(d.ExecutionState),
		BusinessState:    unspecified(d.BusinessState),
		ConsistencyState: unspecified(d.ConsistencyState),
		ObligationState:  unspecified(d.ObligationState),
	}
}
