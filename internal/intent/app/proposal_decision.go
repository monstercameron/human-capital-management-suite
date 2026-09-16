package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// ProposalDecisionRequest is the caller's assertion of the proposal page it
// is deciding. The service compares every supplied digest with the server's
// current simulation and WorkItem assignment; it never accepts a caller's
// assertion as the authoritative proposal or rendered projection.
type ProposalDecisionRequest struct {
	IntentID                 string
	ProposalRevisionID       string
	MaterialProposalDigest   string
	RenderedProjectionDigest string
	RequirementID            string
	Reason                   string
}

// ProposalDecisionResponse is the durable decision and the workflow result
// produced after the completed WorkItem is resumed. Decision is retained even
// when the workflow parks again or reports a degraded downstream dimension.
type ProposalDecisionResponse struct {
	Decision  intentapproval.ApprovalDecision
	Execution ExecutionResult
}

// Stable refusal causes for the two governed decision entry points.
var (
	ErrProposalDecisionUnavailable = errors.New("app: proposal decision is unavailable")
	ErrProposalDecisionStage       = errors.New("app: proposal decision is not currently actionable")
	ErrProposalDecisionStale       = errors.New("app: proposal decision is stale")
	ErrProposalDecisionExpired     = errors.New("app: proposal decision is past its SLA deadline")
	ErrProposalDecisionSeparation  = errors.New("app: proposal decision violates separation of duties")
	ErrProposalDecisionRoute       = errors.New("app: proposal decision caller is not the routed approver")
	ErrProposalDecisionConflict    = errors.New("app: proposal decision conflicts with an existing decision")
)

const (
	proposalDecisionCapabilityApprove = "hcmnext.work.approve_proposal"
	proposalDecisionCapabilityReject  = "hcmnext.work.reject_proposal"
	proposalDecisionEvidenceInvoked   = "INVOKED"
	proposalDecisionEvidenceRefused   = "REFUSED"
	proposalDecisionVersion           = 1
	reasonProposalDecisionRejected    = "work.proposal.request_rejected"
	reasonProposalDecisionExpired     = "work.proposal.sla_expired"
	reasonProposalDecisionInvalidated = "work.proposal.authority_invalidated"
	reasonProposalDecisionSeparation  = "work.proposal.separation_of_duties"
	reasonProposalDecisionRoute       = "work.proposal.assignment"
	reasonProposalDecisionConflict    = "work.proposal.already_decided"
	reasonProposalDecisionStage       = "work.proposal.not_actionable"
)

// proposalDecisioner owns the durable human-work transaction. The service
// owns admission, current re-simulation and driver resume; this port keeps
// those concerns out of the transport and lets JourneyEngine.Decide use the
// exact same operation as an API caller.
type proposalDecisioner interface {
	completeProposalDecision(
		context.Context,
		*trust.Principal,
		intent.Instance,
		runtime.StartRequest,
		ProposalDecisionRequest,
		bool,
	) (decidedApproval, error)
}

// bindProposalDecisioner is called only by the composition that has the
// durable execution database and driver. A service without that composition
// fails closed rather than manufacturing an approval record.
func (s *IntentService) bindProposalDecisioner(d proposalDecisioner) {
	s.proposalDecisioner = d
}

// ApproveProposal records an approval through the same execution-authority
// gate and current simulation used by ExecuteIntent, then completes exactly
// one routed approval WorkItem and advances the caller-driven workflow in the
// same transaction through the approval kernel (WF-STEP-018).
func (s *IntentService) ApproveProposal(ctx context.Context, req ProposalDecisionRequest) (*ProposalDecisionResponse, error) {
	return s.decideProposal(ctx, req, true)
}

// RejectProposal is the rejection twin of ApproveProposal. Both operations
// share admission, binding, SLA, separation-of-duties, durable completion and
// resume mechanics; only the immutable decision outcome differs.
func (s *IntentService) RejectProposal(ctx context.Context, req ProposalDecisionRequest) (*ProposalDecisionResponse, error) {
	return s.decideProposal(ctx, req, false)
}

