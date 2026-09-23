//go:build js && wasm

package main

import (
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// This file turns a media reference into something an <img> can show, without
// the timeline ever waiting for it.
//
// The protected read takes its grant from a header
// (internal/transport/chatmedia/handler.go:302-307), and an <img src> cannot
// send one, so the bytes are fetched with the header and handed to
// createObjectURL. The blob URL is cached with the grant: it is what the
// renderer uses, and it lives exactly as long as this page does.

// chatMediaRoute is the media boundary the edge mounts at the origin root
// (internal/application/chat_media.go, OverlayChatMedia). The product shell's
// connect-src names this prefix, so the fetch below is the one HTTP call the
// page may make besides its assets.
const chatMediaRoute = "/v1/chat/media/"

var chatMediaCache = newChatMediaGrants()
var chatMediaSlots = make(chan struct{}, 4)
var chatDownloadSlot = make(chan struct{}, 1)
var chatDownloadURLs = struct {
	sync.Mutex
	urls  map[string]bool
	order []string
}{urls: map[string]bool{}}
var chatMediaFetches = struct {
	sync.Mutex
	active map[string]chatMediaFetchControl
}{active: map[string]chatMediaFetchControl{}}
var chatMediaImageBridge js.Func
var chatMediaDownloadBridge js.Func
var chatMediaBridgeSequence uint64

type chatMediaFetchControl struct {
	epoch      uint64
	controller js.Value
}

func abortChatMediaFetches() {
	chatMediaFetches.Lock()
	active := chatMediaFetches.active
	chatMediaFetches.active = map[string]chatMediaFetchControl{}
	chatMediaFetches.Unlock()
	for _, fetch := range active {
		fetch.controller.Call("abort")
	}
}

func registerChatMediaFetch(id string, epoch uint64, controller js.Value) {
	chatMediaFetches.Lock()
	chatMediaFetches.active[id] = chatMediaFetchControl{epoch: epoch, controller: controller}
	chatMediaFetches.Unlock()
}

func unregisterChatMediaFetch(id string, epoch uint64) {
	chatMediaFetches.Lock()
	if current, ok := chatMediaFetches.active[id]; ok && current.epoch == epoch {
		delete(chatMediaFetches.active, id)
	}
	chatMediaFetches.Unlock()
}

func init() {
	chatMediaCache.SetRevoker(func(raw string) {
		revokeChatMediaObjectURL(raw)
		chatBrowser.clearChatMediaURL(raw)
	})
}

func revokeChatMediaObjectURL(raw string) {
	if raw == "" {
		return
	}
	if viewer := js.Global().Get("document").Call("querySelector", ".chat-image-viewer img"); viewer.Truthy() && viewer.Get("src").String() == raw {
		time.AfterFunc(250*time.Millisecond, func() { revokeChatMediaObjectURL(raw) })
		return
	}
	objectURL := js.Global().Get("URL")
	if objectURL.Truthy() {
		objectURL.Call("revokeObjectURL", raw)
	}
}

func clearChatMediaCache() {
	abortChatMediaFetches()
	chatDownloadURLs.Lock()
	urls := chatDownloadURLs.urls
	chatDownloadURLs.urls = map[string]bool{}
	chatDownloadURLs.order = nil
	chatDownloadURLs.Unlock()
	for raw := range urls {
		revokeChatMediaObjectURL(raw)
	}
	chatMediaCache.Clear()
	chatBrowser.clearChatMediaURLs()
}

func syncVisibleChatMedia(cfg journeyclient.Config, messageIDs []string) {
	model := chatBrowser.snapshot()
	byID := make(map[string]chatui.Message, len(model.Messages)+len(model.ThreadMessages))
	for _, message := range model.Messages {
		byID[message.ID] = message
	}
	for _, message := range model.ThreadMessages {
		byID[message.ID] = message
	}
	if model.ThreadParent != nil {
		byID[model.ThreadParent.ID] = *model.ThreadParent
	}
	artifacts := make([]string, 0, chatMediaCacheMaxItems)
	for _, id := range messageIDs {
		if message, ok := byID[id]; ok {
			for _, attachment := range message.Attachments {
				artifacts = append(artifacts, attachment.ID)
			}
		}
	}
	chatMediaCache.SetWanted(artifacts)
	// Image bytes are requested by chatui's near-viewport observer. Keeping
	// only artifact IDs in this wanted set bounds grant caching without
	// downloading original files for every mounted timeline row.
}

func installChatMediaImageBridge(cfg journeyclient.Config) {
	if chatMediaImageBridge.Value.Truthy() {
		chatMediaImageBridge.Release()
	}
	if chatMediaDownloadBridge.Value.Truthy() {
		chatMediaDownloadBridge.Release()
	}
	chatMediaImageBridge = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) < 3 {
			return js.Global().Get("Promise").Call("reject", "invalid image request")
		}
		id, variant, signal := args[0].String(), args[1].String(), args[2]
		var executor js.Func
		executor = js.FuncOf(func(_ js.Value, promiseArgs []js.Value) any {
			resolve, reject := promiseArgs[0], promiseArgs[1]
			executor.Release()
			go func() {
				raw := fetchChatMediaImageVariant(cfg, id, variant, signal)
				if raw == "" {
					reject.Invoke("chat media image request failed")
					return
				}
				resolve.Invoke(raw)
			}()
			return nil
		})
		return js.Global().Get("Promise").New(executor)
	})
	chatMediaDownloadBridge = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) >= 2 {
			go downloadChatAttachment(cfg, args[0].String(), args[1].String())
		}
		return nil
	})
	js.Global().Set("hcmChatMediaFetch", chatMediaImageBridge)
	js.Global().Set("hcmChatMediaDownload", chatMediaDownloadBridge)
}

