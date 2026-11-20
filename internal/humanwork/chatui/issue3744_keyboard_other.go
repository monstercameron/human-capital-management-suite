//go:build !(js && wasm)

package chatui

func bindChatKeyboardModel(Model) func() { return nil }
