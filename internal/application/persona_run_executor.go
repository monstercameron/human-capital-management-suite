package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	// ErrPersonaRunExecutorUnavailable marks incomplete or inconsistent production composition.
	ErrPersonaRunExecutorUnavailable = errors.New("application: persona run executor unavailable")
	// ErrPersonaRunExecutorBusy marks a run that already owns a worker or cannot be claimed.
	ErrPersonaRunExecutorBusy = errors.New("application: persona run is already executing")
	// ErrPersonaRunModelFailure marks a refused, incomplete, or unavailable model result.
	ErrPersonaRunModelFailure = errors.New("application: persona model execution failed")
	// ErrPersonaRunOutputRejected marks output that failed validation or binding.
	ErrPersonaRunOutputRejected = errors.New("application: persona model output rejected")
	// ErrPersonaRunDeliveryFailure marks failure to deliver validated output to chat.
	ErrPersonaRunDeliveryFailure = errors.New("application: persona reply delivery failed")
)

// PersonaRunFailure is the stable, non-sensitive terminal failure returned to
// the invocation boundary. Code and Retryable are suitable for progress UI.
type PersonaRunFailure struct {
	Code      string
	Retryable bool
	kind      error
}

func (f *PersonaRunFailure) Error() string {
	if f == nil {
		return ErrPersonaRunExecutorUnavailable.Error()
	}
	return fmt.Sprintf("persona run failed: %s", f.Code)
}

// Unwrap exposes a stable failure class without provider or authority details.
func (f *PersonaRunFailure) Unwrap() error {
	if f == nil || f.kind == nil {
		return ErrPersonaRunExecutorUnavailable
	}
	return f.kind
}

// PersonaRunModelWork is built from pinned admission and execution state. It
// contains no caller-selected model, route, budget, or credential.
type PersonaRunModelWork struct{ Request AgentModelExecutorRequest }

// PersonaRunModelWorkSource reconstructs one trusted model request from the
// immutable admission and current run. Implementations recheck context and
// policy and must not take model controls from chat content.
type PersonaRunModelWorkSource interface {
	BuildPersonaRunModelWork(context.Context, agentrun.Record, runstate.Run) (PersonaRunModelWork, error)
}

// PersonaRunT0ToolExecutionPort exposes reviewed tools from exact pins and
// current invocation authority, then executes one proposal under that same
// durable admission. Implementations reject unknown skills and tool names.
type PersonaRunT0ToolExecutionPort interface {
	ToolSchemas(context.Context, agentrun.Record, runstate.Run) ([]agentmodel.ToolSchema, error)
	Execute(context.Context, agentrun.Record, runstate.Run, agentmodel.ToolProposal) ([]byte, string, string, error)
}

// PersonaRunOutputValidator validates and durably persists normalized model
// output under the admission's current security authority.
type PersonaRunOutputValidator interface {
	ValidateAndPersistPersonaOutput(context.Context, agentrun.Record, runstate.Run, agentmodel.ModelResult) (agentsecurity.FinalOutputPersistence, error)
}

// PersonaRunReplyDeliverer applies the current audience floor before any chat
// result is committed.
type PersonaRunReplyDeliverer interface {
	Deliver(context.Context, PersonaReplyDeliveryRequest) (PersonaReplyDeliveryReceipt, error)
}

// PersonaRunBackgroundReplyDeliverer commits a sealed output as an authenticated
// worker. Invoker identifiers are recipient provenance, never a HUMAN principal.
type PersonaRunBackgroundReplyDeliverer interface {
	DeliverBackgroundPersonaReply(context.Context, agentrun.Record, runstate.Run, agentsecurity.FinalOutputPersistence) (PersonaReplyDeliveryReceipt, error)
}

// PersonaRunStarterConfig provides all admission, execution, model, validation,
// and delivery dependencies. No provider or authorization fallback exists.
type PersonaRunStarterConfig struct {
	Builder          *PersonaRunRequestBuilder
	Authority        agentrun.Authority
	AdmissionStore   agentrun.Store
	ExecutionStore   runstate.Store
	AdmissionRecheck runstate.AdmissionRechecker
	Model            interface {
		Execute(context.Context, AgentModelExecutorRequest) (AgentModelExecutorResult, error)
	}
	Work            PersonaRunModelWorkSource
	Tools           PersonaRunT0ToolExecutionPort
	Output          PersonaRunOutputValidator
	Reply           PersonaRunReplyDeliverer
	BackgroundReply PersonaRunBackgroundReplyDeliverer
	WorkerID        string
	LeaseTTL        time.Duration
	Now             func() time.Time
}

