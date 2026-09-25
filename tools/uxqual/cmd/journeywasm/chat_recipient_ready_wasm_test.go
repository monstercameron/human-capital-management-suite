//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"sync"
	"syscall/js"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc"
)

// recordingSidebarClient just answers every PutSidebar successfully and
// records when the write actually reached the network, so a test can tell
// whether it happened before or after the readiness signals were raised.
type recordingSidebarClient struct {
	chatv1.ChatExtensionsServiceClient
	mu      sync.Mutex
	putAt   time.Time
	saved   *chatv1.PutSidebarRequest
	putDone chan struct{}
}

func (c *recordingSidebarClient) PutSidebar(_ context.Context, request *chatv1.PutSidebarRequest, _ ...grpc.CallOption) (*chatv1.PutSidebarResponse, error) {
	c.mu.Lock()
	c.putAt = time.Now()
	c.saved = request
	c.mu.Unlock()
	close(c.putDone)
	return &chatv1.PutSidebarResponse{Sidebar: &chatv1.SidebarState{Revision: 1}}, nil
}

func (c *recordingSidebarClient) GetSidebar(context.Context, *chatv1.GetSidebarRequest, ...grpc.CallOption) (*chatv1.GetSidebarResponse, error) {
	return &chatv1.GetSidebarResponse{Sidebar: &chatv1.SidebarState{Revision: 1}}, nil
}

// TestChatSidebarFirstWriteWaitsForConversationsAndRecipientLoad is CHAT-02's
// regression test: the very first sidebar write of a session must not reach
// the network until both the conversation list (chatBrowser.State ==
// StateReady) and the recipient projection's own GetSidebar read
// (chatRecipientBrowser.loadedAt) have landed -- otherwise it uses the wrong
// expected revision and the server correctly refuses it, which is the
// transient "we couldn't save your sidebar" the retest saw once on load.
func TestChatSidebarFirstWriteWaitsForConversationsAndRecipientLoad(t *testing.T) {
	oldPoll, oldTimeout := chatSidebarReadyPoll, chatSidebarReadyTimeout
	chatSidebarReadyPoll = 5 * time.Millisecond
	chatSidebarReadyTimeout = 500 * time.Millisecond
	t.Cleanup(func() { chatSidebarReadyPoll, chatSidebarReadyTimeout = oldPoll, oldTimeout })
	js.Global().Set("innerWidth", 1280)

	cfg := journeyclient.Config{Tenant: "tenant", Subject: "alice", Bearer: "token"}
	chatBrowser.reset(nil, cfg, nil)
	t.Cleanup(func() { chatBrowser.reset(nil, journeyclient.Config{}, nil) })
	// The model starts NOT ready: this is the cold-boot window the finding
	// describes, before ListConversations has answered.
	chatBrowser.mutate(func(model *chatui.Model) {
		model.State = ""
		model.SelectedID = "room-1"
		model.Conversations = []chatui.Conversation{{ID: "room-1", Kind: chatui.PublicChannel}}
		model.Sections = []chatui.SidebarSection{{ID: "channels", Name: "Channels", Chats: model.Conversations}}
	})

	client := &recordingSidebarClient{putDone: make(chan struct{})}
	chatRecipientBrowser.Lock()
	chatRecipientBrowser.generation++
	chatRecipientBrowser.client = client
	chatRecipientBrowser.sidebarRevision = 0
	chatRecipientBrowser.sidebarEdit = 0
	chatRecipientBrowser.sidebarSaved = 0
	chatRecipientBrowser.layout = recipientLayout{}
	// Not yet loaded: startChatRecipientProjection's own GetSidebar read has
	// not landed either.
	chatRecipientBrowser.loadedAt = time.Time{}
	chatRecipientBrowser.hosts = map[string]string{"room-1": "tenant"}
	chatRecipientBrowser.Unlock()

	started := time.Now()
	persistChatRecipientSidebar(cfg, chatBrowser.snapshot())

	// Neither readiness signal exists yet: the write must not have reached
	// the network right away.
	select {
	case <-client.putDone:
		t.Fatal("sidebar write reached the network before the conversation list or the recipient projection had loaded")
	case <-time.After(30 * time.Millisecond):
	}

	// The conversation list lands first (as it does in production: it drives
	// the recipient projection's own GetSidebar read).
	chatBrowser.mutate(func(model *chatui.Model) { model.State = chatui.StateReady })

	select {
	case <-client.putDone:
		t.Fatal("sidebar write reached the network with the conversation list ready but before the recipient projection's own read landed")
	case <-time.After(30 * time.Millisecond):
	}

	// The recipient projection's GetSidebar read lands second.
	chatRecipientBrowser.Lock()
	chatRecipientBrowser.loadedAt = time.Now()
	chatRecipientBrowser.Unlock()

	select {
	case <-client.putDone:
	case <-time.After(2 * time.Second):
		t.Fatal("sidebar write never reached the network once both readiness signals were raised")
	}
	if time.Since(started) < 25*time.Millisecond {
		t.Fatal("write did not actually wait for the readiness signals raised above")
	}
}

// TestChatSidebarWriteDropsDraftsOutsideSections is CHAT-02's second half:
// never send a draft for a room no section can find, since Sections is the
// server's own admission list and a draft outside it is what a race (or a
// left/removed room) can leave behind.
func TestChatSidebarWriteDropsDraftsOutsideSections(t *testing.T) {
	js.Global().Set("innerWidth", 1280)
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "alice", Bearer: "token"}
	chatBrowser.reset(nil, cfg, nil)
	t.Cleanup(func() { chatBrowser.reset(nil, journeyclient.Config{}, nil) })
	chatBrowser.mutate(func(model *chatui.Model) {
		model.State = chatui.StateReady
		model.SelectedID = "room-1"
		model.Conversations = []chatui.Conversation{{ID: "room-1", Kind: chatui.PublicChannel}}
		model.Sections = []chatui.SidebarSection{{ID: "channels", Name: "Channels", Chats: model.Conversations}}
	})
	chatBrowser.setDraft("room-1", "in a real room")
	// A draft for a room that exists in no section -- the state a race (or a
	// room the reader has since left) can leave behind.
	chatBrowser.setDraft("orphan-room", "should never be sent")

	client := &recordingSidebarClient{putDone: make(chan struct{})}
	chatRecipientBrowser.Lock()
	chatRecipientBrowser.generation++
	chatRecipientBrowser.client = client
	chatRecipientBrowser.sidebarRevision = 0
	chatRecipientBrowser.sidebarEdit = 0
	chatRecipientBrowser.sidebarSaved = 0
	chatRecipientBrowser.layout = recipientLayout{}
	chatRecipientBrowser.loadedAt = time.Now()
	chatRecipientBrowser.hosts = map[string]string{"room-1": "tenant"}
	chatRecipientBrowser.Unlock()

	persistChatRecipientSidebar(cfg, chatBrowser.snapshot())
	select {
	case <-client.putDone:
	case <-time.After(2 * time.Second):
		t.Fatal("sidebar write never reached the network")
	}

	var saved recipientLayout
	if err := json.Unmarshal([]byte(client.saved.GetSidebar().GetLayoutJson()), &saved); err != nil {
		t.Fatalf("could not parse the saved layout: %v", err)
	}
	if _, ok := saved.Drafts["orphan-room"]; ok {
		t.Fatal("the write sent a draft for a room outside every section")
	}
	if saved.Drafts["room-1"] != "in a real room" {
		t.Fatalf("the write dropped a draft for a room that is in a section: %+v", saved.Drafts)
	}
}
