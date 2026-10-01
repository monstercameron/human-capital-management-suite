package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/protobuf/proto"
)

var ErrAgentActionInput = errors.New("application: agent action input is invalid")
var ErrAgentActionApproval = errors.New("application: agent action requires the user's exact proposal approval")

// AgentActionCompileRequest is typed model output plus the exact served
// definition seen by the model. This is a proposal, never approval evidence.
type AgentActionCompileRequest struct {
	Definition      *intentsv1.IntentDefinition    `json:"definition"`
	Request         *intentsv1.CreateIntentRequest `json:"request"`
	AgentRunID      string                         `json:"agent_run_id"`
	AgentVersionRef string                         `json:"agent_version_ref"`
	ModelDigest     string                         `json:"model_digest"`
	Sources         []string                       `json:"sources"`
	Taint           []string                       `json:"taint"`
	Uncertainty     string                         `json:"uncertainty"`
}

// AgentActionSubmitRequest is the foreground human's confirmation of exactly
// the proposal displayed in the owner surface. Business approvers retain the
// ordinary work-item decisions; this service exposes no approval operation.
type AgentActionSubmitRequest struct {
	IntentID                string `json:"intent_id"`
	ProposalRevisionID      string `json:"proposal_revision_id"`
	ProposalDigest          string `json:"proposal_digest"`
	ExpectedInstanceVersion uint64 `json:"expected_instance_version"`
}

type AgentActionState struct {
	IntentID           string                        `json:"intent_id"`
	State              string                        `json:"state"`
	InstanceVersion    uint64                        `json:"instance_version"`
	ProposalRevisionID string                        `json:"proposal_revision_id,omitempty"`
	ProposalDigest     string                        `json:"proposal_digest,omitempty"`
	ReceiptRef         string                        `json:"receipt_ref,omitempty"`
	RepairRef          string                        `json:"repair_ref,omitempty"`
	OwnerURL           string                        `json:"owner_url"`
	Subjects           []*intentsv1.SubjectReference `json:"subjects,omitempty"`
	PlannedWrites      []*intentsv1.PlannedWrite     `json:"planned_writes,omitempty"`
	Sources            []string                      `json:"sources,omitempty"`
	Taint              []string                      `json:"taint,omitempty"`
	Uncertainty        string                        `json:"uncertainty,omitempty"`
	Approver           string                        `json:"approver,omitempty"`
}

type agentActionOrigin struct {
	RunID          string   `json:"run_id"`
	AgentVersion   string   `json:"agent_version"`
	ForUser        string   `json:"for_user"`
	ModelDigest    string   `json:"model_digest"`
	ProposalDigest string   `json:"draft_digest"`
	Sources        []string `json:"sources"`
	Taint          []string `json:"taint"`
	Uncertainty    string   `json:"uncertainty"`
}

// AgentActionService bridges agent proposals into the composed BusinessIntent
// and workflow owners. No HCM data is written by this adapter itself.
type AgentActionService struct {
	cell      *app.Cell
	authority AgentActionAuthority
}

func NewAgentActionService(cell *app.Cell) (*AgentActionService, error) {
	if cell == nil || cell.Service == nil || cell.Definitions == nil {
		return nil, ErrAgentActionInput
	}
	return &AgentActionService{cell: cell}, nil
}

