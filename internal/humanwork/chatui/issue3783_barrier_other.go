//go:build !js || !wasm

package chatui

import (
	"errors"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var errChatBarrierUnavailable = errors.New("chatui: information barriers need a browser")

// Without a browser there is no server to ask: native rendering tests draw the
// page from a model and never run its effects.
func chatBarrierCall(string, any, func(ChatBarrierData, string, int, error)) func() { return nil }
func chatBarrierReadAdd(ui.Event) (any, error)                                      { return nil, errChatBarrierUnavailable }
func chatBarrierReadRemove(ui.Event) (any, error)                                   { return nil, errChatBarrierUnavailable }
