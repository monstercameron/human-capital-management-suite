//go:build !js || !wasm

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// Without a browser there is no server to ask: native rendering tests draw the
// page from a model and never run its effects.
func chatAuditCall(string, any, func(ChatAuditData, int, error)) func() { return nil }
func chatAuditExport(ChatAuditFilter, func(int, error))                 {}
func chatAuditReadFilter(ui.Event) ChatAuditFilter                      { return ChatAuditFilter{} }
func chatAuditResetForm()                                               {}
