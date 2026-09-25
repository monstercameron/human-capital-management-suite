//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestChatHistoryPushPreservesRouterStateAndBackRestoresChatState(t *testing.T) {
	browser := installWASMHistory(t, "/workspace/app/chat")
	previousProductHistory := productHistory
	previousControlsRefresh := productHistoryControlsRefresh
	productHistory = &browserProductHistoryController{id: "0123456789abcdef0123456789abcdef"}
	refreshes := 0
	productHistoryControlsRefresh = func() { refreshes++ }
	t.Cleanup(func() {
		productHistory = previousProductHistory
		productHistoryControlsRefresh = previousControlsRefresh
	})
	productState := js.Global().Get("Object").New()
	productState.Set(productHistoryIDField, productHistory.id)
	productState.Set(productHistoryIndexField, 0)
	if !browserHistoryReplaceState(browser.history, productState, browserLocationHref()) {
		t.Fatal("could not seed app history state")
	}
	controller := &chatHistoryController{}
	controller.seed(chatui.Model{SelectedID: "room-one"})
	if controller.seeded {
		t.Fatal("seed accepted chat state before its viewer identity was available")
	}
	controller.seed(chatui.Model{CurrentTenantID: "tenant-one", CurrentUser: "subject-one", SelectedID: "room-one"})
	if !controller.seeded {
		t.Fatal("seed did not retry after the configured viewer identity arrived")
	}
	state, ok := browserHistoryState(browser.history)
	if !ok || !state.Get(chatHistoryStateField).Truthy() {
		t.Fatal("seed did not annotate the current chat entry")
	}

	// The entry may already carry router-owned keys. Chat annotation must keep
	// them so the application router can still resume its route.
	state.Set("hcmRouteMarker", "router-owned")
	controller.push(chatui.Model{CurrentTenantID: "tenant-one", CurrentUser: "subject-one", SelectedID: "room-two"})
	controller.push(chatui.Model{CurrentTenantID: "tenant-one", CurrentUser: "subject-one", SelectedID: "room-two"})
	if got := len(browser.entries); got != 2 {
		t.Fatalf("history entries after one switch and duplicate = %d, want 2", got)
	}
	current, ok := browserHistoryState(browser.history)
	if !ok || current.Get("hcmRouteMarker").String() != "router-owned" {
		t.Fatal("chat push discarded the router-owned history state")
	}
	if id, index, valid := productHistoryState(current); !valid || id != productHistory.id || index != 1 {
		t.Fatalf("app history cursor after chat push = %q/%d valid=%v, want %s/1", id, index, valid, productHistory.id)
	}
	props := productHistory.Props(productui.ResolveProductLocale("en-US"))
	if !props.CanGoBack || props.CanGoForward {
		t.Fatalf("app history controls after chat push = back:%v forward:%v, want back enabled and forward disabled", props.CanGoBack, props.CanGoForward)
	}
	if refreshes != 1 {
		t.Fatalf("shell history controls refreshed %d times after one chat push, want 1", refreshes)
	}
	if !browserHistoryGo(browser.history, -1) {
		t.Fatal("history harness rejected Back")
	}
	backState, ok := browserHistoryState(browser.history)
	if !ok {
		t.Fatal("Back entry has no history state")
	}
	got, valid := readChatNavigationState(backState)
	if !valid || got.OwnerTenantID != "tenant-one" || got.OwnerSubject != "subject-one" || got.ConversationID != "room-one" {
		t.Fatalf("Back restored chat state %#v, valid=%v; want room-one", got, valid)
	}
}

// TestChatHistoryPushWritesRoomIntoAddress is NAV-01's pure URL<->state
// mapping coverage for chat: chatHistoryHref must render exactly the room a
// push/replace selects, so the browser's visible address (not only the
// private hcmChatNavigation history.state blob) agrees with what chat has
// on screen -- the root cause the audit's Back/Forward and reload rows
// (#announcements -> #benefits -> Back/Forward -> reload) traced to
// pushState/replaceState always being called with browserLocationHref(),
// the CURRENT url, instead of an href naming the new room.
func TestChatHistoryPushWritesRoomIntoAddress(t *testing.T) {
	if got := chatHistoryHref(""); got != browserLocationHref() {
		t.Fatalf("chatHistoryHref(\"\") = %q, want the unchanged current href", got)
	}
	browser := installWASMHistory(t, "/workspace/app/chat?locale=de-DE")
	controller := &chatHistoryController{}
	controller.seed(chatui.Model{CurrentTenantID: "tenant-one", CurrentUser: "subject-one", SelectedID: "room-one"})
	if !controller.seeded {
		t.Fatal("seed did not accept the configured viewer identity")
	}
	controller.push(chatui.Model{CurrentTenantID: "tenant-one", CurrentUser: "subject-one", SelectedID: "room-two"})
	// The pushed entry's href must carry the new room as the "#channel="
	// fragment already used for pasted room links, with the rest of the
	// address (path, query) preserved.
	if got := browser.location.Get("pathname").String(); got != "/workspace/app/chat" {
		t.Fatalf("pathname after push = %q, want /workspace/app/chat", got)
	}
	if got := browser.location.Get("search").String(); got != "?locale=de-DE" {
		t.Fatalf("search after push = %q, want ?locale=de-DE preserved", got)
	}
	if got := browser.location.Get("hash").String(); got != "channel=room-two" {
		t.Fatalf("hash after push = %q, want channel=room-two", got)
	}
	// replace() must move the address the same way, without adding an entry.
	controller.replace(chatui.Model{CurrentTenantID: "tenant-one", CurrentUser: "subject-one", SelectedID: "room-three"})
	if got := len(browser.entries); got != 2 {
		t.Fatalf("entries after push+replace = %d, want 2 (replace must not add one)", got)
	}
	if got := browser.location.Get("hash").String(); got != "channel=room-three" {
		t.Fatalf("hash after replace = %q, want channel=room-three", got)
	}
}

