//go:build js && wasm

package chatui

import "syscall/js"

// localTimeZone is the browser's IANA zone, offered first in quiet hours.
func localTimeZone() string {
	intl := js.Global().Get("Intl")
	if !intl.Truthy() {
		return ""
	}
	zone := intl.Call("DateTimeFormat").Call("resolvedOptions").Get("timeZone")
	if zone.Type() != js.TypeString {
		return ""
	}
	return zone.String()
}
