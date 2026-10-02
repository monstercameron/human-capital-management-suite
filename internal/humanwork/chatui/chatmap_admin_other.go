//go:build !js || !wasm

package chatui

import (
	"errors"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var errChatmapAdminUnavailable = errors.New("chatui: location settings need a browser")

// Without a browser there is no server to ask: native rendering tests draw the
// settings from a model and never run the panel's effects.
func chatmapAdminCall(string, any, func(ChatmapAdminData, int, error)) func() { return nil }
func chatmapAdminReadPolicy(ui.Event) (any, error)                            { return nil, errChatmapAdminUnavailable }
func chatmapAdminReadCountry(ui.Event) (any, error)                           { return nil, errChatmapAdminUnavailable }
func chatmapAdminChangedField(ui.Event) string                                { return "" }
