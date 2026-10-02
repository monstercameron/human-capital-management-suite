package chatui

import (
	"net/url"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-030: Conversation details says what each thing is in plain words. The
// pieces here are the joining row (never "Gate"), the member list collapsed to
// its first rows with a Show all button, and the heading of the developer
// disclosure under Integrations.

const (
	chatux030KeyGateAdmin   = "chat.ux030.gate_admin"
	chatux030KeyShowAll     = "chat.ux030.show_all"
	chatux030KeyShowFewer   = "chat.ux030.show_fewer"
	chatux030KeyDevelopers  = "chat.ux030.developers"
	chatux030MembersKey     = "members-all"
	chatux030MembersVisible = 8
)

var chatux030Copy = map[string]map[string]string{
	"en-US": {chatux030KeyGateAdmin: "Joining questions", chatux030KeyShowAll: "Show all {n}", chatux030KeyShowFewer: "Show fewer", chatux030KeyDevelopers: "For developers"},
	"de-DE": {chatux030KeyGateAdmin: "Beitrittsfragen", chatux030KeyShowAll: "Alle {n} anzeigen", chatux030KeyShowFewer: "Weniger anzeigen", chatux030KeyDevelopers: "Für Entwickler"},
	"ar":    {chatux030KeyGateAdmin: "أسئلة الانضمام", chatux030KeyShowAll: "عرض الكل ({n})", chatux030KeyShowFewer: "عرض أقل", chatux030KeyDevelopers: "للمطورين"},
}

// chatux030Gate is the joining row of the panel: one row of the panel's shape
// that names what it opens. Whoever administers the channel reads "Joining
// questions"; a member reads "My answers". It stays the link the Chat client
// listens for (data-gate-open), and is absent where the gate section is.
func chatux030Gate(m Model) ui.Node {
	if chatgateDetailsSection(m) == nil {
		return nil
	}
	c := m.selected()
	label := GateText(m.Locale, "answers")
	if canAdministerConversation(m, c) {
		label = laneText(m, chatux030Copy, chatux030KeyGateAdmin)
	}
	return html.Section(html.Props{Class: "details-section details-gate"},
		html.A(html.Props{Href: "#gate=" + url.QueryEscape(c.ID), Text: label, Dir: "auto", Data: map[string]string{"gate-open": c.ID}}))
}

// chatux030PeopleWindow keeps the first rows of a long member list and offers
// the rest behind a button. A filtered list is never cut: whoever typed a name
// wants every match.
func chatux030PeopleWindow(m Model, h handlers, rows []ui.Node, filtered bool) ([]ui.Node, ui.Node) {
	if filtered || len(rows) <= chatux030MembersVisible {
		return rows, nil
	}
	all := h.local.detailGroups[chatux030MembersKey]
	label := laneTextf(m, chatux030Copy, chatux030KeyShowAll, map[string]string{"n": strings.TrimSpace(m.n(len(rows)))})
	if all {
		label = laneText(m, chatux030Copy, chatux030KeyShowFewer)
	} else {
		rows = rows[:chatux030MembersVisible]
	}
	return rows, html.Button(html.Props{Class: "button secondary small details-members-more", Type: "button", Text: label,
		Data: map[string]string{"action": "details-group", "id": chatux030MembersKey}, Aria: map[string]string{"expanded": boolString(all)}})
}
