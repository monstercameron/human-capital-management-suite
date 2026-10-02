//go:build js && wasm

package main

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// CHATUX-012: the reads made when a conversation opens. Each makes one attempt;
// a failed attempt leaves the page quiet and a retry loop (1 s, 2 s, 5 s, then
// every 15 s) runs until the read lands or the person leaves the conversation.
// A read that already has a loop is not started again, it is only hurried.

// chatux012RetryDirectory reads the worker directory again after a read of it
// failed, so names replace identifiers without the person opening anything.
func chatux012RetryDirectory(cfg journeyclient.Config) {
	chatux012Retry("directory|"+cfg.Tenant+"|"+cfg.Subject, chatux012Alive(cfg, ""), func() bool {
		chatBrowser.chatux012ClearDirectoryHold()
		return loadChatDirectory(cfg)
	})
}

// chatux012RetryDMPeer reads one direct conversation's other member again after
// the read of it failed, so the conversation gets its name without a reload.
func chatux012RetryDMPeer(cfg journeyclient.Config, roomID string) {
	chatux012Retry("dmpeer|"+roomID, chatux012Alive(cfg, ""), func() bool {
		if !chatBrowser.claimDMPeerRead(roomID) {
			return true
		}
		return readChatDMPeer(cfg, roomID)
	})
}

// chatux012RetryProjection reads the conversation list and the open
// conversation again after that read failed, until it lands or the person
// leaves Chat. The page already shows what it could not load; a read that lands
// replaces it.
func chatux012RetryProjection() {
	chatux012Retry("projection", func() bool { return currentPath() == productui.Path(productui.PageChat) }, func() bool {
		_, err := loadChatProjection(context.Background())
		refreshChatRoute()
		return err == nil && chatBrowser.loadError() == ""
	})
}

func loadChannelWidgets(cfg journeyclient.Config, room string) {
	key := "widgets|" + room
	if chatReadRetries.Active(key) {
		chatux012Retry(key, nil, nil)
		return
	}
	if loadChannelWidgetsOnce(cfg, room, false) {
		return
	}
	chatux012Retry(key, chatux012Alive(cfg, room), func() bool { return loadChannelWidgetsOnce(cfg, room, true) })
}

func loadChannelTodo(cfg journeyclient.Config, room string) {
	key := "todo|" + room
	if chatReadRetries.Active(key) {
		chatux012Retry(key, nil, nil)
		return
	}
	if loadChannelTodoOnce(cfg, room, false) {
		return
	}
	chatux012Retry(key, chatux012Alive(cfg, room), func() bool { return loadChannelTodoOnce(cfg, room, true) })
}

func loadChannelPoll(cfg journeyclient.Config, room string) {
	key := "poll|" + room
	if chatReadRetries.Active(key) {
		chatux012Retry(key, nil, nil)
		return
	}
	if loadChannelPollOnce(cfg, room, false) {
		return
	}
	chatux012Retry(key, chatux012Alive(cfg, room), func() bool { return loadChannelPollOnce(cfg, room, true) })
}
