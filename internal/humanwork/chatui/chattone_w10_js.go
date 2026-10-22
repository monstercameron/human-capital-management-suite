//go:build js && wasm

package chatui

import (
	"context"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// chatlangReadingChanged asks the page to read every message's selection again.
func chatlangReadingChanged() {
	ui.PostAsync(func() {
		js.Global().Get("document").Call("dispatchEvent", js.Global().Get("Event").New("chat-reading-settings-changed"))
	})
}

// chattoneSendAsWritten asks the server to deliver the writer's message as
// written and, when it did, asks the page to read the selections again.
func chattoneSendAsWritten(room, post string, done func(error)) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := renderingSettingsClient().SendAsWritten(ctx, room, post)
		if err == nil {
			chatlangReadingChanged()
		}
		done(err)
	}()
}

// chatlangSendReport sends a reader's report of a wrong translation.
func chatlangSendReport(room string, rendering chatrender.Rendering, reason string, done func(error)) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		done(renderingSettingsClient().ReportTranslation(ctx, room, rendering, reason))
	}()
}
