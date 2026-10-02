package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
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
type PersonaRunModelWork struct {
	Request AgentModelExecutorRequest
	// Facts is the agent's own facts and readable documents this request carries,
	// when it carries them (AGENTUX-076). General says the agent may answer from
	// general knowledge where no document covers a question.
	Facts    personaAgentFacts
	HasFacts bool
	General  bool
}

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
	agentUXSpeedSetRunID(ctx, admission.ID)
	done := agentUXSpeedStage(ctx, "run_start")
	run, err := e.state.Start(ctx, admission)
	done()
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
	case "MODEL_UNAVAILABLE", "MODEL_REFUSED_OR_INCOMPLETE", "MODEL_RESULT_INVALID", "MODEL_TIMEOUT", "MODEL_LIMIT", "ANSWER_INTERRUPTED":
		kind = ErrPersonaRunModelFailure
	case "DELIVERY_FAILED":
		kind = ErrPersonaRunDeliveryFailure
	}
	return &PersonaRunFailure{Code: run.TerminalCode, Retryable: run.Retryable, kind: kind}
}

func (e *personaAdmittedRunExecutor) execute(ctx context.Context, admission agentrun.Record, run runstate.Run) error {
	now := e.now().UTC()
	done := agentUXSpeedStage(ctx, "claim")
	claimed, err := e.qualityStateChange(ctx, run, func() (runstate.Run, error) {
		now = e.now().UTC()
		return e.state.Claim(ctx, run.ID, e.workerID, now, e.leaseTTL)
	})
	done()
	if err != nil {
		return fmt.Errorf("%w: claim: %v", ErrPersonaRunExecutorBusy, err)
	}
	defer e.startLeaseRenewal(ctx, claimed)()
	ctx = WithPersonaBackgroundAdmission(ctx, admission)
	noteStep := personaRunStepNoter(ctx, admission)
	noteStep(personaStepReadingQuestion, "")
	done = agentUXSpeedStage(ctx, "build_model_work")
	work, err := e.work.BuildPersonaRunModelWork(ctx, admission, claimed)
	done()
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
		done = agentUXSpeedStage(ctx, "tool_schemas")
		schemas, schemaErr := e.tools.ToolSchemas(ctx, admission, claimed)
		done()
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
	done = agentUXSpeedStage(ctx, "model_execute")
	// Documents a search returned to this run; the ones the sealed answer
	// cites are listed as its sources at delivery.
	var searchedDocuments []personaQualitySearchedDocument
	searchRan := false
	modelResult, claimed, err := e.executeQualityModel(ctx, claimed, work.Request, admission)
	done()
	if err != nil {
		return e.fail(ctx, claimed, personaQualityModelFailureCode(modelResult, err), err)
	}
	if modelResult.Result.Failure != nil || modelResult.Result.Refusal != nil {
		if modelResult.Result.Failure != nil {
			return e.fail(ctx, claimed, personaQualityModelFailureCode(modelResult, nil), personaRunModelShapeCause("first turn", modelResult.Result))
		}
		return e.fail(ctx, claimed, "MODEL_REFUSED_OR_INCOMPLETE", personaRunModelShapeCause("first turn", modelResult.Result))
	}
	claimed, err = e.checkRequestedActions(ctx, admission, claimed, work, modelResult.Result, 1)
	if err != nil {
		return err
	}
	modelAttempt := uint32(1)
	if modelResult.Result.Finish == agentmodel.FinishToolCalls {
		// Text that accompanies a proposal is an announcement of the search,
		// not an answer. It is never delivered, so it is dropped here rather
		// than failing the run on a model that narrates its tool use.
		modelResult.Result.Text = ""
		if e.tools == nil || len(modelResult.Result.ToolProposals) != 1 {
			return e.fail(ctx, claimed, "MODEL_REFUSED_OR_INCOMPLETE", personaRunModelShapeCause("tool proposal turn", modelResult.Result))
		}
		modelDigest, digestErr := personaRunAnyResultDigest(modelResult.Result)
		if digestErr != nil {
			return e.fail(ctx, claimed, "MODEL_RESULT_INVALID", digestErr)
		}
		done = agentUXSpeedStage(ctx, "checkpoint_model_proposal")
		claimed, err = e.qualityCheckpoint(ctx, claimed.ID, e.workerID, claimed.Fence, claimed.Version, runstate.PhaseModelCall, 1, work.Request.StepID, modelDigest, e.now().UTC())
		done()
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
		done = agentUXSpeedStage(ctx, "begin_effect")
		claimed, err = e.qualityStateChange(ctx, claimed, func() (runstate.Run, error) {
			return e.state.BeginEffect(ctx, claimed.ID, e.workerID, effectID, idemKey, argsDigest, claimed.Fence, claimed.Version, e.now().UTC())
		})
		done()
		if err != nil {
			return e.fail(ctx, beforeEffect, "TOOL_ADMISSION_FAILED", err)
		}
		noteStep(personaStepForTool(proposal.Name, proposal.Arguments))
		done = agentUXSpeedStage(ctx, "tool_execute")
		toolOutput, resultRef, resultDigest, toolErr := e.tools.Execute(ctx, admission, claimed, proposal)
		searchedDocuments = personaQualitySearchedDocuments(toolOutput)
		done()
		noteStep(personaStepForDocuments(searchedDocuments))
		if toolErr != nil {
			claimed, err = e.qualityStateChange(ctx, claimed, func() (runstate.Run, error) {
				return e.state.ResolveEffect(ctx, claimed.ID, e.workerID, effectID, claimed.Fence, claimed.Version, runstate.EffectNotApplied, "", "", e.now().UTC())
			})
			if err != nil {
				return fmt.Errorf("%w: persist refused tool outcome: %v", ErrPersonaRunExecutorUnavailable, err)
			}
			return e.fail(ctx, claimed, "TOOL_EXECUTION_FAILED", toolErr)
		}
		done = agentUXSpeedStage(ctx, "resolve_effect")
		claimed, err = e.qualityStateChange(ctx, claimed, func() (runstate.Run, error) {
			return e.state.ResolveEffect(ctx, claimed.ID, e.workerID, effectID, claimed.Fence, claimed.Version, runstate.EffectApplied, resultRef, resultDigest, e.now().UTC())
		})
		done()
		if err != nil {
			return fmt.Errorf("%w: persist tool outcome: %v", ErrPersonaRunExecutorUnavailable, err)
		}
		// A search that finds nothing is a normal result (AGENTUX-076): the model
		// is told so and writes its answer. Only a search that could not run ends
		// the run.
		noMatches := false
		searchRan = proposal.Name == personaDocumentSearchTool || proposal.Name == personaWorkspaceSearchTool
		if searchRan {
			switch code := personaQualitySearchFailure(toolOutput); code {
			case "":
			case "NO_RESULTS":
				noMatches = true
			default:
				return e.fail(ctx, claimed, code, nil)
			}
		}
		done = agentUXSpeedStage(ctx, "build_continuation")
		continuation, workErr := e.work.BuildPersonaRunModelWork(ctx, admission, claimed)
		done()
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
		if noMatches {
			if err := appendPersonaServerInstruction(&continuation.Request, personaNoResultsInstruction, "no-matches"); err != nil {
				return e.fail(ctx, claimed, "MODEL_BINDING_INVALID", err)
			}
		}
		continuation.Request.Model.Tools = nil
		noteStep(personaStepWriting, "")
		done = agentUXSpeedStage(ctx, "model_execute")
		modelResult, claimed, err = e.executeQualityModel(ctx, claimed, continuation.Request, admission)
		done()
		if err != nil {
			return e.fail(ctx, claimed, personaQualityModelFailureCode(modelResult, err), err)
		}
		if modelResult.Result.Failure != nil || modelResult.Result.Refusal != nil {
			if modelResult.Result.Failure != nil {
				return e.fail(ctx, claimed, personaQualityModelFailureCode(modelResult, nil), personaRunModelShapeCause("continuation turn", modelResult.Result))
			}
			return e.fail(ctx, claimed, "MODEL_REFUSED_OR_INCOMPLETE", personaRunModelShapeCause("continuation turn", modelResult.Result))
		}
		claimed, err = e.checkRequestedActions(ctx, admission, claimed, continuation, modelResult.Result, 2)
		if err != nil {
			return err
		}
		if modelResult.Result.Finish != agentmodel.FinishComplete || strings.TrimSpace(modelResult.Result.Text) == "" || len(modelResult.Result.ToolProposals) != 0 {
			return e.fail(ctx, claimed, "MODEL_REFUSED_OR_INCOMPLETE", personaRunModelShapeCause("continuation finish", modelResult.Result))
		}
		work = continuation
		modelAttempt = 2
	} else if modelResult.Result.Finish != agentmodel.FinishComplete || strings.TrimSpace(modelResult.Result.Text) == "" || len(modelResult.Result.ToolProposals) != 0 {
		return e.fail(ctx, claimed, "MODEL_REFUSED_OR_INCOMPLETE", personaRunModelShapeCause("first turn finish", modelResult.Result))
	}
	// A general-purpose agent says when an answer is from no document; the marker
	// becomes the typed flag and is not part of the answer.
	var saidNotFromDocuments bool
	modelResult.Result.Text, saidNotFromDocuments = personaStripNotFromDocuments(modelResult.Result.Text)
	resultDigest, err := personaRunResultDigest(modelResult.Result)
	if err != nil {
		return e.fail(ctx, claimed, "MODEL_RESULT_INVALID", err)
	}
	noteStep(personaStepWriting, "")
	done = agentUXSpeedStage(ctx, "checkpoint_model_result")
	claimed, err = e.qualityCheckpoint(ctx, claimed.ID, e.workerID, claimed.Fence, claimed.Version, runstate.PhaseModelCall, modelAttempt, work.Request.StepID, resultDigest, e.now().UTC())
	done()
	if err != nil {
		return fmt.Errorf("%w: record model result: %v", ErrPersonaRunExecutorUnavailable, err)
	}
	done = agentUXSpeedStage(ctx, "validate_output")
	if !personaReplyHasStatement(modelResult.Result.Text, personaReplyStatementTitles(searchedDocuments)...) {
		// CHATBUG-018: a reply that is only a document title (or a marker that
		// renders as one) answers nothing; it is refused, not posted. CHATBUG-049:
		// the run ends with a code of its own, so the card says the agent answered
		// with only a document's name instead of calling the agent unavailable.
		// When the run searched documents the server says what the agent can read
		// in one sentence of its own and delivers that (CHATBUG-049).
		// The model is asked once more, from the documents' content, before the
		// server's sentence stands in (chatbug049_regenerate.go).
		regenerated, next, again, regenErr := e.regenerateTitleOnly(ctx, admission, claimed, work, modelAttempt+1, searchedDocuments)
		claimed = next
		if regenErr != nil {
			done()
			return regenErr
		}
		if again {
			modelResult.Result = regenerated
		} else {
			composed := personaComposedListReply(searchedDocuments)
			if composed == "" {
				done()
				return e.fail(ctx, claimed, chatcore.AgentAnswerTitleOnlyCode, fmt.Errorf("%w: the reply states nothing beyond a document title", ErrPersonaRunOutputRejected))
			}
			modelResult.Result.Text = composed
		}
	}
	persisted, err := e.output.ValidateAndPersistPersonaOutput(ctx, admission, claimed, modelResult.Result)
	done()
	if err != nil {
		return e.fail(ctx, claimed, "OUTPUT_REJECTED", fmt.Errorf("%w: %v", ErrPersonaRunOutputRejected, err))
	}
	identity := persisted.Identity()
	if admission.Request.Persona == nil || identity.TenantID != admission.Request.Source.TenantID || identity.InvocationID != admission.Request.Source.Key || identity.PostID != admission.Request.Source.Ref || identity.InvokerID != admission.Request.Principal.InvokerID || identity.ConversationID != admission.Request.Audience.ID || identity.ThreadID != admission.Request.Context.ID || identity.PersonaID != admission.Request.Persona.ID || identity.PersonaVersion != admission.Request.Persona.Version || identity.InstallationID != admission.Request.InstallationID || identity.OutputID == "" || persisted.Digest() == "" {
		return e.fail(ctx, claimed, "OUTPUT_BINDING_INVALID", ErrPersonaRunOutputRejected)
	}
	done = agentUXSpeedStage(ctx, "checkpoint_validation")
	claimed, err = e.qualityCheckpoint(ctx, claimed.ID, e.workerID, claimed.Fence, claimed.Version, runstate.PhaseValidation, 1, identity.OutputID, persisted.SemanticDigest(), e.now().UTC())
	done()
	if err != nil {
		return fmt.Errorf("%w: checkpoint validated result: %v", ErrPersonaRunExecutorUnavailable, err)
	}
	done = agentUXSpeedStage(ctx, "delivery")
	extra := personaDeliveryExtra{General: work.General, SaidSo: saidNotFromDocuments, Searched: searchRan}
	if work.HasFacts {
		extra.Named = personaFactsNamedDocuments(modelResult.Result.Text, work.Facts.Documents)
	}
	receipt, err := e.deliverWith(ctx, admission, claimed, persisted, extra, searchedDocuments...)
	done()
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
	done = agentUXSpeedStage(ctx, "checkpoint_delivery")
	_, err = e.qualityCheckpoint(withPersonaRunReplyDelivered(ctx, claimed.AdmissionID), claimed.ID, e.workerID, claimed.Fence, claimed.Version, runstate.PhaseDelivery, 1, admission.ID, deliveryDigest, e.now().UTC())
	done()
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
	run, err = e.qualityCheckpoint(ctx, run.ID, e.workerID, run.Fence, run.Version, runstate.PhaseModelCall, attempt, work.Request.StepID, digest, e.now().UTC())
	if err != nil {
		return run, err
	}
	return run, e.fail(ctx, run, "OUT_OF_SCOPE", ErrPersonaRunOutputRejected)
}

