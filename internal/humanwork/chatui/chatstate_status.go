package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

type ChannelStatusView struct {
	Status               chat.ChannelStatus
	ActorName            string
	Transitions          []chat.StatusTransition
	CanPost              bool
	Loading, Unavailable bool
	Updated              bool
	Error                string
}

func chatstateActorName(m Model, v ChannelStatusView) string {
	if v.Status.ChangedBy == "system" {
		return chatstateText(m, "system")
	}
	if v.ActorName != "" && v.ActorName != v.Status.ChangedBy {
		return v.ActorName
	}
	for _, member := range m.Members {
		if member.ID == v.Status.ChangedBy && member.HomeTenantID == v.Status.TenantID && member.Name != "" && member.Name != member.ID {
			return member.Name
		}
	}
	return chatstateText(m, "none")
}

func chatstateKey(status chatpolicy.ChannelStatus) string {
	switch chatpolicy.NormalizeChannelStatus(status) {
	case chatpolicy.StatusOpen:
		return "open"
	case chatpolicy.StatusAnnouncements:
		return "announcements"
	case chatpolicy.StatusLocked:
		return "locked"
	case chatpolicy.StatusArchived:
		return "archived"
	default:
		return "unknown"
	}
}

// ChannelStatusBadge is shared by the header, sidebar, directory and search.
func ChannelStatusBadge(m Model, view ChannelStatusView) ui.Node {
	key := chatstateKey(view.Status.Status)
	if view.Loading || view.Unavailable {
		key = "unknown"
	}
	symbol := "○"
	switch key {
	case "announcements":
		symbol = "◉"
	case "locked":
		symbol = "🔒"
	case "archived":
		symbol = "▣"
	case "unknown":
		symbol = "?"
	}
	return html.Span(html.Props{Class: "chatstate-badge"}, html.Span(html.Props{Text: symbol, Aria: map[string]string{"hidden": "true"}}), html.Span(html.Props{Class: "chatstate-label", Text: chatstateText(m, key)}))
}

// ChannelStatusChip is the badge the conversation header and the sidebar row
// carry. An Open channel is the ordinary case and shows nothing; a status that
// is still loading shows nothing either. It returns nil in those cases.
func ChannelStatusChip(m Model, view ChannelStatusView) ui.Node {
	// A status that could not be read is not a status: the header and the sidebar
	// show nothing for it. The details panel reports the failure and offers Retry.
	if view.Loading || view.Unavailable || chatstateKey(view.Status.Status) == "open" {
		return nil
	}
	return ChannelStatusBadge(m, view)
}

// ChannelStatusComposerNotice replaces the composer entirely when posting is
// unavailable. A nil node means the usual composer remains available.
func ChannelStatusComposerNotice(m Model, view ChannelStatusView) ui.Node {
	key := ""
	switch {
	// While the status is loading, or when reading it failed, the composer stays.
	// The server decides whether a post is accepted and says so if it is not;
	// taking the composer away on a failed read would stop everyone posting
	// whenever the status service is slow or restarting.
	case view.Loading, view.Unavailable, view.CanPost:
		return nil
	case view.Status.Status == chatpolicy.StatusAnnouncements:
		key = "announcement_composer"
	case view.Status.Status == chatpolicy.StatusLocked:
		key = "locked_composer"
	case view.Status.Status == chatpolicy.StatusArchived:
		key = "archived_composer"
	default:
		key = "permission_composer"
	}
	// The sentence names the channel it is about, as the header does.
	name := displayName(m, m.selected())
	if k := m.selected().Kind; k == PublicChannel || k == PrivateChannel {
		name = "#" + name
	}
	notice := html.P(html.Props{Class: "chatstate-notice", Role: "status", Dir: "auto", Text: strings.ReplaceAll(chatstateText(m, key), "{name}", name)})
	if restore := chatbug082RestoreButton(m, view); restore != nil {
		return html.Div(html.Props{Class: "chatstate-notice-bar"}, notice, restore)
	}
	return notice
}

type ChannelStatusPanelProps struct {
	Model  Model
	View   ChannelStatusView
	Change func(chat.ChangeChannelStatusRequest)
	Retry  func()
	Now    time.Time
	// Section, when set, is the panel's row (CHATUX-027): the status draws its
	// form and notes as the content of that row instead of a row of its own.
	Section sectionWrap
}

