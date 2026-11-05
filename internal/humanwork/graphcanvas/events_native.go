//go:build !js || !wasm

package graphcanvas

import "github.com/monstercameron/GoWebComponents/v5/ui"

// Native stubs: outside a browser there are no DOM events, so coordinates
// read as zero. Tests drive the interaction machine directly.
func pointerCoords(_ string, _ ui.MouseEvent) (x, y float64, button int, shift bool) {
	return 0, 0, 0, false
}

func wheelDeltas(_ string, _ ui.MouseEvent) (dx, dy, x, y float64) {
	return 0, 0, 0, 0
}

func clickOffset(_ string, _ ui.MouseEvent) (x, y float64) { return 0, 0 }
