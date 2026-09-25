//go:build js && wasm

package main

import (
	"encoding/json"
	"errors"
	"strconv"
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

const brandAssetUploadPath = "/workspace/brand-assets"
const brandAssetLifecyclePath = "/workspace/brand-assets/lifecycle"

var errBrandAssetUpload = errors.New("brand asset upload failed")

func uploadBrandAsset(cfg journeyclient.Config, filename string, content []byte, done func(string, error)) {
	go func() {
		fetch := js.Global().Get("fetch")
		if !fetch.Truthy() {
			ui.PostAsync(func() { done("", errBrandAssetUpload) })
			return
		}
		form := js.Global().Get("FormData").New()
		array := js.Global().Get("Uint8Array").New(len(content))
		js.CopyBytesToJS(array, content)
		parts := js.Global().Get("Array").New()
		parts.Call("push", array)
		blobOptions := js.Global().Get("Object").New()
		blobOptions.Set("type", "application/octet-stream")
		blob := js.Global().Get("Blob").New(parts, blobOptions)
		form.Call("append", "asset", blob, filename)
		headers := js.Global().Get("Object").New()
		headers.Set(journeyclient.AuthorizationHeader, journeyclient.BearerScheme+cfg.Bearer)
		options := js.Global().Get("Object").New()
		options.Set("method", "POST")
		options.Set("headers", headers)
		options.Set("body", form)
		options.Set("credentials", "same-origin")
		options.Set("cache", "no-store")
		response := awaitChatJS(fetch.Invoke(brandAssetUploadPath, options))
		if !response.Truthy() || response.Get("status").Int() != 201 {
			ui.PostAsync(func() { done("", errBrandAssetUpload) })
			return
		}
		wire := awaitChatJS(response.Call("json"))
		url := wire.Get("url")
		if !url.Truthy() || url.Type() != js.TypeString {
			ui.PostAsync(func() { done("", errBrandAssetUpload) })
			return
		}
		ui.PostAsync(func() { done(url.String(), nil) })
	}()
}

func loadBrandAssets(cfg journeyclient.Config, locale productui.LocaleContext, before int, done func([]productui.BrandAssetOption, int, error)) {
	go func() {
		fetch := js.Global().Get("fetch")
		if !fetch.Truthy() {
			ui.PostAsync(func() { done(nil, 0, errBrandAssetUpload) })
			return
		}
		headers := js.Global().Get("Object").New()
		headers.Set(journeyclient.AuthorizationHeader, journeyclient.BearerScheme+cfg.Bearer)
		options := js.Global().Get("Object").New()
		options.Set("method", "GET")
		options.Set("headers", headers)
		options.Set("credentials", "same-origin")
		options.Set("cache", "no-store")
		url := brandAssetUploadPath
		if before > 0 {
			url += "?before=" + strconv.Itoa(before)
		}
		response := awaitChatJS(fetch.Invoke(url, options))
		if !response.Truthy() || response.Get("status").Int() != 200 {
			ui.PostAsync(func() { done(nil, 0, errBrandAssetUpload) })
			return
		}
		wire := awaitChatJS(response.Call("json"))
		rows := wire.Get("assets")
		assets := make([]productui.BrandAssetOption, 0, rows.Length())
		for index := 0; index < rows.Length(); index++ {
			row := rows.Index(index)
			if row.Get("removed").Bool() {
				continue
			}
			revision := row.Get("revision").Int()
			url := ""
			if row.Get("url").Truthy() {
				url = row.Get("url").String()
			}
			assets = append(assets, productui.BrandAssetOption{Label: row.Get("name").String() + " · " + locale.Text("appearance.asset_revision_label", map[string]string{"revision": strconv.Itoa(revision)}), URL: url, Revision: revision, HeadRevision: row.Get("head_revision").Int(), CanRollback: row.Get("can_rollback").Bool(), CanRemove: row.Get("can_remove").Bool(), Digest: row.Get("digest").String(), Width: row.Get("width").Int(), Height: row.Get("height").Int()})
		}
		next := wire.Get("next_before").Int()
		ui.PostAsync(func() { done(assets, next, nil) })
	}()
}

func changeBrandAsset(cfg journeyclient.Config, action string, revision, expectedRevision int, done func(string, error)) {
	go func() {
		fetch := js.Global().Get("fetch")
		if !fetch.Truthy() {
			ui.PostAsync(func() { done("", errBrandAssetUpload) })
			return
		}
		payload, _ := json.Marshal(map[string]any{"action": action, "revision": revision, "expected_revision": expectedRevision})
		headers := js.Global().Get("Object").New()
		headers.Set(journeyclient.AuthorizationHeader, journeyclient.BearerScheme+cfg.Bearer)
		headers.Set("Content-Type", "application/json")
		options := js.Global().Get("Object").New()
		options.Set("method", "POST")
		options.Set("headers", headers)
		options.Set("body", string(payload))
		options.Set("credentials", "same-origin")
		options.Set("cache", "no-store")
		response := awaitChatJS(fetch.Invoke(brandAssetLifecyclePath, options))
		if !response.Truthy() || response.Get("status").Int() != 200 {
			ui.PostAsync(func() { done("", errBrandAssetUpload) })
			return
		}
		wire := awaitChatJS(response.Call("json"))
		url := wire.Get("url")
		if !url.Truthy() {
			ui.PostAsync(func() { done("", errBrandAssetUpload) })
			return
		}
		ui.PostAsync(func() { done(url.String(), nil) })
	}()
}
