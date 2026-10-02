package main

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func chatbug005Open(page chat.SavedPage) int {
	open := 0
	for _, item := range page.Items {
		if item.State == chat.SavedTodo {
			open++
		}
	}
	return open
}

// TestTodo_CHATBUG_005_Client: saving, marking done, reopening and unsaving
// change the open count and the list together before the service answers, and
// a row never shows an identifier for its author or its channel.
func TestTodo_CHATBUG_005_Client(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "host", Subject: "walt", Locale: "en-US"}
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	model := chatui.Model{
		SelectedID:    "room",
		Conversations: []chatui.Conversation{{ID: "room", Name: "random"}},
		Messages:      []chatui.Message{{ID: "p1", AuthorID: "danny", Author: "Danny Nguyen", Body: "first line\nsecond", Sequence: 3, SentAt: now}, {ID: "p2", AuthorID: "ben", Author: "Ben Whitaker", Body: "other", Sequence: 4}},
	}
	command := func(action, post string) chatsaveCommand {
		return chatsaveCommand{Action: action, ConversationID: "room", PostID: post}
	}
	page := chat.SavedPage{Items: []chat.SavedItem{}}
	for _, post := range []string{"p1", "p2"} {
		add := chatsaveOptimisticItem(cfg, "host", command("save", post), model, now)
		if add == nil || add.Post == nil || add.Post.AuthorID == "" || add.Channel != "random" {
			t.Fatalf("optimistic item for %s = %+v", post, add)
		}
		page = chatsaveApplyLocal(page, "host", command("save", post), add)
	}
	if chatbug005Open(page) != 2 || len(page.Items) != 2 {
		t.Fatalf("after two saves: %+v", page.Items)
	}
	page = chatsaveApplyLocal(page, "host", command("save", "p1"), chatsaveOptimisticItem(cfg, "host", command("save", "p1"), model, now))
	if len(page.Items) != 2 {
		t.Fatal("saving a saved message twice added a row")
	}
	page = chatsaveApplyLocal(page, "host", command("done", "p1"), nil)
	if chatbug005Open(page) != 1 || len(page.Items) != 2 {
		t.Fatalf("after done: open=%d rows=%d", chatbug005Open(page), len(page.Items))
	}
	page = chatsaveApplyLocal(page, "host", command("reopen", "p1"), nil)
	if chatbug005Open(page) != 2 {
		t.Fatal("reopen did not raise the count")
	}
	page = chatsaveApplyLocal(page, "host", command("remove", "p2"), nil)
	if chatbug005Open(page) != 1 || len(page.Items) != 1 || page.Items[0].PostID != "p1" {
		t.Fatalf("after unsave: %+v", page.Items)
	}
	if other := chatsaveApplyLocal(page, "other-host", command("remove", "p1"), nil); len(other.Items) != 1 {
		t.Fatal("a command for another host removed this row")
	}
	if chatsaveOptimisticItem(cfg, "host", command("save", "off-screen"), model, now) != nil {
		t.Fatal("a message that is not on screen was invented")
	}
	rows := chatsaveRows(page, cfg, model)
	if len(rows) != 1 || rows[0].Author != "Danny Nguyen" || rows[0].Channel != "random" || rows[0].Body != "first line\nsecond" {
		t.Fatalf("row does not match the list: %+v", rows)
	}
}

func TestTodo_CHATBUG_005_NoIdentifiers(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "host", Subject: "walt", Locale: "en-US"}
	copy := chatui.SavedMessagesCopy("en-US")
	for _, tc := range []struct {
		name, conversation, channel, author string
	}{
		{"uuid channel name", "7f3c2d1e-9a4b-4c6d-8e2f-1a2b3c4d5e6f", "7f3c2d1e-9a4b-4c6d-8e2f-1a2b3c4d5e6f", "01a2b3c4-d5e6-4f70-8192-a3b4c5d6e7f8"},
		{"internal key", "dm", "hcmnext.chat.dm.7f3c2d1e9a4b4c6d", "hcmnext.person.12"},
		{"channel named by its id", "general-1", "general-1", "general-1"},
	} {
		item := chat.SavedItem{TenantID: "host", HomeTenantID: "host", PersonID: "walt", ConversationID: tc.conversation, PostID: "p", Availability: "readable", Channel: tc.channel, Post: &chat.Post{AuthorID: tc.author, AuthorHomeTenantID: "host", Body: "hello", CreatedAt: time.Now()}}
		rows := chatsaveRows(chat.SavedPage{Items: []chat.SavedItem{item}}, cfg, chatui.Model{})
		if len(rows) != 1 {
			t.Fatalf("%s: %d rows", tc.name, len(rows))
		}
		if rows[0].Author != copy.AuthorUnavailable {
			t.Fatalf("%s: author %q is not the unavailable copy", tc.name, rows[0].Author)
		}
		if rows[0].Channel != "" && (rows[0].Channel == tc.channel || strings.Contains(rows[0].Channel, "hcmnext.") || chatsaveLooksLikeIdentifier(rows[0].Channel)) {
			t.Fatalf("%s: channel %q is an identifier", tc.name, rows[0].Channel)
		}
	}
	// The name the person sees in the sidebar wins over the service's own.
	item := chat.SavedItem{ConversationID: "dm-1", Channel: "dm-1", Availability: "readable"}
	if got := chatsaveChannelLabel(item, chatui.Model{Conversations: []chatui.Conversation{{ID: "dm-1", Name: "Loretta Haynes"}}}); got != "Loretta Haynes" {
		t.Fatalf("channel label %q", got)
	}
	if chatsaveLooksLikeIdentifier("Q4 hiring huddle") || chatsaveLooksLikeIdentifier("Danny Nguyen") || chatsaveLooksLikeIdentifier("") {
		t.Fatal("a plain name was taken for an identifier")
	}
}
