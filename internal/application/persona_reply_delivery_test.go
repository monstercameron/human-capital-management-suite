package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type personaReplyDeliveryChatFake struct{ chat.ConversationService }

func (*personaReplyDeliveryChatFake) SendEphemeralPost(context.Context, chat.SendEphemeralPostRequest) (chat.EphemeralPost, error) {
	return chat.EphemeralPost{}, errors.New("unexpected ephemeral write")
}

type personaReplyDeliveryCommitFake struct{}

func (personaReplyDeliveryCommitFake) CommitPersonaReply(context.Context, chat.PersonaReplyCommitRequest) (chat.Post, error) {
	return chat.Post{}, errors.New("unexpected public write")
}

type personaReplyDeliveryFloorFake struct{}

func (personaReplyDeliveryFloorFake) AuthorizePersonaOutput(context.Context, agentsecurity.FinalOutputPersistence) (chat.PersonaAudienceDecision, error) {
	return chat.PersonaAudienceDecision{}, errors.New("unexpected audience evaluation")
}

func TestTodo_AGENTP_012_DeliveryRequiresProductionEffects(t *testing.T) {
	service := &personaReplyDeliveryChatFake{}
	if _, err := NewPersonaReplyDelivery(service, personaReplyDeliveryCommitFake{}, personaReplyDeliveryFloorFake{}); err != nil {
		t.Fatalf("complete composition rejected: %v", err)
	}
	if _, err := NewPersonaReplyDelivery(service, nil, personaReplyDeliveryFloorFake{}); !errors.Is(err, ErrPersonaReplyDeliveryUnavailable) {
		t.Fatalf("missing commit capability error = %v", err)
	}
	if _, err := NewPersonaReplyDelivery(nil, personaReplyDeliveryCommitFake{}, personaReplyDeliveryFloorFake{}); !errors.Is(err, ErrPersonaReplyDeliveryUnavailable) {
		t.Fatalf("missing chat service error = %v", err)
	}
}

func TestTodo_AGENTP_012_DeliveryRejectsUnboundOutput(t *testing.T) {
	service := &personaReplyDeliveryChatFake{}
	delivery, err := NewPersonaReplyDelivery(service, personaReplyDeliveryCommitFake{}, personaReplyDeliveryFloorFake{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = delivery.Deliver(context.Background(), PersonaReplyDeliveryRequest{
		Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "alice"},
		Output:    agentsecurity.FinalOutputPersistence{}, IdempotencyKey: "reply-1",
	})
	if !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("unbound output error = %v", err)
	}
}
