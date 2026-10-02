package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestChatPolish_E_PinKeyboardActions(t *testing.T) {
	for key, want := range map[string]string{"ArrowUp": "north", "ArrowDown": "south", "ArrowLeft": "west", "ArrowRight": "east", "Tab": "", "Escape": "", "Enter": ""} {
		if got := chatPolishPinAction(key); got != want {
			t.Fatalf("%s moved pin %s; want %s", key, got, want)
		}
	}
}

func TestChatPolish_F_SavedIdentifiersStayPrivate(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		cfg := journeyclient.Config{Tenant: "home", Subject: "viewer", Locale: locale}
		id := "cd9c112d-f94f-4727-a015-e95e44967aab"
		item := chat.SavedItem{TenantID: "home", HomeTenantID: "home", PersonID: "viewer", ConversationID: id, Channel: id, PostID: "post", Availability: "readable", Post: &chat.Post{AuthorID: id, AuthorHomeTenantID: "home", Body: "Words"}}
		model := chatui.Model{SelectedID: id, Messages: []chatui.Message{{ID: "post", Author: id}}}
		rows := chatsaveRows(chat.SavedPage{Items: []chat.SavedItem{item}}, cfg, model)
		if len(rows) != 1 || rows[0].Author == id || rows[0].Channel == id || rows[0].Body != "Words" {
			t.Fatalf("%s exposed an identifier or lost the readable post: %+v", locale, rows)
		}
	}
}

func TestChatPolish_A_LayersMeasureAfterOpeningAndRestoreFocus(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "internal", "humanwork", "chatui", "agentux_chat5_layers_js.go")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "func positionChatLayer(")
	end := strings.Index(source[start:], "func syncChatAnchoredLayers(")
	body := source[start : start+end]
	if strings.Index(body, `Call("showPopover")`) > strings.Index(body, `Get("scrollHeight")`) {
		t.Fatal("measured a closed zero-height popover")
	}
	for _, required := range []string{"currentChatLayerOpener(root", `root.Call("addEventListener", "click", capture, true)`, `button.Call("focus")`, "chatPolishManagedLayer(kind)"} {
		if !strings.Contains(source, required) {
			t.Fatalf("missing opener/Escape/focus binding %q", required)
		}
	}
}

func TestChatPolish_N_ReconciliationKeepsSendInTabOrder(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "internal", "humanwork", "chatui", "fieldsync_js.go")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `setComposerSendReady(id, found.Index(i).Get("value").String())`) {
		t.Fatal("Send readiness is no longer restored from the live draft after reconciliation")
	}
}

func TestChatPolish_H_SearchOutcomeKeepsPriorContext(t *testing.T) {
	previous := chatui.ChatSearchView{Response: chatsearch.Response{Groups: []chatsearch.Group{{Kind: chatsearch.Message, Rows: []chatsearch.Row{{Text: "Previous result"}}}}}, Recent: []string{"previous"}}
	conversation := chatui.Model{Messages: []chatui.Message{{Body: "Current conversation"}}}
	failed := chatPolishSearchOutcome(previous, conversation, "new", chatsearch.Response{}, nil, "unavailable")
	if failed.Response.Groups[0].Rows[0].Text != "Previous result" || failed.Conversation.Messages[0].Body != "Current conversation" || failed.Query != "new" || failed.Error != "unavailable" {
		t.Fatal("search erased prior context", failed)
	}
	success := chatPolishSearchOutcome(previous, conversation, "new", chatsearch.Response{}, nil, "")
	if len(success.Response.Groups) != 0 || success.Conversation != nil || success.Error != "" {
		t.Fatal("successful empty search kept stale results", success)
	}
}
