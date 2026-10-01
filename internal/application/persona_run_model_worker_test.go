package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaRunWorkerFenceFake struct {
	bound   map[agentsecurity.PersonaRunID]agentsecurity.KillSwitchLeaseID
	steps   []agentsecurity.PersonaRunID
	stepIDs []string
	tenants []string
	deny    error
}

func (f *personaRunWorkerFenceFake) Bind(run agentsecurity.PersonaRunID, tenant string, lease agentsecurity.KillSwitchLeaseID) error {
	if f.bound == nil {
		f.bound = make(map[agentsecurity.PersonaRunID]agentsecurity.KillSwitchLeaseID)
	}
	if run == "" || tenant == "" || lease == "" || f.bound[run] != "" {
		return errors.New("invalid run binding")
	}
	f.bound[run] = lease
	f.tenants = append(f.tenants, tenant)
	return nil
}

func (f *personaRunWorkerFenceFake) RunStep(ctx context.Context, run agentsecurity.PersonaRunID, stepID string, step func(context.Context) error) (agentsecurity.Fallback, error) {
	f.steps = append(f.steps, run)
	f.stepIDs = append(f.stepIDs, stepID)
	if ctx == nil || stepID == "" {
		return agentsecurity.Fallback{}, errors.New("step context and id are required")
	}
	if f.deny != nil {
		return agentsecurity.Fallback{}, f.deny
	}
	if f.bound[run] == "" {
		return agentsecurity.Fallback{}, errors.New("run is not bound")
	}
	return agentsecurity.Fallback{}, step(ctx)
}

type personaRunWorkerLeaseFake struct {
	id agentsecurity.KillSwitchLeaseID
}

type personaRunWorkerTenantFactoryFake struct {
	tenant string
	config PersonaRunStarterConfig
}

func (f *personaRunWorkerTenantFactoryFake) ForPersonaRunTenant(_ context.Context, tenant string) (PersonaRunStarterConfig, error) {
	f.tenant = tenant
	return f.config, nil
}

func (f *personaRunWorkerTenantFactoryFake) ValidatePersonaRunTenantRuntime() error { return nil }

func (f personaRunWorkerLeaseFake) ResolvePersonaRunSecurityLease(context.Context, agentrun.Record, runstate.Run) (agentsecurity.KillSwitchLeaseID, error) {
	return f.id, nil
}

type personaRunWorkerWorkFake struct{ calls int }

func (f *personaRunWorkerWorkFake) BuildPersonaRunModelWork(context.Context, agentrun.Record, runstate.Run) (PersonaRunModelWork, error) {
	f.calls++
	return PersonaRunModelWork{}, nil
}

func TestPersonaRunModelWorker_RequiresExplicitFenceAndLeaseResolver(t *testing.T) {
	fence := &personaRunWorkerFenceFake{}
	leases := personaRunWorkerLeaseFake{id: "security-lease"}
	for _, tc := range []struct {
		name string
		cfg  PersonaRunModelWorkerConfig
	}{
		{name: "no tenant runtime", cfg: PersonaRunModelWorkerConfig{Fence: fence, Leases: leases}},
		{name: "no fence", cfg: PersonaRunModelWorkerConfig{Tenants: &personaRunWorkerTenantFactoryFake{}, Leases: leases}},
		{name: "no security lease resolver", cfg: PersonaRunModelWorkerConfig{Tenants: &personaRunWorkerTenantFactoryFake{}, Fence: fence}},
		{name: "unvalidated tenant runtime", cfg: PersonaRunModelWorkerConfig{Tenants: unvalidatedPersonaRunWorkerTenantFactory{}, Fence: fence, Leases: leases}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewPersonaRunModelWorker(tc.cfg); !errors.Is(err, errPersonaRunModelWorker) {
				t.Fatalf("constructor err=%v, want fail-closed worker error", err)
			}
		})
	}
}

type unvalidatedPersonaRunWorkerTenantFactory struct{}

func (unvalidatedPersonaRunWorkerTenantFactory) ForPersonaRunTenant(context.Context, string) (PersonaRunStarterConfig, error) {
	return PersonaRunStarterConfig{}, nil
}

