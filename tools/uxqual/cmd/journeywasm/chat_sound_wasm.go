//go:build js && wasm

package main

import (
	"strconv"
	"syscall/js"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

const chatSoundStateProperty = "__hcmChatSoundState"

// installChatSoundUnlock registers gesture listeners. The AudioContext is
// attached to the document for this page's lifetime; browsers discard it when
// the page is unloaded, and no process-wide Go audio state is retained.
func installChatSoundUnlock() {
	defer func() { _ = recover() }()
	state, document, ok := chatSoundDocumentState()
	if !ok || state.Get("installed").Bool() {
		return
	}
	state.Set("installed", true)
	listeners := js.Global().Get("Array").New()
	state.Set("listeners", listeners)
	add := func(event string, once bool, callback func(js.Value, []js.Value) any) {
		fn := js.FuncOf(callback)
		options := map[string]any{"passive": true}
		if once {
			options["once"] = true
		}
		document.Call("addEventListener", event, fn, js.ValueOf(options))
		entry := js.Global().Get("Object").New()
		entry.Set("event", event)
		entry.Set("fn", fn)
		listeners.Call("push", entry)
	}
	unlock := func(js.Value, []js.Value) any {
		unlockChatSoundFromGesture(state, document)
		return nil
	}
	add("pointerdown", true, unlock)
	add("keydown", true, unlock)
	// Keep the page-scoped context alive for the document lifetime. The browser
	// closes it on unload; unsupported lifecycle APIs are allowed to fail quiet.
	add("pagehide", true, func(js.Value, []js.Value) any {
		closeChatSoundContext(state)
		return nil
	})
}

func chatSoundDocumentState() (state, document js.Value, ok bool) {
	document = js.Global().Get("document")
	if document.IsUndefined() || document.IsNull() {
		return js.Undefined(), document, false
	}
	root := document.Get("documentElement")
	if root.IsUndefined() || root.IsNull() {
		return js.Undefined(), document, false
	}
	state = root.Get(chatSoundStateProperty)
	if state.IsUndefined() || state.IsNull() {
		state = js.Global().Get("Object").New()
		state.Set("context", js.Null())
		state.Set("installed", false)
		state.Set("lastPlayed", 0)
		state.Set("sequences", js.Global().Get("Object").New())
		state.Set("ready", js.Global().Get("Object").New())
		root.Set(chatSoundStateProperty, state)
	}
	return state, document, true
}

func chatSoundSetBaseline(conversationID string, sequence uint64) {
	chatSoundStoreSequence(conversationID, sequence, true)
}

func noteChatSoundSequence(conversationID string, sequence uint64) {
	chatSoundStoreSequence(conversationID, sequence, true)
}

func chatSoundStoreSequence(conversationID string, sequence uint64, ready bool) {
	defer func() { _ = recover() }()
	state, _, ok := chatSoundDocumentState()
	if !ok || conversationID == "" {
		return
	}
	sequences := state.Get("sequences")
	if sequences.IsUndefined() || sequences.IsNull() {
		sequences = js.Global().Get("Object").New()
		state.Set("sequences", sequences)
	}
	current := sequences.Get(conversationID)
	currentSequence, err := strconv.ParseUint(current.String(), 10, 64)
	if current.Type() != js.TypeString || err != nil || sequence > currentSequence {
		sequences.Set(conversationID, strconv.FormatUint(sequence, 10))
	}
	if ready {
		state.Get("ready").Set(conversationID, true)
	}
}

func chatSoundHasBaseline(conversationID string) bool {
	defer func() { _ = recover() }()
	state, _, ok := chatSoundDocumentState()
	if !ok || conversationID == "" {
		return false
	}
	ready := state.Get("ready")
	if ready.IsUndefined() || ready.IsNull() {
		return false
	}
	value := ready.Get(conversationID)
	return value.Type() == js.TypeBoolean && value.Bool()
}

func chatSoundCurrentSequence(conversationID string) uint64 {
	defer func() { _ = recover() }()
	state, _, ok := chatSoundDocumentState()
	if !ok || conversationID == "" {
		return 0
	}
	sequences := state.Get("sequences")
	if sequences.IsUndefined() || sequences.IsNull() {
		return 0
	}
	value := sequences.Get(conversationID)
	if value.Type() != js.TypeString {
		return 0
	}
	sequence, err := strconv.ParseUint(value.String(), 10, 64)
	if err != nil {
		return 0
	}
	return sequence
}

// claimChatSoundSequence atomically advances a room watermark so the selected
// live stream and inactive-room poll cannot sound for the same post.
func claimChatSoundSequence(conversationID string, sequence uint64) bool {
	if sequence == 0 || sequence <= chatSoundCurrentSequence(conversationID) {
		return false
	}
	chatSoundStoreSequence(conversationID, sequence, true)
	return true
}

func unlockChatSoundFromGesture(state, document js.Value) {
	defer func() { _ = recover() }()
	context := state.Get("context")
	if context.IsUndefined() || context.IsNull() {
		constructor := js.Global().Get("AudioContext")
		if constructor.IsUndefined() || constructor.IsNull() {
			constructor = js.Global().Get("webkitAudioContext")
		}
		if constructor.IsUndefined() || constructor.IsNull() {
			return
		}
		context = constructor.New()
		state.Set("context", context)
	}
	if context.Get("state").String() == "suspended" {
		context.Call("resume")
	}
	removeChatSoundListeners(state, document, "pointerdown", "keydown")
}

func removeChatSoundListeners(state, document js.Value, events ...string) {
	listeners := state.Get("listeners")
	kept := js.Global().Get("Array").New()
	for i := 0; i < listeners.Length(); i++ {
		entry := listeners.Index(i)
		event := entry.Get("event").String()
		remove := false
		for _, candidate := range events {
			if event == candidate {
				remove = true
				break
			}
		}
		if remove {
			document.Call("removeEventListener", event, entry.Get("fn"))
			continue
		}
		kept.Call("push", entry)
	}
	state.Set("listeners", kept)
}

func closeChatSoundContext(state js.Value) {
	defer func() { _ = recover() }()
	context := state.Get("context")
	if !context.IsUndefined() && !context.IsNull() && context.Get("state").String() != "closed" {
		context.Call("close")
	}
	state.Set("context", js.Null())
	state.Set("installed", false)
}

func playChatSoundIfAllowed(model chatui.Model, conversationID string, post *chatv1.Post, now time.Time) {
	defer func() { _ = recover() }()
	if !shouldPlayChatSound(model, conversationID, post, now, chatSystemReducedInterruption(), chatAppReducedInterruption()) {
		return
	}
	state, _, ok := chatSoundDocumentState()
	if !ok {
		return
	}
	context := state.Get("context")
	if context.IsUndefined() || context.IsNull() || context.Get("state").String() != "running" {
		return
	}
	nowMS := float64(now.UnixMilli())
	if last := state.Get("lastPlayed").Float(); last > 0 && nowMS-last < 5000 {
		return
	}
	state.Set("lastPlayed", nowMS)
	nowAudio := context.Get("currentTime").Float()
	oscillator := context.Call("createOscillator")
	gain := context.Call("createGain")
	oscillator.Set("type", "sine")
	oscillator.Get("frequency").Call("setValueAtTime", 740, nowAudio)
	gain.Get("gain").Call("setValueAtTime", 0.035, nowAudio)
	gain.Get("gain").Call("exponentialRampToValueAtTime", 0.001, nowAudio+0.12)
	oscillator.Call("connect", gain)
	gain.Call("connect", context.Get("destination"))
	oscillator.Call("start", nowAudio)
	oscillator.Call("stop", nowAudio+0.125)
}

func chatSystemReducedInterruption() bool {
	defer func() { _ = recover() }()
	matchMedia := js.Global().Get("matchMedia")
	if matchMedia.IsUndefined() || matchMedia.IsNull() {
		return false
	}
	return matchMedia.Invoke("(prefers-reduced-motion: reduce)").Get("matches").Bool()
}

func chatAppReducedInterruption() bool {
	defer func() { _ = recover() }()
	document := js.Global().Get("document")
	root := document.Get("documentElement")
	preference := root.Call("getAttribute", "data-hcm-motion-preference").String()
	return preference == "reduce" || preference == "limited"
}
