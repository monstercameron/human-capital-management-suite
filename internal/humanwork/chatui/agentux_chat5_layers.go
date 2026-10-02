package chatui

import (
	"net/url"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type chatLayerRect struct{ left, top, right, bottom float64 }
type chatLayerGeometry struct{ left, top, width, height float64 }

// Above-only layers use the space above the entire message or composer.
func anchoredChatGeometry(anchor chatLayerRect, width, height, viewportWidth, viewportHeight float64, above, rtl bool) chatLayerGeometry {
	width = min(width, max(0, viewportWidth-16))
	height = min(height, max(0, viewportHeight-16))
	left := anchor.right - width
	if rtl {
		left = anchor.left
	}
	left = max(8, min(left, viewportWidth-width-8))
	// A layer above its anchor keeps chatLayerAboveGap clear of it, so a menu
	// never sits flush against the message bar that opened it.
	const chatLayerAboveGap = 10
	below, over := max(0, viewportHeight-anchor.bottom-12), max(0, anchor.top-8-chatLayerAboveGap)
	useAbove := above || height > below
	if useAbove && over < height && below > over {
		useAbove = false
	}
	top := anchor.bottom + 4
	if useAbove {
		height = min(height, over)
		top = anchor.top - height - chatLayerAboveGap
	} else {
		height = min(height, below)
	}
	return chatLayerGeometry{left, max(8, top), width, height}
}

// chatLayerRectUsable reports whether a measured rectangle belongs to an
// attached, laid-out element. A detached node measures as all zeros, which the
// geometry above would turn into a corner of the page.
func chatLayerRectUsable(r chatLayerRect) bool { return r.right-r.left > 0 && r.bottom-r.top > 0 }

// chatLayerAnchorRect prefers the opener's live rectangle and falls back to the
// rectangle remembered when it was pressed; false means there is nothing to
// anchor to and the layer must stay where it is.
func chatLayerAnchorRect(live, remembered chatLayerRect) (chatLayerRect, bool) {
	if chatLayerRectUsable(live) {
		return live, true
	}
	if chatLayerRectUsable(remembered) {
		return remembered, true
	}
	return chatLayerRect{}, false
}

// chatLayerAbove lists the layers that open above their opener's bar or field.
func chatLayerAbove(kind string) bool {
	switch kind {
	case "reaction", "emoji", "menu", "voice", "writing-style":
		return true
	}
	return false
}

// chatLayerPickOpener chooses among the buttons that carry the remembered
// action and id. The hover bar and the reaction chips both carry "react-pick"
// for one message, so the one sitting where the user pressed (in the bar or
// not) wins, and one with no layout is never chosen while another has some.
func chatLayerPickOpener(inBar, usable []bool, wasInBar bool) int {
	first := -1
	for i := range inBar {
		if i >= len(usable) || !usable[i] {
			continue
		}
		if inBar[i] == wasInBar {
			return i
		}
		if first < 0 {
			first = i
		}
	}
	if first >= 0 {
		return first
	}
	return -1
}

// chatRowPointerActivates: hovering shows a row's actions only where there is
// room and a fine pointer. On a phone-width viewport or a touch screen the
// emulated or sticky hover would draw them on a row nobody is touching.
func chatRowPointerActivates(pointerType string, compact bool) bool {
	return !compact && pointerType != "touch"
}

// chatRowFocusActivates: keyboard focus always shows a row's actions; on a
// compact viewport a tap focuses the row without :focus-visible, and that tap
// is how the actions are asked for.
func chatRowFocusActivates(focusVisible, compact bool) bool { return compact || focusVisible }

// chatLayerSelfPlacedAttr (data-chat-layer-self) marks a layer that positions
// itself: the shared layer code shows it in the top layer but never measures or
// moves it, so the two never fight over its rectangle.
const chatLayerSelfPlacedAttr = "chat-layer-self"

func anchoredChatLayer(props html.Props, kind string, children ...ui.Node) ui.Node {
	if props.Data == nil {
		props.Data = map[string]string{}
	}
	props.Data["chat-layer"] = kind
	if props.Raw == nil {
		props.Raw = map[string]any{}
	}
	props.Raw["popover"] = "manual"
	return html.Div(props, children...)
}

func chatLayerKind(action string) string {
	switch action {
	case "open-todo", "tray-todo":
		return "todo"
	case "open-poll", "tray-poll":
		return "poll"
	case "emoji-toggle":
		return "emoji"
	case "react-pick":
		return "reaction"
	case "menu":
		return "menu"
	case "details", "stats", "agents-here":
		return "details"
	case "reply":
		return "thread"
	case "chat-search-open":
		return "search"
	}
	return ""
}

func chatRowActionsVisible(local localUI, id string) bool {
	return id != "" && (local.pointerRow == id || local.focusRow == id)
}

func chat5RecentEmoji(previous []string, picked string) []string {
	if picked == "" {
		return append([]string(nil), previous...)
	}
	recent := []string{picked}
	for _, emoji := range previous {
		if emoji != picked && len(recent) < 6 {
			recent = append(recent, emoji)
		}
	}
	return recent
}

func chatComposerHint(m Model, h handlers) ui.Node {
	var hint ui.Node
	if strings.TrimSpace(m.Draft) != "" && !h.mentionView.Open && !h.local.emojiCompletion.Open && !h.local.docSuggest.Open {
		hint = composerUnresolvedMention(m, h.composerAgentName)
		if hint == nil {
			hint = composerAgentReplyHint(h.mentionReplyHint)
		}
	}
	return html.Div(html.Props{Class: "composer-hint-slot"}, hint)
}

func chat5Clock(locale string, at time.Time) string {
	clock := at.Local().Format("3:04 PM")
	if strings.HasPrefix(locale, "de") {
		clock = at.Local().Format("15:04")
	}
	if strings.HasPrefix(locale, "ar") {
		clock = arabicDigits(at.Local().Format("15:04"))
	}
	return clock
}

// The rail yields its field while the anchored search panel is open.
func chatSearchLayer(m Model, h handlers) ui.Node {
	if !h.local.searchOpen {
		return nil
	}
	return anchoredChatLayer(html.Props{Class: "chat-search-layer", Role: "dialog", Aria: map[string]string{"label": chatux001Text(m, "chat.ux001.search")}}, "search",
		html.Div(html.Props{Class: "chat-search-layer-head"}, html.Label(html.Props{For: "chat-search", Text: chatux001Text(m, "chat.ux001.search")}), actionButton("icon-button", "chat-search-close", "", m.t(KeyClose), false, icon("close"))),
		html.Input(html.Props{ID: "chat-search", Class: "chat-search", Type: "search", Placeholder: chatux001Text(m, "chat.ux001.search"), Data: map[string]string{"chat-value": m.Search}, AutoComplete: "off", OnInput: h.searchInput, Aria: map[string]string{"controls": "chat-main"}}))
}

// The outer card owns elapsed copy; the runtime continues to own progress and Stop.
func chat5ProgressFrame(m Model, projection PersonaProgressProjection) ui.Node {
	row := RenderPersonaProgress(m, projection)
	if projection.Progress == nil || projection.Progress.ElapsedSeconds < 5 || projection.Progress.pastDeadline(time.Now()) {
		return row
	}
	for _, child := range row.Children {
		if article, ok := child.(*ui.Element); ok && article.Type == "article" {
			seconds := m.nz(projection.Progress.ElapsedSeconds)
			if dateLocale(m.Locale) == "ar" {
				seconds = arabicDigits(seconds)
			}
			article.Children = append(article.Children, html.Span(html.Props{Class: "agent-elapsed-seconds", Text: seconds + " " + personaProgressText(m, "chat.agent.elapsed_seconds", "seconds")}))
		}
	}
	return row
}

func chatConversationAgents(m Model) []ResolvedPersonaMention {
	seen := map[string]bool{}
	var agents []ResolvedPersonaMention
	for _, agent := range m.ResolvedPersonaMentions {
		ref := agent.Reference
		if !validResolvedPersonaReference(ref, m.SelectedID) || ref.TenantID != m.CurrentTenantID || seen[ref.ID] {
			continue
		}
		seen[ref.ID] = true
		agents = append(agents, agent)
	}
	return agents
}

func chatAgentsSection(m Model) ui.Node {
	agents := chatConversationAgents(m)
	if len(agents) == 0 {
		return nil
	}
	rows := []ui.Node{html.H3(html.Props{Text: chat5Text(m, "chat.agents.here")})}
	for _, agent := range agents {
		scope := chatAgentReadScope(m, agent)
		rows = append(rows, html.Div(html.Props{Class: "chat-agent-here"},
			agentDMAvatar(agent.Reference.Display, "avatar small agent-dm-avatar", agentIconFor(m, []string{agent.Reference.ID}, agent.Reference.Display, agent.Icon)), html.Strong(html.Props{Dir: "auto", Text: agent.Reference.Display}), AgentBadgeLabel(m.Locale),
			html.P(html.Props{Text: agent.Purpose}), html.P(html.Props{Text: scope}),
			html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: m.Callbacks.SendMessageWithReferences == nil,
				Data: map[string]string{"action": "agent-ask-here", "id": agent.Reference.ID},
				Aria: map[string]string{"label": chat5Text(m, "chat.agents.ask") + " " + agent.Reference.Display}, Text: chat5Text(m, "chat.agents.ask")})))
	}
	if m.IsTenantAdmin || (m.selected().OwnerID == m.CurrentUser && m.CurrentUser != "") {
		rows = append(rows, html.A(html.Props{Href: "/workspace/app/admin/personas?locale=" + url.QueryEscape(m.Locale), Text: chat5Text(m, "chat.agents.manage")}))
	}
	return html.Section(html.Props{ID: "chat-agents-here", Class: "details-section", TabIndex: -1, Aria: map[string]string{"label": chat5Text(m, "chat.agents.here")}}, rows...)
}

