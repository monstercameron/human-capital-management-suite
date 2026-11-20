package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// #3919: a message can be shared to a channel, a direct message or a group,
// with a note of the sharer's own. The dialog says exactly who will see what
// before Share, nothing is sent until Share is pressed, and a refusal keeps the
// note. In the destination the note reads as the sharer's, above a copy that
// stays attributed to its original author.

// ShareNoteMaxRunes is the longest note the server accepts.
const ShareNoteMaxRunes = 1000

// ShareDestinationEligible reports whether a conversation may be offered as a
// share destination from the room the message is in. Agent conversations are
// never offered: what a person shares there is not a private answer to hand on.
func ShareDestinationEligible(room Conversation, sourceID string) bool {
	if room.ID == "" || room.ID == sourceID || room.Agent {
		return false
	}
	switch room.Kind {
	case PublicChannel, PrivateChannel, DirectMessage, GroupChat:
		return true
	}
	return false
}

func issue3919Text(m Model, key string) string {
	copy := map[string][3]string{
		"note.label":       {"Add a note (optional)", "Notiz hinzufügen (optional)", "أضف ملاحظة (اختياري)"},
		"note.placeholder": {"Say why you are sharing this", "Sagen Sie, warum Sie das teilen", "اذكر سبب مشاركة هذا"},
		"note.kept":        {"Nothing was sent. Your note is still here.", "Es wurde nichts gesendet. Ihre Notiz ist noch da.", "لم يُرسَل شيء. ملاحظتك ما زالت هنا."},
		"summary.title":    {"What will be sent", "Was gesendet wird", "ما الذي سيُرسل"},
		"summary.who":      {"Who will see it", "Wer es sieht", "من سيراها"},
		"summary.note":     {"Your note", "Ihre Notiz", "ملاحظتك"},
		"summary.nonote":   {"No note", "Keine Notiz", "بلا ملاحظة"},
		"summary.copy":     {"Copy of the message", "Kopie der Nachricht", "نسخة من الرسالة"},
		"summary.source":   {"{author}, {time}. A copy of the text only. It does not give anyone access to the original conversation or its files.", "{author}, {time}. Nur eine Kopie des Textes. Sie gibt niemandem Zugriff auf die ursprüngliche Unterhaltung oder ihre Dateien.", "{author}، {time}. نسخة من النص فقط. لا تمنح أحدًا الوصول إلى المحادثة الأصلية أو ملفاتها."},
		"summary.nothing":  {"Choose where to share this. Nothing is sent until you press Share.", "Wählen Sie, wo dies geteilt werden soll. Es wird nichts gesendet, bevor Sie auf Teilen klicken.", "اختر مكان المشاركة. لا يُرسَل شيء حتى تضغط على مشاركة."},
		"aud.public":       {"Everyone in {name}", "Alle in {name}", "الجميع في {name}"},
		"aud.public.n":     {"Everyone in {name}, {n} people", "Alle in {name}, {n} Personen", "الجميع في {name}، {n} من الأعضاء"},
		"aud.private":      {"Members of {name}", "Mitglieder von {name}", "أعضاء {name}"},
		"aud.private.n":    {"Members of {name}, {n} people", "Mitglieder von {name}, {n} Personen", "أعضاء {name}، {n} من الأعضاء"},
		"aud.direct":       {"Only {name}", "Nur {name}", "{name} فقط"},
		"aud.group":        {"Everyone in the group {name}", "Alle in der Gruppe {name}", "الجميع في المجموعة {name}"},
		"aud.group.n":      {"The {n} people in {name}", "Die {n} Personen in {name}", "الأشخاص الـ{n} في {name}"},
		"msg.note_from":    {"Note from {name}: ", "Notiz von {name}: ", "ملاحظة من {name}: "},
	}
	return chatbug039Text(key, copy[key][chatbug039LocaleIndex(m.Locale)], copy[key][0])
}

func issue3919Fill(text string, values map[string]string) string {
	for name, value := range values {
		text = strings.ReplaceAll(text, "{"+name+"}", value)
	}
	return text
}

// issue3919Audience says who will read the share, in words that match the
// kind of conversation chosen.
func issue3919Audience(m Model, room Conversation) string {
	values := map[string]string{"name": room.Name, "n": m.n(room.MemberCount)}
	counted := room.MemberCount > 1
	pick := func(plain, withCount string) string {
		if counted {
			return issue3919Fill(issue3919Text(m, withCount), values)
		}
		return issue3919Fill(issue3919Text(m, plain), values)
	}
	switch room.Kind {
	case DirectMessage:
		return issue3919Fill(issue3919Text(m, "aud.direct"), values)
	case GroupChat:
		return pick("aud.group", "aud.group.n")
	case PrivateChannel:
		return pick("aud.private", "aud.private.n")
	}
	return pick("aud.public", "aud.public.n")
}

