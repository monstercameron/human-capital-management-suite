//go:build !(js && wasm)

package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func useOrganizationVirtualViewport(bool) bool { return false }

func organizationScrollTop(ui.Event) float64 { return 0 }

func useOrganizationVirtualScrollPosition(float64) {}
