// Package intervention defines the finite, governed workflow intervention
// vocabulary. It evaluates requests against a compiled plan and returns an
// immutable receipt; runtime state changes are owned by internal/workflow/runtime.
package intervention

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

const (
	contractVersion = 1
	receiptProfile  = "hcmnext.workflow.intervention.Receipt/v1"
	eventProfile    = "hcmnext.workflow.intervention.Event/v1"
)

// Version reports this package's contract version.
func Version() int { return contractVersion }

// Kind is the closed set of supported workflow interventions. There is no
// custom or free-form kind.
type Kind string

const (
	Pause                     Kind = "PAUSE"
	Resume                    Kind = "RESUME"
	SkipStep                  Kind = "SKIP_STEP"
	RetryStep                 Kind = "RETRY_STEP"
	Reassign                  Kind = "REASSIGN"
	Cancel                    Kind = "CANCEL"
	ForceCompleteWithEvidence Kind = "FORCE_COMPLETE_WITH_EVIDENCE"
)

// Verb-style aliases keep the wire names readable at call sites.
const (
	KindPause                     = Pause
	KindResume                    = Resume
	KindSkipStep                  = SkipStep
	KindRetryStep                 = RetryStep
	KindReassign                  = Reassign
	KindCancel                    = Cancel
	KindForceCompleteWithEvidence = ForceCompleteWithEvidence
)

func (k Kind) Valid() bool {
	switch k {
	case Pause, Resume, SkipStep, RetryStep, Reassign, Cancel, ForceCompleteWithEvidence:
		return true
	default:
		return false
	}
}

// RequiresApproval identifies interventions whose authority must be split
// between the requester and a distinct approver.
func (k Kind) RequiresApproval() bool {
	switch k {
	case SkipStep, Reassign, Cancel, ForceCompleteWithEvidence:
		return true
	default:
		return false
	}
}

// Request is the typed input to one intervention decision. Plan and Frontier
// are the exact compiled/runtime facts used for safe-point eligibility; no
// caller can submit a raw target state or an unrecognized operation.
type Request struct {
	WorkflowID         string
	WorkflowVersion    uint32
	CompiledPlanDigest string
	InstanceID         string
	ExpectedVersion    int64
	Kind               Kind
	StepID             string
	// NodeID is accepted as a spelling for a single-step intervention. When
	// both NodeID and StepID are supplied they must agree.
	NodeID   string
	Frontier []string
	Plan     *workflow.CompiledWorkflow

	Reason         string
	EvidenceRef    string
	RequestedBy    string
	ApprovedBy     string
	Assignee       string
	IdempotencyKey string
	RequestedAt    time.Time

	// CurrentStatus is optional because this package is pure and does not read
	// runtime storage. When supplied, it is checked against the kind's legal
	// precondition vocabulary.
	CurrentStatus string
}

// Transition is the typed, observable runtime transition requested by a
// successful intervention. It is a plan for the runtime owner, not a direct
// runtime-row mutation.
type Transition struct {
	InstanceFrom         string `json:"instance_from"`
	InstanceTo           string `json:"instance_to"`
	NodeFrom             string `json:"node_from,omitempty"`
	NodeTo               string `json:"node_to,omitempty"`
	RequiresRevalidation bool   `json:"requires_revalidation"`
}

// Event is the immutable audit event attached to one receipt.
type Event struct {
	Kind          Kind      `json:"kind"`
	ReceiptDigest string    `json:"receipt_digest"`
	RecordedAt    time.Time `json:"recorded_at"`
	digest        string
}

func (e Event) Digest() string { return e.digest }

// Receipt is the result of evaluating a typed intervention request.
type Receipt struct {
	WorkflowID         string     `json:"workflow_id"`
	WorkflowVersion    uint32     `json:"workflow_version"`
	CompiledPlanDigest string     `json:"compiled_plan_digest"`
	InstanceID         string     `json:"instance_id"`
	ExpectedVersion    int64      `json:"expected_version"`
	Kind               Kind       `json:"kind"`
	StepID             string     `json:"step_id,omitempty"`
	Frontier           []string   `json:"frontier"`
	Reason             string     `json:"reason"`
	EvidenceRef        string     `json:"evidence_ref"`
	RequestedBy        string     `json:"requested_by"`
	ApprovedBy         string     `json:"approved_by,omitempty"`
	Assignee           string     `json:"assignee,omitempty"`
	IdempotencyKey     string     `json:"idempotency_key"`
	RequestedAt        time.Time  `json:"requested_at"`
	Decision           string     `json:"decision"`
	Transition         Transition `json:"transition"`
	Event              Event      `json:"event"`
	digest             string
}

