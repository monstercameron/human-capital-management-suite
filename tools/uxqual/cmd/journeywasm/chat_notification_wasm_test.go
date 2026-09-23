//go:build js && wasm

package main

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

type notificationMenuClient struct {
	chatv1.ConversationServiceClient
	get, update chan string
	request     *chatv1.UpdatePreferencesRequest
}

func (f *notificationMenuClient) GetPreferences(_ context.Context, request *chatv1.GetPreferencesRequest, _ ...grpc.CallOption) (*chatv1.GetPreferencesResponse, error) {
	f.get <- request.GetConversationId()
	return &chatv1.GetPreferencesResponse{Preferences: &chatv1.NotificationPreferences{ConversationId: request.GetConversationId(), TenantId: request.GetTenantId(), Revision: 7, HomeTenantId: "home"}}, nil
}

func (f *notificationMenuClient) UpdatePreferences(_ context.Context, request *chatv1.UpdatePreferencesRequest, _ ...grpc.CallOption) (*chatv1.UpdatePreferencesResponse, error) {
	f.request = request
	f.update <- request.GetPreferences().GetConversationId()
	return &chatv1.UpdatePreferencesResponse{Preferences: request.GetPreferences()}, nil
}

func TestRailNotificationSaveUsesTargetDespiteSelectionChange(t *testing.T) {
	old := chatBrowser.snapshot()
	oldClient := chatBrowser.conversationClient()
	oldRerender := chatRerender
	defer func() {
		chatBrowser.mu.Lock()
		chatBrowser.client = oldClient
		chatBrowser.model = old
		chatBrowser.mu.Unlock()
		chatRerender = oldRerender
	}()
	client := &notificationMenuClient{get: make(chan string, 1), update: make(chan string, 1)}
	chatBrowser.mu.Lock()
	chatBrowser.client = client
	chatBrowser.model = chatui.Model{SelectedID: "selected", RailMenuID: "target", Conversations: []chatui.Conversation{{ID: "selected"}, {ID: "target"}}}
	chatBrowser.mu.Unlock()
	chatRerender = func() {}
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "reader", Bearer: "token"}
	saveChatNotificationMode(cfg, "target", chatui.NotifyMute)
	select {
	case id := <-client.update:
		if id != "target" {
			t.Fatalf("update id = %q", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("notification update did not complete")
	}
	if id := <-client.get; id != "target" {
		t.Fatalf("get id = %q", id)
	}
	request := client.request
	if request.GetExpectedRevision() != 7 || !request.GetPreferences().GetMuted() || request.GetPreferences().GetMentionsOnly() || request.GetPreferences().GetHomeTenantId() != "home" {
		t.Fatalf("incorrect update: %+v", request)
	}
	deadline := time.After(2 * time.Second)
	for {
		model := chatBrowser.snapshot()
		if model.Preferences.Notifications["target"] == chatui.NotifyMute {
			if model.SelectedID != "selected" || !model.Conversations[1].Muted {
				t.Fatalf("wrong row state: %+v", model)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("target row was not updated")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}
