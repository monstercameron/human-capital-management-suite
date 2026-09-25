//go:build !js || !wasm

package productui

// useProjectTaskDialog has no browser dialog to promote outside js/wasm:
// server rendering emits the closed <dialog> and the client opens it.
func useProjectTaskDialog(dialogID, taskID string, close func()) {}
