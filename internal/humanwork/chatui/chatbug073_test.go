package chatui

import (
	stdhtml "html"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func chatbug073Model() Model {
	at := time.Date(2026, 10, 2, 12, 55, 0, 0, time.UTC)
	return Model{
		State: StateReady, Locale: "en-US", SelectedID: "room", CurrentUser: "walt", CurrentTenantID: "tenant",
		Conversations: []Conversation{{ID: "room", Name: "general", Kind: PublicChannel, Joined: true}},
		Messages: []Message{
			{ID: "m1", AuthorID: "walt", Author: "Walt Brennan", Body: "First message", SentAt: at, Revision: 1},
			{ID: "m2", AuthorID: "walt", Author: "Walt Brennan", Body: "Line one\n\n- item a\n- item b EDITED", SentAt: at.Add(time.Minute), Revision: 2, Edited: true},
			{ID: "m3", AuthorID: "loretta", Author: "Loretta Haynes", Body: "Thanks", SentAt: at.Add(2 * time.Minute), Revision: 1},
		},
	}
}

func chatbug073Page(t *testing.T, m Model) string {
	t.Helper()
	page, err := ui.RenderToString(Build(m))
	if err != nil {
		t.Fatal(err)
	}
	return stdhtml.UnescapeString(page)
}

// TestTodo_CHATBUG_073 holds the edit box to the composer's keys, the word
// "edited" to its place after the text, and ArrowUp in an empty composer to the
// viewer's latest message.
func TestTodo_CHATBUG_073(t *testing.T) {
	t.Run("Enter saves and Shift+Enter is a line break", func(t *testing.T) {
		for _, tc := range []struct {
			key              string
			shift, composing bool
			want             bool
		}{
			{"Enter", false, false, true},
			{"Enter", true, false, false},
			{"Enter", false, true, false},
			{"a", false, false, false},
			{"Escape", false, false, false},
		} {
			if got := chatbug073EditSaves(tc.key, tc.shift, tc.composing); got != tc.want {
				t.Errorf("%s shift=%v composing=%v: saves=%v, want %v", tc.key, tc.shift, tc.composing, got, tc.want)
			}
		}
	})

	t.Run("ArrowUp in an empty composer edits the latest own message", func(t *testing.T) {
		opened := ""
		m := chatbug073Model()
		m.Callbacks.BeginEdit = func(id string) { opened = id }
		if !chatbug073ArrowUpEdits(m, "ArrowUp", "", false) || opened != "m2" {
			t.Fatalf("ArrowUp opened %q, want the viewer's latest message m2 (m3 is someone else's)", opened)
		}
		for name, tc := range map[string]struct {
			key, draft string
			modified   bool
			change     func(*Model)
		}{
			"a draft keeps the key":      {key: "ArrowUp", draft: "hel"},
			"a space is a draft":         {key: "ArrowUp", draft: " "},
			"another key":                {key: "ArrowDown"},
			"a held modifier":            {key: "ArrowUp", modified: true},
			"an edit already open":       {key: "ArrowUp", change: func(m *Model) { m.EditingID = "m1" }},
			"nothing of the viewer's":    {key: "ArrowUp", change: func(m *Model) { m.CurrentUser = "nobody" }},
			"no signed-in viewer":        {key: "ArrowUp", change: func(m *Model) { m.CurrentUser = "" }},
			"editing is not available":   {key: "ArrowUp", change: func(m *Model) { m.Callbacks.BeginEdit = nil }},
			"only an agent's own answer": {key: "ArrowUp", change: func(m *Model) { m.Messages = []Message{{ID: "a1", AuthorID: "walt", PersonaActor: &PersonaActor{}}} }},
		} {
			opened = ""
			next := chatbug073Model()
			next.Callbacks.BeginEdit = func(id string) { opened = id }
			if tc.change != nil {
				tc.change(&next)
			}
			if chatbug073ArrowUpEdits(next, tc.key, tc.draft, tc.modified) || opened != "" {
				t.Errorf("%s: ArrowUp opened %q, want nothing", name, opened)
			}
		}
		// The page hands messages over newest first as often as oldest first.
		reversed := chatbug073Model()
		reversed.Messages[0], reversed.Messages[2] = reversed.Messages[2], reversed.Messages[0]
		if latest, ok := chatbug073LatestOwn(reversed); !ok || latest.ID != "m2" {
			t.Fatalf("latest own message of a newest-first page = %q, want m2", latest.ID)
		}
	})

	t.Run("the edit box names its keys and starts at one row", func(t *testing.T) {
		m := chatbug073Model()
		m.EditingID = "m2"
		page := chatbug073Page(t, m)
		box := page[strings.Index(page, `id="edit-m2"`)-200 : strings.Index(page, `id="edit-m2"`)+400]
		if !strings.Contains(box, `rows="1"`) {
			t.Errorf("the edit box does not start at one row: %s", box)
		}
		if !strings.Contains(box, `aria-describedby="edit-hint-m2"`) {
			t.Errorf("the edit box is not described by its key hint: %s", box)
		}
		if !strings.Contains(page, `id="edit-hint-m2"`) || !strings.Contains(page, ">Enter to save, Shift+Enter for a new line, Esc to cancel<") {
			t.Error("the key hint is not under the edit box in English")
		}
		if strings.Contains(page, "message-edited") && strings.Contains(page[strings.Index(page, `data-message-id="m2"`):strings.Index(page, `data-message-id="m3"`)], "message-edited") {
			t.Error("the message open in the edit box still prints \"edited\"")
		}
		aria := chatbug073EditAria("m2", map[string]string{"describedby": "chatmod002-blocked-edit"})
		if aria["describedby"] != "chatmod002-blocked-edit edit-hint-m2" {
			t.Errorf("the moderation line was dropped from the box's description: %q", aria["describedby"])
		}
	})

	t.Run("edited follows the text", func(t *testing.T) {
		page := chatbug073Page(t, chatbug073Model())
		row := page[strings.Index(page, `data-message-id="m2"`):strings.Index(page, `data-message-id="m3"`)]
		// m2 is grouped under m1: the same author within five minutes.
		if !strings.Contains(page[strings.Index(page, `data-message-id="m2"`)-80:strings.Index(page, `data-message-id="m2"`)], "continued") {
			t.Fatalf("premise changed: m2 is not a grouped message: %s", row[:200])
		}
		text, mark := strings.Index(row, "item b EDITED"), strings.Index(row, `class="message-edited"`)
		if mark < 0 || text < 0 || mark < text {
			t.Fatalf("\"edited\" does not follow the message text (text at %d, mark at %d): %s", text, mark, row)
		}
		if !strings.Contains(row, ">(edited)<") {
			t.Errorf("the mark does not read (edited): %s", row)
		}
		meta := row[strings.Index(row, `class="message-meta"`):strings.Index(row, `class="message-body"`)]
		if strings.Contains(meta, "edited") {
			t.Errorf("\"edited\" is still printed above the text: %s", meta)
		}
		first := page[strings.Index(page, `data-message-id="m1"`):strings.Index(page, `data-message-id="m2"`)]
		if strings.Contains(first, "message-edited") {
			t.Error("a message that was never edited is marked edited")
		}
	})

	t.Run("the box grows with its text up to half the window", func(t *testing.T) {
		if got := chatbugCascadeValue(Stylesheet, ".chat-workspace .edit-input", "field-sizing"); got != "content" {
			t.Errorf("field-sizing = %q, want content", got)
		}
		if !strings.Contains(ChatBug073Styles, "max-block-size:50dvh") || chatbugCascadeValue(Stylesheet, ".chat-workspace .edit-input", "resize") != "none" {
			t.Error("the box is not capped at half the window, or still has a drag handle")
		}
		if got := editBoxHeight(81, 800); got != 81 {
			t.Errorf("an 81 px text in an 800 px window = %v, want 81", got)
		}
		if got := editBoxHeight(900, 800); got != 400 {
			t.Errorf("a 900 px text in an 800 px window = %v, want 400", got)
		}
	})
}
