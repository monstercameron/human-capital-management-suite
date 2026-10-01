//go:build js && wasm

package main

import (
	"syscall/js"
	"time"
)

// formatClockInstant renders an instant in the viewer's own locale and time
// zone. The server sends the instant itself; how it reads is the browser's job.
func formatClockInstant(at time.Time, locale string) string {
	intl := js.Global().Get("Intl")
	if !intl.Truthy() || !intl.Get("DateTimeFormat").Truthy() {
		return at.Local().Format("Jan 2, 3:04 PM")
	}
	options := js.Global().Get("Object").New()
	options.Set("dateStyle", "medium")
	options.Set("timeStyle", "short")
	formatter := intl.Get("DateTimeFormat").New(locale, options)
	return formatter.Call("format", js.Global().Get("Date").New(at.UnixMilli())).String()
}
