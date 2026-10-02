package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHATBUG_091: Saved opens on the first tab with items and an empty tab
// tells the truth about the others; the first-use sentence is for a Saved that
// has never held anything.
func TestTodo_CHATBUG_091(t *testing.T) {
	for _, tc := range []struct {
		todo, done, all int
		want            string
	}{{3, 2, 5, "todo"}, {0, 2, 3, "done"}, {0, 0, 1, "all"}, {0, 0, 0, "todo"}} {
		if got := ChatBug091FirstTab(tc.todo, tc.done, tc.all); got != tc.want {
			t.Errorf("counts %d/%d/%d open on %q, want %q", tc.todo, tc.done, tc.all, got, tc.want)
		}
	}

	empty := func(view SavedMessagesView) string {
		t.Helper()
		return renderNode(t, RenderSavedMessages(view))
	}
	firstUse := SavedMessagesCopy("en-US").EmptyTodo

	// To do is empty and Done holds two: the panel says so and links to Done.
	got := empty(SavedMessagesView{Locale: "en-US", Tab: "todo", TodoCount: 0, DoneCount: 2, AllCount: 3})
	for _, want := range []string{"Nothing left to do. Done holds 2.", `data-saved-tab="done"`, "Show Done"} {
		if !strings.Contains(got, want) {
			t.Errorf("an empty To do beside a Done of two misses %q: %s", want, got)
		}
	}
	if strings.Contains(got, firstUse) {
		t.Error("an empty To do beside a Done of two prints the first-use sentence")
	}
	// Done is empty and To do holds items.
	got = empty(SavedMessagesView{Locale: "en-US", Tab: "done", TodoCount: 4, DoneCount: 0, AllCount: 4})
	if !strings.Contains(got, "Nothing is done yet. To do holds 4.") || !strings.Contains(got, `data-saved-tab="todo"`) {
		t.Errorf("an empty Done beside a To do of four says the wrong thing: %s", got)
	}
	// Nothing was ever saved: the first-use sentence, and no link.
	got = empty(SavedMessagesView{Locale: "en-US", Tab: "todo"})
	if !strings.Contains(got, firstUse) || strings.Contains(got, "chatsave-empty-link") {
		t.Errorf("an empty Saved does not print the first-use sentence alone: %s", got)
	}
	// German and Arabic say it in their own words.
	for locale, want := range map[string]string{"de-DE": "Nichts mehr zu erledigen. Unter Erledigt: 2.", "ar": "لا شيء متبقٍ للتنفيذ."} {
		got = empty(SavedMessagesView{Locale: locale, Tab: "todo", DoneCount: 2, AllCount: 2})
		if !strings.Contains(got, want) {
			t.Errorf("%s: an empty To do beside a Done of two misses %q", locale, want)
		}
	}

	// One selected-row style: Saved and Moderation draw the tint and bar a
	// conversation draws, and the conversation row they cover is drawn plain.
	for _, want := range []string{
		`.chatsave-sidebar-row[aria-expanded="true"]`,
		`[data-chatremove-overlay="page"]) .chat-rail .chatmod005-row`,
		`:has(.chatsave-panel:not([hidden])) .chat-rail .chat-row.selected:not(.chatsave-sidebar-row)`,
		`box-shadow:inset 3px 0 0 var(--accent)`,
	} {
		if !strings.Contains(ChatBug091Styles, want) {
			t.Errorf("the selected-row rules miss %q", want)
		}
	}
}

// TestTodo_CHATUX_032: the panels beside the conversation share one header and
// one close button, and every primary button has one disabled look.
func TestTodo_CHATUX_032(t *testing.T) {
	header := renderNode(t, chatux032Header(chatux032HeaderProps{Class: "x", ID: "title", Title: "Saved", Subtitle: "2 to do", FocusHeading: true,
		Close: chatux032CloseButton("Close", false, map[string]string{"saved-action": "close"}, "")}))
	for _, want := range []string{`class="side-heading chat-panel-head x"`, `<h2 id="title" tabindex="-1">Saved</h2>`, `class="chat-panel-sub"`, "2 to do", `class="icon-button chat-panel-close"`, `aria-label="Close"`} {
		if !strings.Contains(header, want) {
			t.Errorf("the shared header misses %q: %s", want, header)
		}
	}
	if strings.Count(header, "chat-panel-close") != 1 {
		t.Errorf("the shared header does not hold exactly one close button: %s", header)
	}

	// Moderation and Saved use the component; so do the details and the person pane's classes.
	moderation := renderNode(t, moderationHeading("en-US"))
	if !strings.Contains(moderation, `chat-panel-head chatmod005-heading`) || !strings.Contains(moderation, `<h2 id="chatremove-title" tabindex="-1">`) || !strings.Contains(moderation, `data-chatremove-close="true"`) {
		t.Errorf("the Moderation header is not the shared one with a focusable title: %s", moderation)
	}
	saved := renderNode(t, RenderSavedMessages(SavedMessagesView{Locale: "en-US", TodoCount: 2, AllCount: 2}))
	if !strings.Contains(saved, "chat-panel-head chatsave-header") || strings.Count(saved, "chat-panel-close") != 1 {
		t.Errorf("the Saved header is not the shared one: %s", saved)
	}

	// The look: 52 px, a 32 px close button, a focus ring for the keyboard only,
	// and one disabled style whose text is the muted ink on the soft surface.
	for _, want := range []string{
		`height:52px`,
		`inline-size:32px`,
		`.chat-workspace .chat-panel-close:focus:not(:focus-visible){outline:none;box-shadow:none}`,
		`.chat-workspace .chat-panel-close:focus-visible{outline:2px solid var(--accent)`,
		`.send-button:disabled`,
		`.button:not(.secondary):disabled`,
		`background:var(--soft);border-color:var(--line);color:var(--muted);opacity:1`,
	} {
		if !strings.Contains(ChatUX032Styles, want) {
			t.Errorf("the shared panel styles miss %q", want)
		}
	}
	if !strings.Contains(Stylesheet, ChatUX032Styles) {
		t.Error("the shared panel styles are not in the stylesheet")
	}
}
