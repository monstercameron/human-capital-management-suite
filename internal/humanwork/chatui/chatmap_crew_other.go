//go:build !js || !wasm

package chatui

import "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"

// Without a browser there is no server to ask: native rendering tests draw the
// crew view from a state and never run the panel's effects.
func chatmapCrewWatch(string, string, string, map[string]string, func(chat.LiveMapView, string, error)) func() {
	return nil
}

func chatmapOrigin() string { return "" }
