package chat

import (
	"context"
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// audienceRecorder keeps the send the transport handed the service and answers
// the tags of two posts.
type audienceRecorder struct {
	*transportChatFake
	sent chatcore.SendPostRequest
}

func (r *audienceRecorder) SendPost(_ context.Context, request chatcore.SendPostRequest) (chatcore.Post, error) {
	r.sent = request
	return chatcore.Post{ID: "p-sent", TenantID: request.TenantID, ConversationID: request.ConversationID, AuthorID: "u", Body: request.Body}, nil
}

func (r *audienceRecorder) ListPosts(context.Context, chatcore.ListPostsRequest) (chatcore.ListPostsResponse, error) {
	return chatcore.ListPostsResponse{Posts: []chatcore.Post{{ID: "p-open"}, {ID: "p-sent"}, {ID: "p-only"}}}, nil
}

func (r *audienceRecorder) PostAudiences(_ context.Context, caller chatcore.Principal, _ string, ids []string) map[string]chatcore.PostAudienceInfo {
	if caller.SubjectID != "u" || len(ids) == 0 {
		return nil
	}
	return map[string]chatcore.PostAudienceInfo{
		"p-sent": {Segments: []string{"Denver office"}},
		"p-only": {Segments: []string{"Night shift"}, Restricted: true, People: 4},
	}
}

// TestTodo_CHATGATE_014: a send names the segments it is for and whether it is
// restricted to them and the service is asked for exactly that; a message for
// everyone asks for nothing; the posts an answer carries are tagged with who they
// are for, and only those.
func TestTodo_CHATGATE_014(t *testing.T) {
	ctx := admittedChatContext(t)
	f := &audienceRecorder{transportChatFake: &transportChatFake{}}
	s := &server{deps: Dependencies{Service: f}}

	sent, err := s.SendPost(ctx, &chatv1.SendPostRequest{TenantId: "t", ConversationId: "c", Body: "hello", IdempotencyKey: "k", AudienceSegments: []string{"Denver office"}, AudienceRestricted: true})
	if err != nil {
		t.Fatal(err)
	}
	if a := f.sent.Audience; a == nil || len(a.Segments) != 1 || a.Segments[0] != "Denver office" || !a.Restricted {
		t.Fatalf("the service was asked for %+v", f.sent.Audience)
	}
	if got := sent.GetPost(); len(got.GetAudienceSegments()) != 1 || got.GetAudienceSegments()[0] != "Denver office" || got.GetAudienceRestricted() {
		t.Fatalf("the sender's answer is not tagged: %+v", got)
	}
	if _, err = s.SendPost(ctx, &chatv1.SendPostRequest{TenantId: "t", ConversationId: "c", Body: "hello", IdempotencyKey: "k2"}); err != nil {
		t.Fatal(err)
	}
	if f.sent.Audience != nil {
		t.Fatalf("a message for everyone asked for %+v", f.sent.Audience)
	}

	listed, err := s.ListPosts(ctx, &chatv1.ListPostsRequest{TenantId: "t", ConversationId: "c"})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]*chatv1.Post{}
	for _, p := range listed.GetPosts() {
		byID[p.GetId()] = p
	}
	if len(byID["p-open"].GetAudienceSegments()) != 0 {
		t.Errorf("a post for everyone is tagged: %+v", byID["p-open"])
	}
	if only := byID["p-only"]; !only.GetAudienceRestricted() || only.GetAudiencePeople() != 4 || only.GetAudienceSegments()[0] != "Night shift" {
		t.Errorf("the restricted post's tag: %+v", only)
	}
	if byID["p-sent"].GetAudienceRestricted() || byID["p-sent"].GetAudiencePeople() != 0 || len(byID["p-sent"].GetAudienceSegments()) != 1 {
		t.Errorf("the addressed post's tag: %+v", byID["p-sent"])
	}
	// A service that tells no tags answers as it always did.
	plain := &server{deps: Dependencies{Service: &transportChatFake{}}}
	plain.markAudiences(ctx, chatcore.Principal{SubjectID: "u"}, "t", []*chatv1.Post{{Id: "x"}})
}
