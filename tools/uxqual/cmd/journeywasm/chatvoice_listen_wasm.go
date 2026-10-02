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
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func listenSay(row js.Value, locale, key string) {
	status := row.Call("querySelector", "[data-chatlisten-status]")
	if status.Truthy() {
		status.Set("textContent", chatui.ListenCopy(locale, key))
	}
}

// listenPlaying switches the player row between its message-only and its
// reading shapes.
func listenPlaying(row js.Value, playing bool) {
	controls := row.Call("querySelector", "[data-chatlisten-controls]")
	if controls.Truthy() {
		controls.Set("hidden", !playing)
	}
}

// listenQuiet is whether the row was opened for a likely screen reader: it
// starts nothing by itself, and its status line says nothing while audio plays.
func listenQuiet(row js.Value) bool { return row.Call("hasAttribute", "data-chatlisten-quiet").Bool() }

// listenSilenceStatus makes the status line stop announcing. A screen reader
// that is reading the page would otherwise talk over the audio each time the
// line changes.
func listenSilenceStatus(row js.Value) {
	if status := row.Call("querySelector", "[data-chatlisten-status]"); status.Truthy() {
		status.Call("setAttribute", "aria-live", "off")
	}
}

// stopListen ends any reading, removes its player row and gives its memory
// back. Nothing stays under a message once reading ends.
func (b *chatvoiceBrowser) stopListen() {
	if b.listenAudio.Truthy() {
		b.listenAudio.Call("pause")
		b.listenAudio.Set("src", "")
		b.listenAudio = js.Undefined()
	}
	if b.listenURL != "" {
		js.Global().Get("URL").Call("revokeObjectURL", b.listenURL)
		b.listenURL = ""
	}
	if b.listenRow.Truthy() {
		b.listenRow.Call("remove")
	}
	b.listenRow = js.Undefined()
}

// listenAction handles a press on a Listen action or on a control of its player
// row. Reading never starts by itself: only a press on "Listen" asks the server
// for speech. The click's detail tells a pointer press (it autoplays once the
// audio is ready) from a keyboard or assistive one (it waits for Play).
func (b *chatvoiceBrowser) listenAction(button, event js.Value) {
	locale := domAttribute(button, "data-chatlisten-locale")
	if locale == "" {
		locale = domAttribute(button.Call("closest", "[data-chatlisten-player]"), "data-chatlisten-locale")
	}
	switch domAttribute(button, "data-chatlisten") {
	case "start":
		detail := 0
		if event.Truthy() && event.Get("detail").Type() == js.TypeNumber {
			detail = event.Get("detail").Int()
		}
		go b.startListen(button, listenAutoplays(detail))
	case "pause":
		if !b.listenAudio.Truthy() {
			return
		}
		row := button.Call("closest", "[data-chatlisten-player]")
		if b.listenAudio.Get("paused").Bool() {
			if row.Truthy() && listenQuiet(row) {
				listenSilenceStatus(row)
				listenSay(row, locale, "playing")
			}
			b.listenAudio.Call("play")
			button.Set("textContent", chatui.ListenCopy(locale, "pause"))
		} else {
			b.listenAudio.Call("pause")
			button.Set("textContent", chatui.ListenCopy(locale, "resume"))
		}
	case "close":
		b.stopListen()
	}
}

// listenHostFor is where the player row goes: under the message being read, or
// in the block under the thread header for a thread.
func (b *chatvoiceBrowser) listenHostFor(thread, post string) js.Value {
	doc := js.Global().Get("document")
	if thread != "" {
		return doc.Call("querySelector", `[data-chatlisten-host="thread"]`)
	}
	message := doc.Call("querySelector", `[data-message-id="`+js.Global().Get("CSS").Call("escape", post).String()+`"]`)
	if !message.Truthy() {
		return js.Undefined()
	}
	if host := message.Call("querySelector", ".message-content"); host.Truthy() {
		return host
	}
	return message
}

// listenRowFor builds the player row under the message, or under the thread
// header, being read.
func (b *chatvoiceBrowser) listenRowFor(thread, post, locale string, quiet bool) js.Value {
	host := b.listenHostFor(thread, post)
	if !host.Truthy() {
		return js.Undefined()
	}
	key := post
	if thread != "" {
		key = thread
	}
	markup, err := ui.RenderToString(chatui.RenderListenPlayer(chatui.ListenPlayerProps{Locale: locale, PostID: key}))
	if err != nil {
		return js.Undefined()
	}
	holder := js.Global().Get("document").Call("createElement", "div")
	holder.Set("innerHTML", markup)
	row := holder.Get("firstElementChild")
	if quiet {
		row.Call("setAttribute", "data-chatlisten-quiet", "")
	}
	host.Call("appendChild", row)
	return row
}

