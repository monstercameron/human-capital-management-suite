//go:build !(js && wasm)

package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func issue3827Timeline() (Model, []Message, *virtualCache) {
	messages := virtualMessages(400)
	m := Model{State: StateReady, SelectedID: "room", Conversations: []Conversation{{ID: "room", Name: "Room", Kind: PublicChannel}}, Messages: messages, HasOlder: true}
	return m, messages, &virtualCache{room: "room", heights: map[string]float64{}}
}

// A window reaches at least a viewport above and below the rows in view,
// whatever the rows measure, and never more rows than the limit.
func TestIssue3827WindowOverscanIsAViewportEachSide(t *testing.T) {
	m, messages, cache := issue3827Timeline()
	for _, viewport := range []float64{480, 623, 900} {
		for _, top := range []float64{9000, 20000} {
			layout := virtualWindow(m, messages, cache, virtualAnchor(m, messages, cache, top, viewport))
			prefix := virtualPrefix(messageHeights(m, messages, cache.heights))
			above, below := layout.desired-42-prefix[layout.start], prefix[layout.end]-(layout.desired-42+viewport)
			if above < viewport || below < viewport {
				t.Errorf("viewport %v at %v: %v px drawn above and %v px below, want a viewport each (%#v)", viewport, top, above, below, layout)
			}
			if layout.end-layout.start > virtualTimelineMaxRows {
				t.Errorf("viewport %v: %d rows drawn", viewport, layout.end-layout.start)
			}
		}
	}
}

// Scrolling inside the margin leaves the window as it is, row for row, and
// asks for no render, although the first row in view changes many times.
func TestIssue3827ScrollInsideOverscanKeepsTheWindow(t *testing.T) {
	m, messages, cache := issue3827Timeline()
	const viewport, origin = 623.0, 12000.0
	old := virtualAnchor(m, messages, cache, origin, viewport)
	drawn := virtualWindow(m, messages, cache, old)
	cache.remember(messages, drawn)
	anchors := map[string]bool{old.anchor: true}
	travel := (virtualOverscanFill - virtualOverscanKeep) * viewport
	for _, direction := range []float64{-1, 1} {
		for moved := 40.0; moved < travel-80; moved += 40 {
			next := virtualAnchor(m, messages, cache, origin+direction*moved, viewport)
			anchors[next.anchor] = true
			wanted := virtualWindow(m, messages, cache, next)
			if wanted.start != drawn.start || wanted.end != drawn.end {
				t.Fatalf("scrolled %v px: window %d..%d became %d..%d", direction*moved, drawn.start, drawn.end, wanted.start, wanted.end)
			}
			if virtualScrollNeedsRender(old, next, drawn, wanted) {
				t.Fatalf("scrolled %v px inside the drawn window: a render was asked for", direction*moved)
			}
			old = next
		}
	}
	if len(anchors) < 4 {
		t.Fatalf("the scroll crossed %d message boundaries; the test needs several", len(anchors))
	}
	// Past the margin the window moves, and the rows it changes are out of sight.
	far := virtualAnchor(m, messages, cache, origin+3*viewport, viewport)
	moved := virtualWindow(m, messages, cache, far)
	if moved.start == drawn.start || !virtualScrollNeedsRender(old, far, drawn, moved) {
		t.Fatalf("three viewports on, the window is still %d..%d", moved.start, moved.end)
	}
}

// Earlier history arriving above keeps the window on the same rows.
func TestIssue3827WindowFollowsItsRowsWhenHistoryIsPrepended(t *testing.T) {
	m, messages, cache := issue3827Timeline()
	position := virtualAnchor(m, messages, cache, 12000, 623)
	drawn := virtualWindow(m, messages, cache, position)
	cache.remember(messages, drawn)
	older := virtualMessages(30)
	for i := range older {
		older[i].ID = "earlier-" + older[i].ID
		older[i].SentAt = messages[0].SentAt.Add(-1e9 * 60 * 40)
	}
	all := append(older, messages...)
	m.Messages = all
	after := virtualWindow(m, all, cache, position)
	if all[after.start].ID != messages[drawn.start].ID || all[after.end-1].ID != messages[drawn.end-1].ID {
		t.Fatalf("window moved off its rows: %q..%q, was %q..%q", all[after.start].ID, all[after.end-1].ID, messages[drawn.start].ID, messages[drawn.end-1].ID)
	}
}

