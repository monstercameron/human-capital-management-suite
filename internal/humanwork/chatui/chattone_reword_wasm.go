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
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// chattoneRewordPath is the writing-style service's path; the reword settings
// are beneath it.
const chattoneRewordPath = "/api/chat-writing-style"

var errChattoneRewordNotAvailable = errors.New("chatui: reword settings are not composed")

func chattoneRewordNotAvailable(err error) bool { return errors.Is(err, errChattoneRewordNotAvailable) }

func chattoneRewordBearer() string {
	island := js.Global().Get("document").Call("getElementById", "journey-config")
	if !island.Truthy() {
		return ""
	}
	var session struct {
		Bearer string `json:"bearer"`
	}
	if json.Unmarshal([]byte(island.Get("textContent").String()), &session) != nil {
		return ""
	}
	return session.Bearer
}

func chattoneRewordCall(ctx context.Context, method, path string, body any) (ChattoneRewordState, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return ChattoneRewordState{}, err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, js.Global().Get("location").Get("origin").String()+path, reader)
	if err != nil {
		return ChattoneRewordState{}, err
	}
	if bearer := chattoneRewordBearer(); bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := (&http.Client{Timeout: 8 * time.Second}).Do(request)
	if err != nil {
		return ChattoneRewordState{}, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil || response.StatusCode != http.StatusOK {
		return ChattoneRewordState{}, errors.New("chatui: reword settings answered with an error")
	}
	var typed struct {
		ChattoneRewordState
		Reason string `json:"reason"`
	}
	if json.Unmarshal(data, &typed) != nil {
		return ChattoneRewordState{}, errors.New("chatui: reword settings answer is not understood")
	}
	if !typed.Available {
		return ChattoneRewordState{}, errChattoneRewordNotAvailable
	}
	return typed.ChattoneRewordState, nil
}

func chattoneRewordLoad(conversation string, done func(ChattoneRewordState, error)) func() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		view, err := chattoneRewordCall(ctx, http.MethodGet, chattoneRewordPath+"/reword?conversation_id="+url.QueryEscape(conversation), nil)
		if ctx.Err() == nil {
			done(view, err)
		}
	}()
	return cancel
}

func chattoneRewordSend(path string, body map[string]any, done func(ChattoneRewordState, error)) {
	go func() {
		view, err := chattoneRewordCall(context.Background(), http.MethodPost, chattoneRewordPath+path, body)
		done(view, err)
	}()
}

func init() {
	// The reword row sits in Chat preferences, in the slot the Writing style row
	// reserved. It asks the server for the selected conversation, so it is drawn
	// only where the server composed the reword settings.
	chatbug045WritingStyleRow = func(m Model) ui.Node {
		if m.SelectedID == "" {
			return nil
		}
		return ui.CreateElement(chattoneRewordComponent, chattoneRewordProps{Locale: m.Locale, Conversation: m.SelectedID})
	}
}