const DecisionAccepted = "ACCEPTED"

// Digest returns the receipt's canonical identity.
func (r Receipt) Digest() string { return r.digest }

// Verify confirms both the receipt and its attached event.
func (r Receipt) Verify() error {
	if r.digest == "" || r.digest != receiptDigest(r) {
		return refuse(CodeReceiptMutated, r.InstanceID, "intervention receipt content does not match its digest")
	}
	if r.Event.Digest() == "" || r.Event.Digest() != eventDigest(r.Event) {
		return refuse(CodeEventMutated, r.InstanceID, "intervention event content does not match its digest")
	}
	return nil
}

// Explain returns a compact review narrative. Programmatic decisions should
// branch on the typed Kind and Transition fields.
func (r Receipt) Explain() string {
	return fmt.Sprintf("%s intervention for %s: %s requested by %s%s",
		r.Kind, r.InstanceID, r.Decision, r.RequestedBy, approvalText(r.ApprovedBy))
}

func Explain(r Receipt) string { return r.Explain() }

// Evaluate validates and accepts one typed intervention, returning the exact
// transition the runtime owner may apply. It never reads or mutates storage.
func Evaluate(req Request) (Receipt, error) {
	if err := req.Validate(); err != nil {
		return Receipt{}, err
	}
	frontier := normalizedFrontier(req)
	for _, nodeID := range frontier {
		if _, ok := req.Plan.Node(nodeID); !ok {
			return Receipt{}, refuse(CodeUnknownNode, nodeID, "intervention frontier names a node absent from the compiled plan")
		}
		if !req.Plan.InterventionEligible(nodeID) {
			return Receipt{}, refuse(CodeIneligibleRegion, nodeID, "intervention is refused inside a compiled atomic region")
		}
	}
	if err := validateStatus(req); err != nil {
		return Receipt{}, err
	}
	to, nodeTo := transitionFor(req)
	r := Receipt{
		WorkflowID: req.WorkflowID, WorkflowVersion: req.WorkflowVersion,
		CompiledPlanDigest: req.CompiledPlanDigest, InstanceID: req.InstanceID,
		ExpectedVersion: req.ExpectedVersion, Kind: req.Kind, StepID: stepID(req),
		Frontier: frontier, Reason: req.Reason, EvidenceRef: req.EvidenceRef,
		RequestedBy: req.RequestedBy, ApprovedBy: req.ApprovedBy, Assignee: req.Assignee,
		IdempotencyKey: req.IdempotencyKey, RequestedAt: req.RequestedAt.UTC(),
		Decision:   DecisionAccepted,
		Transition: Transition{InstanceFrom: req.CurrentStatus, InstanceTo: to, NodeFrom: stepID(req), NodeTo: nodeTo, RequiresRevalidation: req.Kind == Resume || req.Kind == ForceCompleteWithEvidence},
	}
	r.digest = receiptDigest(r)
	r.Event = Event{Kind: r.Kind, ReceiptDigest: r.digest, RecordedAt: r.RequestedAt}
	r.Event.digest = eventDigest(r.Event)
	return r, nil
}

// Create and Intervene are names for the same pure decision boundary.
func Create(req Request) (Receipt, error)    { return Evaluate(req) }
func Intervene(req Request) (Receipt, error) { return Evaluate(req) }

