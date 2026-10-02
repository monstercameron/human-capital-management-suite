package chat

import (
	"context"
	"reflect"
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type reactionBatchService struct {
	transportChatFake
	calls int
	got   chatcore.ListReactionsRequest
}

func (s *reactionBatchService) ListReactions(_ context.Context, r chatcore.ListReactionsRequest) (chatcore.ListReactionsResponse, error) {
	s.calls++
	s.got = r
	out := chatcore.ListReactionsResponse{}
	for _, id := range r.PostIDs {
		out.Reactions = append(out.Reactions, chatcore.Reaction{TenantID: r.TenantID, ConversationID: r.ConversationID, PostID: id, HomeTenantID: "home", SubjectID: "u", Emoji: "+1"})
	}
	return out, nil
}

// TestTodo_CHATBUG_014_ReactionBatch: the posts a request names reach the
// service as one call, and each reaction comes back saying which post it is on.
func TestTodo_CHATBUG_014_ReactionBatch(t *testing.T) {
	ctx := admittedChatContext(t)
	service := &reactionBatchService{}
	s := &server{deps: Dependencies{Service: service}}
	out, err := s.ListReactions(ctx, &chatv1.ListReactionsRequest{TenantId: "host", ConversationId: "c", PostIds: []string{"p1", "p2", "p3"}, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if service.calls != 1 {
		t.Fatalf("three posts cost %d service calls, want one", service.calls)
	}
	if !reflect.DeepEqual(service.got.PostIDs, []string{"p1", "p2", "p3"}) || service.got.PostID != "" || service.got.Page.PageSize != 100 || service.got.ConversationID != "c" {
		t.Fatalf("the service was asked %+v", service.got)
	}
	if service.got.Principal.SubjectID == "" {
		t.Fatal("the batch was read without the admitted principal")
	}
	var posts []string
	for _, reaction := range out.GetReactions() {
		posts = append(posts, reaction.GetPostId())
	}
	if !reflect.DeepEqual(posts, []string{"p1", "p2", "p3"}) || out.GetNextCursor() != "" {
		t.Fatalf("answer posts=%v cursor=%q", posts, out.GetNextCursor())
	}
}
