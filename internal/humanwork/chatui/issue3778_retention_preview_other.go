//go:build !js || !wasm

package chatui

// Without a browser there is no server to ask: native rendering tests draw the
// panel from a model and never press its button.
func chatRetentionPreviewCall(func(ChatRetentionPreviewData, int, error)) {}
