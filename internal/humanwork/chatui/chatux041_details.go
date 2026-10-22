package chatui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-041: the pieces of Conversation details that round three added or
// rewrote: the role-label count, the joining row's state, the agent facts, and
// the confirmation a destructive status needs.

// chatux041RolesValue is the value of the Role labels row: how many labels are
// set among the people who are in the channel, not how many people there are.
func chatux041RolesValue(m Model) string {
	set := 0
	for _, member := range m.ChannelTeam.Members {
		if strings.TrimSpace(member.RoleLabel) == "" {
			continue
		}
		for _, current := range m.Members {
			if current.HomeTenantID == member.HomeTenantID && current.ID == member.SubjectID {
				set++
				break
			}
		}
	}
	if set == 0 {
		return laneText(m, chatux041Copy, chatux041KeyRolesNone)
	}
	return laneTextf(m, chatux041Copy, chatux041KeyRolesSet, map[string]string{"n": strings.TrimSpace(m.n(set))})
}

// chatux041GateState says where the joining gate stands, in words. The page
// knows a gate only through Model.GateJoins: a nil map means the list was not
// read and nothing is said, a channel missing from a read list has no gate.
func chatux041GateState(m Model, c Conversation) string {
	if m.GateJoins == nil {
		return ""
	}
	gate, gated := chatgateJoin(m, c)
	if !gated {
		if c.Kind == PrivateChannel {
			return laneText(m, chatux041Copy, chatux041KeyGateInvite)
		}
		return laneText(m, chatux041Copy, chatux041KeyGateAnyone)
	}
	count := laneText(m, chatux041Copy, chatux041KeyGateOne)
	if gate.Questions != 1 {
		count = laneTextf(m, chatux041Copy, chatux041KeyGateMany, map[string]string{"n": strings.TrimSpace(m.n(gate.Questions))})
	}
	locale := chatgateLocale(m.Locale)
	switch gate.Mode {
	case "automatic":
		return count + " · " + GateText(locale, "automatic")
	case "rule":
		return count + " · " + GateText(locale, "rule")
	}
	return count + " · " + GateText(locale, "review")
}

// chatux041Fact is one labelled fact of an agent: the label in the one label
// style, then its value.
func chatux041Fact(label string, value ui.Node) []ui.Node {
	return []ui.Node{html.Tag("dt", html.Props{Class: "agent-fact-label", Text: label}), html.Tag("dd", html.Props{Class: "agent-fact-value"}, value)}
}

// chatux041FactList is a list of plain sentences as the value of a fact.
func chatux041FactList(lines []string) ui.Node {
	items := make([]ui.Node, 0, len(lines))
	for _, line := range lines {
		items = append(items, html.Li(html.Props{Dir: "auto", Text: line}))
	}
	return html.Ul(html.Props{}, items...)
}

// chatux041StatusAck is the confirmation a destructive status change asks for:
// a box that must be ticked before Confirm is live. Archived makes the channel
// read-only and takes it off the channel list, so it asks; Locked has its own end
// time and Announcements only hides nothing.
func chatux041StatusAck(m Model, props ChannelStatusPanelProps, choice string) (node ui.Node, needed, given bool) {
	if choice != "ARCHIVED" {
		return nil, false, false
	}
	key := "ack:status:" + strconv.FormatUint(uint64(props.View.Status.Revision), 10) + ":" + choice
	given = props.Dirty[key]
	node = html.Label(html.Props{Class: "chatstate-ack"},
		html.Input(html.Props{ID: "chatstate-ack", Type: "checkbox", Checked: given, OnInput: props.Input,
			Data: map[string]string{"action": chatux041InputAction, "id": key}}),
		html.Span(html.Props{Text: laneText(m, chatux041Copy, chatux041KeyAck)}))
	return node, true, given
}
