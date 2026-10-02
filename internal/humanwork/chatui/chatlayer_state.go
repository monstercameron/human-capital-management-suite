package chatui

import "strings"

// This file is the client's layer state, kept free of the browser so that open,
// close, Escape order, focus return and pressed state are decided in one place
// and tested without one. The wasm half (agentux_chat5_layers_js.go) only reads
// and writes the DOM around these answers.

// chatSidebarSettingsGroup is the group of the sidebar's panels: Chat
// preferences and the Channels menu (CHATUX-002), and the two panels that were
// footer rows before it (Quiet hours and Reading languages). One of a group is
// open at a time.
const chatSidebarSettingsGroup = "sidebar-settings"

// chatDialogSelector matches the page-level dialogs (Create conversation,
// Browse, Add members, Share, Join, an agent's profile). Anchored panels are
// drawn in the top layer, above a dialog's scrim, so they close while one is open.
const chatDialogSelector = ".chat-dialog"

// chatLayerGroup names the set of layer kinds that exclude each other. A kind
// outside any shared group is its own group.
func chatLayerGroup(kind string) string {
	switch kind {
	case "quiet-hours", "reading-languages", chatux002PrefsKind, chatux002ChannelsKind:
		return chatSidebarSettingsGroup
	}
	return kind
}

// chatLayersToClose lists, by index into open, the layers that must close when
// a layer of kind opening opens. open holds the kind of every layer that is open
// now, with "" for the layer being opened (it is never closed by itself).
func chatLayersToClose(open []string, opening string) []int {
	var closing []int
	for i, kind := range open {
		if kind != "" && chatLayerGroup(kind) == chatLayerGroup(opening) {
			closing = append(closing, i)
		}
	}
	return closing
}

// chatLayerOutsideDismisses reports whether a click that lands outside the layer
// closes it. Sidebar settings panels are plain popovers with nothing else to
// close them; menus, pickers and trays are closed by the state that owns them.
func chatLayerOutsideDismisses(kind string) bool {
	// The formatting tools hold nothing the person typed, so a press elsewhere
	// closes them as it closes a sidebar panel (AGENTUX-062).
	// The Add menu holds a list of things to add and nothing typed either.
	return chatLayerGroup(kind) == chatSidebarSettingsGroup || kind == "formatting" || kind == chatComposerAddKind
}

// chatLayerWidth is the width a layer asks for before the viewport limits it.
func chatLayerWidth(kind string, viewportWidth float64) float64 {
	want := 360.0
	switch kind {
	case "poll":
		want = 400
	case chatComposerAddKind:
		want = 340
	case "gif":
		want = 420
	case "rail-menu":
		// The conversation's menu is a narrow list of commands.
		want = 200
	}
	return min(want, max(0, viewportWidth-16))
}

// chatLayerWritesGeneralWidth reports whether a layer is given the general
// width before it is placed. A sidebar panel that has an anchor is not: its
// placement gives it the sidebar's narrower width, and writing the general width
// first laid its text out wider and shorter for one measurement, which threw away
// how far the person had scrolled inside it (the Save button of Reading languages
// could not be reached).
func chatLayerWritesGeneralWidth(kind string, anchored bool) bool {
	return !anchored || chatLayerGroup(kind) != chatSidebarSettingsGroup
}

// chatLayerContentHeight is the height a layer needs to show everything it
// holds: the scrolled content plus its border, which scrollHeight leaves out.
// Capping a layer at scrollHeight alone is two pixels short and gives a
// three-row list a scrollbar.
func chatLayerContentHeight(scrollHeight, offsetHeight, clientHeight float64) float64 {
	return scrollHeight + max(0, offsetHeight-clientHeight)
}

// chatLayerPlacement is where a layer is drawn. A layer that opens upward is
// pinned by its bottom edge and one that opens downward by its top edge, and
// neither has a fixed height: content that arrives after the layer opened
// (a list that finished loading) grows it until maxHeight, then scrolls.
type chatLayerPlacement struct {
	left, width float64
	up          bool
	// top is the top edge of a downward layer; bottom is the distance from the
	// bottom of the viewport to the bottom edge of an upward one.
	top, bottom float64
	maxHeight   float64
}

// chatLayerPlace turns the side choice of anchoredChatGeometry into a
// placement that does not depend on the content's height staying what it was
// when the layer was measured.
func chatLayerPlace(g chatLayerGeometry, anchor chatLayerRect, viewportHeight float64) chatLayerPlacement {
	p := chatLayerPlacement{left: g.left, width: g.width}
	if g.top < anchor.top {
		edge := g.top + g.height
		p.up, p.bottom, p.maxHeight = true, max(0, viewportHeight-edge), max(0, edge-8)
		return p
	}
	p.top, p.maxHeight = g.top, max(0, viewportHeight-g.top-12)
	return p
}

