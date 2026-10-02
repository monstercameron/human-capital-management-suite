package chatui

import (
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// chatux027ManageScope is the group of the rows under Manage channel for the
// open conversation: another conversation starts with every row closed.
func chatux027ManageScope(m Model) string { return chatux027ManageGroup + "|" + m.SelectedID }

// chatux027PersonalScope is the group of the rows about the reader.
func chatux027PersonalScope(m Model) string { return chatux027PersonalGroup + "|" + m.SelectedID }

// chatux027Rows drops the rows that are not there.
func chatux027Rows(rows ...ui.Node) []ui.Node {
	kept := make([]ui.Node, 0, len(rows))
	for _, row := range rows {
		if row != nil {
			kept = append(kept, row)
		}
	}
	return kept
}

// chatux027Status is the Status row: the channel status component draws its
// own form and notes and hands them to the panel as one row.
func chatux027Status(m Model, h handlers) ui.Node {
	view, ok := m.ChannelStatuses[m.SelectedID]
	if !ok {
		return nil
	}
	return ChannelStatusPanel(ChannelStatusPanelProps{Model: m, View: view, Change: m.ChangeChannelStatus, Retry: m.RetryChannelStatus, Now: time.Now(),
		Section: chatux027Wrap(m, h, chatux027ManageScope(m), "status", "manage-status")})
}

// chatux027Roles is the Role labels row: the number of labels at the right, and
// inside, the sentence that says what a label is and the list.
func chatux027Roles(m Model, h handlers, parts *channelWidgetParts) ui.Node {
	if parts == nil {
		return nil
	}
	scope := chatux027ManageScope(m)
	var value ui.Node
	if parts.RoleCount > 0 {
		value = ui.Text(m.n(parts.RoleCount))
	}
	open := chatux027IsOpen(h.local, scope, "roles", false)
	return chatux027Section(m, sectionSpec{ID: "roles", Group: scope, Class: "manage-wrapped", Label: m.t(KeyTeamRoles), Value: value, Open: open,
		Body: chatux027RolesBody(m, h, parts)})
}

// chatux027Project is the Project and milestones row: the project's title (or
// the number of milestones) at the right, and inside, the editor, the list and
// the form that adds one.
func chatux027Project(m Model, h handlers, parts *channelWidgetParts) ui.Node {
	if parts == nil {
		return nil
	}
	scope := chatux027ManageScope(m)
	var value ui.Node
	if title := m.ChannelProject.Title; title != "" {
		value = ui.Text(title)
	} else if parts.MilestoneCount > 0 {
		value = ui.Text(m.n(parts.MilestoneCount))
	}
	open := chatux027IsOpen(h.local, scope, "project", false)
	return chatux027Section(m, sectionSpec{ID: "project", Group: scope, Label: chatux005Text(m, "project"), Value: value, Open: open,
		Body: chatux027ProjectBody(m, h, parts)})
}

// chatux027Filters is the two filter rows: the channel's own and, for a
// workspace administrator, the workspace's. A row's settings are built only
// while it is open, because both draw the same field ids.
func chatux027Filters(m Model, h handlers, c Conversation) []ui.Node {
	if m.ChatFeatures != nil && !m.ChatFeatures.Filters {
		return nil
	}
	if !canAdministerConversation(m, c) {
		return nil
	}
	scope := chatux027ManageScope(m)
	var rows []ui.Node
	if m.FilterSettings != nil {
		open := chatux027IsOpen(h.local, scope, "filters", m.ShowFilterSettings)
		var body ui.Node
		if open {
			body = html.Div(html.Props{Dir: direction(m.Locale)}, m.FilterSettings())
		} else {
			body = html.Div(html.Props{})
		}
		rows = append(rows, chatux027Section(m, sectionSpec{ID: "filters", Group: scope, Class: "chatfilter-entry", Label: chatfilterText(m, "manage"), Open: open, Body: body}))
	}
	if m.IsTenantAdmin && m.WorkspaceFilterSettings != nil {
		open := chatux027IsOpen(h.local, scope, "workspace-filters", false)
		var body ui.Node
		if open {
			body = html.Div(html.Props{Dir: direction(m.Locale)}, m.WorkspaceFilterSettings())
		} else {
			body = html.Div(html.Props{})
		}
		rows = append(rows, chatux027Section(m, sectionSpec{ID: "workspace-filters", Group: scope, Class: "chatfilter-entry", Label: modadminText(m, "ws_entry"), Open: open, Body: body}))
	}
	if m.FilterSettings == nil && !(m.IsTenantAdmin && m.WorkspaceFilterSettings != nil) {
		// Nothing to open: the line stands alone, with no control to press.
		rows = append(rows, chatux027Section(m, sectionSpec{ID: "filters", Group: scope, Class: "chatfilter-entry", Label: chatfilterText(m, "title")}))
	}
	return rows
}

// chatux027Translation is the Language row. The translation component loads
// the setting and hands its content to the panel, so a load or save that fails
// is said inside this row.
func chatux027Translation(m Model, h handlers, c Conversation) ui.Node {
	if m.ChatFeatures == nil || !m.ChatFeatures.Translation || !canAdministerConversation(m, c) {
		return nil
	}
	wrap := chatux027Wrap(m, h, chatux027ManageScope(m), "language", "chatlangadmin-entry")
	return html.WithKey(ui.CreateElement(translationAdminPanel, translationAdminProps{Locale: m.Locale, Conversation: c.ID, Mode: translationModeChannel, Text: m.Text, Section: wrap}), "chatlangadmin-"+c.ID)
}

// chatux027Integrations is the Integrations row: what an app may do, and under
// "For developers" the button that copies a sample request.
func chatux027Integrations(m Model, h handlers, c Conversation) ui.Node {
	if !canAdministerConversation(m, c) {
		return nil
	}
	scope := chatux027ManageScope(m)
	open := chatux027IsOpen(h.local, scope, "integrations", false)
	// The sample request is a developer action: a text button under its own
	// "For developers" row, so the loudest control of the panel is not one most
	// managers never need (CHATUX-030).
	devScope := scope + "|integrations"
	devOpen := chatux027IsOpen(h.local, devScope, "integrations-dev", false)
	copyButton := html.Button(html.Props{Class: "button secondary small", Type: "button", Disabled: m.Callbacks.CopyConversationAPICurl == nil,
		Data: map[string]string{"action": "copy-conversation-api-curl", "id": c.ID}}, icon("copy"), html.Span(html.Props{Text: chatdetailsText(m, "integrations_copy")}))
	developers := chatux027Section(m, sectionSpec{ID: "integrations-dev", Group: devScope, Class: "integrations-developers", Label: laneText(m, chatux030Copy, chatux030KeyDevelopers), Open: devOpen,
		Body: chatux027Actions(copyButton)})
	return chatux027Section(m, sectionSpec{ID: "integrations", Group: scope, Class: "integrations-section", Label: m.t(KeyIntegrationsTitle), Open: open,
		Body: html.Div(html.Props{}, chatux027Help(m.t(KeyIntegrationsHint)), developers)})
}

// chatux027Notifications is the Notifications row of the reader: the choice in
// force at the right, the three choices inside.
func chatux027Notifications(m Model, h handlers, conversationID string, mode NotificationMode) ui.Node {
	current, options := notificationChoices(m, conversationID, mode, false)
	scope := chatux027PersonalScope(m)
	open := chatux027IsOpen(h.local, scope, "notify", false)
	return html.Section(html.Props{Class: "details-section details-notify"},
		chatux027Section(m, sectionSpec{ID: "notify", Group: scope, Label: chatux005Text(m, "notifications"), Value: ui.Text(current), Open: open, Body: options}))
}

// chatux027Reading is the reading-language row of the reader, drawn by the
// rendering component through the panel's row.
func chatux027Reading(m Model, h handlers) ui.Node {
	if m.SelectedID == "" || (m.ChatFeatures != nil && !m.ChatFeatures.Renderings) {
		return nil
	}
	wrap := chatux027Wrap(m, h, chatux027PersonalScope(m), "reading", "")
	return html.WithKey(ui.CreateElement(chatbug058Panel, chatbug058Props{Locale: m.Locale, Conversation: m.SelectedID, Section: wrap}), "chatbug058-"+m.SelectedID)
}
