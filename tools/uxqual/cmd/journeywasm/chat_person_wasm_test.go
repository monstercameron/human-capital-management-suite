//go:build js && wasm

package main

import (
	"context"
	"errors"
	"strings"
	"syscall/js"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/testkit/render"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc"
)

type personWorkerClient struct {
	journeyv1.JourneyServiceClient
	read chan struct{}
}

func (f *personWorkerClient) ListWorkers(context.Context, *journeyv1.ListWorkersRequest, ...grpc.CallOption) (*journeyv1.ListWorkersResponse, error) {
	close(f.read)
	return &journeyv1.ListWorkersResponse{Workers: []*journeyv1.Worker{{WorkerRef: "hc-050-rafael-torres", PreferredName: "Rafael", LegalName: "Rafael Torres", JobTitle: "Director of People Operations", OrgUnit: "People Operations"}}}, nil
}

func TestChatPersonAsyncReadRepaintsMountedPanel(t *testing.T) {
	oldDocument := js.Global().Get("document")
	oldWindowAdd, oldWindowRemove := js.Global().Get("addEventListener"), js.Global().Get("removeEventListener")
	oldWorkers, oldRerender := chatWorkers, chatRerender
	oldState := chatBrowser.snapshot()
	noop := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	query := js.FuncOf(func(js.Value, []js.Value) any { return js.Null() })
	queryAll := js.FuncOf(func(js.Value, []js.Value) any { return js.Global().Get("Array").New() })
	document := js.Global().Get("Object").New()
	document.Set("documentElement", js.Null())
	document.Set("querySelector", query)
	document.Set("querySelectorAll", queryAll)
	document.Set("addEventListener", noop)
	document.Set("removeEventListener", noop)
	js.Global().Set("document", document)
	js.Global().Set("addEventListener", noop)
	js.Global().Set("removeEventListener", noop)
	t.Cleanup(func() {
		js.Global().Set("document", oldDocument)
		js.Global().Set("addEventListener", oldWindowAdd)
		js.Global().Set("removeEventListener", oldWindowRemove)
		chatWorkers, chatRerender = oldWorkers, oldRerender
		chatBrowser.mutate(func(model *chatui.Model) { *model = oldState })
		query.Release()
		queryAll.Release()
		noop.Release()
	})
	cfg := journeyclient.Config{Tenant: "harborcare-demo", Subject: "hc-050-rafael-torres", Bearer: "test"}
	chatBrowser.reset(nil, cfg, nil)
	resetChatPersonActions()
	chatBrowser.mutate(func(model *chatui.Model) {
		model.State = chatui.StateReady
		model.Callbacks = withChatPersonCallbacks(chatui.Callbacks{}, cfg)
	})
	workers := &personWorkerClient{read: make(chan struct{})}
	chatWorkers = workers
	view := productui.NewView(productui.PageChat, "HarborCare", "Rafael Torres", "scope")
	fixture := render.New(t, render.WithQueuedScheduler())
	defer fixture.Cleanup()
	fixture.Render(ui.CreateElement(renderChatPage, chatPageProps{View: view}))
	fixture.Stabilize()
	openChatPerson(cfg, cfg.Subject)
	fixture.Stabilize()
	select {
	case <-workers.read:
	case <-time.After(3 * time.Second):
		t.Fatal("worker read did not complete")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		fixture.Stabilize()
		if strings.Contains(fixture.Text(), "Director of People Operations") && chatBrowser.snapshot().PersonDetails.Ready {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("async person read did not repaint mounted panel: %s", fixture.Text())
}

func TestSelfDMPeerHydratesAfterReloadAndMemberRead(t *testing.T) {
	const subject = "hc-050-rafael-torres"
	cfg := journeyclient.Config{Tenant: "harborcare-demo", Subject: subject, Bearer: "test"}
	fake := &personDMClient{members: []*chatv1.Membership{{HomeTenantId: cfg.Tenant, SubjectId: subject}}}
	oldRerender := chatRerender
	chatRerender = func() {}
	t.Cleanup(func() { chatRerender = oldRerender; chatBrowser.reset(nil, journeyclient.Config{}, nil) })
	chatBrowser.reset(fake, cfg, nil)
	room := chatui.Conversation{ID: "opaque-self-dm", Name: "opaque-self-dm", Kind: chatui.DirectMessage, Joined: true}
	chatBrowser.mutate(func(model *chatui.Model) {
		model.SelectedID = room.ID
		model.Conversations = []chatui.Conversation{room}
		model.Sections = []chatui.SidebarSection{{ID: "direct", Chats: []chatui.Conversation{room}}}
	})
	chatBrowser.mergeDirectory(map[string]string{subject: "Rafael Torres"})
	startChatDMPeers(cfg, []chatui.Conversation{room})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		model := chatBrowser.snapshot()
		if model.PeerIDs[room.ID] == subject && model.Conversations[0].Name == "Rafael Torres" && model.Sections[0].Chats[0].Name == "Rafael Torres" {
			loadChatMembers(cfg)
			model = chatBrowser.snapshot()
			if model.PeerIDs[room.ID] != subject || model.Conversations[0].Name != "Rafael Torres" || model.Sections[0].Chats[0].Name != "Rafael Torres" {
				t.Fatalf("member read lost self DM label: %+v", model)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("reload did not resolve self DM label: %+v", chatBrowser.snapshot())
}

func TestChatDMPeerRejectsAmbiguousMemberships(t *testing.T) {
	member := func(id string) *chatv1.Membership { return &chatv1.Membership{SubjectId: id} }
	for _, tc := range []struct {
		name    string
		members []*chatv1.Membership
		want    string
	}{
		{name: "self", members: []*chatv1.Membership{member("me")}, want: "me"},
		{name: "pair", members: []*chatv1.Membership{member("me"), member("peer")}, want: "peer"},
		{name: "missing viewer", members: []*chatv1.Membership{member("peer")}},
		{name: "duplicate", members: []*chatv1.Membership{member("me"), member("me")}},
		{name: "group", members: []*chatv1.Membership{member("me"), member("peer"), member("third")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := chatDMPeerFromMemberships(tc.members, "me"); got != tc.want {
				t.Fatalf("peer = %q, want %q", got, tc.want)
			}
		})
	}
}

type personDMClient struct {
	chatv1.ConversationServiceClient
	listed     chan struct{}
	release    chan struct{}
	created    chan *chatv1.CreateConversationRequest
	rooms      []*chatv1.Conversation
	members    []*chatv1.Membership
	createRoom *chatv1.Conversation
}

func (f *personDMClient) ListConversations(context.Context, *chatv1.ListConversationsRequest, ...grpc.CallOption) (*chatv1.ListConversationsResponse, error) {
	if f.listed != nil {
		f.listed <- struct{}{}
	}
	if f.release != nil {
		<-f.release
	}
	return &chatv1.ListConversationsResponse{Conversations: f.rooms}, nil
}
func (f *personDMClient) ListMemberships(context.Context, *chatv1.ListMembershipsRequest, ...grpc.CallOption) (*chatv1.ListMembershipsResponse, error) {
	return &chatv1.ListMembershipsResponse{Memberships: f.members}, nil
}
func (f *personDMClient) CreateConversation(_ context.Context, request *chatv1.CreateConversationRequest, _ ...grpc.CallOption) (*chatv1.CreateConversationResponse, error) {
	f.created <- request
	return &chatv1.CreateConversationResponse{Conversation: f.createRoom}, nil
}
func (f *personDMClient) ListPosts(context.Context, *chatv1.ListPostsRequest, ...grpc.CallOption) (*chatv1.ListPostsResponse, error) {
	return nil, errors.New("test stops after room selection")
}

func setupPersonDMTest(t *testing.T, fake *personDMClient) journeyclient.Config {
	t.Helper()
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "alice", Bearer: "token"}
	resetChatPersonActions()
	chatBrowser.reset(fake, cfg, nil)
	chatBrowser.mutate(func(model *chatui.Model) {
		model.ShowPerson = true
		model.PersonDetails = &chatui.PersonDetails{ID: "bob", Name: "Bob", Ready: true}
		model.Sections = []chatui.SidebarSection{{ID: "channels", Name: "Channels"}, {ID: "direct", Name: "Direct messages"}}
	})
	chatPersonActions.Lock()
	chatPersonActions.authorized = true
	chatPersonActions.Unlock()
	return cfg
}

func TestChatPersonDMReusesAdmittedRoom(t *testing.T) {
	fake := &personDMClient{created: make(chan *chatv1.CreateConversationRequest, 1), rooms: []*chatv1.Conversation{{Id: "dm", TenantId: "tenant", Kind: chatv1.ConversationKind_CONVERSATION_KIND_DIRECT, Joined: true}}, members: []*chatv1.Membership{{HomeTenantId: "tenant", SubjectId: "alice"}, {HomeTenantId: "tenant", SubjectId: "bob"}}}
	cfg := setupPersonDMTest(t, fake)
	chatBrowser.mutate(func(model *chatui.Model) { model.SelectedID = "dm" })
	chatBrowser.mu.Lock()
	chatBrowser.opening = "dm"
	chatBrowser.mu.Unlock()
	startChatPersonDM(cfg, "bob")
	model := chatBrowser.snapshot()
	if model.ShowPerson || model.PersonDetails != nil || model.PeerIDs["dm"] != "bob" || len(model.Conversations) != 1 || model.Conversations[0].ID != "dm" || model.Conversations[0].Name != "Bob" || len(model.Sections[1].Chats) != 1 || model.Sections[1].Chats[0].Name != "Bob" {
		t.Fatalf("matching admitted room was not opened: %+v", model)
	}
	select {
	case <-fake.created:
		t.Fatal("matching room was duplicated")
	default:
	}
}

func TestChatPersonDMDoubleClickAndStaleRead(t *testing.T) {
	fake := &personDMClient{listed: make(chan struct{}, 2), release: make(chan struct{}), created: make(chan *chatv1.CreateConversationRequest, 2)}
	cfg := setupPersonDMTest(t, fake)
	done := make(chan struct{})
	go func() { startChatPersonDM(cfg, "bob"); close(done) }()
	select {
	case <-fake.listed:
	case <-time.After(3 * time.Second):
		t.Fatal("listing did not start")
	}
	startChatPersonDM(cfg, "bob")
	select {
	case <-fake.listed:
		t.Fatal("double click started another listing")
	default:
	}
	chatBrowser.mutate(func(model *chatui.Model) { model.ShowPerson = false; model.PersonDetails = nil })
	close(fake.release)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stale listing did not finish")
	}
	select {
	case <-fake.created:
		t.Fatal("stale profile created a conversation")
	default:
	}
	if chatBrowser.snapshot().ShowPerson {
		t.Fatal("stale result reopened profile")
	}
}

func TestChatPersonDMCreateHasStableScopedPayload(t *testing.T) {
	fake := &personDMClient{created: make(chan *chatv1.CreateConversationRequest, 1), createRoom: &chatv1.Conversation{Id: "new-dm", TenantId: "tenant", Kind: chatv1.ConversationKind_CONVERSATION_KIND_DIRECT, Joined: true}}
	cfg := setupPersonDMTest(t, fake)
	startChatPersonDM(cfg, "bob")
	select {
	case request := <-fake.created:
		if request.GetTenantId() != "tenant" || request.GetOwnerId() != "alice" || request.GetName() != "" || request.GetKind() != chatv1.ConversationKind_CONVERSATION_KIND_DIRECT || len(request.GetMembers()) != 1 || request.GetMembers()[0].GetSubjectId() != "bob" || request.GetMembers()[0].GetTenantId() != "tenant" || request.GetIdempotencyKey() != chatPersonDMKey("tenant", "alice", "bob") {
			t.Fatalf("unsafe DM request: %+v", request)
		}
	default:
		t.Fatal("no DM create request")
	}
	model := chatBrowser.snapshot()
	if model.ShowPerson || model.SelectedID != "new-dm" || len(model.Conversations) != 1 || model.Conversations[0].ID != "new-dm" || model.Conversations[0].Name != "Bob" || model.PeerIDs["new-dm"] != "bob" || len(model.Sections[1].Chats) != 1 || model.Sections[1].Chats[0].Name != "Bob" {
		t.Fatalf("created DM was not opened: %+v", model)
	}
}

func TestChatPersonDMKeyIsStableForPair(t *testing.T) {
	a := chatPersonDMKey("tenant", "alice", "bob")
	if a == "" || a != chatPersonDMKey("tenant", "bob", "alice") || a == chatPersonDMKey("other", "alice", "bob") || a == chatPersonDMKey("tenant", "alice", "eve") {
		t.Fatalf("unstable or unscoped pair key: %q", a)
	}
	if self := chatPersonDMKey("tenant", "alice", "alice"); self == "" || self == a || self == chatPersonDMKey("other", "alice", "alice") {
		t.Fatalf("unstable or unscoped self key: %q", self)
	}
}

func TestChatPersonDMSelfCreateUsesOneMember(t *testing.T) {
	fake := &personDMClient{created: make(chan *chatv1.CreateConversationRequest, 1), createRoom: &chatv1.Conversation{Id: "self-dm", TenantId: "tenant", Kind: chatv1.ConversationKind_CONVERSATION_KIND_DIRECT, Joined: true}}
	cfg := setupPersonDMTest(t, fake)
	chatBrowser.mutate(func(model *chatui.Model) {
		model.PersonDetails = &chatui.PersonDetails{ID: "alice", Name: "Alice", Ready: true}
	})
	startChatPersonDM(cfg, "alice")
	select {
	case request := <-fake.created:
		if request.GetTenantId() != "tenant" || request.GetOwnerId() != "alice" || request.GetKind() != chatv1.ConversationKind_CONVERSATION_KIND_DIRECT || len(request.GetMembers()) != 1 || request.GetMembers()[0].GetTenantId() != "tenant" || request.GetMembers()[0].GetSubjectId() != "alice" || request.GetIdempotencyKey() != chatPersonDMKey("tenant", "alice", "alice") {
			t.Fatalf("unsafe self DM request: %+v", request)
		}
	default:
		t.Fatal("no self DM create request")
	}
	model := chatBrowser.snapshot()
	if model.ShowPerson || model.SelectedID != "self-dm" || len(model.Conversations) != 1 || model.PeerIDs["self-dm"] != "alice" || model.Conversations[0].Name != "Alice" {
		t.Fatalf("self DM was not opened privately: %+v", model)
	}
}

func TestChatDirectMembersMatchRequiresActiveSameTenantPair(t *testing.T) {
	good := []*chatv1.Membership{{HomeTenantId: "tenant", SubjectId: "alice"}, {HomeTenantId: "tenant", SubjectId: "bob"}}
	if !chatDirectMembersMatch(good, "tenant", "alice", "bob") {
		t.Fatal("valid pair was not reused")
	}
	if !chatDirectMembersMatch(good[:1], "tenant", "alice", "alice") {
		t.Fatal("single-member self DM was not reused")
	}
	if chatDirectMembersMatch(good, "tenant", "alice", "alice") || chatDirectMembersMatch([]*chatv1.Membership{{HomeTenantId: "other", SubjectId: "alice"}}, "tenant", "alice", "alice") {
		t.Fatal("self DM accepted a second or foreign-tenant member")
	}
	for _, bad := range [][]*chatv1.Membership{
		{good[0]},
		{good[0], good[0]},
		{good[0], {HomeTenantId: "other", SubjectId: "bob"}},
		{good[0], {HomeTenantId: "tenant", SubjectId: "eve"}},
	} {
		if chatDirectMembersMatch(bad, "tenant", "alice", "bob") {
			t.Fatalf("nonmatching pair was reused: %+v", bad)
		}
	}
}
