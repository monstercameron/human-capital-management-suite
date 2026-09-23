//go:build !(js && wasm)

package chatui

func startChatGiphyPostEmbeds(string, string, string) func() { return func() {} }
