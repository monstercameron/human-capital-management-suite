package application

import (
	"context"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"testing"
)

type chattonePGContext struct{ chat *chat.Service }

func (s chattonePGContext) WritingContext(ctx context.Context, id chatrewrite.Identity) (chatrewrite.ConversationFacts, []string, error) {
	p := chat.Principal{TenantID: id.Tenant, SubjectID: id.Person}
	page, err := s.chat.ListPosts(ctx, chat.ListPostsRequest{Principal: p, TenantID: id.Tenant, ConversationID: id.Conversation, Descending: true, Page: chat.Page{PageSize: 6}})
	if err != nil {
		return chatrewrite.ConversationFacts{}, nil, err
	}
	recent := []string{}
	lengths := []int{}
	for _, post := range page.Posts {
		recent = append(recent, post.Body)
		lengths = append(lengths, len(post.Body))
	}
	return chatrewrite.ConversationFacts{RecentLengths: lengths, MemberCount: 2}, recent, nil
}
func TestTodo_CHATTONE_004_Integration(t *testing.T) {
	s, ctx, model, _, _, ledger := chattoneFixture(t)
	db := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, db)
	raw, err := chatstore.New(context.Background(), chatstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(raw.Close)
	store := chatstore.NewAdapter(raw)
	service := chat.NewService(store, s.Now)
	service.SetAuthority(servedPersonaChatAuthority{})
	owner := chat.Principal{TenantID: "tenant", SubjectID: "owner"}
	if _, err := service.CreateConversation(context.Background(), chat.CreateConversationRequest{Principal: owner, TenantID: "tenant", ConversationID: "room", Kind: chat.PublicChannel, Name: "room"}); err != nil {
		t.Fatal(err)
	}
	member, err := service.AddMembership(context.Background(), chat.AddMembershipRequest{Principal: owner, Membership: chat.Membership{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: "room", SubjectID: "person", HistoryVisibility: chat.FullHistory}})
	if err != nil {
		t.Fatal(err)
	}
	post, err := service.SendPost(context.Background(), chat.SendPostRequest{Principal: owner, TenantID: "tenant", ConversationID: "room", Body: "Please keep 42 unchanged.", IdempotencyKey: "original"})
	if err != nil {
		t.Fatal(err)
	}
	s.Authority = ChattoneChatAuthority{Chat: &PersonaChatSurface{Chat: service, Now: s.Now}}
	s.Conversations = chattonePGContext{service}
	suggestion, err := s.ReadSuggestion(ctx, "room")
	if err != nil || suggestion.Suggestion == nil || suggestion.Suggestion.StyleID != "concise" {
		t.Fatal(suggestion, err)
	}
	reply, err := s.RewriteDraft(ctx, ChattoneDraft{ConversationID: "room", Draft: "Please retain @Dana and 42.", StyleID: "professional"})
	if err != nil || reply.Draft != "Kindly retain @Dana and 42." || model.calls != 1 || len(ledger.Lines()) != 2 {
		t.Fatal(reply, err)
	}
	saved, err := store.GetPost(ctx, "tenant", "room", post.ID)
	if err != nil || saved.Body != post.Body || saved.Revision != post.Revision {
		t.Fatal("rewrite mutated the record", saved, err)
	}
	if _, err := service.RemoveMembership(context.Background(), chat.RemoveMembershipRequest{Principal: owner, TenantID: "tenant", HomeTenantID: "tenant", ConversationID: "room", SubjectID: "person", ExpectedRevision: member.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RewriteDraft(ctx, ChattoneDraft{ConversationID: "room", Draft: "Please retain this draft.", StyleID: "professional"}); !errors.Is(err, personachat.ErrDenied) || model.calls != 1 {
		t.Fatal("revocation did not precede model call", err)
	}
	if _, err := store.GetPost(ctx, "other", "room", post.ID); err == nil {
		t.Fatal("store crossed tenant boundary")
	}
}