func (s *IntentService) decideProposal(ctx context.Context, req ProposalDecisionRequest, approve bool) (*ProposalDecisionResponse, error) {
	principal, inv, ownedErr := caller(ctx)
	if ownedErr != nil {
		return nil, ownedErr
	}
	if err := validateProposalDecisionRequest(req, approve); err != nil {
		return nil, proposalDecisionEnvelope(reasonProposalDecisionRejected, err)
	}
	inst, _, ownedErr := s.loadInstance(ctx, principal.Tenant().String(), req.IntentID)
	if ownedErr != nil {
		return nil, ownedErr
	}
	def, err := s.defs.Resolve(inst.Definition)
	if err != nil {
		return nil, envelope.New(envelope.CodeNotFound, reasonIntentNotFound,
			"the resource does not exist or is not visible").WithDiagnostic(err)
	}
	if gateErr := s.authorizeDecision(def); gateErr != nil {
		s.recordProposalDecisionEvidence(ctx, principal.Tenant().String(), req.IntentID, approve, proposalDecisionEvidenceRefused, gateErr.ReasonRef())
		return nil, gateErr
	}
	if s.proposalDecisioner == nil || s.executor == nil || s.executionResolver == nil || s.executionVersions == nil || s.tenantUUID == nil {
		err := envelope.New(envelope.CodeFailedPrecondition, reasonExecutionUnavailable,
			"workflow execution is not configured for this service")
		s.recordProposalDecisionEvidence(ctx, principal.Tenant().String(), req.IntentID, approve, proposalDecisionEvidenceRefused, err.ReasonRef())
		return nil, err
	}

	simulated, ownedErr := s.simulateDetailed(ctx, principal, purposeOf(principal, inv), inst, def)
	artifact := simulated.Artifact
	if ownedErr != nil {
		s.recordProposalDecisionEvidence(ctx, principal.Tenant().String(), req.IntentID, approve, proposalDecisionEvidenceRefused, ownedErr.ReasonRef())
		return nil, ownedErr
	}
	if artifact.GetProposalRevisionId() == "" {
		err := proposalDecisionEnvelope(reasonNoExecutablePlan, ErrProposalDecisionStage)
		s.recordProposalDecisionEvidence(ctx, principal.Tenant().String(), req.IntentID, approve, proposalDecisionEvidenceRefused, err.ReasonRef())
		return nil, err
	}
	if req.ProposalRevisionID != artifact.GetProposalRevisionId() || req.MaterialProposalDigest != artifact.GetMaterialProposalDigest().GetDigest() {
		err := proposalDecisionEnvelope(reasonStaleRevision, ErrProposalDecisionStale)
		s.recordProposalDecisionEvidence(ctx, principal.Tenant().String(), req.IntentID, approve, proposalDecisionEvidenceRefused, err.ReasonRef())
		return nil, err
	}
	if simulated.Revision == nil {
		return nil, proposalDecisionEnvelope(reasonNoExecutablePlan, ErrProposalDecisionStage)
	}
	start, startErr := s.executionStart(inst, artifact, journeyApprovalRef(req.IntentID), *simulated.Revision)
	if startErr != nil {
		return nil, startErr
	}

	decision, decisionErr := s.proposalDecisioner.completeProposalDecision(ctx, principal, inst, start, req, approve)
	if decisionErr != nil {
		err := proposalDecisionError(decisionErr)
		s.recordProposalDecisionEvidence(ctx, principal.Tenant().String(), req.IntentID, approve, proposalDecisionEvidenceRefused, err.ReasonRef())
		return nil, err
	}
	if decision.needsResume() {
		result, resumeErr := s.executor.Resume(ctx, ExecutionResumeRequest{
			Start:                   start,
			InstanceID:              decision.instance.InstanceID,
			ExpectedInstanceVersion: decision.instance.InstanceVersion,
			WorkItem:                decision.item,
			Outcome:                 decision.outcome,
		})
		if resumeErr != nil {
			err := executionError(resumeErr)
			s.recordProposalDecisionEvidence(ctx, principal.Tenant().String(), req.IntentID, approve, proposalDecisionEvidenceRefused, err.ReasonRef())
			return nil, err
		}
		decision.execution = result
	}
	if decision.refusal != nil {
		// WF-STEP-003: the approval was closed (EXPIRED or INVALIDATED) and
		// that route taken; the caller's decision was not recorded.
		err := proposalDecisionError(decision.refusal)
		s.recordProposalDecisionEvidence(ctx, principal.Tenant().String(), req.IntentID, approve, proposalDecisionEvidenceRefused, err.ReasonRef())
		return nil, err
	}
	s.recordProposalDecisionEvidence(ctx, principal.Tenant().String(), req.IntentID, approve, proposalDecisionEvidenceInvoked, "")
	return &ProposalDecisionResponse{Decision: decision.decision, Execution: decision.execution}, nil
}

