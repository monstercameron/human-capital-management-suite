package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

var errPersonaRunModelWorker = errors.New("application: persona run model worker unavailable")

// PersonaRunSecurityFence binds a run to an active, independently issued
// security lease and fences each bounded execution step.
type PersonaRunSecurityFence interface {
	Bind(agentsecurity.PersonaRunID, string, agentsecurity.KillSwitchLeaseID) error
	RunStep(context.Context, agentsecurity.PersonaRunID, string, func(context.Context) error) (agentsecurity.Fallback, error)
}

// PersonaRunSecurityLeaseResolver resolves the kill-switch lease for one
// admitted run. It must not derive that lease from provider credentials.
type PersonaRunSecurityLeaseResolver interface {
	ResolvePersonaRunSecurityLease(context.Context, agentrun.Record, runstate.Run) (agentsecurity.KillSwitchLeaseID, error)
}

// PersonaRunTenantRuntimeFactory returns per-tenant durable stores and a
// current-authority rechecker while reusing tenant-aware model/output ports.
type PersonaRunTenantRuntimeFactory interface {
	ForPersonaRunTenant(context.Context, string) (PersonaRunStarterConfig, error)
}

// PersonaRunTenantRuntimeValidator proves that static production worker ports
// are complete before the server advertises persona invocation capability.
type PersonaRunTenantRuntimeValidator interface {
	ValidatePersonaRunTenantRuntime() error
}

// PersonaRunModelWorkerConfig composes the existing durable starter with
// authoritative model work, output persistence, delivery, and step fencing.
type PersonaRunModelWorkerConfig struct {
	Tenants PersonaRunTenantRuntimeFactory
	Fence   PersonaRunSecurityFence
	Leases  PersonaRunSecurityLeaseResolver
}

// PersonaRunModelWorker admits and executes persona chat invocations through
// the durable runstate service and shared agent-model gateway.
type PersonaRunModelWorker struct {
	tenants PersonaRunTenantRuntimeFactory
	fence   PersonaRunSecurityFence
	leases  PersonaRunSecurityLeaseResolver
	// cost holds every run to its agent's spend limits once bound (AGENTCOST-006).
	cost personaRunCostBinding
}

type personaRunStepGeneration struct {
	fence   uint64
	version uint64
}

type personaRunStepIdentityState struct {
	mu   sync.RWMutex
	runs map[string]personaRunStepGeneration
}

func (s *personaRunStepIdentityState) set(run runstate.Run) {
	if s == nil || run.ID == "" {
		return
	}
	s.mu.Lock()
	if s.runs == nil {
		s.runs = make(map[string]personaRunStepGeneration)
	}
	s.runs[run.ID] = personaRunStepGeneration{fence: run.Fence, version: run.Version}
	s.mu.Unlock()
}

func (s *personaRunStepIdentityState) get(runID string) (personaRunStepGeneration, bool) {
	if s == nil || runID == "" {
		return personaRunStepGeneration{}, false
	}
	s.mu.RLock()
	generation, ok := s.runs[runID]
	s.mu.RUnlock()
	return generation, ok
}

// NewPersonaRunModelWorker requires a complete starter, a boundable security
// fence, and a trusted lease resolver. It never supplies model or policy defaults.
func NewPersonaRunModelWorker(cfg PersonaRunModelWorkerConfig) (*PersonaRunModelWorker, error) {
	if isNilPersonaOutputPort(cfg.Tenants) || isNilPersonaOutputPort(cfg.Fence) || isNilPersonaOutputPort(cfg.Leases) {
		return nil, errPersonaRunModelWorker
	}
	validator, ok := cfg.Tenants.(PersonaRunTenantRuntimeValidator)
	if !ok || isNilPersonaOutputPort(validator) || validator.ValidatePersonaRunTenantRuntime() != nil {
		return nil, errPersonaRunModelWorker
	}
	return &PersonaRunModelWorker{tenants: cfg.Tenants, fence: cfg.Fence, leases: cfg.Leases}, nil
}

