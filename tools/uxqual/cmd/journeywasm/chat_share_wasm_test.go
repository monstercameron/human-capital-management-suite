//go:build js && wasm

package main

import (
	"context"
	"syscall/js"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc"
)

type shareClient struct {
	chatv1.ConversationServiceClient
	list           chan *chatv1.ListConversationsResponse
	requests       chan *chatv1.ForwardPostRequest
	forwardResults chan error
	listReturned   chan struct{}
	listStarted    chan struct{}
}

func (c *shareClient) ListConversations(context.Context, *chatv1.ListConversationsRequest, ...grpc.CallOption) (*chatv1.ListConversationsResponse, error) {
	if c.listStarted != nil {
		close(c.listStarted)
	}
	result := <-c.list
	if c.listReturned != nil {
		close(c.listReturned)
	}
	return result, nil
}
func (c *shareClient) ForwardPost(_ context.Context, request *chatv1.ForwardPostRequest, _ ...grpc.CallOption) (*chatv1.ForwardPostResponse, error) {
	c.requests <- request
	if err := <-c.forwardResults; err != nil {
		return nil, err
	}
	return &chatv1.ForwardPostResponse{}, nil
}

func waitShare(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("share state did not advance")
}

func TestTodo_CHAT_030_WASMForwardCallback(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "alice", Bearer: "token"}
	client := &shareClient{list: make(chan *chatv1.ListConversationsResponse, 1), requests: make(chan *chatv1.ForwardPostRequest, 3), forwardResults: make(chan error, 3)}
	client.forwardResults <- context.DeadlineExceeded
	client.forwardResults <- context.DeadlineExceeded
	client.forwardResults <- nil
	chatBrowser.reset(client, cfg, func() chatui.Callbacks { return chatCallbacks(cfg) })
	chatBrowser.mutate(func(m *chatui.Model) {
		m.SelectedID = "source"
		m.Draft = "keep"
		m.Messages = []chatui.Message{{ID: "post", Author: "Alice", Body: "hello"}}
	})
	callbacks := chatCallbacks(cfg)
	callbacks.OpenShare("post")
	client.list <- &chatv1.ListConversationsResponse{Conversations: []*chatv1.Conversation{
		{TenantId: "tenant", Id: "source", Name: "Source", Kind: chatv1.ConversationKind_CONVERSATION_KIND_PUBLIC_CHANNEL},
		{TenantId: "tenant", Id: "dest", Name: "Dest", Kind: chatv1.ConversationKind_CONVERSATION_KIND_PRIVATE_CHANNEL},
		{TenantId: "tenant", Id: "dm", Name: "DM", Kind: chatv1.ConversationKind_CONVERSATION_KIND_DIRECT},
	}}
	waitShare(t, func() bool { return !chatBrowser.snapshot().ShareLoading })
	if got := chatBrowser.snapshot().ShareDestinations; len(got) != 1 || got[0].ID != "dest" {
		t.Fatalf("destinations = %#v", got)
	}
	callbacks.SelectShareDestination("dest")
	callbacks.ShareMessage()
	callbacks.ShareMessage()
	request := <-client.requests
	if request.GetSourcePostId() != "post" || request.GetSourceConversationId() != "source" || request.GetDestinationConversationId() != "dest" || request.GetSourceAttribution() != nil {
		t.Fatalf("forward request = %#v", request)
	}
	waitShare(t, func() bool { return !chatBrowser.snapshot().SharePending })
	if len(client.requests) != 0 {
		t.Fatal("double-click forwarded twice")
	}
	callbacks.ShareMessage()
	retry := <-client.requests
	if retry.GetIdempotencyKey() != request.GetIdempotencyKey() {
		t.Fatal("retry key changed")
	}
	waitShare(t, func() bool { return !chatBrowser.snapshot().SharePending })
	callbacks.ShareMessage()
	success := <-client.requests
	if success.GetIdempotencyKey() != request.GetIdempotencyKey() {
		t.Fatal("success attempt changed key")
	}
	waitShare(t, func() bool { return chatBrowser.snapshot().SharePostID == "" })
	if chatBrowser.currentNotice() == "" {
		t.Fatal("success notice missing")
	}
	if chatBrowser.snapshot().Draft != "keep" || chatBrowser.snapshot().SelectedID != "source" {
		t.Fatal("share changed draft or room")
	}
	chatBrowser.reset(nil, journeyclient.Config{}, nil)
}

func TestTodo_CHAT_030_WASMStaleListingAfterAuthReset(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "alice", Bearer: "token"}
	client := &shareClient{list: make(chan *chatv1.ListConversationsResponse, 1), listReturned: make(chan struct{}), listStarted: make(chan struct{})}
	chatBrowser.reset(client, cfg, func() chatui.Callbacks { return chatCallbacks(cfg) })
	chatBrowser.mutate(func(m *chatui.Model) {
		m.SelectedID = "source"
		m.Messages = []chatui.Message{{ID: "post", Body: "hello"}}
	})
	chatCallbacks(cfg).OpenShare("post")
	select {
	case <-client.listStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("old listing did not start")
	}
	chatBrowser.reset(nil, journeyclient.Config{Tenant: "other", Subject: "bob"}, nil)
	client.list <- &chatv1.ListConversationsResponse{Conversations: []*chatv1.Conversation{{TenantId: "tenant", Id: "dest", Name: "Dest", Kind: chatv1.ConversationKind_CONVERSATION_KIND_PUBLIC_CHANNEL}}}
	select {
	case <-client.listReturned:
	case <-time.After(3 * time.Second):
		t.Fatal("old listing did not complete")
	}
	if m := chatBrowser.snapshot(); m.SharePostID != "" || len(m.ShareDestinations) != 0 || m.CurrentUser != "bob" {
		t.Fatalf("stale listing leaked: %#v", m)
	}
}

func TestTodo_CHAT_030_WASMRepaintsFastShareResult(t *testing.T) {
	global := js.Global()
	oldDoc, oldFrame, oldRerender := global.Get("document"), global.Get("requestAnimationFrame"), chatRerender
	defer func() {
		global.Set("document", oldDoc)
		global.Set("requestAnimationFrame", oldFrame)
		chatRerender = oldRerender
	}()
	chatBrowser.reset(nil, journeyclient.Config{}, nil)
	chatBrowser.mutate(func(m *chatui.Model) { m.SharePostID = "post"; m.ShareVersion = 2 })
	dataset := js.ValueOf(map[string]any{"shareVersion": "1"})
	contains := js.FuncOf(func(js.Value, []js.Value) any { return true })
	defer contains.Release()
	dialog := js.ValueOf(map[string]any{"dataset": dataset, "contains": contains})
	query := js.FuncOf(func(js.Value, []js.Value) any { return dialog })
	defer query.Release()
	global.Set("document", js.ValueOf(map[string]any{"querySelector": query, "activeElement": js.ValueOf(map[string]any{})}))
	frame := js.FuncOf(func(_ js.Value, args []js.Value) any { args[0].Invoke(); return nil })
	defer frame.Release()
	global.Set("requestAnimationFrame", frame)
	refreshes := 0
	chatRerender = func() {
		refreshes++
		if refreshes == 2 {
			dataset.Set("shareVersion", "2")
		}
	}
	refreshChatShareRoute()
	if refreshes != 2 {
		t.Fatalf("refreshes = %d, want 2", refreshes)
	}
	chatBrowser.reset(nil, journeyclient.Config{}, nil)
}