// PersonaRunStarter admits chat invocations, durably creates the run, and
// executes one bounded persona model turn through the shared gateway.
type PersonaRunStarter struct {
	adapter *personaChatAdmissionAdapter
}

// NewPersonaRunStarter refuses incomplete production execution composition.
func NewPersonaRunStarter(cfg PersonaRunStarterConfig) (*PersonaRunStarter, error) {
	if cfg.Builder == nil || cfg.Builder.source == nil || cfg.Authority == nil || cfg.AdmissionStore == nil || cfg.ExecutionStore == nil || cfg.AdmissionRecheck == nil || cfg.Model == nil || cfg.Work == nil || cfg.Output == nil || cfg.Reply == nil || strings.TrimSpace(cfg.WorkerID) == "" || cfg.LeaseTTL <= 0 || cfg.Now == nil {
		return nil, ErrPersonaRunExecutorUnavailable
	}
	admission, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: cfg.Authority, Store: cfg.AdmissionStore, Now: cfg.Now})
	if err != nil {
		return nil, fmt.Errorf("%w: admission service: %v", ErrPersonaRunExecutorUnavailable, err)
	}
	state, err := runstate.New(cfg.ExecutionStore, cfg.AdmissionRecheck)
	if err != nil {
		return nil, fmt.Errorf("%w: runstate service: %v", ErrPersonaRunExecutorUnavailable, err)
	}
	executor := &personaAdmittedRunExecutor{state: state, store: cfg.ExecutionStore, model: cfg.Model, work: cfg.Work, tools: cfg.Tools, output: cfg.Output, reply: cfg.Reply, backgroundReply: cfg.BackgroundReply, workerID: cfg.WorkerID, leaseTTL: cfg.LeaseTTL, now: cfg.Now}
	adapter, err := newPersonaChatAdmissionAdapter(cfg.Builder, admission, executor)
	if err != nil {
		return nil, fmt.Errorf("%w: admission adapter: %v", ErrPersonaRunExecutorUnavailable, err)
	}
	return &PersonaRunStarter{adapter: adapter}, nil
}

// Start admits a trusted persona mention. Replayed invocations resolve to the
// same run ID; a second worker cannot claim its current lease.
func (s *PersonaRunStarter) Start(ctx context.Context, request agentinvoke.RunRequest) error {
	if s == nil || s.adapter == nil || ctx == nil {
		return ErrPersonaRunExecutorUnavailable
	}
	return s.adapter.Start(ctx, request)
}

type personaAdmittedRunExecutor struct {
	state *runstate.Service
	store runstate.Store
	model interface {
		Execute(context.Context, AgentModelExecutorRequest) (AgentModelExecutorResult, error)
	}
	work            PersonaRunModelWorkSource
	tools           PersonaRunT0ToolExecutionPort
	output          PersonaRunOutputValidator
	reply           PersonaRunReplyDeliverer
	backgroundReply PersonaRunBackgroundReplyDeliverer
	workerID        string
	leaseTTL        time.Duration
	now             func() time.Time
}

func (e *personaAdmittedRunExecutor) Start(ctx context.Context, admission agentrun.Record) (runstate.Run, error) {
	if e == nil || e.state == nil || e.store == nil || e.now == nil {
		return runstate.Run{}, ErrPersonaRunExecutorUnavailable
	}
	run, err := e.state.Start(ctx, admission)
	if errors.Is(err, runstate.ErrInvalid) {
		return runstate.Run{}, fmt.Errorf("%w: accepted admission rejected: %v", ErrPersonaRunExecutorUnavailable, err)
	}
	if err != nil {
		// Concurrent duplicate admission may have created the same stable run.
		run, err = e.store.Get(ctx, admission.ID)
		if err != nil || run.AdmissionID != admission.ID || run.RequestDigest != admission.RequestDigest {
			return runstate.Run{}, fmt.Errorf("%w: durable run identity conflict", ErrPersonaRunExecutorUnavailable)
		}
	}
	switch run.State {
	case runstate.StateCompleted:
		return run, nil
	case runstate.StateReady:
		if err := e.execute(ctx, admission, run); err != nil {
			latest, getErr := e.store.Get(ctx, admission.ID)
			if getErr == nil {
				return latest, err
			}
			return runstate.Run{}, err
		}
		latest, err := e.store.Get(ctx, admission.ID)
		return latest, err
	case runstate.StateFailed:
		return run, personaRunStoredFailure(run)
	default:
		return run, ErrPersonaRunExecutorBusy
	}
}

