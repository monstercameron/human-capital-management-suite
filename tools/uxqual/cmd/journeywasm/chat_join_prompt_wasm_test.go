//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc"
)

type joinPromptHistoryClient struct {
	chatv1.ChatExtensionsServiceClient
	response *chatv1.GetSidebarResponse
	calls    int
}

func (c *joinPromptHistoryClient) GetSidebar(context.Context, *chatv1.GetSidebarRequest, ...grpc.CallOption) (*chatv1.GetSidebarResponse, error) {
	c.calls++
	return c.response, nil
}

func TestDismissedJoinPromptSurvivesSidebarReload(t *testing.T) {
	oldClient, oldLayout, oldLoaded := chatRecipientBrowser.client, chatRecipientBrowser.layout, chatRecipientBrowser.loadedAt
	oldRevision, oldGeneration := chatRecipientBrowser.sidebarRevision, chatRecipientBrowser.generation
	oldEdit, oldSaved := chatRecipientBrowser.sidebarEdit, chatRecipientBrowser.sidebarSaved
	client := &joinPromptHistoryClient{response: &chatv1.GetSidebarResponse{Sidebar: &chatv1.SidebarState{
		Revision: 5, LayoutJson: `{"dismissedJoinPrompts":["room-101"]}`,
	}}}
	chatRecipientBrowser.Lock()
	chatRecipientBrowser.client, chatRecipientBrowser.layout, chatRecipientBrowser.loadedAt = client, recipientLayout{}, time.Time{}
	chatRecipientBrowser.sidebarRevision, chatRecipientBrowser.generation = 1, oldGeneration+1
	chatRecipientBrowser.sidebarEdit, chatRecipientBrowser.sidebarSaved = 0, 0
	chatRecipientBrowser.Unlock()
	t.Cleanup(func() {
		chatRecipientBrowser.Lock()
		chatRecipientBrowser.client, chatRecipientBrowser.layout, chatRecipientBrowser.loadedAt = oldClient, oldLayout, oldLoaded
		chatRecipientBrowser.sidebarRevision, chatRecipientBrowser.generation = oldRevision, oldGeneration
		chatRecipientBrowser.sidebarEdit, chatRecipientBrowser.sidebarSaved = oldEdit, oldSaved
		chatRecipientBrowser.Unlock()
	})
	if !chatChannelJoinPromptDismissed(context.Background(), journeyclient.Config{Tenant: "tenant", Subject: "reader"}, "room-101") {
		t.Fatal("a declined channel prompt was not restored from the user's sidebar state")
	}
	if client.calls != 1 {
		t.Fatalf("sidebar read calls = %d, want one", client.calls)
	}
	var layout recipientLayout
	if err := json.Unmarshal([]byte(client.response.GetSidebar().GetLayoutJson()), &layout); err != nil || len(layout.DismissedJoinPrompts) != 1 || layout.DismissedJoinPrompts[0] != "room-101" {
		t.Fatalf("persisted prompt history = %+v, err=%v", layout.DismissedJoinPrompts, err)
	}
}
