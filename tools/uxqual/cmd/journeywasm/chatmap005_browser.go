//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Live location, browser side: the running share, its banner, the crew map and
// the channel settings form. The position is read only while a share the person
// started is running; there is no resume after a reload.

type chatmapLiveRunState struct {
	session *ChatmapLiveSession
	cancel  context.CancelFunc
	host    js.Value
}

var (
	chatmapLiveMu  sync.Mutex
	chatmapLiveRun *chatmapLiveRunState
	chatmapPolicy  ChatmapPolicyWire
)

type chatmapLiveHTTP struct{}

func (chatmapLiveHTTP) SendUpdate(ctx context.Context, s *ChatmapLiveSession, place chat.LocationPlace) error {
	cfg := chatBrowser.config(journeyclient.Config{})
	_, err := ChatmapRequestHTTP(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), "update", ChatmapRequest{TenantID: s.Tenant, ConversationID: s.Conversation, PostID: s.PostID, ID: s.ShareID, Place: place})
	return err
}

func init() {
	// Signing out ends every live share. The request is sent with keepalive so
	// it survives the page following the logout link.
	click := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || !args[0].Get("target").Truthy() || !args[0].Get("target").Get("closest").Truthy() {
			return nil
		}
		if !args[0].Get("target").Call("closest", "a.jn-logout,a[href$='/logout']").Truthy() {
			return nil
		}
		cfg := personaChatHTTPConfig(chatBrowser.config(journeyclient.Config{}))
		if cfg.Bearer == "" || cfg.TunnelURL == "" {
			return nil
		}
		js.Global().Call("fetch", cfg.TunnelURL+ChatmapEndpoint("endmine"), map[string]any{
			"method": "POST", "keepalive": true,
			"headers": map[string]any{"Authorization": "Bearer " + cfg.Bearer, "Content-Type": "application/json"},
			"body":    `{"Reason":"signed_out"}`,
		})
		return nil
	})
	js.Global().Get("document").Call("addEventListener", "click", click, true)
}

// ChatmapEndpoint is the path of one location action on the product's origin.
func ChatmapEndpoint(action string) string { return "/api/chat/locations/v1/" + action }

func chatmapLiveStart(p *ChatmapPending, share chat.LocationShare) {
	chatmapLiveMu.Lock()
	defer chatmapLiveMu.Unlock()
	if chatmapLiveRun != nil {
		chatmapLiveRun.cancel()
	}
	if share.ExpiresAt == nil {
		return
	}
	interval := share.LiveInterval
	if interval <= 0 {
		interval = chatmapLiveInterval
	}
	session := &ChatmapLiveSession{Tenant: p.Tenant, Subject: p.Subject, Conversation: p.Conversation, PostID: p.PostID, ShareID: share.ID, ExpiresAt: *share.ExpiresAt, Interval: interval, Precision: p.Draft.Place.Precision, Radius: p.Draft.Place.ApproximateRadius}
	ctx, cancel := context.WithCancel(context.Background())
	run := &chatmapLiveRunState{session: session, cancel: cancel}
	chatmapLiveRun = run
	chatmapRenderBanner(run)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(session.Interval):
			}
			cfg := chatBrowser.config(journeyclient.Config{})
			step, cancelStep := context.WithTimeout(ctx, 20*time.Second)
			_, done := session.Tick(step, chatmapDeviceRecorder{}, chatmapLiveHTTP{}, time.Now(), cfg.Tenant, cfg.Subject)
			cancelStep()
			if ctx.Err() != nil {
				return
			}
			if done {
				chatmapLiveMu.Lock()
				if chatmapLiveRun == run {
					chatmapLiveRun = nil
				}
				chatmapLiveMu.Unlock()
				chatmapRemoveBanner(run)
				chatStreamRender.Schedule()
				return
			}
			chatmapRenderBanner(run)
		}
	}()
}

func chatmapRenderBanner(run *chatmapLiveRunState) {
	cfg := chatBrowser.config(journeyclient.Config{})
	document := js.Global().Get("document")
	if !run.host.Truthy() {
		run.host = document.Call("createElement", "div")
		document.Get("body").Call("appendChild", run.host)
	}
	markup, err := ui.RenderToString(chatui.ChatmapLiveBanner(cfg.Locale, ChatmapMinutesLeft(run.session.ExpiresAt, time.Now())))
	if err == nil {
		run.host.Set("innerHTML", markup)
	}
}

