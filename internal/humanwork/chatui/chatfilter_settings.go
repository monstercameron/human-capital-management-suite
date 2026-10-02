package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// canAdministerConversation reports whether the viewer may run this
// conversation's settings: a workspace administrator, its owner, or one of its
// current channel managers. It is the same set the server accepts for managing
// message filters and for installing an app, so a control behind it is never
// shown to someone the server will refuse.
func canAdministerConversation(m Model, c Conversation) bool {
	if m.IsTenantAdmin {
		return true
	}
	if m.CurrentUser == "" {
		return false
	}
	if c.OwnerID == m.CurrentUser {
		return true
	}
	if c.ID != m.SelectedID {
		return false
	}
	for _, member := range m.ChannelTeam.Members {
		if member.SubjectID == m.CurrentUser && (member.HomeTenantID == "" || member.HomeTenantID == m.CurrentTenantID) && member.Role == "MEMBERSHIP_ROLE_MANAGER" {
			return true
		}
	}
	return false
}

// filterSettingsEntry is the Filters section of Conversation details. It is
// absent for a person who may not manage filters. The settings themselves open
// inside the panel from the client's FilterSettings view; a model without one
// shows the read-only summary and no control rather than a link to nowhere.
func filterSettingsEntry(m Model, c Conversation) ui.Node {
	if m.ChatFeatures != nil && !m.ChatFeatures.Filters {
		return nil
	}
	if !canAdministerConversation(m, c) {
		return nil
	}
	// Keyed by conversation so opening another one starts closed and reloads.
	return html.WithKey(ui.CreateElement(filterSettingsSection, filterSettingsProps{Model: m}), "chatfilter-"+c.ID+boolString(m.ShowFilterSettings))
}

type filterSettingsProps struct{ Model Model }

func filterSettingsSection(props filterSettingsProps) ui.Node {
	m := props.Model
	// Only one of the two panels is open at a time: they share their field ids.
	initial := ""
	if m.ShowFilterSettings {
		initial = "channel"
	}
	open := ui.UseState(initial)
	toggleChannel := ui.UseEvent(func() {
		open.Update(func(current string) string {
			if current == "channel" {
				return ""
			}
			return "channel"
		})
	})
	toggleWorkspace := ui.UseEvent(func() {
		open.Update(func(current string) string {
			if current == "workspace" {
				return ""
			}
			return "workspace"
		})
	})
	children := []ui.Node{html.H3(html.Props{Text: chatfilterText(m, "title")}), html.P(html.Props{Class: "field-hint", Text: chatfilterText(m, "direct")})}
	if m.FilterSettings != nil {
		children = append(children, html.Button(html.Props{Class: "chat-disclosure-button", Type: "button", OnClick: toggleChannel, Aria: map[string]string{"expanded": boolString(open.Get() == "channel")}},
			html.Span(html.Props{Text: chatfilterText(m, "manage")}), icon("chevron-down")))
		if open.Get() == "channel" {
			children = append(children, html.Div(html.Props{Class: "chat-disclosure-body chatfilter-entry-body"}, m.FilterSettings()))
		}
	}
	// A workspace administrator also reaches the workspace's own filters here.
	if m.IsTenantAdmin && m.WorkspaceFilterSettings != nil {
		children = append(children, html.Button(html.Props{Class: "chat-disclosure-button", Type: "button", OnClick: toggleWorkspace, Aria: map[string]string{"expanded": boolString(open.Get() == "workspace")}},
			html.Span(html.Props{Text: modadminText(m, "ws_entry")}), icon("chevron-down")))
		if open.Get() == "workspace" {
			children = append(children, html.Div(html.Props{Class: "chat-disclosure-body chatfilter-entry-body"}, m.WorkspaceFilterSettings()))
		}
	}
	return html.Section(html.Props{Class: "details-section chatfilter-entry", Dir: direction(m.Locale)}, children...)
}
