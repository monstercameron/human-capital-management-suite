package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type personaChatConversationServiceStub struct {
	chat.ConversationService
	chat.ReferenceService
	post             chat.Post
	sendCalls        int
	referenceCalls   int
	lastRequest      chat.SendPostRequest
	lastReferenceReq chat.SendPostWithReferencesRequest
}

func (s *personaChatConversationServiceStub) SendPost(_ context.Context, request chat.SendPostRequest) (chat.Post, error) {
	s.sendCalls++
	s.lastRequest = request
	return s.post, nil
}

func (s *personaChatConversationServiceStub) SendPostWithReferences(_ context.Context, request chat.SendPostWithReferencesRequest) (chat.Post, error) {
	s.referenceCalls++
	s.lastReferenceReq = request
	return s.post, nil
}

func TestTodo_AGENT_027_ConversationDecoratorRoutesSendsThroughCoordinator(t *testing.T) {
	tests := []struct {
		name       string
		references []chat.Reference
		wantPlain  int
		wantWith   int
	}{
		{name: "plain post", wantPlain: 1},
		{name: "referenced post", references: []chat.Reference{{Kind: chat.PersonMention, ID: "person-1", TenantID: "tenant-1"}}, wantWith: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := &personaChatConversationServiceStub{post: chat.Post{ID: "post-1"}}
			coordinator := &personaChatInvocation{chat: base}
			service, err := newPersonaChatConversationService(base, base, coordinator, nil)
			if err != nil {
				t.Fatal(err)
			}
			request := chat.SendPostRequest{TenantID: "tenant-1", ConversationID: "conversation-1", Body: "hello", References: test.references}
			if len(test.references) == 0 {
				if _, err = service.SendPost(context.Background(), request); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err = service.SendPostWithReferences(context.Background(), chat.SendPostWithReferencesRequest{
					SendPostRequest: request,
					References:      test.references,
				}); err != nil {
					t.Fatal(err)
				}
			}
			if base.sendCalls != test.wantPlain || base.referenceCalls != test.wantWith {
				t.Fatalf("forwarded calls = plain %d, with references %d; want %d, %d", base.sendCalls, base.referenceCalls, test.wantPlain, test.wantWith)
			}
			if len(test.references) > 0 {
				if len(base.lastReferenceReq.References) != 1 || base.lastReferenceReq.References[0] != test.references[0] {
					t.Fatalf("references were not preserved by coordinator: %#v", base.lastReferenceReq.References)
				}
			}
		})
	}
}

func TestTodo_AGENTP_008_ConversationDecoratorRequiresCompositionPorts(t *testing.T) {
	base := &personaChatConversationServiceStub{}
	for _, test := range []struct {
		name         string
		conversation chat.ConversationService
		references   chat.ReferenceService
		invocation   *personaChatInvocation
	}{
		{name: "missing conversation", references: base, invocation: &personaChatInvocation{}},
		{name: "missing reference forwarding", conversation: base, invocation: &personaChatInvocation{}},
		{name: "missing coordinator", conversation: base, references: base},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := newPersonaChatConversationService(test.conversation, test.references, test.invocation, nil); err == nil {
				t.Fatal("constructor accepted incomplete composition")
			}
		})
	}
}
