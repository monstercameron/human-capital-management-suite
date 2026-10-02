package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"sort"
	"time"
)

func integrate2StatusDetails(m Model) ui.Node {
	view, ok := m.ChannelStatuses[m.SelectedID]
	if !ok {
		return nil
	}
	return ChannelStatusPanel(ChannelStatusPanelProps{Model: m, View: view, Change: m.ChangeChannelStatus, Retry: m.RetryChannelStatus, Now: time.Now()})
}

func integrate2ArchivedChannels(m Model) ui.Node {
	if m.ChatFeatures == nil || !m.ChatFeatures.Status {
		return nil
	}
	views := []ChannelStatusView{}
	for _, v := range m.ChannelStatuses {
		if v.Status.Status == chatpolicy.StatusArchived {
			views = append(views, v)
		}
	}
	if len(views) == 0 {
		return nil
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Status.Name < views[j].Status.Name })
	return chatPolishDisclosure(html.Props{Class: "chatstate-archived"}, chatPolishDisclosureLabel(html.Props{Text: chatstateText(m, "archived")}), ChannelStatusDirectory(ChannelStatusDirectoryProps{Model: m, Views: views, Archived: true, Open: m.Callbacks.SelectConversation, Restore: func(v ChannelStatusView) {
		if m.Callbacks.OpenConversationDetails != nil {
			m.Callbacks.OpenConversationDetails(v.Status.ConversationID)
		}
	}}))
}
