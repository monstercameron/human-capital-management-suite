package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

func TestTodo_AGENTP_008_PersonaRunBindingStartsDurableRun(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	builder, err := NewPersonaRunRequestBuilder(&personaRunFactsSourceFake{facts: personaRunRequestBuilderFacts()})
	if err != nil {
		t.Fatal(err)
	}
	admissionStore := agentrun.NewMemoryAdmissionStore()
	executionStore := runstate.NewMemoryStore()
	binding, err := NewPersonaRunBinding(PersonaRunBindingConfig{
		Builder: builder, Authority: personaChatAdmissionAuthorityFake{}, AdmissionStore: admissionStore,
		ExecutionStore: executionStore, AdmissionRecheck: personaChatAdmissionRecheckerFake{}, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	invocation := personaRunRequestBuilderInvocation()
	if err := binding.Start(context.Background(), invocation); err != nil {
		t.Fatal(err)
	}

	request, err := builder.BuildPersonaChatAdmission(context.Background(), invocation)
	if err != nil {
		t.Fatal(err)
	}
	id, err := agentrun.AdmissionRequestID(request.Source)
	if err != nil {
		t.Fatal(err)
	}
	run, err := executionStore.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if run.ID != id || run.AdmissionID != id || run.State != runstate.StateReady || len(run.Checkpoints) != 1 || run.Checkpoints[0].Phase != runstate.PhaseAdmission {
		t.Fatalf("durable run=%+v, want READY admission checkpoint", run)
	}
}

func TestTodo_AGENTP_008_PersonaRunBindingResolvesExactAgentIdentityBeforeGrant(t *testing.T) {
	binding, err := NewPersonaRunBinding(PersonaRunBindingConfig{
		Builder: mustPersonaRunBuilder(t), Authority: personaChatAdmissionAuthorityFake{}, AdmissionStore: agentrun.NewMemoryAdmissionStore(),
		ExecutionStore: runstate.NewMemoryStore(), AdmissionRecheck: personaChatAdmissionRecheckerFake{},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := binding.ResolveTargetAgentID(context.Background(), personaRunRequestBuilderInvocation())
	if err != nil || got != "agent-1" {
		t.Fatalf("ResolveTargetAgentID = %q, %v; want exact server-resolved agent-1", got, err)
	}
}

func TestTodo_AGENTP_008_PersonaRunBindingFailsClosedWhenComposedIncomplete(t *testing.T) {
	cases := []PersonaRunBindingConfig{
		{},
		{Builder: &PersonaRunRequestBuilder{}, Authority: personaChatAdmissionAuthorityFake{}, AdmissionStore: agentrun.NewMemoryAdmissionStore(), ExecutionStore: runstate.NewMemoryStore(), AdmissionRecheck: personaChatAdmissionRecheckerFake{}},
		{Builder: mustPersonaRunBuilder(t), AdmissionStore: agentrun.NewMemoryAdmissionStore(), ExecutionStore: runstate.NewMemoryStore(), AdmissionRecheck: personaChatAdmissionRecheckerFake{}},
		{Builder: mustPersonaRunBuilder(t), Authority: personaChatAdmissionAuthorityFake{}, ExecutionStore: runstate.NewMemoryStore(), AdmissionRecheck: personaChatAdmissionRecheckerFake{}},
		{Builder: mustPersonaRunBuilder(t), Authority: personaChatAdmissionAuthorityFake{}, AdmissionStore: agentrun.NewMemoryAdmissionStore(), AdmissionRecheck: personaChatAdmissionRecheckerFake{}},
		{Builder: mustPersonaRunBuilder(t), Authority: personaChatAdmissionAuthorityFake{}, AdmissionStore: agentrun.NewMemoryAdmissionStore(), ExecutionStore: runstate.NewMemoryStore()},
	}
	for i, cfg := range cases {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			if _, err := NewPersonaRunBinding(cfg); !errors.Is(err, errPersonaRunBinding) {
				t.Fatalf("error=%v, want fail-closed binding error", err)
			}
		})
	}
}

func TestTodo_AGENTP_008_PersonaRunBindingRejectsNilContextAndInvalidInvocation(t *testing.T) {
	binding, err := NewPersonaRunBinding(PersonaRunBindingConfig{
		Builder: mustPersonaRunBuilder(t), Authority: personaChatAdmissionAuthorityFake{}, AdmissionStore: agentrun.NewMemoryAdmissionStore(),
		ExecutionStore: runstate.NewMemoryStore(), AdmissionRecheck: personaChatAdmissionRecheckerFake{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := binding.Start(nil, personaRunRequestBuilderInvocation()); !errors.Is(err, errPersonaRunBinding) {
		t.Fatalf("nil context error=%v", err)
	}
	invalid := personaRunRequestBuilderInvocation()
	invalid.Mode = agentinvoke.RunMode("SPONSORED")
	if err := binding.Start(context.Background(), invalid); !errors.Is(err, errPersonaRunBinding) {
		t.Fatalf("invalid invocation error=%v", err)
	}
}

func mustPersonaRunBuilder(t *testing.T) *PersonaRunRequestBuilder {
	t.Helper()
	builder, err := NewPersonaRunRequestBuilder(&personaRunFactsSourceFake{facts: personaRunRequestBuilderFacts()})
	if err != nil {
		t.Fatal(err)
	}
	return builder
}
