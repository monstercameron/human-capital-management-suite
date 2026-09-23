//go:build js && wasm

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

type heldChatPostRead struct {
	room    string
	release chan struct{}
}

type heldChatPostClient struct {
	chatv1.ConversationServiceClient
	reads chan heldChatPostRead
	done  chan struct{}
}

func (f *heldChatPostClient) ListPosts(_ context.Context, request *chatv1.ListPostsRequest, _ ...grpc.CallOption) (*chatv1.ListPostsResponse, error) {
	read := heldChatPostRead{room: request.GetConversationId(), release: make(chan struct{})}
	f.reads <- read
	select {
	case <-read.release:
	case <-f.done:
	}
	return nil, errors.New("test read failed")
}

func TestChatRepeatedClickKeepsOnePendingReadAndLocalRender(t *testing.T) {
	oldModel, oldClient := chatBrowser.snapshot(), chatBrowser.conversationClient()
	oldCfg := chatBrowser.config(journeyclient.Config{})
	oldRerender, oldRetry := chatRerender, productRouteRetry
	client := &heldChatPostClient{reads: make(chan heldChatPostRead, 3), done: make(chan struct{})}
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "reader", Locale: "en-US"}
	chatBrowser.reset(client, cfg, nil)
	localRenders, routeReads := 0, 0
	chatRerender = func() { localRenders++ }
	productRouteRetry = func() { routeReads++ }
	t.Cleanup(func() {
		close(client.done)
		chatBrowser.reset(oldClient, oldCfg, nil)
		chatBrowser.mutate(func(model *chatui.Model) { *model = oldModel })
		chatRerender, productRouteRetry = oldRerender, oldRetry
	})

	openChatConversation(cfg, "room-a")
	first := waitHeldChatRead(t, client.reads)
	if first.room != "room-a" || !chatBrowser.alreadyOpening("room-a") {
		t.Fatal("first click did not start the selected room's read")
	}
	generation, renders := chatBrowser.currentGeneration(), localRenders
	openChatConversation(cfg, "room-a")
	if chatBrowser.currentGeneration() != generation || localRenders != renders || routeReads != 0 || len(client.reads) != 0 {
		t.Fatal("second click restarted the pending read or revalidated the route")
	}
	openChatConversation(cfg, "room-b")
	second := waitHeldChatRead(t, client.reads)
	if second.room != "room-b" || !chatBrowser.alreadyOpening("room-b") || routeReads != 0 {
		t.Fatal("switching to another room did not use a local render and start its read")
	}
	close(first.release)
	close(second.release)
	deadline := time.After(2 * time.Second)
	for chatBrowser.alreadyOpening("room-b") {
		select {
		case <-deadline:
			t.Fatal("failed read did not release the opening latch")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	openChatConversation(cfg, "room-b")
	retry := waitHeldChatRead(t, client.reads)
	if retry.room != "room-b" || routeReads != 0 {
		t.Fatal("a failed read did not allow explicit retry in place")
	}
	close(retry.release)
	waitChatOpenFinished(t, "room-b")
}

func waitChatOpenFinished(t *testing.T, room string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for chatBrowser.alreadyOpening(room) {
		select {
		case <-deadline:
			t.Fatalf("%s read did not finish", room)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func waitHeldChatRead(t *testing.T, reads <-chan heldChatPostRead) heldChatPostRead {
	t.Helper()
	select {
	case read := <-reads:
		return read
	case <-time.After(2 * time.Second):
		t.Fatal("chat post read did not start")
		return heldChatPostRead{}
	}
}
