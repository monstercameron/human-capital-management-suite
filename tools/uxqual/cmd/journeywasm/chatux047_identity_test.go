package main

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// chatux047List is the answer a person's conversation list gets from a server
// that names the people and agents it carries: #general, a direct conversation
// with an agent and one with a person, and the timeline of #general with a
// person's message and an agent's stored announcement in it.
func chatux047List() (*chatv1.ListConversationsResponse, []*chatv1.Post) {
	icon := &chatv1.AgentIcon{Glyph: "calendar", Shape: "circle", Foreground: "--hcm-color-info", Background: "--hcm-color-info-surface"}
	list := &chatv1.ListConversationsResponse{Conversations: []*chatv1.Conversation{
		{Id: "general", Kind: chatv1.ConversationKind_CONVERSATION_KIND_PUBLIC_CHANNEL, Name: "general", Joined: true},
		// The agent conversation's stored name is the conversation id until a
		// directory names it: the answer's peer is what names it.
		{Id: "dm-assistant", Kind: chatv1.ConversationKind_CONVERSATION_KIND_DIRECT, Name: "dm-assistant", Joined: true,
			Peer: &chatv1.Identity{SubjectId: "assistant-agent", DisplayName: "Assistant", Agent: true, AgentId: "assistant-agent", AgentIcon: icon, AgentIconRevision: 2}},
		{Id: "dm-loretta", Kind: chatv1.ConversationKind_CONVERSATION_KIND_DIRECT, Name: "dm-loretta", Joined: true,
			Peer: &chatv1.Identity{SubjectId: "loretta", DisplayName: "Loretta Haynes", AvatarRef: "/photo/loretta.png"}},
	}}
	stored, _ := json.Marshal(chatui.AgentAnnouncementMessage{AgentName: "Agent", OwnerName: "Walt Brennan", Text: "Upcoming company holidays remaining in 2026:\n- Labor Day"})
	when := timestamppb.New(time.Date(2026, 10, 1, 20, 30, 0, 0, time.UTC))
	posts := []*chatv1.Post{
		{Id: "p1", ConversationId: "general", AuthorId: "luis", Body: "Hello team", Sequence: 1, Revision: 1, CreatedAt: when,
			Author: &chatv1.Identity{SubjectId: "luis", DisplayName: "Luis Ortega", AvatarRef: "/photo/luis.png"}},
		{Id: "p2", ConversationId: "general", AuthorId: "hcmnext.local.persona.assistant", Body: chatui.AgentAnnouncementBodyPrefix + string(stored), Sequence: 2, Revision: 1, CreatedAt: when,
			Author: &chatv1.Identity{SubjectId: "hcmnext.local.persona.assistant", DisplayName: "Assistant", Agent: true, AgentId: "assistant-agent", AgentIcon: icon}},
	}
	return list, posts
}

// chatux047FirstFrame is the model the page holds the moment the list answer has
// been adopted: the conversation rows from the answer, the answer's peers and
// photographs, the answer's timeline, and nothing from any directory (the
// worker directory has not answered, the agent directory has not been read).
func chatux047FirstFrame(locale string, list *chatv1.ListConversationsResponse, posts []*chatv1.Post) chatui.Model {
	ctx := productui.ResolveProductLocale(locale)
	rows := list.GetConversations()
	conversations := make([]chatui.Conversation, 0, len(rows))
	for _, row := range rows {
		room := chatux047Peer(chatBrowseConversation(row), row)
		room = chatDirectoryNamedDirect(room, chatux047PeerIDs(nil, rows), map[string]string{})
		conversations = append(conversations, room)
	}
	return chatui.Model{
		State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), Text: func(key string) string { return ctx.Text(key) },
		CurrentUser: "walt", CurrentTenantID: "tenant", SelectedID: "general",
		Conversations: conversations, PeerIDs: chatux047PeerIDs(nil, rows), PhotoURLs: chatux047PeerPhotos(rows),
		Messages:      chatMessages(posts, ctx.Resolved, map[string]string{}, nil, time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)),
		PersonaLookup: chatui.PersonaLookupIdle,
	}
}

var chatux047Strong = regexp.MustCompile(`(?s)class="message-author[^"]*"[^>]*>(.*?)</(?:strong|button)>`)