func chatmapRemoveBanner(run *chatmapLiveRunState) {
	if run.host.Truthy() {
		run.host.Call("remove")
		run.host = js.Undefined()
	}
}

// chatmapLiveStop ends every live share of this person: the loop stops, the
// server deletes the positions, and the banner goes.
func chatmapLiveStop(reason string) {
	chatmapLiveMu.Lock()
	run := chatmapLiveRun
	chatmapLiveRun = nil
	chatmapLiveMu.Unlock()
	if run != nil {
		run.cancel()
		chatmapRemoveBanner(run)
	}
	cfg := chatBrowser.config(journeyclient.Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, _ = ChatmapRequestHTTP(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), "endmine", ChatmapRequest{Reason: reason})
	chatStreamRender.Schedule()
}

// chatmapLiveForget drops the local loop when its share was ended elsewhere
// (from the message card or the Sharing now list).
func chatmapLiveForget(shareID string) {
	chatmapLiveMu.Lock()
	run := chatmapLiveRun
	if run != nil && run.session.ShareID == shareID {
		chatmapLiveRun = nil
	} else {
		run = nil
	}
	chatmapLiveMu.Unlock()
	if run != nil {
		run.cancel()
		chatmapRemoveBanner(run)
	}
}

func chatmapOpenMessage(post string) {
	row := js.Global().Get("document").Call("querySelector", "[data-message-id='"+post+"']")
	if row.Truthy() {
		row.Call("scrollIntoView", map[string]any{"block": "center"})
		if row.Get("tabIndex").Int() < 0 {
			row.Set("tabIndex", -1)
		}
		row.Call("focus")
	}
}

// crewMap shows everyone sharing live to this conversation now, drawn by the
// product's own code from the positions the server returned.
func (b *chatmapBrowser) crewMap(sheet js.Value) {
	cfg := chatBrowser.config(journeyclient.Config{})
	data := sheet.Get("dataset")
	req := ChatmapRequest{TenantID: data.Get("tenant").String(), ConversationID: data.Get("conversation").String()}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	body, err := ChatmapRequestHTTP(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), "map", req)
	region := sheet.Call("querySelector", "[data-chatmap-crew]")
	if err != nil || !region.Truthy() {
		b.status(sheet, "failed")
		return
	}
	var view chat.LiveMapView
	if json.Unmarshal(body, &view) != nil {
		b.status(sheet, "failed")
		return
	}
	entries := []chatui.ChatmapCrewEntry{}
	pins := []chat.CrewPin{}
	document := js.Global().Get("document")
	for _, share := range view.Shares {
		name := chatui.ChatmapText(cfg.Locale, "you")
		if share.SharerID != cfg.Subject {
			name = share.SharerID
			if author := document.Call("querySelector", "[data-message-id='"+share.PostID+"'] .message-author"); author.Truthy() {
				name = author.Get("textContent").String()
			}
		}
		number := len(entries) + 1
		entries = append(entries, chatui.ChatmapCrewEntry{Number: number, Name: name, Share: share})
		pins = append(pins, chat.CrewPin{Number: number, Label: name, Position: *share.Place.Position, Paused: share.Paused})
	}
	for index := range view.Sites {
		site := view.Sites[index]
		number := len(entries) + 1
		entries = append(entries, chatui.ChatmapCrewEntry{Number: number, Site: &site})
		pins = append(pins, chat.CrewPin{Number: number, Label: site.Label, Position: site.Position, Site: true})
	}
	pictureURL := ""
	if len(view.Shares) > 0 {
		if picture, perr := chat.RenderCrewMap(pins, chat.MapSize{Width: 400, Height: 300}, chatmapTheme(sheet, cfg.Locale)); perr == nil {
			pictureURL = chatmapBlobURL(picture.Image)
		}
	}
	origin := js.Global().Get("location").Get("origin").String()
	markup, rerr := ui.RenderToString(chatui.ChatmapCrewView(cfg.Locale, entries, pictureURL, origin, time.Now()))
	if rerr != nil {
		b.status(sheet, "failed")
		return
	}
	region.Set("innerHTML", markup)
	b.status(sheet, "")
	go b.applyPolicy(sheet, region)
}

