//go:build js && wasm

package main

import (
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatNavigationForAddress is the navigation state an address entry is stamped
// with while its conversation opens: the thread the address names stays in the
// address bar until it has been opened or found unavailable (#3965).
func chatNavigationForAddress(navigation chatui.Model, hash string) chatui.Model {
	if parent := chatAddressOpenThread(hash); parent != "" {
		navigation.ShowThread, navigation.ThreadParentID = true, parent
	}
	return navigation
}

// chatRestoreAddressedThread opens the thread an address names once the
// conversation has been read, the way a click on its reply count would. The
// reader's own read of the conversation decides: a root that is not in it
// (deleted, outside the reader's access, never existed) opens nothing, the
// address drops the thread, and one plain line says so.
func chatRestoreAddressedThread(id, parent string) {
	if parent == "" {
		return
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		model := chatBrowser.snapshot()
		if model.SelectedID != id {
			return
		}
		if !chatBrowser.alreadyOpening(id) && (model.State == chatui.StateReady || model.State == chatui.StateEmpty) {
			if model.ShowThread && model.ThreadParentID == parent {
				return
			}
			if open := model.Callbacks.OpenThread; open != nil {
				for _, message := range model.Messages {
					if message.ID == parent {
						ui.PostAsync(func() { open(parent) })
						return
					}
				}
			}
			chatThreadAddressUnavailable(id, parent)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func chatThreadAddressUnavailable(id, parent string) {
	model := chatBrowser.snapshot()
	if model.SelectedID != id || (model.ShowThread && model.ThreadParentID == parent) {
		return
	}
	chatHistory.replace(model)
	noteChatAction(chatui.ThreadUnavailableNotice(chatBrowser.localeTag()))
}
