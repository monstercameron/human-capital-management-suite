//go:build js && wasm

package main

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"syscall/js"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func mediaWASMFunc(t *testing.T, fn func(js.Value, []js.Value) any) js.Func {
	t.Helper()
	f := js.FuncOf(fn)
	t.Cleanup(f.Release)
	return f
}

func mediaWASMPNG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	return out.Bytes()
}

func mediaWASMChunk(data []byte) js.Value {
	chunk := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(chunk, data)
	return chunk
}

func mediaWASMResponse(t *testing.T, contentType string, chunks []js.Value, cancelled *int) js.Value {
	t.Helper()
	next := 0
	read := mediaWASMFunc(t, func(js.Value, []js.Value) any {
		if next >= len(chunks) {
			return js.ValueOf(map[string]any{"done": true})
		}
		chunk := chunks[next]
		next++
		return js.ValueOf(map[string]any{"done": false, "value": chunk})
	})
	cancel := mediaWASMFunc(t, func(js.Value, []js.Value) any { *cancelled++; return nil })
	reader := js.ValueOf(map[string]any{"read": read, "cancel": cancel})
	getReader := mediaWASMFunc(t, func(js.Value, []js.Value) any { return reader })
	body := js.ValueOf(map[string]any{"getReader": getReader, "cancel": cancel})
	getHeader := mediaWASMFunc(t, func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].String() == "Content-Type" {
			return contentType
		}
		return js.Null()
	})
	return js.ValueOf(map[string]any{"status": 200, "headers": js.ValueOf(map[string]any{"get": getHeader}), "body": body})
}

func mediaWASMGlobals(t *testing.T, fetch js.Func, created, revoked *[]string) {
	t.Helper()
	global := js.Global()
	oldFetch, oldURL, oldDocument := global.Get("fetch"), global.Get("URL"), global.Get("document")
	createURL := mediaWASMFunc(t, func(js.Value, []js.Value) any {
		raw := "blob:test-" + string(rune('a'+len(*created)))
		*created = append(*created, raw)
		return raw
	})
	revokeURL := mediaWASMFunc(t, func(_ js.Value, args []js.Value) any {
		*revoked = append(*revoked, args[0].String())
		return nil
	})
	query := mediaWASMFunc(t, func(js.Value, []js.Value) any { return js.Null() })
	global.Set("fetch", fetch)
	global.Set("URL", js.ValueOf(map[string]any{"createObjectURL": createURL, "revokeObjectURL": revokeURL}))
	global.Set("document", js.ValueOf(map[string]any{"querySelector": query}))
	t.Cleanup(func() {
		global.Set("fetch", oldFetch)
		global.Set("URL", oldURL)
		global.Set("document", oldDocument)
	})
}

func TestChatMediaWASMStreamCapCancelsBeforeBlob(t *testing.T) {
	first := js.Global().Get("Uint8Array").New(chatMediaPreviewMaxBytes)
	second := js.ValueOf(map[string]any{"byteLength": 1})
	cancelled := 0
	response := mediaWASMResponse(t, "image/png", []js.Value{first, second}, &cancelled)
	fetch := mediaWASMFunc(t, func(js.Value, []js.Value) any { return response })
	created, revoked := []string{}, []string{}
	mediaWASMGlobals(t, fetch, &created, &revoked)
	raw, status, encoded, charge := readChatMediaBytes(journeyclient.Config{Tenant: "tenant", Bearer: "token"}, "room", "artifact", "grant", js.Null())
	if raw != "" || status != 413 || encoded != 0 || charge != 0 || cancelled != 1 || len(created) != 0 {
		t.Fatalf("oversized stream: url=%q status=%d encoded=%d charge=%d cancelled=%d blobs=%d", raw, status, encoded, charge, cancelled, len(created))
	}
}

