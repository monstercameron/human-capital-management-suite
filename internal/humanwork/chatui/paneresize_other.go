//go:build !(js && wasm)

package chatui

// installPaneResize is a browser concern (see paneresize_js.go); the
// server-rendered tree carries the column widths as attributes the browser
// build applies on hydration.
func installPaneResize() {}

// paneResizeCommit is assigned by the render so the browser half can persist
// a dragged width; the server-rendered tree never drags.
var paneResizeCommit func(pane string, px int)