func personaRunStoredFailure(run runstate.Run) error {
	kind := ErrPersonaRunOutputRejected
	switch run.TerminalCode {
	case "CONTEXT_UNAVAILABLE", "MODEL_BINDING_INVALID":
		kind = ErrPersonaRunExecutorUnavailable
	case "MODEL_UNAVAILABLE", "MODEL_REFUSED_OR_INCOMPLETE", "MODEL_RESULT_INVALID":
		kind = ErrPersonaRunModelFailure
	case "DELIVERY_FAILED":
		kind = ErrPersonaRunDeliveryFailure
	}
	return &PersonaRunFailure{Code: run.TerminalCode, Retryable: run.Retryable, kind: kind}
}

func (e *personaAdmittedRunExecutor) execute(ctx context.Context, admission agentrun.Record, run runstate.Run) error {
	now := e.now().UTC()
	claimed, err := e.state.Claim(ctx, run.ID, e.workerID, now, e.leaseTTL)
	if err != nil {
		return fmt.Errorf("%w: claim: %v", ErrPersonaRunExecutorBusy, err)
	}
	ctx = WithPersonaBackgroundAdmission(ctx, admission)
	work, err := e.work.BuildPersonaRunModelWork(ctx, admission, claimed)
	if err != nil {
		return e.fail(ctx, claimed, "CONTEXT_UNAVAILABLE", err)
	}
	if work.Request.Task.TenantID != claimed.TenantID || work.Request.Task.TaskID != claimed.ID || work.Request.Task.AgentID != claimed.AgentDigest {
		return e.fail(ctx, claimed, "MODEL_BINDING_INVALID", ErrPersonaRunOutputRejected)
	}
	if err := bindPersonaRunModelStep(&work.Request, claimed.ID, 1); err != nil {
		return e.fail(ctx, claimed, "MODEL_BINDING_INVALID", err)
	}
	if e.tools != nil {
		schemas, schemaErr := e.tools.ToolSchemas(ctx, admission, claimed)
		if schemaErr != nil {
			return e.fail(ctx, claimed, "TOOL_POLICY_UNAVAILABLE", schemaErr)
		}
		if err := validatePersonaRunToolProjection(schemas); err != nil {
			return e.fail(ctx, claimed, "TOOL_POLICY_UNAVAILABLE", err)
		}
		work.Request.Model.Tools = schemas
	} else if len(work.Request.Model.Tools) > 0 {
		return e.fail(ctx, claimed, "TOOL_POLICY_UNAVAILABLE", ErrPersonaRunOutputRejected)
	}
	modelResult, err := e.model.Execute(ctx, work.Request)
	if err != nil {
		return e.fail(ctx, claimed, "MODEL_UNAVAILABLE", err)
	}
	if modelResult.Result.Failure != nil || modelResult.Result.Refusal != nil {
		return e.fail(ctx, claimed, "MODEL_REFUSED_OR_INCOMPLETE", ErrPersonaRunModelFailure)
	}
	claimed, err = e.checkRequestedActions(ctx, admission, claimed, work, modelResult.Result, 1)
	if err != nil {
		return err
	}
	modelAttempt := uint32(1)
	if modelResult.Result.Finish == agentmodel.FinishToolCalls {
		if e.tools == nil || strings.TrimSpace(modelResult.Result.Text) != "" || len(modelResult.Result.ToolProposals) != 1 {
			return e.fail(ctx, claimed, "MODEL_REFUSED_OR_INCOMPLETE", ErrPersonaRunModelFailure)
		}
		modelDigest, digestErr := personaRunAnyResultDigest(modelResult.Result)
		if digestErr != nil {
			return e.fail(ctx, claimed, "MODEL_RESULT_INVALID", digestErr)
		}
		claimed, err = e.state.Checkpoint(ctx, claimed.ID, e.workerID, claimed.Fence, claimed.Version, runstate.PhaseModelCall, 1, work.Request.StepID, modelDigest, e.now().UTC())
		if err != nil {
			return fmt.Errorf("%w: record proposal result: %v", ErrPersonaRunExecutorUnavailable, err)
		}
		proposal := modelResult.Result.ToolProposals[0]
		if !personaRunToolProposalProjected(work.Request.Model.Tools, proposal) {
			return e.fail(ctx, claimed, "TOOL_PROPOSAL_UNAVAILABLE", errPersonaRuntimeTools)
		}
		argsDigest := personaRunBytesDigest(proposal.Arguments)
		effectID, idemKey := personaRunToolEffectIdentity(claimed.ID, work.Request.StepID, proposal.ID)
		beforeEffect := claimed
		claimed, err = e.state.BeginEffect(ctx, claimed.ID, e.workerID, effectID, idemKey, argsDigest, claimed.Fence, claimed.Version, e.now().UTC())
		if err != nil {
			return e.fail(ctx, beforeEffect, "TOOL_ADMISSION_FAILED", err)
		}
		toolOutput, resultRef, resultDigest, toolErr := e.tools.Execute(ctx, admission, claimed, proposal)
		if toolErr != nil {
			claimed, err = e.state.ResolveEffect(ctx, claimed.ID, e.workerID, effectID, claimed.Fence, claimed.Version, runstate.EffectNotApplied, "", "", e.now().UTC())
			if err != nil {
				return fmt.Errorf("%w: persist refused tool outcome: %v", ErrPersonaRunExecutorUnavailable, err)
			}
			return e.fail(ctx, claimed, "TOOL_EXECUTION_FAILED", toolErr)
		}
		claimed, err = e.state.ResolveEffect(ctx, claimed.ID, e.workerID, effectID, claimed.Fence, claimed.Version, runstate.EffectApplied, resultRef, resultDigest, e.now().UTC())
		if err != nil {
			return fmt.Errorf("%w: persist tool outcome: %v", ErrPersonaRunExecutorUnavailable, err)
		}
		continuation, workErr := e.work.BuildPersonaRunModelWork(ctx, admission, claimed)
		if workErr != nil {
			return e.fail(ctx, claimed, "CONTEXT_UNAVAILABLE", workErr)
		}
		if continuation.Request.Task.TenantID != claimed.TenantID || continuation.Request.Task.TaskID != claimed.ID || continuation.Request.Task.AgentID != claimed.AgentDigest || continuation.Request.ToolResultClass == "" {
			return e.fail(ctx, claimed, "MODEL_BINDING_INVALID", ErrPersonaRunOutputRejected)
		}
		if work.Request.Model.ActionPolicy != nil && continuation.Request.Model.ActionPolicy == nil {
			return e.fail(ctx, claimed, "MODEL_BINDING_INVALID", ErrPersonaRunOutputRejected)
		}
		if err := bindPersonaRunModelStep(&continuation.Request, claimed.ID, 2); err != nil {
			return e.fail(ctx, claimed, "MODEL_BINDING_INVALID", err)
		}
		if err := appendPersonaRunToolContinuation(&continuation.Request, proposal, toolOutput); err != nil {
			return e.fail(ctx, claimed, "MODEL_BINDING_INVALID", err)
		}
		continuation.Request.Model.Tools = nil
		modelResult, err = e.model.Execute(ctx, continuation.Request)
		if err != nil {
			return e.fail(ctx, claimed, "MODEL_UNAVAILABLE", err)
		}
		if modelResult.Result.Failure != nil || modelResult.Result.Refusal != nil {
			return e.fail(ctx, claimed, "MODEL_REFUSED_OR_INCOMPLETE", ErrPersonaRunModelFailure)
		}
		claimed, err = e.checkRequestedActions(ctx, admission, claimed, continuation, modelResult.Result, 2)
		if err != nil {
			return err
		}
		if modelResult.Result.Finish != agentmodel.FinishComplete || strings.TrimSpace(modelResult.Result.Text) == "" || len(modelResult.Result.ToolProposals) != 0 {
			return e.fail(ctx, claimed, "MODEL_REFUSED_OR_INCOMPLETE", ErrPersonaRunModelFailure)
		}
		work = continuation
		modelAttempt = 2
	} else if modelResult.Result.Finish != agentmodel.FinishComplete || strings.TrimSpace(modelResult.Result.Text) == "" || len(modelResult.Result.ToolProposals) != 0 {
		return e.fail(ctx, claimed, "MODEL_REFUSED_OR_INCOMPLETE", ErrPersonaRunModelFailure)
	}
	resultDigest, err := personaRunResultDigest(modelResult.Result)
	if err != nil {
		return e.fail(ctx, claimed, "MODEL_RESULT_INVALID", err)
	}
	claimed, err = e.state.Checkpoint(ctx, claimed.ID, e.workerID, claimed.Fence, claimed.Version, runstate.PhaseModelCall, modelAttempt, work.Request.StepID, resultDigest, e.now().UTC())
	if err != nil {
		return fmt.Errorf("%w: record model result: %v", ErrPersonaRunExecutorUnavailable, err)
	}
	persisted, err := e.output.ValidateAndPersistPersonaOutput(ctx, admission, claimed, modelResult.Result)
	if err != nil {
		return e.fail(ctx, claimed, "OUTPUT_REJECTED", fmt.Errorf("%w: %v", ErrPersonaRunOutputRejected, err))
	}
	identity := persisted.Identity()
	if admission.Request.Persona == nil || identity.TenantID != admission.Request.Source.TenantID || identity.InvocationID != admission.Request.Source.Key || identity.PostID != admission.Request.Source.Ref || identity.InvokerID != admission.Request.Principal.InvokerID || identity.ConversationID != admission.Request.Audience.ID || identity.ThreadID != admission.Request.Context.ID || identity.PersonaID != admission.Request.Persona.ID || identity.PersonaVersion != admission.Request.Persona.Version || identity.InstallationID != admission.Request.InstallationID || identity.OutputID == "" || persisted.Digest() == "" {
		return e.fail(ctx, claimed, "OUTPUT_BINDING_INVALID", ErrPersonaRunOutputRejected)
	}
	claimed, err = e.state.Checkpoint(ctx, claimed.ID, e.workerID, claimed.Fence, claimed.Version, runstate.PhaseValidation, 1, identity.OutputID, persisted.SemanticDigest(), e.now().UTC())
	if err != nil {
		return fmt.Errorf("%w: checkpoint validated result: %v", ErrPersonaRunExecutorUnavailable, err)
	}
	receipt, err := e.deliver(ctx, admission, claimed, persisted)
	if err != nil {
		return e.fail(ctx, claimed, "DELIVERY_FAILED", err)
	}
	if !validPersonaRunReplyReceipt(receipt) {
		return e.fail(ctx, claimed, "DELIVERY_RECEIPT_INVALID", ErrPersonaRunDeliveryFailure)
	}
	deliveryDigest, err := personaRunDeliveryDigest(receipt)
	if err != nil {
		return fmt.Errorf("%w: delivery receipt invalid", ErrPersonaRunExecutorUnavailable)
	}
	_, err = e.state.Checkpoint(ctx, claimed.ID, e.workerID, claimed.Fence, claimed.Version, runstate.PhaseDelivery, 1, admission.ID, deliveryDigest, e.now().UTC())
	if err != nil {
		return fmt.Errorf("%w: checkpoint delivery: %v", ErrPersonaRunExecutorUnavailable, err)
	}
	return nil
}

