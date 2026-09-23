//go:build js && wasm

package main

import (
	"context"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc"
)

type embedClient struct {
	chatv1.ConversationServiceClient
	requests chan string
	results  chan *chatv1.ResolveShareLinkResponse
}

func (c *embedClient) ResolveShareLink(_ context.Context, req *chatv1.ResolveShareLinkRequest, _ ...grpc.CallOption) (*chatv1.ResolveShareLinkResponse, error) {
	c.requests <- req.GetToken()
	return <-c.results, nil
}

func TestChatEmbedWASMResolutionAndStaleAuth(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "alice", Bearer: "token"}
	client := &embedClient{requests: make(chan string, 4), results: make(chan *chatv1.ResolveShareLinkResponse, 4)}
	chatBrowser.reset(client, cfg, nil)
	chatBrowser.mutate(func(m *chatui.Model) {
		m.SelectedID = "room"
		m.EmbedOrigin = "https://hcm.example"
		m.Messages = []chatui.Message{{Body: "/chat/share/one"}}
	})
	resolveVisibleChatEmbeds(cfg)
	select {
	case token := <-client.requests:
		if token != "one" {
			t.Fatal(token)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("initial post not resolved")
	}
	client.results <- &chatv1.ResolveShareLinkResponse{Conversation: &chatv1.Conversation{TenantId: "tenant", Id: "source", Name: "Source"}, Post: &chatv1.Post{TenantId: "tenant", ConversationId: "source", Id: "post", Body: "authorized body"}}
	waitShare(t, func() bool { return chatBrowser.snapshot().Embeds["one"].State == "ready" })
	chatBrowser.mutate(func(m *chatui.Model) {
		m.Messages = append(m.Messages, chatui.Message{Body: "/chat/share/two"})
		m.Draft = "/chat/share/three"
	})
	resolveVisibleChatEmbeds(cfg)
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case token := <-client.requests:
			seen[token] = true
		case <-time.After(2 * time.Second):
			t.Fatal("live/draft links not resolved")
		}
	}
	if !seen["two"] || !seen["three"] {
		t.Fatalf("requests = %#v", seen)
	}
	client.results <- &chatv1.ResolveShareLinkResponse{Conversation: &chatv1.Conversation{TenantId: "other", Id: "foreign"}, Post: &chatv1.Post{TenantId: "other", ConversationId: "foreign", Id: "post", Body: "foreign secret"}}
	client.results <- &chatv1.ResolveShareLinkResponse{}
	waitShare(t, func() bool {
		return chatBrowser.snapshot().Embeds["two"].State == "unavailable" && chatBrowser.snapshot().Embeds["three"].State == "unavailable"
	})
	chatBrowser.invalidateChatEmbeds()
	resolveVisibleChatEmbeds(cfg)
	select {
	case <-client.requests:
	case <-time.After(2 * time.Second):
		t.Fatal("revalidation not requested")
	}
	chatBrowser.reset(client, journeyclient.Config{Tenant: "tenant", Subject: "bob", Bearer: "other"}, nil)
	client.results <- &chatv1.ResolveShareLinkResponse{Conversation: &chatv1.Conversation{TenantId: "tenant", Id: "source"}, Post: &chatv1.Post{TenantId: "tenant", ConversationId: "source", Id: "post", Body: "stale secret"}}
	time.Sleep(30 * time.Millisecond)
	if len(chatBrowser.snapshot().Embeds) != 0 {
		t.Fatal("stale authorized body crossed principal reset")
	}
}
