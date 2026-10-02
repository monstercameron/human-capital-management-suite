package chatui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/emojiset"
)

// The picker is tested against the emoji data the product ships: the files under
// the workspace assets, read here from disk exactly as the server would send them.

func emojiAssetPathFor(name string) string {
	return filepath.Join("..", "workspace", "assets", name)
}

func emojiTestIndex(t testing.TB, reader string) *emojiset.Index {
	t.Helper()
	read := func(name string) []byte {
		data, err := os.ReadFile(emojiAssetPathFor(name))
		if err != nil {
			t.Fatalf("the emoji data is not in the workspace assets: %v", err)
		}
		return data
	}
	set, err := emojiset.ParseOrder(read("emoji-order.json"))
	if err != nil {
		t.Fatal(err)
	}
	english, err := emojiset.ParseLang(read("emoji-en.json"), set)
	if err != nil {
		t.Fatal(err)
	}
	lang, err := emojiset.ParseLang(read("emoji-"+reader+".json"), set)
	if err != nil {
		t.Fatal(err)
	}
	return emojiset.NewIndex(set, lang, english)
}

// withEmojiHost runs fn with the picker wired to a plain localUI, the data loaded
// for the reader's language, and no stored preferences; it restores the globals.
func withEmojiHost(t testing.TB, model Model, fn func(local *localUI)) {
	t.Helper()
	savedHost, savedData, savedInsert := chatEmojiHost, chatEmojiData, chatEmojiInsert
	defer func() { chatEmojiHost, chatEmojiData, chatEmojiInsert = savedHost, savedData, savedInsert }()
	local := &localUI{}
	chatEmojiHost = chatEmojiHostT{
		read:  func() localUI { return *local },
		apply: func(change func(*localUI)) { change(local) },
		quiet: func(change func(*localUI)) { change(local) },
		model: model,
	}
	lang := chatEmojiLang(model.Locale)
	chatEmojiData = chatEmojiDataStore{status: emojiDataReady, lang: lang, index: emojiTestIndex(t, lang)}
	chatEmojiInsert = nil
	fn(local)
}

func emojiMarkup(t *testing.T, m Model, st emojiPickerState, kind string) string {
	t.Helper()
	return renderNode(t, chatEmojiPicker(m, st, kind, chatEmojiData.index, chatEmojiData.status, chatEmojiHost.prefs))
}

func emojiNodes(t *testing.T, markup string, match func(*xhtml.Node) bool) []*xhtml.Node {
	return chatPolishNodes(t, markup, match)
}

func emojiHasClass(n *xhtml.Node, class string) bool { return chatPolishHasClass(n, class) }

func emojiText(n *xhtml.Node) string {
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// emojiCellGlyphs lists the glyphs of the cells drawn, in order.
func emojiCellGlyphs(t *testing.T, markup string) []string {
	var glyphs []string
	for _, n := range emojiNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "button" && emojiHasClass(n, "emoji-pop-btn") }) {
		glyphs = append(glyphs, chatPolishAttr(n, "data-emoji"))
	}
	return glyphs
}

func emojiModel(locale string) Model {
	return Model{State: StateReady, SelectedID: "room", Locale: locale, CurrentUser: "ari", CurrentTenantID: "tenant", Conversations: []Conversation{{ID: "room", Name: "People"}}}
}

// --- CHATEMOJI-001 on the rendered tree -------------------------------------------------

// A person finds an emoji by typing what it means, in their own language or in
// English; the best match is first and already highlighted; an empty query is the
// whole set; no result is one plain line that names the query.
func TestTodo_CHATEMOJI_001_Browser(t *testing.T) {
	for _, tc := range []struct {
		locale, query, want, name string
	}{
		{"en-US", "party", "🎉", "party popper"},
		{"en-US", ":thumbs_up:", "👍", "thumbs up"},
		{"en-US", "FIRE", "🔥", "fire"},
		{"de-DE", "Feuer", "🔥", "Feuer"},
		{"de-DE", "feuer", "🔥", "Feuer"},
		{"de-DE", "fire", "🔥", "Feuer"},
		{"de-DE", "Freudentranen", "😂", "Gesicht mit Freudentränen"},
		{"de-DE", "party", "🎉", "party popper"},
		{"ar", "نار", "🔥", "نار"},
		{"ar", "fire", "🔥", "نار"},
		{"ar", "منزل", "🏠", "منزل"},
	} {
		t.Run(tc.locale+"/"+tc.query, func(t *testing.T) {
			m := emojiModel(tc.locale)
			withEmojiHost(t, m, func(*localUI) {
				st := emojiQuery(emojiOpenState("chat-composer", false, false, false), tc.query)
				markup := emojiMarkup(t, m, st, "emoji")
				glyphs := emojiCellGlyphs(t, markup)
				if len(glyphs) == 0 || glyphs[0] != tc.want {
					t.Fatalf("searching %q drew %v, want %s first", tc.query, glyphs, tc.want)
				}
				active := emojiNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "div" && emojiHasClass(n, "active") })
				if len(active) != 1 || !strings.Contains(emojiText(active[0]), tc.want) {
					t.Fatalf("the best match is not the one highlighted: %d highlighted", len(active))
				}
				if !strings.Contains(markup, ">"+tc.name+"<") {
					t.Errorf("the footer does not name the best match %q in %s", tc.name, tc.locale)
				}
				if headings := emojiNodes(t, markup, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-pop-head") }); len(headings) != 0 {
					t.Errorf("a results list has %d headings", len(headings))
				}
			})
		})
	}

	m := emojiModel("en-US")
	withEmojiHost(t, m, func(*localUI) {
		// No result: one plain line naming the query, no cells, no stale highlight.
		st := emojiQuery(emojiOpenState("chat-composer", false, false, false), "  zzzzqq ")
		markup := emojiMarkup(t, m, st, "emoji")
		if len(emojiCellGlyphs(t, markup)) != 0 || !strings.Contains(markup, "No emoji match “zzzzqq”.") {
			t.Fatalf("no-result state: %s", markup)
		}
		notes := emojiNodes(t, markup, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-pop-note") })
		if len(notes) != 1 || chatPolishAttr(notes[0], "role") != "status" {
			t.Fatalf("%d notes", len(notes))
		}
		// An empty query is the full set: the frequently used row then every emoji in Unicode's order.
		full := buildEmojiView(emojiOpenState("chat-composer", false, false, false), chatEmojiData.index, emojiPrefs{})
		if want := len(emojiStarter) + len(chatEmojiData.index.Set.Entries); full.seq.len() != want {
			t.Fatalf("the empty query offers %d emoji, want the starter row and all %d", full.seq.len(), want-len(emojiStarter))
		}
		// A query that matched by a keyword the other language lacks still finds it.
		if got := chatEmojiData.index.Search("coffee", nil); len(got) == 0 {
			t.Fatal("an English keyword finds nothing")
		}
	})
}

