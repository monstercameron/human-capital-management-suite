//go:build !(js && wasm)

package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func docsSuggestEventIndex(ui.MouseEvent) string { return "" }