func (e *personaAdmittedRunExecutor) deliver(ctx context.Context, admission agentrun.Record, run runstate.Run, persisted agentsecurity.FinalOutputPersistence, searched ...personaQualitySearchedDocument) (PersonaReplyDeliveryReceipt, error) {
	return e.deliverWith(ctx, admission, run, persisted, personaDeliveryExtra{}, searched...)
}

func (e *personaAdmittedRunExecutor) deliverWith(ctx context.Context, admission agentrun.Record, run runstate.Run, persisted agentsecurity.FinalOutputPersistence, extra personaDeliveryExtra, searched ...personaQualitySearchedDocument) (PersonaReplyDeliveryReceipt, error) {
	identity := persisted.Identity()
	documents, citationDetails := personaQualityCitedDocuments(searched, persisted.Citations())
	notFromDocuments := personaNotFromDocuments(extra.General, extra.SaidSo, extra.Searched, documents)
	// Listed documents the answer names are linked under it, from the ids the
	// server read. A document already cited is not listed twice.
	cited := make(map[string]bool, len(documents))
	for _, document := range documents {
		cited[document.Reference.DocumentID] = true
	}
	for _, named := range extra.Named {
		if !cited[named.Reference.DocumentID] {
			documents = append(documents, named)
		}
	}
	if principal, ok := personaRunChatPrincipal(ctx, identity.TenantID, identity.InvokerID); ok {
		if admission.Request.Persona != nil {
			// A reply that may not be posted to the whole audience goes to the
			// invoker's own conversation with the agent. That conversation is
			// selected from this admitted invocation, never from the request,
			// and only while the run it belongs to is still within its deadline.
			invocation := personaRunInvocation(admission.Request)
			invocation.Grant.ExpiresAt = admission.Request.Deadline
			ctx = WithPersonaDMInvocation(ctx, invocation)
		}
		return e.reply.Deliver(ctx, PersonaReplyDeliveryRequest{Principal: principal, Output: persisted, IdempotencyKey: admission.ID, Documents: documents, CitationDetails: citationDetails, NotFromDocuments: notFromDocuments})
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
	refusal := personaRunFailureRefusal(code, cause)
	if refusal.Gate == runstate.FailureGateDeliveryAudience {
		code = "DELIVERY_AUDIENCE_DENIED"
	}
	retryable := chatcore.AgentAnswerFailureFor("en-US", "", code).Retryable
	_, err := e.qualityStateChange(ctx, run, func() (runstate.Run, error) {
		return e.state.FailWithRefusal(ctx, run.ID, e.workerID, code, retryable, refusal, run.Fence, run.Version, e.now().UTC())
	})
	if err != nil {
		return fmt.Errorf("%w: terminal failure %s could not be persisted", ErrPersonaRunExecutorUnavailable, code)
	}
	kind := ErrPersonaRunOutputRejected
	switch code {
	case "CONTEXT_UNAVAILABLE", "MODEL_BINDING_INVALID":
		kind = ErrPersonaRunExecutorUnavailable
	case "MODEL_UNAVAILABLE", "MODEL_REFUSED_OR_INCOMPLETE", "MODEL_RESULT_INVALID", "MODEL_TIMEOUT", "MODEL_LIMIT", "ANSWER_INTERRUPTED":
		kind = ErrPersonaRunModelFailure
	case "DELIVERY_FAILED":
		kind = ErrPersonaRunDeliveryFailure
	}
	slog.WarnContext(ctx, "hcmnext.persona_run_failed", "run_id", run.ID, "code", code, "gate", refusal.Gate, "owner", refusal.Owner, "location", refusal.Location)
	// Details may contain business data and are deliberately not exposed unless
	// a local diagnostic session explicitly opts in.
	if cause != nil && os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") == "1" {
		slog.WarnContext(ctx, "hcmnext.persona_run_failed", "run_id", run.ID, "code", code, "cause", cause.Error())
	}
	return &PersonaRunFailure{Code: code, Retryable: retryable, kind: kind}
}

