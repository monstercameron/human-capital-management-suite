package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type backgroundReplyRefusal struct {
	calls  int
	record agentrun.Record
	run    runstate.Run
	err    error
}

func (r *backgroundReplyRefusal) DeliverBackgroundPersonaReply(_ context.Context, record agentrun.Record, run runstate.Run, _ agentsecurity.FinalOutputPersistence) (PersonaReplyDeliveryReceipt, error) {
	r.calls++
	r.record = record
	r.run = run
	return PersonaReplyDeliveryReceipt{}, r.err
}

func TestTodo_AGENTP_008_Security_BackgroundDeliveryDoesNotSubstituteHumanIdentity(t *testing.T) {
	denied := errors.New("independent native owner refusal")
	native := &backgroundReplyRefusal{err: denied}
	executor := &personaAdmittedRunExecutor{backgroundReply: native}
	record := agentrun.Record{ID: "accepted"}
	run := runstate.Run{ID: "accepted", Fence: 4, Version: 8}
	if _, err := executor.deliver(context.Background(), record, run, agentsecurity.FinalOutputPersistence{}); !errors.Is(err, denied) || native.calls != 1 || native.record.ID != record.ID || native.run.Fence != run.Fence {
		t.Fatalf("native owner err=%v calls=%d", err, native.calls)
	}
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	principal := foregroundPrincipal(t, at.Add(-time.Hour), at.Add(-time.Minute), "persona-mention", "human", "tenant")
	if _, err := executor.deliver(trust.WithPrincipal(context.Background(), principal), record, run, agentsecurity.FinalOutputPersistence{}); !errors.Is(err, ErrPersonaRunOutputRejected) || native.calls != 1 {
		t.Fatalf("human caller was substituted with worker: %v calls=%d", err, native.calls)
	}
	executor.backgroundReply = nil
	if _, err := executor.deliver(context.Background(), record, run, agentsecurity.FinalOutputPersistence{}); !errors.Is(err, ErrPersonaRunOutputRejected) {
		t.Fatal(err)
	}
}

func TestTodo_AGENTP_008_Security_BackgroundWakeRejectsUnboundComposition(t *testing.T) {
	if _, err := NewPersonaBackgroundDispatcher(nil, nil, nil, nil); !errors.Is(err, errPersonaRunModelWorker) {
		t.Fatal(err)
	}
	var dispatcher *PersonaBackgroundDispatcher
	if err := dispatcher.Wake(context.Background(), "tenant", "admission"); !errors.Is(err, errPersonaRunModelWorker) {
		t.Fatal(err)
	}
	if err := dispatcher.DispatchTenant(context.Background(), "tenant", 1); !errors.Is(err, errPersonaRunModelWorker) {
		t.Fatal(err)
	}
	dispatcher = &PersonaBackgroundDispatcher{}
	if err := dispatcher.Wake(context.Background(), "tenant", "admission"); !errors.Is(err, errPersonaRunModelWorker) {
		t.Fatal(err)
	}
	if err := dispatcher.DispatchTenant(context.Background(), "tenant", 1); !errors.Is(err, errPersonaRunModelWorker) {
		t.Fatal(err)
	}
	if _, err := (&PersonaFinalOutputSource{}).RecoverPersonaRunOutput(context.Background(), agentrun.Record{}, runstate.Run{}); !errors.Is(err, errPersonaFinalOutputSourceUnavailable) {
		t.Fatal(err)
	}
	native := &backgroundReplyRefusal{err: errors.New("unexpected")}
	if _, err := (personaFencedBackgroundReply{inner: native}).DeliverBackgroundPersonaReply(context.Background(), agentrun.Record{}, runstate.Run{}, agentsecurity.FinalOutputPersistence{}); !errors.Is(err, ErrPersonaRunDeliveryFailure) || native.calls != 0 {
		t.Fatalf("missing fence reached delivery: %v calls=%d", err, native.calls)
	}
}
