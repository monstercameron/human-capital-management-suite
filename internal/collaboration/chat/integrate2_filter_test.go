package chat

import (
	"context"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"testing"
)

type integrate2FilterAuthor struct {
	input chatfilter.Input
	err   error
}

func (f integrate2FilterAuthor) FilterAuthorInput(context.Context, Post) (chatfilter.Input, error) {
	return f.input, f.err
}
func TestTodo_CHATMOD_003_Integrate2AuthorFacts(t *testing.T) {
	repo := &chatfilterFixture{action: "mask"}
	policy := &FilterContentPolicy{Filters: &chatfilter.Service{Store: repo, Registry: chatfilter.NewRegistry()}, AuthorIdentity: integrate2FilterAuthor{err: ErrUnavailable}}
	post := Post{ID: "post", TenantID: "tenant", AuthorHomeTenantID: "tenant", AuthorID: "author", Body: "quartz"}
	if _, err := policy.MaskedBody(t.Context(), post, Principal{TenantID: "tenant", SubjectID: "reader", Roles: []string{"hcm_admin"}}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("unavailable author facts bypassed", err)
	}
	policy.AuthorIdentity = integrate2FilterAuthor{input: chatfilter.Input{Tenant: "forged", Subject: "reader", Roles: []string{"hcm_admin"}}}
	if body, err := policy.MaskedBody(t.Context(), post, Principal{TenantID: "tenant", SubjectID: "reader"}); err != nil || body == post.Body {
		t.Fatal("reader exemption trusted", body, err)
	}
	f := &fakeStore{}
	s := WithFilterMetadata(newChatfilterTestService(f, nil))
	if _, err := s.CommitPersonaReply(t.Context(), PersonaReplyCommitRequest{Body: "quartz"}); !errors.Is(err, ErrPermissionDenied) || f.mutations != 0 {
		t.Fatal("unproved persona filtering side effect", err)
	}
}

func TestTodo_CHATMOD_003_Integrate2PersonaFilter(t *testing.T) {
	repo := &chatfilterFixture{action: "block"}
	policy := &FilterContentPolicy{Filters: &chatfilter.Service{Store: repo, Registry: chatfilter.NewRegistry()}}
	r := PersonaReplyCommitRequest{TenantID: "tenant", ConversationID: "room", AuthorID: "agent", AuthorHomeTenantID: "tenant", ExpectedAudienceRevision: 1, OutputDigest: "digest", Body: "quartz", ParentID: "thread"}
	r.Proof = personaDeliveryProof{tenantID: r.TenantID, conversationID: r.ConversationID, authorID: r.AuthorID, audienceRevision: 1, outputDigest: r.OutputDigest, bodyDigest: digestBody(r.Body, r.ParentID), parentID: r.ParentID}
	if err := policy.checkPersonaReplyContent(t.Context(), r, Conversation{Kind: PublicChannel}); !errors.Is(err, chatfilter.ErrBlocked) {
		t.Fatal("persona bypassed block", err)
	}
	r.Body = "allowed"
	r.Proof = personaDeliveryProof{tenantID: r.TenantID, conversationID: r.ConversationID, authorID: r.AuthorID, audienceRevision: 1, outputDigest: r.OutputDigest, bodyDigest: digestBody(r.Body, r.ParentID), parentID: r.ParentID}
	if err := policy.checkPersonaReplyContent(t.Context(), r, Conversation{Kind: PublicChannel}); err != nil {
		t.Fatal("opaque delivery required a human author session", err)
	}
}
