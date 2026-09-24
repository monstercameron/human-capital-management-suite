package application

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// fakeDocumentChat answers as chat would for one reader: the rooms they may
// see, and share links only for posts in those rooms.
type fakeDocumentChat struct {
	chatcore.ConversationService
	rooms     []chatcore.Conversation
	posts     map[string]chatcore.Post
	listErr   error
	principal chatcore.Principal
	resolved  []string
}

func (f *fakeDocumentChat) ListConversations(_ context.Context, r chatcore.ListConversationsRequest) (chatcore.ListConversationsResponse, error) {
	f.principal = r.Principal
	if !r.IncludeDiscoverable {
		return chatcore.ListConversationsResponse{}, errors.New("want discoverable listing")
	}
	return chatcore.ListConversationsResponse{Conversations: f.rooms}, f.listErr
}

func (f *fakeDocumentChat) ResolveShareLink(_ context.Context, p chatcore.Principal, token string) (chatcore.Conversation, *chatcore.Post, error) {
	f.resolved = append(f.resolved, token)
	post, ok := f.posts[token]
	if !ok {
		return chatcore.Conversation{}, nil, chatcore.ErrPermissionDenied
	}
	for _, room := range f.rooms {
		if room.ID == post.ConversationID {
			return room, &post, nil
		}
	}
	return chatcore.Conversation{ID: post.ConversationID, TenantID: "other"}, &post, nil
}

func documentReaderContext(t *testing.T, tenant, subject string) context.Context {
	t.Helper()
	now := time.Now()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "digest", Roles: []string{"employee"}})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), p)
}

func TestDocumentChatRefsExtractSyntax(t *testing.T) {
	md := strings.Join([]string{
		"# Heading is not a channel",
		"Ask in #people-ops or #People_Ops, see [#exec](channel:conv-9) and ## nothing.",
		"Owner: @rafael.torres and @hc-050-rafael-torres, mail rafael@example.com, cc [@Ana](person:hc-7).",
		"Decision: /workspace/app/chat#share=tok_A1 and https://hcm.example/chat/share/tokB2.",
		"Issue #3 stays prose too, as does a#b.",
	}, "\n")
	got := extractDocumentChatRefs(md)
	if want := []string{"id:conv-9", "name:people-ops", "name:3"}; !reflect.DeepEqual(got.channels, want) {
		t.Fatalf("channels = %v, want %v", got.channels, want)
	}
	if want := []string{"hc-7", "rafael.torres", "hc-050-rafael-torres"}; !reflect.DeepEqual(got.people, want) {
		t.Fatalf("people = %v, want %v", got.people, want)
	}
	if want := []string{"tok_A1", "tokB2"}; !reflect.DeepEqual(got.messages, want) {
		t.Fatalf("messages = %v, want %v", got.messages, want)
	}
	if DocumentChannelName("  People  Ops_team ") != "people-ops-team" || DocumentPersonHandle("Rafael  Torres-Diaz") != "rafael.torres.diaz" {
		t.Fatal("normalizers changed")
	}
}

