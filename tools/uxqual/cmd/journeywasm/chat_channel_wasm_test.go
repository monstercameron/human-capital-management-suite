//go:build js && wasm

package main

import (
	"context"
	"errors"
	"syscall/js"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc"
)

type channelLinkClient struct {
	chatv1.ConversationServiceClient
	get   chan string
	allow bool
}

func (c *channelLinkClient) GetConversation(_ context.Context, request *chatv1.GetConversationRequest, _ ...grpc.CallOption) (*chatv1.GetConversationResponse, error) {
	c.get <- request.GetConversationId()
	if !c.allow {
		return nil, errors.New("permission denied")
	}
	return &chatv1.GetConversationResponse{Conversation: &chatv1.Conversation{Id: request.GetConversationId(), TenantId: request.GetTenantId(), Kind: chatv1.ConversationKind_CONVERSATION_KIND_PUBLIC_CHANNEL, Name: "Authorized"}}, nil
}
func (*channelLinkClient) ListPosts(context.Context, *chatv1.ListPostsRequest, ...grpc.CallOption) (*chatv1.ListPostsResponse, error) {
	return &chatv1.ListPostsResponse{}, nil
}
func (*channelLinkClient) ListPins(context.Context, *chatv1.ListPinsRequest, ...grpc.CallOption) (*chatv1.ListPinsResponse, error) {
	return &chatv1.ListPinsResponse{}, nil
}
func (*channelLinkClient) WatchConversation(context.Context, *chatv1.WatchConversationRequest, ...grpc.CallOption) (chatv1.ConversationService_WatchConversationClient, error) {
	return nil, errors.New("test stream unavailable")
}

func TestChatChannelFragmentOpensAuthorizedRoomBeyondFirstListPage(t *testing.T) {
	oldModel, oldClient := chatBrowser.snapshot(), chatBrowser.conversationClient()
	oldCfg := chatBrowser.config(journeyclient.Config{})
	oldLocation := js.Global().Get("location")
	client := &channelLinkClient{get: make(chan string, 1), allow: true}
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "reader", Locale: "en-US"}
	chatBrowser.reset(client, cfg, nil)
	js.Global().Set("location", js.ValueOf(map[string]any{"origin": "https://hcm.example", "hash": "#channel=room-101"}))
	resetChatChannelFragment()
	t.Cleanup(func() {
		cancelChatSubscription()
		resetChatChannelFragment()
		js.Global().Set("location", oldLocation)
		chatBrowser.reset(oldClient, oldCfg, nil)
		chatBrowser.mutate(func(model *chatui.Model) { *model = oldModel })
	})
	openChatChannelFragment(cfg)
	select {
	case id := <-client.get:
		if id != "room-101" {
			t.Fatalf("authorized lookup requested %q", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("new-tab channel fragment did not read beyond first list page")
	}
	deadline := time.After(2 * time.Second)
	for chatBrowser.selectedID() != "room-101" {
		select {
		case <-deadline:
			t.Fatal("authorized channel link did not select room")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if len(chatBrowser.snapshot().Conversations) != 1 {
		t.Fatal("authorized room was not added to rail")
	}
}

func TestChatChannelFragmentDeniedLookupDoesNotSelectOrNameRoom(t *testing.T) {
	oldModel, oldClient := chatBrowser.snapshot(), chatBrowser.conversationClient()
	oldCfg := chatBrowser.config(journeyclient.Config{})
	oldLocation := js.Global().Get("location")
	client := &channelLinkClient{get: make(chan string, 1)}
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "reader", Locale: "en-US"}
	chatBrowser.reset(client, cfg, nil)
	js.Global().Set("location", js.ValueOf(map[string]any{"origin": "https://hcm.example", "hash": "#channel=private-room"}))
	resetChatChannelFragment()
	t.Cleanup(func() {
		cancelChatSubscription()
		resetChatChannelFragment()
		js.Global().Set("location", oldLocation)
		chatBrowser.reset(oldClient, oldCfg, nil)
		chatBrowser.mutate(func(model *chatui.Model) { *model = oldModel })
	})
	openChatChannelFragment(cfg)
	select {
	case <-client.get:
	case <-time.After(2 * time.Second):
		t.Fatal("missing authorized lookup")
	}
	time.Sleep(10 * time.Millisecond)
	if model := chatBrowser.snapshot(); model.SelectedID != "" || len(model.Conversations) != 0 {
		t.Fatalf("denied link changed chat projection: %+v", model)
	}
}
