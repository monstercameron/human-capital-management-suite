//go:build !js || !wasm

package chatui

import "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"

// The native build has no network client: a press does nothing.
func chatlangReadingChanged() {}

func chattoneSendAsWritten(_, _ string, done func(error)) { done(nil) }

func chatlangSendReport(_ string, _ chatrender.Rendering, _ string, done func(error)) { done(nil) }