// TestTodo_CHATUX_047: the first frame after the list answer, with the agent
// directory and the people directory still pending, already names every message
// author and every direct-conversation row, in en-US, de-DE and ar. No author
// line reads "...", no row or header is called "Conversation", and the empty
// avatar tile is not drawn for an agent whose icon the answer carried.
func TestTodo_CHATUX_047(t *testing.T) {
	list, posts := chatux047List()
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		model := chatux047FirstFrame(locale, list, posts)
		if model.PersonaLookup == chatui.PersonaLookupReady || len(model.ResolvedPersonaMentions) != 0 || len(model.SearchDirectory) != 0 {
			t.Fatalf("%s: the fixture is not the directory-pending frame", locale)
		}
		page, err := ui.RenderToString(chatui.Build(model))
		if err != nil {
			t.Fatalf("%s: %v", locale, err)
		}
		for _, name := range []string{"Luis Ortega", "Assistant", "Loretta Haynes"} {
			if !strings.Contains(page, ">"+name+"<") {
				t.Errorf("%s: the first frame does not show %q", locale, name)
			}
		}
		authors := []string{}
		for _, match := range chatux047Strong.FindAllStringSubmatch(page, -1) {
			authors = append(authors, strings.TrimSpace(match[1]))
		}
		if len(authors) < 2 || authors[0] != "Luis Ortega" || authors[1] != "Assistant" {
			t.Errorf("%s: author lines are %q, want Luis Ortega then Assistant", locale, authors)
		}
		for _, bad := range []string{">…<", ">...<", ">Conversation<", "chatux037-bar chatux047-name"} {
			if strings.Contains(page, bad) {
				t.Errorf("%s: the first frame has %q", locale, bad)
			}
		}
		// The agent's icon came with the answer: no empty tile where it goes.
		if strings.Contains(page, "agent-icon-pending") || !strings.Contains(page, `class="agent-icon"`) {
			t.Errorf("%s: the agent's avatar is an empty tile although the answer carried its icon", locale)
		}
		// The person's photograph is on the message and on the row from the answer.
		for _, photo := range []string{"/photo/luis.png", "/photo/loretta.png"} {
			if !strings.Contains(page, photo) {
				t.Errorf("%s: the first frame does not draw %s", locale, photo)
			}
		}
		// The agent badge is still the announcement path's own: one on the row's
		// author line and one on the sidebar row.
		if strings.Count(page, "agent-badge") < 2 {
			t.Errorf("%s: the Agent badge went missing", locale)
		}
	}
}

// A name the answer could not give is the shared loading bar, never "..." and
// never the word "Conversation".
func TestTodo_CHATUX_047_UnknownNameIsTheBar(t *testing.T) {
	list, posts := chatux047List()
	for _, row := range list.GetConversations() {
		row.Peer = nil
	}
	for _, post := range posts {
		post.Author = nil
	}
	model := chatux047FirstFrame("en-US", list, posts)
	model.SelectedID = "dm-assistant"
	page, err := ui.RenderToString(chatui.Build(model))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{">…<", ">...<", ">Conversation<"} {
		if strings.Contains(page, bad) {
			t.Errorf("an unnamed frame has %q", bad)
		}
	}
	if !strings.Contains(page, "chatux037-bar chatux047-name") {
		t.Errorf("an unnamed frame has no loading bar")
	}
}

// A directory that knows the person names them the same way the answer did, and
// the answer's name is used when it does not: the directory only refines.
func TestTodo_CHATUX_047_DirectoryOnlyRefines(t *testing.T) {
	_, posts := chatux047List()
	named := chatMessage(posts[0], "en-US", map[string]string{}, time.Now())
	if named.Author != "Luis Ortega" || named.Avatar != "/photo/luis.png" || named.AuthorAgent != nil {
		t.Fatalf("answer-named person: %+v", named)
	}
	refined := chatMessage(posts[0], "en-US", map[string]string{"luis": "Luis A. Ortega"}, time.Now())
	if refined.Author != "Luis A. Ortega" {
		t.Fatalf("the directory did not refine the name: %q", refined.Author)
	}
	agent := chatMessage(posts[1], "en-US", map[string]string{}, time.Now())
	if agent.Author != "Assistant" || agent.AuthorAgent == nil || agent.AuthorAgent.AgentID != "assistant-agent" || !agent.AuthorAgent.Icon.Valid() {
		t.Fatalf("answer-named agent: %+v", agent)
	}
	bare := chatMessage(&chatv1.Post{Id: "p", AuthorId: "hc-050-rafael-torres"}, "en-US", map[string]string{}, time.Now())
	if bare.Author != "Rafael Torres" || bare.AuthorAgent != nil {
		t.Fatalf("an answer that names no one keeps the humanized reference: %+v", bare)
	}
}

// When the list names every direct peer it does not wait for the agent list.
func TestTodo_CHATUX_047_ListDoesNotWaitWhenNamed(t *testing.T) {
	list, _ := chatux047List()
	if !chatux047AllDirectNamed(list.GetConversations()) {
		t.Fatal("a list that names every direct peer must not wait for the agent list")
	}
	list.GetConversations()[2].Peer = nil
	if chatux047AllDirectNamed(list.GetConversations()) {
		t.Fatal("a direct conversation without a named peer must keep waiting")
	}
}