func TestPersonaRunModelWorker_UsesAuthenticatedTenantRuntime(t *testing.T) {
	factory := &personaRunWorkerTenantFactoryFake{}
	worker, err := NewPersonaRunModelWorker(PersonaRunModelWorkerConfig{
		Tenants: factory, Fence: &personaRunWorkerFenceFake{}, Leases: personaRunWorkerLeaseFake{id: "lease"},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := agentinvoke.RunRequest{TenantID: "tenant-a", InvokerID: "alice"}
	if err := worker.Start(context.Background(), request); !errors.Is(err, errPersonaRunModelWorker) || factory.tenant != "" {
		t.Fatalf("unauthenticated start err=%v tenant=%q, want fail before tenant lookup", err, factory.tenant)
	}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "alice", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow,
		SessionRef: "persona-run-worker-test", IssuedAt: time.Now().UTC().Add(-time.Minute),
		ExpiresAt: time.Now().UTC().Add(time.Hour), CredentialDigest: "credential-digest",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), principal)
	if err := worker.Start(ctx, request); !errors.Is(err, errPersonaRunModelWorker) {
		t.Fatalf("incomplete tenant runtime error=%v, want fail-closed composition", err)
	}
	if factory.tenant != "tenant-a" {
		t.Fatalf("tenant runtime lookup=%q, want authenticated tenant-a", factory.tenant)
	}
}

func TestPersonaRunModelWorker_BindsTrustedSecurityLeaseBeforeFirstStep(t *testing.T) {
	fence := &personaRunWorkerFenceFake{}
	inner := &personaRunWorkerWorkFake{}
	steps := &personaRunStepIdentityState{}
	work := fencedPersonaRunModelWorkSource{inner: inner, fence: fence, leases: personaRunWorkerLeaseFake{id: "kill-switch-lease"}, steps: steps}
	admission := agentrun.Record{ID: "run-123", Request: agentrun.Request{Source: agentrun.SourceIdentity{TenantID: "tenant-123"}}}
	run := runstate.Run{ID: "run-123", TenantID: "tenant-123", AdmissionID: "run-123", Version: 1, Fence: 1}
	if _, err := work.BuildPersonaRunModelWork(context.Background(), admission, run); err != nil {
		t.Fatal(err)
	}
	if fence.bound[agentsecurity.PersonaRunID(run.ID)] != "kill-switch-lease" || inner.calls != 1 || len(fence.steps) != 1 || fence.tenants[0] != "tenant-123" || len(fence.stepIDs[0]) == 0 {
		t.Fatalf("binding=%v inner calls=%d fenced steps=%v", fence.bound, inner.calls, fence.steps)
	}
}

func TestPersonaRunModelWorker_FailsClosedWhenFenceRejectsWorkStep(t *testing.T) {
	fence := &personaRunWorkerFenceFake{deny: errors.New("lease revoked")}
	inner := &personaRunWorkerWorkFake{}
	work := fencedPersonaRunModelWorkSource{inner: inner, fence: fence, leases: personaRunWorkerLeaseFake{id: "lease"}, steps: &personaRunStepIdentityState{}}
	_, err := work.BuildPersonaRunModelWork(context.Background(), agentrun.Record{ID: "run-123", Request: agentrun.Request{Source: agentrun.SourceIdentity{TenantID: "tenant-123"}}}, runstate.Run{ID: "run-123", TenantID: "tenant-123", Version: 1, Fence: 1})
	if !errors.Is(err, ErrPersonaRunExecutorUnavailable) || inner.calls != 0 {
		t.Fatalf("fenced build err=%v inner calls=%d, want unavailable and no work", err, inner.calls)
	}
}