// applyPolicy reads the channel's settings: the composer then explains what is
// off, and an administrator gets the settings form under the crew map.
func (b *chatmapBrowser) applyPolicy(sheet js.Value, formHost js.Value) {
	cfg := chatBrowser.config(journeyclient.Config{})
	data := sheet.Get("dataset")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	body, err := ChatmapRequestHTTP(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), "policy", ChatmapRequest{TenantID: data.Get("tenant").String(), ConversationID: data.Get("conversation").String()})
	var policy ChatmapPolicyWire
	if err != nil || json.Unmarshal(body, &policy) != nil {
		return
	}
	chatmapPolicy = policy
	if !policy.SharingEnabled {
		b.status(sheet, "sharing_off")
		for _, key := range []string{"device", "send", "lookup"} {
			if button := sheet.Call("querySelector", "[data-chatmap-action="+key+"]"); button.Truthy() {
				button.Set("disabled", true)
			}
		}
	}
	if duration := chatmapField(sheet, "duration"); duration.Truthy() && !policy.LiveEnabled {
		for _, key := range []string{"live15", "live60", "live480"} {
			if option := duration.Call("querySelector", "option[value="+key+"]"); option.Truthy() {
				option.Set("disabled", true)
			}
		}
	}
	if !policy.ExactAllowed {
		chatmapSetValue(sheet, "precision", "approximate")
		if precision := chatmapField(sheet, "precision"); precision.Truthy() {
			if option := precision.Call("querySelector", "option[value=exact]"); option.Truthy() {
				option.Set("disabled", true)
			}
		}
	}
	if policy.CanAdminister && formHost.Truthy() && !formHost.Call("querySelector", "[data-chatmap-policy-form]").Truthy() {
		markup, rerr := ui.RenderToString(chatui.ChatmapPolicyForm(cfg.Locale, chatui.ChatmapPolicy{SharingEnabled: policy.SharingEnabled, LiveEnabled: policy.LiveEnabled, ExactAllowed: policy.ExactAllowed, MaxLiveSeconds: policy.MaxLiveSeconds, MaxRetentionSeconds: policy.MaxRetentionSeconds}))
		if rerr == nil {
			holder := js.Global().Get("document").Call("createElement", "div")
			holder.Set("innerHTML", markup)
			formHost.Call("appendChild", holder)
		}
	}
}

func (b *chatmapBrowser) savePolicy(sheet js.Value) {
	cfg := chatBrowser.config(journeyclient.Config{})
	data := sheet.Get("dataset")
	form := sheet.Call("querySelector", "[data-chatmap-policy-form]")
	if !form.Truthy() {
		return
	}
	checked := func(field string) bool {
		return form.Call("querySelector", "[data-chatmap-policy="+field+"]").Get("checked").Bool()
	}
	number := func(field string) int {
		n, _ := strconv.Atoi(form.Call("querySelector", "[data-chatmap-policy="+field+"]").Get("value").String())
		return n
	}
	policy := ChatmapPolicyWire{SharingEnabled: checked("sharing"), LiveEnabled: checked("live"), ExactAllowed: checked("exact"), MaxLiveSeconds: number("maxlive"), MaxRetentionSeconds: number("retention")}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := ChatmapRequestHTTP(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), "setpolicy", ChatmapRequest{TenantID: data.Get("tenant").String(), ConversationID: data.Get("conversation").String(), Policy: policy}); err != nil {
		b.status(sheet, "failed")
		return
	}
	b.status(sheet, "saved")
}

// chatmapLocalTime formats a moment in the reader's own time zone, as the
// browser knows it; Go's own zone data is not the browser's in wasm.
func chatmapLocalTime(milliseconds int64, locale string) string {
	moment := js.Global().Get("Date").New(milliseconds)
	return moment.Call("toLocaleTimeString", locale, map[string]any{"hour": "2-digit", "minute": "2-digit"}).String()
}
