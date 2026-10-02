//go:build js && wasm

package journeycss

// Stylesheet returns the page's CSS. The browser bundle carries the finished
// sheet as one string instead of the typed GWC declarations that build it
// (about 440 KB of compiled code for 62 KB of CSS); the server builds the
// sheet from those declarations, and TestStylesheetBlobMatchesBuilder keeps
// the two byte-identical, so the hash the server pins in style-src is the
// hash of the bytes the client injects.
func Stylesheet() string { return stylesheetBlob }
