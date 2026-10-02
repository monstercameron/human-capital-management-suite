package main

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestTodo_CHATSTATE_002(t *testing.T) {
	view := chatui.ChannelStatusView{Status: chat.ChannelStatus{TenantID: "tenant", ConversationID: "room", Revision: 1}, CanPost: true}
	payload, _ := json.Marshal(chat.ChannelStatus{TenantID: "tenant", ConversationID: "room", Status: chatpolicy.StatusLocked, Revision: 2})
	updated, err := applyChatstateEvent(view, "tenant", "room", payload, time.Now())
	if err != nil || updated.Status.Status != chatpolicy.StatusLocked || !updated.Updated || updated.CanPost {
		t.Fatalf("update = %+v %v", updated, err)
	}
	if _, err = applyChatstateEvent(view, "other", "room", payload, time.Now()); !errors.Is(err, errChatstateEvent) {
		t.Fatalf("tenant leak = %v", err)
	}
	old, err := applyChatstateEvent(updated, "tenant", "room", payload, time.Now())
	if err != nil || old.Status.Revision != 2 {
		t.Fatal("stale event changed projection")
	}
	if _, err = applyChatstateEvent(view, "tenant", "room", []byte(`{"status":"forged"}`), time.Now()); !errors.Is(err, errChatstateEvent) {
		t.Fatal("forged event allowed")
	}
}
