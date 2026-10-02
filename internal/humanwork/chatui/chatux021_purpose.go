package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// purposeFieldID is the purpose box. The submit handler reads it by id.
const purposeFieldID = "channel-team-purpose"

// chatux021Purpose is the About section's purpose line, edited in place
// (CHATUX-021): the row shows the purpose, pressing it turns the row into a
// box, Enter saves, Escape or Cancel puts the row back, and a save that went
// through says "Saved" for two seconds. Before, the row and the box both read
// "Purpose", the form had Save and no Cancel, and it stayed open afterwards
// with no sign that anything had happened.
func chatux021Purpose(m Model, h handlers, parts *channelWidgetParts) ui.Node {
	if parts == nil {
		return nil
	}
	label := m.t(KeyTeamPurpose)
	disabled := parts.Disabled || m.Callbacks.SetChannelTeamPurpose == nil
	purpose := strings.TrimSpace(m.ChannelTeam.Purpose)
	if m.ChannelWidgetsPending && h.local.purposeDraft != "" {
		// The save is in flight: show what was typed, not the old purpose.
		purpose = h.local.purposeDraft
	}
	if h.local.purposeEditing && !disabled {
		// The same shape as every form in the panel (CHATUX-027): the label over the
		// field, the hint under it, then the button row with the main button first.
		return html.Form(html.Props{Class: "details-purpose-form manage-sec-form", OnSubmit: h.teamPurposeSubmit},
			html.Div(html.Props{},
				html.Label(html.Props{Class: "details-purpose-label", For: purposeFieldID, Text: label}),
				html.Input(html.Props{ID: purposeFieldID, Class: "chat-input", Type: "text", MaxLength: 500, AutoFocus: true, AutoComplete: "off", OnKeyDown: h.purposeKey,
					Data: map[string]string{"chat-value": h.local.purposeDraft}, Aria: map[string]string{"describedby": "details-purpose-hint"}})),
			html.P(html.Props{ID: "details-purpose-hint", Class: "details-purpose-hint manage-sec-hint", Text: laneText(m, chatux021Copy, keyChatux021PurposeHint)}),
			html.Div(html.Props{Class: "details-purpose-actions manage-sec-actions"},
				html.Button(html.Props{Class: "button small", Type: "submit", Text: m.t(KeyWidgetSave)}),
				html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": "purpose-cancel"}, Text: m.t(KeyCancel)})))
	}
	value := purpose
	valueClass := "details-purpose-value"
	if value == "" {
		value, valueClass = laneText(m, chatux021Copy, keyChatux021PurposeAdd), valueClass+" muted"
	}
	row := html.Button(html.Props{Class: "details-purpose-row", Type: "button", Disabled: disabled,
		Data: map[string]string{"action": "purpose-edit"}, Aria: map[string]string{"label": laneText(m, chatux021Copy, keyChatux021PurposeEdit) + ": " + value}},
		html.Span(html.Props{Class: "details-purpose-label", Text: label}),
		html.Span(html.Props{Class: valueClass, Dir: "auto", Text: value}),
		html.Span(html.Props{Class: "details-purpose-cue", Aria: map[string]string{"hidden": "true"}}, icon("edit")))
	children := []ui.Node{row}
	if m.PurposeSaved && !m.ChannelWidgetsPending {
		children = append(children, html.P(html.Props{Class: "details-purpose-saved", Role: "status", Aria: map[string]string{"live": "polite"}}, icon("check"), ui.Text(laneText(m, chatux021Copy, keyChatux021Saved))))
	}
	return html.Div(html.Props{Class: "details-purpose"}, children...)
}

// chatux021Click handles the details panel's own buttons: opening the purpose
// box, putting it away, and the person pane's Back. It reports whether the
// click was one of them.
func chatux021Click(e ui.MouseEvent, m Model, local localStore) bool {
	action, _, _ := eventAction(e)
	switch action {
	case "purpose-edit":
		current := strings.TrimSpace(m.ChannelTeam.Purpose)
		local.update(func(u *localUI) { u.purposeEditing, u.purposeDraft = true, current })
		focusField(purposeFieldID)
		return true
	case "purpose-cancel":
		local.update(func(u *localUI) { u.purposeEditing = false })
		return true
	}
	return false
}

// chatux021PurposeKey is the purpose box's key handler: Escape puts the row
// back without saving. Enter is the form's own submit.
func chatux021PurposeKey(e ui.KeyboardEvent, local localStore) {
	if e.GetKey() != "Escape" {
		return
	}
	e.PreventDefault()
	e.StopPropagation()
	local.update(func(u *localUI) { u.purposeEditing = false })
}

// chatux021PurposeSubmitted closes the box once a save has been sent and keeps
// what was typed to show while it goes through.
func chatux021PurposeSubmitted(local localStore, text string) {
	local.update(func(u *localUI) { u.purposeEditing, u.purposeDraft = false, text })
}

// personPaneHeading is the head of the person pane: a Back arrow to the
// conversation details it replaced (when they were open), the title, and Close.
// Before, one X did the first job and was labelled as the second.
func personPaneHeading(m Model) ui.Node {
	children := []ui.Node{}
	if m.ShowDetails {
		children = append(children, actionButton("icon-button", "person-back", "", laneText(m, chatux021Copy, keyChatux021PersonBack), m.Callbacks.ClosePerson == nil, icon("arrow-left")))
	}
	children = append(children,
		html.H2(html.Props{Class: "person-pane-heading", Raw: map[string]any{"tabindex": "-1"}, Text: m.t(KeyPersonDetails)}),
		actionButton("icon-button chat-panel-close", "close-person", "", m.t(KeyClosePerson), m.Callbacks.ClosePerson == nil, icon("close")))
	return html.Div(html.Props{Class: "side-heading chat-panel-head person-pane-head"}, children...)
}