// listenThreadNames is each author's name as the open thread shows it, for the
// server to say before that author's words.
func listenThreadNames() map[string]string {
	m := chatBrowser.snapshot()
	names := map[string]string{}
	add := func(msg chatui.Message) {
		if msg.AuthorID != "" && strings.TrimSpace(msg.Author) != "" {
			names[msg.AuthorID] = msg.Author
		}
	}
	if m.ThreadParent != nil {
		add(*m.ThreadParent)
	}
	for _, msg := range m.Messages {
		if msg.ID == m.ThreadParentID {
			add(msg)
		}
	}
	for _, msg := range m.ThreadMessages {
		add(msg)
	}
	return names
}

func (b *chatvoiceBrowser) startListen(button js.Value, autoplay bool) {
	b.stopListen()
	locale := domAttribute(button, "data-chatlisten-locale")
	input := voiceSpeakInput{TenantID: domAttribute(button, "data-chatlisten-tenant"), ConversationID: domAttribute(button, "data-chatlisten-conversation"), PostID: domAttribute(button, "data-chatlisten-post"), ThreadID: domAttribute(button, "data-chatlisten-thread")}
	if input.TenantID == "" {
		input.TenantID = b.config.Tenant
	}
	if input.ThreadID != "" {
		input.Names = listenThreadNames()
	}
	row := b.listenRowFor(input.ThreadID, input.PostID, locale, !autoplay)
	if !row.Truthy() {
		return
	}
	b.listenRow = row
	listenSay(row, locale, "loading")
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
	speech, err := voiceSpeakTimed(ctx, http.DefaultClient, b.config, input)
	if b.disposed || !row.Get("isConnected").Bool() || !b.listenRow.Equal(row) {
		return
	}
	if err != nil {
		listenSay(row, locale, listenStatusKey(err))
		return
	}
	if !strings.HasPrefix(strings.ToLower(speech.ContentType), "audio/") {
		listenSay(row, locale, "failed")
		return
	}
	array := js.Global().Get("Uint8Array").New(len(speech.Audio))
	js.CopyBytesToJS(array, speech.Audio)
	blob := js.Global().Get("Blob").New([]any{array}, map[string]any{"type": strings.Split(speech.ContentType, ";")[0]})
	raw := js.Global().Get("URL").Call("createObjectURL", blob).String()
	audio := js.Global().Get("Audio").New(raw)
	audio.Set("playbackRate", b.playback.Speed)
	b.listenAudio, b.listenURL = audio, raw
	if speed := row.Call("querySelector", "[data-chatlisten-speed]"); speed.Truthy() {
		speed.Set("value", fmt.Sprint(b.playback.Speed))
	}
	ended := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		if b.listenRow.Equal(row) {
			b.stopListen()
		}
		return nil
	})
	timings := speech.Timings
	progress := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		duration := audio.Get("duration").Float()
		if bar := row.Call("querySelector", "[data-chatlisten-progress]"); bar.Truthy() && duration > 0 {
			bar.Set("value", audio.Get("currentTime").Float()/duration)
		}
		listenMarkSentence(row, timings, int64(audio.Get("currentTime").Float()*1000))
		return nil
	})
	b.callbacks = append(b.callbacks, ended, progress)
	audio.Call("addEventListener", "ended", ended)
	audio.Call("addEventListener", "timeupdate", progress)
	listenPlaying(row, true)
	if !autoplay {
		// Likely a screen reader: say once that the audio is ready, and start
		// nothing. The Play button is the way in.
		listenSay(row, locale, "ready")
		if pause := row.Call("querySelector", `[data-chatlisten="pause"]`); pause.Truthy() {
			pause.Set("textContent", chatui.ListenCopy(locale, "play"))
		}
		return
	}
	listenSay(row, locale, "playing")
	audio.Call("play")
}

// listenMarkSentence marks the sentence being read in the player row. Without
// timings, or between two sentences, nothing is marked.
func listenMarkSentence(row js.Value, timings []chatui.ListenTiming, positionMS int64) {
	mark := row.Call("querySelector", "[data-chatlisten-sentence]")
	if !mark.Truthy() || len(timings) == 0 {
		return
	}
	index := chatui.ListenSentenceAt(timings, positionMS)
	if index < 0 {
		mark.Set("hidden", true)
		return
	}
	if mark.Get("textContent").String() != timings[index].Text {
		mark.Set("textContent", timings[index].Text)
	}
	mark.Set("hidden", false)
}

// listenSpeed follows the speed choice on a reading control.
func (b *chatvoiceBrowser) listenSpeed(target js.Value) {
	var speed float64
	_, _ = fmt.Sscan(target.Get("value").String(), &speed)
	if b.playback.SetSpeed(speed) == nil {
		b.savePreferences()
		if b.listenAudio.Truthy() {
			b.listenAudio.Set("playbackRate", speed)
		}
	}
}