// --- CHATEMOJI-002 ----------------------------------------------------------------------

func TestTodo_CHATEMOJI_002(t *testing.T) {
	t.Run("opening shows frequently used with the starter row, never an empty heading", func(t *testing.T) {
		m := emojiModel("en-US")
		withEmojiHost(t, m, func(*localUI) {
			st := emojiOpenState("chat-composer", false, false, false)
			markup := emojiMarkup(t, m, st, "emoji")
			glyphs := emojiCellGlyphs(t, markup)
			if len(glyphs) < len(emojiStarter) || strings.Join(glyphs[:len(emojiStarter)], " ") != "👍 ❤️ 😂 🎉 🙏 👀 ✅ 🔥" {
				t.Fatalf("starter row = %v", glyphs)
			}
			// Every heading in the whole grid has emoji under it.
			everything := st
			everything.ViewH = 1e6
			v := buildEmojiView(everything, chatEmojiData.index, emojiPrefs{})
			for i, row := range v.rows {
				if row.Head && (i+1 >= len(v.rows) || v.rows[i+1].Head) {
					t.Fatalf("heading %d (group %d) has nothing under it", i, row.Group)
				}
			}
			if v.rows[0].Group != emojiGroupFrequent || !v.rows[0].Head {
				t.Fatal("the grid does not start with Frequently used")
			}
			// Nine groups plus Frequently used, each with a heading.
			heads := 0
			for _, row := range v.rows {
				if row.Head {
					heads++
				}
			}
			if heads != 10 {
				t.Fatalf("%d headings, want Frequently used and Unicode's nine groups", heads)
			}
		})
	})

	t.Run("frequently used is the person's own most used", func(t *testing.T) {
		prefs := emojiPrefs{}
		for _, glyph := range []string{"🏆", "🏆", "🏆", "🎮", "🎮", "🌳", "🍕"} {
			prefs = prefs.bump(glyph)
		}
		if got := strings.Join(prefs.frequent(), " "); got != "🏆 🎮 🍕 🌳" {
			t.Fatalf("frequent = %q (most used first, then most recent)", got)
		}
		if got := strings.Join(prefs.quickReactions(), " "); got != "🏆 🎮 🍕" {
			t.Fatalf("quick reactions = %q", got)
		}
		if got := strings.Join((emojiPrefs{}).quickReactions(), " "); got != "👍 ✅ 👀" {
			t.Fatalf("default quick reactions = %q", got)
		}
		one := emojiPrefs{}.bump("🔥")
		if got := strings.Join(one.quickReactions(), " "); got != "🔥 👍 ✅" {
			t.Fatalf("one own emoji fills up with the defaults: %q", got)
		}
		if got := strings.Join(emojiPrefs{}.bump("👍").quickReactions(), " "); got != "👍 ✅ 👀" {
			t.Fatalf("a duplicate of a default was added: %q", got)
		}
	})

	t.Run("type then Enter inserts the best match at the caret and closes", func(t *testing.T) {
		m := emojiModel("en-US")
		withEmojiHost(t, m, func(local *localUI) {
			var inserted []string
			chatEmojiInsert = func(target, glyph string, keepOpen bool) {
				inserted = append(inserted, fmt.Sprintf("%s|%s|%v", target, glyph, keepOpen))
			}
			chatEmojiToggle("chat-composer")
			if !local.emoji.Open || local.emoji.Target != "chat-composer" || local.emoji.Reaction {
				t.Fatalf("toggle did not open the composer picker: %+v", local.emoji)
			}
			chatEmojiSetQuery("party")
			if !chatEmojiKey("Enter", false, true, false) {
				t.Fatal("Enter was not handled")
			}
			if len(inserted) != 1 || inserted[0] != "chat-composer|🎉|false" {
				t.Fatalf("inserted %v", inserted)
			}
			if local.emoji.Open {
				t.Fatal("choosing in the composer left the picker open")
			}
			if chatEmojiHost.prefs.top(1)[0] != "🎉" {
				t.Fatal("the choice was not counted as a use")
			}
			// Shift keeps it open for more.
			chatEmojiToggle("chat-composer")
			chatEmojiSetQuery("fire")
			chatEmojiKey("Enter", true, true, false)
			if len(inserted) != 2 || inserted[1] != "chat-composer|🔥|true" || !local.emoji.Open {
				t.Fatalf("Shift+Enter: inserted %v, open %v", inserted, local.emoji.Open)
			}
			// Enter with nothing to choose changes nothing.
			chatEmojiSetQuery("zzzz")
			chatEmojiKey("Enter", false, true, false)
			if len(inserted) != 2 {
				t.Fatalf("Enter on an empty result inserted %v", inserted)
			}
		})
	})

	t.Run("a reaction is chosen, added and the picker closes", func(t *testing.T) {
		m := emojiModel("en-US")
		var reacted []string
		var picker []string
		m.PickerID = "post-7"
		m.Callbacks.ReactWith = func(id, emoji string) { reacted = append(reacted, id+"|"+emoji) }
		m.Callbacks.OpenPicker = func(id string) { picker = append(picker, id) }
		withEmojiHost(t, m, func(local *localUI) {
			chatEmojiSetQuery("trophy")
			chatEmojiKey("Enter", false, true, false)
			if len(reacted) != 1 || reacted[0] != "post-7|🏆" {
				t.Fatalf("reacted %v", reacted)
			}
			if len(picker) != 1 || picker[0] != "" {
				t.Fatalf("the picker was not closed through the model: %v", picker)
			}
			if local.emoji.Open {
				t.Fatal("state left open")
			}
			// The rendered reaction picker acts through the reaction action.
			markup := emojiMarkup(t, m, emojiStateFor(emojiPickerState{}, "post-7", true), "reaction")
			if !strings.Contains(markup, `data-action="react-with" data-emoji="👍"`) && !strings.Contains(markup, `data-action="react-with"`) {
				t.Fatal("reaction choices do not carry the react-with action")
			}
			if strings.Contains(markup, `data-action="emoji-insert"`) {
				t.Fatal("a reaction picker offers composer insertion")
			}
		})
	})

	t.Run("Escape clears a query first and closes second", func(t *testing.T) {
		m := emojiModel("en-US")
		withEmojiHost(t, m, func(local *localUI) {
			chatEmojiToggle("chat-composer")
			chatEmojiSetQuery("fire")
			if !chatEmojiEscape() || local.emoji.Query != "" || !local.emoji.Open {
				t.Fatalf("first Escape: state %+v", local.emoji)
			}
			if chatEmojiEscape() || local.emoji.Open {
				t.Fatalf("second Escape: state %+v", local.emoji)
			}
		})
	})

	t.Run("arrow keys move through the grid across headings and in reading direction", func(t *testing.T) {
		m := emojiModel("en-US")
		withEmojiHost(t, m, func(local *localUI) {
			chatEmojiToggle("chat-composer")
			active := func() int { return local.emoji.Active }
			chatEmojiKey("ArrowRight", false, true, false)
			if active() != 1 {
				t.Fatalf("right = %d", active())
			}
			chatEmojiKey("ArrowDown", false, true, false)
			// The starter row is eight wide: one row down from position 1 is the first row of the next section.
			v := buildEmojiView(local.emoji, chatEmojiData.index, chatEmojiHost.prefs)
			want := v.rows[emojiRowOfPos(v.rows, len(emojiStarter))].First + 1
			if active() != want {
				t.Fatalf("down across a heading = %d, want %d", active(), want)
			}
			chatEmojiKey("ArrowUp", false, true, false)
			if active() != 1 {
				t.Fatalf("back up = %d", active())
			}
			// Left and right belong to the text cursor while a query is typed.
			chatEmojiSetQuery("face")
			before := active()
			if chatEmojiKey("ArrowRight", false, true, false) || active() != before {
				t.Fatal("ArrowRight in a non-empty search field moved the highlight")
			}
			chatEmojiKey("ArrowDown", false, true, false)
			if active() != before+emojiCols && active() < before+1 {
				t.Fatalf("down in results = %d", active())
			}
		})
		// Right-to-left swaps left and right.
		rows, _ := emojiRows(emojiSeq{frequent: emojiStarter}, nil, emojiMetrics(false))
		if got := emojiMove(rows, 8, 3, "ArrowRight", true, 5); got != 2 {
			t.Fatalf("ArrowRight in a right-to-left grid = %d, want the previous emoji", got)
		}
		if got := emojiMove(rows, 8, 3, "ArrowLeft", true, 5); got != 4 {
			t.Fatalf("ArrowLeft in a right-to-left grid = %d, want the next emoji", got)
		}
		if got := emojiMove(rows, 8, 0, "ArrowLeft", false, 5); got != 0 {
			t.Fatalf("moving before the first emoji = %d", got)
		}
	})

	t.Run("a skin tone is chosen once, applies to every emoji that has tones, and is remembered", func(t *testing.T) {
		m := emojiModel("de-DE")
		withEmojiHost(t, m, func(local *localUI) {
			chatEmojiToggle("chat-composer")
			chatEmojiToneToggle()
			if !local.emoji.ToneOpen {
				t.Fatal("the tone control did not open")
			}
			open := emojiMarkup(t, m, local.emoji, "emoji")
			swatches := emojiNodes(t, open, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-pop-swatch") })
			if len(swatches) != 6 {
				t.Fatalf("%d tone choices, want the default and five tones", len(swatches))
			}
			if label := chatPolishAttr(swatches[1], "aria-label"); label != "helle Hautfarbe" {
				t.Fatalf("tone 1 is named %q", label)
			}
			chatEmojiToneSet(3)
			if local.emoji.ToneOpen || chatEmojiHost.prefs.Tone != 3 {
				t.Fatalf("tone not applied: %+v %+v", local.emoji, chatEmojiHost.prefs)
			}
			v := buildEmojiView(emojiOpenState("chat-composer", false, false, false), chatEmojiData.index, chatEmojiHost.prefs)
			var thumbs, face string
			for pos := 0; pos < v.seq.len(); pos++ {
				cell, _ := v.cell(pos)
				switch cell.Glyph {
				case "👍🏽":
					thumbs = cell.Glyph
				case "😀":
					face = cell.Glyph
				}
			}
			if thumbs == "" || face == "" {
				t.Fatalf("tone 3 gave thumbs %q, grinning face %q: an emoji with tones must take the tone, one without must not", thumbs, face)
			}
			// Remembered for the person: it round-trips through what is stored.
			again := decodeEmojiPrefs(chatEmojiHost.prefs.encode())
			if again.Tone != 3 {
				t.Fatalf("stored preference = %+v", again)
			}
			for _, bad := range []string{"", "{", `{"v":2,"tone":3}`, `{"v":1,"tone":9}`, strings.Repeat("x", 70<<10)} {
				if decodeEmojiPrefs(bad).Tone != 0 {
					t.Fatalf("damaged preference %q was believed", bad[:min(len(bad), 12)])
				}
			}
		})
	})

	t.Run("the footer names the emoji under the pointer or the highlight, with its shortcode", func(t *testing.T) {
		m := emojiModel("en-US")
		withEmojiHost(t, m, func(*localUI) {
			st := emojiOpenState("chat-composer", false, false, false)
			markup := emojiMarkup(t, m, st, "emoji")
			if !strings.Contains(markup, ">thumbs up<") || !strings.Contains(markup, ">:thumbs_up:<") {
				t.Fatalf("the footer does not name the highlighted emoji: %s", markup)
			}
			st.Hover = 8 // the eighth starter emoji
			markup = emojiMarkup(t, m, st, "emoji")
			if !strings.Contains(markup, ">fire<") || !strings.Contains(markup, ">:fire:<") {
				t.Fatalf("the footer does not follow the pointer: %s", markup)
			}
		})
	})

	t.Run("only the rows near the viewport are in the page", func(t *testing.T) {
		set, en, de := syntheticEmojiSet(4000)
		ix := emojiset.NewIndex(set, de, en)
		m := emojiModel("en-US")
		st := emojiOpenState("chat-composer", false, false, false)
		st.ViewH = 252
		full := renderNode(t, chatEmojiPicker(m, st, "emoji", ix, emojiDataReady, emojiPrefs{}))
		cells := len(emojiCellGlyphs(t, full))
		if cells == 0 || cells > 24*emojiCols {
			t.Fatalf("%d emoji buttons drawn for a grid of 4,000; want a few rows' worth", cells)
		}
		// The spacers account for every row that is not drawn.
		v := buildEmojiView(st, ix, emojiPrefs{})
		first, last := emojiWindow(v.rows, st.Scroll, st.ViewH, v.metrics)
		spacers := emojiNodes(t, full, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-pop-spacer") })
		if len(spacers) != 2 {
			t.Fatalf("%d spacers", len(spacers))
		}
		var tops, bottoms int
		fmt.Sscan(chatPolishAttr(spacers[0], "data-h"), &tops)
		fmt.Sscan(chatPolishAttr(spacers[1], "data-h"), &bottoms)
		drawn := 0.0
		for i := first; i <= last; i++ {
			drawn += v.rows[i].H
		}
		if got, want := float64(tops+bottoms)+drawn, v.total; got < want-2 || got > want+2 {
			t.Fatalf("spacers %d + %d and %v px of rows = %v, want the grid's %v", tops, bottoms, drawn, got, want)
		}
		// Scrolled to the middle, a different window is drawn and the top spacer follows.
		st.Scroll = v.total / 2
		mid := renderNode(t, chatEmojiPicker(m, st, "emoji", ix, emojiDataReady, emojiPrefs{}))
		midSpacers := emojiNodes(t, mid, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-pop-spacer") })
		var midTop int
		fmt.Sscan(chatPolishAttr(midSpacers[0], "data-h"), &midTop)
		if midTop < int(v.total/2)-int(12*emojiCellDesktop) || len(emojiCellGlyphs(t, mid)) > 24*emojiCols {
			t.Fatalf("scrolled window: top spacer %d, %d buttons", midTop, len(emojiCellGlyphs(t, mid)))
		}
		// Every row is reachable by keyboard without drawing it: moving to the last emoji scrolls to it.
		st = emojiPickerState{Open: true, Target: "chat-composer", ViewH: 252}
		for i := 0; i < 5; i++ {
			st = emojiNavigate(st, v, "PageDown", false)
		}
		if st.Active <= 5*emojiCols || !st.ScrollSet || st.Scroll <= 0 {
			t.Fatalf("PageDown x5 left the highlight at %d, scroll %v (set %v)", st.Active, st.Scroll, st.ScrollSet)
		}
	})

	t.Run("clicking a category tab scrolls to it and scrolling marks the tab", func(t *testing.T) {
		m := emojiModel("en-US")
		withEmojiHost(t, m, func(local *localUI) {
			chatEmojiToggle("chat-composer")
			chatEmojiTab(7) // Symbols
			if !local.emoji.ScrollSet || local.emoji.Scroll <= 0 {
				t.Fatalf("tab did not ask for a scroll: %+v", local.emoji)
			}
			v := buildEmojiView(local.emoji, chatEmojiData.index, chatEmojiHost.prefs)
			if cell, _ := v.cell(local.emoji.Active); cell.Glyph != "⚠️" {
				t.Fatalf("the first emoji of Symbols is highlighted: got %q", cell.Glyph)
			}
			markup := emojiMarkup(t, m, local.emoji, "emoji")
			current := emojiNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "aria-current") == "true" })
			if len(current) != 1 || chatPolishAttr(current[0], "aria-label") != "Symbols" {
				t.Fatalf("the marked tab after scrolling to Symbols: %d marked", len(current))
			}
			// Scrolling by hand to the top marks Frequently used again, and only a change of rows asks for a render.
			next, render := emojiScrolled(local.emoji, v, 0, 252)
			if !render {
				t.Fatal("scrolling from Symbols to the top did not ask for a render")
			}
			if g, _ := chatEmojiCurrentGroup(next, v); g != emojiGroupFrequent {
				t.Fatalf("current group at the top = %d", g)
			}
			_, again := emojiScrolled(next, v, 1, 252)
			if again {
				t.Fatal("a one-pixel scroll that changes no row asked for a render")
			}
		})
	})
}

