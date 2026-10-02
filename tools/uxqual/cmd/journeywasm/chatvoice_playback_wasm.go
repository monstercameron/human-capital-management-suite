//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func (b *chatvoiceBrowser) preferenceKey() string {
	return "chatvoice:" + b.config.Tenant + ":" + b.config.Subject
}

// chatvoiceSession keeps playback speed and the collapsed state for the life of
// the page, until the account's own copy has been read. Browser storage is
// reserved to the approved adapters (WEB-031); the lasting copy is the reader's
// sidebar layout on the server (CHATEMOJI-004), the same store as the emoji
// choices.
var chatvoiceSession = map[string]string{}

func (b *chatvoiceBrowser) loadPreferences() {
	chatRecipientBrowser.Lock()
	raw := chatRecipientBrowser.layout.VoicePrefs
	chatRecipientBrowser.Unlock()
	if raw == "" {
		raw = chatvoiceSession[b.preferenceKey()]
	}
	if raw != "" {
		p := voicePreferenceDecode(raw)
		b.playback.Speed = p.Speed
		b.playback.Collapsed = p.Collapsed
	}
}
func (b *chatvoiceBrowser) savePreferences() {
	raw := voicePreferenceEncode(b.playback)
	chatvoiceSession[b.preferenceKey()] = raw
	chatRecipientBrowser.Lock()
	chatRecipientBrowser.layout.VoicePrefs = raw
	chatRecipientBrowser.Unlock()
	persistChatRecipientSidebar(b.config, chatBrowser.snapshot())
}
func voiceDOMRequest(root js.Value, fallbackTenant string) chat.TranscriptionRequest {
	tenant := root.Call("getAttribute", "data-chatvoice-tenant").String()
	if tenant == "" {
		tenant = fallbackTenant
	}
	return chat.TranscriptionRequest{TenantID: tenant, ConversationID: root.Call("getAttribute", "data-chatvoice-conversation").String(), PostID: root.Call("getAttribute", "data-chatvoice-post").String(), ArtifactID: root.Call("getAttribute", "data-chatvoice-player").String()}
}
func (b *chatvoiceBrowser) playerFeedback(root js.Value, key string) {
	node := root.Call("querySelector", "[data-chatvoice-feedback]")
	if node.Truthy() {
		node.Set("textContent", chatui.VoiceCopy(root.Call("getAttribute", "data-chatvoice-locale").String(), key))
	}
}

func (b *chatvoiceBrowser) hydratePlayer(root js.Value) {
	if !root.Truthy() || !root.Get("isConnected").Bool() {
		return
	}
	// The account's saved speed may have arrived since the player was installed.
	b.loadPreferences()
	r := voiceDOMRequest(root, b.config.Tenant)
	if r.PostID == "" || r.ConversationID == "" || r.ArtifactID == "" {
		return
	}
	key := r.TenantID + ":" + r.ConversationID + ":" + r.PostID + ":" + r.ArtifactID
	if b.loading[key] {
		return
	}
	b.loading[key] = true
	go func() {
		defer func() { delete(b.loading, key) }()
		var record chat.VoiceRecord
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := voiceRequest(ctx, http.DefaultClient, b.config, "read", r, &record); err != nil {
			b.playerFeedback(root, "none")
			return
		}
		if !root.Get("isConnected").Bool() {
			return
		}
		b.transcripts[key] = record
		b.renderTranscript(root, record)
		if record.Transcript.State == chat.TranscriptPending {
			time.AfterFunc(3*time.Second, func() {
				if !b.disposed {
					b.hydratePlayer(root)
				}
			})
		}
	}()
}
func (b *chatvoiceBrowser) renderTranscript(root js.Value, record chat.VoiceRecord) {
	locale := root.Call("getAttribute", "data-chatvoice-locale").String()
	author := root.Call("closest", "[data-chatvoice-author]")
	isAuthor := author.Truthy() && author.Call("getAttribute", "data-chatvoice-author").String() == b.config.Subject
	p := chatui.VoicePlayerProps{Locale: locale, ID: record.ArtifactID, PostID: record.PostID, DurationMS: record.DurationMS, Transcript: record.Transcript, Waveform: record.Attachment.Waveform, CanCorrect: isAuthor, CanReport: true, CanRetry: record.Transcript.State == chat.TranscriptFailed || record.Transcript.State == chat.TranscriptUnavailable}
	markup, err := ui.RenderToString(chatui.RenderVoicePlayer(p))
	if err != nil {
		return
	}
	// The only inserted markup is rendered by GoWebComponents, which escapes
	// transcript text. Playback nodes remain mounted while derived text arrives.
	temporary := js.Global().Get("document").Call("createElement", "div")
	temporary.Set("innerHTML", markup)
	current := root.Call("querySelector", "[data-chatvoice-transcript]")
	next := temporary.Call("querySelector", "[data-chatvoice-transcript]")
	if current.Truthy() && next.Truthy() {
		current.Get("parentNode").Call("replaceChild", next, current)
		expand := next.Call("querySelector", "[data-chatvoice-expand]")
		if expand.Truthy() {
			chatui.SetChatDisclosureOpen(expand, !b.playback.Collapsed)
		}
	}
	waveform := root.Call("querySelector", "[data-chatvoice-waveform]")
	nextWaveform := temporary.Call("querySelector", "[data-chatvoice-waveform]")
	if waveform.Truthy() && nextWaveform.Truthy() {
		waveform.Get("parentNode").Call("replaceChild", nextWaveform, waveform)
	}
	clock := root.Call("querySelector", "[data-chatvoice-time]")
	if clock.Truthy() {
		clock.Set("textContent", voiceBrowserTime(int64(b.playback.Positions[root.Call("getAttribute", "data-chatvoice-playback").String()]*1000))+" / "+voiceBrowserTime(record.DurationMS))
	}
	checkbox := root.Call("querySelector", "[data-chatvoice-collapsed]")
	if checkbox.Truthy() {
		checkbox.Set("checked", b.playback.Collapsed)
	}
	selectNode := root.Call("querySelector", "[data-chatvoice-speed]")
	if selectNode.Truthy() {
		selectNode.Set("value", fmt.Sprint(b.playback.Speed))
	}
}
func (b *chatvoiceBrowser) playerAction(root js.Value, action string) {
	if action == "play-toggle" {
		audio := root.Call("querySelector", "audio")
		if audio.Truthy() && !audio.Get("paused").Bool() {
			audio.Call("pause")
			return
		}
		go b.playProtected(root)
		return
	}
	r := voiceDOMRequest(root, b.config.Tenant)
	key := r.TenantID + ":" + r.ConversationID + ":" + r.PostID + ":" + r.ArtifactID
	record, ok := b.transcripts[key]
	if !ok {
		b.hydratePlayer(root)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var out chat.VoiceRecord
		var input any
		switch action {
		case "retry":
			input = struct {
				Request  chat.TranscriptionRequest
				Revision uint64
			}{r, record.Transcript.Revision}
		case "correct":
			field := root.Call("querySelector", "[data-chatvoice-correction]")
			if !field.Truthy() {
				return
			}
			input = struct {
				Request  chat.TranscriptionRequest
				Revision uint64
				Text     string
			}{r, record.Transcript.Revision, field.Get("value").String()}
		case "report":
			input = r
		default:
			return
		}
		if action == "report" {
			if err := voiceRequest(ctx, http.DefaultClient, b.config, action, input, nil); err != nil {
				b.playerFeedback(root, "actionfailed")
			} else {
				b.playerFeedback(root, "reported")
			}
			return
		}
		if err := voiceRequest(ctx, http.DefaultClient, b.config, action, input, &out); err != nil {
			b.playerFeedback(root, "actionfailed")
			return
		}
		b.transcripts[key] = out
		b.renderTranscript(root, out)
		if action == "retry" {
			b.hydratePlayer(root)
		}
	}()
}