func validateProposalDecisionRequest(req ProposalDecisionRequest, approve bool) error {
	for field, value := range map[string]string{
		"intent_id": req.IntentID, "proposal_revision_id": req.ProposalRevisionID,
		"material_proposal_digest": req.MaterialProposalDigest,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", field)
		}
	}
	if req.RenderedProjectionDigest != "" && !workitem.ValidDigest(req.RenderedProjectionDigest) {
		return fmt.Errorf("rendered_projection_digest is not a sha256 digest")
	}
	if !approve && strings.TrimSpace(req.Reason) == "" {
		return errors.New("reason is required for rejection")
	}
	return nil
}

func (s *IntentService) recordProposalDecisionEvidence(ctx context.Context, tenant, intentID string, approve bool, decision, reason string) {
	if s.evidence == nil {
		return
	}
	at := time.Now().UTC()
	if s.clock != nil {
		at = s.clock().Time()
	}
	id := proposalDecisionCapabilityReject
	if approve {
		id = proposalDecisionCapabilityApprove
	}
	_, _ = s.evidence.RecordInvocation(ctx, capability.InvocationEvidence{
		CapabilityID: id, CapabilityVersion: proposalDecisionVersion, SubjectRef: intentID, Tenant: tenant,
		Decision: decision, ReasonCode: reason, OccurredAt: at,
	})
}

func proposalDecisionEnvelope(reason string, cause error) *envelope.Error {
	code := envelope.CodeFailedPrecondition
	if errors.Is(cause, ErrProposalDecisionRoute) || errors.Is(cause, ErrProposalDecisionSeparation) {
		code = envelope.CodePermissionDenied
	}
	return envelope.New(code, reason, "a precondition for the operation is not met").WithDiagnostic(cause)
}

func proposalDecisionError(err error) *envelope.Error {
	// WF-STEP-018: a workflow failure raised by the approval kernel is
	// already projected by executionError.
	var owned *envelope.Error
	if errors.As(err, &owned) {
		return owned
	}
	switch {
	case errors.Is(err, ErrProposalDecisionInvalidated):
		return proposalDecisionEnvelope(reasonProposalDecisionInvalidated, err)
	case errors.Is(err, ErrProposalDecisionUnavailable):
		return envelope.New(envelope.CodeFailedPrecondition, reasonExecutionUnavailable,
			"workflow execution is not configured for this service").WithDiagnostic(err)
	case errors.Is(err, ErrProposalDecisionExpired):
		return proposalDecisionEnvelope(reasonProposalDecisionExpired, err)
	case errors.Is(err, ErrProposalDecisionStale):
		return proposalDecisionEnvelope(reasonStaleRevision, err)
	case errors.Is(err, ErrProposalDecisionSeparation):
		return proposalDecisionEnvelope(reasonProposalDecisionSeparation, err)
	case errors.Is(err, ErrProposalDecisionRoute):
		return proposalDecisionEnvelope(reasonProposalDecisionRoute, err)
	case errors.Is(err, ErrProposalDecisionConflict):
		return proposalDecisionEnvelope(reasonProposalDecisionConflict, err)
	case errors.Is(err, ErrProposalDecisionStage):
		return proposalDecisionEnvelope(reasonProposalDecisionStage, err)
	case errors.Is(err, workspace.ErrJourneyStage):
		return proposalDecisionEnvelope(reasonProposalDecisionStage, err)
	default:
		return envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(err)
	}
}

func proposalWorkItemError(err error) error {
	switch workitem.CodeOf(err) {
	case workitem.CodeStaleItem, workitem.CodeAlreadyClaimed, workitem.CodeOutputImmutable:
		return fmt.Errorf("%w: %v", ErrProposalDecisionConflict, err)
	case workitem.CodeClaimExpired:
		return fmt.Errorf("%w: %v", ErrProposalDecisionExpired, err)
	default:
		return journeyWorkItemError(err)
	}
}

