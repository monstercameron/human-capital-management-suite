package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// CHATLANG-004: reading and writing across languages. The server's selection
// for this reader decides what is shown; everything here only says so: a small
// mark in words, the original one press away, each text carrying its own
// language and direction, and one muted line under the composer. The text the
// server delivered is never hidden: a pending, slow or failed translation
// leaves the message as written.

// ChatlangAudience is the server's answer to "who will read this in another
// language": per language, how many other people read a translation. It is
// counts only.
type ChatlangAudience struct {
	Language string         `json:"language"`
	Offered  bool           `json:"offered"`
	Readers  map[string]int `json:"readers"`
}

// ChatlangModel is the page's view of the conversation's languages.
type ChatlangModel struct {
	Audience ChatlangAudience
	// AudienceRoom is the conversation Audience was read for: an answer for
	// another conversation is never shown.
	AudienceRoom string
}

// chatlangLocal is the presentation state of this page session: which
// originals the reader opened, whether the conversation shows them all, and
// which of their own messages has its language picker open.
type chatlangLocal struct {
	original map[string]bool
	all      map[string]bool
	fixing   string
	busy     bool
	failed   bool
}

func (l chatlangLocal) originalShown(room, id string) bool { return l.all[room] || l.original[id] }

// chatlangKnownTag is a language tag worth putting in a lang attribute: one
// the product reads, and neither "no language" nor "several".
func chatlangKnownTag(tag string) string {
	tag = chatrender.Language(tag)
	if tag == "" || tag == "und" || tag == "mul" || !chatrender.Supported(tag) {
		return ""
	}
	return tag
}

// chatlangDir is the direction a language is written in: right to left for
// Arabic, left to right for the rest, and "auto" when the language is not known.
func chatlangDir(tag string) string {
	switch chatrender.Language(tag) {
	case "":
		return "auto"
	case "ar", "he", "fa", "ur":
		return "rtl"
	}
	return "ltr"
}

// chatlangMessage is what the server's selection says about one message for
// this reader.
type chatlangMessage struct {
	Known         bool
	Translated    bool
	Reworded      bool
	Pending       bool
	NotTranslated bool
	BodyLang      string
	SourceLang    string
	CanOriginal   bool
}

// chatlangSelectionFor returns the server's selection for exactly this
// revision of the message; a selection for an older revision is never used.
func chatlangSelectionFor(m Model, msg Message) (ReaderSelection, bool) {
	sel, ok := m.ReaderSelections[msg.ID]
	if !ok {
		return ReaderSelection{}, false
	}
	if sel.Rendering.Message != "" {
		revision := sel.Rendering.Revision
		return sel, sel.Rendering.Message == msg.ID && (revision == msg.Revision || (msg.Revision == 0 && revision == 1))
	}
	return sel, sel.Revision == msg.Revision || sel.Revision == 0
}

func chatlangMessageOf(m Model, msg Message) chatlangMessage {
	var v chatlangMessage
	if msg.PersonaActor != nil {
		return v
	}
	sel, ok := chatlangSelectionFor(m, msg)
	if !ok {
		return v
	}
	mark := sel.Mark
	v.Known = true
	v.SourceLang = chatlangKnownTag(mark.SourceLanguage)
	v.BodyLang = v.SourceLang
	asked := false
	for _, kind := range mark.Wanted {
		asked = asked || kind == chatrender.Translate
	}
	switch mark.State {
	case "ready":
		for _, kind := range mark.Kinds {
			if kind == chatrender.Translate {
				v.Translated = true
			}
			if kind == chatrender.Reword {
				v.Reworded = true
			}
		}
		if v.Translated {
			v.BodyLang = chatlangKnownTag(sel.Rendering.Language)
			v.CanOriginal = mark.CanShowOriginal && msg.Body != ""
		}
	case "pending":
		v.Pending = asked && mark.CanShowOriginal
	case "fallback":
		v.NotTranslated = asked
	}
	return v
}

// chatlangBodyProps puts the language and direction of the text that is shown
// on the element that holds it, so a screen reader pronounces it in the right
// voice and right-to-left text is laid out right inside a left-to-right page
// and the reverse. A message whose language is not known keeps "auto".
func chatlangBodyProps(m Model, msg Message, props html.Props) html.Props {
	if lang := chatlangMessageOf(m, msg).BodyLang; lang != "" {
		props.Lang, props.Dir = lang, chatlangDir(lang)
	}
	return props
}