func syntheticEmojiSet(n int) (*emojiset.Set, *emojiset.Lang, *emojiset.Lang) {
	set := &emojiset.Set{Groups: []emojiset.Group{{ID: "smileys-emotion", Name: "A", Count: n / 2}, {ID: "flags", Name: "B", First: n / 2, Count: n - n/2}}}
	en := &emojiset.Lang{Code: "en"}
	for i := 0; i < n; i++ {
		group := 0
		if i >= n/2 {
			group = 1
		}
		set.Entries = append(set.Entries, emojiset.Entry{Glyph: fmt.Sprintf("g%d", i), Group: group})
		en.Names = append(en.Names, fmt.Sprintf("emoji number %d", i))
		en.Keywords = append(en.Keywords, "")
	}
	return set, en, en
}

// --- accessibility, right-to-left, styles --------------------------------------------------

func TestTodo_CHATEMOJI_002_Accessibility(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, kind := range []string{"emoji", "reaction"} {
			t.Run(locale+"/"+kind, func(t *testing.T) {
				m := emojiModel(locale)
				withEmojiHost(t, m, func(*localUI) {
					st := emojiOpenState("chat-composer", kind == "reaction", false, false)
					markup := emojiMarkup(t, m, st, kind)
					dialog := emojiNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "role") == "dialog" })
					if len(dialog) != 1 || chatPolishAttr(dialog[0], "aria-label") == "" {
						t.Fatalf("the picker is not one labelled dialog: %d", len(dialog))
					}
					wantLabel := chatEmojiText(m, emojiKeyTitle)
					if kind == "reaction" {
						wantLabel = chatEmojiText(m, emojiKeyReactTitle)
					}
					if got := chatPolishAttr(dialog[0], "aria-label"); got != wantLabel {
						t.Errorf("dialog label %q, want %q", got, wantLabel)
					}
					if chatPolishAttr(dialog[0], "data-chat-layer") != kind || chatPolishAttr(dialog[0], "popover") != "manual" {
						t.Error("the picker is not a chat layer of its kind")
					}
					// A grid with rows and cells, and a combobox that points at it and at the highlighted cell.
					grids := emojiNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "role") == "grid" })
					if len(grids) != 1 || chatPolishAttr(grids[0], "aria-label") == "" || chatPolishAttr(grids[0], "aria-rowcount") == "" {
						t.Fatalf("grid: %d", len(grids))
					}
					boxes := emojiNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "role") == "combobox" })
					if len(boxes) != 1 || chatPolishAttr(boxes[0], "aria-label") == "" || chatPolishAttr(boxes[0], "aria-controls") != chatPolishAttr(grids[0], "id") {
						t.Fatalf("combobox: %d", len(boxes))
					}
					target := chatPolishAttr(boxes[0], "aria-activedescendant")
					found := emojiNodes(t, markup, func(n *xhtml.Node) bool {
						return chatPolishAttr(n, "id") == target && chatPolishAttr(n, "role") == "gridcell"
					})
					if target == "" || len(found) != 1 || chatPolishAttr(found[0], "aria-selected") != "true" {
						t.Fatalf("aria-activedescendant %q does not name the selected gridcell", target)
					}
					// Every emoji button is named for the emoji, never left as a bare glyph.
					buttons := emojiNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "button" })
					if len(buttons) < 10 {
						t.Fatalf("%d buttons", len(buttons))
					}
					for _, b := range buttons {
						name := chatPolishAttr(b, "aria-label")
						if strings.TrimSpace(name) == "" || name == strings.TrimSpace(emojiText(b)) {
							t.Errorf("button %q has no name of its own (%q)", emojiText(b), name)
						}
					}
					for _, b := range emojiNodes(t, markup, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-pop-btn") }) {
						if chatPolishAttr(b, "tabindex") != "-1" {
							t.Errorf("an emoji button is a tab stop: the highlight is the one stop (combobox)")
							break
						}
					}
					// Tabs: named, tooltips, exactly one current.
					tabs := emojiNodes(t, markup, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-pop-tab") })
					if len(tabs) != 10 {
						t.Fatalf("%d category tabs, want Frequently used and nine groups", len(tabs))
					}
					current := 0
					for _, tab := range tabs {
						if chatPolishAttr(tab, "aria-label") == "" || chatPolishAttr(tab, "title") != chatPolishAttr(tab, "aria-label") {
							t.Errorf("tab %q: name %q tooltip %q", emojiText(tab), chatPolishAttr(tab, "aria-label"), chatPolishAttr(tab, "title"))
						}
						if chatPolishAttr(tab, "aria-current") == "true" {
							current++
						}
					}
					if current != 1 {
						t.Errorf("%d current tabs", current)
					}
					// The copy is the reader's language: group names differ between languages.
					switch locale {
					case "de-DE":
						if !strings.Contains(markup, "Häufig verwendet") || !strings.Contains(markup, "Tiere &amp; Natur") && !strings.Contains(markup, "Tiere & Natur") {
							t.Error("German copy missing")
						}
					case "ar":
						if !strings.Contains(markup, "الأكثر استخدامًا") || !strings.Contains(markup, "الحيوانات والطبيعة") {
							t.Error("Arabic copy missing")
						}
					default:
						if !strings.Contains(markup, "Frequently used") || !strings.Contains(markup, "Animals") {
							t.Error("English copy missing")
						}
					}
					// The skin-tone control is one named button.
					tone := emojiNodes(t, markup, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-pop-tone") })
					if len(tone) != 1 || chatPolishAttr(tone[0], "aria-label") == "" || chatPolishAttr(tone[0], "aria-expanded") != "false" {
						t.Fatalf("tone control: %d", len(tone))
					}
					for _, forbidden := range []string{"<style", ` style="`, "chat.emoji", "emoji-pop-sticky\" >"} {
						if strings.Contains(markup, forbidden) {
							t.Errorf("markup contains %q", forbidden)
						}
					}
				})
			})
		}
	}
}

