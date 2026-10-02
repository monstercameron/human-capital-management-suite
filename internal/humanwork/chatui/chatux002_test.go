package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// chatux002Model is a signed-in reader whose page can save preferences and open
// the dialogs the Channels menu leads to.
func chatux002Model(locale string) Model {
	m := chat4Fixture(locale, "sent", false)
	m.Callbacks.SavePreferences = func(Preferences) {}
	m.Callbacks.OpenBrowse = func() {}
	m.Callbacks.OpenCreate = func() {}
	m.Callbacks.CreateSection = func(string) {}
	m.Callbacks.ToggleSection = func(string) {}
	return m
}

func chatux002NodeText(n *xhtml.Node) string {
	var out strings.Builder
	var walk func(*xhtml.Node)
	walk = func(c *xhtml.Node) {
		if c.Type == xhtml.TextNode {
			out.WriteString(c.Data)
		}
		for k := c.FirstChild; k != nil; k = k.NextSibling {
			walk(k)
		}
	}
	walk(n)
	return strings.TrimSpace(out.String())
}

func chatux002Find(t *testing.T, markup string, match func(*xhtml.Node) bool) []*xhtml.Node {
	t.Helper()
	return chatPolishNodes(t, markup, match)
}

func chatux002Layers(t *testing.T, markup, kind string) []*xhtml.Node {
	return chatux002Find(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-chat-layer") == kind })
}

func chatux002Inside(n *xhtml.Node, match func(*xhtml.Node) bool) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == xhtml.ElementNode && match(p) {
			return true
		}
	}
	return false
}

