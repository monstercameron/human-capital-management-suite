//go:build !js || !wasm

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func chatPhotoErrorHandler() ui.Handler { return ui.Handler{} }
func chatPhotoLoadHandler() ui.Handler  { return ui.Handler{} }