func chatAgentReadScope(m Model, agent ResolvedPersonaMention) string {
	switch agent.DocumentScope {
	case "CHANNEL_DOCUMENTS":
		return chat5Text(m, "chat.agents.reads_channel")
	case "WORKSPACE_DOCUMENTS":
		return chat5Text(m, "chat.agents.reads_workspace")
	}
	for _, class := range agent.DataClasses {
		if class == "POLICY_DOCUMENT" {
			return chatPolishPolicyScope(m.Locale)
		}
	}
	return chat5Text(m, "chat.agents.reads_channel")
}

func selectChatAgent(m Model, mentions mentionStore, id string) {
	for _, agent := range chatConversationAgents(m) {
		if agent.Reference.ID == id {
			mentions.RemovePersonas("chat-composer", m.SelectedID)
			mentions.AddPersonaToken("chat-composer", m.SelectedID, agent.Reference)
			mentions.Set(mentionState{})
			focusAgentChatComposer()
			return
		}
	}
}

func chatMessageMenuItems(m Model, msg Message) []ui.Node {
	pinAction, pinLabel := "pin", m.t(KeyPin)
	if msg.Pinned {
		pinAction, pinLabel = "unpin", m.t(KeyUnpin)
	}
	copyLabel := m.t(KeyCopyLink)
	if m.PinReferenceUnavailable {
		copyLabel = m.t(KeyPinCopyGuestUnavailable)
	}
	items := []ui.Node{html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: m.Callbacks.CopyLink == nil || m.PinReferenceUnavailable, Data: map[string]string{"action": "copy-link", "id": msg.ID}, Title: copyLabel}, icon("link"), html.Span(html.Props{Text: copyLabel})), copyContentsMenuItem(m, msg)}
	shareDisabled := m.Callbacks.OpenShare == nil || strings.TrimSpace(msg.Body) == ""
	shareTitle := m.t(KeyShareToChannel)
	if strings.TrimSpace(msg.Body) == "" && len(msg.Attachments) > 0 {
		shareTitle = m.t(KeyShareAttachments)
	}
	shareAria := m.t(KeyShareToChannel)
	if shareTitle != m.t(KeyShareToChannel) {
		shareAria += ". " + shareTitle
	}
	items = append(items, html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: shareDisabled, Title: shareTitle, Aria: map[string]string{"label": shareAria}, Data: map[string]string{"action": "open-share", "id": msg.ID}}, icon("reply"), html.Span(html.Props{Text: m.t(KeyShareToChannel)})))
	if msg.AuthorID == m.CurrentUser {
		items = append(items,
			html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: m.Callbacks.BeginEdit == nil, Title: m.t(KeyEdit), Data: map[string]string{"action": "edit", "id": msg.ID}}, icon("edit"), html.Span(html.Props{Text: m.t(KeyEdit)})),
		)
		items = append(items, chatlangMenuItems(m, msg)...)
	}
	items = append(items, html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: m.Callbacks.Pin == nil, Data: map[string]string{"action": pinAction, "id": msg.ID}, Title: pinLabel}, icon("pin"), html.Span(html.Props{Text: pinLabel})))
	items = append(items, chatsaveAction(m, msg, true))
	items = append(items, html.Div(html.Props{Class: "menu-separator", Role: "separator"}))
	// CHATBUG-030: a person's own message has exactly one destructive command,
	// Delete message. Somebody else's message offers Report, plus the
	// moderator's "Remove for everyone" (after its own separator) for a viewer
	// who holds the removal permission. Never both Delete and Remove.
	if msg.AuthorID == m.CurrentUser {
		items = append(items, html.Button(html.Props{Class: "menu-item danger", Type: "button", Role: "menuitem", Disabled: m.Callbacks.DeleteMessage == nil, Title: m.t(KeyDelete), Data: map[string]string{"action": "delete", "id": msg.ID}}, icon("trash"), html.Span(html.Props{Text: m.t(KeyDelete)})))
	} else {
		items = append(items, moderationMenuLink(m.Locale, m.SelectedID, msg.ID, "report"))
		if m.chatmod005CanRemove() {
			items = append(items, html.Div(html.Props{Class: "menu-separator", Role: "separator"}), moderationMenuLink(m.Locale, m.SelectedID, msg.ID, "remove"))
		}
	}
	return items
}