func TestTodo_CHATUX_002(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		m = chatux002Model(m.Locale)
		markup := chatPolishMarkup(t, rail(m, handlers{}), width, theme)

		// One Chat preferences control, a gear, in the Conversations heading beside
		// the new-conversation control.
		label := chatux002Text(m, keyChatux002Prefs)
		gears := chatux002Find(t, markup, func(n *xhtml.Node) bool { return n.Data == "button" && chatPolishHasClass(n, "chatux002-gear") })
		if len(gears) != 1 {
			t.Fatalf("%d Chat preferences controls, want one", len(gears))
		}
		gear := gears[0]
		if !chatPolishAncestor(gear, "rail-head-actions") || !chatPolishAncestor(gear, "rail-head") {
			t.Fatal("the gear is not in the Conversations heading")
		}
		if chatPolishAttr(gear, "title") != label || chatPolishAttr(gear, "aria-label") != label || label == "" || strings.HasPrefix(label, "chat.") {
			t.Fatalf("gear tooltip %q, accessible name %q, want %q", chatPolishAttr(gear, "title"), chatPolishAttr(gear, "aria-label"), label)
		}
		if gear.FirstChild == nil || !strings.Contains(chatPolishAttr(gear.FirstChild, "class"), "icon-settings") {
			t.Fatal("the control is not a gear")
		}
		create := chatux002Find(t, markup, func(n *xhtml.Node) bool {
			return chatPolishAttr(n, "data-action") == "open-create" && chatPolishAncestor(n, "rail-head-actions")
		})
		if len(create) != 1 || !chatPolishHasClass(create[0].Parent, "rail-head-actions") || !chatPolishHasClass(gear.Parent.Parent, "rail-head-actions") {
			t.Fatalf("the gear does not sit beside the new-conversation control: %d", len(create))
		}

		// One panel holds Quiet hours and Reading languages, each under a plain
		// heading with its current value.
		panels := chatux002Layers(t, markup, chatux002PrefsKind)
		if len(panels) != 1 {
			t.Fatalf("%d preference panels, want one", len(panels))
		}
		if chatux002Layers(t, markup, "quiet-hours") != nil || chatux002Layers(t, markup, "reading-languages") != nil {
			t.Fatal("Quiet hours or Reading languages still has a panel of its own")
		}
		sections := chatux002Find(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-prefs-section") != "" })
		got := []string{}
		for _, section := range sections {
			if !chatux002Inside(section, func(p *xhtml.Node) bool { return chatPolishAttr(p, "data-chat-layer") == chatux002PrefsKind }) {
				t.Fatalf("section %s is outside the panel", chatPolishAttr(section, "data-prefs-section"))
			}
			heads := chatux002Descend(section, func(n *xhtml.Node) bool { return n.Data == "h3" && chatPolishHasClass(n, "chat-prefs-title") })
			values := chatux002Descend(section, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "chat-prefs-value") })
			if len(heads) != 1 || len(values) != 1 {
				t.Fatalf("%s: %d headings and %d values, want one each", chatPolishAttr(section, "data-prefs-section"), len(heads), len(values))
			}
			got = append(got, chatPolishAttr(section, "data-prefs-section")+"="+chatux002NodeText(heads[0])+"|"+chatux002NodeText(values[0]))
		}
		wantQuiet := "quiet-hours=" + m.t(KeyQuietHours) + "|" + m.t(KeyOff)
		// CHATBUG-045 changed this assertion: the row's title is "Reading language".
		wantReading := "reading-languages=" + chatbug045Text(m, keyChatbug045Reading) + "|" + RenderingText(m.Locale, chatrenderLanguage(m.Locale))
		if len(got) != 2 || got[0] != wantQuiet || got[1] != wantReading {
			t.Fatalf("preference sections = %q, want [%q %q]", got, wantQuiet, wantReading)
		}
		// The existing controls moved whole: same ids, same behaviour hooks.
		chat4Require(t, markup, `id="quiet-hours"`, `id="quiet-start"`, `id="quiet-end"`, `id="quiet-timezone"`, `id="chatrender-reading"`)

		// The language service is not available: no Reading languages section.
		without := m
		without.ChatFeatures = &ChatFeatures{Renderings: false}
		bare := chatPolishMarkup(t, rail(without, handlers{}), width, theme)
		if strings.Contains(bare, `data-prefs-section="reading-languages"`) || strings.Contains(bare, `id="chatrender-reading"`) {
			t.Fatal("a Reading languages section is drawn without the language service")
		}
		chat4Require(t, bare, `data-prefs-section="quiet-hours"`)

		// Quiet hours on: one small moon with a tooltip beside the heading; off: none.
		if strings.Contains(markup, "rail-quiet-moon") {
			t.Fatal("a moon is drawn while quiet hours are off")
		}
		on := m
		on.Preferences = Preferences{QuietHours: true, QuietTimezone: "UTC", QuietStartMinute: 22 * 60, QuietEndMinute: 7 * 60}
		moonMarkup := chatPolishMarkup(t, rail(on, handlers{}), width, theme)
		moons := chatux002Find(t, moonMarkup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "rail-quiet-moon") })
		if len(moons) != 1 || !chatPolishAncestor(moons[0], "rail-title-group") || chatPolishAttr(moons[0], "title") == "" || chatPolishAttr(moons[0], "title") != chatPolishAttr(moons[0], "aria-label") {
			t.Fatalf("%d moons, or the moon is not beside the heading with a tooltip", len(moons))
		}
		if title := chatPolishAttr(moons[0], "title"); strings.Contains(title, "{") || !strings.Contains(title, quietClock(on, 22*60)) {
			t.Fatalf("moon tooltip = %q", title)
		}

		// New section, Add channels and Browse channels are one menu on the Channels
		// heading; Direct messages has none.
		menus := chatux002Layers(t, markup, chatux002ChannelsKind)
		if len(menus) != 1 {
			t.Fatalf("%d channel menus, want one", len(menus))
		}
		host := chatux002Inside(menus[0], func(p *xhtml.Node) bool {
			return chatPolishHasClass(p, "section-controls") && chatux002Inside(p, func(s *xhtml.Node) bool { return chatPolishAttr(s, "data-section-id") == "channels" })
		})
		if !host {
			t.Fatal("the menu is not in the Channels heading")
		}
		items := map[string]string{}
		for _, n := range chatux002Find(t, markup, func(n *xhtml.Node) bool {
			return n.Data == "button" && chatux002Inside(n, func(p *xhtml.Node) bool { return chatPolishAttr(p, "data-chat-layer") == chatux002ChannelsKind })
		}) {
			if action := chatPolishAttr(n, "data-action"); action != "" && action != "cancel-section-create" {
				items[action] = chatux002NodeText(n)
			}
		}
		for action, name := range map[string]string{"open-create": chatux002Text(m, keyChatux002AddName), "open-browse": m.t(KeyBrowse), "open-section-create": m.t(KeyNewSection)} {
			if !strings.Contains(items[action], name) {
				t.Fatalf("menu item %s = %q, want it to name %q", action, items[action], name)
			}
		}
		chat4Require(t, markup, `id="chat-section-create"`, `id="chat-new-section"`)

		// The foot of the list holds nothing but conversations: no fixed row, no
		// footer, no Add channels row, and none of the three settings outside a panel.
		for _, gone := range []string{"rail-footer", "rail-link", "rail-add", "rail-prefs"} {
			if strings.Contains(markup, `"`+gone+`"`) || strings.Contains(markup, " "+gone+" ") || strings.Contains(markup, " "+gone+`"`) {
				t.Fatalf("the list still draws %s", gone)
			}
		}
		for _, word := range []string{m.t(KeyQuietHours), m.t(KeyNewSection), m.t(KeyBrowse), m.t(KeyAddChannels), RenderingText(m.Locale, "title")} {
			for _, n := range chatux002Find(t, markup, func(n *xhtml.Node) bool {
				return n.Type == xhtml.ElementNode && n.FirstChild != nil && n.FirstChild.Type == xhtml.TextNode && strings.TrimSpace(n.FirstChild.Data) == word
			}) {
				if !chatux002Inside(n, func(p *xhtml.Node) bool { return chatPolishAttr(p, "data-chat-layer") != "" }) {
					t.Fatalf("%q is drawn in the list itself", word)
				}
			}
		}
		navs := chatux002Find(t, markup, func(n *xhtml.Node) bool { return n.Data == "nav" })
		scroll := -1
		var after []string
		for child := navs[0].FirstChild; child != nil; child = child.NextSibling {
			if child.Type != xhtml.ElementNode {
				continue
			}
			if chatPolishHasClass(child, "rail-scroll") {
				scroll = 0
				continue
			}
			if scroll == 0 {
				after = append(after, chatPolishAttr(child, "class"))
			}
		}
		if scroll != 0 || len(after) > 1 {
			t.Fatalf("elements under the list: %q", after)
		}
	})
}

