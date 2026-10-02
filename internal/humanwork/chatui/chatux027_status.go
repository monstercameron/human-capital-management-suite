package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// chatux027StatusActions is the form's button row: Confirm, waiting until a
// status is chosen, and, inside the panel's row, Cancel.
func chatux027StatusActions(m Model, props ChannelStatusPanelProps, choice string) ui.Node {
	confirm := html.Button(html.Props{Class: "button small", Type: "submit", Disabled: choice == "", Text: chatstateText(m, "confirm")})
	if props.Section == nil {
		return confirm
	}
	return chatux027Actions(confirm, chatux027CancelButton(m, chatux027ManageScope(m), "status"))
}

// chatux027StatusSection is the Status row of Manage channel: the current
// state at the right, and inside, the form that changes it and whatever the
// last attempt left to say. Failures and the "changed" line are inside the row
// they belong to, in full; a row with a failure opens first.
func chatux027StatusSection(m Model, v ChannelStatusView, props ChannelStatusPanelProps, form ui.Node, retry ui.Handler) ui.Node {
	var body []ui.Node
	if form != nil {
		body = append(body, form)
	}
	failed := false
	if v.Loading {
		body = append(body, html.P(html.Props{Class: "manage-sec-help", Role: "status", Text: chatstateText(m, "loading")}))
	}
	if v.Unavailable {
		failed = true
		body = append(body, chatux027Error(m, chatstateText(m, "error"), retry, ""))
	}
	if v.Error != "" {
		failed = true
		key := v.Error
		switch key {
		case "revision_conflict", "channel_held", "permission_denied", "last_reopener":
		default:
			key = "error"
		}
		body = append(body, chatux027Error(m, chatstateText(m, key), retry, ""))
	}
	if v.Updated {
		body = append(body, html.P(html.Props{Class: "manage-sec-help", Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}},
			ui.Text(chatstateText(m, "updated")+": "+chatstateText(m, chatstateKey(v.Status.Status)))))
	}
	content := sectionContent{Label: chatstateText(m, "title"), Value: ChannelStatusBadge(m, v), Failed: failed}
	if len(body) > 0 {
		content.Body = html.Div(html.Props{Class: "chatstate-section manage-status-block", Dir: agentReplyDirection(m.Locale)}, body...)
	}
	return props.Section(content)
}
