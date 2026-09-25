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
	get     chan string
	add     chan *chatv1.AddMembershipRequest
	release chan struct{}
	allow   bool
}

func (c *channelLinkClient) GetConversation(_ context.Context, request *chatv1.GetConversationRequest, _ ...grpc.CallOption) (*chatv1.GetConversationResponse, error) {
	c.get <- request.GetConversationId()
	if c.release != nil {
		<-c.release
	}
	if !c.allow {
		return nil, errors.New("permission denied")
	}
	return &chatv1.GetConversationResponse{Conversation: &chatv1.Conversation{Id: request.GetConversationId(), TenantId: request.GetTenantId(), Kind: chatv1.ConversationKind_CONVERSATION_KIND_PUBLIC_CHANNEL, Name: "Authorized"}}, nil
}
func (c *channelLinkClient) ListConversations(_ context.Context, request *chatv1.ListConversationsRequest, _ ...grpc.CallOption) (*chatv1.ListConversationsResponse, error) {
	return &chatv1.ListConversationsResponse{Conversations: []*chatv1.Conversation{{Id: "room-101", TenantId: request.GetTenantId(), Kind: chatv1.ConversationKind_CONVERSATION_KIND_PUBLIC_CHANNEL, Name: "Authorized", Joined: false}}}, nil
}
func (c *channelLinkClient) AddMembership(_ context.Context, request *chatv1.AddMembershipRequest, _ ...grpc.CallOption) (*chatv1.AddMembershipResponse, error) {
	if c.add != nil {
		c.add <- request
	}
	return &chatv1.AddMembershipResponse{Membership: request.GetMembership()}, nil
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

func TestUnjoinedPublicChannelLinkAsksBeforeAddingRoomToRail(t *testing.T) {
	oldModel, oldClient := chatBrowser.snapshot(), chatBrowser.conversationClient()
	oldCfg := chatBrowser.config(journeyclient.Config{})
	chatRecipientBrowser.Lock()
	oldSidebarClient, oldSidebarLayout, oldSidebarLoadedAt := chatRecipientBrowser.client, chatRecipientBrowser.layout, chatRecipientBrowser.loadedAt
	chatRecipientBrowser.client, chatRecipientBrowser.layout, chatRecipientBrowser.loadedAt = nil, recipientLayout{}, time.Time{}
	chatRecipientBrowser.Unlock()
	oldLocation := js.Global().Get("location")
	client := &channelLinkClient{get: make(chan string, 1), add: make(chan *chatv1.AddMembershipRequest, 1), allow: true}
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
		chatRecipientBrowser.Lock()
		chatRecipientBrowser.client, chatRecipientBrowser.layout, chatRecipientBrowser.loadedAt = oldSidebarClient, oldSidebarLayout, oldSidebarLoadedAt
		chatRecipientBrowser.Unlock()
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
	model := chatBrowser.snapshot()
	if len(model.Conversations) != 0 {
		t.Fatalf("unjoined public channel appeared in rail before consent: %+v", model.Conversations)
	}
	if model.JoinPromptID != "room-101" || model.PreviewConversation == nil || model.PreviewConversation.Name != "Authorized" {
		t.Fatalf("channel preview did not ask to join: %+v", model)
	}
	select {
	case request := <-client.add:
		t.Fatalf("channel joined before the reader accepted the prompt: %+v", request)
	default:
	}
	joinChatConversation(cfg, "room-101")
	select {
	case request := <-client.add:
		if request.GetMembership().GetConversationId() != "room-101" || request.GetMembership().GetSubjectId() != "reader" {
			t.Fatalf("join request = %+v", request)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("accepting the prompt did not persist membership")
	}
	model = chatBrowser.snapshot()
	if model.JoinPromptID != "" || len(model.Conversations) != 1 || !model.Conversations[0].Joined {
		t.Fatalf("accepted room did not move into the rail: %+v", model)
	}
}

func TestChatChannelFragmentDeniedLookupShowsUnavailableRoom(t *testing.T) {
	oldModel, oldClient := chatBrowser.snapshot(), chatBrowser.conversationClient()
	oldCfg := chatBrowser.config(journeyclient.Config{})
	oldLocation := js.Global().Get("location")
	client := &channelLinkClient{get: make(chan string, 1), release: make(chan struct{})}
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
	if pending := chatBrowser.snapshot(); pending.SelectedID != "private-room" || pending.State != chatui.StateLoading {
		t.Fatalf("private link left the previous room visible while authorization was pending: %+v", pending)
	}
	// An ordinary Chat refresh can advance the generation while the link is
	// resolving. The URL still owns the view when the denied read returns.
	chatBrowser.selectChatConversation("previous-room")
	close(client.release)
	deadline := time.After(2 * time.Second)
	for {
		model := chatBrowser.snapshot()
		if model.SelectedID == "private-room" && model.State == chatui.StateError {
			if len(model.Conversations) != 0 || model.Error == "" {
				t.Fatalf("denied link disclosed a room or omitted its error: %+v", model)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatalf("denied link left the previous room under the private URL: %+v", model)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}