func personaRunFailureRefusal(code string, cause error) runstate.FailureRefusal {
	gate := runstate.FailureGateOutputSchema
	switch code {
	case "CONTEXT_UNAVAILABLE":
		gate = runstate.FailureGateAuthority
	case "MODEL_BINDING_INVALID":
		gate = runstate.FailureGateModelRoute
	case "MODEL_UNAVAILABLE":
		gate = runstate.FailureGateModelCall
	case "MODEL_TIMEOUT":
		gate = runstate.FailureGateDeadline
	case "MODEL_LIMIT":
		gate = runstate.FailureGateBudget
	case "ANSWER_INTERRUPTED":
		gate = runstate.FailureGateModelCall
	case "MODEL_REFUSED_OR_INCOMPLETE", "MODEL_RESULT_INVALID":
		gate = runstate.FailureGateModelOutput
	case "TOOL_POLICY_UNAVAILABLE":
		gate = runstate.FailureGateToolScope
	case "TOOL_ADMISSION_FAILED", "TOOL_EXECUTION_FAILED":
		gate = runstate.FailureGateToolCall
	case "OUTPUT_REJECTED", "OUTPUT_BINDING_INVALID", chatcore.AgentAnswerTitleOnlyCode:
		gate = runstate.FailureGateOutputGrounding
	case "DELIVERY_FAILED", "DELIVERY_RECEIPT_INVALID":
		gate = runstate.FailureGateDeliveryWrite
	}
	if errors.Is(cause, errPersonaRuntimeTools) {
		gate = runstate.FailureGateToolScope
	}
	if errors.Is(cause, ErrPersonaRunOutputValidatorUnavailable) {
		gate = runstate.FailureGateOutputGrounding
	}
	if errors.Is(cause, errPersonaPrivateChatScope) {
		gate = runstate.FailureGateDeliveryAudience
	}
	location := personaRunFailureLocation(cause)
	if location == "" {
		_, file, line, ok := runtime.Caller(1)
		if ok {
			location = fmt.Sprintf("%s:%d", filepath.Base(file), line)
		} else {
			location = "unknown:0"
		}
	}
	return runstate.FailureRefusal{Gate: gate, Owner: "internal/application", Location: location}
}

func personaRunFailureLocation(cause error) string {
	if cause == nil {
		return ""
	}
	text := cause.Error()
	close := strings.LastIndex(text, ")")
	open := strings.LastIndex(text[:max(0, close)], "(")
	if open < 0 || close < 0 || close <= open+1 {
		return ""
	}
	location := text[open+1 : close]
	if strings.ContainsAny(location, " \t\r\n") || !strings.Contains(location, ":") {
		return ""
	}
	return location
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

// personaRunModelShapeCause names which part of a model result made the run
// stop, without carrying any model text.
func personaRunModelShapeCause(stage string, result agentmodel.ModelResult) error {
	failure, refusal := "", ""
	if result.Failure != nil {
		failure = fmt.Sprintf("%+v", *result.Failure)
	}
	if result.Refusal != nil {
		refusal = fmt.Sprintf("%+v", *result.Refusal)
	}
	return fmt.Errorf("%w: %s: finish=%v text_bytes=%d tool_proposals=%d failure=%q refusal=%q", ErrPersonaRunModelFailure, stage, result.Finish, len(result.Text), len(result.ToolProposals), failure, refusal)
}
