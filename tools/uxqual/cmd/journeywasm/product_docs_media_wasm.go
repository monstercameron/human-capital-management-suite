//go:build js && wasm

package main

import (
	"encoding/json"
	"errors"
	"mime"
	"net/url"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// The document attachment boundary (internal/transport/documentmedia) as
// seen from the page. Every request carries the page's bearer in a header,
// which is why images are read with fetch and shown through blob: URLs, and
// why downloads and exports are fetched and then saved through an anchor
// with a download attribute: nothing ever navigates the page.

const docsMediaRoute = "/v1/documents/media/"

var docsMedia = struct {
	sync.Mutex
	cfg     journeyclient.Config
	port    *productui.DocumentMediaPort
	lists   map[string][]productui.DocumentAttachment
	waiting map[string][]func([]productui.DocumentAttachment, error)
	urls    map[string]string
	slots   chan struct{}
}{lists: map[string][]productui.DocumentAttachment{}, waiting: map[string][]func([]productui.DocumentAttachment, error){}, urls: map[string]string{}, slots: make(chan struct{}, 4)}

// documentMediaPort returns the page's one media port, bound to the
// current credentials. The port value never changes, so the reader's
// memoized text is not rebuilt on every route.
func documentMediaPort(cfg journeyclient.Config) *productui.DocumentMediaPort {
	docsMedia.Lock()
	defer docsMedia.Unlock()
	if docsMedia.cfg.Bearer != cfg.Bearer || docsMedia.cfg.Tenant != cfg.Tenant {
		// Another person or company: nothing cached for the last one stays.
		for _, raw := range docsMedia.urls {
			js.Global().Get("URL").Call("revokeObjectURL", raw)
		}
		docsMedia.lists = map[string][]productui.DocumentAttachment{}
		docsMedia.urls = map[string]string{}
	}
	docsMedia.cfg = cfg
	if docsMedia.port == nil {
		docsMedia.port = &productui.DocumentMediaPort{
			List: docsMediaList, Upload: docsMediaUpload, ObjectURL: docsMediaObjectURL,
			Open: docsMediaOpen, Download: docsMediaDownload, Export: docsMediaExport,
		}
	}
	return docsMedia.port
}

func docsMediaConfig() journeyclient.Config {
	docsMedia.Lock()
	defer docsMedia.Unlock()
	return docsMedia.cfg
}

var errDocsMediaStatus = errors.New("document media: request failed")

// docsMediaFetch issues one authenticated GET and returns the response.
func docsMediaFetch(path string) (js.Value, error) {
	fetch := js.Global().Get("fetch")
	if !fetch.Truthy() {
		return js.Undefined(), errDocsMediaStatus
	}
	cfg := docsMediaConfig()
	headers := js.Global().Get("Object").New()
	headers.Set(journeyclient.AuthorizationHeader, journeyclient.BearerScheme+cfg.Bearer)
	options := js.Global().Get("Object").New()
	options.Set("method", "GET")
	options.Set("headers", headers)
	options.Set("credentials", "same-origin")
	options.Set("cache", "no-store")
	response := awaitChatJS(fetch.Invoke(path, options))
	if !response.Truthy() {
		return js.Undefined(), errDocsMediaStatus
	}
	if status := response.Get("status").Int(); status != 200 {
		return response, docsMediaStatusError(status)
	}
	return response, nil
}

func docsMediaStatusError(status int) error {
	switch status {
	case 413:
		return productui.ErrDocumentMediaTooLarge
	case 415:
		return productui.ErrDocumentMediaUnsupported
	case 403:
		return productui.ErrDocumentMediaForbidden
	}
	return errDocsMediaStatus
}

func docsMediaBytes(response js.Value) ([]byte, error) {
	buffer := awaitChatJS(response.Call("arrayBuffer"))
	if !buffer.Truthy() {
		return nil, errDocsMediaStatus
	}
	array := js.Global().Get("Uint8Array").New(buffer)
	out := make([]byte, array.Length())
	js.CopyBytesToGo(out, array)
	return out, nil
}

type docsMediaWire struct {
	ID         string `json:"id"`
	DocumentID string `json:"document_id"`
	Filename   string `json:"filename"`
	MediaType  string `json:"media_type"`
	Size       int64  `json:"size"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Pages      int    `json:"pages"`
}

func (w docsMediaWire) attachment() productui.DocumentAttachment {
	return productui.DocumentAttachment{ID: w.ID, DocumentID: w.DocumentID, Filename: w.Filename, MediaType: w.MediaType, Size: w.Size, Width: w.Width, Height: w.Height, Pages: w.Pages}
}

// docsMediaList answers from the per-document cache, and coalesces the
// requests of every card and the Attachments section into one fetch.
func docsMediaList(documentID string, done func([]productui.DocumentAttachment, error)) {
	docsMedia.Lock()
	if cached, ok := docsMedia.lists[documentID]; ok {
		docsMedia.Unlock()
		done(cached, nil)
		return
	}
	docsMedia.waiting[documentID] = append(docsMedia.waiting[documentID], done)
	first := len(docsMedia.waiting[documentID]) == 1
	docsMedia.Unlock()
	if !first {
		return
	}
	go func() {
		var list []productui.DocumentAttachment
		response, err := docsMediaFetch(docsMediaRoute + url.PathEscape(documentID))
		if err == nil {
			var body []byte
			body, err = docsMediaBytes(response)
			var wire struct {
				Attachments []docsMediaWire `json:"attachments"`
			}
			if err == nil {
				err = json.Unmarshal(body, &wire)
			}
			for _, item := range wire.Attachments {
				list = append(list, item.attachment())
			}
		}
		docsMedia.Lock()
		waiting := docsMedia.waiting[documentID]
		delete(docsMedia.waiting, documentID)
		if err == nil {
			docsMedia.lists[documentID] = list
		}
		docsMedia.Unlock()
		ui.PostAsync(func() {
			for _, fn := range waiting {
				fn(list, err)
			}
		})
	}()
}

// docsMediaUpload posts one file with XMLHttpRequest, whose upload events
// report progress (fetch has none).
func docsMediaUpload(documentID, filename string, content []byte, progress func(sent, total int64), done func(productui.DocumentAttachment, error)) {
	cfg := docsMediaConfig()
	array := js.Global().Get("Uint8Array").New(len(content))
	js.CopyBytesToJS(array, content)
	blob := js.Global().Get("Blob").New([]any{array})
	form := js.Global().Get("FormData").New()
	form.Call("append", "file", blob, filename)
	xhr := js.Global().Get("XMLHttpRequest").New()
	xhr.Call("open", "POST", docsMediaRoute+url.PathEscape(documentID)+"/upload")
	xhr.Call("setRequestHeader", journeyclient.AuthorizationHeader, journeyclient.BearerScheme+cfg.Bearer)
	var onProgress, onLoad, onFail js.Func
	release := func() {
		onProgress.Release()
		onLoad.Release()
		onFail.Release()
	}
	onProgress = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].Get("lengthComputable").Truthy() && progress != nil {
			sent, total := int64(args[0].Get("loaded").Float()), int64(args[0].Get("total").Float())
			ui.PostAsync(func() { progress(sent, total) })
		}
		return nil
	})
	onLoad = js.FuncOf(func(js.Value, []js.Value) any {
		defer release()
		status := xhr.Get("status").Int()
		body := xhr.Get("responseText").String()
		if status != 201 {
			err := docsMediaStatusError(status)
			ui.PostAsync(func() { done(productui.DocumentAttachment{}, err) })
			return nil
		}
		var wire docsMediaWire
		if err := json.Unmarshal([]byte(body), &wire); err != nil || wire.ID == "" {
			ui.PostAsync(func() { done(productui.DocumentAttachment{}, errDocsMediaStatus) })
			return nil
		}
		docsMedia.Lock()
		delete(docsMedia.lists, documentID)
		docsMedia.Unlock()
		ui.PostAsync(func() { done(wire.attachment(), nil) })
		return nil
	})
	onFail = js.FuncOf(func(js.Value, []js.Value) any {
		defer release()
		ui.PostAsync(func() { done(productui.DocumentAttachment{}, errDocsMediaStatus) })
		return nil
	})
	xhr.Get("upload").Call("addEventListener", "progress", onProgress)
	xhr.Call("addEventListener", "load", onLoad)
	xhr.Call("addEventListener", "error", onFail)
	xhr.Call("addEventListener", "abort", onFail)
	xhr.Call("send", form)
}

// docsMediaBlobURL fetches an attachment and caches a blob: URL for it.
func docsMediaBlobURL(documentID, attachmentID string) (string, error) {
	key := documentID + "/" + attachmentID
	docsMedia.Lock()
	if raw, ok := docsMedia.urls[key]; ok {
		docsMedia.Unlock()
		return raw, nil
	}
	docsMedia.Unlock()
	select {
	case docsMedia.slots <- struct{}{}:
		defer func() { <-docsMedia.slots }()
	case <-time.After(30 * time.Second):
		return "", errDocsMediaStatus
	}
	response, err := docsMediaFetch(docsMediaRoute + url.PathEscape(documentID) + "/" + url.PathEscape(attachmentID))
	if err != nil {
		return "", err
	}
	blob := awaitChatJS(response.Call("blob"))
	if !blob.Truthy() {
		return "", errDocsMediaStatus
	}
	raw := js.Global().Get("URL").Call("createObjectURL", blob).String()
	docsMedia.Lock()
	if existing, ok := docsMedia.urls[key]; ok {
		docsMedia.Unlock()
		js.Global().Get("URL").Call("revokeObjectURL", raw)
		return existing, nil
	}
	docsMedia.urls[key] = raw
	docsMedia.Unlock()
	return raw, nil
}

func docsMediaObjectURL(documentID, attachmentID string, done func(string, error)) {
	go func() {
		raw, err := docsMediaBlobURL(documentID, attachmentID)
		ui.PostAsync(func() { done(raw, err) })
	}()
}

// docsMediaOpen shows the file in a new tab from its blob: URL. The tab must
// open synchronously, inside the click that asked for it, or every browser's
// popup blocker treats the later window.open (after the async fetch that
// built the blob URL) as unrequested and silently drops it — and doing that
// with "noopener" makes the drop undetectable, since window.open then
// answers null whether or not a tab actually opened (DOCS-04). So: open a
// blank, same-origin-capable tab right here, retarget it once the blob URL
// is ready, and fall back to a same-tab download if even the blank tab was
// blocked.
func docsMediaOpen(documentID, attachmentID string, done func(error)) {
	handle := js.Global().Call("open", "", "_blank")
	if !handle.Truthy() {
		docsMediaDownload(documentID, attachmentID, done)
		return
	}
	go func() {
		raw, err := docsMediaBlobURL(documentID, attachmentID)
		ui.PostAsync(func() {
			if err != nil {
				handle.Call("close")
				done(err)
				return
			}
			handle.Get("location").Set("href", raw)
			done(nil)
		})
	}()
}

// docsMediaSave fetches path and saves it under the server's file name.
func docsMediaSave(path, fallbackName string, done func(error)) {
	go func() {
		response, err := docsMediaFetch(path)
		var content []byte
		name, kind := fallbackName, ""
		if err == nil {
			content, err = docsMediaBytes(response)
			if value := response.Get("headers").Call("get", "Content-Disposition"); value.Truthy() {
				if _, params, parseErr := mime.ParseMediaType(value.String()); parseErr == nil && params["filename"] != "" {
					name = params["filename"]
				}
			}
			if value := response.Get("headers").Call("get", "Content-Type"); value.Truthy() {
				kind = value.String()
			}
		}
		ui.PostAsync(func() {
			if err == nil {
				ui.Download(content, name, kind)
			}
			done(err)
		})
	}()
}

func docsMediaDownload(documentID, attachmentID string, done func(error)) {
	docsMediaSave(docsMediaRoute+url.PathEscape(documentID)+"/"+url.PathEscape(attachmentID)+"?download=1", "attachment", done)
}

func docsMediaExport(documentID, format string, done func(error)) {
	if format != "txt" && format != "md" && format != "pdf" {
		done(errDocsMediaStatus)
		return
	}
	docsMediaSave(docsMediaRoute+url.PathEscape(documentID)+"/export?format="+format, "document."+format, done)
}
