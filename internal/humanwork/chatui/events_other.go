//go:build !(js && wasm)

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// The server-rendered tree never dispatches events; these answer "nothing".
func eventAction(ui.Event) (string, string, string)                    { return "", "", "" }
func eventOnBackdrop(ui.Event) bool                                    { return false }
func eventPane(ui.Event) string                                        { return "" }
func domValue(string) string                                           { return "" }
func setDOMValue(string, string)                                       {}
func focusSectionCreate()                                              {}
func closeSectionCreate(bool)                                          {}
func sectionCreateOpen() bool                                          { return false }
func ensureSectionCreateVisible()                                      {}
func positionRailMenu(ui.Event)                                        {}
func menuTriggerIsFocusVisible(ui.Event) bool                          { return false }
func focusRailMenu(func(), bool)                                       {}
func restoreRailMenuFocus(string)                                      {}
func moveRailMenuFocus(ui.Event) bool                                  { return false }
func clearRailMenuDismiss()                                            {}
func focusMessageMenu(string, bool)                                    {}
func rememberMessageMenuTrigger(ui.Event, string)                      {}
func syncOpenMessageMenu(string)                                       {}
func clearMessageMenuGeometry()                                        {}
func restoreMessageMenuFocus(string)                                   {}
func moveMessageMenuFocus(ui.Event, string) bool                       { return false }
func openImageViewer(ui.Event, string, string, string, string, string) {}
func syncImageViewer(string, string)                                   {}
func startChatImageLoading() func()                                    { return func() {} }
