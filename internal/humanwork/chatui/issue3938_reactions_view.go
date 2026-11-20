package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3938: see who used a reaction without changing your own.
//
// The reaction chip toggles the viewer's reaction, so a reader who only wants
// to know who responded had no safe control. View reactions is its own button
// on the reaction row; it opens a dialog that is read-only (it carries no
// toggle) and lists, for each emoji, the people the message was given. The
// people are the ones the server already sent with the message, so the list
// follows the conversation's visibility, membership and reaction privacy: it
// never names anyone the chip itself did not carry. It is drawn from the
// current message every render, so a message that left the view, or lost its
// reactions, is shown as such rather than from a stale copy. People whose name
// is unknown or who are no longer members are counted, never shown as ids.

const (
	issue3938ViewAction  = "view-reactions"
	issue3938CloseAction = "reactions-close"
	// issue3938PageSize bounds the names listed under one emoji.
	issue3938PageSize = 25

	keyIssue3938View        = "chat.issue3938.view"
	keyIssue3938Title       = "chat.issue3938.title"
	keyIssue3938Note        = "chat.issue3938.note"
	keyIssue3938More        = "chat.issue3938.more"
	keyIssue3938Unavailable = "chat.issue3938.unavailable"
	keyIssue3938Empty       = "chat.issue3938.empty"
)

var issue3938Copy = map[string]map[string]string{
	"en-US": {
		keyIssue3938View:        "View reactions",
		keyIssue3938Title:       "Who reacted",
		keyIssue3938Note:        "A reaction is a quick response. It does not mean anyone approved or acknowledged the message.",
		keyIssue3938More:        "{n} more not listed here",
		keyIssue3938Unavailable: "These reactions are no longer available.",
		keyIssue3938Empty:       "No reactions yet.",
	},
	"de-DE": {
		keyIssue3938View:        "Reaktionen ansehen",
		keyIssue3938Title:       "Wer reagiert hat",
		keyIssue3938Note:        "Eine Reaktion ist eine schnelle Antwort. Sie bedeutet nicht, dass jemand die Nachricht genehmigt oder bestätigt hat.",
		keyIssue3938More:        "{n} weitere hier nicht aufgeführt",
		keyIssue3938Unavailable: "Diese Reaktionen sind nicht mehr verfügbar.",
		keyIssue3938Empty:       "Noch keine Reaktionen.",
	},
	"ar": {
		keyIssue3938View:        "عرض التفاعلات",
		keyIssue3938Title:       "من تفاعل",
		keyIssue3938Note:        "التفاعل رد سريع. لا يعني أن أحدًا وافق على الرسالة أو أقرّ بها.",
		keyIssue3938More:        "{n} آخرون غير مدرجين هنا",
		keyIssue3938Unavailable: "هذه التفاعلات لم تعد متاحة.",
		keyIssue3938Empty:       "لا توجد تفاعلات بعد.",
	},
}

// Issue3938Styles keeps the dialog's lists tidy; the button is a reaction chip.
const Issue3938Styles = `.reactions-dialog .reactions-group{margin:0 0 14px}` +
	`.reactions-dialog .reactions-group h3{display:flex;align-items:center;gap:8px;margin:0 0 6px;font-size:.9375rem}` +
	`.reactions-dialog .reactions-people{margin:0;padding:0;list-style:none}` +
	`.reactions-dialog .reactions-people li{padding:4px 0;overflow-wrap:anywhere}` +
	`.reactions-dialog .reactions-more,.reactions-dialog .reactions-note{margin:4px 0 0;color:var(--hcm-color-text-muted);font-size:.8125rem}`

// issue3938ViewButton is the explicit View reactions control on the reaction row.
func issue3938ViewButton(m Model, msg Message) ui.Node {
	label := laneText(m, issue3938Copy, keyIssue3938View)
	return html.Button(html.Props{Class: "reaction view-reactions", Type: "button",
		Data: map[string]string{"action": issue3938ViewAction, "id": msg.ID},
		Aria: map[string]string{"label": label, "haspopup": "dialog"}, Title: label}, icon("people"))
}

