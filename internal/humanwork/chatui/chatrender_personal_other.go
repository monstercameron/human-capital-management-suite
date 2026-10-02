//go:build !js || !wasm

package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// renderingChecksAvailability: without a browser there is no service to ask, so
// the row is drawn from the start (it is what native rendering tests look at).
const renderingChecksAvailability = false

func renderingLoadSettings(_ string, _ func(chatrender.Preference, error)) func() { return nil }
func renderingLoadLanguages(_ string, _ func(map[string]int, error)) func()       { return nil }
func renderingReadSettings(_ ui.Event, _ chatrender.Preference, _ string) (chatrender.Preference, string, error) {
	return chatrender.Preference{}, "", chatrender.ErrUnavailable
}
func renderingSaveSettings(_ string, _ chatrender.Preference, done func(error)) {
	done(chatrender.ErrUnavailable)
}
