//go:build js && wasm

package main

import "net/url"

// chatmodTabOf is the tab a Moderation page address names, so the loading frame
// and the failed frame show the tab that was asked for.
func chatmodTabOf(href string) string {
	parsed, err := url.Parse(href)
	if err != nil {
		return ""
	}
	return parsed.Query().Get("tab")
}
