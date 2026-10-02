//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"syscall/js"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// One bridge owns recording resources and playback positions. Installation is
// explicit so the composition root can supply the authenticated session and
// dispose all streams/listeners on logout or navigation.
type chatvoiceBrowser struct {
	config                                         journeyclient.Config
	root, stream, recorder, analyser, audioContext js.Value
	activeAudio                                    js.Value
	callbacks                                      []js.Func
	chunks                                         []js.Value
	state                                          voiceRecordingSession
	playback                                       *voicePlayback
	previewURL, contentType, key                   string
	content                                        []byte
	timer                                          *time.Ticker
	done                                           chan struct{}
	generation                                     uint64
	sending                                        bool
	OnSent                                         func(chat.Post)
	loading                                        map[string]bool
	transcripts                                    map[string]chat.VoiceRecord
	audioURLs                                      map[string]string
	disposed                                       bool
	// Listen keeps one reading at a time: its audio, object address and player row.
	listenAudio, listenRow js.Value
	listenURL              string
}

func installChatVoice(cfg journeyclient.Config, onSent func(chat.Post)) func() {
	b := &chatvoiceBrowser{config: cfg, playback: newVoicePlayback(), OnSent: onSent, loading: map[string]bool{}, transcripts: map[string]chat.VoiceRecord{}, audioURLs: map[string]string{}}
	b.loadPreferences()
	b.state.Recorder = b
	b.state.State = voiceIdle
	doc := js.Global().Get("document")
	click := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || b.disposed {
			return nil
		}
		target := args[0].Get("target")
		if !target.Truthy() || !target.Get("closest").Truthy() {
			return nil
		}
		if !target.Call("closest", "[data-chatvoice-recorder]").Truthy() {
			open := doc.Call("querySelector", "[data-chatvoice-action=toggle][aria-expanded=true]")
			if open.Truthy() {
				chatui.CloseVoiceComposer(open, false)
			}
		}
		if listen := target.Call("closest", "[data-chatlisten]"); listen.Truthy() {
			args[0].Call("preventDefault")
			b.listenAction(listen, args[0])
			return nil
		}
		button := target.Call("closest", "[data-chatvoice-action]")
		if button.Truthy() {
			root := button.Call("closest", "[data-chatvoice-recorder]")
			if root.Truthy() {
				args[0].Call("preventDefault")
				b.root = root
				if button.Call("getAttribute", "data-chatvoice-action").String() == "toggle" {
					chatui.ToggleVoiceComposer(button)
				} else {
					b.action(button.Call("getAttribute", "data-chatvoice-action").String())
				}
			}
			player := button.Call("closest", "[data-chatvoice-player]")
			if player.Truthy() {
				args[0].Call("preventDefault")
				b.playerAction(player, button.Call("getAttribute", "data-chatvoice-action").String())
			}
		}
		seek := target.Call("closest", "[data-chatvoice-seek]")
		if seek.Truthy() {
			player := seek.Call("closest", "[data-chatvoice-player]")
			audio := player.Call("querySelector", "audio")
			if audio.Truthy() {
				var ms float64
				_, _ = fmt.Sscan(seek.Call("getAttribute", "data-chatvoice-seek").String(), &ms)
				audio.Set("currentTime", ms/1000)
			}
		}
		return nil
	})
	change := js.FuncOf(func(_ js.Value, args []js.Value) any {
		target := args[0].Get("target")
		if target.Call("hasAttribute", "data-chatlisten-speed").Bool() {
			b.listenSpeed(target)
			return nil
		}
		if target.Call("hasAttribute", "data-chatvoice-collapsed").Bool() {
			b.playback.Collapsed = target.Get("checked").Bool()
			b.savePreferences()
			details := target.Call("closest", "[data-chatvoice-player]").Call("querySelector", "[data-chatvoice-expand]")
			if details.Truthy() {
				chatui.SetChatDisclosureOpen(details, !b.playback.Collapsed)
			}
			return nil
		}
		if !target.Call("hasAttribute", "data-chatvoice-speed").Bool() {
			return nil
		}
		var speed float64
		_, _ = fmt.Sscan(target.Get("value").String(), &speed)
		if b.playback.SetSpeed(speed) == nil {
			b.savePreferences()
			audio := target.Call("closest", "[data-chatvoice-player]").Call("querySelector", "audio")
			audio.Set("playbackRate", speed)
		}
		return nil
	})
	play := js.FuncOf(func(_ js.Value, args []js.Value) any {
		audio := args[0].Get("target")
		if !audio.Get("getAttribute").Truthy() {
			return nil
		}
		id := audio.Call("getAttribute", "data-chatvoice-audio")
		if !id.Truthy() {
			return nil
		}
		root := audio.Call("closest", "[data-chatvoice-player]")
		if root.Truthy() {
			approved := audio.Call("getAttribute", "data-chatvoice-play-approved")
			if !approved.Truthy() {
				audio.Call("pause")
				go b.playProtected(root)
				return nil
			}
			audio.Call("removeAttribute", "data-chatvoice-play-approved")
		}
		previous := b.playback.Play(id.String())
		if b.activeAudio.Truthy() && !b.activeAudio.Equal(audio) {
			b.activeAudio.Call("pause")
		}
		b.activeAudio = audio
		if previous != "" && previous != id.String() {
			all := doc.Call("querySelectorAll", "audio[data-chatvoice-audio]")
			for i := 0; i < all.Length(); i++ {
				other := all.Index(i)
				if other.Call("getAttribute", "data-chatvoice-audio").String() == previous {
					other.Call("pause")
				}
			}
		}
		audio.Set("playbackRate", b.playback.Speed)
		if audio.Get("currentTime").Float() == 0 {
			audio.Set("currentTime", b.playback.Positions[id.String()])
		}
		return nil
	})
	position := js.FuncOf(func(_ js.Value, args []js.Value) any {
		audio := args[0].Get("target")
		if !audio.Get("getAttribute").Truthy() {
			return nil
		}
		id := audio.Call("getAttribute", "data-chatvoice-audio")
		if !id.Truthy() {
			return nil
		}
		duration := audio.Get("duration").Float()
		current := audio.Get("currentTime").Float()
		_ = b.playback.Seek(id.String(), current, duration)
		root := audio.Call("closest", "[data-chatvoice-player]")
		if !root.Truthy() {
			return nil
		}
		button := root.Call("querySelector", "[data-chatvoice-action='play-toggle']")
		if button.Truthy() {
			key := "play"
			if !audio.Get("paused").Bool() {
				key = "pauseplay"
			}
			button.Set("textContent", chatui.VoiceCopy(root.Call("getAttribute", "data-chatvoice-locale").String(), key))
		}
		clock := root.Call("querySelector", "[data-chatvoice-time]")
		if clock.Truthy() && !math.IsNaN(duration) {
			clock.Set("textContent", voiceBrowserTime(int64(current*1000))+" / "+voiceBrowserTime(int64(duration*1000)))
		}
		segments := root.Call("querySelectorAll", "[data-chatvoice-seek]")
		for i := 0; i < segments.Length(); i++ {
			segment := segments.Index(i)
			var start float64
			_, _ = fmt.Sscan(segment.Call("getAttribute", "data-chatvoice-seek").String(), &start)
			end := duration * 1000
			_, _ = fmt.Sscan(segment.Call("getAttribute", "data-chatvoice-end").String(), &end)
			segment.Call("setAttribute", "aria-current", fmt.Sprint(voiceSegmentActive(current*1000, start, end, !audio.Get("paused").Bool())))
		}
		return nil
	})
	key := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].Get("key").String() == "Escape" {
			open := doc.Call("querySelector", "[data-chatvoice-action=toggle][aria-expanded=true]")
			if open.Truthy() {
				args[0].Call("preventDefault")
				chatui.ToggleVoiceComposer(open)
			}
		}
		return nil
	})
	b.callbacks = []js.Func{click, change, play, position, key}
	doc.Call("addEventListener", "keydown", key)
	doc.Call("addEventListener", "click", click)
	doc.Call("addEventListener", "change", change)
	doc.Call("addEventListener", "play", play, true)
	doc.Call("addEventListener", "timeupdate", position, true)
	doc.Call("addEventListener", "pause", position, true)
	scan := func() {
		players := doc.Call("querySelectorAll", "[data-chatvoice-player]")
		for i := 0; i < players.Length(); i++ {
			root := players.Index(i)
			if !root.Call("hasAttribute", "data-chatvoice-hydrated").Bool() {
				root.Call("setAttribute", "data-chatvoice-hydrated", "true")
				b.hydratePlayer(root)
			}
		}
	}
	observerCallback := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		scan()
		if b.activeAudio.Truthy() && !b.activeAudio.Get("isConnected").Bool() {
			b.activeAudio.Call("pause")
			b.playback.Pause(b.playback.Active)
		}
		if b.root.Truthy() && !b.root.Get("isConnected").Bool() {
			b.state.Cancel()
		}
		if b.listenRow.Truthy() && !b.listenRow.Get("isConnected").Bool() {
			b.stopListen()
		}
		return nil
	})
	observer := js.Global().Get("MutationObserver").New(observerCallback)
	observer.Call("observe", doc.Get("body"), map[string]any{"childList": true, "subtree": true})
	b.callbacks = append(b.callbacks, observerCallback)
	scan()
	return func() {
		b.disposed = true
		observer.Call("disconnect")
		b.stopListen()
		b.Cancel()
		if b.activeAudio.Truthy() {
			b.activeAudio.Call("pause")
			b.activeAudio.Call("removeAttribute", "src")
		}
		for _, raw := range b.audioURLs {
			js.Global().Get("URL").Call("revokeObjectURL", raw)
		}
		b.audioURLs = map[string]string{}
		doc.Call("removeEventListener", "keydown", key)
		doc.Call("removeEventListener", "click", click)
		doc.Call("removeEventListener", "change", change)
		doc.Call("removeEventListener", "play", play, true)
		doc.Call("removeEventListener", "timeupdate", position, true)
		doc.Call("removeEventListener", "pause", position, true)
		for _, f := range b.callbacks {
			f.Release()
		}
	}
}
func voiceBrowserTime(ms int64) string { return chatui.VoiceClock(voicePageLocale(), ms) }

