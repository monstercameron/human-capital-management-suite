//go:build !js || !wasm

package chatui

func nextChatRenderTick() uint64                { return 0 }
func rebindChatRootListeners(localStore, Model) {}
func releaseChatRootListeners()                 {}
