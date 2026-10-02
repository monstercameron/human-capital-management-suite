package chatui

import (
	stdhtml "html"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_CHATBUG_081 holds Delete message to one question before it deletes,
// and the notice bar to a slot that takes no room.
func TestTodo_CHATBUG_081(t *testing.T) {
	t.Run("the first press asks and the second deletes", func(t *testing.T) {
		for _, tc := range []struct {
			name, action, menu, asking string
			next                       string
			taken                      bool
		}{
			{"Delete in a menu that is not asking", "delete", "m1", "", "m1", true},
			{"Delete in the question", "delete", "m1", "m1", "", false},
			{"Delete in another menu while one asks", "delete", "m2", "m1", "m2", true},
			{"Delete with no menu open", "delete", "", "", "", false},
			{"Cancel, which closes the menu", "menu", "m1", "m1", "", false},
			{"opening a menu", "menu", "", "m1", "", false},
			{"any other press", "copy-link", "m1", "m1", "m1", false},
		} {
			if next, taken := chatbug081DeleteStep(tc.action, tc.menu, tc.asking); next != tc.next || taken != tc.taken {
				t.Errorf("%s: asking=%q taken=%v, want %q %v", tc.name, next, taken, tc.next, tc.taken)
			}
		}

		deleted := 0
		m := chatux022Model()
		m.MenuID = "m1"
		m.Messages = []Message{{ID: "m1", AuthorID: "walt", Body: "Text", Revision: 3}}
		m.Callbacks.DeleteMessage = func(id string, revision uint64) {
			if id == "m1" && revision == 3 {
				deleted++
			}
		}
		local := localStore{box: &localUI{}}
		if !chatbug081Press(m, local, "delete") || deleted != 0 || local.get().deleteAsk != "m1" {
			t.Fatalf("the first press on Delete message did not stop at the question: deleted=%d asking=%q", deleted, local.get().deleteAsk)
		}
		m.deleteAsk = chatbug081Settle(local, m.MenuID)
		if chatbug081Press(m, local, "delete") {
			t.Fatal("the press on the question's Delete message was swallowed")
		}
		m.act("delete", "m1")
		if deleted != 1 || local.get().deleteAsk != "" {
			t.Fatalf("the confirmed delete ran %d times and left asking=%q", deleted, local.get().deleteAsk)
		}
	})

	t.Run("the question replaces the menu's rows", func(t *testing.T) {
		m := chatux022Model()
		own := Message{ID: "m1", AuthorID: "walt", Body: "Text", Revision: 1}
		m.MenuID, m.deleteAsk = "m1", "m1"
		rows := strings.Join(chatux022Rows(t, m, own), " | ")
		if rows != "Cancel | Delete message" {
			t.Fatalf("the asking menu's rows = %q, want Cancel then Delete message", rows)
		}
		markup := stdhtml.UnescapeString(renderNode(t, spanOf(chatMessageMenuItems(m, own))))
		for _, want := range []string{">Delete this message? This cannot be undone.<", `data-action="menu" data-delete-ask="cancel" data-id="m1"`, `data-action="delete" data-delete-ask="confirm" data-id="m1"`, `aria-describedby="chat-delete-ask"`} {
			if !strings.Contains(markup, want) {
				t.Errorf("the question lacks %q: %s", want, markup)
			}
		}
		// The question is the menu it was asked in, and no other.
		m.MenuID = "m2"
		if rows := strings.Join(chatux022Rows(t, m, own), " | "); strings.HasPrefix(rows, "Cancel") {
			t.Fatalf("another message's menu shows the question: %s", rows)
		}
	})

	t.Run("a closed menu ends the question", func(t *testing.T) {
		local := localStore{box: &localUI{deleteAsk: "m1"}}
		if got := chatbug081Settle(local, "m1"); got != "m1" {
			t.Fatalf("the open menu lost its question: %q", got)
		}
		if got := chatbug081Settle(local, ""); got != "" || local.get().deleteAsk != "" {
			t.Fatalf("the question outlived its menu: %q", got)
		}
		local.box.deleteAsk = "m1"
		if got := chatbug081Settle(local, "m2"); got != "" {
			t.Fatalf("the question moved to another menu: %q", got)
		}
	})

	t.Run("the notice takes no room", func(t *testing.T) {
		m := chatux022Model()
		m.Notice = "We couldn't delete this message. The service reported an error. Try again."
		page, err := ui.RenderToString(Build(m))
		if err != nil {
			t.Fatal(err)
		}
		page = stdhtml.UnescapeString(page)
		slot := strings.Index(page, `class="chat-notice-slot"`)
		if slot < 0 || !strings.Contains(page[slot:slot+200], `class="chat-notice"`) {
			t.Fatalf("the notice is not inside its slot: %s", page[max(0, slot):min(len(page), max(0, slot)+300)])
		}
		if !strings.Contains(page, "We couldn't delete this message. The service reported an error. Try again.") {
			t.Fatal("the notice text is not on the page")
		}
		if got := chatbugCascadeValue(Stylesheet, ".chat-workspace .chat-notice-slot", "block-size"); got != "0" {
			t.Fatalf("the notice slot is %q tall, want 0", got)
		}
		if got := chatbugCascadeValue(Stylesheet, ".chat-workspace .chat-notice-slot>.chat-notice", "position"); got != "absolute" {
			t.Fatalf("the notice is %q inside its slot, want absolute", got)
		}
	})
}

// TestTodo_CHATBUG_081_Browser renders the question in the three languages
// with a catalog that answers every key with a bracketed key.
func TestTodo_CHATBUG_081_Browser(t *testing.T) {
	own := Message{ID: "m1", AuthorID: "walt", Body: "Text", Revision: 1}
	for locale, want := range map[string][2]string{
		"en-US": {"Delete this message? This cannot be undone.", "Cancel"},
		"de-DE": {"Diese Nachricht löschen? Das kann nicht rückgängig gemacht werden.", "Abbrechen"},
		"ar":    {"هل تريد حذف هذه الرسالة؟ لا يمكن التراجع عن ذلك.", "إلغاء"},
	} {
		m := chatux022Model()
		m.Locale = locale
		m.Text = func(key string) string { return "⟦" + key + "⟧" }
		m.MenuID, m.deleteAsk = "m1", "m1"
		markup := stdhtml.UnescapeString(renderNode(t, spanOf(chatMessageMenuItems(m, own))))
		if strings.Contains(markup, "⟦") {
			t.Errorf("%s: the question prints a copy key: %s", locale, markup)
		}
		if !strings.Contains(markup, ">"+want[0]+"<") || !strings.Contains(markup, ">"+want[1]+"<") {
			t.Errorf("%s: want %q and %q: %s", locale, want[0], want[1], markup)
		}
	}
}
