//go:build !js || !wasm

package chatui

// Without a browser there is no server to ask: native rendering tests draw the
// rows from a model and never run their effects.
func voiceSettingsCall(string, any, func(VoiceSettingsData, int, error)) func() { return nil }
func voiceSettingsNotify()                                                      {}
func voiceSettingsReset(string, bool)                                           {}
