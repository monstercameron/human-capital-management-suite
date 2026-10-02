//go:build !js || !wasm

package chatui

// The native build has no page to press the gear on and no network client.
func chatlangOpenSettings() {}

// The native build has no browser storage: a dismissal lasts the page session.
func chatlangBarStoredDismissed() bool { return false }
func chatlangRememberBarDismissed()    {}

func chatlangCorrect(_, _ string, _ uint64, _ string, done func(error)) { done(nil) }