func fetchChatMediaImageVariant(cfg journeyclient.Config, id, variant string, signal js.Value) string {
	if id == "" || (variant != "thumbnail" && variant != "display" && variant != "original") || signal.Get("aborted").Truthy() {
		return ""
	}
	model := chatBrowser.snapshot()
	conversationID := model.SelectedID
	active := chatBrowser.config(cfg)
	if conversationID == "" || !chatMediaCache.Wanted(id) {
		return ""
	}
	select {
	case chatMediaSlots <- struct{}{}:
		defer func() { <-chatMediaSlots }()
	case <-time.After(30 * time.Second):
		return ""
	}
	for attempt := 0; attempt < 2; attempt++ {
		if signal.Get("aborted").Truthy() || conversationID != chatBrowser.selectedID() || active.Subject != chatBrowser.config(cfg).Subject || active.Tenant != chatBrowser.config(cfg).Tenant {
			return ""
		}
		grant, ok := chatMediaCache.Get(id, time.Now())
		if !ok {
			epoch, claimed := chatMediaCache.ClaimEpoch(id, time.Now())
			if !claimed {
				// Another visible instance may be minting the shared artifact grant.
				for wait := 0; wait < 80 && !signal.Get("aborted").Truthy(); wait++ {
					time.Sleep(25 * time.Millisecond)
					if grant, ok = chatMediaCache.Get(id, time.Now()); ok {
						break
					}
				}
				if !ok {
					return ""
				}
			} else {
				token, expires, minted := mintChatMediaGrant(active, conversationID, id, signal)
				if !minted || signal.Get("aborted").Truthy() || conversationID != chatBrowser.selectedID() || !chatMediaCache.PutIfEpoch(id, chatMediaGrant{Token: token, ExpiresAt: expires}, epoch) {
					chatMediaCache.ReleaseIfEpoch(id, epoch)
					return ""
				}
				grant, ok = chatMediaCache.Get(id, time.Now())
				if !ok {
					return ""
				}
			}
		}
		variantName := variant
		if variantName == "original" {
			variantName = ""
		}
		response, err := chatMediaFetchVariant(chatMediaRoute+url.PathEscape(id), active, conversationID, grant.Token, variantName, signal)
		if err != nil || !response.Truthy() {
			return ""
		}
		status := response.Get("status").Int()
		if status == 403 && attempt == 0 {
			chatMediaCache.Invalidate(id)
			continue
		}
		if status != 200 {
			return ""
		}
		contentType := response.Get("headers").Call("get", "Content-Type")
		if !contentType.Truthy() || !strings.HasPrefix(strings.ToLower(contentType.String()), "image/") {
			return ""
		}
		maxBytes := int64(chatMediaPreviewMaxBytes)
		if variant == "original" {
			maxBytes = 64 << 20
		}
		if length := response.Get("headers").Call("get", "Content-Length"); length.Truthy() {
			if n, err := strconv.ParseInt(length.String(), 10, 64); err == nil && n > maxBytes {
				return ""
			}
		}
		blob := awaitChatJS(response.Call("blob"))
		if !blob.Truthy() || int64(blob.Get("size").Int()) > maxBytes || signal.Get("aborted").Truthy() {
			return ""
		}
		if conversationID != chatBrowser.selectedID() || active.Subject != chatBrowser.config(cfg).Subject || active.Tenant != chatBrowser.config(cfg).Tenant {
			return ""
		}
		return js.Global().Get("URL").Call("createObjectURL", blob).String()
	}
	return ""
}