func TestDocumentChatRefsResolveAsReader(t *testing.T) {
	const tenant, reader = "tenant-a", "reader-1"
	chat := &fakeDocumentChat{
		rooms: []chatcore.Conversation{
			{ID: "conv-1", TenantID: tenant, Kind: chatcore.PublicChannel, Name: "People Ops", MemberCount: 14, Joined: true},
			{ID: "conv-2", TenantID: tenant, Kind: chatcore.PrivateChannel, Name: "leads", MemberCount: 4, Joined: true},
			{ID: "dm-1", TenantID: tenant, Kind: chatcore.Direct, Name: "dm"},
		},
		posts: map[string]chatcore.Post{
			"tok-ok":   {ID: "post-1", ConversationID: "conv-1", TenantID: tenant, AuthorID: "hc-7", Body: "We ship Friday.", CreatedAt: time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)},
			"tok-gone": {ID: "post-2", ConversationID: "conv-1", TenantID: tenant, AuthorID: "hc-7", Body: "deleted text", Deleted: true},
			"tok-far":  {ID: "post-3", ConversationID: "conv-x", TenantID: tenant, AuthorID: "hc-7", Body: "cross-tenant text"},
		},
	}
	people := []documentPerson{{ID: "hc-7", Name: "Ana Lopez"}, {ID: "hc-8", Name: "Sam Lee"}, {ID: "hc-9", Name: "Sam Lee"}}
	s := documentService{chat: chat, people: func(context.Context, string) ([]documentPerson, error) { return people, nil }}
	md := "See #people-ops, #dm, #nothing-here, [#x](channel:conv-2), [#y](channel:conv-secret). Ask @ana.lopez, @HC-7, @sam.lee, @nobody. " +
		"/workspace/app/chat#share=tok-ok /workspace/app/chat#share=tok-denied /workspace/app/chat#share=tok-gone /workspace/app/chat#share=tok-far"
	got := s.documentChatReferences(documentReaderContext(t, tenant, reader), tenant, reader, md)
	if chat.principal.SubjectID != reader || chat.principal.TenantID != tenant || len(chat.principal.Roles) != 1 {
		t.Fatalf("chat asked as %+v", chat.principal)
	}
	if len(got.Channels) != 3 {
		t.Fatalf("channels = %+v", got.Channels)
	}
	if c := got.Channels[2]; c.Key != "name:people-ops" || c.ConversationID != "conv-1" || c.MemberCount != 14 || c.Locked {
		t.Fatalf("named channel = %+v", c)
	}
	if c := got.Channels[0]; c.Key != "id:conv-2" || !c.Private || c.Name != "leads" {
		t.Fatalf("private member channel = %+v", c)
	}
	if c := got.Channels[1]; c.Key != "id:conv-secret" || !c.Locked || c.Name != "" || c.ConversationID != "" || c.MemberCount != 0 {
		t.Fatalf("unseen channel leaked %+v", c)
	}
	if len(got.People) != 2 || got.People[0].SubjectID != "hc-7" || got.People[1].Key != "hc-7" || got.People[1].DisplayName != "Ana Lopez" {
		t.Fatalf("people = %+v (ambiguous @sam.lee and unknown @nobody must not resolve)", got.People)
	}
	if len(got.Messages) != 4 {
		t.Fatalf("messages = %+v", got.Messages)
	}
	if m := got.Messages[0]; !m.Readable || m.Body != "We ship Friday." || m.AuthorName != "Ana Lopez" || m.ChannelName != "People Ops" || m.PostID != "post-1" {
		t.Fatalf("readable message = %+v", m)
	}
	for _, m := range got.Messages[1:] {
		if m.Readable || m.Body != "" || m.AuthorName != "" || m.ChannelName != "" {
			t.Fatalf("unreadable message leaked %+v", m)
		}
	}
}

func TestDocumentChatRefsWithoutChatOrPrincipal(t *testing.T) {
	md := "#people-ops [#x](channel:conv-2) /workspace/app/chat#share=tok-ok"
	// No chat composed: IDs lock, names drop, permalinks stay unreadable.
	got := documentService{}.documentChatReferences(documentReaderContext(t, "tt", "r"), "tt", "r", md)
	if len(got.Channels) != 1 || !got.Channels[0].Locked || len(got.Messages) != 1 || got.Messages[0].Readable {
		t.Fatalf("no chat = %+v", got)
	}
	// A context whose principal is not the reader never asks chat.
	chat := &fakeDocumentChat{rooms: []chatcore.Conversation{{ID: "conv-2", Kind: chatcore.PublicChannel, Name: "x"}}}
	got = documentService{chat: chat}.documentChatReferences(documentReaderContext(t, "tt", "someone-else"), "tt", "r", md)
	if chat.principal.SubjectID != "" || len(chat.resolved) != 0 || !got.Channels[0].Locked {
		t.Fatalf("mismatched principal = %+v, chat = %+v", got, chat)
	}
	// A failed listing locks rather than failing the read.
	chat = &fakeDocumentChat{listErr: errors.New("down")}
	got = documentService{chat: chat}.documentChatReferences(documentReaderContext(t, "tt", "r"), "tt", "r", md)
	if len(got.Channels) != 1 || !got.Channels[0].Locked {
		t.Fatalf("failed listing = %+v", got)
	}
	if got := (documentService{}).documentChatReferences(context.Background(), "t", "r", "plain text"); len(got.Channels)+len(got.People)+len(got.Messages) != 0 {
		t.Fatalf("plain = %+v", got)
	}
	if svc := withDocumentChat(documentService{}, &fakeDocumentChat{}); svc.(documentService).chat == nil {
		t.Fatal("chat not wired")
	}
	if svc := withDocumentChat(documentService{}, nil); svc.(documentService).chat != nil {
		t.Fatal("nil chat wired")
	}
}

func TestDocumentPeopleDirectoryNilPool(t *testing.T) {
	if documentPeopleDirectory(nil) != nil {
		t.Fatal("nil pool built a directory")
	}
}