// chatrenderLanguage is the reading language a fresh page starts with.
func chatrenderLanguage(locale string) string {
	if i := strings.IndexAny(locale, "-_"); i >= 0 {
		locale = locale[:i]
	}
	return strings.ToLower(locale)
}

// chatux002Descend lists the elements under n that match.
func chatux002Descend(n *xhtml.Node, match func(*xhtml.Node) bool) []*xhtml.Node {
	var found []*xhtml.Node
	var walk func(*xhtml.Node)
	walk = func(c *xhtml.Node) {
		if c.Type == xhtml.ElementNode && match(c) {
			found = append(found, c)
		}
		for k := c.FirstChild; k != nil; k = k.NextSibling {
			walk(k)
		}
	}
	walk(n)
	return found
}

func TestTodo_CHATUX_002_Accessibility(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		m = chatux002Model(m.Locale)
		markup := chatPolishMarkup(t, rail(m, handlers{}), width, theme)

		for _, class := range []string{"chatux002-gear", "section-menu-trigger"} {
			buttons := chatux002Find(t, markup, func(n *xhtml.Node) bool { return n.Data == "button" && chatPolishHasClass(n, class) })
			if len(buttons) != 1 {
				t.Fatalf("%d %s buttons", len(buttons), class)
			}
			b := buttons[0]
			name := chatPolishAttr(b, "aria-label")
			if name == "" || name != chatPolishAttr(b, "title") || chatPolishAttr(b, "type") != "button" || chatPolishAttr(b, "aria-expanded") != "false" || chatPolishAttr(b, "aria-haspopup") != "dialog" || chatPolishAttr(b, "data-chat-disclosure-toggle") != "true" || hasChatPolishAttribute(b, "disabled") {
				t.Fatalf("%s is not an enabled, named, closed disclosure button: %v", class, b.Attr)
			}
			// A plain click opens it: the disclosure is the shared one, and its body is
			// a closed popover dialog that follows the button.
			body := b.NextSibling
			if body == nil || chatPolishAttr(body, "data-chat-disclosure-body") != "true" || !hasChatPolishAttribute(body, "hidden") || chatPolishAttr(body, "popover") != "manual" || chatPolishAttr(body, "role") != "dialog" || chatPolishAttr(body, "aria-label") != name {
				t.Fatalf("%s: the panel is not a labelled closed popover dialog", class)
			}
			if chatPolishAttr(b.Parent, "data-chat-disclosure") != "true" {
				t.Fatalf("%s is not inside a disclosure", class)
			}
		}

		// Every heading names its section; every control in the panel has a name.
		for _, section := range chatux002Find(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-prefs-section") != "" }) {
			id := chatPolishAttr(section, "aria-labelledby")
			heads := chatux002Find(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "id") == id && n.Data == "h3" })
			if id == "" || len(heads) != 1 {
				t.Fatalf("section %s is not named by a heading", chatPolishAttr(section, "data-prefs-section"))
			}
		}
		for _, n := range chatux002Find(t, markup, func(n *xhtml.Node) bool {
			return (n.Data == "input" || n.Data == "select") && chatux002Inside(n, func(p *xhtml.Node) bool { return chatPolishAttr(p, "data-chat-layer") == chatux002PrefsKind })
		}) {
			id := chatPolishAttr(n, "id")
			labelled := chatPolishAttr(n, "aria-label") != "" || len(chatux002Find(t, markup, func(l *xhtml.Node) bool { return l.Data == "label" && chatPolishAttr(l, "for") == id })) > 0 || chatux002Inside(n, func(p *xhtml.Node) bool { return p.Data == "label" })
			if !labelled {
				t.Fatalf("control %q in the preferences panel has no name", id)
			}
		}
		switches := chatux002Find(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "id") == "quiet-hours" })
		if len(switches) != 1 || chatPolishAttr(switches[0], "role") != "switch" || hasChatPolishAttribute(switches[0], "disabled") {
			t.Fatal("the quiet hours switch is not an enabled switch")
		}
		// A page that cannot save preferences says so instead of offering a dead switch.
		m.Callbacks.SavePreferences = nil
		unsaved := chatPolishMarkup(t, rail(m, handlers{}), width, theme)
		if !strings.Contains(unsaved, chatPolishUnavailable(m.Locale)) {
			t.Fatal("an unavailable quiet hours switch does not say why")
		}
		for _, n := range chatux002Find(t, unsaved, func(n *xhtml.Node) bool { return chatPolishAttr(n, "id") == "quiet-hours" }) {
			if !hasChatPolishAttribute(n, "disabled") {
				t.Fatal("quiet hours can be switched without a way to save it")
			}
		}

		// The moon is an image with a name, not decoration only.
		on := chatux002Model(m.Locale)
		on.Preferences.QuietHours = true
		moons := chatux002Find(t, chatPolishMarkup(t, rail(on, handlers{}), width, theme), func(n *xhtml.Node) bool { return chatPolishHasClass(n, "rail-quiet-moon") })
		if len(moons) != 1 || chatPolishAttr(moons[0], "role") != "img" || chatPolishAttr(moons[0], "aria-label") == "" {
			t.Fatal("the moon has no accessible name")
		}
	})

	// Escape and an outside click close both panels; one is open at a time; and
	// nothing waits for a timer, a frame or a focus event (the review pane has none).
	for _, kind := range []string{chatux002PrefsKind, chatux002ChannelsKind} {
		if !chatPolishManagedLayer(kind) || !chatLayerOutsideDismisses(kind) || chatLayerGroup(kind) != chatSidebarSettingsGroup {
			t.Fatalf("%s is not closed by Escape and an outside click as part of the sidebar group", kind)
		}
	}
	if got := chatLayersToClose([]string{chatux002ChannelsKind, ""}, chatux002PrefsKind); len(got) != 1 || got[0] != 0 {
		t.Fatalf("opening Chat preferences closes %v, want the Channels menu", got)
	}
	for _, f := range []struct{ file, name string }{
		{"chatux002_close_js.go", "chatux002CloseSidebarPanels"},
		{"chatux007_jump_js.go", "chatux007SyncJump"},
		{"chatux007_jump_js.go", "chatux007JumpTo"},
	} {
		body := chatbugFuncBody(t, f.file, f.name)
		for _, forbidden := range []string{"requestAnimationFrame", "setTimeout", "FocusEvent", `"focusout"`, `"blur"`} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s depends on %s", f.name, forbidden)
			}
		}
	}
}

