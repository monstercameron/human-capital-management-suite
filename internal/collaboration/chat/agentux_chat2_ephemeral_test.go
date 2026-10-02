package chat

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

type agentUXChat2DirectStore struct{ *fakeStore }

func (s *agentUXChat2DirectStore) ListMemberships(context.Context, string, string, Page) (ListMembershipsResponse, error) {
	return ListMembershipsResponse{Memberships: []Membership{
		{TenantID: "t1", HomeTenantID: "t1", SubjectID: "u1", ConversationID: "agent-dm"},
		{TenantID: "t1", HomeTenantID: "t1", SubjectID: "policy-agent", ConversationID: "agent-dm"},
	}}, nil
}

func TestAgentUXChat2_DirectAgentReplyIsOneDurableUnreadPost(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	base := &fakeStore{conversation: Conversation{ID: "agent-dm", TenantID: "t1", Kind: Direct, Name: "Policy Helper", Revision: 1}, membership: Membership{TenantID: "t1", HomeTenantID: "t1", SubjectID: "u1", ConversationID: "agent-dm"}}
	store := &agentUXChat2DirectStore{fakeStore: base}
	service := NewService(store, func() time.Time { return now })
	service.SetAuthority(verifiedAuthority{store: base})
	ephemeral := NewMemoryEphemeralStore(func() time.Time { return now })
	service.SetEphemeralStore(ephemeral)
	service.SetPersonaDMResolver(testPersonaDMResolver{conversationID: "agent-dm"})
	request := SendEphemeralPostRequest{
		Principal: Principal{TenantID: "t1", SubjectID: "u1"}, TenantID: "t1", ConversationID: "agent-dm", ThreadID: "agent-dm",
		Body: "The policy answer.", AuthorAsAgent: true, IdempotencyKey: "reply-1",
	}
	if err := service.authorize(context.Background(), request.Principal, base.conversation, chatpolicy.ActionPost); err != nil {
		t.Fatalf("source authorization: %v", err)
	}
	if _, err := service.resolvePersonaDirectAuthor(context.Background(), request, "agent-dm"); err != nil {
		t.Fatalf("persona direct author validation: %v", err)
	}

	got, err := service.SendEphemeralPost(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if base.sent.AuthorID != "policy-agent" || base.sent.Body != "The policy answer." || got.DurableCopyPostID != base.sent.ID || got.ID != base.sent.ID {
		t.Fatalf("durable direct reply = %+v, receipt=%+v", base.sent, got)
	}
	if posts, _, err := ephemeral.ListEphemeral(context.Background(), Principal{TenantID: "t1", SubjectID: "u1"}, "t1", "agent-dm", 0, 10); err != nil || len(posts) != 0 {
		t.Fatalf("direct agent reply created duplicate ephemeral post: %+v, err=%v", posts, err)
	}
}

func TestAgentUXChat2_QuestionBacklinkKeepsChannelHash(t *testing.T) {
	got := durableEphemeralBodyFrom("Answer", "/chat/share/signed", "#general")
	if got != "Answer\n\n[chat-agent-question:#general](/chat/share/signed)" {
		t.Fatalf("durable answer marker = %q", got)
	}
}

func TestAgentUXR5Srv_PrivateAnswerProjectsOriginalQuestion(t *testing.T) {
	at := time.Date(2026, 10, 1, 9, 25, 0, 0, time.UTC)
	body := durableEphemeralBodyFromQuestion("Carry over is limited to five days.", "/chat/share/signed", "#general", Post{ID: "question-a", Body: "How much leave can I carry over?", CreatedAt: at})
	if !strings.HasPrefix(body, "Carry over is limited to five days.\n\n[chat-agent-question-context:") || !strings.HasSuffix(body, "](/chat/share/signed)") || strings.Contains(body, "How much leave") {
		t.Fatalf("question context was not projected as hidden metadata: %q", body)
	}
	encoded := strings.TrimSuffix(strings.TrimPrefix(body, "Carry over is limited to five days.\n\n[chat-agent-question-context:"), "](/chat/share/signed)")
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || !strings.Contains(string(decoded), `"label":"#general"`) || !strings.Contains(string(decoded), `"text":"How much leave can I carry over?"`) || !strings.Contains(string(decoded), at.Format(time.RFC3339)) {
		t.Fatalf("question context metadata=%s err=%v", decoded, err)
	}
}

func TestAgentUXR5Srv_PrivateAnswerQuestion_Security(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 25, 0, 0, time.UTC)
	joined := now.Add(-time.Hour)
	base := &fakeStore{
		conversation: Conversation{ID: "private-a", TenantID: "t1", Kind: PrivateChannel, Revision: 1},
		membership:   Membership{TenantID: "t1", HomeTenantID: "t1", SubjectID: "u1", ConversationID: "private-a", JoinedAt: &joined},
	}
	service := newTestService(base, func() time.Time { return now })
	ephemeral := NewMemoryEphemeralStore(func() time.Time { return now })
	service.SetEphemeralStore(ephemeral)
	threadLink := ephemeralQuestionThreadLink("/chat/share/signed", "Private", Post{ID: "question-a", Body: "My question", CreatedAt: now})
	if _, err := ephemeral.PutEphemeral(context.Background(), EphemeralPost{ID: "answer-a", TenantID: "t1", ConversationID: "private-a", RecipientHomeTenantID: "t1", RecipientSubjectID: "u1", Body: "Answer", ThreadLink: threadLink, OnlyVisibleToYou: true, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	request := ListEphemeralPostsRequest{Principal: Principal{TenantID: "t1", SubjectID: "u1"}, TenantID: "t1", ConversationID: "private-a", PageSize: 10}
	if posts, _, err := service.ListEphemeralPosts(context.Background(), request); err != nil || len(posts) != 1 || !strings.Contains(posts[0].ThreadLink, "#hcm-question=") {
		t.Fatalf("current member posts=%+v err=%v", posts, err)
	}
	left := now
	base.membership.LeftAt = &left
	if posts, _, err := service.ListEphemeralPosts(context.Background(), request); !errors.Is(err, ErrPermissionDenied) || len(posts) != 0 {
		t.Fatalf("former member posts=%+v err=%v", posts, err)
	}
}
