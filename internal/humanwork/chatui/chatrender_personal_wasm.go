//go:build js && wasm

package chatui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// renderingChecksAvailability: the row waits for the settings service to answer
// once before it is drawn.
const renderingChecksAvailability = true

// renderingSession remembers that the server answered "not available" (a typed
// answer), and only that: a failed read is never recorded here.
var renderingSession = &ReadingSession{}

func renderingSettingsClient() ReadingSettingsClient {
	client := ReadingSettingsClient{Origin: js.Global().Get("location").Get("origin").String(), HTTP: &http.Client{Timeout: 5 * time.Second}, Session: renderingSession, Refresh: chatSessionRefresh}
	client.Locale = js.Global().Get("document").Get("documentElement").Get("lang").String()
	island := js.Global().Get("document").Call("getElementById", "journey-config")
	if island.Truthy() {
		var session struct {
			Bearer string `json:"bearer"`
		}
		if json.Unmarshal([]byte(island.Get("textContent").String()), &session) == nil {
			client.Bearer = session.Bearer
		}
	}
	return client
}
func renderingLoadSettings(room string, done func(chatrender.Preference, error)) func() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// CHATUX-012: a read that did not get an answer (the server restarting) is
		// repeated, 1 s, 2 s, 5 s and then every 15 s, until it gets one. Only an
		// answer settles the row: the settings, or the typed "not available" the
		// session remembers, or a refusal of the settings themselves.
		for failures := 0; ; failures++ {
			pref, err := renderingSettingsClient().Load(ctx, room)
			if ctx.Err() != nil {
				return
			}
			if err == nil || renderingSession.Off() || errors.Is(err, chatrender.ErrInvalid) {
				done(pref, err)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(ReadBackoff(failures)):
			}
		}
	}()
	return cancel
}
func renderingLoadLanguages(room string, done func(map[string]int, error)) func() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		counts, err := renderingSettingsClient().LoadLanguages(ctx, room)
		if ctx.Err() == nil {
			done(counts, err)
		}
	}()
	return cancel
}
func renderingReadSettings(event ui.Event, previous chatrender.Preference, room string) (chatrender.Preference, string, error) {
	form := renderingEventForm(event)
	if !form.Truthy() {
		return previous, room, chatrender.ErrInvalid
	}
	pref := previous
	pref.ReadingLanguage = form.Call("querySelector", "[name=reading]").Get("value").String()
	pref.Translate = form.Call("querySelector", "[name=translate]").Get("checked").Bool()
	pref.FurtherLanguages = nil
	pref.SourceOverrides = map[string]bool{}
	for _, name := range []string{"further", "never"} {
		nodes := form.Call("querySelectorAll", "[name="+name+"]:checked")
		for i := 0; i < nodes.Length(); i++ {
			lang := nodes.Index(i).Get("value").String()
			if name == "further" {
				pref.FurtherLanguages = append(pref.FurtherLanguages, lang)
			} else {
				pref.SourceOverrides[lang] = false
			}
		}
	}
	choice := form.Call("querySelector", "[name=conversation]")
	if !choice.Truthy() || !choice.Get("checked").Bool() {
		room = ""
	}
	return pref, room, pref.Validate()
}

// renderingSaves keeps saves in the order they were pressed: a change made while
// the previous one is still on its way waits for it instead of racing it
// (CHATBUG-087).
var renderingSaves sync.Mutex

func renderingSaveSettings(room string, pref chatrender.Preference, done func(error)) {
	go func() {
		renderingSaves.Lock()
		defer renderingSaves.Unlock()
		err := renderingSettingsClient().Save(context.Background(), room, pref)
		if err == nil {
			ui.PostAsync(func() {
				js.Global().Get("document").Call("dispatchEvent", js.Global().Get("Event").New("chat-reading-settings-changed"))
			})
		}
		done(err)
	}()
}
