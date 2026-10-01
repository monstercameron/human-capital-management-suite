package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
)

func TestTodo_AGENTP_017_RuntimeRecoveryRejectsUnverifiedAndUntypedPayload(t *testing.T) {
	if _, err := (&personaRuntimeOutputRecovery{}).RehydrateFinalOutput(context.Background(), agentsecurity.FinalOutputRecoveryRecord{}); !errors.Is(err, agentsecurity.ErrFinalOutputRecoveryUnavailable) {
		t.Fatalf("missing recovery owners accepted: %v", err)
	}
	for _, raw := range []string{`{}`, `{"payload":{"Result":{"Schema":"unknown","Value":{"text":"reply"}},"Narrative":"reply"}}`, `{"payload":{"Result":{"Schema":"persona.chat-reply.v1","Value":{"text":"reply"}},"Narrative":"changed"}}`, `{"payload":{"Result":{"Schema":"persona.chat-reply.v1","Value":{"text":"https://secret.example"}},"Narrative":"https://secret.example"}}`} {
		if _, err := personaRuntimeRecoveryText(json.RawMessage(raw)); !errors.Is(err, agentsecurity.ErrFinalOutputRecoveryUnavailable) {
			t.Fatalf("untyped retained payload accepted: %s %v", raw, err)
		}
	}
	raw := json.RawMessage(`{"payload":{"Result":{"Schema":"persona.chat-reply.v1","Value":{"text":"A governed reply"}},"Narrative":"A governed reply"}}`)
	if text, err := personaRuntimeRecoveryText(raw); err != nil || text != "A governed reply" {
		t.Fatalf("retained reply text=%q err=%v", text, err)
	}
}

type runtimeReplyCurrentAuthorityFake struct {
	err      error
	calls    int
	identity agentsecurity.FinalOutputIdentity
}

func (f *runtimeReplyCurrentAuthorityFake) AuthorizeRecoveredFinalOutput(_ context.Context, output agentsecurity.FinalOutputPersistence) error {
	f.calls++
	f.identity = output.Identity()
	return f.err
}

type runtimeReplyDeliveryFake struct{ calls int }

func (f *runtimeReplyDeliveryFake) Deliver(context.Context, PersonaReplyDeliveryRequest) (PersonaReplyDeliveryReceipt, error) {
	f.calls++
	return PersonaReplyDeliveryReceipt{Private: true, EphemeralPostID: "private-post"}, nil
}

func TestTodo_AGENTP_012_RuntimeDeliveryChecksCurrentSourcesBeforePrivateEffects(t *testing.T) {
	validator, record, run, _, _ := personaRunOutputFixture(t)
	output, err := validator.ValidateAndPersistPersonaOutput(context.Background(), record, run, agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Text: "A governed reply", Finish: agentmodel.FinishComplete})
	if err != nil {
		t.Fatal(err)
	}
	owner := &runtimeReplyCurrentAuthorityFake{err: errors.New("source revoked")}
	next := &runtimeReplyDeliveryFake{}
	delivery := &personaRuntimeCurrentReply{next: next, authority: owner}
	request := PersonaReplyDeliveryRequest{Output: output}
	if _, err := delivery.Deliver(context.Background(), request); !errors.Is(err, owner.err) || owner.calls != 1 || next.calls != 0 || owner.identity != output.Identity() {
		t.Fatalf("revoked source effects calls=%d owner=%d err=%v", next.calls, owner.calls, err)
	}
	owner.err = nil
	if receipt, err := delivery.Deliver(context.Background(), request); err != nil || !receipt.Private || next.calls != 1 {
		t.Fatalf("current delivery receipt=%+v calls=%d err=%v", receipt, next.calls, err)
	}
	request.Output = agentsecurity.FinalOutputPersistence{}
	before := owner.calls
	if _, err := delivery.Deliver(context.Background(), request); !errors.Is(err, ErrPersonaReplyDeliveryUnavailable) || owner.calls != before || next.calls != 1 {
		t.Fatalf("unsealed output reached owners calls=%d err=%v", owner.calls, err)
	}
}
