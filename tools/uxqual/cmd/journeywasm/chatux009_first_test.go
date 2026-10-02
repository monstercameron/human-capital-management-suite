package main

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// CHATUX-009: which reads leave the open conversation's messages until the page
// is on screen. The first load does; every later read of the page does not, so
// an action's refresh, a retry and a revalidation read the timeline with the
// list exactly as before.
func TestTodo_CHATUX_009(t *testing.T) {
	ready := chatui.Model{State: chatui.StateReady, Messages: []chatui.Message{{ID: "m1"}}, SelectedID: "room"}
	for name, tc := range map[string]struct {
		previous chatui.Model
		want     bool
	}{
		"nothing read yet":              {chatui.Model{}, true},
		"loading":                       {chatui.Model{State: chatui.StateLoading}, true},
		"messages already shown":        {ready, false},
		"an empty conversation settled": {chatui.Model{State: chatui.StateEmpty}, false},
		"a failed read being retried":   {chatui.Model{State: chatui.StateError}, false},
		"loading but messages present":  {chatui.Model{State: chatui.StateLoading, Messages: ready.Messages}, false},
	} {
		if got := chatux009FirstLoad(tc.previous); got != tc.want {
			t.Errorf("%s: chatux009FirstLoad = %v, want %v", name, got, tc.want)
		}
	}

	// While the first render is on its way a change made to the model does not
	// re-read the route; once the page is on screen, or the grace period is over,
	// it does.
	chatux009EndRender()
	if chatux009RenderPending() {
		t.Fatal("a render is pending before any route load started")
	}
	chatux009BeginRender()
	if !chatux009RenderPending() {
		t.Fatal("a route load is in flight but the render is not pending")
	}
	chatux009EndRender()
	if chatux009RenderPending() {
		t.Fatal("the page is on screen but the render is still pending")
	}
	chatux009Render.Lock()
	chatux009Render.since = time.Now().Add(-2 * chatux009RenderGrace)
	chatux009Render.Unlock()
	if chatux009RenderPending() {
		t.Fatal("a page left before it mounted holds back every later change")
	}
	chatux009EndRender()

	// The first open of the page is held back from its secondary reads once, and
	// only for the conversation it was marked for.
	chatux009HoldFor("room")
	if chatux009TakeHold("other") {
		t.Fatal("a hold for one room was taken by another")
	}
	if !chatux009TakeHold("room") || chatux009TakeHold("room") {
		t.Fatal("the hold must be taken exactly once")
	}
	chatux009HoldFor("")
	if chatux009TakeHold("") {
		t.Fatal("an empty hold was taken")
	}

	open := chatui.Model{SelectedID: "room"}
	preview := chatui.Model{SelectedID: "room", PreviewConversation: &chatui.Conversation{ID: "room"}}
	searching := chatui.Model{SelectedID: "room", Search: "budget"}
	for name, tc := range map[string]struct {
		firstLoad, listed, streaming bool
		model                        chatui.Model
		want                         bool
	}{
		"first load of a joined room":           {true, true, false, open, true},
		"not the first load":                    {false, true, false, open, false},
		"no room selected":                      {true, false, false, chatui.Model{}, false},
		"a room that is not in the list":        {true, false, false, open, false},
		"a preview of a room not joined":        {true, true, false, preview, false},
		"a search is open":                      {true, true, false, searching, false},
		"a stream is already delivering a room": {true, true, true, open, false},
	} {
		if got := chatux009DeferTimeline(tc.firstLoad, tc.model, tc.listed, tc.streaming); got != tc.want {
			t.Errorf("%s: chatux009DeferTimeline = %v, want %v", name, got, tc.want)
		}
	}
}
