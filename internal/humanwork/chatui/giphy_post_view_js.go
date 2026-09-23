//go:build js && wasm

package chatui

import (
	"strings"
	"syscall/js"
)

const (
	giphyMessageBodySelector = ".message-body"
	giphyMessageLinkSelector = "a[href]"
	giphyPostCardSelector    = ".giphy-post-embed"
)

type giphyPostView struct {
	root                 js.Value
	room, principal, key string
	picker               *GiphyPicker
	mutations            js.Value
	mutationCallback     js.Func
	imageCleanup         func()
	links                []GiphyLink
	results              map[string]GiphyResult
	desired              map[string]bool
	lastSignature        string
	closed               bool
}

var activeGiphyPostView *giphyPostView

// startChatGiphyPostEmbeds resolves only canonical GIPHY links currently
// rendered in the virtual timeline and thread pane. The picker is separate
// from the composer picker, and its callback is fenced to this room/principal
// lifetime so late responses cannot decorate another conversation.
func startChatGiphyPostEmbeds(apiKey, room, principal string) func() {
	closeActiveGiphyPostView()
	if !GiphyConfigured(GiphyPickerConfig{APIKey: apiKey}) {
		return func() {}
	}
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return func() {}
	}
	root := doc.Call("querySelector", ".chat-workspace")
	if !root.Truthy() {
		return func() {}
	}
	view := &giphyPostView{
		root: root, room: room, principal: principal, key: strings.TrimSpace(apiKey),
		results: map[string]GiphyResult{}, desired: map[string]bool{},
	}
	activeGiphyPostView = view
	view.picker = NewGiphyPicker(GiphyPickerConfig{APIKey: view.key}, GiphyPickerCallbacks{
		OnResults: func(results []GiphyResult, _ bool, _ bool) {
			if !view.current() {
				return
			}
			for _, result := range results {
				if view.desired[result.ID] {
					view.results[result.ID] = result
				}
			}
			view.decorate()
		},
	})
	view.scan()
	if constructor := js.Global().Get("MutationObserver"); constructor.Type() == js.TypeFunction {
		view.mutationCallback = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if !view.current() || (len(args) > 0 && giphyOnlyEmbedMutations(args[0])) {
				return nil
			}
			if view.current() {
				view.scan()
			}
			return nil
		})
		view.mutations = constructor.New(view.mutationCallback)
		options := js.Global().Get("Object").New()
		options.Set("childList", true)
		options.Set("subtree", true)
		view.mutations.Call("observe", root, options)
	}
	return func() { view.close() }
}

func closeActiveGiphyPostView() {
	if activeGiphyPostView != nil {
		activeGiphyPostView.close()
	}
}

func (v *giphyPostView) current() bool {
	if v == nil || v.closed || activeGiphyPostView != v || !v.root.Truthy() || !v.root.Get("isConnected").Truthy() {
		return false
	}
	dataset := v.root.Get("dataset")
	if !dataset.Truthy() || dataset.Get("selectedId").String() != v.room {
		return false
	}
	principal := dataset.Get("principal")
	actualPrincipal := ""
	if principal.Type() == js.TypeString {
		actualPrincipal = principal.String()
	}
	return actualPrincipal == v.principal
}