func (s *AgentActionService) principal(ctx context.Context) (*trust.Principal, error) {
	if s == nil || s.cell == nil || s.cell.Service == nil || ctx == nil {
		return nil, ErrAgentActionInput
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman || !p.Assurance().AtLeast(trust.AssuranceLow) {
		return nil, ErrAgentActionApproval
	}
	return p, nil
}

// Compile validates owner schemas and current authority before persisting the
// effect-free draft. The idempotency identity binds all model provenance and
// the principal; changing any material produces a different proposal.
func (s *AgentActionService) Compile(ctx context.Context, req AgentActionCompileRequest) (AgentActionState, error) {
	p, err := s.principal(ctx)
	if err != nil {
		return AgentActionState{}, err
	}
	if req.Definition == nil || req.Request == nil || strings.TrimSpace(req.AgentRunID) == "" || strings.TrimSpace(req.AgentVersionRef) == "" || strings.TrimSpace(req.ModelDigest) == "" || strings.TrimSpace(req.Uncertainty) == "" || len(req.Sources) == 0 || len(req.Taint) == 0 {
		return AgentActionState{}, ErrAgentActionInput
	}
	for _, ref := range append(append([]string(nil), req.Sources...), req.Taint...) {
		if strings.TrimSpace(ref) == "" {
			return AgentActionState{}, ErrAgentActionInput
		}
	}
	request := proto.Clone(req.Request).(*intentsv1.CreateIntentRequest)
	// Authority and identity come from the verified request principal. Reject a
	// contradictory initiator, then populate an omitted server-owned field.
	if initiator := request.GetInitiator(); initiator != nil && (initiator.GetPrincipalId() != p.Subject() || initiator.GetKind() != intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN || initiator.GetIdentityAssuranceRef() != p.EvidenceID()) {
		return AgentActionState{}, ErrAgentActionInput
	}
	request.Initiator = &intentsv1.PrincipalReference{PrincipalId: p.Subject(), Kind: intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN, IdentityAssuranceRef: p.EvidenceID()}
	if request.ParentIntentId != nil || request.OriginEventRef != nil {
		return AgentActionState{}, ErrAgentActionInput
	}
	request.IdempotencyKey = "agent:validate"
	if err := s.cell.Service.ValidateAgentIntentDraft(ctx, req.Definition, request); err != nil {
		return AgentActionState{}, err
	}
	req.Request = request
	canonicalRequest := proto.Clone(request).(*intentsv1.CreateIntentRequest)
	canonicalRequest.Initiator = nil
	req.Request = canonicalRequest
	draftDigest, err := agentActionProposalDigest(p.Tenant().String(), p.Subject(), req)
	if err != nil {
		return AgentActionState{}, err
	}
	if err := s.verifyAuthority(ctx, AgentActionAuthorityPin{TenantID: p.Tenant().String(), ForUser: p.Subject(), RunID: req.AgentRunID, AgentVersionRef: req.AgentVersionRef, ModelDigest: req.ModelDigest, DraftDigest: draftDigest, Proposal: &req}); err != nil {
		return AgentActionState{}, err
	}
	request.IdempotencyKey = "agent:action:" + strings.TrimPrefix(draftDigest, "sha256:")
	originBytes, err := json.Marshal(agentActionOrigin{RunID: req.AgentRunID, AgentVersion: req.AgentVersionRef, ForUser: p.Subject(), ModelDigest: req.ModelDigest, ProposalDigest: draftDigest, Sources: req.Sources, Taint: req.Taint, Uncertainty: req.Uncertainty})
	if err != nil {
		return AgentActionState{}, err
	}
	origin := "agent-run:" + string(originBytes)
	request.OriginEventRef = &origin
	created, err := s.cell.Service.CreateIntent(ctx, request)
	if err != nil {
		return AgentActionState{}, err
	}
	state := agentActionState(created.GetIntent())
	simulation, err := s.cell.Service.SimulateIntent(ctx, &intentsv1.SimulateIntentRequest{IntentId: state.IntentID, ExpectedInstanceVersion: state.InstanceVersion})
	if err != nil {
		return state, err
	}
	state.ProposalRevisionID = simulation.GetSimulation().GetProposalRevisionId()
	state.ProposalDigest = simulation.GetSimulation().GetMaterialProposalDigest().GetDigest()
	state.PlannedWrites = simulation.GetSimulation().GetPlannedWrites()
	return state, nil
}

// Submit accepts no agent or chat approval claims. It re-simulates the durable
// draft under the foreground human and sends the exact proposal to the normal
// workflow start, where business approval tasks and effect-boundary checks run.
func (s *AgentActionService) Submit(ctx context.Context, req AgentActionSubmitRequest) (AgentActionState, error) {
	p, err := s.principal(ctx)
	if err != nil {
		return AgentActionState{}, err
	}
	if req.IntentID == "" || req.ProposalRevisionID == "" || req.ProposalDigest == "" || req.ExpectedInstanceVersion == 0 {
		return AgentActionState{}, ErrAgentActionApproval
	}
	current, err := s.cell.Service.GetIntent(ctx, &intentsv1.GetIntentRequest{IntentId: req.IntentID})
	if err != nil {
		return AgentActionState{}, err
	}
	instance := current.GetIntent()
	if instance.GetInitiator().GetPrincipalId() != p.Subject() || !strings.HasPrefix(instance.GetOriginEventRef(), "agent-run:") || instance.GetInstanceVersion() != req.ExpectedInstanceVersion {
		return AgentActionState{}, ErrAgentActionApproval
	}
	var origin agentActionOrigin
	if json.Unmarshal([]byte(strings.TrimPrefix(instance.GetOriginEventRef(), "agent-run:")), &origin) != nil || origin.ForUser != p.Subject() {
		return AgentActionState{}, ErrAgentActionApproval
	}
	simulated, err := s.cell.Service.SimulateIntent(ctx, &intentsv1.SimulateIntentRequest{IntentId: req.IntentID, ExpectedInstanceVersion: req.ExpectedInstanceVersion})
	if err != nil {
		return AgentActionState{}, err
	}
	artifact := simulated.GetSimulation()
	if artifact.GetProposalRevisionId() != req.ProposalRevisionID || artifact.GetMaterialProposalDigest().GetDigest() != req.ProposalDigest {
		return AgentActionState{}, ErrAgentActionApproval
	}
	if err := s.verifyAuthority(ctx, AgentActionAuthorityPin{TenantID: p.Tenant().String(), ForUser: p.Subject(), RunID: origin.RunID, AgentVersionRef: origin.AgentVersion, ModelDigest: origin.ModelDigest, DraftDigest: origin.ProposalDigest, IntentID: req.IntentID, ProposalRevisionID: req.ProposalRevisionID, ProposalDigest: req.ProposalDigest}); err != nil {
		return AgentActionState{}, err
	}
	if s.cell.Journey == nil {
		return AgentActionState{}, app.ErrLifecycleWritesUnavailable
	}
	// A durable workflow already bound to this exact proposal is a replay.
	// Reading its state does not create a new execution or approve a work item.
	if detail, err := s.cell.Journey.Inspect(ctx, req.IntentID); err == nil && detail.Summary.InstanceID != "" {
		return s.Observe(ctx, req.IntentID)
	}
	if _, err := s.cell.Journey.Execute(ctx, req.IntentID); err != nil {
		if errors.Is(err, workspace.ErrJourneyStage) {
			if detail, inspectErr := s.cell.Journey.Inspect(ctx, req.IntentID); inspectErr == nil && detail.Summary.InstanceID != "" {
				return s.Observe(ctx, req.IntentID)
			}
		}
		return AgentActionState{}, err
	}
	return s.Observe(ctx, req.IntentID)
}

// Observe reports durable owner lifecycle. A committed receipt alone does not
// claim observed success while business or consistency dimensions are pending.
func (s *AgentActionService) Observe(ctx context.Context, intentID string) (AgentActionState, error) {
	if _, err := s.principal(ctx); err != nil {
		return AgentActionState{}, err
	}
	current, err := s.cell.Service.ObserveAgentIntent(ctx, intentID)
	if err != nil {
		return AgentActionState{}, err
	}
	state := agentActionState(current.Instance)
	state.ReceiptRef = current.ReceiptRef
	state.RepairRef = current.RepairRef
	if s.cell.Journey != nil {
		detail, err := s.cell.Journey.Inspect(ctx, intentID)
		if err != nil {
			return AgentActionState{}, err
		}
		state.ProposalRevisionID = detail.Summary.ProposalRevisionID
		state.ProposalDigest = detail.Summary.MaterialDigest
		state.Approver = detail.Summary.Approver
		switch detail.Summary.Stage {
		case workspace.JourneyStageAwaitingApproval, workspace.JourneyStageFinanceApproval, workspace.JourneyStageManagerApproval, workspace.JourneyStageReapproval:
			state.State = "awaiting approval"
		case workspace.JourneyStageWaitingEffectiveDate, workspace.JourneyStageRevalidation, workspace.JourneyStageExecuted, workspace.JourneyStageObservingEffects, workspace.JourneyStageAwaitingAcknowledgement:
			state.State = "executing"
		case workspace.JourneyStageRepairRequired:
			state.State = "needs repair"
		case workspace.JourneyStageFailed, workspace.JourneyStageRejected, workspace.JourneyStageBlocked:
			state.State = "failed"
		}
	}
	if state.RepairRef != "" {
		state.State = "needs repair"
	}
	if state.State == "observed" && state.ReceiptRef == "" {
		state.State = "executing"
	}
	return state, nil
}

func agentActionState(instance *intentsv1.IntentInstance) AgentActionState {
	state := AgentActionState{IntentID: instance.GetIntentId(), State: "draft", InstanceVersion: instance.GetInstanceVersion(), OwnerURL: "/workspace/app/journeys?journey=" + url.QueryEscape(instance.GetIntentId())}
	for _, subject := range instance.GetSubjects() {
		state.Subjects = append(state.Subjects, proto.Clone(subject).(*intentsv1.SubjectReference))
	}
	var origin agentActionOrigin
	if raw, ok := strings.CutPrefix(instance.GetOriginEventRef(), "agent-run:"); ok && json.Unmarshal([]byte(raw), &origin) == nil {
		state.Sources = append([]string(nil), origin.Sources...)
		state.Taint = append([]string(nil), origin.Taint...)
		state.Uncertainty = origin.Uncertainty
	}
	dimensions := instance.GetLifecycle()
	if state.RepairRef != "" || dimensions.GetExecution() == intentsv1.ExecutionState_EXECUTION_STATE_REPAIR_REQUIRED {
		state.State = "needs repair"
		return state
	}
	switch dimensions.GetRequest() {
	case intentsv1.RequestState_REQUEST_STATE_REJECTED, intentsv1.RequestState_REQUEST_STATE_CANCELLED, intentsv1.RequestState_REQUEST_STATE_WITHDRAWN, intentsv1.RequestState_REQUEST_STATE_SUPERSEDED:
		state.State = "failed"
		return state
	case intentsv1.RequestState_REQUEST_STATE_SUBMITTED:
		state.State = "awaiting approval"
	}
	switch dimensions.GetExecution() {
	case intentsv1.ExecutionState_EXECUTION_STATE_BLOCKED:
		state.State = "failed"
	case intentsv1.ExecutionState_EXECUTION_STATE_SCHEDULED, intentsv1.ExecutionState_EXECUTION_STATE_REVALIDATING, intentsv1.ExecutionState_EXECUTION_STATE_EXECUTING:
		state.State = "executing"
	case intentsv1.ExecutionState_EXECUTION_STATE_COMMITTED:
		state.State = "executing"
		if dimensions.GetBusiness() == intentsv1.BusinessState_BUSINESS_STATE_COMPLETED && (dimensions.GetConsistency() == intentsv1.ConsistencyState_CONSISTENCY_STATE_CONSISTENT || dimensions.GetConsistency() == intentsv1.ConsistencyState_CONSISTENCY_STATE_NOT_APPLICABLE) {
			state.State = "observed"
		}
	}
	return state
}