// resolveChatMedia fills in the URLs for everything on screen that has none.
//
// It is called after a render, never before one: an attachment with no URL
// renders as a chip with a loading state, and the URL swaps in when it arrives.
// Nothing here blocks the timeline.
func resolveChatMedia(cfg journeyclient.Config, conversationID string) {
	// Kept as a compatibility hook for the initial projection and stream
	// callbacks. Image bytes are fetched only by chatui's near-viewport loader
	// through hcmChatMediaFetch; this hook must not eagerly read originals.
	_ = cfg
	_ = conversationID
}

// fetchChatMedia mints a grant, reads the bytes with it, and publishes an
// object URL.
//
// retry is false on the second attempt: a 403 means the grant this client held
// was refused -- expired early, or revoked -- so it is dropped and minted once
// more rather than trusted until its stated expiry.
func fetchChatMedia(cfg journeyclient.Config, conversationID, id string, epoch uint64, retry bool) {
	controller := js.Global().Get("AbortController").New()
	registerChatMediaFetch(id, epoch, controller)
	deadline := time.AfterFunc(20*time.Second, func() { controller.Call("abort") })
	defer func() { deadline.Stop(); unregisterChatMediaFetch(id, epoch) }()
	signal := controller.Get("signal")
	grant, expiresAt, ok := mintChatMediaGrant(cfg, conversationID, id, signal)
	if !ok {
		chatMediaCache.FailIfEpoch(id, epoch, time.Now().Add(20*time.Second))
		return
	}
	url, status, encoded, charge := readChatMediaBytes(cfg, conversationID, id, grant, signal)
	if status == 403 && retry {
		if chatMediaCache.InvalidateIfEpoch(id, epoch) {
			if retryEpoch, claimed := chatMediaCache.ClaimEpoch(id, time.Now()); claimed {
				fetchChatMedia(cfg, conversationID, id, retryEpoch, false)
			}
		}
		return
	}
	if url == "" {
		cooldown := 20 * time.Second
		if status == 413 {
			cooldown = time.Hour
			chatBrowser.markChatMediaPreviewUnavailable(id)
		}
		chatMediaCache.FailIfEpoch(id, epoch, time.Now().Add(cooldown))
		return
	}
	if !chatMediaCache.PutIfEpoch(id, chatMediaGrant{Token: grant, ExpiresAt: expiresAt, URL: url, Bytes: charge, EncodedBytes: encoded}, epoch) {
		revokeChatMediaObjectURL(url)
		chatMediaCache.FailIfEpoch(id, epoch, time.Now().Add(time.Minute))
		return
	}
	if chatMediaCache.ApplyIfEpoch(id, epoch, func() bool { return chatBrowser.applyChatMediaURL(id, url) }) {
		chatStreamRender.Schedule()
	}
}

// mintChatMediaGrant asks the media boundary for a download authorization.
func mintChatMediaGrant(cfg journeyclient.Config, conversationID, id string, signal js.Value) (grant string, expiresAt time.Time, ok bool) {
	response, err := chatMediaFetch(chatMediaRoute+id+"/grant", cfg, conversationID, "", signal)
	if err != nil || !response.Truthy() || response.Get("status").Int() != 200 {
		return "", time.Time{}, false
	}
	body := awaitChatJS(response.Call("json"))
	if !body.Truthy() {
		return "", time.Time{}, false
	}
	grant = body.Get("grant").String()
	if grant == "" {
		return "", time.Time{}, false
	}
	if stated := body.Get("grant_expires_at").String(); stated != "" {
		if parsed, parseErr := time.Parse(time.RFC3339, stated); parseErr == nil {
			expiresAt = parsed
		}
	}
	return grant, expiresAt, true
}

