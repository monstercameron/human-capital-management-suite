// Package contract defines the renderer-independent Promotion workspace
// contract consumed by product composition and renderers.
//
// The contract is a plain Go value: no GoWebComponents type, no
// html/template type, and does not perform authorization or resolve business
// data. Callers construct it only after visibility has been evaluated.
//
// Field masking is applied by the caller BEFORE constructing the contract:
// NewWorkspaceContract and its field/action filters drop anything the
// caller's FieldVisibility/ActionVisibility does not name. A masked field or
// action is therefore absent from the resulting Go value entirely -- it
// never appears as a zero-valued or blanked struct field, and it never
// round-trips through JSON, the SSR renderer, or the GWC renderer. That is
// the property UX-001's RED case and UX-QUAL-001's Security test both check:
// "client receives unauthorized field/action" / "renders a field the server
// masked".
package contract

import "time"

// FieldKind names the input semantics of a RequestField so a renderer can
// pick the right control without inventing business rules of its own.
type FieldKind string

const (
	FieldKindText     FieldKind = "text"
	FieldKindTextarea FieldKind = "textarea"
	FieldKindDate     FieldKind = "date"
	FieldKindMoney    FieldKind = "money"
	FieldKindLookup   FieldKind = "lookup"
	FieldKindReadOnly FieldKind = "readonly"
)

// Severity names a preflight finding's or simulation check's severity.
type Severity string

const (
	SeverityBlocking Severity = "blocking"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
	SeveritySuccess  Severity = "success"
)

// SimulationStatus is the overall state of the deterministic simulation run.
type SimulationStatus string

const (
	SimulationPending     SimulationStatus = "pending"
	SimulationReady       SimulationStatus = "ready"
	SimulationNeedsReview SimulationStatus = "needs_review"
	SimulationFailed      SimulationStatus = "failed"
)

// ActionVariant is a rendering hint only; it never changes what the action
// does or whether it is available.
type ActionVariant string

const (
	ActionPrimary   ActionVariant = "primary"
	ActionSecondary ActionVariant = "secondary"
	ActionDanger    ActionVariant = "danger"
)

// FieldValidation is validation metadata the server already evaluated; the
// renderer surfaces it, it never invents or re-derives it.
type FieldValidation struct {
	Required bool
	Message  string // non-empty only when the field currently fails validation
}

// RequestField is one field of the Promotion request, already masked and
// already formatted for display (e.g. money is a pre-formatted string, not a
// raw ledger amount) by the server before this type is constructed.
type RequestField struct {
	ID         string
	Label      string
	Kind       FieldKind
	Value      string
	Validation FieldValidation
}

// PromotionRequest is the worker-identifying header plus the ordered,
// already-masked field list. Field order is significant: both renderers
// must emit fields in this order, because DOM order is how UX-QUAL-001's
// keyboard-only completion criterion derives tab order.
type PromotionRequest struct {
	WorkerID   string
	WorkerName string
	Fields     []RequestField
}

// PreflightFinding is one pre-submission eligibility/policy finding. Labels
// and detail text are server-composed and must never embed a masked field's
// raw value.
type PreflightFinding struct {
	ID       string
	Severity Severity
	Label    string
	Detail   string
}

// SimulationCheck is one line item of the deterministic compensation/
// position simulation.
type SimulationCheck struct {
	Label  string
	Status Severity
	Detail string
}

// SimulationResult is the outcome of the deterministic simulation run that
// must complete before a write is released.
type SimulationResult struct {
	Status      SimulationStatus
	Summary     string
	GeneratedAt time.Time
	Checks      []SimulationCheck
}

// TimelineEvent is one ledger-derived entry in the approval timeline.
type TimelineEvent struct {
	At    time.Time
	Actor string
	Label string
}

// AvailableAction is one semantic action the current actor is authorized to
// take from the current workflow state. Actions absent from this list must
// not be offered by either renderer.
type AvailableAction struct {
	ID             string
	Label          string
	Transition     string
	Variant        ActionVariant
	RequiresReason bool
}

