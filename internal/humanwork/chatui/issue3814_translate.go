package chatui

import (
	"strings"
	"sync"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// Issue 3814: "Translate" in a message's More menu. A reader whose settings
// leave a message as written (a language they also read, automatic translation
// off) presses it for that one message. The press is remembered for this page
// session only and travels with the page's reads of the reading view as
// translate=<id>; the server decides, translates through the same engine, glossary
// and checks as every other translation, and never stores the result on the
// message. The words below say what happened, including a refusal.

var issue3814Asked struct {
	sync.Mutex
	ids map[string]bool
}

// ChatlangTranslateAsked reports whether the reader pressed Translate on a
// message in this page session.
func ChatlangTranslateAsked(id string) bool {
	issue3814Asked.Lock()
	defer issue3814Asked.Unlock()
	return issue3814Asked.ids[id]
}

// ChatlangTranslateAskedAmong returns, of the given message ids, those the
// reader pressed Translate on, in the order given.
func ChatlangTranslateAskedAmong(ids []string) []string {
	issue3814Asked.Lock()
	defer issue3814Asked.Unlock()
	var out []string
	for _, id := range ids {
		if issue3814Asked.ids[id] {
			out = append(out, id)
		}
	}
	return out
}

func chatlangTranslateAsk(id string) {
	issue3814Asked.Lock()
	defer issue3814Asked.Unlock()
	if issue3814Asked.ids == nil {
		issue3814Asked.ids = map[string]bool{}
	}
	issue3814Asked.ids[id] = true
}

// chatlangTranslateReset forgets every press (used by tests and on sign-out).
func chatlangTranslateReset() {
	issue3814Asked.Lock()
	issue3814Asked.ids = nil
	issue3814Asked.Unlock()
}

func issue3814Text(locale, key string) string {
	copy := map[string][3]string{
		"menu":           {"Translate", "Übersetzen", "ترجمة"},
		"menu_a":         {"Translate the message from {name}", "Nachricht von {name} übersetzen", "ترجمة رسالة {name}"},
		"refused_policy": {"This message can't be translated here.", "Diese Nachricht kann hier nicht übersetzt werden.", "لا يمكن ترجمة هذه الرسالة هنا."},
		"refused_same":   {"This message is already in a language you read.", "Diese Nachricht ist bereits in einer Sprache, die Sie lesen.", "هذه الرسالة مكتوبة بلغة تقرؤها بالفعل."},
	}
	if value, ok := copy[key]; ok {
		return chatbug039Text("chat.issue3814."+key, value[chatlangLocaleIndex(locale)], value[0])
	}
	return ""
}

// chatlangTranslateOffered: the menu offers Translate on someone else's
// message with text, in a conversation where the server composes renderings,
// until it is translated, a refusal was given, or a translation is on its way.
func chatlangTranslateOffered(m Model, msg Message) bool {
	if m.ChatFeatures == nil || !m.ChatFeatures.Renderings || msg.PersonaActor != nil {
		return false
	}
	if msg.AuthorID == "" || msg.AuthorID == m.CurrentUser || strings.TrimSpace(msg.Body) == "" {
		return false
	}
	if sel, ok := chatlangSelectionFor(m, msg); ok && sel.Mark.Refused != "" {
		return false
	}
	v := chatlangMessageOf(m, msg)
	return !v.Translated && !v.Pending
}

// chatlangTranslateMenuItems is the "Translate" entry of the More menu.
func chatlangTranslateMenuItems(m Model, msg Message) []ui.Node {
	if !chatlangTranslateOffered(m, msg) {
		return nil
	}
	label := issue3814Text(m.Locale, "menu")
	aria := strings.ReplaceAll(issue3814Text(m.Locale, "menu_a"), "{name}", chatlangAuthor(msg))
	return []ui.Node{html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Title: aria,
		Aria: map[string]string{"label": aria}, Data: map[string]string{"action": "chatlang-translate", "id": msg.ID}},
		icon("language"), html.Span(html.Props{Text: label}))}
}

// chatlangTranslateNote is the status line under a message the reader pressed
// Translate on, until the mark of the translation (or its failure) takes over:
// "Translating…" while the answer is on its way, and the refusal in words.
func chatlangTranslateNote(m Model, msg Message, v chatlangMessage) ui.Node {
	if !ChatlangTranslateAsked(msg.ID) || v.Translated || v.Pending || v.NotTranslated {
		return nil
	}
	state, key := "pending", "translating"
	if sel, ok := chatlangSelectionFor(m, msg); ok {
		switch sel.Mark.Refused {
		case chatrender.RefusedPolicy:
			state, key = "refused", "refused_policy"
		case chatrender.RefusedSameLanguage:
			state, key = "refused", "refused_same"
		}
	}
	text := ""
	if state == "pending" {
		text = chatlangText(m.Locale, key)
	} else {
		text = issue3814Text(m.Locale, key)
	}
	return html.P(html.Props{Class: "chatlang-mark chatlang-note", Role: "status", Data: map[string]string{"chatlang-note": state, "message-id": msg.ID}, Text: text})
}
