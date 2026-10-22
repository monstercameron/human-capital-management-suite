package chatui

import (
	"os"
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"
)

// chatux040Menuitems are the items of a rendered menu.
func chatux040Menuitems(t *testing.T, markup string) []*xhtml.Node {
	t.Helper()
	return chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
		role := chatPolishAttr(n, "role")
		return n.Data == "button" && (role == "menuitem" || role == "menuitemradio")
	})
}

// chatux040LeadsWithGlyph is true when an item's first element is an icon, or the
// check column of the notification choices.
func chatux040LeadsWithGlyph(n *xhtml.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xhtml.ElementNode {
			continue
		}
		return chatPolishHasClass(c, "chat-icon") || chatPolishHasClass(c, "rail-menu-check")
	}
	return false
}

// TestTodo_CHATUX_040: the sidebar's menus, selected rows and Saved. A row or
// section menu is as wide as its longest item (240 to 320 px) and stays below
// the sidebar's head; every item leads with a glyph; one selected-row look that
// is stronger than hover and one text column; Saved's counts add up and say so,
// and a day word inside a sentence is lower case.
func TestTodo_CHATUX_040(t *testing.T) {
	t.Run("menu width and place", func(t *testing.T) {
		for _, tc := range []struct{ natural, viewport, want float64 }{
			{150, 1280, 240}, {240, 1280, 240}, {281, 1280, 281}, {320, 1280, 320}, {900, 1280, 320}, {900, 300, 284},
		} {
			if got := chatux040RailMenuFit(tc.natural, tc.viewport); got != tc.want {
				t.Errorf("a menu asking %.0f px in a %.0f px window is %.0f px wide, want %.0f", tc.natural, tc.viewport, got, tc.want)
			}
		}
		rail, fixed := chatLayerRect{left: 270, top: 82, right: 558, bottom: 900}, chatLayerRect{left: 278, top: 186, right: 550, bottom: 250}
		bounds := chatux040RailMenuBounds(chatLayerRect{0, 0, 1280, 900}, rail, fixed, 1280, false)
		if bounds.top != 186 || bounds.left != 270 || bounds.right != 1280 {
			t.Errorf("the menu's room starts under the sidebar head and runs from the sidebar to the page's far edge: %+v", bounds)
		}
		if rtl := chatux040RailMenuBounds(chatLayerRect{0, 0, 1280, 900}, chatLayerRect{left: 722, top: 82, right: 1010, bottom: 900}, fixed, 1280, true); rtl.left != 0 || rtl.right != 1010 || rtl.top != 186 {
			t.Errorf("in a right-to-left page the room runs from the page's far edge to the sidebar: %+v", rtl)
		}
		// A menu from the first rows of a long list opens under the head, never over it.
		button := chatLayerRect{left: 520, top: 290, right: 546, bottom: 318}
		g := chatLayerPlaceWithin(chatLayerPlace(anchoredChatGeometryIn(button, bounds, 300, 560, false, false), button, 900), bounds, 900)
		if g.top < fixed.top || g.left < rail.left || g.left+g.width < button.right {
			t.Errorf("the menu covers the sidebar head or leaves its button: %+v", g)
		}
		if unmeasured := chatux040RailMenuBounds(chatLayerRect{0, 0, 1280, 900}, chatLayerRect{}, chatLayerRect{}, 1280, false); unmeasured != (chatLayerRect{0, 0, 1280, 900}) {
			t.Errorf("a sidebar that was not measured changes the room: %+v", unmeasured)
		}
	})

	t.Run("a menu in a right-to-left page is placed by its left edge", func(t *testing.T) {
		// inset-inline-end is the left edge in a right-to-left page: setting it to
		// auto released the left edge the placement had just written, and the menu
		// sat at the page's right edge instead of beside its button.
		raw, err := os.ReadFile("agentux_chat5_layers_js.go")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `"inset-inline-end"`) || !strings.Contains(string(raw), `style.Call("setProperty", "right", "auto")`) {
			t.Error("the layer placement releases the logical end edge instead of the physical right edge")
		}
		if !strings.Contains(string(raw), "chatux040RailMenuLayout(") {
			t.Error("the row and section menus are not measured at the width of their longest item")
		}
	})

	t.Run("every item has a glyph", func(t *testing.T) {
		m := chatux020Model()
		m.Preferences.Notifications = map[string]NotificationMode{}
		m.Callbacks.MoveConversationOrder = func(string, int) {}
		for _, id := range []string{"random", "general", "quiet", "sales", "dm-loretta"} {
			m.RailMenuID = id
			items := chatux040Menuitems(t, renderNode(t, chatux020RailMenu(m, handlers{})))
			if len(items) < 6 {
				t.Fatalf("%s: only %d menu items", id, len(items))
			}
			for _, item := range items {
				if !chatux040LeadsWithGlyph(item) {
					t.Errorf("%s: an item has no glyph: %s", id, chatPolishAttr(item, "data-action"))
				}
			}
		}
		for _, id := range []string{"channels", "custom-1"} {
			m.RailMenuID = chatside001MenuPrefix + id
			items := chatux040Menuitems(t, renderNode(t, chatux020RailMenu(m, handlers{})))
			if len(items) == 0 {
				t.Fatalf("section %s has no menu", id)
			}
			for _, item := range items {
				if !chatux040LeadsWithGlyph(item) {
					t.Errorf("section %s: an item has no glyph: %s", id, chatPolishAttr(item, "data-action"))
				}
			}
		}
	})

	t.Run("one selected look, stronger than hover, one text column", func(t *testing.T) {
		for _, want := range []string{
			`--rail-selected:color-mix(in srgb,var(--accent) 16%,var(--canvas))`,
			`--rail-hover:color-mix(in srgb,var(--ink) 5%,transparent)`,
			`.chat-row.selected,`,
			`background:var(--rail-selected);color:var(--ink);font-weight:600;box-shadow:inset 3px 0 0 var(--accent)`,
			`:where(:not(.selected):not([aria-expanded="true"])):hover{background:var(--rail-hover)`,
			`inline-size:24px;block-size:24px`,
			`inline-size:max-content;min-inline-size:240px;max-inline-size:min(320px,calc(100vw - 16px))`,
		} {
			if !strings.Contains(ChatUX040Styles, want) {
				t.Errorf("the sidebar styles miss %q", want)
			}
		}
		if !strings.Contains(Stylesheet, ChatUX040Styles) || !strings.Contains(Stylesheet, ChatUX040SavedStyles) {
			t.Error("the sidebar and Saved styles are not in the stylesheet")
		}
		// Saved and Moderation draw the same tint and bar, not a stronger fill; while
		// Saved is open the conversation behind it keeps a quiet mark of its own.
		for _, want := range []string{
			`.chatsave-sidebar-row[aria-expanded="true"]`,
			`.chatmod005-row`,
			`background:var(--rail-selected);color:var(--ink);font-weight:600;box-shadow:inset 3px 0 0 var(--accent)`,
			`:has(.chatsave-panel:not([hidden])) .chat-rail .chat-row.selected:not(.chatsave-sidebar-row)`,
			`box-shadow:inset 3px 0 0 color-mix(in srgb,var(--ink) 22%,transparent)`,
		} {
			if !strings.Contains(ChatBug091Styles, want) {
				t.Errorf("Saved and Moderation do not draw the conversation's selected look: %q is missing", want)
			}
		}
		if strings.Contains(ChatBug091Styles, "accent) 28%") {
			t.Error("Saved and Moderation still draw a stronger fill than a conversation")
		}
	})

	t.Run("Saved counts add up and day words are lower case", func(t *testing.T) {
		if got := chatux040AllNote("en-US", 0, 2, 3); !strings.Contains(got, "1 saved message whose original was deleted") {
			t.Errorf("All 3 beside To do 0 and Done 2 is not explained: %q", got)
		}
		if got := chatux040AllNote("en-US", 1, 1, 4); !strings.Contains(got, "2 saved messages") {
			t.Errorf("two deleted: %q", got)
		}
		if got := chatux040AllNote("en-US", 1, 2, 3); got != "" {
			t.Errorf("counts that add up are explained: %q", got)
		}
		for locale, want := range map[string]string{"en-US": "yesterday", "de-DE": "gestern", "ar": "أمس"} {
			if got := chatux040DoneWhen(locale, chatsave002Text(locale, "yesterday")); got != want {
				t.Errorf("%s: %q, want %q", locale, got, want)
			}
		}
		if got := chatux040DoneWhen("en-US", "Sep 28"); got != "Sep 28" {
			t.Errorf("a date lost its capital: %q", got)
		}

		rows := chatsave002Rows()
		rows[2].DoneAt = chatsave002Now.Add(-24 * time.Hour)
		view := chatsave002View("all", rows)
		view.AllCount = 4
		all := renderNode(t, RenderSavedMessages(view))
		for _, want := range []string{"Done yesterday", "All also holds 1 saved message whose original was deleted.", `data-saved-all-note="true"`} {
			if !strings.Contains(all, want) {
				t.Errorf("the All tab misses %q", want)
			}
		}
		if strings.Contains(all, "Done Yesterday") {
			t.Error("Yesterday is capitalised inside a sentence")
		}
		view.Tab = "done"
		if done := renderNode(t, RenderSavedMessages(view)); strings.Contains(done, "chatsave-all-note") {
			t.Error("the note is on a tab that does not need it")
		}
		for _, want := range []string{"-webkit-line-clamp:4", `[data-clamped="true"]{-webkit-mask-image:none`, "flex:1 0 100%"} {
			if !strings.Contains(ChatUX040SavedStyles, want) {
				t.Errorf("the Saved styles miss %q", want)
			}
		}
	})
}
