package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"time"
)

func integrate2MessageLocations(m Model, msg Message) []ui.Node {
	out := []ui.Node{}
	for _, view := range m.MessageLocations[msg.ID] {
		revision := view.MessageRevision
		if revision == 0 {
			revision = view.Share.PostRevision
		}
		if view.Share.PostID != msg.ID || revision != msg.Revision {
			continue
		}
		view.Locale = m.Locale
		view.ViewerID = m.CurrentUser
		view.ViewerTenantID = m.CurrentTenantID
		if view.Now.IsZero() {
			view.Now = time.Now().UTC()
		}
		if view.Sharer == "" {
			view.Sharer = msg.Author
		}
		out = append(out, ChatmapLocationEmbed(view))
	}
	return out
}
