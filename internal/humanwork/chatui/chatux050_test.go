package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

// TestTodo_CHATUX_050: the composer at rest is one line plus its tool row and
// grows to a third of the window; on a phone Send stays on the tool row; key
// names are the ones on the reader's keyboard; Saved counts add up; every row
// menu item leads with an icon; workspace filters show only what differs;
// every row's help is one sentence; the details title block scrolls away
// cleanly; a GIF shows its first frame.
func TestTodo_CHATUX_050(t *testing.T) {
	t.Run("ComposerEmptyAndGrowth", func(t *testing.T) {
		// The exact empty total (56-80 px) is pinned by
		// TestTodo_CHATUX_050_Cascade; here the growth contract: the form
		// caps at a third of the window and the field scrolls inside
		// itself past that.
		for _, want := range []string{
			`form.chat-composer{padding:4px 6px;max-block-size:33.333dvh`,
			`.composer-draft .composer-input{flex:0 1 auto;min-block-size:34px;max-block-size:none;padding-block:6px;overflow-y:auto;resize:none}`,
			`.chat-composer .composer-toolbar{flex:none;min-block-size:34px`,
		} {
			if !strings.Contains(Stylesheet, want) {
				t.Errorf("the stylesheet misses %q", want)
			}
		}
	})

	t.Run("PhoneSendOnToolRow", func(t *testing.T) {
		// Send is a child of the tool row in markup, and the row never
		// wraps, so on a phone Send cannot drop onto a row of its own.
		markup := renderNode(t, composerToolRow(Model{Locale: "en-US"}, handlers{}, "chat-composer", false, true))
		toolbar := markup[strings.Index(markup, "composer-toolbar"):]
		if !strings.Contains(toolbar, "send-button") {
			t.Fatalf("Send is not in the composer tool row: %s", markup)
		}
		toolbarSel := func(sel string) bool {
			return strings.HasSuffix(sel, ".composer-toolbar") && strings.Contains(sel, ".chat-composer") && s11Without(sel, "thread-composer")
		}
		for _, width := range []int{320, 390, 600, 760} {
			if got := s11Value(t, s11Env{width: width}, "flex-wrap", toolbarSel); got != "nowrap" {
				t.Errorf("%dpx: the composer tool row wraps %q, want nowrap so Send stays on it", width, got)
			}
		}
	})

	t.Run("ShortcutKeyNames", func(t *testing.T) {
		// The key names are the ones printed on the reader's keyboard:
		// German control is Strg, and the hints never translate Enter.
		for _, tc := range []struct{ locale, platform, want string }{
			{"en-US", "Win32", "Ctrl+K"},
			{"en-US", "MacIntel", "Cmd+K"},
			{"de-DE", "Win32", "Strg+K"},
			{"de-DE", "Linux x86_64", "Strg+K"},
			{"de-DE", "MacIntel", "Cmd+K"},
			{"ar", "Win32", "Ctrl+K"},
			{"", "", "Ctrl+K"},
		} {
			if got := chatux010ShortcutLabelFor(tc.locale, tc.platform); got != tc.want {
				t.Errorf("locale %q platform %q: the shortcut reads %q, want %q", tc.locale, tc.platform, got, tc.want)
			}
		}
		// The machine-readable form is never localised.
		if got := chatux010KeyShortcuts("Win32"); got != "Control+K" {
			t.Errorf("aria-keyshortcuts is %q, want Control+K", got)
		}
	})

	t.Run("SearchChipMirrors", func(t *testing.T) {
		// The chip's own text stays left to right; in Arabic it sits at
		// the inline end of the search field, mirroring English.
		for _, want := range []string{
			`.rail-search .chat-search-shortcut{position:absolute;inset-inline-end:22px`,
			`.chat-workspace[dir="rtl"] .rail-search .chat-search-shortcut{inset-inline-end:auto;left:22px;right:auto}`,
		} {
			if !strings.Contains(Stylesheet, want) {
				t.Errorf("the stylesheet misses %q", want)
			}
		}
		markup := renderNode(t, chatux010ShortcutHint(Model{Locale: "ar"}))
		if !strings.Contains(markup, `dir="ltr"`) {
			t.Errorf("the Arabic shortcut chip does not keep its own direction: %s", markup)
		}
	})

	t.Run("SavedCounts", func(t *testing.T) {
		// Counts that add up need no explanation; an All that is larger
		// than To do plus Done says why on its own tab.
		if note := chatux040AllNote("en-US", 0, 2, 2); note != "" {
			t.Errorf("counts that add up still explain %q", note)
		}
		markup := renderNode(t, RenderSavedMessages(SavedMessagesView{Locale: "en-US", Tab: "todo", TodoCount: 0, DoneCount: 2, AllCount: 3}))
		at := strings.Index(markup, `data-saved-tab="all"`)
		button := ""
		if at >= 0 {
			button = markup[strings.LastIndex(markup[:at], "<button"):]
			button = button[:strings.Index(button, ">")+1]
		}
		if !strings.Contains(button, "whose original was deleted") {
			t.Errorf("the All tab does not explain its extra item: %s", markup)
		}
	})

	t.Run("RowMenuIcons", func(t *testing.T) {
		// Every item of the conversation row menu leads with a glyph: the
		// notification radios are no exception. They keep their radio
		// role and the choice in force keeps its check, now at the end.
		m := Model{Locale: "en-US", RailMenuID: "general", SelectedID: "general",
			Conversations: []Conversation{{ID: "general", Name: "general", Kind: PublicChannel, Joined: true}},
			Callbacks:     walkCallbacks()}
		m.Preferences.Notifications = map[string]NotificationMode{"general": NotifyMention}
		markup := renderNode(t, chatux020RailMenu(m, handlers{}))
		rest := markup
		items, radios := 0, 0
		for {
			at := strings.Index(rest, `class="menu-item`)
			if at < 0 {
				break
			}
			items++
			end := strings.Index(rest[at:], "</button>")
			// The renderer writes aria attributes before the class.
			button := rest[strings.LastIndex(rest[:at], "<button") : at+end]
			if strings.Contains(button, `role="menuitemradio"`) {
				radios++
				if !strings.Contains(button, `aria-checked=`) {
					t.Errorf("a notification choice lost its radio state: %s", button)
				}
			}
			if !strings.Contains(button, "<svg") {
				t.Errorf("a row menu item has no leading icon: %s", button)
			}
			rest = rest[at+end+len("</button>"):]
		}
		if items == 0 || radios != 3 {
			t.Errorf("the row menu holds %d items with %d notification radios, want 3 radios among several items", items, radios)
		}
		if !strings.Contains(markup, `class="rail-menu-check"`) || !strings.Contains(markup, "\u2713") {
			t.Error("the choice in force lost its check")
		}
	})

	t.Run("WorkspaceFiltersDiff", func(t *testing.T) {
		// The workspace row opened from a channel shows only the lists
		// where the channel differs, and "Same as workspace" when none
		// does. The workspace's own page still lists everything.
		defs := []chatfilter.Definition{
			{ID: "builtin-en-profanity", Name: "profanity", Language: "en", Kind: "words", Action: "block", Product: true},
			{ID: "builtin-en-slurs", Name: "slurs", Language: "en", Kind: "words", Action: "block", Product: true},
		}
		enabled := []chatfilter.Enablement{
			{RuleID: "builtin-en-profanity", Enabled: true},
			{RuleID: "builtin-en-slurs", Enabled: true},
			{RuleID: "builtin-en-profanity", Channel: "general", Enabled: false},
		}
		m := Model{Locale: "en-US", SelectedID: "general", IsTenantAdmin: true}
		fromChannel := renderNode(t, ModAdminPanel(ModAdminProps{Model: m, Workspace: true, Channel: "general", Definitions: defs, Enablements: enabled, CanManage: true}))
		// The test helper cannot fail on its own mismatched anchor: the
		// test must be able to fail. Both list names render on the
		// workspace page, so their absence below is the diff at work.
		whole := renderNode(t, ModAdminPanel(ModAdminProps{Model: m, Workspace: true, Definitions: defs, Enablements: enabled, CanManage: true}))
		for _, name := range []string{"Profanity", "Slurs"} {
			if !strings.Contains(whole, name) {
				t.Fatalf("the workspace page does not name %q, so the diff below proves nothing: %s", name, whole)
			}
		}
		if !strings.Contains(fromChannel, "Profanity") || strings.Contains(fromChannel, "Slurs") {
			t.Errorf("the channel's workspace row does not show only the differing list: %s", fromChannel)
		}
		same := renderNode(t, ModAdminPanel(ModAdminProps{Model: m, Workspace: true, Channel: "general",
			Definitions: defs, Enablements: enabled[:2], CanManage: true}))
		if !strings.Contains(same, "Same as workspace") {
			t.Errorf("a channel that matches the workspace does not say so: %s", same)
		}
		for locale, want := range map[string]string{"de-DE": "Wie im Arbeitsbereich", "ar": "مطابق لإعدادات مساحة العمل"} {
			m.Locale = locale
			got := renderNode(t, ModAdminPanel(ModAdminProps{Model: m, Workspace: true, Channel: "general",
				Definitions: defs, Enablements: enabled[:2], CanManage: true}))
			if !strings.Contains(got, want) {
				t.Errorf("%s: a channel that matches the workspace does not say %q: %s", locale, want, got)
			}
		}
	})

	t.Run("OneSentenceHelp", func(t *testing.T) {
		// Every details row's help is at most one sentence; a bare phrase
		// is shorter still and fine. Endings in the three shipped
		// languages all count.
		sentences := func(text string) int {
			return strings.Count(text, ".") + strings.Count(text, "!") + strings.Count(text, "?") + strings.Count(text, "\u061f") + strings.Count(text, "\u2026")
		}
		helps := map[string]string{
			"agent privacy":    agentux070ChannelText(Model{Locale: "en-US"}, "help"),
			"agent privacy de": agentux070ChannelText(Model{Locale: "de-DE"}, "help"),
			"agent privacy ar": agentux070ChannelText(Model{Locale: "ar"}, "help"),
			"integrations":     englishCopy[KeyIntegrationsHint],
			"team":             englishCopy[KeyTeamNote],
			"project":          englishCopy[KeyProjectNote],
			"workspace scope":  laneText(Model{Locale: "en-US"}, chatux041Copy, chatux041KeyWorkspaceScope),
		}
		for name, text := range helps {
			if n := sentences(text); n > 1 {
				t.Errorf("%s help is %d sentences, want one: %q", name, n, text)
			}
		}
		// The two trims keep their facts in one sentence each.
		if got := englishCopy[KeyIntegrationsHint]; got != "An installed app can read and post here with its app token." {
			t.Errorf("integrations help is %q", got)
		}
		if got := agentux070ChannelText(Model{Locale: "en-US"}, "help"); got != "When this is on, every agent answer here is shown only to the person who asked." {
			t.Errorf("agent privacy help is %q", got)
		}
	})

	t.Run("TitleBlockScrollsAway", func(t *testing.T) {
		// The details title block fades over the first scroll and never
		// shows through the sticky head, which is opaque; without motion
		// it scrolls under that head like any content.
		for _, want := range []string{
			`.chat-details .details-summary{animation:chatux041-away linear both`,
			`@keyframes chatux041-away{from{opacity:1}to{opacity:0}}`,
			`.side-heading{display:flex;align-items:center;justify-content:space-between;gap:8px;height:52px;flex:none;position:sticky;top:0;background:inherit`,
			`.chat-side{position:relative;display:flex;flex-direction:column;min-height:0;overflow-y:auto;overscroll-behavior:contain;border-inline-start:1px solid var(--line);background:var(--canvas)`,
			`@media(prefers-reduced-motion:reduce){.chat-workspace .chat-details .chat-panel-head,.chat-workspace .chat-details .details-summary{animation:none}}`,
		} {
			if !strings.Contains(Stylesheet, want) {
				t.Errorf("the stylesheet misses %q", want)
			}
		}
	})

	t.Run("GifFirstFrame", func(t *testing.T) {
		// A GIF attachment is drawn as its own picture (the browser shows
		// the first frame) at its stored size, with the badge beside the
		// words, never over them.
		m := Model{Locale: "en-US"}
		msg := Message{ID: "p1", Author: "Ann Lee", Attachments: []Attachment{{ID: "a1", Name: "party.gif", ContentType: "image/gif", Width: 320, Height: 240, Bytes: 12000}}}
		markup := renderNode(t, attachments(m, msg))
		if !strings.Contains(markup, "<img") || !strings.Contains(markup, "attachment-image-open") {
			t.Errorf("a GIF is not drawn as its own picture: %s", markup)
		}
		if !strings.Contains(markup, `data-frame-w="320"`) {
			t.Errorf("a GIF is not sized from its stored dimensions: %s", markup)
		}
		img, badge := strings.Index(markup, "<img"), strings.Index(markup, "attachment-badge")
		if img < 0 || badge < 0 || badge < img {
			t.Errorf("the GIF badge is not drawn after the picture: %s", markup)
		}
	})
}
