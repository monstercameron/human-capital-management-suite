//go:build !(js && wasm)

package chatui

// chatside001InstallDrag is a browser concern (see chatside001_drag_js.go); the
// server-rendered tree never drags.
func chatside001InstallDrag(func(string, string)) {}

// chatside001InstallTouch is the browser's long press on a row (see
// chatside001_touch_js.go).
func chatside001InstallTouch(func(string)) {}
