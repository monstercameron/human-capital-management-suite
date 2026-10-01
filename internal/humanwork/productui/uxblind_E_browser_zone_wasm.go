//go:build js && wasm

package productui

import (
	"syscall/js"
	"time"
)

func browserLocalHour() (int, bool) {
	now := time.Now()
	intl := js.Global().Get("Intl")
	if !intl.Truthy() {
		return now.Hour(), true
	}
	options := intl.Get("DateTimeFormat").New().Call("resolvedOptions")
	zone := options.Get("timeZone").String()
	location, err := time.LoadLocation(zone)
	if err != nil {
		return now.Hour(), true
	}
	return now.In(location).Hour(), true
}
