//go:build js && wasm

package chatui

import (
	"strconv"
	"strings"
	"syscall/js"
	"time"
)

const (
	giphyAPIBase     = "https://api.giphy.com/v1/gifs"
	giphyMaxOffset   = 499
	giphyDefaultWait = 250
)

// GiphyPicker calls GIPHY directly from the browser. Results remain transient
// and each input change invalidates the previous request before debouncing.
type GiphyPicker struct {
	config    GiphyPickerConfig
	callbacks GiphyPickerCallbacks
	sequence  uint64
	offset    int
	query     string
	ids       []string
	trending  bool
	hasMore   bool
	loading   bool
	timer     *time.Timer
	abort     js.Value
	closed    bool
}

// NewGiphyPicker returns a picker. When no API key is configured, it reports
// the unavailable state and never makes a request.
func NewGiphyPicker(config GiphyPickerConfig, callbacks GiphyPickerCallbacks) *GiphyPicker {
	picker := &GiphyPicker{config: config, callbacks: callbacks, abort: js.Undefined()}
	if !GiphyConfigured(config) && callbacks.OnUnavailable != nil {
		callbacks.OnUnavailable()
	}
	return picker
}

// Search debounces a search query and starts a fresh result sequence.
func (p *GiphyPicker) Search(query string) {
	query = LimitGiphyQuery(strings.TrimSpace(query))
	if query == "" {
		p.Trending()
		return
	}
	p.begin(query, false)
}

// Trending requests the GIPHY trending endpoint.
func (p *GiphyPicker) Trending() { p.begin("", true) }

// Resolve requests the metadata for up to one page of validated GIF IDs.
// The caller matches results by ID and retains them only as transient view data.
func (p *GiphyPicker) Resolve(ids []string) {
	p.Cancel()
	if p.closed || !GiphyConfigured(p.config) {
		return
	}
	p.ids = UniqueGiphyIDs(ids)
	p.query, p.trending, p.offset, p.hasMore = "", false, 0, false
	p.loading = false
	if len(p.ids) == 0 {
		return
	}
	p.request(p.sequence, "", false, 0, false)
}

// LoadMore requests the next bounded 20-result page for the current view.
func (p *GiphyPicker) LoadMore() {
	if p.closed || p.loading || !p.hasMore || !GiphyConfigured(p.config) {
		return
	}
	nextOffset := p.offset + GiphyPageSize
	if nextOffset > giphyMaxOffset {
		p.hasMore = false
		return
	}
	p.offset = nextOffset
	p.request(p.sequence, p.query, p.trending, p.offset, true)
}

// Select validates an API result again before returning it to the view.
func (p *GiphyPicker) Select(result GiphyResult) bool {
	canonical, id, ok := CanonicalGiphyURL(result.URL)
	if !ok || !ValidGiphyID(result.ID) || id != result.ID || result.Rating != GiphyRating || !validGiphyMediaURL(result.PreviewURL) || !validGiphyMediaURL(result.ThumbnailURL) {
		return false
	}
	result.URL = canonical
	if p.callbacks.OnSelect != nil {
		p.callbacks.OnSelect(result)
	}
	return true
}

// Cancel aborts the current request and clears the debounce timer.
func (p *GiphyPicker) Cancel() {
	p.sequence++
	wasLoading := p.loading
	p.loading = false
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	if p.abort.Truthy() {
		p.abort.Call("abort")
		p.abort = js.Undefined()
	}
	if wasLoading && p.callbacks.OnLoading != nil {
		p.callbacks.OnLoading(false)
	}
}

// Close invalidates pending work and makes the picker inert.
func (p *GiphyPicker) Close() {
	p.Cancel()
	p.closed = true
}

func (p *GiphyPicker) begin(query string, trending bool) {
	p.Cancel()
	if p.closed || !GiphyConfigured(p.config) {
		return
	}
	p.query, p.trending, p.offset, p.hasMore = query, trending, 0, false
	p.ids = nil
	p.loading = false
	seq := p.sequence
	wait := p.config.DebounceMillis
	if wait <= 0 {
		wait = giphyDefaultWait
	}
	if trending || query == "" {
		p.request(seq, query, trending, 0, false)
		return
	}
	p.timer = time.AfterFunc(time.Duration(wait)*time.Millisecond, func() {
		if !p.closed && p.sequence == seq {
			p.request(seq, query, false, 0, false)
		}
	})
}