func TestChatEmojiStylesUseTokensAndLogicalProperties(t *testing.T) {
	css := ChatEmojiStyles
	if !strings.Contains(Stylesheet, css[:80]) {
		t.Fatal("the emoji picker styles are not part of the chat stylesheet")
	}
	for _, re := range []string{`#[0-9a-fA-F]{3,8}\b`, `\brgba?\(`, `\bhsla?\(`, `margin-left`, `margin-right`, `padding-left`, `padding-right`, `text-align:\s*(left|right)`, `float:`, `<style`, `\bleft:`, `\bright:`} {
		if m := regexp.MustCompile(re).FindString(css); m != "" {
			t.Errorf("styles use %q: only --hcm-* tokens and logical properties are allowed", m)
		}
	}
	for _, m := range regexp.MustCompile(`var\(--([a-z0-9-]+)`).FindAllStringSubmatch(css, -1) {
		if !strings.HasPrefix(m[1], "hcm-") {
			t.Errorf("styles use the token --%s", m[1])
		}
	}
	for _, m := range regexp.MustCompile(`border-radius:([^;}]+)`).FindAllStringSubmatch(css, -1) {
		if !regexp.MustCompile(`^(var\(--hcm-radius-(control|surface)\)|0|50%)( (var\(--hcm-radius-(control|surface)\)|0))*$`).MatchString(strings.TrimSpace(m[1])) {
			t.Errorf("border-radius %q is not a token", m[1])
		}
	}
	// Reduced motion: nothing in the picker animates.
	if strings.Contains(css, "animation") || strings.Contains(css, "transition:") && !strings.Contains(css, "prefers-reduced-motion") {
		t.Error("the picker animates without honouring reduced motion")
	}
}