func TestChatMediaWASMChargesEncodedAndDecodedBeforePublishing(t *testing.T) {
	data := mediaWASMPNG(t)
	response := mediaWASMResponse(t, "image/png", []js.Value{mediaWASMChunk(data)}, new(int))
	fetch := mediaWASMFunc(t, func(js.Value, []js.Value) any { return response })
	created, revoked := []string{}, []string{}
	mediaWASMGlobals(t, fetch, &created, &revoked)
	raw, status, encoded, charge := readChatMediaBytes(journeyclient.Config{Tenant: "tenant", Bearer: "token"}, "room", "artifact", "grant", js.Null())
	if raw == "" || status != 200 || encoded != int64(len(data)) || charge != int64(len(data))+24 || len(created) != 1 {
		t.Fatalf("valid image: url=%q status=%d encoded=%d charge=%d blobs=%d", raw, status, encoded, charge, len(created))
	}
	bad := mediaWASMResponse(t, "image/jpeg", []js.Value{mediaWASMChunk(data)}, new(int))
	fetchBad := mediaWASMFunc(t, func(js.Value, []js.Value) any { return bad })
	js.Global().Set("fetch", fetchBad)
	raw, status, _, charge = readChatMediaBytes(journeyclient.Config{Tenant: "tenant", Bearer: "token"}, "room", "artifact", "grant", js.Null())
	if raw != "" || status != 413 || charge != 0 || len(created) != 1 {
		t.Fatalf("mismatched type published preview: url=%q status=%d charge=%d blobs=%d", raw, status, charge, len(created))
	}
}

func TestChatMediaWASMStaleCompletionAbortsAndRevokes(t *testing.T) {
	oldCache := chatMediaCache
	chatMediaCache = newChatMediaGrants()
	t.Cleanup(func() { chatMediaCache = oldCache })
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "reader", Bearer: "token"}
	chatBrowser.reset(nil, cfg, nil)
	chatBrowser.mutate(func(model *chatui.Model) {
		model.SelectedID = "room"
		model.Messages = []chatui.Message{{ID: "post", Attachments: []chatui.Attachment{{ID: "artifact", ContentType: "image/png"}}}}
	})
	chatMediaCache.SetWanted([]string{"artifact"})
	epoch, claimed := chatMediaCache.ClaimEpoch("artifact", time.Now())
	if !claimed {
		t.Fatal("preview claim failed")
	}
	grantJSON := mediaWASMFunc(t, func(js.Value, []js.Value) any { return js.ValueOf(map[string]any{"grant": "short-grant"}) })
	grantResponse := js.ValueOf(map[string]any{"status": 200, "json": grantJSON})
	var resolve js.Value
	executor := mediaWASMFunc(t, func(_ js.Value, args []js.Value) any { resolve = args[0]; return nil })
	deferred := js.Global().Get("Promise").New(executor)
	requested := make(chan js.Value, 1)
	fetch := mediaWASMFunc(t, func(_ js.Value, args []js.Value) any {
		if strings.Contains(args[0].String(), "/grant?") {
			return grantResponse
		}
		requested <- args[1].Get("signal")
		return deferred
	})
	created, revoked := []string{}, []string{}
	mediaWASMGlobals(t, fetch, &created, &revoked)
	done := make(chan struct{})
	go func() { fetchChatMedia(cfg, "room", "artifact", epoch, false); close(done) }()
	var signal js.Value
	select {
	case signal = <-requested:
	case <-time.After(3 * time.Second):
		t.Fatal("artifact read did not start")
	}
	clearChatMediaCache()
	if !signal.Get("aborted").Bool() {
		t.Fatal("room change did not abort in-flight fetch")
	}
	response := mediaWASMResponse(t, "image/png", []js.Value{mediaWASMChunk(mediaWASMPNG(t))}, new(int))
	resolve.Invoke(response)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stale fetch did not finish")
	}
	if len(created) != 1 || len(revoked) != 1 || revoked[0] != created[0] || chatBrowser.snapshot().Messages[0].Attachments[0].URL != "" {
		t.Fatalf("stale preview escaped: created=%v revoked=%v model=%+v", created, revoked, chatBrowser.snapshot().Messages[0].Attachments[0])
	}
}