func issue3919Destination(m Model) (Conversation, bool) {
	for _, room := range m.ShareDestinations {
		if room.ID == m.ShareDestinationID && m.ShareDestinationID != "" {
			return room, true
		}
	}
	return Conversation{}, false
}

// issue3919NoteField is the optional note, labelled, with its length limit.
func issue3919NoteField(m Model, h handlers) ui.Node {
	return html.Div(html.Props{Class: "share-note"},
		html.Label(html.Props{For: "share-note", Text: issue3919Text(m, "note.label")}),
		html.Textarea(html.Props{ID: "share-note", Class: "chat-input", Rows: 3, MaxLength: ShareNoteMaxRunes, Dir: "auto", Placeholder: issue3919Text(m, "note.placeholder"), Data: map[string]string{"chat-value": m.ShareNote}, Disabled: m.SharePending, OnInput: h.shareNote}, ui.Text(m.ShareNote)))
}

// issue3919Summary is the exact audience, note and source, as a polite status
// region so a screen reader hears it change with the choice. The note line is
// not live: it changes with every key typed, and the person typing it is
// already hearing what they type.
func issue3919Summary(m Model) ui.Node {
	room, ok := issue3919Destination(m)
	if !ok {
		return html.Div(html.Props{Class: "share-summary-slot", Role: "status"},
			html.P(html.Props{Class: "share-disclosure", Text: issue3919Text(m, "summary.nothing")}))
	}
	note := strings.TrimSpace(m.ShareNote)
	noteText := issue3919Text(m, "summary.nonote")
	if note != "" {
		noteText = note
	}
	facts := []ui.Node{
		html.Tag("dt", html.Props{Text: issue3919Text(m, "summary.who")}), html.Tag("dd", html.Props{Dir: "auto", Text: issue3919Audience(m, room)}),
		html.Tag("dt", html.Props{Text: issue3919Text(m, "summary.note")}), html.Tag("dd", html.Props{Class: "share-summary-note", Dir: "auto", Aria: map[string]string{"live": "off"}, Text: noteText}),
	}
	if msg := m.ShareSource; msg != nil {
		source := issue3919Fill(issue3919Text(m, "summary.source"), map[string]string{"author": msg.Author, "time": msg.TimeLabel})
		facts = append(facts, html.Tag("dt", html.Props{Text: issue3919Text(m, "summary.copy")}), html.Tag("dd", html.Props{Dir: "auto", Text: source}))
	}
	return html.Div(html.Props{Class: "share-summary-slot", Role: "status"},
		html.Div(html.Props{Class: "share-summary", Role: "group", Aria: map[string]string{"label": issue3919Text(m, "summary.title")}},
			html.Strong(html.Props{Text: issue3919Text(m, "summary.title")}),
			html.Tag("dl", html.Props{}, facts...)))
}

// issue3919Kept says a refusal changed nothing and kept the note.
func issue3919Kept(m Model) ui.Node {
	if m.ShareError == "" || strings.TrimSpace(m.ShareNote) == "" {
		return nil
	}
	return html.P(html.Props{Class: "share-caution", Text: issue3919Text(m, "note.kept")})
}

// issue3919ForwardNote is the sharer's note above the copy in a timeline. The
// message's author is the sharer; the copy keeps its original author's badge.
func issue3919ForwardNote(m Model, msg Message) ui.Node {
	note := strings.TrimSpace(msg.ForwardNote)
	if note == "" {
		return nil
	}
	return html.P(html.Props{Class: "forward-note", Dir: "auto"},
		html.Span(html.Props{Class: "sr-only", Text: issue3919Fill(issue3919Text(m, "msg.note_from"), map[string]string{"name": msg.Author})}),
		ui.Text(note))
}

const issue3919Styles = `.share-note{display:grid;gap:4px}.share-note textarea{resize:vertical;min-block-size:64px}` +
	`.share-summary{display:grid;gap:6px;padding:10px 12px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--soft)}` +
	`.share-summary dl{display:grid;grid-template-columns:max-content minmax(0,1fr);gap:2px 12px;margin:0;font-size:.8125rem;line-height:1.4}.share-summary dt{color:var(--muted)}.share-summary dd{margin:0;min-width:0;overflow-wrap:anywhere}.share-summary-note{white-space:pre-wrap}` +
	`.forward-note{margin:0 0 6px;padding-inline-start:10px;border-inline-start:3px solid var(--accent);white-space:pre-wrap;overflow-wrap:anywhere}` +
	`@media(max-width:480px){.share-summary dl{grid-template-columns:minmax(0,1fr)}.share-summary dd{margin-block-end:4px}}`