// readChatMediaBytes reads the artifact with its grant and returns an object
// URL the renderer can use.
func readChatMediaBytes(cfg journeyclient.Config, conversationID, id, grant string, signal js.Value) (url string, status int, encoded, charge int64) {
	response, err := chatMediaFetch(chatMediaRoute+id, cfg, conversationID, grant, signal)
	if err != nil || !response.Truthy() {
		return "", 0, 0, 0
	}
	status = response.Get("status").Int()
	if status != 200 && status != 206 {
		return "", status, 0, 0
	}
	if length := response.Get("headers").Call("get", "Content-Length"); length.Truthy() {
		if n, parseErr := strconv.ParseInt(length.String(), 10, 64); parseErr == nil && n > chatMediaPreviewMaxBytes {
			response.Get("body").Call("cancel")
			return "", 413, 0, 0
		}
	}
	reader := response.Get("body")
	if !reader.Truthy() {
		return "", status, 0, 0
	}
	reader = reader.Call("getReader")
	chunks := js.Global().Get("Array").New()
	data := make([]byte, 0, 64*1024)
	for {
		part := awaitChatJS(reader.Call("read"))
		if !part.Truthy() {
			return "", status, 0, 0
		}
		if part.Get("done").Bool() {
			break
		}
		chunk := part.Get("value")
		encoded += int64(chunk.Get("byteLength").Int())
		if encoded > chatMediaPreviewMaxBytes {
			reader.Call("cancel")
			return "", 413, 0, 0
		}
		chunks.Call("push", chunk)
		partBytes := make([]byte, chunk.Get("byteLength").Int())
		js.CopyBytesToGo(partBytes, chunk)
		data = append(data, partBytes...)
	}
	options := js.Global().Get("Object").New()
	contentType := response.Get("headers").Call("get", "Content-Type")
	if !contentType.Truthy() || !strings.HasPrefix(strings.ToLower(contentType.String()), "image/") {
		return "", 413, 0, 0
	}
	decodedBytes, valid := chatPreviewDimensions(data, contentType.String())
	if !valid {
		return "", 413, 0, 0
	}
	charge = encoded + decodedBytes
	options.Set("type", contentType.String())
	blob := js.Global().Get("Blob").New(chunks, options)
	objectURL := js.Global().Get("URL")
	if !objectURL.Truthy() {
		return "", status, 0, 0
	}
	return objectURL.Call("createObjectURL", blob).String(), status, encoded, charge
}

// chatMediaFetch issues one request to the media boundary.
//
// The boundary reads the conversation from the query (the trusted principal
// reader in internal/application/chat_media.go), and the bearer from the
// Authorization header like every other edge request. A rejected fetch (the
// page's CSP refusing the call, or the network) comes back as an error rather
// than an undefined response, so no caller reads a status off nothing.
func chatMediaFetch(path string, cfg journeyclient.Config, conversationID, grant string, signal js.Value) (js.Value, error) {
	return chatMediaFetchVariant(path, cfg, conversationID, grant, "", signal)
}

func chatMediaFetchVariant(path string, cfg journeyclient.Config, conversationID, grant, variant string, signal js.Value) (js.Value, error) {
	fetch := js.Global().Get("fetch")
	if !fetch.Truthy() {
		return js.Undefined(), errChatMediaUnavailable
	}
	path += "?conversation_id=" + url.QueryEscape(conversationID)
	if variant != "" {
		path += "&variant=" + url.QueryEscape(variant)
	}
	headers := js.Global().Get("Object").New()
	headers.Set(journeyclient.AuthorizationHeader, journeyclient.BearerScheme+cfg.Bearer)
	headers.Set("X-Chat-Tenant", cfg.Tenant)
	headers.Set("X-Chat-Conversation", conversationID)
	if grant != "" {
		// The boundary takes the grant from a header and nowhere else, which is
		// why the bytes cannot simply be an <img src>.
		headers.Set("X-Chat-Media-Grant", grant)
	}
	options := js.Global().Get("Object").New()
	options.Set("method", "GET")
	options.Set("headers", headers)
	options.Set("credentials", "same-origin")
	options.Set("cache", "no-store")
	options.Set("signal", signal)
	response := awaitChatJS(fetch.Invoke(path, options))
	if !response.Truthy() {
		return js.Undefined(), errChatMediaRejected
	}
	return response, nil
}

// errChatMediaRejected is returned when the fetch promise rejected.
var errChatMediaRejected = chatMediaError("chat media: the request was refused before it was answered")