func giphyOnlyEmbedMutations(records js.Value) bool {
	if !records.Truthy() || records.Length() == 0 {
		return false
	}
	for i := 0; i < records.Length(); i++ {
		record := records.Index(i)
		if record.Get("type").String() != "childList" {
			return false
		}
		found := false
		for _, property := range []string{"addedNodes", "removedNodes"} {
			nodes := record.Get(property)
			for j := 0; nodes.Truthy() && j < nodes.Length(); j++ {
				found = true
				node := nodes.Index(j)
				classes := node.Get("classList")
				if !classes.Truthy() || !classes.Call("contains", "giphy-post-embed").Bool() {
					return false
				}
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (v *giphyPostView) scan() {
	if !v.current() {
		return
	}
	links := make([]GiphyLink, 0, GiphyPageSize)
	seen := map[string]bool{}
	bodies := v.root.Call("querySelectorAll", giphyMessageBodySelector)
	for i := 0; i < bodies.Length() && len(links) < GiphyPageSize; i++ {
		body := bodies.Index(i)
		// The share dialog marks the chat layout aria-hidden while open. Ignore
		// its retained timeline so a hidden view cannot spend a GIPHY request.
		if body.Call("closest", ".chat-layout[aria-hidden='true']").Truthy() {
			continue
		}
		anchors := body.Call("querySelectorAll", giphyMessageLinkSelector)
		for j := 0; j < anchors.Length() && len(links) < GiphyPageSize; j++ {
			canonical, id, ok := CanonicalGiphyURL(anchors.Index(j).Call("getAttribute", "href").String())
			if !ok || seen[id] {
				continue
			}
			seen[id] = true
			links = append(links, GiphyLink{ID: id, URL: canonical})
		}
	}
	ids := make([]string, 0, len(links))
	desired := make(map[string]bool, len(links))
	for _, link := range links {
		ids = append(ids, link.ID)
		desired[link.ID] = true
	}
	signature := strings.Join(ids, "\x00")
	if signature != v.lastSignature {
		for id := range v.results {
			if !desired[id] {
				delete(v.results, id)
			}
		}
		v.desired, v.links, v.lastSignature = desired, links, signature
		missing := make([]string, 0, len(ids))
		for _, id := range ids {
			if _, ok := v.results[id]; !ok {
				missing = append(missing, id)
			}
		}
		if len(missing) > 0 {
			v.picker.Resolve(missing)
		} else {
			v.picker.Cancel()
		}
	} else {
		v.links = links
	}
	v.decorate()
}

func (v *giphyPostView) decorate() {
	if !v.current() {
		return
	}
	if v.imageCleanup != nil {
		v.imageCleanup()
		v.imageCleanup = nil
	}
	bodies := v.root.Call("querySelectorAll", giphyMessageBodySelector)
	for i := 0; i < bodies.Length(); i++ {
		body := bodies.Index(i)
		if body.Call("closest", ".chat-layout[aria-hidden='true']").Truthy() {
			v.removeCards(body)
			continue
		}
		links := body.Call("querySelectorAll", giphyMessageLinkSelector)
		bodyIDs := map[string]bool{}
		for j := 0; j < links.Length(); j++ {
			canonical, id, ok := CanonicalGiphyURL(links.Index(j).Call("getAttribute", "href").String())
			result, hasResult := v.results[id]
			if !ok || !hasResult || bodyIDs[id] {
				continue
			}
			embeds := ResolveGiphyPostEmbeds(canonical, []GiphyResult{result})
			if len(embeds) == 0 {
				continue
			}
			bodyIDs[id] = true
			card := body.Call("querySelector", "[data-giphy-post-embed-id='"+id+"']")
			if card.Truthy() {
				continue
			}
			v.appendCard(body, embeds[0])
		}
		cards := body.Call("querySelectorAll", giphyPostCardSelector)
		for j := cards.Length() - 1; j >= 0; j-- {
			card := cards.Index(j)
			id := card.Get("dataset").Get("giphyPostEmbedId").String()
			if !bodyIDs[id] {
				v.removeCard(card)
			}
		}
	}
	v.imageCleanup = observeGiphyPostEmbeds(v.root)
}

func (v *giphyPostView) removeCards(body js.Value) {
	cards := body.Call("querySelectorAll", giphyPostCardSelector)
	for i := cards.Length() - 1; i >= 0; i-- {
		v.removeCard(cards.Index(i))
	}
}

func (*giphyPostView) removeCard(card js.Value) {
	img := card.Call("querySelector", "img")
	if img.Truthy() {
		img.Call("removeAttribute", "src")
		img.Call("removeAttribute", "data-giphy-embed-src")
	}
	card.Call("remove")
}

func (v *giphyPostView) appendCard(body js.Value, embed GiphyPostEmbed) {
	doc := js.Global().Get("document")
	card := doc.Call("createElement", "a")
	card.Set("className", "giphy-post-embed")
	card.Call("setAttribute", "href", embed.PageURL)
	card.Call("setAttribute", "target", "_blank")
	card.Call("setAttribute", "rel", "noopener noreferrer")
	card.Call("setAttribute", "data-giphy-post-embed-id", embed.ID)
	alt := embed.Alt
	if alt == "" {
		alt = "Animated GIF from GIPHY"
	}
	card.Call("setAttribute", "aria-label", alt+" · Powered by GIPHY")
	img := doc.Call("createElement", "img")
	img.Set("className", "giphy-post-embed-image")
	img.Call("setAttribute", "data-giphy-embed-src", embed.MediaURL)
	img.Call("setAttribute", "alt", alt)
	card.Call("appendChild", img)
	attribution := doc.Call("createElement", "span")
	attribution.Set("className", "giphy-post-embed-attribution")
	attribution.Set("textContent", "Powered by GIPHY")
	card.Call("appendChild", attribution)
	body.Call("appendChild", card)
}

func (v *giphyPostView) close() {
	if v == nil || v.closed {
		return
	}
	v.closed = true
	if v.picker != nil {
		v.picker.Close()
	}
	if v.mutations.Truthy() {
		v.mutations.Call("disconnect")
		v.mutationCallback.Release()
	}
	if v.imageCleanup != nil {
		v.imageCleanup()
		v.imageCleanup = nil
	}
	if v.root.Truthy() && v.root.Get("querySelectorAll").Type() == js.TypeFunction {
		cards := v.root.Call("querySelectorAll", giphyPostCardSelector)
		for i := cards.Length() - 1; i >= 0; i-- {
			img := cards.Index(i).Call("querySelector", "img")
			if img.Truthy() {
				img.Call("removeAttribute", "src")
				img.Call("removeAttribute", "data-giphy-embed-src")
			}
			cards.Index(i).Call("remove")
		}
	}
	v.links = nil
	v.results = nil
	v.desired = nil
	if activeGiphyPostView == v {
		activeGiphyPostView = nil
	}
}