// chatLayerPlaceWithin keeps a placement inside bounds as its content grows:
// a layer that opens upward stops at the top of bounds and one that opens
// downward at the bottom, and it scrolls from there (CHATBUG-051).
func chatLayerPlaceWithin(p chatLayerPlacement, bounds chatLayerRect, viewportHeight float64) chatLayerPlacement {
	if p.up {
		p.maxHeight = max(0, viewportHeight-p.bottom-bounds.top-8)
		return p
	}
	p.maxHeight = max(0, bounds.bottom-p.top-12)
	return p
}

// chatSidebarLayerPlacement puts a sidebar panel next to its opener and inside
// the sidebar column, however wide the layer would like to be: directly above a
// row in the lower half of the viewport, directly below one in the upper half
// (the controls in the list's headings).
func chatSidebarLayerPlacement(row, column chatLayerRect, viewportWidth, viewportHeight float64, rtl bool) chatLayerPlacement {
	const gap = 6
	columnWidth := column.right - column.left
	width := min(360, columnWidth-16, viewportWidth-16)
	width = max(width, min(220, viewportWidth-16))
	left := column.left + 8
	if rtl {
		left = column.right - 8 - width
	}
	left = max(8, min(left, viewportWidth-width-8))
	if (row.top+row.bottom)/2 < viewportHeight/2 {
		top := row.bottom + gap
		return chatLayerPlacement{left: left, width: width, top: top, maxHeight: max(0, viewportHeight-top-12)}
	}
	edge := row.top - gap
	return chatLayerPlacement{left: left, width: width, up: true, bottom: max(0, viewportHeight-edge), maxHeight: max(0, edge-8)}
}

// chatFocusHolder describes what holds keyboard focus when a deferred focus
// restore is about to run.
type chatFocusHolder struct {
	// Idle is true when nothing but the page itself has focus.
	Idle bool
	// Connected is false when the focused element has left the page.
	Connected bool
}

// chatFocusRestoreAllowed: focus goes back to a dialog's or layer's opener only
// if nobody has taken it since. The restore waits for a frame, and a person who
// clicked the composer in the meantime must keep the composer; the old restore
// took focus back and the click was lost.
func chatFocusRestoreAllowed(h chatFocusHolder) bool { return h.Idle || !h.Connected }

// chatEscapeState is everything Escape needs to choose what to close.
type chatEscapeState struct {
	// TopLayer is the kind of the topmost open anchored layer, "" if none.
	TopLayer string
	// Tray is the open channel tray ("todo", "poll") or "".
	Tray                                                 string
	SectionCreate, Search, Share, RailMenu, Picker, Menu bool
	Create, Browse, AddMembers, Person, Thread, Details  bool
	SearchText, Editing, Sidebar                         bool
}

// chatEscapeStep names the one thing Escape closes, topmost first, or "" when
// there is nothing to close. A layer that is not the topmost anchored layer
// waits its turn: a reaction picker behind an open to-do list stays open.
func chatEscapeStep(s chatEscapeState) string {
	top := s.TopLayer
	switch {
	case s.SectionCreate && top == "":
		return "section-create"
	case s.Search && (top == "search" || top == ""):
		return "search"
	case s.Share:
		return "share"
	case s.RailMenu:
		return "rail-menu"
	case s.Picker && (top == "reaction" || top == ""):
		return "reaction"
	case s.Menu && (top == "menu" || top == ""):
		return "menu"
	case s.Tray != "" && (top == s.Tray || top == ""):
		return "tray"
	case s.Create:
		return "create"
	case s.Browse:
		return "browse"
	case s.AddMembers:
		return "add-members"
	case s.Person:
		return "person"
	case s.Thread:
		return "thread"
	case s.Details:
		return "details"
	case s.SearchText:
		return "search-text"
	case s.Editing:
		return "edit"
	case s.Sidebar:
		return "rail"
	}
	return ""
}

// chatTrayToggle is the tray after one of the header or chip buttons is
// pressed: the pressed one opens, replacing the other, and pressing the open
// one closes it. The header buttons are drawn pressed exactly while this is
// their tray.
func chatTrayToggle(current, action string) (next string, opening bool) {
	which := "todo"
	if strings.HasSuffix(action, "poll") {
		which = "poll"
	}
	if current == which {
		return "", false
	}
	return which, true
}

// ClearThreadOverlays closes the reaction picker and the message menu that
// belong to the thread pane. They are anchored to the pane's messages, so
// closing the pane while one is open used to leave it drawn over the page with
// nothing to anchor to.
func ClearThreadOverlays(m *Model) {
	if m == nil {
		return
	}
	inThread := func(id string) bool {
		id = strings.TrimPrefix(id, "thread:")
		if id == "" {
			return false
		}
		for _, reply := range m.ThreadMessages {
			if reply.ID == id {
				return true
			}
		}
		return false
	}
	if inThread(m.PickerID) {
		m.PickerID = ""
	}
	if strings.HasPrefix(m.MenuID, "thread:") || inThread(m.MenuID) {
		m.MenuID = ""
	}
}
