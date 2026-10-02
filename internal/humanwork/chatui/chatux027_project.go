package chatui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-027: the bodies of the Role labels and Project and milestones rows,
// written for the panel's one shape. A role label is one compact line per
// person, saved when the field is committed; the project's fields are in the
// open row and not behind a second "Edit" row; adding a milestone is a text
// button that opens its fields in place.

// chatux027RoleChange saves the role label of the person whose field was just
// committed (Enter, or leaving the field). Nothing is sent for a field the
// person did not change: the browser does not report one.
func chatux027RoleChange(e ui.ChangeEvent, m Model) {
	action, id, _ := eventAction(e)
	if action != "team-role-change" || m.Callbacks.SetChannelTeamRoleLabel == nil || m.ChannelWidgetsPending {
		return
	}
	i, err := strconv.Atoi(id)
	if err != nil || i < 0 || i >= len(m.ChannelTeam.Members) {
		return
	}
	member := m.ChannelTeam.Members[i]
	m.Callbacks.SetChannelTeamRoleLabel(member.HomeTenantID, member.SubjectID, strings.TrimSpace(e.GetValue()))
}

// chatux027RoleRows is the people of the channel with their role label, one
// line each: the person at the start, the label field at the end. A person who
// has left the roster is not shown, even if a stale read still names them.
func chatux027RoleRows(m Model, h handlers, parts *channelWidgetParts) (rows []ui.Node, people int) {
	disabled := parts.Disabled || m.Callbacks.SetChannelTeamRoleLabel == nil
	query := strings.ToLower(strings.TrimSpace(m.MemberQuery))
	for i, member := range m.ChannelTeam.Members {
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
		people++
		name := widgetMemberName(m, member.HomeTenantID, member.SubjectID)
		if query != "" && !strings.Contains(strings.ToLower(name), query) {
			continue
		}
		id := "channel-team-role-" + strconv.Itoa(i)
		field := []ui.Node{html.Input(html.Props{ID: id, Class: "chat-input manage-role-input", Type: "text", MaxLength: 80, Disabled: disabled, Dir: "auto",
			Placeholder: laneText(m, chatux027Copy, chatux027KeyRoleAdd), OnChange: h.roleChange,
			Data: map[string]string{"chat-value": member.RoleLabel, "action": "team-role-change", "id": strconv.Itoa(i)},
			Aria: map[string]string{"label": m.t(KeyTeamRole) + ": " + name}})}
		if member.RoleLabel != "" {
			field = append(field, html.Span(html.Props{Class: "manage-role-saved", Aria: map[string]string{"hidden": "true"}}, icon("check")))
		}
		rows = append(rows, html.Li(html.Props{Class: "manage-role-row", Title: widgetChannelRole(m, member.Role)},
			personAvatar(m, member.SubjectID, name, "avatar small"),
			html.Span(html.Props{Class: "manage-role-name", Dir: "auto", Text: name}),
			html.Span(html.Props{Class: "manage-role-field"}, field...)))
	}
	return rows, people
}

// chatux027RolesBody is the inside of the Role labels row: the sentence that
// says what a label is, a filter once the list is long, and the list.
func chatux027RolesBody(m Model, h handlers, parts *channelWidgetParts) ui.Node {
	rows, people := chatux027RoleRows(m, h, parts)
	children := []ui.Node{chatux027Help(m.t(KeyTeamNote))}
	if people > 8 || m.MemberQuery != "" {
		children = append(children, html.Div(html.Props{Class: "member-filter"},
			html.Label(html.Props{Class: "sr-only", For: "role-filter", Text: m.t(KeyMemberFilter)}),
			icon("search"),
			html.Input(html.Props{ID: "role-filter", Class: "chat-search", Type: "search", Placeholder: m.t(KeyMemberFilter), Data: map[string]string{"chat-value": m.MemberQuery}, AutoComplete: "off", OnInput: h.memberFilter})))
	}
	children = append(children, html.Ul(html.Props{Class: "manage-role-list"}, rows...))
	return html.Div(html.Props{}, children...)
}