// Provenance identifies where the contract's data came from and which
// capability/version produced it, so the workspace never has to guess at
// business meaning.
type Provenance struct {
	CapabilityID      string
	CapabilityVersion string
	SourceSystem      string
	AsOf              time.Time
}

// WorkspaceContract is the full renderer-independent Promotion workspace
// payload: request, preflight findings, simulation result, and approval
// timeline, plus the available actions and provenance. Every renderer
// (GWC or the Go SSR fallback) consumes exactly this type and nothing else.
type WorkspaceContract struct {
	WorkspaceID string
	Title       string
	Request     PromotionRequest
	Preflight   []PreflightFinding
	Simulation  SimulationResult
	Timeline    []TimelineEvent
	Actions     []AvailableAction
	Provenance  Provenance
}

// SourceRecord is the full, unmasked record a capability resolves before
// authorization. It never leaves the server process: NewWorkspaceContract
// is the only supported way to turn it into a WorkspaceContract, and doing
// so always applies FieldVisibility and ActionVisibility first.
type SourceRecord struct {
	WorkspaceID string
	Title       string
	WorkerID    string
	WorkerName  string
	AllFields   []RequestField
	Preflight   []PreflightFinding
	Simulation  SimulationResult
	Timeline    []TimelineEvent
	AllActions  []AvailableAction
	Provenance  Provenance
}

// FieldVisibility names the field IDs the current actor/context is
// authorized to see. Anything not present (or present with a false value)
// is masked: NewWorkspaceContract omits it from PromotionRequest.Fields
// entirely rather than blanking its Value.
type FieldVisibility map[string]bool

// ActionVisibility names the action IDs the current actor/context is
// authorized to invoke from the current workflow state. Anything not
// present (or false) is masked the same way fields are.
type ActionVisibility map[string]bool

// Allow builds a FieldVisibility/ActionVisibility that permits exactly the
// given IDs. It is a convenience for tests and callers building an allowlist
// from a policy decision.
func Allow(ids ...string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// NewWorkspaceContract projects a SourceRecord into a WorkspaceContract,
// keeping only the fields and actions the visibility maps allow. Order is
// preserved from SourceRecord.AllFields/AllActions so masking never
// reorders what remains.
//
// Preflight, Simulation, and Timeline are carried through unfiltered: those
// are server-composed narrative/audit content that must not name a masked
// field's raw value in the first place (that invariant is a server-side
// authoring rule enforced by tests in this package and by
// UX-QUAL-001's Security test, not by field-level filtering here).
func NewWorkspaceContract(source SourceRecord, fields FieldVisibility, actions ActionVisibility) WorkspaceContract {
	visibleFields := make([]RequestField, 0, len(source.AllFields))
	for _, f := range source.AllFields {
		if fields[f.ID] {
			visibleFields = append(visibleFields, f)
		}
	}

	visibleActions := make([]AvailableAction, 0, len(source.AllActions))
	for _, a := range source.AllActions {
		if actions[a.ID] {
			visibleActions = append(visibleActions, a)
		}
	}

	return WorkspaceContract{
		WorkspaceID: source.WorkspaceID,
		Title:       source.Title,
		Request: PromotionRequest{
			WorkerID:   source.WorkerID,
			WorkerName: source.WorkerName,
			Fields:     visibleFields,
		},
		Preflight:  source.Preflight,
		Simulation: source.Simulation,
		Timeline:   source.Timeline,
		Actions:    visibleActions,
		Provenance: source.Provenance,
	}
}

// MaskedFieldIDs returns the AllFields IDs that NewWorkspaceContract would
// drop for the given visibility map. Tests use this to know which
// identifiers/values must never appear in rendered output.
func MaskedFieldIDs(source SourceRecord, fields FieldVisibility) []string {
	var out []string
	for _, f := range source.AllFields {
		if !fields[f.ID] {
			out = append(out, f.ID)
		}
	}
	return out
}
