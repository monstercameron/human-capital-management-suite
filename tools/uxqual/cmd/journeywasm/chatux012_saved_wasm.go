//go:build js && wasm

package main

import (
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// chatux012RetrySaved reads the saved list again (1 s, 2 s, 5 s, then every
// 15 s) after a read of it could not get through. Until CHATUX-012 a failed read
// switched the Saved row off and nothing switched it back on until the person
// reloaded; now a read that lands puts the list and its count back.
func chatux012RetrySaved(b *chatsaveBrowser) {
	key := "saved|" + b.cfg.Tenant + "|" + b.cfg.Subject
	chatux012Retry(key, func() bool { return !b.disposed }, func() bool {
		done := make(chan bool, 1)
		ui.PostAsync(func() {
			if b.disposed {
				done <- true
				return
			}
			b.listDown = false
			b.refresh = chatsaveRefresh{}
			b.retryDone = done
			b.sync()
		})
		select {
		case ok := <-done:
			return ok
		case <-time.After(30 * time.Second):
			return false
		}
	})
}
