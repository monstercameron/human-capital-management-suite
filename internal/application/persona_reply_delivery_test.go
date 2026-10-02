package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatstream"
)

type personaReplyDeliveryChatFake struct{ chat.ConversationService }

func (*personaReplyDeliveryChatFake) SendEphemeralPost(context.Context, chat.SendEphemeralPostRequest) (chat.EphemeralPost, error) {
	return chat.EphemeralPost{}, errors.New("unexpected ephemeral write")
}

type agentUXPrivateDeliveryChat struct {
	chat.ConversationService
	publicWrites     int
	ephemeralWrites  int
	ephemeralRequest chat.SendEphemeralPostRequest
}

func (s *agentUXPrivateDeliveryChat) GetConversation(_ context.Context, request chat.GetConversationRequest) (chat.Conversation, error) {
	return chat.Conversation{ID: request.ConversationID, TenantID: request.TenantID, Kind: chat.PublicChannel}, nil
}

func (s *agentUXPrivateDeliveryChat) SendPost(context.Context, chat.SendPostRequest) (chat.Post, error) {
	s.publicWrites++
	return chat.Post{ID: "unexpected-public"}, nil
}

func (s *agentUXPrivateDeliveryChat) SendEphemeralPost(_ context.Context, request chat.SendEphemeralPostRequest) (chat.EphemeralPost, error) {
	s.ephemeralWrites++
	s.ephemeralRequest = request
	return chat.EphemeralPost{ID: "private-answer", DurableCopyPostID: "dm-answer", DurableCopyConversationID: "agent-dm", Body: request.Body}, nil
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

func TestTodo_AGENTUX_026_PrivateDeliveryWritesNoDurablePublicReceipt(t *testing.T) {
	validator, admission, run, _, _ := personaRunOutputFixture(t)
	output, err := validator.ValidateAndPersistPersonaOutput(context.Background(), admission, run, agentmodel.ModelResult{Text: "A private grounded answer.", Finish: agentmodel.FinishComplete})
	if err != nil {
		t.Fatal(err)
	}
	service := &agentUXPrivateDeliveryChat{}
	delivery, err := NewPersonaReplyDelivery(service, personaReplyDeliveryCommitFake{}, personaReplyDeliveryFloorFake{})
	if err != nil {
		t.Fatal(err)
	}
	identity := output.Identity()
	receipt, err := delivery.Deliver(context.Background(), PersonaReplyDeliveryRequest{
		Principal: chat.Principal{TenantID: identity.TenantID, SubjectID: identity.InvokerID},
		Output:    output, IdempotencyKey: "agentux-private-answer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if service.ephemeralWrites != 1 || service.publicWrites != 0 || !receipt.Private || receipt.Public || receipt.EphemeralPostID != "private-answer" || receipt.DurableCopyPostID != "dm-answer" {
		t.Fatalf("private delivery = %+v, ephemeral writes=%d public writes=%d", receipt, service.ephemeralWrites, service.publicWrites)
	}
	if !service.ephemeralRequest.AuthorAsAgent || service.ephemeralRequest.QuestionPostID != identity.PostID {
		t.Fatalf("private delivery did not bind agent authorship and original question: %+v identity=%+v", service.ephemeralRequest, identity)
	}
}

func TestTodo_AGENTUX_028_SecondChannelMemberCannotProjectPrivateAnswer(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	post := chat.EphemeralPost{
		ID: "answer", TenantID: "tenant", ConversationID: "general", ThreadID: "question",
		RecipientHomeTenantID: "tenant", RecipientSubjectID: "alice", Body: "private policy answer", OnlyVisibleToYou: true,
		CreatedAt: now, ExpiresAt: now.Add(time.Hour), ThreadLink: "/chat/general/question", Sequence: 17,
	}
	payload, err := json.Marshal(post)
	if err != nil {
		t.Fatal(err)
	}
	event := chatstream.Event{
		TenantID: "tenant", ConversationID: "general", Sequence: 17, Ephemeral: true, ExpiresAt: post.ExpiresAt, Payload: payload,
		RecipientHomeTenantID: "tenant", RecipientSubjectID: "alice",
	}
	bob := chat.WatchConversationRequest{Principal: chat.Principal{TenantID: "tenant", SubjectID: "bob"}, TenantID: "tenant", ConversationID: "general"}
	if projected, err := projectChatStreamEvent(context.Background(), bob, event, now); !errors.Is(err, chat.ErrPermissionDenied) || projected.EphemeralDelivery != nil {
		t.Fatalf("second member private projection = %+v, error=%v", projected, err)
	}
}
