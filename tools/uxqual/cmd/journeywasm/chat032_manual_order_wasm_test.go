//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"syscall/js"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc"
)

type manualOrderSidebarClient struct {
	chatv1.ChatExtensionsServiceClient
	requests chan *chatv1.PutSidebarRequest
}

func (c *manualOrderSidebarClient) PutSidebar(_ context.Context, request *chatv1.PutSidebarRequest, _ ...grpc.CallOption) (*chatv1.PutSidebarResponse, error) {
	c.requests <- request
	return &chatv1.PutSidebarResponse{Sidebar: &chatv1.SidebarState{Revision: request.GetExpectedRevision() + 1, LayoutJson: request.GetSidebar().GetLayoutJson()}}, nil
}

func TestTodo_CHAT_032_BrowserManualOrderPersists(t *testing.T) {
	js.Global().Set("innerWidth", 1280)
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "alice", Bearer: "token"}
	chatBrowser.reset(nil, cfg, nil)
	chatBrowser.mutate(func(model *chatui.Model) {
		model.Conversations = []chatui.Conversation{{ID: "first", Kind: chatui.PublicChannel}, {ID: "second", Kind: chatui.PublicChannel}}
		model.Sections = []chatui.SidebarSection{{ID: "channels", Name: "Channels", Chats: model.Conversations}}
	})
	client := &manualOrderSidebarClient{requests: make(chan *chatv1.PutSidebarRequest, 1)}
	chatRecipientBrowser.Lock()
	chatRecipientBrowser.generation++
	chatRecipientBrowser.client = client
	chatRecipientBrowser.hosts = map[string]string{"first": "tenant", "second": "tenant"}
	chatRecipientBrowser.sidebarRevision = 1
	chatRecipientBrowser.sidebarEdit = 0
	chatRecipientBrowser.sidebarSaved = 0
	chatRecipientBrowser.layout = recipientLayout{}
	chatRecipientBrowser.Unlock()
	t.Cleanup(func() {
		chatBrowser.reset(nil, journeyclient.Config{}, nil)
		chatRecipientBrowser.Lock()
		chatRecipientBrowser.generation++
		chatRecipientBrowser.client = nil
		chatRecipientBrowser.hosts = nil
		chatRecipientBrowser.Unlock()
	})

	callbacks := withChatRecipientCallbacks(chatui.Callbacks{}, cfg, func() {})
	callbacks.MoveConversationOrder("second", -1)
	var request *chatv1.PutSidebarRequest
	select {
	case request = <-client.requests:
	case <-time.After(5 * time.Second):
		t.Fatal("manual move did not save the personal sidebar")
	}
	chatRecipientBrowser.sidebarWrite.Lock()
	chatRecipientBrowser.sidebarWrite.Unlock()
	var saved recipientLayout
	if err := json.Unmarshal([]byte(request.GetSidebar().GetLayoutJson()), &saved); err != nil {
		t.Fatal(err)
	}
	if request.GetExpectedRevision() != 1 || len(saved.Sections) != 2 || len(saved.Sections[0].Chats) != 2 {
		t.Fatalf("saved revision/layout = %d / %+v", request.GetExpectedRevision(), saved.Sections)
	}
	if saved.Sections[0].Chats[0].ConversationID != "second" || saved.Sections[0].Chats[1].ConversationID != "first" {
		t.Fatalf("saved order = %+v", saved.Sections[0].Chats)
	}
}
