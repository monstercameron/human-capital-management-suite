package chatui

import (
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATGATE-014: Conversation details lists the labels a channel's joining
// questions gave people and who holds each. A label is a name and nothing more,
// so the list says so; the server answers only people who are in the channel, and
// only with labels that come from questions shown to members.

// GateLabel is one label and the people who hold it, by subject identifier.
type GateLabel struct {
	Name   string
	People []string
}

const (
	chatgate014KeyHeading = "chat.gate014.heading"
	chatgate014KeyNote    = "chat.gate014.note"

	// chatgate014HoldersShown is how many names a label shows before the rest are
	// behind Show all.
	chatgate014HoldersShown = 5
)

var chatgate014Copy = map[string]map[string]string{
	"en-US": {chatgate014KeyHeading: "Labels", chatgate014KeyNote: "From the answers people gave when they joined. A label is only a name: it gives no access."},
	"de-DE": {chatgate014KeyHeading: "Labels", chatgate014KeyNote: "Aus den Antworten beim Beitritt. Ein Label ist nur ein Name und gibt keinen Zugriff."},
	"ar":    {chatgate014KeyHeading: "التسميات", chatgate014KeyNote: "من إجابات الأشخاص عند الانضمام. التسمية مجرد اسم ولا تمنح أي صلاحية."},
}

// chatgate014Holders names the people of one label who are in the conversation,
// sorted by the name shown. A person the member list does not hold is left out,
// so a stale answer from the server never names somebody who has left.
func chatgate014Holders(m Model, label GateLabel) []string {
	names := map[string]string{}
	for _, p := range m.Members {
		if p.Agent {
			continue
		}
		name := p.Name
		if name == "" || name == p.ID {
			name = m.t(KeyTodoMemberFallback)
		}
		if p.ID != "" && p.ID == m.CurrentUser {
			name += " (" + m.t(KeyYou) + ")"
		}
		names[p.ID] = name
	}
	out := []string{}
	for _, id := range label.People {
		if name, ok := names[id]; ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// chatgate014Labels is the Labels section of Conversation details, absent when
// the channel has none.
func chatgate014Labels(m Model, h handlers) ui.Node {
	c := m.selected()
	if c.Kind == DirectMessage || c.ID == "" {
		return nil
	}
	rows := []ui.Node{}
	separator := ", "
	if chatEmojiLocale(m.Locale) == "ar" {
		separator = "، "
	}
	for _, label := range m.GateLabels[c.ID] {
		holders := chatgate014Holders(m, label)
		if label.Name == "" || len(holders) == 0 {
			continue
		}
		key := "gate-label-" + label.Name
		more := len(holders) > chatgate014HoldersShown
		open := h.local.detailGroups[key]
		shown := holders
		if more && !open {
			shown = holders[:chatgate014HoldersShown]
		}
		row := []ui.Node{
			html.Span(html.Props{Class: "details-label-name", Dir: "auto", Text: label.Name}),
			html.Span(html.Props{Class: "details-label-people", Dir: "auto", Text: strings.Join(shown, separator)}),
		}
		if more {
			text := laneText(m, chatux030Copy, chatux030KeyShowFewer)
			if !open {
				text = laneTextf(m, chatux030Copy, chatux030KeyShowAll, map[string]string{"n": strings.TrimSpace(m.n(len(holders)))})
			}
			row = append(row, html.Button(html.Props{Class: "button secondary small details-label-more", Type: "button", Text: text,
				Data: map[string]string{"action": "details-group", "id": key}, Aria: map[string]string{"expanded": boolString(open)}}))
		}
		rows = append(rows, html.Li(html.Props{Class: "details-label-row", Data: map[string]string{"gate-label": label.Name}}, row...))
	}
	if len(rows) == 0 {
		return nil
	}
	heading := laneText(m, chatgate014Copy, chatgate014KeyHeading)
	return html.Section(html.Props{ID: "chat-details-labels", Class: "details-section details-labels", Aria: map[string]string{"label": heading}},
		html.H3(html.Props{Text: heading}),
		html.P(html.Props{Class: "details-labels-note", Dir: "auto", Text: laneText(m, chatgate014Copy, chatgate014KeyNote)}),
		html.Ul(html.Props{Class: "details-label-list"}, rows...))
}

// ChatgateLabelStyles draws the Labels section: one row per label, its name above
// the people who hold it, in the quiet type of the panel's other lists.
const ChatgateLabelStyles = `.details-labels-note{margin:4px 0 8px;font-size:.75rem;line-height:1.4;color:var(--muted);overflow-wrap:anywhere}` +
	`.details-label-list{list-style:none;margin:0;padding:0;display:grid;gap:10px}` +
	`.details-label-row{display:flex;flex-wrap:wrap;align-items:baseline;column-gap:8px;row-gap:2px;min-inline-size:0}` +
	`.details-label-name{font-size:.875rem;font-weight:600;overflow-wrap:anywhere}` +
	`.details-label-people{flex:1 1 100%;font-size:.8125rem;color:var(--muted);overflow-wrap:anywhere}` +
	`.details-label-more{flex:none;block-size:28px;min-block-size:28px;padding-inline:12px;font-size:.75rem}`
