//go:build js && wasm

package chatui

import (
	"context"
	"encoding/json"
	"strings"
	"syscall/js"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// chatmapCrewInterval is how often an open crew view reads the map again.
const chatmapCrewInterval = 30 * time.Second

func chatmapOrigin() string { return js.Global().Get("location").Get("origin").String() }

// chatmapCrewWatch reads the channel's live map now and then every half minute
// until cancelled. The picture is drawn here, by the product's own code, from the
// positions the server returned, and shown through a blob of this page's origin;
// the previous blob is released when a new one replaces it.
func chatmapCrewWatch(tenant, conversation, locale string, names map[string]string, done func(chat.LiveMapView, string, error)) func() {
	ctx, cancel := context.WithCancel(context.Background())
	var blob string
	release := func() {
		if blob != "" {
			js.Global().Get("URL").Call("revokeObjectURL", blob)
			blob = ""
		}
	}
	go func() {
		for {
			var view chat.LiveMapView
			raw, _, err := chatmapRawRequest(ctx, "map", map[string]string{"TenantID": tenant, "ConversationID": conversation})
			if err == nil {
				err = json.Unmarshal(raw, &view)
			}
			if ctx.Err() != nil {
				release()
				return
			}
			picture := ""
			if err == nil {
				if pins := chatmapPins(view, names); len(view.Shares) > 0 && len(pins) > 0 {
					if drawn, perr := chat.RenderCrewMap(pins, chat.MapSize{Width: 400, Height: 300}, chatmapDocumentTheme(locale)); perr == nil {
						release()
						blob = chatmapBlob(drawn.Image)
						picture = blob
					}
				}
			}
			done(view, picture, err)
			select {
			case <-ctx.Done():
				release()
				return
			case <-time.After(chatmapCrewInterval):
			}
		}
	}()
	return func() {
		cancel()
	}
}

func chatmapBlob(data []byte) string {
	array := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(array, data)
	blob := js.Global().Get("Blob").New([]any{array}, map[string]any{"type": "image/svg+xml"})
	return js.Global().Get("URL").Call("createObjectURL", blob).String()
}

// chatmapDocumentTheme reads the page's own colours for the picture.
func chatmapDocumentTheme(locale string) chat.MapTheme {
	style := js.Global().Call("getComputedStyle", js.Global().Get("document").Get("documentElement"))
	read := func(key string) string { return strings.TrimSpace(style.Call("getPropertyValue", key).String()) }
	return chat.MapTheme{Locale: locale, Dark: strings.Contains(style.Get("colorScheme").String(), "dark"), Surface: read("--hcm-color-surface"), Text: read("--hcm-color-text"), Accent: read("--hcm-color-brand-primary")}
}
