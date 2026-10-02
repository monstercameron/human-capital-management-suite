package chatui

import (
	"net/url"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-005: the Conversation details panel leads with what people open it
// for. The order is About, Pinned, Members (agents first), Notifications for
// me, then one collapsed "Manage channel" group for people who may manage the
// channel. The panel remembers which groups were open for the page session in
// the client's own local state, never in browser storage.

const (
	chatux005GroupManage  = "manage"
	chatux005GroupProject = "project"
)

// chatux005Text is the reviewed copy of this panel in the three product
// languages. A catalog that answers a missing key with a placeholder never
// reaches the page.
func chatux005Text(m Model, key string) string {
	copy := map[string][3]string{
		"about":         {"About", "Info", "نبذة"},
		"created_by":    {"Created by {name}", "Erstellt von {name}", "أنشأها {name}"},
		"people":        {"People", "Personen", "الأشخاص"},
		"agents":        {"Agents", "Agenten", "الوكلاء"},
		"manage":        {"Manage channel", "Kanal verwalten", "إدارة القناة"},
		"project":       {"Project and milestones", "Projekt und Meilensteine", "المشروع والمراحل الرئيسية"},
		"notifications": {"Notifications for me", "Benachrichtigungen für mich", "الإشعارات الخاصة بي"},
		"pin_copy":      {"Copy link", "Link kopieren", "نسخ الرابط"},
		"no_pins":       {"Nothing is pinned yet. Pin a message from its menu.", "Noch nichts angeheftet. Heften Sie eine Nachricht über ihr Menü an.", "لا توجد رسائل مثبّتة بعد. ثبّت رسالة من قائمتها."},
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

// setDetailGroup records whether one group of the details panel is open.
func (u *localUI) setDetailGroup(id string, open bool) {
	next := make(map[string]bool, len(u.detailGroups)+1)
	for key, value := range u.detailGroups {
		next[key] = value
	}
	next[id] = open
	u.detailGroups = next
}

// chatux005Group is a collapsed group of the details panel. Its button flips
// the body at once through the Chat disclosure handler, and the click is also
// recorded in the local state so the group stays as the person left it when
// the panel is redrawn, closed or opened on another channel.
func chatux005Group(h handlers, id, label string, children ...ui.Node) ui.Node {
	open := h.local.detailGroups[id]
	bodyID := "chat-details-group-" + id
	toggle := html.Button(html.Props{Class: "chat-disclosure-button details-group-toggle", Type: "button",
		Data: map[string]string{"chat-disclosure-toggle": "true", "action": "details-group", "id": id},
		Aria: map[string]string{"expanded": boolString(open), "controls": bodyID}},
		html.Span(html.Props{Text: label}), icon("chevron-down"))
	body := html.Div(html.Props{ID: bodyID, Class: "chat-disclosure-body details-group-body", Hidden: !open, Data: map[string]string{"chat-disclosure-body": "true"}}, children...)
	return html.Div(html.Props{Class: "details-group", Data: map[string]string{"chat-disclosure": "true", "details-group": id}}, toggle, body)
}

// chatux005Click handles the details panel's group buttons. It reports whether
// the click was one of them.
func chatux005Click(e ui.MouseEvent, local localStore) bool {
	action, id, _ := eventAction(e)
	if action != "details-group" || id == "" {
		return false
	}
	open := !local.get().detailGroups[id]
	local.update(func(u *localUI) { u.setDetailGroup(id, open) })
	return true
}

// chatux005MayManage is who sees the Manage channel group: the people who may
// run the channel's settings, and anyone the server lets change its status.
func chatux005MayManage(m Model, c Conversation) bool {
	if canAdministerConversation(m, c) {
		return true
	}
	view, ok := m.ChannelStatuses[m.SelectedID]
	return ok && m.ChangeChannelStatus != nil && len(view.Transitions) > 0 && !view.Loading && !view.Unavailable
}

// chatux005CreatorName is the name of the channel's owner, who created it, when
// the roster names them. The model carries no creation time, so none is shown.
func chatux005CreatorName(m Model, c Conversation) string {
	if c.OwnerID == "" {
		return ""
	}
	if c.OwnerID == m.CurrentUser {
		return m.t(KeyYou)
	}
	for _, member := range m.Members {
		if member.ID == c.OwnerID && member.Name != "" && member.Name != member.ID {
			return member.Name
		}
	}
	return ""
}

func chatux005About(m Model, c Conversation, parts *channelWidgetParts) ui.Node {
	heading := []ui.Node{html.H3(html.Props{Text: chatux005Text(m, "about")})}
	if parts != nil {
		heading = append(heading, html.Div(html.Props{Class: "details-section-actions"}, parts.TeamPin))
	}
	children := []ui.Node{html.Div(html.Props{Class: "details-section-head"}, heading...)}
	if parts != nil {
		children = append(children, widgetError(m), parts.Purpose)
		if name := chatux005CreatorName(m, c); name != "" {
			children = append(children, html.P(html.Props{Class: "details-about-created muted", Dir: "auto", Text: strings.ReplaceAll(chatux005Text(m, "created_by"), "{name}", name)}))
		}
	}
	if view, ok := m.ChannelStatuses[m.SelectedID]; ok {
		if chip := ChannelStatusChip(m, view); chip != nil {
			children = append(children, html.Div(html.Props{Class: "details-about-status"},
				html.Span(html.Props{Class: "muted", Text: chatstateText(m, "title")}), chip), chatstateHistory(m, view))
		}
	}
	if len(children) == 1 {
		return nil
	}
	return html.Section(html.Props{ID: "chat-details-about", Class: "details-section details-about", Aria: map[string]string{"label": chatux005Text(m, "about")}}, children...)
}

// chatux005Pinned is the Pinned section. It keeps its id with nothing pinned,
// and says how to pin, so the header can always open the panel at it.
func chatux005Pinned(m Model) ui.Node {
	if len(m.ChannelPins) > 0 {
		return pinnedSection(m)
	}
	return html.Section(html.Props{ID: "chat-details-pinned", Class: "details-section", TabIndex: -1, Aria: map[string]string{"label": m.t(KeyPinned)}},
		html.H3(html.Props{Text: m.t(KeyPinned)}),
		html.P(html.Props{Class: "field-hint", Text: chatux005Text(m, "no_pins")}))
}

// chatux005Agent is one agent of this conversation as the member list shows it.
type chatux005Agent struct {
	persona *ResolvedPersonaMention
	member  *Member
	name    string
}

// chatux005Agents lists the agents of the conversation: the resolved ones the
// reader may ask, then agents the roster carries that none of those cover.
func chatux005Agents(m Model) []chatux005Agent {
	var out []chatux005Agent
	personas := chatConversationAgents(m)
	for i := range personas {
		out = append(out, chatux005Agent{persona: &personas[i], name: personas[i].Reference.Display})
	}
	for i := range m.Members {
		member := &m.Members[i]
		if !member.Agent {
			continue
		}
		covered := false
		for _, persona := range personas {
			if member.ID == persona.Reference.ID || (persona.Actor != nil && member.ID == persona.Actor.AgentID) {
				covered = true
				break
			}
		}
		if !covered {
			name := member.Name
			if name == "" || name == member.ID {
				name = m.t(KeyTodoMemberFallback)
			}
			out = append(out, chatux005Agent{member: member, name: name})
		}
	}
	return out
}

func chatux005AgentRow(m Model, agent chatux005Agent) ui.Node {
	if agent.persona == nil {
		member := agent.member
		return html.Li(html.Props{Class: "member-row"},
			agentDMAvatar(agent.name, "avatar small agent-dm-avatar", agentIconFor(m, []string{member.ID}, agent.name, member.Icon)),
			html.Span(html.Props{Class: "member-name", Dir: "auto", Text: agent.name}), AgentBadgeLabel(m.Locale))
	}
	persona := *agent.persona
	extra := []ui.Node{html.Button(html.Props{Class: "button secondary small persona-member-ask", Type: "button", Disabled: m.Callbacks.SendMessageWithReferences == nil,
		Data: map[string]string{"action": "agent-ask-here", "id": persona.Reference.ID},
		Aria: map[string]string{"label": chat5Text(m, "chat.agents.ask") + " " + persona.Reference.Display}, Text: chat5Text(m, "chat.agents.ask")})}
	if purpose := strings.TrimSpace(persona.Purpose); purpose != "" {
		extra = append(extra, html.P(html.Props{Class: "persona-member-purpose", Dir: "auto", Text: purpose}))
	}
	return personaMemberRowWith(m, persona, extra...)
}

func chatux005PersonRow(m Model, p Member) ui.Node {
	memberName := p.Name
	if memberName == "" || memberName == p.ID {
		memberName = m.t(KeyTodoMemberFallback)
	}
	avatarClass := "avatar small"
	if p.Online {
		avatarClass += " online"
	}
	name := memberName
	if p.ID != "" && p.ID == m.CurrentUser {
		name += " (" + m.t(KeyYou) + ")"
	}
	row := []ui.Node{personButton(m, p.ID, memberName, "member-person-button", personAvatar(m, p.ID, memberName, avatarClass), html.Span(html.Props{Class: "member-name", Text: name}))}
	if p.Online {
		row = append(row, html.Span(html.Props{Class: "member-status online", Text: m.t(KeyOnline)}))
	} else if p.Subtitle != "" {
		row = append(row, html.Span(html.Props{Class: "member-status", Text: p.Subtitle}))
	}
	return html.Li(html.Props{Class: "member-row"}, row...)
}

// chatux005Members is the Members section: the agents under their own
// subheading, then the people with the filter box.
func chatux005Members(m Model, h handlers, c Conversation) ui.Node {
	query := strings.ToLower(strings.TrimSpace(m.MemberQuery))
	matches := func(name string) bool { return query == "" || strings.Contains(strings.ToLower(name), query) }
	agents := chatux005Agents(m)
	agentRows := []ui.Node{}
	for _, agent := range agents {
		if matches(agent.name) {
			agentRows = append(agentRows, chatux005AgentRow(m, agent))
		}
	}
	people, personRows := 0, []ui.Node{}
	for _, p := range m.Members {
		if p.Agent {
			continue
		}
		people++
		name := p.Name
		if name == "" || name == p.ID {
			name = m.t(KeyTodoMemberFallback)
		}
		if matches(name) {
			personRows = append(personRows, chatux005PersonRow(m, p))
		}
	}
	children := []ui.Node{html.Div(html.Props{Class: "details-section-head"},
		html.H3(html.Props{Text: membersHeading(m, c)}),
		// ACCESS-01: public channels are open membership, so anyone there may add
		// people; a private channel or group is invite-only, so only its owner can.
		html.Div(html.Props{Class: "details-section-actions"},
			addMembersButton(m, c),
			actionButton("icon-button", "refresh-members", "", m.t(KeyRefreshMembers), m.Callbacks.LoadMembers == nil, icon("refresh"))))}
	list := html.Props{Class: "member-list"}
	if len(agents) > 0 {
		children = append(children, html.H4(html.Props{ID: "chat-agents-here", Class: "details-subheading", TabIndex: -1, Text: countedLabel(m, chatux005Text(m, "agents"), len(agents))}),
			html.Ul(html.Props{Class: "member-list agent-list", Aria: map[string]string{"labelledby": "chat-agents-here"}}, agentRows...))
		if m.IsTenantAdmin || (c.OwnerID == m.CurrentUser && m.CurrentUser != "") {
			children = append(children, html.A(html.Props{Class: "details-agents-manage", Href: "/workspace/app/admin/personas?locale=" + url.QueryEscape(m.Locale), Text: chat5Text(m, "chat.agents.manage")}))
		}
		children = append(children, html.H4(html.Props{ID: "chat-details-people", Class: "details-subheading", Text: countedLabel(m, chatux005Text(m, "people"), people)}))
		list.Aria = map[string]string{"labelledby": "chat-details-people"}
	}
	children = append(children, memberFilter(m, h), html.Ul(list, personRows...))
	return html.Section(html.Props{ID: "chat-details-members", Class: "details-section", TabIndex: -1, Aria: map[string]string{"label": membersHeading(m, c)}}, children...)
}

// chatux005Manage is the collapsed Manage channel group, or nil when the
// viewer may not manage the channel or there is nothing in it.
func chatux005Manage(m Model, h handlers, c Conversation, parts *channelWidgetParts) ui.Node {
	if !chatux005MayManage(m, c) {
		return nil
	}
	var body []ui.Node
	add := func(node ui.Node) {
		if node != nil {
			body = append(body, node)
		}
	}
	add(integrate2StatusDetails(m))
	if parts != nil {
		add(html.Section(html.Props{Class: "details-section channel-widget"},
			html.P(html.Props{Class: "muted", Text: m.t(KeyTeamNote)}), parts.Roles))
		add(chatux005Group(h, chatux005GroupProject, countedLabel(m, chatux005Text(m, "project"), parts.MilestoneCount),
			html.Div(html.Props{Class: "details-section-head"}, html.P(html.Props{Class: "muted", Text: m.t(KeyProjectNote)}), parts.ProjectPin),
			parts.ProjectEditor, parts.MilestoneList, parts.MilestoneAdder))
	}
	add(filterSettingsEntry(m, c))
	add(translationSettingsEntry(m, c))
	add(integrationsSection(m, c))
	if len(body) == 0 {
		return nil
	}
	return html.Section(html.Props{ID: "chat-details-manage", Class: "details-section details-manage"}, chatux005Group(h, chatux005GroupManage, chatux005Text(m, "manage"), body...))
}

// chatux005Details is the Conversation details panel.
func chatux005Details(m Model, h handlers) ui.Node {
	c := m.selected()
	var parts *channelWidgetParts
	if c.Kind == PublicChannel || c.Kind == PrivateChannel {
		built := channelWidgetPartsFor(m, h)
		parts = &built
	}
	mode := m.Preferences.Notifications[m.SelectedID]
	if mode == "" {
		mode = NotifyAll
	}
	// Keyed by conversation so the purpose, project and role forms start empty of
	// the last channel's typing when another channel opens.
	keyed := func(node ui.Node, name string) ui.Node {
		if node == nil {
			return nil
		}
		return html.WithKey(node, name+m.SelectedID)
	}
	return html.Aside(html.Props{Class: "chat-side chat-details", Role: "complementary", Data: map[string]string{"pane": "details"}, Aria: map[string]string{"label": m.t(KeyDetails)}},
		paneHandle(m, "details"),
		html.Div(html.Props{Class: "side-heading"}, html.H2(html.Props{Text: m.t(KeyDetails)}), actionButton("icon-button", "close-details", "", m.t(KeyCloseDetails), m.Callbacks.ToggleDetails == nil, icon("close"))),
		html.Div(html.Props{Class: "details-summary"}, conversationAvatar(m, c), html.H3(html.Props{Text: displayName(m, c)}), html.P(html.Props{Class: "conversation-topic", Text: m.t(kindKey(c.Kind))})),
		keyed(chatux005About(m, c, parts), "chatux005-about-"),
		chatux005Pinned(m),
		chatux005Members(m, h, c),
		notificationsControl(m, m.SelectedID, mode),
		chatgateDetailsSection(m),
		keyed(chatux005Manage(m, h, c, parts), "chatux005-manage-"),
	)
}