// Start admits and executes one invocation. Replays use the same durable run ID.
func (w *PersonaRunModelWorker) Start(ctx context.Context, request agentinvoke.RunRequest) error {
	if w == nil || isNilPersonaOutputPort(w.tenants) || isNilPersonaOutputPort(w.fence) || isNilPersonaOutputPort(w.leases) || ctx == nil {
		return errPersonaRunModelWorker
	}
	if _, ok := personaRunChatPrincipal(ctx, request.TenantID, request.InvokerID); !ok {
		return errPersonaRunModelWorker
	}
	if request.TenantID == "" || request.TenantID != strings.TrimSpace(request.TenantID) {
		return errPersonaRunModelWorker
	}
	starterConfig, err := w.tenants.ForPersonaRunTenant(ctx, request.TenantID)
	if err != nil {
		return fmt.Errorf("%w: tenant runtime unavailable", errPersonaRunModelWorker)
	}
	if isNilPersonaOutputPort(starterConfig.Work) || isNilPersonaOutputPort(starterConfig.Model) ||
		isNilPersonaOutputPort(starterConfig.Output) || isNilPersonaOutputPort(starterConfig.Reply) {
		return errPersonaRunModelWorker
	}
	stepState := &personaRunStepIdentityState{}
	starterConfig.Work = fencedPersonaRunModelWorkSource{inner: starterConfig.Work, fence: w.fence, leases: w.leases, steps: stepState}
	starterConfig.Model = fencedPersonaRunModelExecutor{inner: starterConfig.Model, fence: w.fence, steps: stepState}
	starterConfig.Output = fencedPersonaRunOutputValidator{inner: starterConfig.Output, fence: w.fence, steps: stepState}
	starterConfig.Reply = fencedPersonaRunReplyDeliverer{inner: starterConfig.Reply, fence: w.fence, steps: stepState}
	if gate := w.cost.gate.Load(); gate != nil {
		if err := gate.admit(request); err != nil {
			return err
		}
		starterConfig.Model = gate.meter(starterConfig.Model, request)
	}
	starter, err := NewPersonaRunStarter(starterConfig)
	if err != nil {
		return fmt.Errorf("%w: compose tenant durable starter: %w", errPersonaRunModelWorker, err)
	}
	return starter.Start(ctx, request)
}

var _ agentinvoke.RunStarter = (*PersonaRunModelWorker)(nil)
var _ agentinvoke.TargetAgentResolver = (*PersonaRunModelWorker)(nil)

// ResolveTargetAgentID reads the exact published manifest before a delegation
// grant is issued. It uses the same tenant builder as admission and execution.
func (w *PersonaRunModelWorker) ResolveTargetAgentID(ctx context.Context, invocation agentinvoke.RunRequest) (string, error) {
	if w == nil || ctx == nil || isNilPersonaOutputPort(w.tenants) || !validPersonaRunInvocation(invocation) {
		return "", errPersonaRunModelWorker
	}
	cfg, err := w.tenants.ForPersonaRunTenant(ctx, invocation.TenantID)
	if err != nil || cfg.Builder == nil || cfg.Builder.source == nil {
		return "", errPersonaRunModelWorker
	}
	facts, err := cfg.Builder.source.ResolvePersonaRun(ctx, invocation)
	if err != nil || validatePersonaRunFacts(invocation, facts) != nil {
		return "", errPersonaRunModelWorker
	}
	return facts.Agent.AgentID, nil
}

type fencedPersonaRunModelWorkSource struct {
	inner  PersonaRunModelWorkSource
	fence  PersonaRunSecurityFence
	leases PersonaRunSecurityLeaseResolver
	steps  *personaRunStepIdentityState
}

func (s fencedPersonaRunModelWorkSource) BuildPersonaRunModelWork(ctx context.Context, admission agentrun.Record, run runstate.Run) (PersonaRunModelWork, error) {
	if s.inner == nil || s.fence == nil || s.leases == nil || s.steps == nil || ctx == nil || admission.ID == "" || run.ID != admission.ID ||
		strings.TrimSpace(admission.Request.Source.TenantID) == "" || run.TenantID != admission.Request.Source.TenantID {
		return PersonaRunModelWork{}, ErrPersonaRunExecutorUnavailable
	}
	runID := agentsecurity.PersonaRunID(run.ID)
	leaseID, err := s.leases.ResolvePersonaRunSecurityLease(ctx, admission, run)
	if err != nil || leaseID == "" {
		return PersonaRunModelWork{}, fmt.Errorf("%w: trusted run security lease unavailable: %v", ErrPersonaRunExecutorUnavailable, err)
	}
	if err := s.fence.Bind(runID, admission.Request.Source.TenantID, leaseID); err != nil {
		return PersonaRunModelWork{}, fmt.Errorf("%w: bind run security lease", ErrPersonaRunExecutorUnavailable)
	}
	s.steps.set(run)
	var work PersonaRunModelWork
	stepID := personaRunSecurityStepID(run.ID, "context", personaRunStepGenerationKey(run))
	done := agentUXSpeedEvent(ctx, "fence.context")
	_, err = s.fence.RunStep(ctx, runID, stepID, func(stepCtx context.Context) error {
		var stepErr error
		work, stepErr = s.inner.BuildPersonaRunModelWork(stepCtx, admission, run)
		return stepErr
	})
	done()
	if err != nil {
		return PersonaRunModelWork{}, fmt.Errorf("%w: fenced model-work resolution failed: %v", ErrPersonaRunExecutorUnavailable, err)
	}
	return work, nil
}

type fencedPersonaRunModelExecutor struct {
	inner interface {
		Execute(context.Context, AgentModelExecutorRequest) (AgentModelExecutorResult, error)
	}
	fence PersonaRunSecurityFence
	steps *personaRunStepIdentityState
}

