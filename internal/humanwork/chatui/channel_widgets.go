package chatui

import (
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func widgetMemberName(m Model, home, subject string) string {
	for _, member := range m.Members {
		if member.HomeTenantID == home && member.ID == subject && member.Name != "" && member.Name != subject {
			return member.Name
		}
	}
	return m.t(KeyTodoMemberFallback)
}

func widgetStatus(m Model, status string) string {
	switch status {
	case "IN_PROGRESS":
		return m.t(KeyInProgress)
	case "BLOCKED":
		return m.t(KeyBlocked)
	case "DONE":
		return m.t(KeyDone)
	}
	return m.t(KeyPlanned)
}

func widgetChannelRole(m Model, role string) string {
	if role == "MEMBERSHIP_ROLE_MANAGER" {
		return m.t(KeyChannelManager)
	}
	return m.t(KeyChannelMember)
}

func widgetStatusOptions(m Model, selected string) []ui.Node {
	values := []string{"PLANNED", "IN_PROGRESS", "BLOCKED", "DONE"}
	out := make([]ui.Node, 0, len(values))
	for _, value := range values {
		out = append(out, html.Option(html.Props{Value: value, Selected: value == selected, Text: widgetStatus(m, value)}))
	}
	return out
}

func widgetOwnerOptions(m Model, selectedHome, selectedSubject string) []ui.Node {
	out := []ui.Node{html.Option(html.Props{Raw: map[string]any{"value": ""}, Selected: selectedSubject == "", Text: "—"})}
	for _, member := range m.Members {
		if member.ID == "" {
			continue
		}
		out = append(out, html.Option(html.Props{Value: widgetOwnerToken(member.HomeTenantID, member.ID), Selected: member.ID == selectedSubject && member.HomeTenantID == selectedHome, Text: widgetMemberName(m, member.HomeTenantID, member.ID)}))
	}
	return out
}

func widgetOwnerIdentity(m Model, selected string) (string, string) {
	for _, member := range m.Members {
		if widgetOwnerToken(member.HomeTenantID, member.ID) == selected {
			return member.HomeTenantID, member.ID
		}
	}
	return "", ""
}

func widgetOwnerValue(m Model, home, subject string) string {
	if subject == "" {
		return "__none__"
	}
	for _, member := range m.Members {
		if member.HomeTenantID == home && member.ID == subject {
			return widgetOwnerToken(home, subject)
		}
	}
	return "__none__"
}

func widgetOwnerToken(home, subject string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(home)) + "." + base64.RawURLEncoding.EncodeToString([]byte(subject))
}

func widgetError(m Model) ui.Node {
	if m.ChannelWidgetsError == "" {
		return html.Span(html.Props{})
	}
	return html.Div(html.Props{}, html.P(html.Props{Role: "alert", Text: m.t(KeyWidgetError)}), actionButton("button secondary small", "widget-retry", "", m.t(KeyRetry), m.Callbacks.RetryChannelWidgets == nil, ui.Text(m.t(KeyRetry))))
}

