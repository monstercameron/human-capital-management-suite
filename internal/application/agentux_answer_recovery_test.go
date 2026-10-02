package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestAgentUXQuality_Recovery_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenant)
	pool := commonAgentOpenIntegrationStore(t, db)
	repository, err := agentrunstore.NewAdmissionRepository(pool, tenant, values.TenantId("tenant-acme"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	request := personaChatAdmissionRequestFixture()
	bindPersonaChatAdmissionRequest(&request, personaChatRunRequestFixture())
	request.Deadline = now.Add(time.Minute)
	admissions, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: personaChatAdmissionAuthorityFake{}, Store: repository, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	admission, _, err := admissions.Admit(ctx, request)
	if err != nil || admission.Decision != agentrun.DecisionAccepted {
		t.Fatalf("admitted recovery fixture: %+v %v", admission, err)
	}
	durable, err := agentrunstate.New(pool, func(string) uuid.UUID { return tenant })
	if err != nil {
		t.Fatal(err)
	}
	store, err := durable.ForTenant("tenant-acme")
	if err != nil {
		t.Fatal(err)
	}
	state, err := runstate.New(store, personaChatAdmissionRecheckerFake{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := state.Start(ctx, admission)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := state.Claim(ctx, run.ID, "old-worker", now.Add(-time.Second), 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	// A fresh service reads the durable expired lease, as the persona restart
	// dispatcher does. Recovery clears it without replaying any tool effect.
	restarted, err := runstate.New(store, personaChatAdmissionRecheckerFake{})
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := restarted.Recover(ctx, run.ID, claimed.Version, now)
	if err != nil || recovered.State != runstate.StateReady || recovered.Lease != nil {
		t.Fatalf("interrupted run left working: %+v %v", recovered, err)
	}
	next, err := restarted.Claim(ctx, run.ID, "new-worker", now, time.Minute)
	if err != nil || next.Fence <= claimed.Fence || next.Lease.Owner != "new-worker" {
		t.Fatalf("recovery reused old fence: %+v %v", next, err)
	}
	finished, err := restarted.FailWithRefusal(ctx, run.ID, "new-worker", "ANSWER_INTERRUPTED", true, runstate.NewFailureRefusal(runstate.FailureGateModelCall, "internal/application"), next.Fence, next.Version, now)
	if err != nil || finished.State != runstate.StateFailed || !finished.Retryable || finished.TerminalCode != "ANSWER_INTERRUPTED" || strings.Contains(finished.FailureLocation, "secret") {
		t.Fatalf("interruption not terminal and retryable: %+v %v", finished, err)
	}
	loaded, err := store.Get(ctx, run.ID)
	if err != nil || loaded.TerminalCode != finished.TerminalCode || loaded.Lease != nil {
		t.Fatalf("interruption not durable: %+v %v", loaded, err)
	}
	request.Source.Key += "-restart"
	admission, _, err = admissions.Admit(ctx, request)
	if err != nil || admission.Decision != agentrun.DecisionAccepted {
		t.Fatalf("restart admission: %+v %v", admission, err)
	}
	invocations, err := agentinvocationstore.NewWithTenantUUID(pool, func(string) uuid.UUID { return tenant })
	if err != nil {
		t.Fatal(err)
	}
	invocation := personaRunInvocation(request)
	if _, created, err := invocations.Claim(ctx, agentinvoke.Invocation{ID: invocation.InvocationID, TenantID: invocation.TenantID, ConversationID: invocation.ConversationID, ThreadID: invocation.ThreadID, PostID: invocation.InvokingPostID, InvokerID: invocation.InvokerID, PersonaID: invocation.PersonaID, PersonaVersion: invocation.PersonaVersion, InstallationID: invocation.InstallationID, Mode: invocation.Mode, Actor: invocation.Actor}); err != nil || !created {
		t.Fatalf("restart invocation: created=%t err=%v", created, err)
	}
	if saved, readErr := repository.GetByID(ctx, admission.ID); readErr != nil || saved.Decision != agentrun.DecisionAccepted {
		t.Fatalf("restart admission could not be read: %+v %v", saved, readErr)
	}
	run, err = state.Start(ctx, admission)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err = state.Claim(ctx, run.ID, "old-worker", now.Add(-time.Second), 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	model := &qualityRetryModel{failures: 2, kind: "timeout", withError: true}
	work := &qualityLeaseWork{request: AgentModelExecutorRequest{Task: TrustedModelTask{TaskID: run.ID, TenantID: run.TenantID, AgentID: run.AgentDigest}}}
	background := &backgroundReplyRefusal{}
	factory := &personaRunWorkerTenantFactoryFake{config: PersonaRunStarterConfig{
		Authority: personaChatAdmissionAuthorityFake{}, AdmissionRecheck: personaChatAdmissionRecheckerFake{},
		ExecutionStore: store, Model: model, Work: work, BackgroundReply: background,
		WorkerID: "restarted-worker", LeaseTTL: time.Minute, Now: time.Now,
	}}
	worker, err := NewPersonaRunModelWorker(PersonaRunModelWorkerConfig{Tenants: factory, Fence: &personaRunWorkerFenceFake{}, Leases: personaRunWorkerLeaseFake{id: "restart-security-lease"}})
	if err != nil {
		t.Fatal(err)
	}
	outputs := &qualityRecoveryNoOutput{}
	dispatcher, err := NewPersonaBackgroundDispatcher(pool, func(values.TenantId) uuid.UUID { return tenant }, worker, outputs)
	if err != nil {
		t.Fatal(err)
	}
	err = dispatcher.Wake(ctx, "tenant-acme", run.ID)
	var failure *PersonaRunFailure
	if !errors.As(err, &failure) || failure.Code != "MODEL_TIMEOUT" || !failure.Retryable {
		t.Fatalf("restarted dispatcher lost the typed final failure: %v", err)
	}
	loaded, err = store.Get(ctx, run.ID)
	if err != nil || loaded.State != runstate.StateFailed || loaded.TerminalCode != "MODEL_TIMEOUT" || loaded.Lease != nil || loaded.Fence <= claimed.Fence || model.calls != 2 || work.calls != 1 || outputs.calls != 1 || background.calls != 0 {
		t.Fatalf("dispatcher did not resume and terminate interrupted work: %+v model=%d work=%d output=%d delivery=%d err=%v", loaded, model.calls, work.calls, outputs.calls, background.calls, err)
	}
}

type qualityRecoveryNoOutput struct{ calls int }

func (r *qualityRecoveryNoOutput) RecoverPersonaRunOutput(context.Context, agentrun.Record, runstate.Run) (agentsecurity.FinalOutputPersistence, error) {
	r.calls++
	return agentsecurity.FinalOutputPersistence{}, agentpersonastore.ErrNotFound
}