func (e *personaAdmittedRunExecutor) checkRequestedActions(ctx context.Context, admission agentrun.Record, run runstate.Run, work PersonaRunModelWork, result agentmodel.ModelResult, attempt uint32) (runstate.Run, error) {
	if work.Request.Model.ActionPolicy == nil {
		return run, nil
	}
	policy := *work.Request.Model.ActionPolicy
	if admission.Request.Persona == nil || policy.ProfileDigest != admission.Request.Persona.Digest {
		return run, e.fail(ctx, run, "MODEL_BINDING_INVALID", ErrPersonaRunOutputRejected)
	}
	allowed, err := agentmodel.CheckRequestedActions(policy, result.RequestedActions)
	if err != nil {
		return run, e.fail(ctx, run, "MODEL_RESULT_INVALID", err)
	}
	if allowed {
		return run, nil
	}
	digest, err := personaRunAnyResultDigest(result)
	if err != nil {
		return run, e.fail(ctx, run, "MODEL_RESULT_INVALID", err)
	}
	run, err = e.state.Checkpoint(ctx, run.ID, e.workerID, run.Fence, run.Version, runstate.PhaseModelCall, attempt, work.Request.StepID, digest, e.now().UTC())
	if err != nil {
		return run, err
	}
	return run, e.fail(ctx, run, "OUT_OF_SCOPE", ErrPersonaRunOutputRejected)
}

