//go:build js && wasm

package main

import (
	"strings"
	"syscall/js"
)

func agentViewerTimeZone() string {
	intl := js.Global().Get("Intl")
	if !intl.Truthy() {
		return "UTC"
	}
	options := intl.Get("DateTimeFormat").New().Call("resolvedOptions")
	zone := strings.TrimSpace(options.Get("timeZone").String())
	if zone == "" {
		return "UTC"
	}
	return zone
}