func (p *GiphyPicker) request(seq uint64, query string, trending bool, offset int, appendResults bool) {
	if p.closed || p.sequence != seq {
		return
	}
	controller := js.Global().Get("AbortController")
	if !controller.Truthy() {
		p.fail(seq, "GIPHY requests are unavailable in this browser.")
		return
	}
	p.abort = controller.New()
	p.loading = true
	if p.callbacks.OnLoading != nil {
		p.callbacks.OnLoading(true)
	}
	endpoint := giphyAPIBase + "/search"
	if len(p.ids) > 0 {
		endpoint = giphyAPIBase
	} else if trending {
		endpoint = giphyAPIBase + "/trending"
	}
	params := js.Global().Get("URLSearchParams").New()
	params.Call("set", "api_key", strings.TrimSpace(p.config.APIKey))
	params.Call("set", "limit", strconv.Itoa(GiphyPageSize))
	params.Call("set", "offset", strconv.Itoa(offset))
	params.Call("set", "rating", GiphyRating)
	if query != "" && !trending {
		params.Call("set", "q", query)
	}
	if len(p.ids) > 0 {
		params.Call("set", "ids", strings.Join(p.ids, ","))
	}
	options := js.Global().Get("Object").New()
	options.Set("signal", p.abort.Get("signal"))
	options.Set("cache", "no-store")
	fetch := js.Global().Get("fetch")
	if !fetch.Truthy() {
		p.fail(seq, "GIPHY requests are unavailable in this browser.")
		return
	}
	var responseFn, fetchErrorFn js.Func
	responseFn = js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer func() { responseFn.Release() }()
		if p.closed || p.sequence != seq || len(args) == 0 {
			return nil
		}
		response := args[0]
		if !response.Get("ok").Bool() {
			p.fail(seq, "GIPHY could not load GIFs. Try again.")
			return nil
		}
		var jsonFn, jsonErrorFn js.Func
		jsonFn = js.FuncOf(func(_ js.Value, jsonArgs []js.Value) any {
			defer func() { jsonFn.Release() }()
			if p.closed || p.sequence != seq || len(jsonArgs) == 0 {
				return nil
			}
			p.consume(seq, jsonArgs[0], appendResults)
			return nil
		})
		jsonErrorFn = js.FuncOf(func(_ js.Value, _ []js.Value) any {
			defer func() { jsonErrorFn.Release() }()
			p.fail(seq, "GIPHY returned an unreadable response.")
			return nil
		})
		response.Call("json").Call("then", jsonFn).Call("catch", jsonErrorFn)
		return nil
	})
	fetchErrorFn = js.FuncOf(func(_ js.Value, _ []js.Value) any {
		defer func() { fetchErrorFn.Release() }()
		if p.closed || p.sequence != seq {
			return nil
		}
		p.fail(seq, "GIPHY could not load GIFs. Check your connection and try again.")
		return nil
	})
	fetch.Invoke(endpoint+"?"+params.Call("toString").String(), options).Call("then", responseFn).Call("catch", fetchErrorFn)
}

func (p *GiphyPicker) consume(seq uint64, payload js.Value, appendResults bool) {
	results, count, total, valid := parseGiphyPayload(payload)
	if p.closed || p.sequence != seq {
		return
	}
	if !valid {
		p.fail(seq, "GIPHY returned an unexpected response format.")
		return
	}
	p.loading = false
	p.hasMore = count > 0 && p.offset < giphyMaxOffset && (total == 0 || p.offset+count < total)
	if p.callbacks.OnLoading != nil {
		p.callbacks.OnLoading(false)
	}
	if p.callbacks.OnResults != nil {
		p.callbacks.OnResults(results, appendResults, p.hasMore)
	}
}

func parseGiphyPayload(payload js.Value) ([]GiphyResult, int, int, bool) {
	if payload.Type() != js.TypeObject || payload.IsNull() {
		return nil, 0, 0, false
	}
	data := payload.Get("data")
	if data.Type() != js.TypeObject || data.IsNull() || !js.Global().Get("Array").Call("isArray", data).Bool() {
		return nil, 0, 0, false
	}
	results := make([]GiphyResult, 0, GiphyPageSize)
	for i := 0; i < data.Length() && len(results) < GiphyPageSize; i++ {
		item := data.Index(i)
		if item.Type() != js.TypeObject || item.IsNull() {
			continue
		}
		id := giphyJSString(item, "id")
		pageURL, pageID, ok := CanonicalGiphyURL(giphyJSString(item, "url"))
		if !ok || !ValidGiphyID(id) || id != pageID || giphyJSString(item, "rating") != GiphyRating {
			continue
		}
		images := item.Get("images")
		if images.Type() != js.TypeObject || images.IsNull() {
			continue
		}
		preview := imageRendition(images, "fixed_width_small", "url")
		thumbnail := imageRendition(images, "fixed_width_small_still", "url")
		embed := imageRendition(images, "original", "url")
		if !validGiphyMediaURL(preview) || !validGiphyMediaURL(thumbnail) || !validGiphyMediaURL(embed) {
			continue
		}
		alt := strings.TrimSpace(giphyJSString(item, "alt_text"))
		if alt == "" {
			alt = strings.TrimSpace(giphyJSString(item, "title"))
		}
		results = append(results, GiphyResult{ID: id, URL: pageURL, EmbedURL: embed, PreviewURL: preview, ThumbnailURL: thumbnail, Alt: alt, Rating: GiphyRating})
	}
	meta := payload.Get("pagination")
	count, total := 0, 0
	if meta.Type() == js.TypeObject && !meta.IsNull() {
		if value := meta.Get("count"); value.Type() == js.TypeNumber && value.Int() >= 0 {
			count = value.Int()
		}
		if value := meta.Get("total_count"); value.Type() == js.TypeNumber && value.Int() >= 0 {
			total = value.Int()
		}
	}
	return results, count, total, true
}

func imageRendition(images js.Value, rendition, field string) string {
	if images.Type() != js.TypeObject || images.IsNull() {
		return ""
	}
	image := images.Get(rendition)
	if image.Type() != js.TypeObject || image.IsNull() {
		return ""
	}
	value := image.Get(field)
	if value.Type() != js.TypeString {
		return ""
	}
	return value.String()
}

func giphyJSString(object js.Value, key string) string {
	if object.Type() != js.TypeObject || object.IsNull() {
		return ""
	}
	value := object.Get(key)
	if value.Type() != js.TypeString {
		return ""
	}
	return value.String()
}

func (p *GiphyPicker) fail(seq uint64, message string) {
	if p.closed || p.sequence != seq {
		return
	}
	p.hasMore = false
	p.loading = false
	if p.callbacks.OnLoading != nil {
		p.callbacks.OnLoading(false)
	}
	if p.callbacks.OnError != nil {
		p.callbacks.OnError(message)
	}
}
