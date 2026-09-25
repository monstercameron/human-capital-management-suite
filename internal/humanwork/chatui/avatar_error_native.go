//go:build !js || !wasm

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func chatPhotoErrorHandler() ui.Handler { return ui.Handler{} }
func chatPhotoLoadHandler() ui.Handler  { return ui.Handler{} }

// chatAttachmentImageErrorHandler is a no-op on the native build; see
// avatar_error_wasm.go (C-2).
func chatAttachmentImageErrorHandler() ui.Handler { return ui.Handler{} }