func TestChatMediaWASMSlotsDrainWantedAttachments(t *testing.T) {
	oldCache, oldSlots := chatMediaCache, chatMediaSlots
	chatMediaCache, chatMediaSlots = newChatMediaGrants(), make(chan struct{}, 4)
	t.Cleanup(func() { chatMediaCache, chatMediaSlots = oldCache, oldSlots })
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "reader", Bearer: "token"}
	chatBrowser.reset(nil, cfg, nil)
	attachments := make([]chatui.Attachment, 7)
	ids := make([]string, len(attachments))
	for i := range attachments {
		ids[i] = "artifact-" + string(rune('a'+i))
		attachments[i] = chatui.Attachment{ID: ids[i], ContentType: "image/png"}
	}
	chatBrowser.mutate(func(model *chatui.Model) {
		model.SelectedID = "room"
		model.Messages = []chatui.Message{{ID: "post", Attachments: attachments}}
	})
	chatMediaCache.SetWanted(ids)
	resolveChatMedia(cfg, "room")
	if len(chatMediaSlots) != 0 || len(chatMediaCache.grants) != 0 {
		t.Fatal("projection eagerly fetched media bytes instead of waiting for near-viewport demand")
	}
}

func TestChatMediaWASMAuthenticatedVariantFetch(t *testing.T) {
	oldCache, oldSlots := chatMediaCache, chatMediaSlots
	chatMediaCache, chatMediaSlots = newChatMediaGrants(), make(chan struct{}, 4)
	t.Cleanup(func() { chatMediaCache, chatMediaSlots = oldCache, oldSlots })
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "reader", Bearer: "token"}
	chatBrowser.reset(nil, cfg, nil)
	chatBrowser.mutate(func(model *chatui.Model) { model.SelectedID = "room" })
	chatMediaCache.SetWanted([]string{"artifact"})
	controller := js.Global().Get("AbortController").New()
	var paths []string
	var headers []js.Value
	grantJSON := mediaWASMFunc(t, func(js.Value, []js.Value) any { return js.ValueOf(map[string]any{"grant": "short-grant"}) })
	grantResponse := js.ValueOf(map[string]any{"status": 200, "json": grantJSON})
	getHeader := mediaWASMFunc(t, func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].String() == "Content-Type" {
			return "image/jpeg"
		}
		if len(args) > 0 && args[0].String() == "Content-Length" {
			return "256"
		}
		return js.Null()
	})
	blobFn := mediaWASMFunc(t, func(js.Value, []js.Value) any {
		blob := js.Global().Get("Blob").New(js.Global().Get("Array").New("jpeg-data"), js.ValueOf(map[string]any{"type": "image/jpeg"}))
		return blob
	})
	response := js.ValueOf(map[string]any{"status": 200, "headers": js.ValueOf(map[string]any{"get": getHeader}), "blob": blobFn})
	fetch := mediaWASMFunc(t, func(_ js.Value, args []js.Value) any {
		paths = append(paths, args[0].String())
		headers = append(headers, args[1].Get("headers"))
		if strings.Contains(args[0].String(), "/grant?") {
			return grantResponse
		}
		return response
	})
	created, revoked := []string{}, []string{}
	mediaWASMGlobals(t, fetch, &created, &revoked)
	for _, variant := range []string{"thumbnail", "display", "original"} {
		if raw := fetchChatMediaImageVariant(cfg, "artifact", variant, controller.Get("signal")); raw == "" {
			t.Fatalf("%s variant did not produce an object URL", variant)
		}
	}
	if len(paths) != 4 || !strings.Contains(paths[0], "/grant?") || !strings.Contains(paths[1], "variant=thumbnail") || !strings.Contains(paths[2], "variant=display") || strings.Contains(paths[3], "variant=") {
		t.Fatalf("unexpected grant/variant requests: %v", paths)
	}
	for _, requestHeaders := range headers[1:] {
		if requestHeaders.Get("authorization").String() != "Bearer token" || requestHeaders.Get("X-Chat-Tenant").String() != "tenant" || requestHeaders.Get("X-Chat-Conversation").String() != "room" || requestHeaders.Get("X-Chat-Media-Grant").String() != "short-grant" {
			t.Fatalf("variant request dropped authenticated headers: auth=%q tenant=%q conversation=%q grant=%q", requestHeaders.Get("authorization").String(), requestHeaders.Get("X-Chat-Tenant").String(), requestHeaders.Get("X-Chat-Conversation").String(), requestHeaders.Get("X-Chat-Media-Grant").String())
		}
	}
	if len(created) != 3 {
		t.Fatalf("expected three short-lived rendition Blob URLs, got %d", len(created))
	}
}
