//go:build !js || !wasm

package chatui

func bindChatMessageLongPress(Model) func() { return nil }

func focusAgentChatComposer() {}

func pinAgentChatAfterSend(string) {}