func (e fencedPersonaRunModelExecutor) Execute(ctx context.Context, request AgentModelExecutorRequest) (AgentModelExecutorResult, error) {
	if e.inner == nil || e.fence == nil || e.steps == nil || ctx == nil || request.Task.TaskID == "" || request.StepID == "" {
		return AgentModelExecutorResult{}, ErrPersonaRunExecutorUnavailable
	}
	generation, ok := e.steps.get(request.Task.TaskID)
	if !ok {
		return AgentModelExecutorResult{}, ErrPersonaRunExecutorUnavailable
	}
	var result AgentModelExecutorResult
	stepID := personaRunSecurityStepID(request.Task.TaskID, "model", fmt.Sprintf("%s:%d:%d", request.StepID, generation.fence, generation.version))
	done := agentUXSpeedEvent(ctx, "fence.model")
	_, err := e.fence.RunStep(ctx, agentsecurity.PersonaRunID(request.Task.TaskID), stepID, func(stepCtx context.Context) error {
		var stepErr error
		result, stepErr = e.inner.Execute(stepCtx, request)
		return stepErr
	})
	done()
	if err != nil {
		return AgentModelExecutorResult{}, fmt.Errorf("%w: fenced model execution failed: %v", ErrPersonaRunModelFailure, err)
	}
	return result, nil
}

type fencedPersonaRunOutputValidator struct {
	inner PersonaRunOutputValidator
	fence PersonaRunSecurityFence
	steps *personaRunStepIdentityState
}

func (v fencedPersonaRunOutputValidator) ValidateAndPersistPersonaOutput(ctx context.Context, admission agentrun.Record, run runstate.Run, result agentmodel.ModelResult) (agentsecurity.FinalOutputPersistence, error) {
	if v.inner == nil || v.fence == nil || v.steps == nil || ctx == nil || run.ID == "" || run.ID != admission.ID {
		return agentsecurity.FinalOutputPersistence{}, ErrPersonaRunExecutorUnavailable
	}
	v.steps.set(run)
	var persisted agentsecurity.FinalOutputPersistence
	stepID := personaRunSecurityStepID(run.ID, "output", personaRunStepGenerationKey(run))
	done := agentUXSpeedEvent(ctx, "fence.output")
	_, err := v.fence.RunStep(ctx, agentsecurity.PersonaRunID(run.ID), stepID, func(stepCtx context.Context) error {
		var stepErr error
		persisted, stepErr = v.inner.ValidateAndPersistPersonaOutput(stepCtx, admission, run, result)
		return stepErr
	})
	done()
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, fmt.Errorf("%w: fenced output validation failed: %v", ErrPersonaRunOutputRejected, err)
	}
	return persisted, nil
}

type fencedPersonaRunReplyDeliverer struct {
	inner PersonaRunReplyDeliverer
	fence PersonaRunSecurityFence
	steps *personaRunStepIdentityState
}

func (d fencedPersonaRunReplyDeliverer) Deliver(ctx context.Context, request PersonaReplyDeliveryRequest) (PersonaReplyDeliveryReceipt, error) {
	if d.inner == nil || d.fence == nil || d.steps == nil || ctx == nil || request.IdempotencyKey == "" {
		return PersonaReplyDeliveryReceipt{}, ErrPersonaRunExecutorUnavailable
	}
	generation, ok := d.steps.get(request.IdempotencyKey)
	if !ok {
		return PersonaReplyDeliveryReceipt{}, ErrPersonaRunExecutorUnavailable
	}
	var receipt PersonaReplyDeliveryReceipt
	stepID := personaRunSecurityStepID(request.IdempotencyKey, "delivery", fmt.Sprintf("%s:%d:%d", request.IdempotencyKey, generation.fence, generation.version))
	done := agentUXSpeedEvent(ctx, "fence.delivery")
	_, err := d.fence.RunStep(ctx, agentsecurity.PersonaRunID(request.IdempotencyKey), stepID, func(stepCtx context.Context) error {
		var stepErr error
		receipt, stepErr = d.inner.Deliver(stepCtx, request)
		return stepErr
	})
	done()
	if err != nil {
		return PersonaReplyDeliveryReceipt{}, fmt.Errorf("%w: fenced reply delivery failed: %v", ErrPersonaRunDeliveryFailure, err)
	}
	return receipt, nil
}

func personaRunSecurityStepID(runID, phase, discriminator string) string {
	sum := sha256.Sum256([]byte("persona-security-step\x00" + runID + "\x00" + phase + "\x00" + discriminator))
	return "persona-" + phase + "-" + hex.EncodeToString(sum[:16])
}

func personaRunStepGenerationKey(run runstate.Run) string {
	return fmt.Sprintf("%d:%d", run.Fence, run.Version)
}
