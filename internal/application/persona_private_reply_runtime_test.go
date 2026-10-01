package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type privateReplyRuntimeAuthorityFake struct{}

func (privateReplyRuntimeAuthorityFake) ResolvePersonaRunChatReplyAuthority(context.Context, agentrun.Record, runstate.Run) (PersonaRunChatReplyAuthority, error) {
	return PersonaRunChatReplyAuthority{}, errors.New("not used by composition test")
}

type privateReplyRuntimePersisterFake struct{}

func (privateReplyRuntimePersisterFake) PersistPersonaRunFinalOutput(context.Context, agentsecurity.FinalOutputPersistence) error {
	return nil
}

type privateReplyRuntimeChatFake struct{ chat.ConversationService }

func (privateReplyRuntimeChatFake) SendEphemeralPost(context.Context, chat.SendEphemeralPostRequest) (chat.EphemeralPost, error) {
	return chat.EphemeralPost{}, errors.New("not used by composition test")
}

type privateReplyRuntimeCommitterFake struct{}

func (privateReplyRuntimeCommitterFake) CommitPersonaReply(context.Context, chat.PersonaReplyCommitRequest) (chat.Post, error) {
	return chat.Post{}, errors.New("public delivery must remain disabled")
}

func TestPersonaPrivateReplyRuntimeRequiresAllProductionPorts(t *testing.T) {
	_, err := NewPersonaPrivateReplyRuntime(PersonaPrivateReplyRuntimeConfig{})
	if !errors.Is(err, errPersonaPrivateReplyRuntimeUnavailable) {
		t.Fatalf("missing dependencies error = %v", err)
	}
}

func TestPersonaPrivateReplyRuntimeComposesSealedOutputAndPrivateDelivery(t *testing.T) {
	runtime, err := NewPersonaPrivateReplyRuntime(PersonaPrivateReplyRuntimeConfig{
		OutputAuthority: privateReplyRuntimeAuthorityFake{},
		OutputPersister: privateReplyRuntimePersisterFake{},
		OutputSource:    &personaOutputSourceFake{},
		Chat:            privateReplyRuntimeChatFake{},
		ReplyCommitter:  privateReplyRuntimeCommitterFake{},
		OutputPolicy:    PersonaReplyOutputPolicy{TenantOrigin: "https://tenant.example"},
	})
	if err != nil {
		t.Fatalf("compose private reply runtime: %v", err)
	}
	if _, ok := runtime.Output.(*SealedPersonaRunOutputValidator); !ok {
		t.Fatalf("output validator type = %T", runtime.Output)
	}
	if _, ok := runtime.Reply.(*PersonaReplyDelivery); !ok {
		t.Fatalf("reply deliverer type = %T", runtime.Reply)
	}
	if runtime.OutputSource == nil {
		t.Fatal("output recovery source was not retained")
	}
	if _, err := NewPersonaReplyDeliveryWithOutputPolicy(privateReplyRuntimeChatFake{}, privateReplyRuntimeCommitterFake{}, personaPrivateOnlyAudienceFloor{}, PersonaReplyOutputPolicy{}); err != nil {
		t.Fatalf("private-only floor rejected by delivery: %v", err)
	}
}