// chatux027ProjectBody is the inside of the Project and milestones row: the
// project's own fields with Save, then the milestones, then the text button
// that adds one.
func chatux027ProjectBody(m Model, h handlers, parts *channelWidgetParts) ui.Node {
	disabled := parts.Disabled
	form := html.Form(html.Props{Class: "channel-widget-form manage-sec-form", OnSubmit: h.projectDetailsSubmit},
		html.Div(html.Props{},
			html.Label(html.Props{For: "channel-project-title", Text: m.t(KeyProjectTitle)}),
			html.Input(html.Props{ID: "channel-project-title", Class: "chat-input", Type: "text", MaxLength: 160, Data: map[string]string{"chat-value": m.ChannelProject.Title}, Disabled: disabled})),
		html.Div(html.Props{},
			html.Label(html.Props{For: "channel-project-summary", Text: m.t(KeyProjectSummary)}),
			html.Input(html.Props{ID: "channel-project-summary", Class: "chat-input", Type: "text", MaxLength: 500, Data: map[string]string{"chat-value": m.ChannelProject.Summary}, Disabled: disabled})),
		chatux027Actions(
			html.Button(html.Props{Class: "button small", Type: "submit", Disabled: disabled || m.Callbacks.SetChannelProjectDetails == nil, Text: m.t(KeyWidgetSave)}),
			chatux027CancelButton(m, chatux027ManageScope(m), "project")))
	return html.Div(html.Props{},
		html.Div(html.Props{Class: "manage-sec-toolbar"}, chatux027Help(m.t(KeyProjectNote)), parts.ProjectPin),
		form, parts.MilestoneList, chatux027MilestoneAdder(m, h, parts))
}

// chatux027MilestoneAdder is a text button that opens the fields of a new
// milestone under it, in place: not a row of its own.
func chatux027MilestoneAdder(m Model, h handlers, parts *channelWidgetParts) ui.Node {
	scope := chatux027ManageScope(m) + "/add"
	open := chatux027IsOpen(h.local, scope, "milestone", false)
	label := laneText(m, chatux027Copy, chatux027KeyAddMilestone)
	disabled := parts.Disabled
	toggle := html.Button(html.Props{Class: "button secondary small", Type: "button", Text: label,
		Data: map[string]string{"action": "manage-section", "id": "milestone", "extra": scope + ":" + map[bool]string{true: "1", false: "0"}[open]},
		Aria: map[string]string{"expanded": boolString(open), "controls": "chat-manage-milestone-add"}})
	var fields ui.Node
	if open {
		version := m.SelectedID + ":" + strconv.FormatUint(m.ChannelProject.Revision, 10)
		fields = html.Form(html.Props{Class: "channel-widget-form manage-sec-form", OnSubmit: h.milestoneSubmit},
			html.Div(html.Props{}, html.Label(html.Props{For: "channel-milestone-new", Text: m.t(KeyMilestone)}), html.Input(html.Props{ID: "channel-milestone-new", Class: "chat-input", Type: "text", MaxLength: 200, Disabled: disabled})),
			html.Div(html.Props{}, html.Label(html.Props{For: "channel-milestone-new-status", Text: m.t(KeyMilestoneStatus)}),
				html.Select(html.Props{ID: "channel-milestone-new-status", Class: "chat-input", Data: map[string]string{"chat-select-value": "PLANNED", "chat-select-version": version, "chat-select-editable": "true"}, Disabled: disabled}, widgetStatusOptions(m, "PLANNED")...)),
			html.Div(html.Props{}, html.Label(html.Props{For: "channel-milestone-new-owner", Text: m.t(KeyMilestoneOwner)}),
				html.Select(html.Props{ID: "channel-milestone-new-owner", Class: "chat-input", Data: map[string]string{"chat-select-value": "__none__", "chat-select-version": version, "chat-select-editable": "true"}, Disabled: disabled}, widgetOwnerOptions(m, "", "")...)),
			html.Div(html.Props{}, html.Label(html.Props{For: "channel-milestone-new-date", Text: m.t(KeyMilestoneDue)}), html.Input(html.Props{ID: "channel-milestone-new-date", Class: "chat-input", Type: "date", Disabled: disabled})),
			chatux027Actions(
				html.Button(html.Props{Class: "button small", Type: "submit", Disabled: disabled || m.Callbacks.AddChannelProjectMilestone == nil, Text: m.t(KeyWidgetAdd)}),
				html.Button(html.Props{Class: "button secondary small", Type: "button", Text: m.t(KeyCancel),
					Data: map[string]string{"action": "manage-section", "id": "milestone", "extra": scope + ":1"}})))
	}
	return html.Div(html.Props{Class: "manage-milestone-add"}, toggle, html.Div(html.Props{ID: "chat-manage-milestone-add", Hidden: !open}, fields))
}
