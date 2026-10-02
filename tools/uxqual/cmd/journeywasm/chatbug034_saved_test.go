//go:build !(js && wasm)

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

const chatbug034Doc = "doc-47892b80-d600-4401-8244-e2fa2a31caa7"

func chatbug034Page(cfg journeyclient.Config) chat.SavedPage {
	post := chat.Post{ID: "post", TenantID: cfg.Tenant, ConversationID: "random", AuthorID: cfg.Subject, Body: "Open enrollment runs November 2 to 20. doc:" + chatbug034Doc, Sequence: 7, CreatedAt: time.Now()}
	return chat.SavedPage{Items: []chat.SavedItem{
		{TenantID: cfg.Tenant, HomeTenantID: cfg.Tenant, PersonID: cfg.Subject, ConversationID: "random", PostID: "post", State: chat.SavedTodo, Availability: "readable", Post: &post, Channel: "random"},
		{TenantID: cfg.Tenant, HomeTenantID: cfg.Tenant, PersonID: cfg.Subject, ConversationID: "random", PostID: "gone", State: chat.SavedTodo, Availability: "removed"},
	}}
}

// TestTodo_CHATBUG_034 covers the client side of the Saved list: a press on a
// message's bookmark saves or unsaves according to the list, not the button's
// pressed state (so a press on a message the list holds removes it, even when the
// button was redrawn unpressed); a refused change is reported as that change and
// never as the list failing to load; and the documents a saved message refers to
// are read so the panel can show titles.
func TestTodo_CHATBUG_034(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "walt"}
	page := chatbug034Page(cfg)
	if got := chatsaveHoverCommand(page, cfg, "tenant", "random", "post"); got.Action != "remove" || got.ConversationID != "random" || got.PostID != "post" {
		t.Fatalf("a press on a saved message must remove it: %+v", got)
	}
	if got := chatsaveHoverCommand(page, cfg, "tenant", "random", "another"); got.Action != "save" {
		t.Fatalf("a press on a message that is not saved must save it: %+v", got)
	}
	// Another person's save, another host's and another conversation's are not this save.
	for name, other := range map[string]struct {
		cfg        journeyclient.Config
		host, room string
	}{"person": {journeyclient.Config{Tenant: "tenant", Subject: "dana"}, "tenant", "random"}, "host": {cfg, "other-host", "random"}, "room": {cfg, "tenant", "elsewhere"}} {
		if chatsaveIsSaved(page, other.cfg, other.host, other.room, "post") {
			t.Fatalf("a save of %s was taken as this one", name)
		}
	}
	// Save, unsave, save again, as the list changes under the presses.
	cmd := chatsaveHoverCommand(chat.SavedPage{}, cfg, "tenant", "random", "post")
	if cmd.Action != "save" {
		t.Fatalf("first press: %+v", cmd)
	}
	listed := chatsaveApplyLocal(chat.SavedPage{}, "tenant", cmd, &page.Items[0])
	cmd = chatsaveHoverCommand(listed, cfg, "tenant", "random", "post")
	listed = chatsaveApplyLocal(listed, "tenant", cmd, nil)
	if cmd.Action != "remove" || len(listed.Items) != 0 {
		t.Fatalf("second press: %+v %+v", cmd, listed)
	}
	if cmd = chatsaveHoverCommand(listed, cfg, "tenant", "random", "post"); cmd.Action != "save" {
		t.Fatalf("third press: %+v", cmd)
	}

	// A refused change is the change, not the list.
	for _, tc := range []struct {
		code      string
		failed    bool
		wantList  string
		wantEvent string
	}{
		{"unavailable", true, "", "unavailable"},
		{"request_denied", true, "", "request_denied"},
		{"permission_denied", true, "", "permission_denied"},
		{"unavailable", false, "unavailable", ""},
		{"saved_limit", true, "saved_limit", ""},
		{"invalid_argument", true, "invalid_argument", ""},
		{"", false, "", ""},
	} {
		list, action := chatsaveActionFailure(tc.code, tc.failed)
		if list != tc.wantList || action != tc.wantEvent {
			t.Fatalf("%+v: list=%q action=%q", tc, list, action)
		}
	}

	// The panel needs the documents its messages name.
	bodies := chatsaveBodies(page)
	if len(bodies) != 1 || !strings.Contains(bodies[0], chatbug034Doc) {
		t.Fatalf("the readable message's text is not offered for document previews: %v", bodies)
	}
	if ids := chatDocPreviewIDs(chatui.Model{SavedBodies: bodies}); len(ids) != 1 || ids[0] != chatbug034Doc {
		t.Fatalf("the saved message's document is not read: %v", ids)
	}
}

// TestTodo_CHATBUG_034_Browser renders the Saved panel the way the client does for
// a refused unsave and for a message that names a document: the list that loaded
// stays, the failure names the unsave, and the document is its title as a link.
func TestTodo_CHATBUG_034_Browser(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "walt", Locale: "en-US"}
	model := chatui.Model{Locale: "en-US", CurrentUser: "walt", CurrentTenantID: "tenant", SelectedID: "random", Conversations: []chatui.Conversation{{ID: "random", Name: "random", Kind: chatui.PublicChannel}}}
	rows := chatsaveRows(chatbug034Page(cfg), cfg, model)
	listError, actionError := chatsaveActionFailure("unavailable", true)
	view := chatui.SavedMessagesView{Locale: "en-US", Tab: "all", Rows: rows, Error: listError, ActionError: actionError, Action: "remove",
		DocPreviews: map[string]chatui.DocPreview{chatbug034Doc: {ID: chatbug034Doc, Title: "Open enrollment guide", Readable: true, State: "ready"}}}
	markup, err := ui.RenderToString(chatui.RenderSavedMessages(view))
	if err != nil {
		t.Fatal(err)
	}
	copy := chatui.SavedMessagesCopy("en-US")
	if !strings.Contains(markup, chatui.SavedActionFailedText("en-US", "remove")) || strings.Contains(markup, copy.Failed) || strings.Contains(markup, `data-saved-action="retry"`) {
		t.Fatalf("a refused unsave was reported as the list failing: %s", markup)
	}
	if !strings.Contains(markup, "Open enrollment guide") || !strings.Contains(markup, `href="/workspace/app/docs?document=`+chatbug034Doc+`"`) || strings.Contains(markup, ">doc:") || strings.Contains(markup, "doc:"+chatbug034Doc+"<") {
		t.Fatalf("the document reference is not its title as a link: %s", markup)
	}
}