func chatlangAuthor(msg Message) string {
	if msg.Author != "" {
		return msg.Author
	}
	return msg.AuthorID
}

// chatlangExtras are the nodes that follow a message's text: its mark, the
// original beneath the translation when asked for, and the language picker on
// the writer's own message.
func chatlangExtras(m Model, h handlers, msg Message) []ui.Node {
	if m.EditingID == msg.ID {
		return nil
	}
	v := chatlangMessageOf(m, msg)
	local := h.local.chatlang
	var nodes []ui.Node
	if v.Translated || v.Pending || v.NotTranslated {
		text := ""
		switch {
		case v.Translated:
			text = strings.ReplaceAll(chatlangText(m.Locale, "translated"), "{language}", chatlangLanguageName(m.Locale, v.SourceLang))
			if v.SourceLang == "" {
				text = chatlangText(m.Locale, "translated")
				text = strings.ReplaceAll(text, " {language}", "")
			}
		case v.Pending:
			text = chatlangText(m.Locale, "translating")
		default:
			text = chatlangText(m.Locale, "not_translated")
		}
		state := "translated"
		if v.Pending {
			state = "pending"
		} else if v.NotTranslated {
			state = "failed"
		}
		parts := []ui.Node{html.Span(html.Props{Class: "chatlang-mark-text", Text: text})}
		if v.Translated && v.Reworded {
			// Both apply: the reader has the reworded text in their language and both marks.
			parts = []ui.Node{html.Span(html.Props{Class: "chatlang-mark-text", Text: RenderingText(m.Locale, "reworded")}), html.Span(html.Props{Class: "chatlang-mark-text", Text: text})}
		}
		shown := local.originalShown(m.SelectedID, msg.ID)
		if v.CanOriginal {
			label, aria := "show_original", "show_original_a"
			if shown {
				label, aria = "hide_original", "hide_original_a"
			}
			parts = append(parts, html.Button(html.Props{Class: "chatlang-mark-button", Type: "button",
				Data: map[string]string{"action": "chatlang-original", "id": msg.ID},
				Aria: map[string]string{"expanded": boolString(shown), "controls": "chatlang-original-" + msg.ID, "label": strings.ReplaceAll(chatlangText(m.Locale, aria), "{name}", chatlangAuthor(msg))},
				Text: chatlangText(m.Locale, label)}))
		}
		nodes = append(nodes, html.Div(html.Props{Class: "chatlang-mark", Data: map[string]string{"chatlang-state": state, "message-id": msg.ID}}, parts...))
		if v.CanOriginal && shown {
			label := chatlangText(m.Locale, "original")
			if v.SourceLang != "" {
				label = strings.ReplaceAll(chatlangText(m.Locale, "original_in"), "{language}", chatlangLanguageName(m.Locale, v.SourceLang))
			}
			textProps := html.Props{Class: "chatlang-original-text", Dir: "auto"}
			if v.SourceLang != "" {
				textProps.Lang, textProps.Dir = v.SourceLang, chatlangDir(v.SourceLang)
			}
			nodes = append(nodes, html.Div(html.Props{ID: "chatlang-original-" + msg.ID, Class: "chatlang-original", Data: map[string]string{"chatlang-original": msg.ID}},
				html.Span(html.Props{Class: "chatlang-original-label", Text: label}),
				html.Div(textProps, markdownMessageBody(m, msg.Body)...)))
		}
	}
	if local.fixing == msg.ID && msg.AuthorID == m.CurrentUser {
		nodes = append(nodes, chatlangFixer(m, local, msg, v))
	}
	return nodes
}

