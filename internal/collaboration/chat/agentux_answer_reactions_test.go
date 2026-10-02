package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type qualityReactionAuthority struct{ binding AgentReactionBinding }

func (a qualityReactionAuthority) ResolveAgentReaction(context.Context, Principal, string, string, string) (AgentReactionBinding, error) {
	return a.binding, nil
}

func TestAgentUXQuality_Reactions(t *testing.T) {
	choices := AgentReactionChoices()
	if len(choices) != 40 {
		t.Fatalf("reaction vocabulary=%d", len(choices))
	}
	seen := map[string]bool{}
	for _, choice := range choices {
		if choice.Hint == "" || choice.Emoji == "" || seen[choice.Key] || !ValidAgentReactionKey(choice.Key) || AgentAnswerReaction(choice.Key, false) != choice.Emoji || AgentAnswerReaction(choice.Key, true) != "✅" {
			t.Fatalf("unsafe or incomplete reaction choice: %+v", choice)
		}
		seen[choice.Key] = true
	}
	choices[0].Key = "mutated"
	if AgentAnswerReaction("unknown", false) != "✅" || AgentReactionChoices()[0].Key != "check" || ValidAgentReactionKey("🏖️") {
		t.Fatal("reaction fallback or table isolation failed")
	}
}

func TestAgentUXQuality_Reactions_Security(t *testing.T) {
	now := time.Now().UTC()
	identity, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "t1", Subject: "agent-a", SubjectKind: trust.SubjectKindAgent, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "quality-reaction", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "fixture-digest"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), identity)
	principal := Principal{TenantID: "t1", SubjectID: "agent-a"}
	store := &fakeStore{conversation: Conversation{ID: "c1", TenantID: "t1", Kind: PublicChannel, Revision: 1}, post: Post{ID: "p1", TenantID: "t1", ConversationID: "c1", Body: "Question"}}
	service := newChatfilterTestService(store, time.Now)
	binding := AgentReactionBinding{TenantID: "t1", ConversationID: "c1", InvokingPostID: "p1", AgentSubjectID: "agent-a", Enabled: true}
	request := AddReactionRequest{Principal: principal, Reaction: Reaction{TenantID: "t1", ConversationID: "c1", PostID: "p1", Emoji: "👀"}}
	if _, err := service.AddReaction(ctx, request); !errors.Is(err, ErrPermissionDenied) || store.mutations != 0 {
		t.Fatalf("missing invocation authority: %v mutations=%d", err, store.mutations)
	}
	service.SetAgentReactionAuthority(qualityReactionAuthority{binding})
	if _, err := service.AddReaction(ctx, request); err != nil || store.mutations != 1 {
		t.Fatalf("admitted reaction: %v mutations=%d", err, store.mutations)
	}
	for _, change := range []func(*AddReactionRequest){func(r *AddReactionRequest) { r.Reaction.PostID = "other" }, func(r *AddReactionRequest) { r.Reaction.TenantID = "other" }, func(r *AddReactionRequest) { r.Principal.SubjectID = "spoof" }, func(r *AddReactionRequest) { r.Reaction.Emoji = "unsafe" }} {
		foreign := request
		change(&foreign)
		if _, err := service.AddReaction(ctx, foreign); err == nil || store.mutations != 1 {
			t.Fatalf("forged reaction accepted: %+v err=%v", foreign, err)
		}
	}
	binding.PublicPrivate = true
	service.SetAgentReactionAuthority(qualityReactionAuthority{binding})
	request.Reaction.Emoji = "🏖️"
	if _, err := service.AddReaction(ctx, request); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("private topic hinted publicly: %v", err)
	}
	binding.Enabled = false
	service.SetAgentReactionAuthority(qualityReactionAuthority{binding})
	request.Reaction.Emoji = "✅"
	if _, err := service.AddReaction(ctx, request); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("disabled setting ignored: %v", err)
	}
	if err := service.RemoveReaction(ctx, RemoveReactionRequest{Principal: principal, TenantID: "t1", ConversationID: "c1", PostID: "p1", Emoji: "👀"}); err != nil || store.mutations != 2 {
		t.Fatalf("working reaction not removable after disabling: %v", err)
	}
}
