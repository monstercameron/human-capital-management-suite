package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// chatbug049Model is a model fixture that answers with the words it is given.
// No provider is called.
type chatbug049Model struct{ text string }

func (m chatbug049Model) Execute(context.Context, AgentModelExecutorRequest) (AgentModelExecutorResult, error) {
	return AgentModelExecutorResult{Result: agentmodel.ModelResult{Text: m.text, Finish: agentmodel.FinishComplete}}, nil
}

// chatbug049Dispatch runs the admitted run with the fixture model, on the same
// worker composition the recovery tests use.
func chatbug049Dispatch(h *agentRunRecoveryHarness, model chatbug049Model) error {
	h.t.Helper()
	factory := &personaRunWorkerTenantFactoryFake{config: PersonaRunStarterConfig{
		Authority: personaChatAdmissionAuthorityFake{}, AdmissionRecheck: personaChatAdmissionRecheckerFake{},
		ExecutionStore: h.store, Model: model, Work: agentRunRecoveryWork{}, BackgroundReply: &backgroundReplyRefusal{},
		WorkerID: "worker-049", LeaseTTL: time.Minute, Now: h.now,
	}}
	worker, err := NewPersonaRunModelWorker(PersonaRunModelWorkerConfig{Tenants: factory, Fence: &personaRunWorkerFenceFake{}, Leases: personaRunWorkerLeaseFake{id: "lease-049"}})
	if err != nil {
		h.t.Fatal(err)
	}
	dispatcher, err := NewPersonaBackgroundDispatcher(h.pool, func(values.TenantId) uuid.UUID { return h.tenant }, worker, &qualityRecoveryNoOutput{})
	if err != nil {
		h.t.Fatal(err)
	}
	return dispatcher.DispatchTenantRecovering(h.ctx, agentRunRecoveryTenant, 8)
}

// A reply that is nothing but a citation of a document ends the run with a code
// of its own, and the card the asker reads says the agent answered with only a
// document's name. The run is over, not retried by the server, and the asker
// may ask again.
func TestTodo_CHATBUG_049_Integration(t *testing.T) {
	h := newAgentRunRecoveryHarness(t)
	inv := h.request("")
	run, err := h.state.Start(h.ctx, h.admit(inv, 2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	err = chatbug049Dispatch(h, chatbug049Model{text: "[[1]]"})
	var failure *PersonaRunFailure
	if !errors.As(err, &failure) || failure.Code != chat.AgentAnswerTitleOnlyCode || failure.Retryable || !errors.Is(err, ErrPersonaRunOutputRejected) {
		t.Fatalf("a title-only reply ended as %v (%+v), want the title-only refusal", err, failure)
	}
	finished := h.run(run)
	if finished.State != runstate.StateFailed || finished.TerminalCode != chat.AgentAnswerTitleOnlyCode || finished.Retryable || finished.FailureGate != string(runstate.FailureGateOutputGrounding) {
		t.Fatalf("run = %+v", finished)
	}
	card := h.card(inv)
	if !cardFinal(card) || card.FailureCode != chat.AgentAnswerTitleOnlyCode {
		t.Fatalf("card = %+v", card)
	}
	reason := chat.AgentAnswerFailureFor("en-US", "Policy Helper", card.FailureCode)
	if reason.Sentence != "Policy Helper answered with only the name of a document, so the answer is not shown." || strings.Contains(reason.Sentence, "not available") {
		t.Fatalf("the card reads %q", reason.Sentence)
	}

	// A reply that says something is not refused by this check: it goes on to the
	// next stage, which this fixture does not have, and ends there instead.
	said := h.request("")
	if _, err = h.state.Start(h.ctx, h.admit(said, 2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	err = chatbug049Dispatch(h, chatbug049Model{text: "Employees carry over up to 40 hours [[1]]."})
	if !errors.As(err, &failure) || failure.Code != "OUTPUT_REJECTED" {
		t.Fatalf("a reply with a statement ended as %v, want the later output refusal", err)
	}
	if card = h.card(said); card.FailureCode != "OUTPUT_REJECTED" || strings.Contains(chat.AgentAnswerFailureFor("en-US", "Policy Helper", card.FailureCode).Sentence, "not available") {
		t.Fatalf("card = %+v", card)
	}
}