func (r Request) Validate() error {
	switch {
	case !r.Kind.Valid():
		return refuse(CodeUnsupportedKind, string(r.Kind), "workflow intervention kind is not declared")
	case r.WorkflowID == "":
		return refuse(CodeInvalidRequest, "", "workflow id is required")
	case r.WorkflowVersion == 0:
		return refuse(CodeInvalidRequest, r.WorkflowID, "workflow version must be at least 1")
	case r.CompiledPlanDigest == "":
		return refuse(CodeInvalidRequest, r.WorkflowID, "compiled-plan digest is required")
	case r.InstanceID == "":
		return refuse(CodeInvalidRequest, r.WorkflowID, "instance id is required")
	case r.ExpectedVersion < 1:
		return refuse(CodeInvalidRequest, r.InstanceID, "expected instance version must be at least 1")
	case r.Plan == nil:
		return refuse(CodeInvalidRequest, r.InstanceID, "compiled plan is required for eligibility")
	case r.Plan.Digest() != r.CompiledPlanDigest:
		return refuse(CodePlanMismatch, r.InstanceID, "request digest does not match the supplied compiled plan")
	case r.Plan.WorkflowID != "" && r.Plan.WorkflowID != r.WorkflowID:
		return refuse(CodePlanMismatch, r.InstanceID, "request workflow id does not match the supplied compiled plan")
	case r.Plan.Version != 0 && r.Plan.Version != r.WorkflowVersion:
		return refuse(CodePlanMismatch, r.InstanceID, "request workflow version does not match the supplied compiled plan")
	case r.Reason == "":
		return refuse(CodeInvalidRequest, r.InstanceID, "intervention reason is required")
	case r.EvidenceRef == "":
		return refuse(CodeInvalidRequest, r.InstanceID, "intervention evidence ref is required")
	case r.RequestedBy == "":
		return refuse(CodeInvalidRequest, r.InstanceID, "requester is required")
	case r.IdempotencyKey == "":
		return refuse(CodeInvalidRequest, r.InstanceID, "idempotency key is required")
	case r.RequestedAt.IsZero():
		return refuse(CodeInvalidRequest, r.InstanceID, "requested instant is required")
	}
	if r.StepID != "" && r.NodeID != "" && r.StepID != r.NodeID {
		return refuse(CodeInvalidRequest, r.InstanceID, "step id and node id must agree when both are supplied")
	}
	if len(normalizedFrontier(r)) == 0 {
		return refuse(CodeInvalidRequest, r.InstanceID, "intervention requires a frontier for safe-point eligibility")
	}
	if r.ApprovedBy == r.RequestedBy && r.Kind.RequiresApproval() {
		return refuse(CodeSeparationOfDuties, r.InstanceID, "approver must be distinct from requester")
	}
	if r.Kind.RequiresApproval() && r.ApprovedBy == "" {
		return refuse(CodeApprovalRequired, r.InstanceID, "this intervention kind requires a distinct approver")
	}
	if r.Kind == Reassign && r.Assignee == "" {
		return refuse(CodeInvalidRequest, r.InstanceID, "reassign requires an assignee")
	}
	if (r.Kind == SkipStep || r.Kind == RetryStep) && stepID(r) == "" {
		return refuse(CodeInvalidRequest, r.InstanceID, "step intervention requires a step id")
	}
	if r.Kind == ForceCompleteWithEvidence && r.EvidenceRef == "" {
		return refuse(CodeInvalidRequest, r.InstanceID, "force complete requires completion evidence")
	}
	return nil
}

func stepID(r Request) string {
	if r.StepID != "" && r.NodeID != "" && r.StepID != r.NodeID {
		return r.StepID
	}
	if r.StepID != "" {
		return r.StepID
	}
	return r.NodeID
}

func normalizedFrontier(r Request) []string {
	out := append([]string(nil), r.Frontier...)
	if len(out) == 0 && r.NodeID != "" {
		out = []string{r.NodeID}
	}
	if len(out) == 0 && r.StepID != "" {
		out = []string{r.StepID}
	}
	sort.Strings(out)
	unique := out[:0]
	for _, id := range out {
		if id != "" && (len(unique) == 0 || unique[len(unique)-1] != id) {
			unique = append(unique, id)
		}
	}
	return unique
}