// Each play acquires a fresh grant and reads through the protected media
// boundary. Audio is never read through the image-only preview fetcher.
func (b *chatvoiceBrowser) playProtected(root js.Value) {
	r := voiceDOMRequest(root, b.config.Tenant)
	playbackID := root.Call("getAttribute", "data-chatvoice-playback").String()
	audio := root.Call("querySelector", "audio")
	if !audio.Truthy() || b.disposed {
		return
	}
	controller := js.Global().Get("AbortController").New()
	signal := controller.Get("signal")
	grant, expires, ok := mintChatMediaGrant(b.config, r.ConversationID, r.ArtifactID, signal)
	_, valid := voiceGrantTTL(time.Now(), expires)
	if !ok || !valid {
		audio.Call("pause")
		audio.Call("removeAttribute", "src")
		b.playerFeedback(root, "playbackfailed")
		return
	}
	response, err := chatMediaFetch(chatMediaRoute+r.ArtifactID, b.config, r.ConversationID, grant, signal)
	if err != nil || !response.Truthy() || response.Get("status").Int() != http.StatusOK {
		b.playerFeedback(root, "playbackfailed")
		return
	}
	contentType := response.Get("headers").Call("get", "Content-Type")
	if !contentType.Truthy() || !strings.HasPrefix(strings.ToLower(contentType.String()), "audio/") {
		b.playerFeedback(root, "playbackfailed")
		return
	}
	blob := awaitChatJS(response.Call("blob"))
	if !blob.Truthy() || blob.Get("size").Int() > chat.VoiceMaxBytes || b.disposed || !root.Get("isConnected").Bool() {
		b.playerFeedback(root, "playbackfailed")
		return
	}
	raw := js.Global().Get("URL").Call("createObjectURL", blob).String()
	ttl, valid := voiceGrantTTL(time.Now(), expires)
	if !valid || !voiceOriginSafe(raw) {
		js.Global().Get("URL").Call("revokeObjectURL", raw)
		b.playerFeedback(root, "playbackfailed")
		return
	}
	previous := b.audioURLs[playbackID]
	if previous != "" {
		js.Global().Get("URL").Call("revokeObjectURL", previous)
	}
	b.audioURLs[playbackID] = raw
	audio.Set("src", raw)
	audio.Call("setAttribute", "data-chatvoice-play-approved", "true")
	audio.Set("playbackRate", b.playback.Speed)
	position := b.playback.Positions[playbackID]
	loaded := js.FuncOf(func(_ js.Value, _ []js.Value) any { audio.Set("currentTime", position); return nil })
	audio.Call("addEventListener", "loadedmetadata", loaded, map[string]any{"once": true})
	b.callbacks = append(b.callbacks, loaded)
	audio.Call("play")
	time.AfterFunc(ttl, func() {
		if b.audioURLs[playbackID] == raw {
			audio.Call("pause")
			audio.Call("removeAttribute", "src")
			js.Global().Get("URL").Call("revokeObjectURL", raw)
			delete(b.audioURLs, playbackID)
		}
	})
}