// --- placement ------------------------------------------------------------------------------

func TestChatEmojiPlacementIsAboveTheOpenerInsideTheColumn(t *testing.T) {
	column := chatLayerRect{360, 80, 1420, 880}
	button := chatLayerRect{632, 836, 660, 866} // the composer's emoji button
	got := chatEmojiPlace(button, column, 1440, 900, false, false)
	if got.Left != 632 || got.Width != emojiPickerWidth {
		t.Fatalf("composer picker not aligned to its button: %+v", got)
	}
	if got.Top+got.Height > button.top-emojiPickerGap+0.001 || got.Top < 8 {
		t.Fatalf("composer picker does not sit above its button: %+v", got)
	}
	// It never covers the button and never leaves the column or the viewport.
	for _, vw := range []float64{1440, 900, 700, 400, 320} {
		for _, bx := range []float64{20, 380, 632, 1300, 1410} {
			b := chatLayerRect{bx, 836, bx + 28, 866}
			col := chatLayerRect{min(360, vw/3), 80, vw - 20, 880}
			p := chatEmojiPlace(b, col, vw, 900, false, false)
			if p.Left < 8-0.001 || p.Left+p.Width > vw-8+0.001 || p.Top < 8-0.001 || p.Top+p.Height > 900-8+0.001 {
				t.Fatalf("picker leaves the viewport at width %v, button %v: %+v", vw, bx, p)
			}
			if p.Top < b.bottom && p.Top+p.Height > b.top && p.Left < b.right && p.Left+p.Width > b.left {
				t.Fatalf("picker covers its button at width %v, button %v: %+v", vw, bx, p)
			}
		}
	}
	// Right-to-left mirrors the alignment: the picker's end edge meets the button's.
	rtl := chatEmojiPlace(chatLayerRect{900, 836, 928, 866}, column, 1440, 900, true, false)
	if rtl.Left+rtl.Width != 928 {
		t.Fatalf("right-to-left composer picker: %+v", rtl)
	}
	// A reaction aligns to the end of the message's action bar.
	bar := chatLayerRect{1200, 400, 1410, 424}
	react := chatEmojiPlace(bar, column, 1440, 900, false, true)
	if react.Left+react.Width != 1410 {
		t.Fatalf("reaction picker is not aligned to the bar's end: %+v", react)
	}
	// With no room above, it opens below; with no room either side the grid gives up height.
	top := chatEmojiPlace(chatLayerRect{1200, 40, 1410, 64}, column, 1440, 900, false, true)
	if top.Top < 64 {
		t.Fatalf("a bar near the top opened the picker over it: %+v", top)
	}
	cramped := chatEmojiPlace(chatLayerRect{1200, 150, 1410, 174}, column, 1440, 330, false, true)
	if cramped.Height >= emojiPickerHeight || cramped.Top < 8 || cramped.Top+cramped.Height > 330-8+0.001 {
		t.Fatalf("a cramped viewport: %+v", cramped)
	}
}

