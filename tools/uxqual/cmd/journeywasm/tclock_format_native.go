//go:build !(js && wasm)

package main

import "time"

// formatClockInstant is the native stand-in for the browser's locale-aware
// formatter, used by tests and the build tool; the served client always runs
// the wasm form.
func formatClockInstant(at time.Time, _ string) string { return at.Local().Format("Jan 2, 3:04 PM") }
