package chatui

import (
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// CHATGATE-010: a poll addressed to segments says who it is for, tells a reader
// outside them that they can read it and not vote, and reports results by
// segment only for groups with enough voters.

// chatgatePollAudience is the line naming who the poll is for and, for a reader
// who is not among them, why the choices are off.
func chatgatePollAudience(m Model, view chat.Chatcmd002View) []ui.Node {
	if len(view.Card.Audience) == 0 {
		return nil
	}
	who := strings.Join(view.Card.Audience, ", ")
	nodes := []ui.Node{html.P(html.Props{Class: "chatcmd002-meta chatgate-poll-for", Dir: "auto", Text: fmt.Sprintf(GateText(m.Locale, "pollFor"), who)})}
	if view.NotInAudience {
		nodes = append(nodes, html.P(html.Props{Class: "chatcmd002-note chatgate-poll-outside", Role: "status", Dir: "auto", Text: fmt.Sprintf(GateText(m.Locale, "pollOutside"), who)}))
	}
	return nodes
}

// chatgatePollBreakdown is the result by segment. Groups the service withheld are
// counted, never named: a reader learns how many were left out and why.
func chatgatePollBreakdown(m Model, view chat.Chatcmd002View) []ui.Node {
	b := view.BySegment
	if b == nil || len(view.Card.Audience) == 0 || view.Card.Poll == nil {
		return nil
	}
	text := map[string]string{}
	for _, option := range view.Card.Poll.Options {
		text[option.ID] = option.Text
	}
	rows := []ui.Node{}
	for _, group := range b.Segments {
		parts := []string{}
		for _, option := range view.Card.Poll.Options {
			parts = append(parts, text[option.ID]+": "+chatCount(m.Locale, group.Options[option.ID]))
		}
		rows = append(rows, html.Li(html.Props{Class: "chatgate-poll-group", Data: map[string]string{"gate-poll-group": group.Segment}},
			html.Span(html.Props{Dir: "auto", Text: fmt.Sprintf(GateText(m.Locale, "pollGroup"), group.Segment, group.Voters)}),
			html.Span(html.Props{Class: "chatcmd002-note", Dir: "auto", Text: " " + strings.Join(parts, " · ")})))
	}
	nodes := []ui.Node{html.H4(html.Props{Class: "chatgate-poll-by", Text: GateText(m.Locale, "pollByGroup")})}
	if len(rows) > 0 {
		nodes = append(nodes, html.Ul(html.Props{Class: "chatgate-poll-groups"}, rows...))
	}
	if b.Withheld > 0 {
		nodes = append(nodes, html.P(html.Props{Class: "chatcmd002-note chatgate-poll-withheld", Role: "status", Text: fmt.Sprintf(GateText(m.Locale, "pollWithheld"), b.Withheld, b.K)}))
	} else if len(rows) == 0 {
		nodes = append(nodes, html.P(html.Props{Class: "chatcmd002-note chatgate-poll-withheld", Role: "status", Text: fmt.Sprintf(GateText(m.Locale, "pollNoGroups"), b.K)}))
	}
	return nodes
}

// chatgatePollStyles: the heading and list of groups are small and wrap; the
// lines that say who a poll is for or why it cannot be voted on keep the card's
// own muted text.
const chatgatePollStyles = `.chat-workspace .chatgate-poll-by{margin:8px 0 2px;font-size:.8125rem;font-weight:600}` +
	`.chat-workspace .chatgate-poll-groups{list-style:none;margin:0;padding:0;display:grid;gap:4px}` +
	`.chat-workspace .chatgate-poll-group,.chat-workspace .chatgate-poll-for,.chat-workspace .chatgate-poll-outside,.chat-workspace .chatgate-poll-withheld{overflow-wrap:anywhere}`