// --- rendered tree, the way the other Chat browser tests read it -------------------------------

func TestTodo_CHATEMOJI_002_Browser(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		withEmojiHost(t, m, func(*localUI) {
			// The composer: the trigger is always there, the picker only while it is open.
			closed := chatPolishMarkup(t, chatEmojiComposerPicker(m, localUI{}, "chat-composer", false), width, theme)
			if strings.Contains(closed, "data-emoji-picker") || !strings.Contains(closed, `data-action="emoji-toggle"`) || !strings.Contains(closed, `aria-expanded="false"`) {
				t.Fatalf("closed composer picker: %s", closed)
			}
			st := emojiOpenState("chat-composer", false, width <= 390, width <= 767)
			opened := chatPolishMarkup(t, chatEmojiComposerPicker(m, localUI{emoji: st}, "chat-composer", false), width, theme)
			composerLayer := chatPolishMarkup(t, chatEmojiComposerLayer(m, localUI{emoji: st}, "chat-composer"), width, theme)
			triggers := emojiNodes(t, opened, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-trigger") })
			pickers := emojiNodes(t, composerLayer, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-emoji-picker") == "emoji" })
			if len(triggers) != 1 || len(pickers) != 1 || chatPolishAttr(triggers[0], "aria-expanded") != "true" {
				t.Fatalf("open composer picker: %d triggers, %d pickers", len(triggers), len(pickers))
			}
			if chatPolishAttr(triggers[0], "aria-controls") != chatPolishAttr(pickers[0], "id") || chatPolishAttr(pickers[0], "data-target") != "chat-composer" {
				t.Fatal("the button does not name the picker it opened")
			}
			if chatPolishAttr(pickers[0], "data-sheet") != boolString(width <= 767) {
				t.Fatalf("width %d: data-sheet = %q", width, chatPolishAttr(pickers[0], "data-sheet"))
			}
			// The thread composer's picker is the same component with its own ids.
			thread := chatPolishMarkup(t, chatEmojiComposerLayer(m, localUI{emoji: emojiOpenState("thread-composer", false, false, false)}, "thread-composer"), width, theme)
			if !strings.Contains(thread, `id="emoji-emoji-thread-composer"`) || strings.Contains(thread, `id="emoji-emoji-chat-composer"`) {
				t.Fatal("the thread composer's picker is not its own")
			}
			// An open picker belongs to one composer: the other draws none.
			other := chatPolishMarkup(t, chatEmojiComposerLayer(m, localUI{emoji: st}, "thread-composer"), width, theme)
			if strings.Contains(other, "data-emoji-picker") {
				t.Fatal("one open picker drew in two composers")
			}
			// A disabled composer's button stays disabled.
			if disabled := chatPolishMarkup(t, chatEmojiComposerPicker(m, localUI{}, "chat-composer", true), width, theme); !strings.Contains(disabled, " disabled") {
				t.Fatal("the button of a disabled composer is enabled")
			}

			// The reaction picker follows the message model.
			if chatEmojiReactionLayer(m, localUI{}) != nil {
				t.Fatal("a reaction picker is drawn with no message picked")
			}
			m.PickerID = "question"
			reaction := chatPolishMarkup(t, chatEmojiReactionLayer(m, localUI{}), width, theme)
			layer := emojiNodes(t, reaction, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-emoji-picker") == "reaction" })
			if len(layer) != 1 || chatPolishAttr(layer[0], "data-target") != "question" || chatPolishAttr(layer[0], "data-chat-layer") != "reaction" {
				t.Fatalf("reaction picker: %d layers", len(layer))
			}
			for _, b := range emojiNodes(t, reaction, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-pop-btn") }) {
				if chatPolishAttr(b, "data-action") != "react-with" || chatPolishAttr(b, "data-id") != "question" {
					t.Fatalf("a reaction choice does not react to the message: %s", reaction)
				}
			}
			// The old picker's pieces are gone from the page: one component, no second variant.
			page := chatPolishMarkup(t, Build(m), width, theme)
			for _, old := range []string{`class="reaction-picker"`, `class="emoji-picker"`, "emoji-category", "chat.emoji.faces", ">Faces<", ">Recent<"} {
				if strings.Contains(page, old) {
					t.Errorf("the page still carries the old picker: %q", old)
				}
			}
		})
	})
}

func TestChatEmojiPickerOpensFromTheRenderedComposerButton(t *testing.T) {
	model := Model{
		State: StateReady, SelectedID: "room", ShowThread: true, ThreadParentID: "root",
		Conversations: []Conversation{{ID: "room", Name: "People"}},
		Callbacks:     Callbacks{ReplyInThread: func(string, string) {}},
	}
	markup := render(t, model)
	for _, id := range []string{"chat-composer", "thread-composer"} {
		want := fmt.Sprintf(`data-action="emoji-toggle" data-id="%s"`, id)
		if !strings.Contains(markup, want) {
			t.Errorf("no emoji button for %s", id)
		}
	}
	if strings.Contains(markup, "data-emoji-picker") {
		t.Fatal("a picker is drawn before anything opened it")
	}
}

// A load that fails while the data is already there changes nothing, and typing
// into a picker whose load failed tries the load again.
func TestChatEmojiLoadFailureKeepsGoodDataAndTypingRetries(t *testing.T) {
	m := emojiModel("en-US")
	withEmojiHost(t, m, func(local *localUI) {
		chatEmojiToggle("chat-composer")
		chatEmojiData.status = emojiDataLoading // a second load is in flight
		chatEmojiLoaded("en", nil, fmt.Errorf("connection refused"))
		if chatEmojiData.index == nil || chatEmojiData.status != emojiDataReady {
			t.Fatalf("a failed second load spoiled the data: status %v, index %v", chatEmojiData.status, chatEmojiData.index != nil)
		}
		// With nothing loaded, the failure shows, and typing starts another attempt.
		chatEmojiData = chatEmojiDataStore{status: emojiDataLoading}
		chatEmojiLoaded("en", nil, fmt.Errorf("connection refused"))
		if chatEmojiData.status != emojiDataFailed {
			t.Fatalf("status = %v, want failed", chatEmojiData.status)
		}
		chatEmojiSetQuery("party")
		if chatEmojiData.status != emojiDataLoading || local.emoji.Query != "party" {
			t.Fatalf("typing did not retry the load: status %v, query %q", chatEmojiData.status, local.emoji.Query)
		}
	})
}

// The picker is on screen from the click itself: while closed, the composer holds
// a hidden picker drawn from what is compiled in, and open with no emoji data yet
// it shows the search field, the Frequently used row and the footer at once.
func TestTodo_CHATEMOJI_002_OpensBeforeTheDataHasLoaded(t *testing.T) {
	m := emojiModel("en-US")
	withEmojiHost(t, m, func(*localUI) {
		chatEmojiData = chatEmojiDataStore{} // nothing loaded, nothing asked for yet
		chatEmojiHost.prefs = emojiPrefs{}.bump("🏆").bump("🏆").bump("🎮")

		// Closed: a hidden shell, not a layer, with the person's own frequently used row.
		closed := renderNode(t, chatEmojiComposerLayer(m, localUI{}, "chat-composer"))
		shells := emojiNodes(t, closed, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-emoji-shell") == "emoji" })
		if len(shells) != 1 || chatPolishAttr(shells[0], "data-target") != "chat-composer" || chatPolishAttr(shells[0], "popover") != "manual" {
			t.Fatalf("closed composer holds %d shells: %s", len(shells), closed)
		}
		for _, forbidden := range []string{"data-emoji-picker", "data-chat-layer", "emoji-pop-note", "⟦", "Loading"} {
			if strings.Contains(closed, forbidden) {
				t.Errorf("the closed shell carries %q", forbidden)
			}
		}
		if got := strings.Join(emojiCellGlyphs(t, closed), " "); got != "🏆 🎮" {
			t.Fatalf("the shell's Frequently used row = %q, want the person's own", got)
		}
		if len(emojiNodes(t, closed, func(n *xhtml.Node) bool { return n.Data == "input" && emojiHasClass(n, "emoji-pop-input") })) != 1 {
			t.Fatal("the shell has no search field")
		}

		// Open, with the data still on its way: everything but the categories.
		st := emojiOpenState("chat-composer", false, false, false)
		chatEmojiData.status = emojiDataLoading
		open := renderNode(t, chatEmojiComposerLayer(m, localUI{emoji: st}, "chat-composer"))
		pickers := emojiNodes(t, open, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-emoji-picker") == "emoji" })
		if len(pickers) != 1 || strings.Contains(open, "data-emoji-shell") || chatPolishAttr(pickers[0], "id") != chatPolishAttr(shells[0], "id") {
			t.Fatalf("the open picker is not the shell's own element: %s", open)
		}
		if got := strings.Join(emojiCellGlyphs(t, open), " "); got != "🏆 🎮" {
			t.Fatalf("Frequently used while loading = %q", got)
		}
		heads := emojiNodes(t, open, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-pop-head") })
		if len(heads) != 1 || emojiText(heads[0]) != "Frequently used" {
			t.Fatalf("%d headings while loading, want Frequently used alone", len(heads))
		}
		notes := emojiNodes(t, open, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-pop-note") })
		if len(notes) != 1 || !strings.Contains(emojiText(notes[0]), "Loading") {
			t.Fatal("the picker does not say the full list is loading")
		}
		for _, want := range []string{`role="dialog"`, `role="combobox"`, "emoji-pop-foot", "emoji-pop-tone", "emoji-pop-tabs"} {
			if !strings.Contains(open, want) {
				t.Errorf("the open picker lacks %q before the data has loaded", want)
			}
		}
		// Typing already works: a query with no data shows the loading note, not a crash.
		typed := emojiQuery(st, "party")
		if got := renderNode(t, chatEmojiComposerLayer(m, localUI{emoji: typed}, "chat-composer")); !strings.Contains(got, "emoji-pop-note") {
			t.Error("a query typed before the data has loaded draws nothing to say so")
		}

		// The data arrives: the same element gains its categories.
		chatEmojiData = chatEmojiDataStore{status: emojiDataReady, lang: "en", index: emojiTestIndex(t, "en")}
		full := renderNode(t, chatEmojiComposerLayer(m, localUI{emoji: st}, "chat-composer"))
		if len(emojiNodes(t, full, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-pop-tab") })) != 10 {
			t.Fatal("the loaded picker lacks its category tabs")
		}
	})
}

// The browser half cannot run in a native test, so the order that makes the
// picker immediate is pinned in the source: the click shows the shell before the
// state change, and the data is asked for on pointer or focus, not on the click.
func TestTodo_CHATEMOJI_002_ImmediateOpenIsWired(t *testing.T) {
	data, err := os.ReadFile("chatemoji_js.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	for _, want := range []string{
		"chatEmojiShowShell(id)",
		"add(\"pointerover\", prefetch)",
		"add(\"focusin\", prefetch)",
		"chatEmojiSettleShells()",
		"[data-emoji-shell][data-target='",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("chatemoji_js.go no longer carries %q", want)
		}
	}
	if strings.Index(src, "chatEmojiShowShell(id)") > strings.Index(src, "A click anywhere else on the page") {
		t.Error("the shell is shown after the click-outside check")
	}
}

// The pickers live in one component of their own, drawn once for the page: a
// shell per composer that exists, and none inside the composer's own markup.
func TestTodo_CHATEMOJI_002_LayersAreOneComponentOfTheWorkspace(t *testing.T) {
	m := emojiModel("en-US")
	withEmojiHost(t, m, func(*localUI) {
		page := render(t, m)
		if got := strings.Count(page, `data-emoji-shell="emoji"`); got != 1 {
			t.Fatalf("%d emoji shells on a page with one composer", got)
		}
		threaded := m
		threaded.ShowThread, threaded.ThreadParentID = true, "root"
		threaded.Callbacks = Callbacks{ReplyInThread: func(string, string) {}}
		if got := strings.Count(render(t, threaded), `data-emoji-shell="emoji"`); got != 2 {
			t.Fatalf("%d emoji shells with the thread open, want one per composer", got)
		}
		composer := renderNode(t, chatEmojiComposerPicker(m, localUI{}, "chat-composer", false))
		if strings.Contains(composer, "emoji-pop") {
			t.Fatal("the composer's own markup still holds a picker")
		}
		if got := chatEmojiLayerNodes(m, localUI{}); len(got) != 3 || got[0] != nil || got[2] != nil {
			t.Fatalf("layers for a page with no reaction and no thread: %v", got)
		}
	})
}

// A change to the picker's own state redraws the layers, not the workspace; the
// two are redrawn together only when the picker opens or closes.
func TestTodo_CHATEMOJI_002_PickerStateRedrawsOnlyTheLayers(t *testing.T) {
	m := emojiModel("en-US")
	withEmojiHost(t, m, func(local *localUI) {
		layers, workspace := 0, 0
		chatEmojiHost.layerRender = func() { layers++ }
		chatEmojiHost.apply = func(change func(*localUI)) { workspace++; change(local) }
		chatEmojiToggle("chat-composer")
		if layers != 1 || workspace != 1 {
			t.Fatalf("opening redrew layers %d and workspace %d times, want 1 and 1", layers, workspace)
		}
		layers, workspace = 0, 0
		chatEmojiSetQuery("party")
		chatEmojiHover(2)
		chatEmojiToneToggle()
		chatEmojiKey("ArrowRight", false, false, false)
		if workspace != 0 || layers == 0 || local.emoji.Query != "party" {
			t.Fatalf("typing and moving redrew the workspace %d times and the layers %d; query %q", workspace, layers, local.emoji.Query)
		}
		layers, workspace = 0, 0
		chatEmojiLoaded("en", chatEmojiData.index, nil)
		if workspace != 0 || layers != 1 {
			t.Fatalf("the data arriving redrew the workspace %d times and the layers %d", workspace, layers)
		}
		chatEmojiClose(false)
		if workspace != 1 || layers != 2 || local.emoji.Open {
			t.Fatalf("closing redrew workspace %d, layers %d, open %v", workspace, layers, local.emoji.Open)
		}
	})
}

// A typed query draws only the emoji that contain it, nothing to fill the grid,
// and a query that matches nothing says so in one plain line.
func TestTodo_CHATEMOJI_001_Browser_OnlyMatchesAreDrawn(t *testing.T) {
	for _, tc := range []struct {
		locale, query string
		want          []string
	}{
		{"en-US", "party", []string{"🎉"}},
		{"en-US", "PARTY", []string{"🎉"}},
		{"en-US", "partyy", nil},
		{"en-US", "ptry", nil},
		{"en-US", "zzzz", nil},
		{"en-US", "fi", []string{"🔥", "🙏"}},
		{"de-DE", "party", []string{"🎉"}},
		{"de-DE", "Feuer", []string{"🔥"}},
		{"de-DE", "zzzz", nil},
		{"ar", "نار", []string{"🔥"}},
		{"ar", "party", []string{"🎉"}},
		{"ar", "zzzz", nil},
	} {
		t.Run(tc.locale+"/"+tc.query, func(t *testing.T) {
			m := emojiModel(tc.locale)
			withEmojiHost(t, m, func(*localUI) {
				st := emojiQuery(emojiOpenState("chat-composer", false, false, false), tc.query)
				markup := emojiMarkup(t, m, st, "emoji")
				got := emojiCellGlyphs(t, markup)
				if strings.Join(got, " ") != strings.Join(tc.want, " ") {
					t.Fatalf("%q drew %v, want %v and nothing else", tc.query, got, tc.want)
				}
				notes := emojiNodes(t, markup, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-pop-note") })
				if (len(tc.want) == 0) != (len(notes) == 1) || len(notes) > 1 {
					t.Fatalf("%q: %d notes for %d results", tc.query, len(notes), len(tc.want))
				}
				if len(tc.want) == 0 && !strings.Contains(emojiText(notes[0]), tc.query) {
					t.Errorf("the no-result line does not name the query: %q", emojiText(notes[0]))
				}
				if heads := emojiNodes(t, markup, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-pop-head") }); len(heads) != 0 {
					t.Errorf("%d headings over a result list", len(heads))
				}
			})
		})
	}
}
