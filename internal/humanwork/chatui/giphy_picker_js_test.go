//go:build js && wasm

package chatui

import (
	"strings"
	"syscall/js"
	"testing"
	"time"
)

func TestParseGiphyPayloadMalformedDataFailsClosed(t *testing.T) {
	for _, raw := range []string{`{}`, `{"data":null}`, `{"data":{}}`, `{"data":[null,{}, {"id":"x","url":"https://giphy.com/gifs/x","rating":"g","images":null}]}`} {
		value := js.Global().Get("JSON").Call("parse", raw)
		results, _, _, valid := parseGiphyPayload(value)
		if raw == `{"data":[null,{}, {"id":"x","url":"https://giphy.com/gifs/x","rating":"g","images":null}]}` {
			if !valid || len(results) != 0 {
				t.Fatalf("malformed image rows yielded valid=%v results=%v", valid, results)
			}
			continue
		}
		if valid || len(results) != 0 {
			t.Fatalf("malformed payload %s yielded valid=%v results=%v", raw, valid, results)
		}
	}
}

func TestGiphyPickerEmptySearchUsesTrendingEndpoint(t *testing.T) {
	previous := js.Global().Get("fetch")
	defer js.Global().Set("fetch", previous)
	var requestURL string
	var jsonFunc js.Func
	fetch := js.FuncOf(func(_ js.Value, args []js.Value) any {
		requestURL = args[0].String()
		response := js.Global().Get("Object").New()
		response.Set("ok", true)
		jsonFunc = js.FuncOf(func(js.Value, []js.Value) any {
			payload := js.ValueOf(map[string]any{"data": []any{}, "pagination": map[string]any{"count": 0, "total_count": 0}})
			return js.Global().Get("Promise").Call("resolve", payload)
		})
		response.Set("json", jsonFunc)
		return js.Global().Get("Promise").Call("resolve", response)
	})
	defer fetch.Release()
	defer func() {
		if jsonFunc.Value.Truthy() {
			jsonFunc.Release()
		}
	}()
	js.Global().Set("fetch", fetch)
	results := make(chan struct{}, 1)
	picker := NewGiphyPicker(GiphyPickerConfig{APIKey: "test-key", DebounceMillis: 1}, GiphyPickerCallbacks{
		OnResults: func([]GiphyResult, bool, bool) { results <- struct{}{} },
	})
	defer picker.Close()
	picker.Search("   ")
	select {
	case <-results:
	case <-time.After(time.Second):
		t.Fatal("empty search did not finish the trending request")
	}
	if !strings.HasPrefix(requestURL, giphyAPIBase+"/trending?") || strings.Contains(requestURL, "q=") {
		t.Fatalf("empty search used the wrong endpoint/query: %q", requestURL)
	}
}

func TestGiphyPickerDismissalRestoresFocusOnlyWhenRequested(t *testing.T) {
	previous := js.Global().Get("document")
	defer js.Global().Set("document", previous)
	docFactory := js.Global().Get("Function").New(`
		var query={removeEventListener:function(){}};
		var dialog={hidden:false};
		var closeButton={removeEventListener:function(){},remove:function(){}};
		var trigger={focusCount:0,setAttribute:function(){},focus:function(){this.focusCount++}};
		return {query:query,dialog:dialog,trigger:trigger,closeButton:closeButton,
		 getElementById:function(id){return id==='compose-giphy-picker-query'?query:id==='compose-giphy-picker'?dialog:null},
		 querySelector:function(){return trigger},removeEventListener:function(){}};`)
	doc := docFactory.Invoke()
	js.Global().Set("document", doc)
	for _, restore := range []bool{false, true} {
		doc.Get("dialog").Set("hidden", false)
		doc.Get("trigger").Set("focusCount", 0)
		queryFn := js.FuncOf(func(js.Value, []js.Value) any { return nil })
		keyFn := js.FuncOf(func(js.Value, []js.Value) any { return nil })
		closeFn := js.FuncOf(func(js.Value, []js.Value) any { return nil })
		v := &giphyView{picker: NewGiphyPicker(GiphyPickerConfig{}, GiphyPickerCallbacks{}), query: queryFn, key: keyFn, close: closeFn, closeButton: doc.Get("closeButton")}
		views := newGiphyPickerViews()
		views.targetID, views.view = "compose", v
		if restore {
			if !views.handleKey("compose", "Escape") {
				t.Fatal("Escape did not dismiss the picker")
			}
		} else {
			views.close("compose", false)
		}
		if got := doc.Get("dialog").Get("hidden").Bool(); !got {
			t.Fatal("dismissal did not hide the dialog")
		}
		want := 0
		if restore {
			want = 1
		}
		if got := doc.Get("trigger").Get("focusCount").Int(); got != want {
			t.Fatalf("restoreFocus=%v, trigger focused %d times, want %d", restore, got, want)
		}
	}
}

