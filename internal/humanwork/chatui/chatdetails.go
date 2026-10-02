package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// chatdetailsText is the reviewed copy for the small pieces of Conversation
// details that the shared catalog does not carry. A catalog that answers with
// a placeholder for a missing key never reaches the page.
func chatdetailsText(m Model, key string) string {
	copy := map[string][3]string{
		"integrations_copy": {"Copy sample request for apps", "Beispielanfrage für Apps kopieren", "نسخ مثال طلب للتطبيقات"},
	}
	values, ok := copy[key]
	if !ok {
		return ""
	}
	index := 0
	if strings.HasPrefix(m.Locale, "de") {
		index = 1
	}
	if strings.HasPrefix(m.Locale, "ar") {
		index = 2
	}
	return chatbug039Text(key, values[index], values[0])
}

// countedLabel is a heading with its count, "Members · 18". A count that is
// zero or that the number formatter could not write leaves the label alone, so
// no label ever ends in a separator.
func countedLabel(m Model, label string, count int) string {
	if count <= 0 {
		return label
	}
	written := strings.TrimSpace(m.n(count))
	if written == "" {
		return label
	}
	return label + " · " + written
}

// personaMemberRow is an agent at the end of the member list. It carries the
// agent's own stored icon and the Agent badge, like the Agents here section,
// and opens to the agent's profile.
func personaMemberRow(m Model, persona ResolvedPersonaMention) ui.Node {
	return personaMemberRowWith(m, persona)
}

// personaMemberRowWith is personaMemberRow with more nodes after the profile
// disclosure in the same list item: the agent's description and its Ask button.
func personaMemberRowWith(m Model, persona ResolvedPersonaMention, after ...ui.Node) ui.Node {
	name := persona.Reference.Display
	ids := []string{persona.Reference.ID}
	if persona.Actor != nil {
		ids = append(ids, persona.Actor.AgentID)
	}
	label := chatPolishDisclosureLabel(html.Props{Class: "persona-member-label"},
		agentDMAvatar(name, "avatar small agent-dm-avatar", agentIconFor(m, ids, name, persona.Icon)),
		html.Strong(html.Props{Class: "persona-member-name", Dir: "auto", Text: name}),
		AgentBadgeLabel(m.Locale))
	return html.Li(html.Props{Class: "member-row persona-member-row"}, append([]ui.Node{chatPolishDisclosure(html.Props{}, label, personaProfileCard(m.Locale, persona))}, after...)...)
}

// notificationsControl is the per-conversation notification choice in the
// panel's own disclosure style: a row naming the current choice that opens to
// the three choices. Each choice is the delegated action the rail menu uses.
func notificationsControl(m Model, conversationID string, mode NotificationMode) ui.Node {
	choices := []struct {
		action string
		mode   NotificationMode
		key    string
	}{{"rail-notify-all", NotifyAll, KeyNotifyAll}, {"rail-notify-mentions", NotifyMention, KeyNotifyMentions}, {"rail-notify-mute", NotifyMute, KeyNotifyMute}}
	current := m.t(KeyNotifyAll)
	buttons := make([]ui.Node, 0, len(choices))
	for _, choice := range choices {
		if choice.mode == mode {
			current = m.t(choice.key)
		}
		buttons = append(buttons, html.Button(html.Props{Class: "button secondary small details-notify-option", Type: "button", Disabled: m.Callbacks.SetConversationNotification == nil,
			Data: map[string]string{"action": choice.action, "id": conversationID}, Aria: map[string]string{"pressed": boolString(choice.mode == mode)}, Text: m.t(choice.key)}))
	}
	return html.Section(html.Props{Class: "details-section details-notify"},
		chatPolishDisclosure(html.Props{Class: "channel-widget-edit"},
			chatPolishDisclosureLabel(html.Props{Class: "channel-widget-summary"},
				html.Span(html.Props{Class: "channel-widget-summary-label", Text: chatux005Text(m, "notifications")}),
				html.Span(html.Props{Class: "channel-widget-summary-value", Text: current})),
			html.Div(html.Props{Class: "details-notify-options", Role: "group", Aria: map[string]string{"label": chatux005Text(m, "notifications")}}, buttons...)))
}

// chatstateHistory lists who changed the channel's status, when, why and until
// when, one row for each fact that is known. A channel whose status never
// changed has no history, so it prints nothing: the badge above it already
// says the current state.
func chatstateHistory(m Model, v ChannelStatusView) ui.Node {
	var rows []ui.Node
	row := func(label, value string, auto bool) {
		props := html.Props{Text: value}
		if auto {
			props.Dir = "auto"
		}
		rows = append(rows, html.Tag("dt", html.Props{Text: label}), html.Tag("dd", props))
	}
	if v.Status.ChangedBy != "" {
		if actor := chatstateActorName(m, v); actor != chatstateText(m, "none") {
			row(chatstateText(m, "by"), actor, true)
		}
	}
	if v.Status.ChangedAt != nil {
		row(chatstateText(m, "when"), v.Status.ChangedAt.Format("2006-01-02 15:04 MST"), false)
	}
	if reason := strings.TrimSpace(v.Status.Reason); reason != "" {
		row(chatstateText(m, "why"), reason, true)
	}
	if v.Status.Until != nil {
		row(chatstateText(m, "ends"), v.Status.Until.Format("2006-01-02 15:04 MST"), false)
	}
	if len(rows) == 0 {
		return nil
	}
	return html.Tag("dl", html.Props{Class: "chatstate-history"}, rows...)
}