// voicePageLocale is the locale the voice controls on the page were drawn for,
// so the running clock is written in the same numerals as the clock it replaces.
func voicePageLocale() string {
	if node := js.Global().Get("document").Call("querySelector", "[data-chatvoice-locale]"); node.Truthy() {
		return node.Call("getAttribute", "data-chatvoice-locale").String()
	}
	return ""
}
func (b *chatvoiceBrowser) copy(key string) string {
	return chatui.VoiceCopy(b.root.Call("getAttribute", "data-chatvoice-locale").String(), key)
}
func (b *chatvoiceBrowser) status(key string) {
	if b.root.Truthy() {
		node := b.root.Call("querySelector", "[data-chatvoice-status]")
		if node.Truthy() {
			node.Set("textContent", b.copy(key))
		}
	}
}
func (b *chatvoiceBrowser) controls() {
	if !b.root.Truthy() {
		return
	}
	nodes := b.root.Call("querySelectorAll", "[data-chatvoice-action]")
	for i := 0; i < nodes.Length(); i++ {
		node := nodes.Index(i)
		action := node.Call("getAttribute", "data-chatvoice-action").String()
		enabled := false
		switch action {
		case "start", "again":
			enabled = b.state.State != voiceRequesting && b.state.State != voiceRecording && b.state.State != voicePaused && !b.sending
		case "pause", "stop":
			enabled = b.state.State == voiceRecording || b.state.State == voicePaused
		case "discard":
			enabled = b.state.State != voiceIdle && !b.sending
		case "send":
			enabled = b.state.State == voicePreview && len(b.content) > 0 && !b.sending
		}
		node.Set("disabled", !enabled)
		if action == "pause" {
			key := "pause"
			if b.state.State == voicePaused {
				key = "resume"
			}
			node.Set("textContent", b.copy(key))
		}
	}
}
func (b *chatvoiceBrowser) action(action string) {
	switch action {
	case "start", "again":
		b.Cancel()
		b.state.State = voiceExplained
		_ = b.state.Start()
	case "pause":
		if b.state.Pause() == nil {
			key := "recording"
			if b.state.State == voicePaused {
				key = "paused"
			}
			b.status(key)
		}
	case "stop":
		_ = b.state.Stop()
	case "discard":
		b.state.Cancel()
		b.status("explain")
	case "send":
		go b.send()
	}
	b.controls()
}
func (b *chatvoiceBrowser) Start() error {
	media := js.Global().Get("navigator").Get("mediaDevices")
	constructor := js.Global().Get("MediaRecorder")
	if !media.Truthy() || !constructor.Truthy() {
		b.status("missing")
		return chat.ErrVoiceUnavailable
	}
	b.contentType = ""
	for _, kind := range []string{"audio/webm;codecs=opus", "audio/ogg;codecs=opus"} {
		if constructor.Call("isTypeSupported", kind).Bool() {
			b.contentType = kind
			break
		}
	}
	if b.contentType == "" {
		b.status("unavailable")
		return chat.ErrVoiceUnavailable
	}
	b.status("requesting")
	b.generation++
	generation := b.generation
	go func() {
		// Await with an explicit rejection callback, preserving permission versus
		// missing-device diagnostics instead of silently swallowing rejection.
		type permission struct {
			stream js.Value
			reason string
		}
		result := make(chan permission, 1)
		resolve := js.FuncOf(func(_ js.Value, a []js.Value) any { result <- permission{stream: a[0]}; return nil })
		reject := js.FuncOf(func(_ js.Value, a []js.Value) any {
			reason := "failed"
			if len(a) > 0 {
				switch a[0].Get("name").String() {
				case "NotAllowedError", "SecurityError":
					reason = "denied"
				case "NotFoundError", "NotReadableError":
					reason = "missing"
				}
			}
			result <- permission{reason: reason}
			return nil
		})
		media.Call("getUserMedia", map[string]any{"audio": true, "video": false}).Call("then", resolve).Call("catch", reject)
		p := <-result
		resolve.Release()
		reject.Release()
		if generation != b.generation {
			if p.stream.Truthy() {
				voiceStopTracks(p.stream)
			}
			return
		}
		if p.reason != "" {
			b.state.State = voiceFailed
			if p.reason == "denied" {
				b.state.State = voiceDenied
			}
			if p.reason == "missing" {
				b.state.State = voiceNoMicrophone
			}
			b.status(p.reason)
			b.controls()
			return
		}
		b.stream = p.stream
		b.chunks = nil
		b.recorder = constructor.New(p.stream, map[string]any{"mimeType": b.contentType, "audioBitsPerSecond": 32000})
		data := js.FuncOf(func(_ js.Value, a []js.Value) any {
			if generation == b.generation && a[0].Get("data").Get("size").Int() > 0 {
				b.chunks = append(b.chunks, a[0].Get("data"))
			}
			return nil
		})
		stop := js.FuncOf(func(_ js.Value, _ []js.Value) any {
			if generation == b.generation {
				go b.preview(generation)
			}
			return nil
		})
		errorCallback := js.FuncOf(func(_ js.Value, _ []js.Value) any {
			b.Cancel()
			b.state.State = voiceFailed
			b.status("failed")
			b.controls()
			return nil
		})
		b.callbacks = append(b.callbacks, data, stop, errorCallback)
		b.recorder.Set("ondataavailable", data)
		b.recorder.Set("onstop", stop)
		b.recorder.Set("onerror", errorCallback)
		contextConstructor := js.Global().Get("AudioContext")
		if contextConstructor.Truthy() {
			b.audioContext = contextConstructor.New()
			b.analyser = b.audioContext.Call("createAnalyser")
			b.analyser.Set("fftSize", 256)
			b.audioContext.Call("createMediaStreamSource", p.stream).Call("connect", b.analyser)
		}
		b.recorder.Call("start", 1000)
		if b.state.Ready() != nil {
			b.Cancel()
			return
		}
		b.status("recording")
		b.controls()
		b.timer = time.NewTicker(time.Second)
		b.done = make(chan struct{})
		ticker, done := b.timer, b.done
		last := time.Now()
		for {
			select {
			case now := <-ticker.C:
				delta := now.Sub(last).Milliseconds()
				last = now
				level := 0.0
				if b.analyser.Truthy() {
					samples := js.Global().Get("Uint8Array").New(256)
					b.analyser.Call("getByteTimeDomainData", samples)
					sum := 0.0
					for i := 0; i < 256; i++ {
						v := (float64(samples.Index(i).Int()) - 128) / 128
						sum += v * v
					}
					level = math.Min(1, math.Sqrt(sum/256))
				}
				_ = b.state.Tick(delta, level)
				if b.root.Truthy() {
					b.root.Call("querySelector", "[data-chatvoice-meter]").Set("value", level)
					b.root.Call("querySelector", "[data-chatvoice-clock]").Set("textContent", voiceBrowserTime(b.state.ElapsedMS)+" / "+voiceBrowserTime(b.state.RemainingMS()))
				}
				b.controls()
			case <-done:
				return
			}
		}
	}()
	return nil
}
func (b *chatvoiceBrowser) Pause() error {
	if !b.recorder.Truthy() {
		return errVoiceRecorderState
	}
	b.recorder.Call("pause")
	return nil
}
func (b *chatvoiceBrowser) Resume() error {
	if !b.recorder.Truthy() {
		return errVoiceRecorderState
	}
	b.recorder.Call("resume")
	return nil
}
func (b *chatvoiceBrowser) Stop() error {
	if !b.recorder.Truthy() {
		return errVoiceRecorderState
	}
	b.recorder.Call("stop")
	b.releaseCapture()
	return nil
}
func voiceStopTracks(stream js.Value) {
	tracks := stream.Call("getTracks")
	for i := 0; i < tracks.Length(); i++ {
		tracks.Index(i).Call("stop")
	}
}
func (b *chatvoiceBrowser) releaseCapture() {
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	if b.done != nil {
		close(b.done)
		b.done = nil
	}
	if b.stream.Truthy() {
		voiceStopTracks(b.stream)
		b.stream = js.Undefined()
	}
	if b.audioContext.Truthy() {
		b.audioContext.Call("close")
		b.audioContext = js.Undefined()
		b.analyser = js.Undefined()
	}
}
func (b *chatvoiceBrowser) Cancel() {
	b.generation++
	if b.recorder.Truthy() && b.recorder.Get("state").String() != "inactive" {
		b.recorder.Call("stop")
	}
	b.releaseCapture()
	b.recorder = js.Undefined()
	b.content = nil
	b.chunks = nil
	b.key = ""
	if b.previewURL != "" {
		js.Global().Get("URL").Call("revokeObjectURL", b.previewURL)
		b.previewURL = ""
	}
	if b.root.Truthy() {
		audio := b.root.Call("querySelector", "[data-chatvoice-preview]")
		if audio.Truthy() {
			audio.Call("pause")
			audio.Call("removeAttribute", "src")
			audio.Call("load")
		}
	}
}
func (b *chatvoiceBrowser) preview(generation uint64) {
	parts := make([]any, len(b.chunks))
	for i, p := range b.chunks {
		parts[i] = p
	}
	blob := js.Global().Get("Blob").New(parts, map[string]any{"type": b.contentType})
	if blob.Get("size").Int() > chat.VoiceMaxBytes {
		b.Cancel()
		b.state.State = voiceFailed
		b.status("failed")
		b.controls()
		return
	}
	buffer := awaitChatJS(blob.Call("arrayBuffer"))
	if generation != b.generation || !buffer.Truthy() {
		return
	}
	content := make([]byte, buffer.Get("byteLength").Int())
	js.CopyBytesToGo(content, js.Global().Get("Uint8Array").New(buffer))
	b.content = content
	b.previewURL = js.Global().Get("URL").Call("createObjectURL", blob).String()
	b.root.Call("querySelector", "[data-chatvoice-preview]").Set("src", b.previewURL)
	b.key = fmt.Sprintf("voice-%d", time.Now().UnixNano())
	b.state.State = voicePreview
	b.status("listen")
	b.controls()
}
func (b *chatvoiceBrowser) send() {
	if b.sending || b.state.State != voicePreview || len(b.content) == 0 {
		return
	}
	b.sending = true
	b.status("sending")
	b.controls()
	defer func() { b.sending = false; b.controls() }()
	input := voiceSendInput{TenantID: b.root.Call("getAttribute", "data-chatvoice-tenant").String(), ConversationID: b.root.Call("getAttribute", "data-chatvoice-conversation").String(), IdempotencyKey: b.key, ContentType: b.contentType, Content: b.content, Waveform: append([]float64(nil), b.state.Waveform...), Note: b.root.Call("querySelector", "[data-chatvoice-note]").Get("value").String()}
	input.Locale = b.root.Call("getAttribute", "data-chatvoice-locale").String()
	var result voiceSendReply
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := voiceRequest(ctx, http.DefaultClient, b.config, "send", input, &result); err != nil {
		b.status("sendfailed")
		return
	}
	b.state.Cancel()
	b.status("sent")
	if b.OnSent != nil {
		b.OnSent(result.Post)
	}
}