// errChatMediaUnavailable is returned when the page has no fetch at all.
var errChatMediaUnavailable = chatMediaError("chat media: this page cannot fetch")

type chatMediaError string

func (e chatMediaError) Error() string { return string(e) }

// awaitChatJS resolves a promise without blocking the wasm event loop's only
// goroutine. A blocking wait on a JS promise from the goroutine the runtime
// dispatches callbacks on deadlocks the page.
func awaitChatJS(promise js.Value) js.Value {
	if !promise.Truthy() {
		return js.Undefined()
	}
	if promise.Type() != js.TypeObject || !promise.Get("then").Truthy() {
		return promise
	}
	done := make(chan js.Value, 1)
	onResolved := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			done <- args[0]
			return nil
		}
		done <- js.Undefined()
		return nil
	})
	defer onResolved.Release()
	onRejected := js.FuncOf(func(js.Value, []js.Value) any { done <- js.Undefined(); return nil })
	defer onRejected.Release()
	promise.Call("then", onResolved, onRejected)
	return <-done
}

// applyChatMediaURL places a resolved URL on every message that references the
// artifact, in the timeline and in the open thread.
func (s *chatState) applyChatMediaURL(id, url string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := applyChatMediaURL(s.model.Messages, id, url)
	if applyChatMediaURL(s.model.ThreadMessages, id, url) {
		changed = true
	}
	if s.model.ThreadParent != nil {
		parent := []chatui.Message{*s.model.ThreadParent}
		if applyChatMediaURL(parent, id, url) {
			*s.model.ThreadParent = parent[0]
			changed = true
		}
	}
	return changed
}

func (s *chatState) clearChatMediaURLs() {
	s.mu.Lock()
	for i := range s.model.Messages {
		for j := range s.model.Messages[i].Attachments {
			s.model.Messages[i].Attachments[j].URL = ""
		}
	}
	for i := range s.model.ThreadMessages {
		for j := range s.model.ThreadMessages[i].Attachments {
			s.model.ThreadMessages[i].Attachments[j].URL = ""
		}
	}
	if s.model.ThreadParent != nil {
		for j := range s.model.ThreadParent.Attachments {
			s.model.ThreadParent.Attachments[j].URL = ""
		}
	}
	s.mu.Unlock()
}

func (s *chatState) clearChatMediaURL(raw string) {
	s.mu.Lock()
	for _, messages := range [][]chatui.Message{s.model.Messages, s.model.ThreadMessages} {
		for i := range messages {
			for j := range messages[i].Attachments {
				if messages[i].Attachments[j].URL == raw {
					messages[i].Attachments[j].URL = ""
				}
			}
		}
	}
	if s.model.ThreadParent != nil {
		for j := range s.model.ThreadParent.Attachments {
			if s.model.ThreadParent.Attachments[j].URL == raw {
				s.model.ThreadParent.Attachments[j].URL = ""
			}
		}
	}
	s.mu.Unlock()
}

func (s *chatState) chatMediaPreviewUnavailable(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.model.ThreadParent != nil {
		for _, attachment := range s.model.ThreadParent.Attachments {
			if attachment.ID == id && attachment.PreviewUnavailable {
				return true
			}
		}
	}
	for _, messages := range [][]chatui.Message{s.model.Messages, s.model.ThreadMessages} {
		for _, message := range messages {
			for _, attachment := range message.Attachments {
				if attachment.ID == id && attachment.PreviewUnavailable {
					return true
				}
			}
		}
	}
	return false
}

func (s *chatState) markChatMediaPreviewUnavailable(id string) {
	s.mu.Lock()
	changed := false
	if s.model.ThreadParent != nil {
		for i := range s.model.ThreadParent.Attachments {
			if s.model.ThreadParent.Attachments[i].ID == id && !s.model.ThreadParent.Attachments[i].PreviewUnavailable {
				s.model.ThreadParent.Attachments[i].PreviewUnavailable = true
				changed = true
			}
		}
	}
	for _, messages := range [][]chatui.Message{s.model.Messages, s.model.ThreadMessages} {
		for i := range messages {
			for j := range messages[i].Attachments {
				if messages[i].Attachments[j].ID == id && !messages[i].Attachments[j].PreviewUnavailable {
					messages[i].Attachments[j].PreviewUnavailable = true
					changed = true
				}
			}
		}
	}
	s.mu.Unlock()
	if changed {
		chatStreamRender.Schedule()
	}
}