// channelStatusPanel is the Status row of Manage channel: the current state at
// the right of the row, and under it the form to change it. Nothing is chosen
// when the form opens and Confirm waits until something is; the lock's end
// time is asked for only once Locked is the choice, and what the choice will
// change for members is written under it before anyone confirms. Failures and
// the live "status changed" line stay outside the row, so a closed row still
// announces them. The history of past changes is in About, not here.
func channelStatusPanel(props ChannelStatusPanelProps) ui.Node {
	m, v := props.Model, props.View
	errorText := ui.UseState("")
	choice := ui.UseState("")
	pick := ui.UseEvent(func(e ui.ChangeEvent) { choice.Set(statusChoiceOf(e.GetValue())) })
	submit := ui.UseEvent(func(e ui.FormEvent) {
		e.PreventDefault()
		change, ok := ChannelStatusFormRequest(v, domValue("chatstate-choice"), domValue("chatstate-reason"), domValue("chatstate-until"), props.Now)
		if !ok {
			errorText.Set(chatstateText(m, "invalid"))
			return
		}
		errorText.Set("")
		choice.Set("")
		setDOMValue("chatstate-choice", statusNoChoice)
		setDOMValue("chatstate-reason", "")
		setDOMValue("chatstate-until", "")
		if props.Change != nil {
			props.Change(change)
		}
	})
	retry := ui.UseEvent(func() {
		if props.Retry != nil {
			props.Retry()
		}
	})
	var row ui.Node
	form := channelStatusForm(m, v, props, choice.Get(), errorText.Get(), pick, submit)
	if props.Section != nil {
		return chatux027StatusSection(m, v, props, form, retry)
	}
	if form != nil {
		row = manageRow("manage-status", chatstateText(m, "title"), ChannelStatusBadge(m, v), form)
	} else {
		row = manageStatic("manage-status", chatstateText(m, "title"), ChannelStatusBadge(m, v))
	}
	children := []ui.Node{row}
	if v.Loading {
		children = append(children, html.P(html.Props{Class: "manage-note", Role: "status", Text: chatstateText(m, "loading")}))
	}
	if v.Unavailable {
		children = append(children, html.P(html.Props{Class: "manage-note", Role: "alert", Text: chatstateText(m, "error")}),
			html.Button(html.Props{Class: "button secondary small", Type: "button", OnClick: retry, Disabled: props.Retry == nil, Text: chatstateText(m, "retry")}))
	}
	if v.Error != "" {
		key := v.Error
		switch key {
		case "revision_conflict", "channel_held", "permission_denied", "last_reopener":
		default:
			key = "error"
		}
		children = append(children, html.P(html.Props{Class: "manage-note", Role: "alert", Text: chatstateText(m, key)}))
	}
	if v.Updated {
		children = append(children, html.P(html.Props{Class: "manage-note", Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}, Text: chatstateText(m, "updated") + ": " + chatstateText(m, chatstateKey(v.Status.Status))}))
	}
	return html.Div(html.Props{Class: "chatstate-section manage-status-block", Dir: agentReplyDirection(m.Locale)}, children...)
}

// channelStatusForm is the change form, or nil when this person may not change
// the status right now.
func channelStatusForm(m Model, v ChannelStatusView, props ChannelStatusPanelProps, choice, formError string, pick, submit ui.Handler) ui.Node {
	if len(v.Transitions) == 0 || props.Change == nil || v.Loading || v.Unavailable {
		return nil
	}
	options := []ui.Node{html.Option(html.Props{Value: statusNoChoice, Text: laneText(m, chatux021Copy, keyChatux021StatusChoose)})}
	effect := ""
	for _, transition := range v.Transitions {
		key := chatstateKey(transition.Status)
		if key == "unknown" {
			continue
		}
		options = append(options, html.Option(html.Props{Value: string(transition.Status), Text: chatstateText(m, key)}))
		if choice == string(transition.Status) {
			effect = chatstateText(m, key+"_effect")
		}
	}
	label := chatstateText(m, "change")
	if v.Status.Status == chatpolicy.StatusArchived {
		label = chatstateText(m, "restore")
	}
	fields := []ui.Node{
		html.Label(html.Props{For: "chatstate-choice", Text: label}),
		html.Select(html.Props{ID: "chatstate-choice", Name: "status", OnChange: pick, Aria: map[string]string{"describedby": "chatstate-effect"}}, options...),
		html.P(html.Props{ID: "chatstate-effect", Class: "chatstate-effect", Text: effect}),
	}
	if choice == string(chatpolicy.StatusLocked) {
		fields = append(fields, html.Label(html.Props{For: "chatstate-until", Text: chatstateText(m, "until")}),
			html.Input(html.Props{ID: "chatstate-until", Class: "chat-input", Name: "until", Type: "datetime-local", Aria: map[string]string{"describedby": "chatstate-form-error"}}))
	}
	fields = append(fields,
		html.Label(html.Props{For: "chatstate-reason", Text: chatstateText(m, "reason")}),
		html.Textarea(html.Props{ID: "chatstate-reason", Class: "chat-input", Name: "reason", Rows: 3, Required: true, Aria: map[string]string{"describedby": "chatstate-form-error"}, Raw: map[string]any{"maxlength": 2000}}),
		html.P(html.Props{ID: "chatstate-form-error", Class: "chatstate-form-error", Role: "alert", Text: formError}),
		chatux027StatusActions(m, props, choice))
	return html.Form(html.Props{Class: "chatstate-form", OnSubmit: submit}, fields...)
}