func TestChatHistoryTracksPersonPaneAndAnnotatesHashEntryInPlace(t *testing.T) {
	browser := installWASMHistory(t, "/workspace/app/chat#channel=room-one")
	controller := &chatHistoryController{}
	base := chatui.Model{CurrentTenantID: "tenant-one", CurrentUser: "subject-one", SelectedID: "room-one"}
	controller.seed(base)
	if !controller.seeded {
		t.Fatal("chat history did not seed the hash entry")
	}

	person := base
	person.ShowPerson = true
	person.PersonDetails = &chatui.PersonDetails{ID: "person-one"}
	controller.replace(person)
	if got := len(browser.entries); got != 1 {
		t.Fatalf("annotating a hash navigation created %d entries, want 1", got)
	}
	state, ok := browserHistoryState(browser.history)
	if !ok {
		t.Fatal("annotated hash entry has no history state")
	}
	got, valid := readChatNavigationState(state)
	if !valid || got.PersonID != "person-one" || got.ConversationID != "room-one" {
		t.Fatalf("annotated hash state = %#v, valid=%v; want room-one with person-one open", got, valid)
	}

	closed := base
	controller.push(closed)
	if got := len(browser.entries); got != 2 {
		t.Fatalf("closing the person pane created %d entries, want 2", got)
	}
	if !browserHistoryGo(browser.history, -1) {
		t.Fatal("history harness rejected Back")
	}
	back, ok := browserHistoryState(browser.history)
	if !ok {
		t.Fatal("Back entry has no history state")
	}
	got, valid = readChatNavigationState(back)
	if !valid || got.PersonID != "person-one" {
		t.Fatalf("Back restored person pane state %#v, valid=%v; want person-one", got, valid)
	}
}

// TestChatHistoryAddressesChannelsByReadableName pins NAV-01's chosen chat
// URL form end to end in the browser layer: a push for a uniquely named
// channel writes "#channel=<name>", a direct message keeps its id, and the
// fragment reader resolves both the name form and the legacy id form back to
// the room id every other chat path works with.
func TestChatHistoryAddressesChannelsByReadableName(t *testing.T) {
	oldModel := chatBrowser.snapshot()
	t.Cleanup(func() { chatBrowser.mutate(func(model *chatui.Model) { *model = oldModel }) })
	chatBrowser.mutate(func(model *chatui.Model) {
		model.Conversations = []chatui.Conversation{
			{ID: "room-ann", Name: "announcements", Kind: chatui.PublicChannel, Joined: true},
			{ID: "room-dm", Name: "Ana Lopez", Kind: chatui.DirectMessage, Joined: true},
		}
		model.PreviewConversation = &chatui.Conversation{ID: "room-preview", Name: "benefits", Kind: chatui.PublicChannel}
	})
	browser := installWASMHistory(t, "/workspace/app/chat")
	controller := &chatHistoryController{}
	base := chatui.Model{CurrentTenantID: "tenant-one", CurrentUser: "subject-one"}
	for _, step := range []struct{ id, hash string }{
		{id: "room-ann", hash: "channel=announcements"},
		{id: "room-dm", hash: "channel=room-dm"},
		{id: "room-preview", hash: "channel=benefits"},
	} {
		model := base
		model.SelectedID = step.id
		controller.push(model)
		if got := browser.location.Get("hash").String(); got != step.hash {
			t.Fatalf("hash after selecting %s = %q, want %q", step.id, got, step.hash)
		}
	}

	oldLocation := js.Global().Get("location")
	t.Cleanup(func() { js.Global().Set("location", oldLocation) })
	for _, test := range []struct{ hash, want string }{
		{hash: "#channel=announcements", want: "room-ann"},
		{hash: "#channel=room-ann", want: "room-ann"},
		{hash: "#channel=benefits", want: "room-preview"},
		{hash: "#channel=room-elsewhere", want: "room-elsewhere"},
	} {
		js.Global().Set("location", js.ValueOf(map[string]any{"origin": "https://hcm.example", "hash": test.hash}))
		if got, _ := currentChatChannelFragment(); got != test.want {
			t.Fatalf("currentChatChannelFragment(%s) = %q, want %q", test.hash, got, test.want)
		}
	}
}
