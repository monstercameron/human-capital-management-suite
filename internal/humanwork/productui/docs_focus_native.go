//go:build !(js && wasm)

package productui

import (
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Outside the browser there is no focus, clipboard or timer to drive:
// server-rendered Docs dialogs and forms work in document order without them.

func docsFocusFirst([]string, bool) bool { return false }

func docsFocusIfIdle(string) {}

func docsTrapModal(string, string, []string) func() { return nil }

func docsBindMenuKeys() func() { return nil }

func docsAfter(time.Duration, func()) func() { return nil }

func docsEventInDialog(ui.Event) bool { return false }

func docsEventInside(ui.Event, string) bool { return false }