func validateStatus(r Request) error {
	if r.CurrentStatus == "" {
		return nil
	}
	status := strings.ToUpper(r.CurrentStatus)
	switch r.Kind {
	case Pause:
		if status != "RUNNING" && status != "WAITING" && status != "CREATED" {
			return refuse(CodeIllegalPrecondition, r.InstanceID, "pause requires a live instance")
		}
	case Resume:
		if status != "PAUSED" {
			return refuse(CodeIllegalPrecondition, r.InstanceID, "resume requires a PAUSED instance")
		}
	case RetryStep:
		if status != "FAILED" {
			return refuse(CodeIllegalPrecondition, r.InstanceID, "retry requires a failed step")
		}
	case ForceCompleteWithEvidence:
		if status == "COMPLETED" || status == "CANCELLED" || status == "SUPERSEDED" {
			return refuse(CodeIllegalPrecondition, r.InstanceID, "force complete cannot resurrect a terminal instance")
		}
	}
	return nil
}

func transitionFor(r Request) (string, string) {
	switch r.Kind {
	case Pause:
		return "PAUSE_REQUESTED", ""
	case Resume:
		return "RUNNING", ""
	case SkipStep:
		return "", "SKIPPED"
	case RetryStep:
		return "", "READY"
	case Reassign:
		return "", "ASSIGNED"
	case Cancel:
		return "CANCELLING", ""
	case ForceCompleteWithEvidence:
		return "COMPLETED", ""
	default:
		return "", ""
	}
}

type receiptIdentity struct {
	WorkflowID         string
	WorkflowVersion    uint32
	CompiledPlanDigest string
	InstanceID         string
	ExpectedVersion    int64
	Kind               Kind
	StepID             string
	Frontier           []string
	Reason             string
	EvidenceRef        string
	RequestedBy        string
	ApprovedBy         string
	Assignee           string
	IdempotencyKey     string
	RequestedAt        time.Time
	Decision           string
	Transition         Transition
}

func receiptDigest(r Receipt) string {
	return digest(receiptProfile, receiptIdentity{r.WorkflowID, r.WorkflowVersion, r.CompiledPlanDigest, r.InstanceID, r.ExpectedVersion, r.Kind, r.StepID, r.Frontier, r.Reason, r.EvidenceRef, r.RequestedBy, r.ApprovedBy, r.Assignee, r.IdempotencyKey, r.RequestedAt, r.Decision, r.Transition})
}

func eventDigest(e Event) string {
	return digest(eventProfile, struct {
		Kind          Kind
		ReceiptDigest string
		RecordedAt    time.Time
	}{e.Kind, e.ReceiptDigest, e.RecordedAt})
}

func digest(profile string, value any) string {
	b, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	h := sha256.New()
	_, _ = h.Write([]byte(profile))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

func approvalText(approver string) string {
	if approver == "" {
		return ""
	}
	return ", approved by " + approver
}

const (
	CodeInvalidRequest      = "INVALID_REQUEST"
	CodeUnsupportedKind     = "UNSUPPORTED_KIND"
	CodeApprovalRequired    = "APPROVAL_REQUIRED"
	CodeSeparationOfDuties  = "SEPARATION_OF_DUTIES"
	CodeIneligibleRegion    = "INELIGIBLE_REGION"
	CodePlanMismatch        = "PLAN_MISMATCH"
	CodeIllegalPrecondition = "ILLEGAL_PRECONDITION"
	CodeUnknownNode         = "UNKNOWN_NODE"
	CodeReceiptMutated      = "RECEIPT_MUTATED"
	CodeEventMutated        = "EVENT_MUTATED"
)

var ErrIntervention = errors.New("workflow/intervention: rejected")

type Error struct{ Code, Ref, Detail string }

func (e *Error) Error() string {
	if e.Ref == "" {
		return "workflow/intervention: " + e.Code + ": " + e.Detail
	}
	return "workflow/intervention: " + e.Code + " [" + e.Ref + "]: " + e.Detail
}
func (e *Error) Unwrap() error { return ErrIntervention }
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
func refuse(code, ref, detail string) *Error { return &Error{Code: code, Ref: ref, Detail: detail} }

// ErrorCode reports the refusal's stable code for telemetry classification
// (internal/workflow/observe.ErrorCode); it never carries message text.
func (e *Error) ErrorCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}
