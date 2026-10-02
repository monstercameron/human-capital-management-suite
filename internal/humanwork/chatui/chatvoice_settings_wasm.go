//go:build js && wasm

package chatui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"syscall/js"
	"time"
)

var errVoiceSettingsInput = errors.New("chatui: voice settings need a signed-in page")

// voiceSettingsCall asks the voice settings routes with the same admitted
// session credential as Chat. It answers the status so a row can tell a refusal
// from a failure, and returns a cancel for the effect.
func voiceSettingsCall(action string, body any, done func(VoiceSettingsData, int, error)) func() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		data, status, err := voiceSettingsRequest(ctx, action, body)
		if ctx.Err() == nil {
			done(data, status, err)
		}
	}()
	return cancel
}

// voiceSettingsRequest asks once and, when the server refuses the page's
// credential (a tab that outlived its session or the server's restart), gets a
// fresh one and asks once more (CHATBUG-087).
func voiceSettingsRequest(ctx context.Context, action string, body any) (VoiceSettingsData, int, error) {
	data, status, err := voiceSettingsAttempt(ctx, action, body)
	if status != http.StatusUnauthorized && !(status == http.StatusForbidden && action == "switch") {
		return data, status, err
	}
	if _, refreshErr := chatSessionRefresh(ctx); refreshErr != nil {
		return data, status, err
	}
	return voiceSettingsAttempt(ctx, action, body)
}

func voiceSettingsAttempt(ctx context.Context, action string, body any) (VoiceSettingsData, int, error) {
	var data VoiceSettingsData
	origin, err := url.Parse(js.Global().Get("location").Get("origin").String())
	if err != nil || origin.Host == "" || (origin.Scheme != "http" && origin.Scheme != "https") || origin.User != nil {
		return data, 0, errVoiceSettingsInput
	}
	bearer := ""
	if island := js.Global().Get("document").Call("getElementById", "journey-config"); island.Truthy() {
		var session struct {
			Bearer string `json:"bearer"`
		}
		if json.Unmarshal([]byte(island.Get("textContent").String()), &session) == nil {
			bearer = session.Bearer
		}
	}
	if bearer == "" {
		return data, 0, errVoiceSettingsInput
	}
	switch action {
	case "settings", "policy", "switch":
	default:
		return data, 0, errVoiceSettingsInput
	}
	origin.Path, origin.RawQuery, origin.Fragment = "/api/chat/voice/"+action, "", ""
	encoded, err := json.Marshal(body)
	if err != nil {
		return data, 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, origin.String(), bytes.NewReader(encoded))
	if err != nil {
		return data, 0, err
	}
	request.Header.Set("Authorization", "Bearer "+bearer)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return data, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return data, response.StatusCode, errors.New("chatui: voice settings answered " + strconv.Itoa(response.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return data, response.StatusCode, err
	}
	if err = json.Unmarshal(raw, &data); err != nil {
		return data, response.StatusCode, err
	}
	return data, response.StatusCode, nil
}

// voiceSettingsChangedEvent tells the rest of the page that a voice switch was
// saved, so the composer asks the server again where voice is allowed.
const voiceSettingsChangedEvent = "chat-voice-switches-changed"

func voiceSettingsNotify() {
	document := js.Global().Get("document")
	if document.Truthy() {
		document.Call("dispatchEvent", js.Global().Get("Event").New(voiceSettingsChangedEvent))
	}
}

// voiceSettingsReset puts a switch back to the position the server holds. A
// re-render with an unchanged answer leaves the control where the person moved it.
func voiceSettingsReset(id string, checked bool) {
	if node := js.Global().Get("document").Call("getElementById", id); node.Truthy() {
		node.Set("checked", checked)
	}
}
