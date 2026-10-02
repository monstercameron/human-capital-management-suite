package application

import (
	"context"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"testing"
)

type integrate2ReaderFixture struct {
	chat.ConversationService
	post   chat.Post
	calls  int
	denied bool
}

func (f *integrate2ReaderFixture) GetConversation(context.Context, chat.GetConversationRequest) (chat.Conversation, error) {
	if f.denied {
		return chat.Conversation{}, chat.ErrPermissionDenied
	}
	return chat.Conversation{}, nil
}
func (f *integrate2ReaderFixture) ListPosts(context.Context, chat.ListPostsRequest) (chat.ListPostsResponse, error) {
	f.calls++
	if f.denied {
		return chat.ListPostsResponse{}, chat.ErrPermissionDenied
	}
	return chat.ListPostsResponse{Posts: []chat.Post{f.post}}, nil
}
func TestIntegrate2ReaderAuthorityAndOriginal(t *testing.T) {
	reader := &integrate2ReaderFixture{post: chat.Post{ID: "post", TenantID: "tenant-a", ConversationID: "room", Body: "authored", Revision: 2}}
	p := chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}
	scope := chatstore.RenderingScope{Principal: p, Tenant: "tenant-a", Conversation: "room"}
	policy := integrate2RenderingPolicy{Chat: reader}
	ctx := chatrenderRequest(t, "GET", "/", "").Context()
	if err := policy.AuthorizeRendering(ctx, scope, "selection"); err != nil {
		t.Fatal(err)
	}
	if post, err := integrate2ReadPost(ctx, reader, p, scope.Tenant, scope.Conversation, "post"); err != nil || post.Body != "authored" || post.Revision != 2 {
		t.Fatal(post, err)
	}
	if _, err := integrate2ReadPost(ctx, reader, p, scope.Tenant, "another-room", "post"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatal("cross-room original", err)
	}
	scope.Principal.SubjectID = "forged"
	if err := policy.AuthorizeRendering(ctx, scope, "selection"); !errors.Is(err, chatrender.ErrDenied) {
		t.Fatal("forged reader", err)
	}
	scope.Principal = p
	scope.Conversation = ""
	scope.Tenant = "foreign"
	if err := policy.AuthorizeRendering(ctx, scope, "settings"); !errors.Is(err, chatrender.ErrDenied) {
		t.Fatal("foreign preferences", err)
	}
	reader.denied = true
	if _, err := integrate2ReadPost(ctx, reader, p, "tenant-a", "room", "post"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal(err)
	}
}

type integrate2RouteFixture struct {
	calls        int
	tenant, room string
	err          error
}

func (f *integrate2RouteFixture) ChatWriteContext(ctx context.Context, tenant, room string) (context.Context, error) {
	f.calls++
	f.tenant, f.room = tenant, room
	return ctx, f.err
}
func TestIntegrate2GateRequiresRouteAndSession(t *testing.T) {
	port := &chatgateHTTPFixture{}
	route := &integrate2RouteFixture{}
	surface := integrate2GateSurface{ChatgateSurface: port, Routes: route}
	if _, err := surface.GateRequest(context.Background(), ChatgateRequest{Conversation: "room"}); !errors.Is(err, chatgate.ErrDenied) || port.calls != 0 {
		t.Fatal(err)
	}
	ctx := chatrenderRequest(t, "GET", "/", "").Context()
	if _, err := surface.GateRequest(ctx, ChatgateRequest{Conversation: "room"}); err != nil || port.calls != 1 || route.tenant != "tenant-a" || route.room != "room" {
		t.Fatal("gate route", route, port.calls, err)
	}
	route.err = chat.ErrUnavailable
	if _, err := surface.GateRequest(ctx, ChatgateRequest{Conversation: "room"}); !errors.Is(err, chat.ErrUnavailable) || port.calls != 1 {
		t.Fatal("gate route bypass", err)
	}
}

func TestIntegrate2SavedReaderProjection(t *testing.T) {
	f := &chatrenderHTTPFixture{rendering: chatrender.Rendering{Message: "post", Revision: 1, Text: "selected", Tone: chatrender.AsWritten}}
	service := integrate2Saved{Rendering: f}
	items := []chat.SavedItem{{TenantID: "tenant-a", ConversationID: "room", PostID: "post", Availability: "readable", Note: "private note", Post: &chat.Post{ID: "post", Body: "authored"}}}
	result := service.readerItems(context.Background(), chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}, items)
	if result[0].Post.Body != "selected" || items[0].Post.Body != "authored" || len(integrate2MatchSaved(result, "authored")) != 0 || len(integrate2MatchSaved(result, "private note")) != 1 {
		t.Fatal("saved rendering/original leak", result, items)
	}
	if integrate2SavedPort(composedChat{}) != nil {
		t.Fatal("missing saved core exposed")
	}
}

func TestIntegrate2AgentControlSourceRequiresAttestation(t *testing.T) {
	body, err := chatui.AnnouncementMessageBody(chatui.AgentAnnouncementMessage{OwnerName: "Alex", Text: "Visible authored answer"})
	if err != nil {
		t.Fatal(err)
	}
	post := chat.Post{TenantID: "tenant-a", ConversationID: "channel-a", AuthorID: "agent-ref:comp", Body: body}
	policy := integrate2RenderingPolicy{}
	if _, err := policy.visibleSource(t.Context(), post); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatal("unattested control envelope selected", err)
	}
	facts := currentPersonaReferenceFacts(post.AuthorID)
	facts.InstallationExpires = facts.InstallationExpires.AddDate(1, 0, 0)
	policy.Personas = &personaReferenceLookupFake{byReference: map[string]personaReferenceFacts{post.AuthorID: facts}}
	source, err := policy.visibleSource(t.Context(), post)
	if err != nil || source.Body != "Visible authored answer" || post.Body != body {
		t.Fatal("visible source or original mutated", source, post, err)
	}
}

func TestIntegrate2ReaderPreservesAgentAuthorFact(t *testing.T) {
	ctx := chatrenderRequest(t, "GET", "/", "").Context()
	facts := currentPersonaReferenceFacts("agent-ref:comp")
	facts.InstallationExpires = facts.InstallationExpires.AddDate(1, 0, 0)
	source := chatFilterIdentity{personas: &personaReferenceLookupFake{byReference: map[string]personaReferenceFacts{"agent-ref:comp": facts}}}
	post := chat.Post{TenantID: "tenant-a", ConversationID: "channel-a", AuthorID: "agent-ref:comp", AuthorHomeTenantID: "tenant-a"}
	input, err := source.FilterAuthorInput(ctx, post)
	if err != nil || !input.Agent || input.Subject != post.AuthorID {
		t.Fatal("agent exemption fact lost in reader projection", input, err)
	}
	source.personas = nil
	input, err = source.FilterAuthorInput(ctx, post)
	if err != nil || input.Agent {
		t.Fatal("missing author fact granted agent exemption", input, err)
	}
}