func downloadChatAttachment(cfg journeyclient.Config, postID, id string) {
	select {
	case chatDownloadSlot <- struct{}{}:
		defer func() { <-chatDownloadSlot }()
	default:
		return
	}
	model := chatBrowser.snapshot()
	name := ""
	if model.ThreadParent != nil && model.ThreadParent.ID == postID {
		for _, attachment := range model.ThreadParent.Attachments {
			if attachment.ID == id {
				name = attachment.Name
			}
		}
	}
	for _, messages := range [][]chatui.Message{model.Messages, model.ThreadMessages} {
		for _, message := range messages {
			if message.ID != postID {
				continue
			}
			for _, attachment := range message.Attachments {
				if attachment.ID == id {
					name = attachment.Name
				}
			}
		}
	}
	if name == "" || model.SelectedID == "" {
		return
	}
	active := chatBrowser.config(cfg)
	controller := js.Global().Get("AbortController").New()
	registerChatMediaFetch("download:"+id, 0, controller)
	deadline := time.AfterFunc(30*time.Second, func() { controller.Call("abort") })
	defer func() { deadline.Stop(); unregisterChatMediaFetch("download:"+id, 0) }()
	signal := controller.Get("signal")
	grant, _, ok := mintChatMediaGrant(active, model.SelectedID, id, signal)
	if !ok {
		chatActionFailed("download attachment", errChatMediaRejected)
		return
	}
	response, err := chatMediaFetch(chatMediaRoute+id, active, model.SelectedID, grant, signal)
	if err != nil || !response.Truthy() || response.Get("status").Int() != 200 {
		chatActionFailed("download attachment", errChatMediaRejected)
		return
	}
	if length := response.Get("headers").Call("get", "Content-Length"); length.Truthy() {
		if n, parseErr := strconv.ParseInt(length.String(), 10, 64); parseErr == nil && n > 64<<20 {
			chatActionFailed("download attachment", errChatMediaRejected)
			return
		}
	}
	reader := response.Get("body")
	if !reader.Truthy() {
		chatActionFailed("download attachment", errChatMediaRejected)
		return
	}
	reader = reader.Call("getReader")
	chunks := js.Global().Get("Array").New()
	var size int64
	for {
		part := awaitChatJS(reader.Call("read"))
		if !part.Truthy() {
			chatActionFailed("download attachment", errChatMediaRejected)
			return
		}
		if part.Get("done").Bool() {
			break
		}
		chunk := part.Get("value")
		size += int64(chunk.Get("byteLength").Int())
		if size > 64<<20 {
			reader.Call("cancel")
			chatActionFailed("download attachment", errChatMediaRejected)
			return
		}
		chunks.Call("push", chunk)
	}
	if active.Tenant != chatBrowser.config(cfg).Tenant || active.Subject != chatBrowser.config(cfg).Subject || model.SelectedID != chatBrowser.selectedID() {
		return
	}
	blob := js.Global().Get("Blob").New(chunks)
	raw := js.Global().Get("URL").Call("createObjectURL", blob).String()
	doc := js.Global().Get("document")
	anchor := doc.Call("createElement", "a")
	anchor.Set("href", raw)
	anchor.Set("download", strings.NewReplacer("/", "_", "\\", "_").Replace(name))
	doc.Get("body").Call("appendChild", anchor)
	anchor.Call("click")
	anchor.Call("remove")
	chatDownloadURLs.Lock()
	chatDownloadURLs.urls[raw] = true
	chatDownloadURLs.order = append(chatDownloadURLs.order, raw)
	var old string
	if len(chatDownloadURLs.order) > 2 {
		old = chatDownloadURLs.order[0]
		chatDownloadURLs.order = chatDownloadURLs.order[1:]
		delete(chatDownloadURLs.urls, old)
	}
	chatDownloadURLs.Unlock()
	if old != "" {
		revokeChatMediaObjectURL(old)
	}
	time.AfterFunc(30*time.Second, func() {
		chatDownloadURLs.Lock()
		if chatDownloadURLs.urls[raw] {
			delete(chatDownloadURLs.urls, raw)
			chatDownloadURLs.Unlock()
			revokeChatMediaObjectURL(raw)
			return
		}
		chatDownloadURLs.Unlock()
	})
}