// The list's own height changing keeps the reader's current place, not the
// place of the render that started the observer.
func TestIssue3827ResizeKeepsTheCurrentPlace(t *testing.T) {
	current := virtualPosition{room: "room", anchor: "post-0200", offset: 12, top: 18000, height: 623}
	got := virtualResized(current, virtualPosition{room: "room", anchor: "post-0150", top: 13000, height: 700})
	if got.anchor != "post-0200" || got.top != 18000 || got.height != 700 {
		t.Fatalf("resized position = %#v", got)
	}
	if first := virtualResized(virtualPosition{bottom: true}, virtualPosition{room: "room", height: 700}); first.room != "room" || first.height != 700 {
		t.Fatalf("first position = %#v", first)
	}
}

// A portrait that failed is not drawn again; one that loaded, or that nobody
// has tried, is.
func TestIssue3827FailedPortraitIsRemembered(t *testing.T) {
	const failed, fine = "/workspace/assets/issue3827-missing.jpg", "/workspace/assets/issue3827-fine.jpg"
	t.Cleanup(func() { chatPhotoResolved(failed, true) })
	chatPhotoResolved(fine, true)
	if chatPhotoURL(" "+failed+" ") != failed || chatPhotoURL(fine) != fine {
		t.Fatal("an address nobody has seen fail was withheld")
	}
	chatPhotoResolved(failed, false)
	if got := chatPhotoURL(failed); got != "" {
		t.Fatalf("failed portrait is drawn again: %q", got)
	}
	if chatPhotoURL(fine) != fine || chatPhotoURL("") != "" {
		t.Fatal("the failure of one address changed another")
	}
	markup, err := ui.RenderToString(personAvatarWithPhoto("Rafael Torres", "avatar", chatPhotoURL(failed)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "chat-avatar-photo") || !strings.Contains(markup, "RT") {
		t.Fatalf("avatar of a failed portrait = %s", markup)
	}
}

// While the timeline scrolls under a resting mouse the hovered row is not
// renamed: that state is the workspace's, and each change draws the composer.
func TestIssue3827ScrollDoesNotRenameTheHoveredRow(t *testing.T) {
	var gate chatPointerGate
	if gate.wait(0) != 0 {
		t.Fatal("a pointer move with no scroll before it has to wait")
	}
	workspaceRenders := 0
	hover := func(now float64) {
		if gate.wait(now) == 0 {
			workspaceRenders++
		}
	}
	// Thirty wheel ticks 120 ms apart; after each the browser reports the row
	// now under the pointer.
	now := 1000.0
	for tick := 0; tick < 30; tick++ {
		gate.scroll(now)
		hover(now + 5)
		hover(now + 60)
		now += 120
	}
	if workspaceRenders != 0 {
		t.Fatalf("the scroll renamed the hovered row %d times", workspaceRenders)
	}
	last := now - 120
	if left := gate.wait(last + 50); left != chatScrollSettleMs-50 {
		t.Fatalf("wait 50 ms after the last scroll = %v", left)
	}
	hover(last + chatScrollSettleMs)
	if workspaceRenders != 1 {
		t.Fatalf("the settled scroll named the hovered row %d times, want once", workspaceRenders)
	}
}

// The Send button is written only when its state is not the one wanted.
func TestIssue3827SendButtonIsWrittenOnlyWhenItChanges(t *testing.T) {
	for _, c := range []struct {
		disabled    bool
		aria        string
		ready, want bool
	}{
		{true, "true", false, false},
		{false, "false", true, false},
		{true, "true", true, true},
		{false, "false", false, true},
		{true, "", false, true},
		{false, "true", true, true},
	} {
		if got := chatSendButtonStale(c.disabled, c.aria, c.ready); got != c.want {
			t.Errorf("disabled=%v aria=%q ready=%v: write=%v, want %v", c.disabled, c.aria, c.ready, got, c.want)
		}
	}
}

func issue3827Rows(layout virtualLayout, messages []Message) map[string]bool {
	rows := map[string]bool{}
	for _, msg := range messages[layout.start:layout.end] {
		rows[msg.ID] = true
	}
	return rows
}

// A window is drawn three viewports out on each side of the rows in view.
func TestIssue3827WindowReachesThreeViewportsEachSide(t *testing.T) {
	m, messages, cache := issue3827Timeline()
	const viewport = 623.0
	layout := virtualWindow(m, messages, cache, virtualAnchor(m, messages, cache, 12000, viewport))
	prefix := virtualPrefix(messageHeights(m, messages, cache.heights))
	above, below := layout.desired-42-prefix[layout.start], prefix[layout.end]-(layout.desired-42+viewport)
	if above < 3*viewport || below < 3*viewport {
		t.Fatalf("%v px drawn above and %v px below, want three viewports each (%#v)", above, below, layout)
	}
}

// Scrolling back and forth over a few screens creates no row at all, and a
// move of the window keeps every row it still covers (only the far edges change).
func TestIssue3827ScrollBackAndForthCreatesNoRows(t *testing.T) {
	m, messages, cache := issue3827Timeline()
	const viewport, origin = 623.0, 12000.0
	drawn := virtualWindow(m, messages, cache, virtualAnchor(m, messages, cache, origin, viewport))
	cache.remember(messages, drawn)
	mounted := issue3827Rows(drawn, messages)
	created, removed := 0, 0
	step := func(top float64) {
		layout := virtualWindow(m, messages, cache, virtualAnchor(m, messages, cache, top, viewport))
		cache.remember(messages, layout)
		now := issue3827Rows(layout, messages)
		for id := range now {
			if !mounted[id] {
				created++
			}
		}
		for id := range mounted {
			if !now[id] {
				removed++
			}
		}
		mounted = now
	}
	// 30 wheel ticks of 400 px one way and 30 back is 12000 px; two screens
	// either way (2 x 623 px) is what an ordinary look around covers.
	for tick := 1; tick <= 3; tick++ {
		step(origin - float64(tick)*400)
	}
	for tick := 3; tick >= 0; tick-- {
		step(origin - float64(tick)*400)
	}
	for tick := 1; tick <= 3; tick++ {
		step(origin + float64(tick)*400)
	}
	if created != 0 || removed != 0 {
		t.Fatalf("scrolling about two screens created %d and removed %d rows", created, removed)
	}
	// A longer scroll (past the margin) moves the window; rows in both windows stay.
	before := mounted
	step(origin + 2000)
	kept := 0
	for id := range mounted {
		if before[id] {
			kept++
		}
	}
	if created == 0 {
		t.Fatal("a 2000 px scroll did not move the window")
	}
	if len(mounted) > virtualTimelineMaxRows {
		t.Fatalf("%d rows mounted", len(mounted))
	}
	if kept == 0 {
		t.Fatal("the moved window kept none of the rows it covered before")
	}
}

// The command list's data-open number follows the list's own state, not every
// change of local state (a scroll names the hovered row, which is local state).
func TestIssue3827CommandMenuNumberIgnoresOtherLocalChanges(t *testing.T) {
	box := &localUI{}
	store := localStore{box: box, tick: ui.UseState(uint64(0))}
	store.update(func(u *localUI) { u.commandMenu = composerCommandMenu{Target: "chat-composer", Open: true} })
	first := box.menuSeq
	for i := 0; i < 13; i++ {
		store.update(func(u *localUI) { u.pointerRow = "row-" + itoa(i) })
	}
	if box.menuSeq != first || box.seq <= first {
		t.Fatalf("menuSeq %d after pointer changes, was %d (seq %d)", box.menuSeq, first, box.seq)
	}
	store.update(func(u *localUI) { u.commandMenu.Query = "po" })
	if box.menuSeq != box.seq {
		t.Fatalf("a change of the list did not move its number: %d, seq %d", box.menuSeq, box.seq)
	}
}
