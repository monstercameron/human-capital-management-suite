//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

type chatmapDeviceRecorder struct{}

func (chatmapDeviceRecorder) ReadPosition(ctx context.Context) (chat.LocationPosition, error) {
	geo := js.Global().Get("navigator").Get("geolocation")
	if !geo.Truthy() {
		return chat.LocationPosition{}, errChatmapNoFix
	}
	type fix struct {
		p   chat.LocationPosition
		err error
	}
	done := make(chan fix, 1)
	success := js.FuncOf(func(_ js.Value, args []js.Value) any {
		c := args[0].Get("coords")
		select {
		case done <- fix{p: chat.LocationPosition{Latitude: c.Get("latitude").Float(), Longitude: c.Get("longitude").Float(), Accuracy: c.Get("accuracy").Float()}}:
		default:
		}
		return nil
	})
	failure := js.FuncOf(func(_ js.Value, args []js.Value) any {
		err := errChatmapNoFix
		if len(args) > 0 && args[0].Get("code").Int() == 1 {
			err = errChatmapRefused
		}
		select {
		case done <- fix{err: err}:
		default:
		}
		return nil
	})
	geo.Call("getCurrentPosition", success, failure, map[string]any{"enableHighAccuracy": true, "timeout": 10000, "maximumAge": 0})
	// Browser callbacks remain alive through timeout; there is no watchPosition.
	select {
	case result := <-done:
		success.Release()
		failure.Release()
		return result.p, result.err
	case <-ctx.Done():
		go func() { <-done; success.Release(); failure.Release() }()
		return chat.LocationPosition{}, ctx.Err()
	}
}

type chatmapBrowser struct {
	draft                     ChatmapDraft
	pending                   *ChatmapPending
	sheet                     js.Value
	click, input, key, online js.Func
	busy                      bool
	previewURL, mapURL        string
	places                    []chat.LocationPlace
	sites                     []chat.LocationSite
	observe, visible          js.Func
	mutation, images          js.Value
}

func init() {
	b := &chatmapBrowser{}
	document := js.Global().Get("document")
	b.click = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			b.handleClick(args[0])
		}
		return nil
	})
	b.input = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		target := args[0].Get("target")
		if !target.Truthy() || !target.Get("closest").Truthy() {
			return nil
		}
		sheet := target.Call("closest", "[data-chatmap-sheet]")
		if sheet.Truthy() {
			b.sheet = sheet
			field := target.Get("dataset").Get("chatmapField").String()
			if field == "lookup" {
				index, err := strconv.Atoi(target.Get("value").String())
				if err == nil && index >= 0 && index < len(b.places) {
					b.choosePlace(sheet, b.places[index])
				}
			}
			if field == "siteid" {
				for _, site := range b.sites {
					if site.ID == target.Get("value").String() {
						chatmapSetValue(sheet, "source", "job_site")
						chatmapSetValue(sheet, "precision", "exact")
						b.choosePlace(sheet, chat.LocationPlace{Position: &site.Position, Label: site.Label, Address: site.Address})
					}
				}
			}
			b.preview(sheet)
		}
		return nil
	})
	b.key = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		event := args[0]
		target := event.Get("target")
		if !target.Truthy() {
			return nil
		}
		if event.Get("key").String() == "Enter" && target.Get("tagName").String() == "INPUT" && target.Call("closest", "[data-chatmap-sheet]").Truthy() {
			event.Call("preventDefault")
			event.Call("stopImmediatePropagation")
			return nil
		}
		if target.Call("matches", "[data-chatmap-preview-region]").Bool() {
			action := chatPolishPinAction(event.Get("key").String())
			if action != "" {
				event.Call("preventDefault")
				target.Call("querySelector", "[data-chatmap-action="+action+"]").Call("click")
				return nil
			}
		}
		if event.Get("key").String() == "Escape" {
			if target.Call("closest", "dialog[open]").Truthy() {
				return nil
			}
			sheet := target.Call("closest", "[data-chatmap-sheet]")
			if sheet.Truthy() && chatui.ChatLayerIsTop(sheet) {
				event.Call("preventDefault")
				event.Call("stopImmediatePropagation")
				chatui.CloseLocationComposer(sheet, true)
			}
		}
		// "/location" typed in the composer is the command registry's now
		// (chatui composer_commands.go): it empties the draft and presses the
		// same toggle this file handles on click.
		return nil
	})
	b.online = js.FuncOf(func(js.Value, []js.Value) any {
		if b.pending != nil && b.pending.Queued {
			go b.deliver()
		}
		return nil
	})
	document.Call("addEventListener", "click", b.click)
	document.Call("addEventListener", "input", b.input)
	document.Call("addEventListener", "change", b.input)
	document.Call("addEventListener", "keydown", b.key, true)
	js.Global().Call("addEventListener", "online", b.online)
	if js.Global().Get("IntersectionObserver").Truthy() {
		b.visible = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				entries := args[0]
				for i := 0; i < entries.Get("length").Int(); i++ {
					entry := entries.Index(i)
					if entry.Get("isIntersecting").Bool() {
						image := entry.Get("target")
						b.images.Call("unobserve", image)
						go b.loadPicture(image)
					}
				}
			}
			return nil
		})
		b.images = js.Global().Get("IntersectionObserver").New(b.visible, map[string]any{"rootMargin": "0px"})
	}
	b.observe = js.FuncOf(func(js.Value, []js.Value) any { b.observeCards(); return nil })
	b.mutation = js.Global().Get("MutationObserver").New(b.observe)
	b.mutation.Call("observe", document.Get("documentElement"), map[string]any{"childList": true, "subtree": true})
	b.observeCards()
}
func chatmapField(sheet js.Value, key string) js.Value {
	return sheet.Call("querySelector", "[data-chatmap-field='"+key+"']")
}
func chatmapValue(sheet js.Value, key string) string {
	v := chatmapField(sheet, key)
	if !v.Truthy() {
		value := sheet.Call("getAttribute", "data-chatmap-"+key)
		if value.Type() == js.TypeString {
			return value.String()
		}
		return ""
	}
	return v.Get("value").String()
}
func chatmapNumber(sheet js.Value, key string) (float64, error) {
	return strconv.ParseFloat(chatmapValue(sheet, key), 64)
}