func (e *personaAdmittedRunExecutor) deliver(ctx context.Context, admission agentrun.Record, run runstate.Run, persisted agentsecurity.FinalOutputPersistence) (PersonaReplyDeliveryReceipt, error) {
	identity := persisted.Identity()
	if principal, ok := personaRunChatPrincipal(ctx, identity.TenantID, identity.InvokerID); ok {
		return e.reply.Deliver(ctx, PersonaReplyDeliveryRequest{Principal: principal, Output: persisted, IdempotencyKey: admission.ID})
	}
	if _, hasPrincipal := trust.FromContext(ctx); hasPrincipal || isNilPersonaOutputPort(e.backgroundReply) {
		return PersonaReplyDeliveryReceipt{}, ErrPersonaRunOutputRejected
	}
	return e.backgroundReply.DeliverBackgroundPersonaReply(ctx, admission, run, persisted)
}

func validPersonaRunReplyReceipt(receipt PersonaReplyDeliveryReceipt) bool {
	if receipt.Public && !receipt.Private {
		return receipt.PublicPostID != "" && receipt.EphemeralPostID == ""
	}
	if receipt.Private && !receipt.Public {
		return receipt.EphemeralPostID != "" && receipt.PublicPostID == ""
	}
	return false
}

func (e *personaAdmittedRunExecutor) fail(ctx context.Context, run runstate.Run, code string, cause error) error {
	// Terminal runstate failures cannot currently resume under the same durable
	// invocation. Keep Retryable false so callers require a fresh invocation.
	_, err := e.state.Fail(ctx, run.ID, e.workerID, code, false, run.Fence, run.Version, e.now().UTC())
	if err != nil {
		return fmt.Errorf("%w: terminal failure %s could not be persisted", ErrPersonaRunExecutorUnavailable, code)
	}
	kind := ErrPersonaRunOutputRejected
	switch code {
	case "CONTEXT_UNAVAILABLE", "MODEL_BINDING_INVALID":
		kind = ErrPersonaRunExecutorUnavailable
	case "MODEL_UNAVAILABLE", "MODEL_REFUSED_OR_INCOMPLETE", "MODEL_RESULT_INVALID":
		kind = ErrPersonaRunModelFailure
	case "DELIVERY_FAILED":
		kind = ErrPersonaRunDeliveryFailure
	}
	_ = cause // Details may contain business data and are deliberately not exposed.
	return &PersonaRunFailure{Code: code, Retryable: false, kind: kind}
}

func personaRunResultDigest(result agentmodel.ModelResult) (string, error) {
	if strings.TrimSpace(result.Text) == "" {
		return "", ErrPersonaRunModelFailure
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func personaRunAnyResultDigest(result agentmodel.ModelResult) (string, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return personaRunBytesDigest(encoded), nil
}

func personaRunDeliveryDigest(receipt PersonaReplyDeliveryReceipt) (string, error) {
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func personaRunChatPrincipal(ctx context.Context, tenant, subject string) (chatcore.Principal, bool) {
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman ||
		principal.Tenant().String() != tenant || principal.Subject() != subject {
		return chatcore.Principal{}, false
	}
	return chatcore.Principal{TenantID: tenant, SubjectID: subject}, true
}

var _ agentinvoke.RunStarter = (*PersonaRunStarter)(nil)
