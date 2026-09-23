//go:build !(js && wasm)

package chatui

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func virtualMessages(n int) []Message {
	base := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	result := make([]Message, n)
	for i := range result {
		result[i] = Message{ID: fmt.Sprintf("post-%04d", i), AuthorID: fmt.Sprintf("author-%d", i%17), Author: "Reader", Body: strings.Repeat("text ", i%35+1), SentAt: base.Add(time.Duration(i) * time.Minute)}
	}
	return result
}

func TestChatVirtualTimelineBoundsAndAnchor(t *testing.T) {
	messages := virtualMessages(2000)
	m := Model{State: StateReady, SelectedID: "room", Conversations: []Conversation{{ID: "room", Name: "Room", Kind: PublicChannel}}, Messages: messages, HasOlder: true}
	cache := &virtualCache{room: "room", heights: map[string]float64{}}
	bottom := virtualWindow(m, messages, cache, virtualPosition{bottom: true, height: 740})
	if !bottom.active || bottom.end != len(messages) || bottom.end-bottom.start > virtualTimelineMaxRows {
		t.Fatalf("bottom window = %#v", bottom)
	}
	markup := render(t, m)
	if got := strings.Count(markup, `data-virtual-row=`); got == 0 || got > virtualTimelineMaxRows+4 {
		t.Fatalf("rendered %d of 2000 message rows", got)
	}
	position := virtualAnchor(m, messages, cache, 50000, 740)
	middle := virtualWindow(m, messages, cache, position)
	if middle.start == bottom.start || middle.start < 100 || middle.end-middle.start > virtualTimelineMaxRows {
		t.Fatalf("middle window = %#v", middle)
	}
	firstVisible := position.anchor
	prepend := virtualMessages(30)
	for i := range prepend {
		prepend[i].ID = fmt.Sprintf("earlier-%04d", i)
		prepend[i].SentAt = messages[0].SentAt.Add(-time.Duration(30-i) * time.Minute)
	}
	withOlder := append(prepend, messages...)
	m.Messages = withOlder
	after := virtualWindow(m, withOlder, cache, position)
	if after.desired <= middle.desired || withOlder[virtualIndex(virtualPrefix(messageHeights(m, withOlder, cache.heights)), after.desired-42)].ID != firstVisible {
		t.Fatalf("prepend lost anchor %q: before=%v after=%v", firstVisible, middle.desired, after.desired)
	}
	cache.heights[firstVisible] = 420
	measured := virtualWindow(m, withOlder, cache, position)
	if measured.end-measured.start > virtualTimelineMaxRows || measured.start > after.start+1 {
		t.Fatalf("variable-height window = %#v", measured)
	}
}

func TestChatVirtualTimelineKeepsStatusAndPagingControls(t *testing.T) {
	m := Model{State: StateError, SelectedID: "room", Messages: virtualMessages(1000), Error: "failed"}
	if strings.Contains(render(t, m), `data-virtual-row=`) {
		t.Fatal("error rendered retained messages")
	}
	m.State = StateReady
	m.HasOlder = true
	m.HasNewer = true
	m.ThreadHasOlder = true
	m.ThreadHasNewer = true
	m.ShowThread = true
	m.ThreadParentID = m.Messages[0].ID
	m.ThreadMessages = []Message{{ID: "reply", Body: "Reply"}}
	markup := render(t, m)
	for _, want := range []string{"Show earlier messages", "Show newer messages", "Show earlier replies", "Show newer replies"} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestChatVirtualScrollOnlyRendersWhenWindowCanChange(t *testing.T) {
	old := virtualPosition{room: "room", anchor: "post-10", offset: 12, top: 1200, height: 740}
	withinRow := old
	withinRow.offset = 38
	withinRow.top = 1226
	if virtualPositionNeedsRender(old, withinRow) {
		t.Fatal("pixel movement inside the same anchor scheduled a list render")
	}
	newAnchor := withinRow
	newAnchor.anchor = "post-11"
	if !virtualPositionNeedsRender(withinRow, newAnchor) {
		t.Fatal("crossing a message boundary did not schedule the new virtual window")
	}
	resized := withinRow
	resized.height = 800
	if !virtualPositionNeedsRender(withinRow, resized) {
		t.Fatal("viewport resize did not schedule the recalculated window")
	}
}

func TestChatUnavailableMediaHasTerminalLabel(t *testing.T) {
	requestedPost, requestedAttachment := "", ""
	m := Model{State: StateReady, SelectedID: "room", Conversations: []Conversation{{ID: "room", Name: "Room"}}, Messages: []Message{{ID: "post", Body: "Media", Attachments: []Attachment{{ID: "image", Name: "large.png", ContentType: "image/png", PreviewUnavailable: true}, {ID: "file", Name: "large.zip", ContentType: "application/zip", PreviewUnavailable: true}}}}, Callbacks: Callbacks{DownloadAttachment: func(post, attachment string) { requestedPost, requestedAttachment = post, attachment }}}
	markup := render(t, m)
	if strings.Count(markup, "Preview unavailable") != 2 || strings.Count(markup, `data-action="download-attachment"`) != 2 || strings.Contains(markup, "Loading attachment") {
		t.Fatal("terminal media state rendered as pending")
	}
	m.actWith("download-attachment", "post", "image")
	if requestedPost != "post" || requestedAttachment != "image" {
		t.Fatal("download action lost source identity")
	}
	m.Messages[0].Attachments[0].URL = "blob:preview"
	m.Messages[0].Attachments[0].PreviewUnavailable = false
	if strings.Count(render(t, m), `data-action="download-attachment"`) != 2 {
		t.Fatal("loaded image lost explicit download fallback")
	}
}

func TestChatThreadParentSurvivesTimelineEviction(t *testing.T) {
	parent := Message{ID: "evicted-root", Author: "Parent author", Body: "Thread context survives"}
	m := Model{State: StateReady, SelectedID: "room", Conversations: []Conversation{{ID: "room", Name: "Room"}}, Messages: []Message{{ID: "latest", Body: "Recent"}}, ShowThread: true, ThreadParentID: parent.ID, ThreadParent: &parent, ThreadMessages: []Message{{ID: "reply", Author: "Reader", Body: "Reply"}}}
	markup := render(t, m)
	if !strings.Contains(markup, "Thread context survives") || !strings.Contains(markup, "Reply") {
		t.Fatal("evicted thread root or reply missing")
	}
	m.ThreadParentID = "other"
	if strings.Contains(render(t, m), "Thread context survives") {
		t.Fatal("stale thread root appeared under another parent")
	}
}
