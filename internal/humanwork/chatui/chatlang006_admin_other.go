//go:build !js || !wasm

package chatui

import (
	"errors"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var errTranslationAdminUnavailable = errors.New("chatui: translation administration needs a browser")

// Without a browser there is no server to ask: native rendering tests draw the
// form from a model and never run the panel's effects.
func translationAdminCall(string, string, any, func(TranslationAdminData, int, error)) func() {
	return nil
}
func translationAdminReadWorkspace(ui.Event, TranslationAdminData) (any, error) {
	return nil, errTranslationAdminUnavailable
}
func translationAdminReadChannel(ui.Event, string) (any, error) {
	return nil, errTranslationAdminUnavailable
}
func translationAdminReadTerm(ui.Event) (any, error) { return nil, errTranslationAdminUnavailable }
func translationAdminReadTermID(ui.Event) string     { return "" }
