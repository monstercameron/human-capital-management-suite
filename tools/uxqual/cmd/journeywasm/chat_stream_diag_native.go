//go:build !(js && wasm)

package main

// publishChatStreamLog has nowhere to publish outside the browser. The ring
// itself is shared so the logic that fills it is tested on the host.
func publishChatStreamLog(*chatStreamRing) {}
