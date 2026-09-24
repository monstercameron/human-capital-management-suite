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
