package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTP_008_Integration_DeadlineSurvivesAdmissionAndWorkerClaim(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenantID := uuid.New()
	db.Exec(t, "INSERT INTO tenant(tenant_id) VALUES ($1)", tenantID)
	store := commonAgentOpenIntegrationStore(t, db)
	facts := personaRunRequestBuilderFacts()
	facts.Deadline = facts.Deadline.Add(987654321 * time.Nanosecond)
	invocation := personaRunRequestBuilderInvocation()
	invocation.InvocationID = uuid.NewString()
	invocation.Actor.InvocationID = invocation.InvocationID
	facts.TriggerID = invocation.InvocationID
	invocations, err := agentinvocationstore.NewWithTenantUUID(store, func(string) uuid.UUID { return tenantID })
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := invocations.Claim(ctx, agentinvoke.Invocation{ID: invocation.InvocationID, TenantID: invocation.TenantID, ConversationID: invocation.ConversationID,
		ThreadID: invocation.ThreadID, PostID: invocation.InvokingPostID, InvokerID: invocation.InvokerID, PersonaID: invocation.PersonaID, PersonaVersion: invocation.PersonaVersion,
		InstallationID: invocation.InstallationID, Mode: invocation.Mode, State: agentinvoke.InvocationStarted, Skills: invocation.Skills, Grant: invocation.Grant, Actor: invocation.Actor}); err != nil {
		t.Fatal(err)
	}
	builder, err := NewPersonaRunRequestBuilder(&personaRunFactsSourceFake{facts: facts})
	if err != nil {
		t.Fatal(err)
	}
	request, err := builder.BuildPersonaChatAdmission(ctx, invocation)
	if err != nil {
		t.Fatal(err)
	}
	authority := &personaRunCurrentAuthorityFake{snapshot: agentrun.AuthoritySnapshot{Agent: request.Agent, InstallationID: request.InstallationID,
		Principal: request.Principal, Audience: request.Audience, Context: request.Context, BudgetCeiling: request.Budget,
		GrantRef: request.Principal.DelegatedCredentialRef, PolicyDigest: builderDigest("e")}}
	repo, err := agentrunstore.NewAdmissionRepository(store, tenantID, values.TenantId(facts.TenantID))
	if err != nil {
		t.Fatal(err)
	}
	now := facts.Deadline.Add(-time.Hour)
	admissions, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: authority, Store: repo, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	admitted, _, err := admissions.Admit(ctx, request)
	if err != nil || admitted.Decision != agentrun.DecisionAccepted {
		t.Fatalf("admission: %v", err)
	}
	rechecker, err := NewPersonaRunAdmissionRechecker(facts.TenantID, personaRunAdmissionReader{store: repo}, authority)
	if err != nil {
		t.Fatal(err)
	}
	runs, err := agentrunstate.New(store, func(tenant string) uuid.UUID {
		if tenant == facts.TenantID {
			return tenantID
		}
		return uuid.Nil
	})
	if err != nil {
		t.Fatal(err)
	}
	tenantRuns, err := runs.ForTenant(facts.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := runstate.New(tenantRuns, rechecker)
	if err != nil {
		t.Fatal(err)
	}
	started, err := worker.Start(ctx, admitted)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := worker.Claim(ctx, started.ID, "deadline-worker", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePersonaModelWorkBinding(admitted, claimed); err != nil {
		t.Fatalf("durable worker claim lost its admitted binding: %v", err)
	}
	if request.Deadline.After(facts.Deadline) || facts.Deadline.Sub(request.Deadline) >= time.Microsecond {
		t.Fatal("normalization extended or materially shortened the authorized deadline")
	}
	claimed.Deadline = claimed.Deadline.Add(time.Microsecond)
	if validatePersonaModelWorkBinding(admitted, claimed) == nil {
		t.Fatal("changed durable deadline was accepted")
	}
}