func TestTodo_CHATUX_002_Placement(t *testing.T) {
	// The gear and the plus sit in headings at the top of the sidebar: their panels
	// open below them, inside the sidebar column, at every width and in both
	// directions. A row at the foot still opens its panel above it.
	for _, size := range [][2]float64{{1440, 900}, {800, 600}, {390, 844}, {320, 568}} {
		for _, rtl := range []bool{false, true} {
			width, height := size[0], size[1]
			col := chatLayerRect{0, 0, min(300, width), height}
			if rtl {
				col = chatLayerRect{width - min(300, width), 0, width, height}
			}
			opener := chatLayerRect{col.right - 80, 8, col.right - 48, 44}
			p := chatSidebarLayerPlacement(opener, col, width, height, rtl)
			if p.up || p.top < opener.bottom || p.left < col.left || p.left+p.width > col.right || p.left < 8 || p.left+p.width > width-8 || p.maxHeight <= 0 || p.top+p.maxHeight > height {
				t.Fatalf("%vx%v rtl=%v: the panel does not open below its heading control inside the column: %+v", width, height, rtl, p)
			}
			foot := chatLayerRect{col.left, height - 40, col.right, height - 6}
			if q := chatSidebarLayerPlacement(foot, col, width, height, rtl); !q.up {
				t.Fatalf("%vx%v: a row at the foot no longer opens upward", width, height)
			}
		}
	}
}
