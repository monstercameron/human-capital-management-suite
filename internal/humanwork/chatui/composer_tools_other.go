//go:build !js || !wasm

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// Outside the browser there is no window to measure and nothing to press: the
// tool row's decisions live in composer_tools.go, and these are its seams.

func viewportAtLeast(int) bool                 { return true }
func closeComposerAddMenu(bool)                {}
func dismissComposerAddMenu(ui.Event)          {}
func composerAddMenuKey(ui.KeyboardEvent) bool { return false }
func clickComposerControl(string)              {}
func insertComposerMention(string)             {}
func openComposerLocation()                    {}

func rememberComposerLayerOpener(string) {}