// issue3938Action applies a press of the two controls to the local state and
// reports whether it was taken. Opening changes no reaction.
func issue3938Action(local localStore, action, id string) bool {
	switch action {
	case issue3938ViewAction:
		local.update(func(u *localUI) { u.reactionsFor = id })
		return true
	case issue3938CloseAction:
		local.update(func(u *localUI) { u.reactionsFor = "" })
		return true
	}
	return false
}

func issue3938Message(m Model, id string) (Message, bool) {
	for _, list := range [][]Message{m.Messages, m.ThreadMessages} {
		for _, msg := range list {
			if msg.ID == id {
				return msg, true
			}
		}
	}
	if m.ThreadParent != nil && m.ThreadParent.ID == id {
		return *m.ThreadParent, true
	}
	return Message{}, false
}

// issue3938Dialog is the read-only list of who reacted, from the message as it
// stands in this render.
func issue3938Dialog(m Model, id string) ui.Node {
	title := laneText(m, issue3938Copy, keyIssue3938Title)
	body := []ui.Node{}
	msg, ok := issue3938Message(m, id)
	shown := 0
	if ok {
		for _, chip := range msg.Chips {
			if chip.Count <= 0 {
				continue
			}
			shown++
			body = append(body, issue3938Group(m, chip))
		}
	}
	switch {
	case !ok:
		body = append(body, html.P(html.Props{Role: "status", Dir: "auto", Text: laneText(m, issue3938Copy, keyIssue3938Unavailable)}))
	case shown == 0:
		body = append(body, html.P(html.Props{Role: "status", Dir: "auto", Text: laneText(m, issue3938Copy, keyIssue3938Empty)}))
	default:
		body = append(body, html.P(html.Props{Class: "reactions-note", Dir: "auto", Text: laneText(m, issue3938Copy, keyIssue3938Note)}))
	}
	head := html.Div(html.Props{Class: "side-heading"}, html.H2(html.Props{Text: title}),
		actionButton("icon-button", issue3938CloseAction, "", m.t(KeyClose), false, icon("close")))
	return html.Div(html.Props{Class: "chat-dialog-backdrop"}, html.Dialog(html.Props{Class: "chat-dialog reactions-dialog", Open: true, Role: "dialog", Dir: m.Direction,
		Aria: map[string]string{"modal": "true", "label": title}}, append([]ui.Node{head}, body...)...))
}

func issue3938Group(m Model, chip ReactionChip) ui.Node {
	names := make([]string, 0, len(chip.PeopleIDs)+1)
	if chip.Mine {
		names = append(names, laneText(m, chatux021Copy, keyChatux019You))
	}
	for _, id := range chip.PeopleIDs {
		if name := strings.TrimSpace(chatKnownName(m, id)); name != "" {
			names = append(names, name)
		}
	}
	if len(names) > chip.Count {
		names = names[:chip.Count]
	}
	listed := names
	if len(listed) > issue3938PageSize {
		listed = listed[:issue3938PageSize]
	}
	items := make([]ui.Node, 0, len(listed))
	for _, name := range listed {
		items = append(items, html.Li(html.Props{Dir: "auto", Text: name}))
	}
	heading := html.H3(html.Props{}, chatEmojiGlyphNode(m, chip.Emoji),
		html.Span(html.Props{Dir: "auto", Text: chatux019EmojiName(m, chip.Emoji) + " · " + m.n(chip.Count)}))
	nodes := []ui.Node{heading, html.Ul(html.Props{Class: "reactions-people"}, items...)}
	if rest := chip.Count - len(listed); rest > 0 {
		nodes = append(nodes, html.P(html.Props{Class: "reactions-more", Dir: "auto", Text: laneTextf(m, issue3938Copy, keyIssue3938More, map[string]string{"n": m.n(rest)})}))
	}
	return html.Div(html.Props{Class: "reactions-group"}, nodes...)
}
