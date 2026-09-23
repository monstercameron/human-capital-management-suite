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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type conflictingSidebarClient struct {
	chatv1.ChatExtensionsServiceClient
	puts  int
	gets  int
	saved *chatv1.PutSidebarRequest
	done  chan struct{}
}

func (c *conflictingSidebarClient) PutSidebar(_ context.Context, request *chatv1.PutSidebarRequest, _ ...grpc.CallOption) (*chatv1.PutSidebarResponse, error) {
	c.puts++
	if c.puts == 1 {
		return nil, status.Error(codes.Aborted, "sidebar revision changed")
	}
	c.saved = request
	close(c.done)
	return &chatv1.PutSidebarResponse{Sidebar: &chatv1.SidebarState{Revision: 3}}, nil
}

func (c *conflictingSidebarClient) GetSidebar(context.Context, *chatv1.GetSidebarRequest, ...grpc.CallOption) (*chatv1.GetSidebarResponse, error) {
	c.gets++
	return &chatv1.GetSidebarResponse{Sidebar: &chatv1.SidebarState{Revision: 2, LayoutJson: `{"sections":[{"id":"channels","chats":[{"hostTenantId":"tenant","conversationId":"other"}]}],"drafts":{"other":"other tab draft"}}`}}, nil
}

func TestTodo_CHAT_035_BrowserConflictRebasesUnsentDraft(t *testing.T) {
	js.Global().Set("innerWidth", 1280)
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "alice", Bearer: "token"}
	chatBrowser.reset(nil, cfg, nil)
	chatBrowser.mutate(func(model *chatui.Model) {
		model.SelectedID = "local"
		model.Conversations = []chatui.Conversation{{ID: "local", Kind: chatui.PublicChannel}, {ID: "other", Kind: chatui.PublicChannel}}
		model.Sections = []chatui.SidebarSection{{ID: "channels", Name: "Channels", Chats: model.Conversations}}
	})
	chatBrowser.loadDrafts(map[string]string{"other": "stale draft"})
	chatBrowser.setDraft("local", "unsent in this tab")
	client := &conflictingSidebarClient{done: make(chan struct{})}
	chatRecipientBrowser.Lock()
	chatRecipientBrowser.generation++
	chatRecipientBrowser.client = client
	chatRecipientBrowser.sidebarRevision = 1
	chatRecipientBrowser.sidebarEdit = 0
	chatRecipientBrowser.sidebarSaved = 0
	chatRecipientBrowser.layout = recipientLayout{}
	chatRecipientBrowser.hosts = map[string]string{"local": "tenant", "other": "tenant"}
	chatRecipientBrowser.Unlock()
	persistChatRecipientSidebar(cfg, chatBrowser.snapshot(), true)
	select {
	case <-client.done:
	case <-time.After(5 * time.Second):
		t.Fatal("conflicted sidebar write did not retry")
	}
	chatRecipientBrowser.sidebarWrite.Lock()
	chatRecipientBrowser.sidebarWrite.Unlock()
	if client.puts != 2 || client.gets != 1 || client.saved.GetExpectedRevision() != 2 {
		t.Fatalf("conflict retry = %d puts, %d gets, revision %d", client.puts, client.gets, client.saved.GetExpectedRevision())
	}
	var saved recipientLayout
	if err := json.Unmarshal([]byte(client.saved.GetSidebar().GetLayoutJson()), &saved); err != nil || len(saved.Drafts) != 2 || saved.Drafts["local"] != "unsent in this tab" || saved.Drafts["other"] != "other tab draft" {
		t.Fatalf("retry lost a draft: %+v, %v", saved.Drafts, err)
	}
	if len(saved.Sections) != 1 || len(saved.Sections[0].Chats) != 2 || saved.Sections[0].Chats[1].ConversationID != "local" {
		t.Fatalf("retry omitted this tab's new room: %+v", saved.Sections)
	}
	if got := chatBrowser.draft("other"); got != "other tab draft" {
		t.Fatalf("stale local draft remained after rebase: %q", got)
	}
}
