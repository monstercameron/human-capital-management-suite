package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

// CHATBUG-082: the notice under an archived channel's messages carries a
// Restore button for the people who may restore it. The button opens the
// details panel at the Status row of Manage channel, where the reason is asked
// for, instead of leaving the notice to say "a workspace administrator can".

// chatbug082CanRestore reports whether this person may take the channel out of
// the archive: the server offered the Open transition and the page can send it.
func chatbug082CanRestore(m Model, view ChannelStatusView) bool {
	if view.Status.Status != chatpolicy.StatusArchived || m.ChangeChannelStatus == nil || view.Loading || view.Unavailable {
		return false
	}
	for _, transition := range view.Transitions {
		if transition.Status == chatpolicy.StatusOpen {
			return true
		}
	}
	return false
}

func chatbug082RestoreButton(m Model, view ChannelStatusView) ui.Node {
	if !chatbug082CanRestore(m, view) {
		return nil
	}
	return html.Button(html.Props{Class: "button secondary small chatstate-restore", Type: "button", Data: map[string]string{"action": "channel-restore", "id": view.Status.ConversationID}, Text: chatstateText(m, "restore")})
}

// chatbug082Click opens the details at the Status row of Manage channel. It
// reports whether the click was the notice's Restore button.
func chatbug082Click(e ui.MouseEvent, m Model, local localStore) bool {
	if action, _, _ := eventAction(e); action != "channel-restore" {
		return false
	}
	cb := m.Callbacks
	if cb.ToggleDetails == nil {
		return true
	}
	if m.ShowThread && cb.CloseThread != nil {
		cb.CloseThread()
	}
	if m.ShowPerson && cb.ClosePerson != nil {
		cb.ClosePerson()
	}
	local.update(func(u *localUI) { u.setDetailGroup(chatux005GroupManage, true) })
	cb.ToggleDetails(true)
	chatux001ScrollDetails("restore")
	return true
}