func inlineChannelWidgets(m Model) ui.Node {
	if m.ChannelTeam.Revision == 0 && m.ChannelProject.Revision == 0 && !m.ChannelWidgetsLoading && m.ChannelWidgetsError == "" {
		return html.Span(html.Props{})
	}
	cards := []ui.Node{}
	if m.ChannelWidgetsLoading {
		cards = append(cards, html.P(html.Props{Role: "status", Text: m.t(KeyWidgetLoading)}))
	}
	if m.ChannelWidgetsError != "" {
		cards = append(cards, widgetError(m))
	}
	if m.ChannelTeam.Pinned {
		rows := []ui.Node{}
		for _, member := range m.ChannelTeam.Members {
			if widgetOwnerValue(m, member.HomeTenantID, member.SubjectID) != "__none__" {
				label := widgetMemberName(m, member.HomeTenantID, member.SubjectID)
				if member.RoleLabel != "" {
					label += " · " + member.RoleLabel
				}
				rows = append(rows, html.Li(html.Props{Text: label}))
			}
		}
		cards = append(cards, html.Section(html.Props{Class: "channel-widget-inline-card", Aria: map[string]string{"label": m.t(KeyTeamWidget)}}, html.Strong(html.Props{Text: m.t(KeyTeamWidget)}), html.P(html.Props{Text: m.ChannelTeam.Purpose}), html.Ul(html.Props{}, rows...)))
	}
	if m.ChannelProject.Pinned {
		title := strings.TrimSpace(m.ChannelProject.Title)
		if title == "" {
			title = m.t(KeyProjectWidget)
		}
		rows := []ui.Node{}
		for _, milestone := range m.ChannelProject.Milestones {
			label := milestone.Text + " · " + widgetStatus(m, milestone.Status)
			if milestone.OwnerSubjectID != "" && widgetOwnerValue(m, milestone.OwnerHomeTenantID, milestone.OwnerSubjectID) != "__none__" {
				label += " · " + widgetMemberName(m, milestone.OwnerHomeTenantID, milestone.OwnerSubjectID)
			}
			if milestone.DueDate != "" {
				label += " · " + milestone.DueDate
			}
			rows = append(rows, html.Li(html.Props{Text: label}))
		}
		cards = append(cards, html.Section(html.Props{Class: "channel-widget-inline-card", Aria: map[string]string{"label": m.t(KeyProjectWidget)}}, html.Strong(html.Props{Text: title}), html.P(html.Props{Text: m.ChannelProject.Summary}), html.Ul(html.Props{}, rows...)))
	}
	if len(cards) == 0 {
		return html.Span(html.Props{})
	}
	return html.Div(html.Props{Class: "channel-widgets-inline"}, cards...)
}

