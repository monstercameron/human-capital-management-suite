//go:build !(js && wasm)

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// shiftHeld has no keyboard on the server-rendered path; Enter never fires
// there, so the answer only matters in the browser build.
func shiftHeld(ui.KeyboardEvent) bool { return false }
