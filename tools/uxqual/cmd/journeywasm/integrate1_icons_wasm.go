//go:build js && wasm

package main

import (
	"context"
	"encoding/base64"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"net/http"
	"strconv"
	"strings"
	"syscall/js"
	"time"
)

func installIntegrate1Icons(cfg journeyclient.Config) func() {
	disposed := false
	busy := false
	document := js.Global().Get("document")
	click := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if disposed || len(args) == 0 {
			return nil
		}
		target := args[0].Get("target")
		if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
			return nil
		}
		button := target.Call("closest", "[data-agent-icon-action]")
		if !button.Truthy() {
			return nil
		}
		args[0].Call("preventDefault")
		root := button.Call("closest", "[data-agent-icon-controls]")
		action := button.Call("getAttribute", "data-agent-icon-action").String()
		preview := root.Call("querySelector", "[data-agent-icon-preview]")
		if action == "cancel" {
			preview.Set("hidden", true)
			root.Call("querySelector", "[data-agent-icon-action='regenerate']").Call("focus")
			return nil
		}
		if busy {
			return nil
		}
		busy = true
		button.Set("disabled", true)
		id := root.Call("getAttribute", "data-agent-icon-controls").String()
		revision, _ := strconv.ParseInt(root.Call("getAttribute", "data-agent-icon-revision").String(), 10, 64)
		locale := productui.LocaleContext{Resolved: root.Call("getAttribute", "data-agent-icon-locale").String()}
		endpoint := action
		commandAction := action
		if action == "regenerate" {
			endpoint = "preview"
		}
		if action == "apply" {
			endpoint = "regenerate"
			commandAction = "regenerate"
		}
		if action == "upload" {
			integrate1IconUpload(cfg, root, button, locale, id, revision, func() { busy = false })
			return nil
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			var reply struct {
				Icon     agenticon.Value `json:"icon"`
				Revision int64           `json:"revision"`
				ImageURL string          `json:"image_url"`
			}
			input := struct {
				PersonaID        string `json:"persona_id"`
				ExpectedRevision int64  `json:"expected_revision"`
				Action           string `json:"action"`
			}{id, revision, commandAction}
			err := integrate1IconRequest(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), endpoint, input, &reply)
			ui.PostAsync(func() {
				if disposed {
					return
				}
				busy = false
				button.Set("disabled", false)
				status := root.Call("querySelector", "[data-agent-icon-status]")
				if err != nil || !reply.Icon.Valid() {
					status.Set("textContent", productui.Integrate1IconText(locale, "failed"))
					return
				}
				if action == "regenerate" {
					markup := agenticon.SVG(reply.Icon)
					root.Call("querySelector", "[data-agent-icon-preview-image]").Set("innerHTML", markup)
					preview.Set("hidden", false)
					preview.Call("querySelector", "button").Call("focus")
					return
				}
				root.Call("setAttribute", "data-agent-icon-revision", strconv.FormatInt(reply.Revision, 10))
				preview.Set("hidden", true)
				root.Call("querySelector", "[data-agent-icon-action='undo']").Set("hidden", action == "undo")
				if card := root.Call("closest", ".persona-admin-card"); card.Truthy() {
					if image := card.Call("querySelector", ".persona-admin-card-title .agent-icon"); image.Truthy() {
						image.Set("outerHTML", agenticon.SVG(reply.Icon))
					}
				}
				status.Set("textContent", productui.Integrate1IconText(locale, "changed"))
				if action == "apply" || action == "undo" {
					root.Call("querySelector", "[data-agent-icon-action='regenerate']").Call("focus")
				}
			})
		}()
		return nil
	})
	document.Call("addEventListener", "click", click)
	return func() { disposed = true; document.Call("removeEventListener", "click", click); click.Release() }
}

// integrate1IconUpload reads the owner's chosen file and posts it to the
// upload endpoint. Only PNG and JPEG are sent; the server validates and
// re-encodes before storing.
func integrate1IconUpload(cfg journeyclient.Config, root, button js.Value, locale productui.LocaleContext, id string, revision int64, release func()) {
	status := root.Call("querySelector", "[data-agent-icon-status]")
	fail := func() {
		release()
		button.Set("disabled", false)
		status.Set("textContent", productui.Integrate1IconText(locale, "failed"))
	}
	fileInput := root.Call("querySelector", "[data-agent-icon-file]")
	if !fileInput.Truthy() || fileInput.Get("files").Length() == 0 {
		fail()
		return
	}
	file := fileInput.Get("files").Index(0)
	mediaType := file.Get("type").String()
	if mediaType != "image/png" && mediaType != "image/jpeg" {
		fail()
		return
	}
	document := js.Global().Get("document")
	reader := js.Global().Get("FileReader").New()
	var onload, onerror js.Func
	onerror = js.FuncOf(func(js.Value, []js.Value) any {
		onload.Release()
		onerror.Release()
		fail()
		return nil
	})
	onload = js.FuncOf(func(js.Value, []js.Value) any {
		defer onload.Release()
		defer onerror.Release()
		dataURL := reader.Get("result").String()
		comma := strings.Index(dataURL, ",")
		raw, err := base64.StdEncoding.DecodeString(dataURL[comma+1:])
		if comma < 0 || err != nil {
			fail()
			return nil
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var reply struct {
				Icon     agenticon.Value `json:"icon"`
				Revision int64           `json:"revision"`
				ImageURL string          `json:"image_url"`
			}
			input := struct {
				PersonaID        string `json:"persona_id"`
				ExpectedRevision int64  `json:"expected_revision"`
				Action           string `json:"action"`
				Image            []byte `json:"image"`
				ImageMediaType   string `json:"image_media_type"`
			}{id, revision, "upload", raw, mediaType}
			err := integrate1IconRequest(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), "upload", input, &reply)
			ui.PostAsync(func() {
				release()
				button.Set("disabled", false)
				if err != nil || !reply.Icon.Valid() {
					status.Set("textContent", productui.Integrate1IconText(locale, "failed"))
					return
				}
				root.Call("setAttribute", "data-agent-icon-revision", strconv.FormatInt(reply.Revision, 10))
				root.Call("querySelector", "[data-agent-icon-action='undo']").Set("hidden", false)
				if card := root.Call("closest", ".persona-admin-card"); card.Truthy() {
					if image := card.Call("querySelector", ".persona-admin-card-title .agent-icon"); image.Truthy() {
						image.Set("outerHTML", agenticon.SVG(reply.Icon))
						if url := strings.TrimSpace(reply.ImageURL); url != "" {
							if fresh := card.Call("querySelector", ".persona-admin-card-title .agent-icon"); fresh.Truthy() {
								img := document.Call("createElement", "img")
								img.Set("className", "agent-icon-photo-img")
								img.Call("setAttribute", "src", url)
								img.Call("setAttribute", "alt", "")
								img.Call("setAttribute", "loading", "lazy")
								img.Call("setAttribute", "decoding", "async")
								fresh.Get("classList").Call("add", "agent-icon-photo")
								fresh.Call("appendChild", img)
							}
						}
					}
				}
				status.Set("textContent", productui.Integrate1IconText(locale, "changed"))
			})
		}()
		return nil
	})
	reader.Set("onload", onload)
	reader.Set("onerror", onerror)
	reader.Call("readAsDataURL", file)
}

var integrate1IconsCleanup func()

func configureIntegrate1Icons(cfg journeyclient.Config) {
	if integrate1IconsCleanup != nil {
		integrate1IconsCleanup()
	}
	integrate1IconsCleanup = installIntegrate1Icons(cfg)
}
