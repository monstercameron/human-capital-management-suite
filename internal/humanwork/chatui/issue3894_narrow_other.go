//go:build !js || !wasm

package chatui

// issue3894Narrow reports whether the thread covers the channel. Native
// renders never do.
var issue3894Narrow = func() bool { return false }