func channelWidgetSections(m Model, h handlers) ui.Node {
	c := m.selected()
	if c.Kind != PublicChannel && c.Kind != PrivateChannel {
		return html.Span(html.Props{})
	}
	disabled := m.ChannelWidgetsLoading || m.ChannelWidgetsPending || m.ChannelWidgetsError != "" || m.ChannelTeam.Revision == 0 || m.ChannelProject.Revision == 0
	teamRows := []ui.Node{}
	for i, member := range m.ChannelTeam.Members {
		// Never show a name or role label for a member absent from the current
		// authorized roster, even if a stale widget read still contains the ID.
		present := false
		for _, current := range m.Members {
			if current.HomeTenantID == member.HomeTenantID && current.ID == member.SubjectID {
				present = true
				break
			}
		}
		if !present {
			continue
		}
		id := "channel-team-role-" + strconv.Itoa(i)
		teamRows = append(teamRows, html.Li(html.Props{Class: "channel-widget-row"},
			html.Strong(html.Props{Text: widgetMemberName(m, member.HomeTenantID, member.SubjectID)}),
			html.Span(html.Props{Class: "muted", Text: widgetChannelRole(m, member.Role)}),
			html.Label(html.Props{For: id, Text: m.t(KeyTeamRole)}),
			html.Input(html.Props{ID: id, Class: "chat-input", Type: "text", MaxLength: 80, Data: map[string]string{"chat-value": member.RoleLabel}, Disabled: disabled}),
			actionButton("button secondary small", "team-role-save", strconv.Itoa(i), m.t(KeyWidgetSave), disabled || m.Callbacks.SetChannelTeamRoleLabel == nil, ui.Text(m.t(KeyWidgetSave)))))
	}
	milestones := []ui.Node{}
	for i, item := range m.ChannelProject.Milestones {
		prefix := "channel-milestone-"
		suffix := strconv.Itoa(i)
		fields := []ui.Node{
			html.Label(html.Props{For: prefix + "text-" + suffix, Text: m.t(KeyMilestone)}), html.Input(html.Props{ID: prefix + "text-" + suffix, Class: "chat-input", Type: "text", MaxLength: 200, Data: map[string]string{"chat-value": item.Text}, Disabled: disabled}),
			html.Label(html.Props{For: prefix + "status-" + suffix, Text: m.t(KeyMilestoneStatus)}), html.Select(html.Props{ID: prefix + "status-" + suffix, Class: "chat-input", Data: map[string]string{"chat-select-value": item.Status, "chat-select-version": m.SelectedID + ":" + strconv.FormatUint(m.ChannelProject.Revision, 10), "chat-select-editable": "true"}, Disabled: disabled}, widgetStatusOptions(m, item.Status)...),
			html.Label(html.Props{For: prefix + "owner-" + suffix, Text: m.t(KeyMilestoneOwner)}), html.Select(html.Props{ID: prefix + "owner-" + suffix, Class: "chat-input", Data: map[string]string{"chat-select-value": widgetOwnerValue(m, item.OwnerHomeTenantID, item.OwnerSubjectID), "chat-select-version": m.SelectedID + ":" + strconv.FormatUint(m.ChannelProject.Revision, 10), "chat-select-editable": "true"}, Disabled: disabled}, widgetOwnerOptions(m, item.OwnerHomeTenantID, item.OwnerSubjectID)...),
			html.Label(html.Props{For: prefix + "date-" + suffix, Text: m.t(KeyMilestoneDue)}), html.Input(html.Props{ID: prefix + "date-" + suffix, Class: "chat-input", Type: "date", Data: map[string]string{"chat-value": item.DueDate}, Disabled: disabled}),
			actionButton("button secondary small", "milestone-save", item.ID, m.t(KeyWidgetSave), disabled || m.Callbacks.UpdateChannelProjectMilestone == nil, ui.Text(m.t(KeyWidgetSave))),
			actionButton("button secondary small", "milestone-delete", item.ID, m.t(KeyWidgetDelete), disabled || m.Callbacks.DeleteChannelProjectMilestone == nil, ui.Text(m.t(KeyWidgetDelete))),
		}
		summary := widgetStatus(m, item.Status)
		if item.DueDate != "" {
			summary += " · " + item.DueDate
		}
		milestones = append(milestones, html.Li(html.Props{Class: "channel-widget-row"}, widgetEditor(m, item.Text, summary, html.Div(html.Props{Class: "channel-widget-fields"}, fields...))))
	}
	return html.Div(html.Props{Class: "channel-widgets-details"},
		html.Section(html.Props{Class: "details-section channel-widget", Aria: map[string]string{"label": m.t(KeyTeamWidget)}},
			html.Div(html.Props{Class: "details-section-head"}, html.H3(html.Props{Text: m.t(KeyTeamWidget)}), actionButton("icon-button", "team-pin", "", map[bool]string{true: m.t(KeyWidgetUnpin), false: m.t(KeyWidgetPin)}[m.ChannelTeam.Pinned], disabled || !m.ChannelTeam.CanPin || m.Callbacks.SetChannelWidgetPinned == nil, icon(map[bool]string{true: "pin-filled", false: "pin"}[m.ChannelTeam.Pinned]))),
			html.P(html.Props{Class: "muted", Text: m.t(KeyTeamNote)}), widgetError(m),
			widgetEditor(m, m.t(KeyTeamPurpose), m.ChannelTeam.Purpose, html.Form(html.Props{Class: "channel-widget-form", OnSubmit: h.teamPurposeSubmit}, html.Label(html.Props{For: "channel-team-purpose", Text: m.t(KeyTeamPurpose)}), html.Input(html.Props{ID: "channel-team-purpose", Class: "chat-input", Type: "text", MaxLength: 500, Data: map[string]string{"chat-value": m.ChannelTeam.Purpose}, Disabled: disabled}), html.Button(html.Props{Class: "button secondary small", Type: "submit", Disabled: disabled || m.Callbacks.SetChannelTeamPurpose == nil, Text: m.t(KeyWidgetSave)}))),
			// Round 3 C-12: this disclosure edits role labels; titled "Members · 19"
			// it duplicated the Members section below it.
			html.Details(html.Props{Class: "channel-widget-roster"}, html.Summary(html.Props{Text: m.t(KeyTeamRoles) + " · " + m.n(len(teamRows))}), html.Ul(html.Props{Class: "channel-widget-list"}, teamRows...))),
		html.Section(html.Props{Class: "details-section channel-widget", Aria: map[string]string{"label": m.t(KeyProjectWidget)}},
			html.Div(html.Props{Class: "details-section-head"}, html.H3(html.Props{Text: m.t(KeyProjectWidget)}), actionButton("icon-button", "project-pin", "", map[bool]string{true: m.t(KeyWidgetUnpin), false: m.t(KeyWidgetPin)}[m.ChannelProject.Pinned], disabled || !m.ChannelProject.CanPin || m.Callbacks.SetChannelWidgetPinned == nil, icon(map[bool]string{true: "pin-filled", false: "pin"}[m.ChannelProject.Pinned]))),
			html.P(html.Props{Class: "muted", Text: m.t(KeyProjectNote)}),
			widgetEditor(m, m.t(KeyProjectTitle), m.ChannelProject.Title, html.Form(html.Props{Class: "channel-widget-form", OnSubmit: h.projectDetailsSubmit}, html.Label(html.Props{For: "channel-project-title", Text: m.t(KeyProjectTitle)}), html.Input(html.Props{ID: "channel-project-title", Class: "chat-input", Type: "text", MaxLength: 160, Data: map[string]string{"chat-value": m.ChannelProject.Title}, Disabled: disabled}), html.Label(html.Props{For: "channel-project-summary", Text: m.t(KeyProjectSummary)}), html.Input(html.Props{ID: "channel-project-summary", Class: "chat-input", Type: "text", MaxLength: 500, Data: map[string]string{"chat-value": m.ChannelProject.Summary}, Disabled: disabled}), html.Button(html.Props{Class: "button secondary small", Type: "submit", Disabled: disabled || m.Callbacks.SetChannelProjectDetails == nil, Text: m.t(KeyWidgetSave)}))),
			html.Ul(html.Props{Class: "channel-widget-list"}, milestones...),
			widgetAdder(m.t(KeyWidgetAdd), html.Form(html.Props{Class: "channel-widget-form", OnSubmit: h.milestoneSubmit}, html.Label(html.Props{For: "channel-milestone-new", Text: m.t(KeyMilestone)}), html.Input(html.Props{ID: "channel-milestone-new", Class: "chat-input", Type: "text", MaxLength: 200, Disabled: disabled}), html.Label(html.Props{For: "channel-milestone-new-status", Text: m.t(KeyMilestoneStatus)}), html.Select(html.Props{ID: "channel-milestone-new-status", Class: "chat-input", Data: map[string]string{"chat-select-value": "PLANNED", "chat-select-version": m.SelectedID + ":" + strconv.FormatUint(m.ChannelProject.Revision, 10), "chat-select-editable": "true"}, Disabled: disabled}, widgetStatusOptions(m, "PLANNED")...), html.Label(html.Props{For: "channel-milestone-new-owner", Text: m.t(KeyMilestoneOwner)}), html.Select(html.Props{ID: "channel-milestone-new-owner", Class: "chat-input", Data: map[string]string{"chat-select-value": "__none__", "chat-select-version": m.SelectedID + ":" + strconv.FormatUint(m.ChannelProject.Revision, 10), "chat-select-editable": "true"}, Disabled: disabled}, widgetOwnerOptions(m, "", "")...), html.Label(html.Props{For: "channel-milestone-new-date", Text: m.t(KeyMilestoneDue)}), html.Input(html.Props{ID: "channel-milestone-new-date", Class: "chat-input", Type: "date", Disabled: disabled}), html.Button(html.Props{Class: "button secondary small", Type: "submit", Disabled: disabled || m.Callbacks.AddChannelProjectMilestone == nil, Text: m.t(KeyWidgetAdd)})))))
}