// chatmapActionLabel sets the words on one of the sheet's buttons.
func chatmapActionLabel(sheet js.Value, action, key string) {
	if !sheet.Truthy() {
		return
	}
	if button := sheet.Call("querySelector", "[data-chatmap-action="+action+"]"); button.Truthy() {
		button.Set("textContent", chatui.ChatmapText(sheet.Get("dataset").Get("locale").String(), key))
	}
}

func (b *chatmapBrowser) status(sheet js.Value, key string) {
	if !sheet.Truthy() {
		return
	}
	if node := sheet.Call("querySelector", "[data-chatmap-status]"); node.Truthy() {
		node.Set("textContent", chatui.ChatmapText(sheet.Get("dataset").Get("locale").String(), key))
	}
}
func (b *chatmapBrowser) readDraft(sheet js.Value) (ChatmapDraft, error) {
	now := time.Now()
	_, live := ChatmapLiveDuration(chatmapValue(sheet, "duration"))
	if live && chat.LocationSource(chatmapValue(sheet, "source")) != chat.LocationDevice {
		// A live share follows the device; it cannot follow an address or a site.
		return ChatmapDraft{}, chat.ErrLocationUnavailable
	}
	if chat.LocationSource(chatmapValue(sheet, "source")) == chat.LocationJobSite {
		if chatmapValue(sheet, "siteid") == "" {
			return ChatmapDraft{}, chat.ErrLocationUnavailable
		}
		return ChatmapDraft{Place: chat.LocationPlace{Source: chat.LocationJobSite, SiteID: chatmapValue(sheet, "siteid"), CapturedAt: now, Precision: "exact"}, Note: chatmapValue(sheet, "note"), ExpiresAt: ChatmapExpiry(chatmapValue(sheet, "duration"), now)}, nil
	}
	if text := strings.TrimSpace(chatmapValue(sheet, "readable")); chat.LocationSource(chatmapValue(sheet, "source")) == chat.LocationAddress && chatmapValue(sheet, "lat") == "" && text != "" {
		// Typed text alone is a complete share: no position is invented for it.
		return ChatmapDraft{Place: chat.LocationPlace{Source: chat.LocationAddress, Address: text, CapturedAt: now, Precision: "exact"}, Note: chatmapValue(sheet, "note"), ExpiresAt: ChatmapExpiry(chatmapValue(sheet, "duration"), now), Captured: true}, nil
	}
	lat, e1 := chatmapNumber(sheet, "lat")
	lon, e2 := chatmapNumber(sheet, "lon")
	accuracy, e3 := chatmapNumber(sheet, "accuracy")
	if e1 != nil || e2 != nil || e3 != nil {
		return ChatmapDraft{}, chat.ErrInvalidArgument
	}
	source := chat.LocationSource(chatmapValue(sheet, "source"))
	if source == chat.LocationJobSite {
		return ChatmapDraft{}, chat.ErrLocationUnavailable
	}
	captured := now
	if source == chat.LocationDevice {
		if !b.draft.Captured {
			return ChatmapDraft{}, errChatmapNoFix
		}
		captured = b.draft.Place.CapturedAt
	}
	p := chat.LocationPlace{Position: &chat.LocationPosition{Latitude: lat, Longitude: lon, Accuracy: accuracy}, Source: source, Label: chatmapValue(sheet, "label"), Address: chatmapValue(sheet, "readable"), Precision: chatmapValue(sheet, "precision"), ApproximateRadius: 500, CapturedAt: captured}
	prepared, err := chat.PrepareLocation(p, now)
	if err != nil {
		return ChatmapDraft{}, err
	}
	return ChatmapDraft{Place: prepared, Note: chatmapValue(sheet, "note"), ExpiresAt: ChatmapExpiry(chatmapValue(sheet, "duration"), now), Captured: true, Live: live}, nil
}
func (b *chatmapBrowser) preview(sheet js.Value) {
	_, live := ChatmapLiveDuration(chatmapValue(sheet, "duration"))
	if node := sheet.Call("querySelector", "[data-chatmap-live-note]"); node.Truthy() {
		node.Set("hidden", !live)
	}
	draft, err := b.readDraft(sheet)
	if err != nil {
		if send := sheet.Call("querySelector", "[data-chatmap-action=send]"); send.Truthy() {
			send.Set("disabled", true)
		}
		if live && err == chat.ErrLocationUnavailable {
			b.status(sheet, "live_device")
		}
		return
	}
	if draft.Place.Source == chat.LocationJobSite {
		for _, site := range b.sites {
			if site.ID == draft.Place.SiteID {
				draft.Place.Position = &site.Position
				draft.Place.Label = site.Label
				draft.Place.Address = site.Address
			}
		}
	}
	if draft.Place.Position == nil && draft.Place.Source == chat.LocationAddress {
		// No position to draw: readers get the typed address and "Map detail
		// unavailable", and the person can send it as it stands.
		if image := sheet.Call("querySelector", "[data-chatmap-preview]"); image.Truthy() {
			image.Set("hidden", true)
		}
		if send := sheet.Call("querySelector", "[data-chatmap-action=send]"); send.Truthy() {
			send.Set("disabled", false)
		}
		return
	}
	pic, err := (chat.SchematicMap{}).Render(draft.Place, chat.LocationMapZoom(draft.Place, chat.MapSize{Width: 400, Height: 200}), chat.MapSize{Width: 400, Height: 200}, chatmapTheme(sheet, sheet.Get("dataset").Get("locale").String()))
	if err != nil {
		return
	}
	image := sheet.Call("querySelector", "[data-chatmap-preview]")
	if image.Truthy() {
		if b.previewURL != "" {
			js.Global().Get("URL").Call("revokeObjectURL", b.previewURL)
		}
		b.previewURL = chatmapBlobURL(pic.Image)
		image.Set("src", b.previewURL)
		image.Set("hidden", false)
		if placeholder := image.Get("parentElement").Call("querySelector", ".chat-icon"); placeholder.Truthy() {
			placeholder.Set("hidden", true)
		}
	}
	if send := sheet.Call("querySelector", "[data-chatmap-action=send]"); send.Truthy() {
		send.Set("disabled", false)
	}
}
func chatmapBlobURL(data []byte) string {
	array := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(array, data)
	blob := js.Global().Get("Blob").New([]any{array}, map[string]any{"type": "image/svg+xml"})
	return js.Global().Get("URL").Call("createObjectURL", blob).String()
}
func chatmapTheme(element js.Value, locale string) chat.MapTheme {
	style := js.Global().Call("getComputedStyle", element)
	read := func(key string) string { return strings.TrimSpace(style.Call("getPropertyValue", key).String()) }
	return chat.MapTheme{Locale: locale, Dark: strings.Contains(style.Get("colorScheme").String(), "dark"), Surface: read("--hcm-color-surface"), Text: read("--hcm-color-text"), Accent: read("--hcm-color-brand-primary")}
}
func (b *chatmapBrowser) handleClick(event js.Value) {
	target := event.Get("target")
	if !target.Truthy() || !target.Get("closest").Truthy() {
		return
	}
	button := target.Call("closest", "[data-chatmap-action]")
	if !button.Truthy() {
		return
	}
	event.Call("preventDefault")
	action := button.Get("dataset").Get("chatmapAction").String()
	if action == "toggle" {
		chatui.ToggleLocationComposer(button)
		if panel := button.Call("closest", ".chatmap-control").Call("querySelector", "[data-chatmap-sheet]"); panel.Truthy() {
			go b.applyPolicy(panel, js.Undefined())
		}
		return
	}
	if action == "stop-all" {
		go chatmapLiveStop(chat.LiveEndedStopped)
		return
	}
	if action == "crew-open" {
		chatmapOpenMessage(button.Get("dataset").Get("post").String())
		return
	}
	sheet := button.Call("closest", "[data-chatmap-sheet]")
	if !sheet.Truthy() {
		card := button.Call("closest", "[data-share]")
		if card.Truthy() && card.Get("dataset").Get("share").String() != "" {
			if action == "copy" {
				if expiry, err := strconv.ParseInt(card.Get("dataset").Get("expires").String(), 10, 64); err == nil && time.Now().UnixMilli() >= expiry {
					return
				}
				address := card.Call("querySelector", "[data-chatmap-address]")
				clipboard := js.Global().Get("navigator").Get("clipboard")
				if address.Truthy() && clipboard.Truthy() {
					promise := clipboard.Call("writeText", address.Get("textContent").String())
					go func() { awaitChatJS(promise) }()
				}
				return
			}
			go b.cardAction(card, action)
		}
		return
	}
	b.sheet = sheet
	switch action {
	case "sheet-close":
		chatui.CloseLocationComposer(sheet, true)
	case "source":
		source := button.Get("dataset").Get("source").String()
		chatmapSetValue(sheet, "source", source)
		if source == "typed_address" {
			// Typed words start without the position read for "Where I am".
			for _, key := range []string{"lat", "lon", "accuracy"} {
				chatmapSetValue(sheet, key, "")
			}
		}
		choices := sheet.Call("querySelectorAll", "[data-chatmap-choice]")
		for i := 0; i < choices.Length(); i++ {
			choices.Index(i).Set("hidden", choices.Index(i).Get("dataset").Get("chatmapChoice").String() != source)
		}
		buttons := sheet.Call("querySelectorAll", "[data-chatmap-action=source]")
		for i := 0; i < buttons.Length(); i++ {
			buttons.Index(i).Call("setAttribute", "aria-pressed", boolText(buttons.Index(i).Get("dataset").Get("source").String() == source))
		}
		b.status(sheet, "")
		b.preview(sheet)
		if source == "job_site" && !sheet.Call("querySelector", "[data-chatmap-unavailable=sites]").Get("hidden").Bool() {
			return
		}
		if source == "job_site" {
			go b.load(sheet, "sites")
		}
	case "pin":
		if _, err := b.readDraft(sheet); err != nil {
			b.status(sheet, "no_fix")
			sheet.Call("querySelector", "[data-chatmap-preview-region]").Call("focus")
			return
		}
		chatmapSetValue(sheet, "source", "picked_on_map")
		b.preview(sheet)
		b.status(sheet, "")
	case "sites", "lookup", "sharing":
		go b.load(sheet, action)
	case "crewmap":
		go b.crewMap(sheet)
	case "save-policy":
		go b.savePolicy(sheet)
	case "device":
		if b.busy {
			return
		}
		b.busy = true
		go func() {
			defer func() { b.busy = false }()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			err := b.draft.PressRead(ctx, chatmapDeviceRecorder{}, time.Now())
			if err != nil {
				b.status(sheet, b.draft.Status)
				// A failed fix leaves one obvious way forward: the same button, now "Try again".
				chatmapActionLabel(sheet, "device", "retry")
				return
			}
			chatmapActionLabel(sheet, "device", "device")
			p := b.draft.Place.Position
			chatmapSetValue(sheet, "source", "device")
			chatmapSetValue(sheet, "lat", strconv.FormatFloat(p.Latitude, 'f', 6, 64))
			chatmapSetValue(sheet, "lon", strconv.FormatFloat(p.Longitude, 'f', 6, 64))
			chatmapSetValue(sheet, "accuracy", strconv.FormatFloat(p.Accuracy, 'f', 0, 64))
			b.status(sheet, b.draft.Status)
			b.preview(sheet)
		}()
	case "north", "south", "east", "west":
		field, delta := "lat", 0.0001
		if action == "east" || action == "west" {
			field = "lon"
		}
		if action == "south" || action == "west" {
			delta = -delta
		}
		n, err := chatmapNumber(sheet, field)
		if err != nil {
			b.status(sheet, "no_fix")
			return
		}
		chatmapSetValue(sheet, field, strconv.FormatFloat(n+delta, 'f', 6, 64))
		chatmapSetValue(sheet, "source", "picked_on_map")
		b.preview(sheet)
	case "send":
		if b.busy {
			return
		}
		d, err := b.readDraft(sheet)
		if err != nil {
			key := "no_fix"
			if _, live := ChatmapLiveDuration(chatmapValue(sheet, "duration")); live && err == chat.ErrLocationUnavailable {
				key = "live_device"
			} else if err == chat.ErrLocationUnavailable {
				key = "sites_unavailable"
			}
			b.status(sheet, key)
			return
		}
		cfg := chatBrowser.config(journeyclient.Config{})
		if b.pending != nil && !b.pending.Sent && b.pending.Conversation != sheet.Get("dataset").Get("conversation").String() {
			b.status(sheet, "offline")
			return
		}
		if b.pending == nil || b.pending.Sent {
			b.pending = &ChatmapPending{Draft: d, Tenant: cfg.Tenant, Subject: cfg.Subject, Conversation: sheet.Get("dataset").Get("conversation").String(), Key: fmt.Sprintf("chatmap-%d", time.Now().UnixNano())}
		}
		fields := sheet.Call("querySelectorAll", "[data-chatmap-field]")
		for i := 0; i < fields.Get("length").Int(); i++ {
			fields.Index(i).Set("disabled", true)
		}
		go b.deliver()
	}
}
func (b *chatmapBrowser) deliver() {
	if b.busy {
		return
	}
	b.busy = true
	defer func() { b.busy = false }()
	cfg := chatBrowser.config(journeyclient.Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = b.pending.Deliver(ctx, b, js.Global().Get("navigator").Get("onLine").Bool(), time.Now(), cfg.Tenant, cfg.Subject)
	if !b.pending.Sent && b.pending.Draft.Status == "failed" && !js.Global().Get("navigator").Get("onLine").Bool() {
		b.pending.Queued = true
		b.pending.Draft.Status = "offline"
	}
	b.status(b.sheet, b.pending.Draft.Status)
	// A share that did not go says so and the Send button reads "Try again".
	if !b.pending.Sent && b.pending.Draft.Status == "failed" {
		chatmapActionLabel(b.sheet, "send", "retry")
	} else {
		chatmapActionLabel(b.sheet, "send", "send")
	}
	if b.pending.Sent || b.pending.Draft.Status == "discarded" {
		fields := b.sheet.Call("querySelectorAll", "[data-chatmap-field]")
		for i := 0; i < fields.Get("length").Int(); i++ {
			field := fields.Index(i)
			field.Set("disabled", false)
			if field.Get("tagName").String() == "INPUT" {
				field.Set("value", "")
			}
		}
		b.draft = ChatmapDraft{}
		if b.pending.Draft.Status == "discarded" {
			b.pending = nil
		}
	}
}
func (b *chatmapBrowser) SendLocation(ctx context.Context, p *ChatmapPending) error {
	cfg := chatBrowser.config(journeyclient.Config{})
	if p.PostID == "" {
		client := chatBrowser.conversationClient()
		if client == nil {
			return chat.ErrUnavailable
		}
		note := p.Draft.Note
		if strings.TrimSpace(note) == "" {
			note = chatui.ChatmapText(cfg.Locale, "location")
		}
		sent, err := client.SendPost(chatRPCContext(ctx, cfg), &chatv1.SendPostRequest{TenantId: p.Tenant, ConversationId: p.Conversation, Body: note, IdempotencyKey: p.Key})
		if err != nil {
			return err
		}
		p.PostID = sent.GetPost().GetId()
		p.Revision = sent.GetPost().GetRevision()
	}
	current := chatBrowser.config(journeyclient.Config{})
	if current.Tenant != p.Tenant || current.Subject != p.Subject {
		return chat.ErrPermissionDenied
	}
	if p.Draft.Live && p.ShareID == "" {
		// One live share runs at a time on a device: an earlier one ends first.
		chatmapLiveStop(chat.LiveEndedStopped)
	}
	request := ChatmapRequest{TenantID: p.Tenant, ConversationID: p.Conversation, PostID: p.PostID, PostRevision: p.Revision, Place: p.Draft.Place, ExpiresAt: p.Draft.ExpiresAt}
	if p.Draft.Live {
		request.Live, request.LiveIntervalSeconds = true, int(chatmapLiveInterval/time.Second)
	}
	raw, err := ChatmapRequestHTTP(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), "attach", request)
	if err != nil {
		return err
	}
	var share chat.LocationShare
	if json.Unmarshal(raw, &share) == nil {
		p.ShareID = share.ID
		if p.Draft.Live {
			chatmapLiveStart(p, share)
		}
	}
	chatStreamRender.Schedule()
	return nil
}
func (b *chatmapBrowser) cardAction(card js.Value, action string) {
	cfg := chatBrowser.config(journeyclient.Config{})
	data := card.Get("dataset")
	req := ChatmapRequest{TenantID: data.Get("tenant").String(), ConversationID: data.Get("conversation").String(), PostID: data.Get("post").String(), ID: data.Get("share").String()}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if action == "close" {
		dialog := card.Call("querySelector", "dialog")
		if dialog.Truthy() {
			dialog.Call("close")
		}
		if b.mapURL != "" {
			js.Global().Get("URL").Call("revokeObjectURL", b.mapURL)
			b.mapURL = ""
		}
		return
	}
	if action == "stop" {
		_, err := ChatmapRequestHTTP(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), "end", req)
		if err == nil {
			chatmapLiveForget(req.ID)
			card.Set("textContent", chatui.ChatmapText(cfg.Locale, "expired")+" · "+fmt.Sprintf(chatui.ChatmapText(cfg.Locale, "ended_at"), chatmapLocalTime(time.Now().UnixMilli(), cfg.Locale)))
		}
		return
	}
	body, err := ChatmapRequestHTTP(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), "read", req)
	if err != nil {
		return
	}
	var share chat.LocationShare
	if json.Unmarshal(body, &share) != nil || share.Ended || share.Place.Position == nil {
		return
	}
	switch action {
	case "open", "zoom_in", "zoom_out", "north", "south", "east", "west":
		zoom, _ := strconv.Atoi(data.Get("zoom").String())
		if zoom == 0 {
			zoom = chat.LocationMapZoom(share.Place, chat.MapSize{Width: 800, Height: 400})
		}
		panX, _ := strconv.Atoi(data.Get("panX").String())
		panY, _ := strconv.Atoi(data.Get("panY").String())
		switch action {
		case "zoom_in":
			zoom = min(20, zoom+1)
		case "zoom_out":
			zoom = max(1, zoom-1)
		case "north":
			panY = min(800, panY+80)
		case "south":
			panY = max(-800, panY-80)
		case "east":
			panX = max(-1200, panX-80)
		case "west":
			panX = min(1200, panX+80)
		}
		data.Set("zoom", strconv.Itoa(zoom))
		data.Set("panX", strconv.Itoa(panX))
		data.Set("panY", strconv.Itoa(panY))
		req.Zoom = zoom
		req.Size = chat.MapSize{Width: 800, Height: 400}
		req.Theme = chatmapTheme(card, cfg.Locale)
		req.Theme.PanX = panX
		req.Theme.PanY = panY
		picture, err := ChatmapRequestHTTP(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), "picture", req)
		if err != nil {
			return
		}
		if b.mapURL != "" {
			js.Global().Get("URL").Call("revokeObjectURL", b.mapURL)
		}
		b.mapURL = chatmapBlobURL(picture)
		img := card.Call("querySelector", "[data-chatmap-large-picture]")
		if img.Truthy() {
			img.Set("src", b.mapURL)
		}
		dialog := card.Call("querySelector", "dialog")
		if dialog.Truthy() && !dialog.Get("open").Bool() {
			dialog.Call("showModal")
		}
	}
}
func (b *chatmapBrowser) choosePlace(sheet js.Value, p chat.LocationPlace) {
	if p.Position == nil {
		return
	}
	for key, value := range map[string]string{
		"lat":      strconv.FormatFloat(p.Position.Latitude, 'f', 6, 64),
		"lon":      strconv.FormatFloat(p.Position.Longitude, 'f', 6, 64),
		"accuracy": strconv.FormatFloat(p.Position.Accuracy, 'f', 0, 64),
		"label":    p.Label, "readable": p.Address,
	} {
		chatmapSetValue(sheet, key, value)
	}
	if p.Source == chat.LocationAddress {
		chatmapSetValue(sheet, "source", "typed_address")
	}
	b.preview(sheet)
}
func (b *chatmapBrowser) load(sheet js.Value, action string) {
	cfg := chatBrowser.config(journeyclient.Config{})
	b.status(sheet, "loading")
	req := ChatmapRequest{TenantID: sheet.Get("dataset").Get("tenant").String(), ConversationID: sheet.Get("dataset").Get("conversation").String(), Query: chatmapValue(sheet, "readable")}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	endpoint := action
	if action == "sharing" {
		// The Sharing now list is the person's own shares across conversations.
		endpoint = "mine"
	}
	body, err := ChatmapRequestHTTP(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), endpoint, req)
	active := chatBrowser.config(journeyclient.Config{})
	if active.Tenant != cfg.Tenant || active.Subject != cfg.Subject {
		return
	}
	if err != nil {
		b.choiceUnavailable(sheet, action)
		return
	}
	document := js.Global().Get("document")
	switch action {
	case "lookup":
		if json.Unmarshal(body, &b.places) != nil {
			b.choiceUnavailable(sheet, action)
			return
		}
		selector := chatmapField(sheet, "lookup")
		selector.Set("hidden", false)
		selector.Set("textContent", "")
		for index, place := range b.places {
			option := document.Call("createElement", "option")
			option.Set("value", strconv.Itoa(index))
			option.Set("textContent", place.Label+" · "+place.Address)
			selector.Call("appendChild", option)
		}
		if len(b.places) > 0 {
			b.choosePlace(sheet, b.places[0])
		} else {
			b.choiceUnavailable(sheet, action)
			return
		}
	case "sites":
		if json.Unmarshal(body, &b.sites) != nil {
			b.choiceUnavailable(sheet, action)
			return
		}
		if b.draft.Place.Position != nil {
			point := b.draft.Place.Position
			distance := func(s chat.LocationSite) float64 {
				return ChatmapDistance(*point, s.Position)
			}
			sort.SliceStable(b.sites, func(i, j int) bool { return distance(b.sites[i]) < distance(b.sites[j]) })
		}
		selector := chatmapField(sheet, "siteid")
		selector.Set("hidden", false)
		selector.Set("textContent", "")
		prompt := document.Call("createElement", "option")
		prompt.Set("value", "")
		prompt.Set("textContent", chatui.ChatmapText(cfg.Locale, "site"))
		selector.Call("appendChild", prompt)
		for _, site := range b.sites {
			option := document.Call("createElement", "option")
			option.Set("value", site.ID)
			option.Set("textContent", site.Label+" · "+site.Address)
			selector.Call("appendChild", option)
		}
		if len(b.sites) == 0 {
			b.choiceUnavailable(sheet, action)
			return
		}
	case "sharing":
		var shares []chat.LocationShare
		if json.Unmarshal(body, &shares) != nil {
			b.choiceUnavailable(sheet, action)
			return
		}
		region := sheet.Call("querySelector", "[data-chatmap-sharing]")
		region.Set("textContent", "")
		if len(shares) == 0 {
			region.Set("textContent", chatui.ChatmapText(cfg.Locale, "mine_empty"))
		}
		for _, share := range shares {
			markup, err := ui.RenderToString(chatui.ChatmapLocationEmbed(chatui.ChatmapEmbed{Share: share, Sharer: chatui.ChatmapText(cfg.Locale, "you"), ViewerID: cfg.Subject, ViewerTenantID: cfg.Tenant, Locale: cfg.Locale, Now: time.Now()}))
			if err != nil {
				continue
			}
			holder := document.Call("createElement", "div")
			holder.Set("innerHTML", markup)
			region.Call("appendChild", holder)
		}
	}
	b.status(sheet, "")
}
func (b *chatmapBrowser) observeCards() {
	cards := js.Global().Get("document").Call("querySelectorAll", ".chatmap-card[data-share]:not([data-share='']):not([data-chatmap-observed])")
	for i := 0; i < cards.Get("length").Int(); i++ {
		card := cards.Index(i)
		card.Call("setAttribute", "data-chatmap-observed", "true")
		image := card.Call("querySelector", "img")
		if image.Truthy() && b.images.Truthy() && image.Get("src").String() == "" {
			b.images.Call("observe", image)
		}
		if ended := card.Call("querySelector", "[data-chatmap-ended-ms]"); ended.Truthy() {
			if ms, err := strconv.ParseInt(ended.Get("dataset").Get("chatmapEndedMs").String(), 10, 64); err == nil {
				locale := card.Get("dataset").Get("locale").String()
				ended.Set("textContent", fmt.Sprintf(chatui.ChatmapText(locale, "ended_at"), chatmapLocalTime(ms, locale)))
			}
		}
		if card.Get("dataset").Get("live").String() == "true" && image.Truthy() {
			// A live card's picture follows the sharer while the page is open.
			var refresh func()
			refresh = func() {
				if !card.Get("isConnected").Bool() || card.Get("dataset").Get("live").String() != "true" {
					return
				}
				go b.loadPicture(image)
				time.AfterFunc(chatmapLiveInterval, refresh)
			}
			time.AfterFunc(chatmapLiveInterval, refresh)
		}
		expires := card.Get("dataset").Get("expires").String()
		if milliseconds, err := strconv.ParseInt(expires, 10, 64); err == nil {
			delay := max(time.Duration(0), time.Until(time.UnixMilli(milliseconds)))
			time.AfterFunc(delay, func() {
				if card.Get("isConnected").Bool() {
					card.Set("textContent", chatui.ChatmapText(card.Get("dataset").Get("locale").String(), "expired"))
				}
			})
		}
	}
}
func (b *chatmapBrowser) loadPicture(image js.Value) {
	card := image.Call("closest", ".chatmap-card")
	if !card.Truthy() {
		return
	}
	cfg := chatBrowser.config(journeyclient.Config{})
	data := card.Get("dataset")
	req := ChatmapRequest{TenantID: data.Get("tenant").String(), ConversationID: data.Get("conversation").String(), PostID: data.Get("post").String(), ID: data.Get("share").String()}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	raw, err := ChatmapRequestHTTP(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), "read", req)
	if err != nil {
		return
	}
	var share chat.LocationShare
	if json.Unmarshal(raw, &share) != nil || share.Ended || share.Place.Position == nil {
		return
	}
	req.Zoom = chat.LocationMapZoom(share.Place, chat.MapSize{Width: 400, Height: 200})
	req.Size = chat.MapSize{Width: 400, Height: 200}
	req.Theme = chatmapTheme(card, cfg.Locale)
	bytes, err := ChatmapRequestHTTP(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), "picture", req)
	current := chatBrowser.config(journeyclient.Config{})
	if err != nil || cfg.Tenant != current.Tenant || cfg.Subject != current.Subject || !image.Get("isConnected").Bool() {
		return
	}
	blob := chatmapBlobURL(bytes)
	image.Set("src", blob)
	until := time.Minute
	if share.ExpiresAt != nil {
		until = min(until, max(time.Duration(0), time.Until(*share.ExpiresAt)))
	}
	time.AfterFunc(until, func() { js.Global().Get("URL").Call("revokeObjectURL", blob) })
}

func chatmapSetValue(sheet js.Value, key, value string) {
	if field := chatmapField(sheet, key); field.Truthy() {
		field.Set("value", value)
	} else {
		sheet.Call("setAttribute", "data-chatmap-"+key, value)
	}
}

func (b *chatmapBrowser) choiceUnavailable(sheet js.Value, action string) {
	if line := sheet.Call("querySelector", "[data-chatmap-unavailable="+action+"]"); line.Truthy() {
		line.Set("hidden", false)
	}
	if button := sheet.Call("querySelector", "[data-chatmap-action="+action+"]"); button.Truthy() {
		button.Set("disabled", true)
	}
	b.status(sheet, "")
}