// chatlangFixer is the writer's correction of a wrong detection: the language
// of their own message, one press each. "No language" says the message is not
// to be translated at all.
func chatlangFixer(m Model, local chatlangLocal, msg Message, v chatlangMessage) ui.Node {
	options := []ui.Node{}
	for _, tag := range []string{"en", "de", "fr", "es", "pt", "ar", "ja", "hi", "und"} {
		name := chatlangText(m.Locale, "fix_none")
		if tag != "und" {
			name = chatlangLanguageName(m.Locale, tag)
		}
		current := tag == v.SourceLang || (tag == "und" && v.SourceLang == "")
		options = append(options, html.Button(html.Props{Class: "chatlang-fix-option", Type: "button", Disabled: local.busy,
			Data: map[string]string{"action": "chatlang-correct", "id": msg.ID, "extra": tag},
			Aria: map[string]string{"pressed": boolString(current)}, Text: name}))
	}
	status := ui.Node(nil)
	switch {
	case local.busy:
		status = html.P(html.Props{Class: "chatlang-fix-status", Role: "status", Text: chatlangText(m.Locale, "fix_saving")})
	case local.failed:
		status = html.P(html.Props{Class: "chatlang-fix-status", Role: "alert", Text: chatlangText(m.Locale, "fix_failed")})
	}
	children := []ui.Node{
		html.Span(html.Props{Class: "chatlang-fix-label", Text: chatlangText(m.Locale, "fix_prompt")}),
		html.Div(html.Props{Class: "chatlang-fix-options"}, options...),
		html.Button(html.Props{Class: "chatlang-mark-button", Type: "button", Disabled: local.busy, Data: map[string]string{"action": "chatlang-fix-cancel", "id": msg.ID}, Text: chatlangText(m.Locale, "fix_cancel")}),
	}
	if status != nil {
		children = append(children, status)
	}
	return html.Div(html.Props{Class: "chatlang-fix", Role: "group", Aria: map[string]string{"label": chatlangText(m.Locale, "fix_group")}}, children...)
}

// chatlangMenuItems is the writer's "Message language…" entry in the More menu
// of their own message, offered once the server has answered for it.
func chatlangMenuItems(m Model, msg Message) []ui.Node {
	if msg.AuthorID == "" || msg.AuthorID != m.CurrentUser || m.ChatFeatures == nil || !m.ChatFeatures.Renderings || !chatlangMessageOf(m, msg).Known {
		return nil
	}
	return []ui.Node{html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Title: chatlangText(m.Locale, "fix_menu"),
		Data: map[string]string{"action": "chatlang-fix", "id": msg.ID}}, icon("info"), html.Span(html.Props{Text: chatlangText(m.Locale, "fix_menu")}))}
}

// chatlangBarShown: the conversation has a message that is translated, being
// translated, or could not be, so the switch and the settings are one press
// away. A conversation in one language never shows it.
func chatlangBarShown(m Model) bool {
	if m.ChatFeatures == nil || !m.ChatFeatures.Renderings || m.SelectedID == "" {
		return false
	}
	for _, msg := range m.Messages {
		if v := chatlangMessageOf(m, msg); v.Translated || v.Pending || v.NotTranslated {
			return true
		}
	}
	return false
}

// chatlangBar is the conversation-level switch "Show originals" and the way to
// the personal language settings.
func chatlangBar(m Model, h handlers) ui.Node {
	if !chatlangBarShown(m) {
		return nil
	}
	shown := h.local.chatlang.all[m.SelectedID]
	return html.Div(html.Props{Class: "chatlang-bar", Role: "group", Aria: map[string]string{"label": chatlangText(m.Locale, "bar_label")}},
		html.Span(html.Props{Class: "chatlang-bar-text", Text: chatlangText(m.Locale, "bar")}),
		html.Button(html.Props{Class: "chatlang-bar-button", Type: "button", Data: map[string]string{"action": "chatlang-originals", "id": m.SelectedID},
			Aria: map[string]string{"pressed": boolString(shown)}, Text: chatlangText(m.Locale, "bar_originals")}),
		html.Button(html.Props{Class: "chatlang-bar-button", Type: "button", Data: map[string]string{"action": "chatlang-settings"},
			Aria: map[string]string{"haspopup": "dialog"}, Text: chatlangText(m.Locale, "bar_settings")}))
}

// chatlangComposerLine is the one muted line under the message box saying who
// will read a translation. It is not drawn when everyone reads the writer's
// language, when the server has not answered, or for another conversation.
func chatlangComposerLine(m Model) ui.Node {
	if m.ChatFeatures == nil || !m.ChatFeatures.Renderings || m.Chatlang.AudienceRoom != m.SelectedID || m.SelectedID == "" {
		return nil
	}
	line := ChatlangAudienceLine(m, m.Chatlang.Audience)
	if line == "" {
		return nil
	}
	return html.P(html.Props{Class: "chatlang-audience", Data: map[string]string{"chatlang-audience": "true"}, Text: line})
}