// widgetEditor collapses one widget form to a summary row -- the field's
// label, its current value (or "Not set") and an Edit cue -- and shows the
// form only once the row is opened. Round 3 C-9: every form open at once
// made the details pane a wall of empty inputs and full-width Save buttons
// in a panel meant for who is here. The form stays in the DOM while closed,
// so the submit handlers that read fields by id are unchanged.
func widgetEditor(m Model, label, value string, form ui.Node) ui.Node {
	if value == "" {
		value = m.t(KeyWidgetNotSet)
	}
	return html.Details(html.Props{Class: "channel-widget-edit"},
		html.Summary(html.Props{Class: "channel-widget-summary"},
			html.Span(html.Props{Class: "channel-widget-summary-label", Text: label}),
			html.Span(html.Props{Class: "channel-widget-summary-value", Text: value}),
			html.Span(html.Props{Class: "channel-widget-edit-cue", Aria: map[string]string{"hidden": "true"}, Text: m.t(KeyWidgetEdit)})),
		form)
}

// widgetAdder is widgetEditor for a create form: the closed row is the
// action itself ("+ Add milestone").
func widgetAdder(label string, form ui.Node) ui.Node {
	return html.Details(html.Props{Class: "channel-widget-edit adder"},
		html.Summary(html.Props{Class: "channel-widget-summary"}, icon("plus"), html.Span(html.Props{Class: "channel-widget-summary-label", Text: label})),
		form)
}
