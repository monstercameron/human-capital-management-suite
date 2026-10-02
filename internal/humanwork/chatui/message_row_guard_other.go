//go:build !js || !wasm

package chatui

func bindMessageMenuSurfaceGuard(func()) func() { return nil }
func bindChatRowActionGuard(localStore) func()  { return nil }
func reconcileChatRowActions(localStore)        {}