func TestPersonaRunModelWorker_FencesModelAndReplySteps(t *testing.T) {
	fence := &personaRunWorkerFenceFake{bound: map[agentsecurity.PersonaRunID]agentsecurity.KillSwitchLeaseID{"run-123": "lease"}}
	steps := &personaRunStepIdentityState{}
	steps.set(runstate.Run{ID: "run-123", Fence: 2, Version: 7})
	model := fencedPersonaRunModelExecutor{fence: fence, inner: personaRunWorkerModelFake{}, steps: steps}
	request := AgentModelExecutorRequest{Task: TrustedModelTask{TenantID: "tenant", TaskID: "run-123", AgentID: "agent"}, StepID: "model-step-1"}
	if _, err := model.Execute(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	delivery := fencedPersonaRunReplyDeliverer{fence: fence, inner: personaRunWorkerReplyFake{}, steps: steps}
	if _, err := delivery.Deliver(context.Background(), PersonaReplyDeliveryRequest{IdempotencyKey: "run-123"}); err != nil {
		t.Fatal(err)
	}
	if len(fence.steps) != 2 || fence.steps[0] != "run-123" || fence.steps[1] != "run-123" {
		t.Fatalf("fenced steps=%v, want model and reply steps for run-123", fence.steps)
	}
}

func TestPersonaRunModelWorker_ChangesDurableStepIDAcrossClaimRecovery(t *testing.T) {
	fence := &personaRunWorkerFenceFake{bound: map[agentsecurity.PersonaRunID]agentsecurity.KillSwitchLeaseID{"run-123": "lease"}}
	steps := &personaRunStepIdentityState{}
	steps.set(runstate.Run{ID: "run-123", Fence: 3, Version: 8})
	model := fencedPersonaRunModelExecutor{fence: fence, inner: personaRunWorkerModelFake{}, steps: steps}
	request := AgentModelExecutorRequest{Task: TrustedModelTask{TenantID: "tenant", TaskID: "run-123", AgentID: "agent"}, StepID: "model-step-1"}
	if _, err := model.Execute(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	first := fence.stepIDs[0]
	steps.set(runstate.Run{ID: "run-123", Fence: 4, Version: 9})
	if _, err := model.Execute(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if fence.stepIDs[1] == first || len(fence.stepIDs[1]) == 0 {
		t.Fatalf("recovered security step id=%q, first=%q; want a distinct deterministic claim step", fence.stepIDs[1], first)
	}
}

func TestPersonaRunModelWorker_FencesOutputValidationAndPersistence(t *testing.T) {
	fence := &personaRunWorkerFenceFake{bound: map[agentsecurity.PersonaRunID]agentsecurity.KillSwitchLeaseID{"run-123": "lease"}}
	inner := &personaRunWorkerOutputFake{}
	validator := fencedPersonaRunOutputValidator{inner: inner, fence: fence, steps: &personaRunStepIdentityState{}}
	_, err := validator.ValidateAndPersistPersonaOutput(context.Background(), agentrun.Record{ID: "run-123"}, runstate.Run{ID: "run-123", Version: 1, Fence: 1}, agentmodel.ModelResult{Text: "answer"})
	if err != nil {
		t.Fatal(err)
	}
	if inner.calls != 1 || len(fence.steps) != 1 || fence.steps[0] != "run-123" {
		t.Fatalf("validator calls=%d fenced steps=%v", inner.calls, fence.steps)
	}
}

type personaRunWorkerModelFake struct{}

func (personaRunWorkerModelFake) Execute(context.Context, AgentModelExecutorRequest) (AgentModelExecutorResult, error) {
	return AgentModelExecutorResult{Result: agentmodel.ModelResult{Text: "answer"}}, nil
}

type personaRunWorkerReplyFake struct{}

func (personaRunWorkerReplyFake) Deliver(context.Context, PersonaReplyDeliveryRequest) (PersonaReplyDeliveryReceipt, error) {
	return PersonaReplyDeliveryReceipt{Private: true, EphemeralPostID: "ephemeral"}, nil
}

type personaRunWorkerOutputFake struct{ calls int }

func (f *personaRunWorkerOutputFake) ValidateAndPersistPersonaOutput(context.Context, agentrun.Record, runstate.Run, agentmodel.ModelResult) (agentsecurity.FinalOutputPersistence, error) {
	f.calls++
	return agentsecurity.FinalOutputPersistence{}, nil
}

var _ agentinvoke.RunStarter = (*PersonaRunModelWorker)(nil)