// decisionWire mirrors workflow/steps/approval's durable JSON shape. It is
// intentionally local: the steps package keeps its encoding private, while
// this reader only exists to make a committed replay return the same decision.
type proposalDecisionWire struct {
	DecisionID           string                       `json:"decision_id"`
	Binding              proposalDecisionBindingWire  `json:"binding"`
	Outcome              string                       `json:"outcome"`
	Approver             proposalDecisionApproverWire `json:"approver"`
	AuthorityDecisionRef string                       `json:"authority_decision_ref"`
	Reason               string                       `json:"reason"`
	DecidedAt            string                       `json:"decided_at"`
	VoteDigest           string                       `json:"vote_digest"`
}

type proposalDecisionBindingWire struct {
	RequirementID              string                  `json:"requirement_id"`
	RequirementRevision        uint64                  `json:"requirement_revision"`
	IntentID                   string                  `json:"intent_id"`
	ProposalRevisionID         string                  `json:"proposal_revision_id"`
	ProposalDigest             digest.Reference        `json:"proposal_digest"`
	TaskVersion                uint64                  `json:"task_version"`
	RenderedProjectionDigest   string                  `json:"rendered_projection_digest"`
	ControlSnapshots           intent.ControlSnapshots `json:"control_snapshots"`
	RequirementDigest          string                  `json:"requirement_digest"`
	ResolutionExpressionDigest string                  `json:"resolution_expression_digest"`
}

type proposalDecisionApproverWire struct {
	PrincipalID          string `json:"principal_id"`
	IdentityAssuranceRef string `json:"identity_assurance_ref"`
	SessionRef           string `json:"session_ref"`
	Via                  string `json:"via"`
	DelegationID         string `json:"delegation_id,omitempty"`
}

func proposalDecisionFromRecord(rec workitem.DecisionRecord) (intentapproval.ApprovalDecision, error) {
	var wire proposalDecisionWire
	if err := json.Unmarshal(rec.Body, &wire); err != nil {
		return intentapproval.ApprovalDecision{}, fmt.Errorf("decode committed approval decision: %w", err)
	}
	at, err := time.Parse(time.RFC3339Nano, wire.DecidedAt)
	if err != nil {
		return intentapproval.ApprovalDecision{}, fmt.Errorf("decode committed approval instant: %w", err)
	}
	decision := intentapproval.ApprovalDecision{
		DecisionID: wire.DecisionID,
		Binding: intentapproval.DecisionBinding{
			RequirementID: wire.Binding.RequirementID, RequirementRevision: wire.Binding.RequirementRevision,
			IntentID: wire.Binding.IntentID, ProposalRevisionID: wire.Binding.ProposalRevisionID,
			ProposalDigest: wire.Binding.ProposalDigest, TaskVersion: wire.Binding.TaskVersion,
			RenderedProjectionDigest: wire.Binding.RenderedProjectionDigest,
			ControlSnapshots:         wire.Binding.ControlSnapshots, RequirementDigest: wire.Binding.RequirementDigest,
			ResolutionExpressionDigest: wire.Binding.ResolutionExpressionDigest,
		},
		Outcome: intentapproval.Outcome(wire.Outcome),
		Approver: intentapproval.ApproverReference{
			PrincipalID: wire.Approver.PrincipalID, IdentityAssuranceRef: wire.Approver.IdentityAssuranceRef,
			SessionRef: wire.Approver.SessionRef, Via: humanwork.CandidateSource(wire.Approver.Via), DelegationID: wire.Approver.DelegationID,
		},
		AuthorityDecisionRef: wire.AuthorityDecisionRef, Reason: wire.Reason, DecidedAt: values.NewInstant(at), VoteDigest: wire.VoteDigest,
	}
	if decision.DecisionID == "" || decision.Digest() == "" || decision.Digest() != rec.BodyDigest {
		return intentapproval.ApprovalDecision{}, fmt.Errorf("%w: committed approval decision digest does not verify", ErrProposalDecisionConflict)
	}
	return decision, nil
}
