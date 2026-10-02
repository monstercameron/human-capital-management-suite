//go:build !(js && wasm)

package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHATBUG_014_VirtualFromFirstPage: a conversation of a hundred or so
// messages, which is what a first page of posts usually holds, draws the rows
// near the viewport and not all of them; a short one is still drawn whole; and
// the window follows the reader's place.
func TestTodo_CHATBUG_014_VirtualFromFirstPage(t *testing.T) {
	room := []Conversation{{ID: "room", Name: "Room", Kind: PublicChannel}}
	messages := virtualMessages(120)
	m := Model{State: StateReady, SelectedID: "room", Conversations: room, Messages: messages}
	cache := &virtualCache{room: "room", heights: map[string]float64{}}

	// Opened at the newest message: the window ends at the last row and holds
	// a few screens of rows, not the conversation.
	bottom := virtualWindow(m, messages, cache, virtualPosition{bottom: true, height: 740})
	if !bottom.active || bottom.end != len(messages) {
		t.Fatalf("a 120-message conversation is not windowed at its newest message: %#v", bottom)
	}
	if drawn := bottom.end - bottom.start; drawn > 40 {
		t.Fatalf("the first paint draws %d of 120 rows", drawn)
	}
	markup := render(t, m)
	rows := strings.Count(markup, `data-virtual-row=`)
	if rows == 0 || rows > 44 {
		t.Fatalf("the page drew %d message rows of 120", rows)
	}
	if !strings.Contains(markup, `data-virtual-row="post-0119"`) {
		t.Fatal("the newest message is not drawn")
	}
	if strings.Contains(markup, `data-virtual-row="post-0000"`) {
		t.Fatal("the oldest message is drawn although it is far above the viewport")
	}
	if !strings.Contains(markup, `data-virtual-spacer="before"`) || !strings.Contains(markup, `data-virtual-spacer="after"`) {
		t.Fatal("the rows that are not drawn have no spacers to hold their place")
	}

	// A reader who scrolled up keeps their place: the window is around the
	// row they were reading, and going back to the newest ends at the last row.
	position := virtualAnchor(m, messages, cache, 3000, 740)
	if position.bottom || position.anchor == "" {
		t.Fatalf("a place in the middle was taken for the bottom: %#v", position)
	}
	middle := virtualWindow(m, messages, cache, position)
	anchored := false
	for _, message := range messages[middle.start:middle.end] {
		anchored = anchored || message.ID == position.anchor
	}
	if !middle.active || !anchored || middle.end == len(messages) || middle.start == 0 {
		t.Fatalf("the window %d..%d does not hold the reader's row %s", middle.start, middle.end, position.anchor)
	}
	position.bottom = true
	if newest := virtualWindow(m, messages, cache, position); newest.end != len(messages) {
		t.Fatalf("going to the newest message ends at row %d of %d", newest.end, len(messages))
	}

	// A short conversation is drawn whole, without the window's bookkeeping.
	short := Model{State: StateReady, SelectedID: "room", Conversations: room, Messages: virtualMessages(virtualTimelineThreshold)}
	if layout := virtualWindow(short, short.Messages, cache, virtualPosition{bottom: true, height: 740}); layout.active || layout.end != virtualTimelineThreshold {
		t.Fatalf("a %d-message conversation is windowed: %#v", virtualTimelineThreshold, layout)
	}
	if whole := render(t, short); strings.Contains(whole, `data-virtual-row=`) || strings.Count(whole, `data-message-id="post-`) < virtualTimelineThreshold {
		t.Fatal("a short conversation was not drawn whole")
	}
	if virtualTimelineThreshold > 40 {
		t.Fatalf("the threshold is %d: a first page of posts draws every row again", virtualTimelineThreshold)
	}
}

// TestTodo_CHATBUG_014_VirtualWholeOnRequest: code that needs a row far from
// the viewport (a search hit, a quoted original, a card) finds it on the page
// in a timeline of the length that used to be drawn whole. Such a timeline is
// windowed until one of them asks, and then it is drawn whole.
func TestTodo_CHATBUG_014_VirtualWholeOnRequest(t *testing.T) {
	t.Cleanup(func() { virtualWholeRoom = "" })
	room := []Conversation{{ID: "room", Name: "Room", Kind: PublicChannel}, {ID: "other", Name: "Other", Kind: PublicChannel}}
	messages := virtualMessages(120)
	m := Model{State: StateReady, SelectedID: "room", Conversations: room, Messages: messages}
	cache := &virtualCache{room: "room", heights: map[string]float64{}}
	at := virtualPosition{bottom: true, height: 740}

	if virtualDrawnWhole(m, len(messages)) || !virtualWindow(m, messages, cache, at).active {
		t.Fatal("a 120-message timeline is drawn whole although nobody asked for a far row")
	}

	// A search hit to focus is drawn with the whole timeline, as it was.
	focused := m
	focused.FocusMessageID = "post-0003"
	if !virtualDrawnWhole(focused, len(messages)) || virtualWindow(focused, messages, cache, at).active {
		t.Fatal("a timeline with a message to focus is windowed: the hit's row may not be on the page")
	}
	if markup := render(t, focused); strings.Contains(markup, `data-virtual-row=`) || !strings.Contains(markup, `data-message-id="post-0003"`) {
		t.Fatal("the message to focus is not drawn")
	}

	// A request to reveal a row has the conversation drawn whole until it is
	// left (RevealTimelineMessage sets this for the conversation on the page).
	virtualWholeRoom = "room"
	if !virtualDrawnWhole(m, len(messages)) || virtualWindow(m, messages, cache, at).active {
		t.Fatal("the requested conversation is still windowed")
	}
	if markup := render(t, m); strings.Contains(markup, `data-virtual-row=`) || !strings.Contains(markup, `data-message-id="post-0000"`) {
		t.Fatal("the requested conversation was not drawn whole")
	}
	elsewhere := m
	elsewhere.SelectedID = "other"
	if virtualDrawnWhole(elsewhere, len(messages)) {
		t.Fatal("a request for one conversation drew another one whole")
	}
	render(t, elsewhere)
	if virtualWholeRoom != "" {
		t.Fatal("the request outlived the conversation it was made for")
	}

	// A timeline longer than that was never drawn whole and still is not.
	long := Model{State: StateReady, SelectedID: "room", Conversations: room, Messages: virtualMessages(virtualWholeLimit + 1), FocusMessageID: "post-0003"}
	virtualWholeRoom = "room"
	if virtualDrawnWhole(long, len(long.Messages)) || !virtualWindow(long, long.Messages, cache, at).active {
		t.Fatalf("a %d-message timeline is drawn whole", len(long.Messages))
	}
	if RevealTimelineMessage("post-0003") {
		t.Fatal("a reveal outside the browser reported a page change")
	}
}
