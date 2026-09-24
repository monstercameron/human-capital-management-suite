//go:build js && wasm

package chatui

import "syscall/js"

// MountWebEmbed creates a cross-origin iframe with the minimum browser
// capability needed for interactive content. The caller owns the returned
// cleanup. Invalid or unsupported embeds show localized fallback text.
func MountWebEmbed(parent js.Value, embed WebEmbedDescriptor, fallback string, onResize func(int)) func() {
	if !parent.Truthy() || parent.Get("appendChild").Type() != js.TypeFunction {
		return func() {}
	}
	doc := js.Global().Get("document")
	if !doc.Truthy() || doc.Get("createElement").Type() != js.TypeFunction || ValidateWebEmbedDescriptor(embed) != nil {
		appendWebEmbedFallback(parent, doc, fallback, "")
		return func() {}
	}
	frame := doc.Call("createElement", "iframe")
	frame.Set("className", "chat-web-embed-frame")
	frame.Call("setAttribute", "title", embed.Title)
	frame.Call("setAttribute", "sandbox", "allow-scripts")
	frame.Call("setAttribute", "referrerpolicy", "no-referrer")
	frame.Call("setAttribute", "allow", "")
	frame.Call("setAttribute", "loading", "lazy")
	frame.Call("setAttribute", "data-chat-embed-origin", embed.Origin)
	frame.Call("setAttribute", "src", embed.URL)
	parent.Call("appendChild", frame)

	win := js.Global().Get("window")
	var messageHandler js.Func
	if onResize != nil && win.Truthy() && win.Get("addEventListener").Type() == js.TypeFunction {
		messageHandler = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			event := args[0]
			if event.Get("origin").Type() != js.TypeString || event.Get("origin").String() != embed.Origin || !event.Get("source").Equal(frame.Get("contentWindow")) {
				return nil
			}
			data := event.Get("data")
			if !data.Truthy() || data.Type() != js.TypeObject || data.Get("version").Type() != js.TypeNumber || data.Get("grant").Type() != js.TypeString || data.Get("nonce").Type() != js.TypeString || data.Get("action").Type() != js.TypeString || data.Get("height").Type() != js.TypeNumber {
				return nil
			}
			height, err := ValidateWebEmbedBridge(embed, WebEmbedBridgeMessage{
				Version: data.Get("version").Int(), Grant: data.Get("grant").String(), Nonce: data.Get("nonce").String(),
				Action: data.Get("action").String(), Height: data.Get("height").Int(),
			})
			if err == nil {
				frame.Get("style").Set("height", itoa(height)+"px")
				onResize(height)
			}
			return nil
		})
		win.Call("addEventListener", "message", messageHandler)
	}
	var errorHandler js.Func
	if frame.Get("addEventListener").Type() == js.TypeFunction {
		errorHandler = js.FuncOf(func(js.Value, []js.Value) any {
			frame.Call("setAttribute", "hidden", "")
			appendWebEmbedFallback(parent, doc, fallback, embed.URL)
			return nil
		})
		frame.Call("addEventListener", "error", errorHandler, js.ValueOf(map[string]any{"once": true}))
	}
	return func() {
		if messageHandler.Value.Type() == js.TypeFunction {
			win.Call("removeEventListener", "message", messageHandler)
			messageHandler.Release()
		}
		if errorHandler.Value.Type() == js.TypeFunction {
			frame.Call("removeEventListener", "error", errorHandler)
			errorHandler.Release()
		}
		frame.Call("remove")
	}
}

func appendWebEmbedFallback(parent, doc js.Value, text, approvedURL string) {
	if !doc.Truthy() || doc.Get("createElement").Type() != js.TypeFunction {
		return
	}
	status := doc.Call("createElement", "div")
	status.Set("className", "chat-web-embed-fallback")
	status.Call("setAttribute", "role", "status")
	status.Set("textContent", text)
	if approvedURL != "" {
		link := doc.Call("createElement", "a")
		link.Set("textContent", approvedURL)
		link.Call("setAttribute", "href", approvedURL)
		link.Call("setAttribute", "target", "_blank")
		link.Call("setAttribute", "rel", "noopener noreferrer")
		status.Call("appendChild", link)
	}
	parent.Call("appendChild", status)
}
