//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"syscall/js"
)

// chatmod005Meaningful is true when the summary changes what the page draws.
func chatmod005Meaningful(s chatui.ModerationState) bool {
	return s.Visible() || len(s.Notices) > 0 || s.RemoveEverywhere || s.ReviewEverywhere || len(s.Removable) > 0 || len(s.Reviewable) > 0
}

// chatmod005Refresh brings the chat up to date after a moderation change and
// reads the moderation summary again: the count on the sidebar entry, the
// notices, and where Remove and Restore are offered. Nothing waits on a timer or
// on the page having focus; it runs when the page loads and after each change.
func chatmod005Refresh(cfg journeyclient.Config, reloadChat bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if reloadChat {
		_, _ = loadChatProjection(ctx)
		refreshChatRoute()
	}
	summary, err := chatmod005SummaryWith(ctx, http.DefaultClient, cfg)
	if err != nil {
		return
	}
	next := chatui.ModerationStateFromSummary(summary)
	ui.PostAsync(func() {
		var previous chatui.ModerationState
		chatBrowser.mutate(func(m *chatui.Model) {
			previous = m.Moderation
			m.Moderation = next
		})
		if reflect.DeepEqual(previous, next) || !(chatmod005Meaningful(previous) || chatmod005Meaningful(next)) {
			return
		}
		if previous.Visible() == next.Visible() && reflect.DeepEqual(previous.Notices, next.Notices) && reflect.DeepEqual(previous.Removable, next.Removable) && reflect.DeepEqual(previous.Reviewable, next.Reviewable) {
			// Only the count changed: write it where it is shown.
			if badge := js.Global().Get("document").Call("querySelector", "[data-moderation-count]"); badge.Truthy() {
				text := strconv.Itoa(next.Attention())
				if label := chatBrowser.snapshot().Number; label != nil {
					text = label(next.Attention())
				}
				badge.Set("textContent", text)
				badge.Set("hidden", next.Attention() == 0)
			}
			return
		}
		refreshChatRoute()
	})
}