func ChannelStatusPanel(props ChannelStatusPanelProps) ui.Node {
	return ui.CreateElement(channelStatusPanel, props)
}

// ChannelStatusFormRequest reads the live DOM values supplied by the submit
// handler, so rerenders never overwrite what the person has typed.
func ChannelStatusFormRequest(view ChannelStatusView, choice, reason, until string, now time.Time) (chat.ChangeChannelStatusRequest, bool) {
	request := chat.ChangeChannelStatusRequest{TenantID: view.Status.TenantID, ConversationID: view.Status.ConversationID, ExpectedRevision: view.Status.Revision, Status: chatpolicy.ChannelStatus(choice), Reason: strings.TrimSpace(reason)}
	allowed := false
	for _, transition := range view.Transitions {
		if transition.Status == request.Status {
			allowed = true
		}
	}
	if !allowed || request.Reason == "" || len(request.Reason) > 2000 {
		return request, false
	}
	if until != "" {
		at, err := time.ParseInLocation("2006-01-02T15:04", until, time.Local)
		if err != nil || request.Status != chatpolicy.StatusLocked || !at.After(now) {
			return request, false
		}
		request.Until = &at
	}
	return request, true
}

// ArchivedChannelStatuses keeps archived channels out of ordinary projections.
func ArchivedChannelStatuses(views []ChannelStatusView, archived bool) []ChannelStatusView {
	out := []ChannelStatusView{}
	for _, view := range views {
		if (view.Status.Status == chatpolicy.StatusArchived) == archived {
			out = append(out, view)
		}
	}
	return out
}

func ChannelStatusSystemLine(m Model, view ChannelStatusView) ui.Node {
	actor := chatstateActorName(m, view)
	line := strings.NewReplacer("{actor}", actor, "{status}", chatstateText(m, chatstateKey(view.Status.Status)), "{reason}", view.Status.Reason).Replace(chatstateText(m, "system_line"))
	return html.P(html.Props{Class: "chatstate-notice", Role: "status", Dir: "auto", Aria: map[string]string{"live": "polite"}, Text: line})
}

type ChannelStatusDirectoryProps struct {
	Model    Model
	Views    []ChannelStatusView
	Archived bool
	Open     func(string)
	Restore  func(ChannelStatusView)
}

func channelStatusDirectory(props ChannelStatusDirectoryProps) ui.Node {
	click := ui.UseEvent(func(e ui.MouseEvent) {
		action, id, _ := eventAction(e)
		for _, view := range props.Views {
			if view.Status.ConversationID != id {
				continue
			}
			if action == "chatstate-open" && props.Open != nil {
				props.Open(id)
			}
			if action == "chatstate-restore" && props.Restore != nil {
				for _, transition := range view.Transitions {
					if transition.Status == chatpolicy.StatusOpen {
						props.Restore(view)
						break
					}
				}
			}
			return
		}
	})
	m := props.Model
	title := m.t(KeyBrowseTitle)
	if props.Archived {
		title = chatstateText(m, "archived")
	}
	rows := []ui.Node{}
	for _, view := range ArchivedChannelStatuses(props.Views, props.Archived) {
		name := view.Status.Name
		if strings.TrimSpace(name) == "" || name == view.Status.ConversationID {
			name = m.t(KeyConversation)
		}
		children := []ui.Node{html.Strong(html.Props{Text: name, Dir: "auto"}), ChannelStatusBadge(m, view), html.Button(html.Props{Type: "button", Text: chatstateText(m, "open_channel"), Disabled: props.Open == nil, Data: map[string]string{"action": "chatstate-open", "id": view.Status.ConversationID}})}
		if props.Archived && props.Restore != nil {
			for _, transition := range view.Transitions {
				if transition.Status == chatpolicy.StatusOpen {
					children = append(children, html.Button(html.Props{Type: "button", Text: chatstateText(m, "restore"), Data: map[string]string{"action": "chatstate-restore", "id": view.Status.ConversationID}}))
					break
				}
			}
		}
		rows = append(rows, html.Li(html.Props{Class: "chatstate-directory-row"}, children...))
	}
	if len(rows) == 0 && props.Archived {
		rows = append(rows, html.Li(html.Props{Text: chatstateText(m, "archive_empty")}))
	}
	return html.Section(html.Props{Class: "chatstate-section", OnClick: click, Dir: agentReplyDirection(m.Locale)}, html.H2(html.Props{Text: title}), html.Ul(html.Props{}, rows...))
}

func ChannelStatusDirectory(props ChannelStatusDirectoryProps) ui.Node {
	return ui.CreateElement(channelStatusDirectory, props)
}

// statusNoChoice is the value of the form's first option, "Choose a status".
// An option with an empty value takes its text as its value, so the empty
// choice is spelled out.
const statusNoChoice = "__none__"

// statusChoiceOf is the status a select value names, or "" for nothing chosen.
func statusChoiceOf(value string) string {
	if value = strings.TrimSpace(value); value != statusNoChoice {
		return value
	}
	return ""
}
