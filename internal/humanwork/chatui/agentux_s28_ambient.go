package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AmbientGrant is one agent installed in the open conversation that can be
// given "Reads every message here", with the state the server reports: whether
// the conversation's administrator turned it on, and whether it is paused for a
// spent daily limit (AGENTUX-066).
type AmbientGrant struct {
	Agent   string
	Enabled bool
	Paused  bool
}

// AmbientState is everything Chat knows from the ambient agents' read of the
// open conversation that the reads line and opt-out do not carry: the
// installed agents and whether this viewer may switch them, and the offers
// (to-do and reminder cards) the server filtered for this viewer
// (AGENTUX-067, -068). Failed keeps the last good Cards and offers a retry.
type AmbientState struct {
	Grants  []AmbientGrant
	Manages bool
	Cards   []AgentUXAmbientCard
	Failed  bool
}

const (
	keyAmbientGrantHeading = "agentux066.grant_heading"
	keyAmbientGrantSwitch  = "agentux066.grant_switch"
	keyAmbientGrantHelp    = "agentux066.grant_help"
)

var agentux066GrantCopy = map[string]map[string]string{
	"en-US": {
		keyAmbientGrantHeading: "Reads every message here", keyAmbientGrantSwitch: "Reads every message here",
		keyAmbientGrantHelp: "Everyone in this conversation can see which agents read messages. Each person can turn it off for their own messages.",
	},
	"de-DE": {
		keyAmbientGrantHeading: "Liest jede Nachricht hier", keyAmbientGrantSwitch: "Liest jede Nachricht hier",
		keyAmbientGrantHelp: "Alle in dieser Unterhaltung sehen, welche Agenten Nachrichten lesen. Jede Person kann es für die eigenen Nachrichten ausschalten.",
	},
	"ar": {
		keyAmbientGrantHeading: "يقرأ كل رسالة هنا", keyAmbientGrantSwitch: "يقرأ كل رسالة هنا",
		keyAmbientGrantHelp: "يرى الجميع في هذه المحادثة أي الوكلاء يقرؤون الرسائل. يمكن لكل شخص إيقاف ذلك لرسائله.",
	},
}

// ambientGrantSection is the conversation administrator's switch for each
// installed agent that can read every message. Members and people in a direct
// conversation between two people get nothing here: the line that tells them
// who reads is ambientReadsSection. The click is handled by the page's ambient
// client from the data-ambient-* attributes, so the control works the same
// whichever surface renders it, and the server refuses anyone but a manager.
func ambientGrantSection(m Model) ui.Node {
	if !m.Ambient.Manages || len(m.Ambient.Grants) == 0 {
		return nil
	}
	// The server refuses any direct conversation, with an agent as well as with
	// a person, so the switch is never drawn there.
	if m.selected().Kind == DirectMessage {
		return nil
	}
	rows := make([]ui.Node, 0, len(m.Ambient.Grants))
	for _, grant := range m.Ambient.Grants {
		if grant.Agent == "" {
			continue
		}
		next := "true"
		if grant.Enabled {
			next = "false"
		}
		name := ambientAgentName(m, grant.Agent)
		rows = append(rows, html.WithKey(html.Li(html.Props{Class: "ambient-grant-row"},
			html.Span(html.Props{Class: "ambient-grant-name", Dir: "auto", Text: name}),
			html.Button(html.Props{Class: "button secondary small ambient-grant-switch", Type: "button", Role: "switch",
				Aria: map[string]string{"checked": boolString(grant.Enabled), "label": name + ": " + laneText(m, agentux066GrantCopy, keyAmbientGrantSwitch)},
				Raw:  map[string]any{"data-ambient-action": "GRANT", "data-ambient-agent": grant.Agent, "data-ambient-enabled": next, "data-ambient-conversation": m.SelectedID},
				Text: laneText(m, agentux066GrantCopy, keyAmbientGrantSwitch)})), "ambient-grant:"+grant.Agent))
	}
	if len(rows) == 0 {
		return nil
	}
	return html.Div(html.Props{Class: "ambient-grants", Aria: map[string]string{"label": laneText(m, agentux066GrantCopy, keyAmbientGrantHeading)}},
		html.Ul(html.Props{Class: "member-list ambient-grant-list"}, rows...),
		html.P(html.Props{Class: "ambient-reads-help muted", Dir: "auto", Text: laneText(m, agentux066GrantCopy, keyAmbientGrantHelp)}))
}

// ambientCardsForPost are the offers made for one message, shown under it. The
// server already filtered them for this viewer; RenderAgentUXAmbientCard
// checks a private card against the viewer again.
func ambientCardsForPost(m Model, post string) []ui.Node {
	var nodes []ui.Node
	for _, card := range m.Ambient.Cards {
		if card.Source != post || card.Conversation != "" && card.Conversation != m.SelectedID {
			continue
		}
		if node := RenderAgentUXAmbientCard(m, card); node != nil {
			nodes = append(nodes, html.WithKey(node, "ambient:"+card.ID))
		}
	}
	return nodes
}

// ambientCardsUnplaced are the offers whose message is not on the loaded page
// of the timeline, and the retry line when the last read failed. They close the
// timeline so an offer is never lost because its message is far up.
func ambientCardsUnplaced(m Model, loaded []Message) []ui.Node {
	have := make(map[string]bool, len(loaded))
	for _, msg := range loaded {
		have[msg.ID] = true
	}
	var nodes []ui.Node
	for _, card := range m.Ambient.Cards {
		if have[card.Source] || card.Conversation != "" && card.Conversation != m.SelectedID {
			continue
		}
		if node := RenderAgentUXAmbientCard(m, card); node != nil {
			nodes = append(nodes, html.WithKey(node, "ambient:"+card.ID))
		}
	}
	if m.Ambient.Failed {
		nodes = append(nodes, html.WithKey(RenderAgentUXAmbientList(m, nil, "error"), "ambient:error"))
	}
	return nodes
}