func TestGiphyPickerSelectionLeavesFocusInComposer(t *testing.T) {
	previous := js.Global().Get("document")
	defer js.Global().Set("document", previous)
	docFactory := js.Global().Get("Function").New(`
		var query={removeEventListener:function(){}};
		var dialog={hidden:false};
		var closeButton={removeEventListener:function(){},remove:function(){}};
		var trigger={focusCount:0,setAttribute:function(){},focus:function(){this.focusCount++}};
		var composer={value:'',selectionStart:0,selectionEnd:0,focusCount:0,
		 setSelectionRange:function(s,e){this.selectionStart=s;this.selectionEnd=e},
		 dispatchEvent:function(){return true},focus:function(){this.focusCount++}};
		return {query:query,dialog:dialog,trigger:trigger,closeButton:closeButton,composer:composer,
		 getElementById:function(id){return id==='chat-composer'?composer:id==='compose-giphy-picker-query'?query:id==='compose-giphy-picker'?dialog:null},
		 querySelector:function(){return trigger},removeEventListener:function(){}};`)
	doc := docFactory.Invoke()
	js.Global().Set("document", doc)
	queryFn := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	keyFn := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	closeFn := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	views := newGiphyPickerViews()
	item := GiphyResult{ID: "abc123", URL: "https://giphy.com/gifs/wave-abc123", Rating: GiphyRating,
		PreviewURL: "https://media.giphy.com/media/abc123/100w.gif", ThumbnailURL: "https://media.giphy.com/media/abc123/100w_still.jpg"}
	picker := NewGiphyPicker(GiphyPickerConfig{APIKey: "test-key"}, GiphyPickerCallbacks{
		OnSelect: func(result GiphyResult) {
			views.close("compose", false)
			insertGiphyLink("chat-composer", result.URL)
		},
	})
	views.targetID, views.view = "compose", &giphyView{picker: picker, query: queryFn, key: keyFn, close: closeFn, closeButton: doc.Get("closeButton"), results: map[string]GiphyResult{"abc123": item}}
	views.selectResult("compose", "abc123")
	if got := doc.Get("composer").Get("value").String(); got != item.URL {
		t.Fatalf("composer value=%q, want %q", got, item.URL)
	}
	if got := doc.Get("composer").Get("focusCount").Int(); got != 1 {
		t.Fatalf("composer focus count=%d, want 1", got)
	}
	if got := doc.Get("trigger").Get("focusCount").Int(); got != 0 {
		t.Fatalf("GIF trigger stole focus %d times after selection", got)
	}
}

func TestGiphyPickerCloseControlIsVisibleAndNamed(t *testing.T) {
	docFactory := js.Global().Get("Function").New(`
		var button={attrs:{},listeners:{},setAttribute:function(k,v){this.attrs[k]=v},addEventListener:function(k,v){this.listeners[k]=v}};
		var before={inserted:null,before:function(node){this.inserted=node}};
		return {button:button,before:before,createElement:function(tag){button.tag=tag;return button}};`)
	doc := docFactory.Invoke()
	click := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	defer click.Release()
	button := createGiphyCloseButton(doc, doc.Get("before"), "GIF-Auswahl schließen", click)
	if button.Get("tag").String() != "button" || button.Get("type").String() != "button" || button.Get("textContent").String() != "×" {
		t.Fatalf("close affordance is not a visible button: tag=%q type=%q text=%q", button.Get("tag").String(), button.Get("type").String(), button.Get("textContent").String())
	}
	if doc.Get("before").Get("inserted").IsNull() || button.Get("attrs").Get("aria-label").String() != "GIF-Auswahl schließen" || button.Get("listeners").Get("click").IsUndefined() {
		t.Fatal("close affordance was not inserted, localized, and wired")
	}
}

func TestGiphyPickerWithoutKeyIssuesNoRequests(t *testing.T) {
	previous := js.Global().Get("fetch")
	defer js.Global().Set("fetch", previous)
	calls, unavailable := 0, 0
	fetch := js.FuncOf(func(js.Value, []js.Value) any { calls++; return nil })
	defer fetch.Release()
	js.Global().Set("fetch", fetch)
	picker := NewGiphyPicker(GiphyPickerConfig{}, GiphyPickerCallbacks{OnUnavailable: func() { unavailable++ }})
	picker.Search("hello")
	picker.Trending()
	picker.Resolve([]string{"abc"})
	picker.LoadMore()
	if calls != 0 || unavailable != 1 {
		t.Fatalf("fetch calls=%d unavailable=%d", calls, unavailable)
	}
	picker.Close()
}
