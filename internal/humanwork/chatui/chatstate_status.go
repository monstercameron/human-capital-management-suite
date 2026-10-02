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
	return html.P(html.Props{Class: "chatstate-notice", Role: "status", Text: chatstateText(m, key)})
}

type ChannelStatusPanelProps struct {
	Model  Model
	View   ChannelStatusView
	Change func(chat.ChangeChannelStatusRequest)
	Retry  func()
	Now    time.Time
}

func channelStatusPanel(props ChannelStatusPanelProps) ui.Node {
	m, v := props.Model, props.View
	errorText := ui.UseState("")
	submit := ui.UseEvent(func(e ui.FormEvent) {
		e.PreventDefault()
		change, ok := ChannelStatusFormRequest(v, domValue("chatstate-choice"), domValue("chatstate-reason"), domValue("chatstate-until"), props.Now)
		if !ok {
			errorText.Set(chatstateText(m, "invalid"))
			return
		}
		errorText.Set("")
		if props.Change != nil {
			props.Change(change)
		}
	})
	retry := ui.UseEvent(func() {
		if props.Retry != nil {
			props.Retry()
		}
	})
	children := []ui.Node{html.H3(html.Props{Text: chatstateText(m, "title")}), ChannelStatusBadge(m, v)}
	if v.Loading {
		children = append(children, html.P(html.Props{Role: "status", Text: chatstateText(m, "loading")}))
	}
	if v.Unavailable {
		children = append(children, html.P(html.Props{Role: "alert", Text: chatstateText(m, "error")}), html.Button(html.Props{Type: "button", OnClick: retry, Disabled: props.Retry == nil, Text: chatstateText(m, "retry")}))
	}
	if v.Error != "" {
		key := v.Error
		switch key {
		case "revision_conflict", "channel_held", "permission_denied", "last_reopener":
		default:
			key = "error"
		}
		children = append(children, html.P(html.Props{Role: "alert", Text: chatstateText(m, key)}))
	}
	if history := chatstateHistory(m, v); history != nil {
		children = append(children, history)
	}
	if v.Updated {
		children = append(children, html.P(html.Props{Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}, Text: chatstateText(m, "updated") + ": " + chatstateText(m, chatstateKey(v.Status.Status))}))
	}
	if len(v.Transitions) > 0 && props.Change != nil && !v.Loading && !v.Unavailable {
		options, effects := []ui.Node{}, []ui.Node{}
		for _, transition := range v.Transitions {
			key := chatstateKey(transition.Status)
			if key == "unknown" {
				continue
			}
			options = append(options, html.Option(html.Props{Value: string(transition.Status), Text: chatstateText(m, key)}))
			effects = append(effects, html.Li(html.Props{}, html.Strong(html.Props{Text: chatstateText(m, key) + ": "}), ui.Text(chatstateText(m, key+"_effect"))))
		}
		label := chatstateText(m, "change")
		if v.Status.Status == chatpolicy.StatusArchived {
			label = chatstateText(m, "restore")
		}
		children = append(children, chatPolishDisclosure(html.Props{}, chatPolishDisclosureLabel(html.Props{Text: label}), html.Form(html.Props{OnSubmit: submit},
			html.Label(html.Props{For: "chatstate-choice", Text: chatstateText(m, "change")}), html.Select(html.Props{ID: "chatstate-choice", Name: "status", Aria: map[string]string{"describedby": "chatstate-effects"}}, options...),
			html.Ul(html.Props{ID: "chatstate-effects", Class: "chatstate-effects"}, effects...),
			html.Label(html.Props{For: "chatstate-reason", Text: chatstateText(m, "reason")}), html.Textarea(html.Props{ID: "chatstate-reason", Name: "reason", Rows: 3, Required: true, Aria: map[string]string{"describedby": "chatstate-form-error"}, Raw: map[string]any{"maxlength": 2000}}),
			html.Label(html.Props{For: "chatstate-until", Text: chatstateText(m, "until")}), html.Input(html.Props{ID: "chatstate-until", Name: "until", Type: "datetime-local", Aria: map[string]string{"describedby": "chatstate-form-error"}}),
			html.P(html.Props{ID: "chatstate-form-error", Role: "alert", Text: errorText.Get()}), html.Button(html.Props{Type: "submit", Text: chatstateText(m, "confirm")}))))
	}
	return html.Section(html.Props{Class: "details-section chatstate-section", Dir: agentReplyDirection(m.Locale)}, children...)
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
