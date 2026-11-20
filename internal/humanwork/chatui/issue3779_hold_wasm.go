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

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var errChatHoldInput = errors.New("chatui: legal holds need a signed-in page")

// chatHoldCall asks the hold service and answers the status, so the page can
// tell a refusal it should explain from a failure. It returns a cancel for the
// effect.
func chatHoldCall(action string, body any, done func(ChatHoldData, int, error)) func() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		var data ChatHoldData
		raw, status, err := chatHoldRequest(ctx, action, body)
		if err == nil && action == "list" {
			err = json.Unmarshal(raw, &data)
		}
		if ctx.Err() == nil {
			done(data, status, err)
		}
	}()
	return cancel
}

// chatHoldRequest asks once and, when the server refuses the page's credential
// (a tab that outlived its session), gets a fresh one and asks once more.
func chatHoldRequest(ctx context.Context, action string, body any) ([]byte, int, error) {
	raw, status, err := chatHoldAttempt(ctx, action, body)
	if status != http.StatusUnauthorized {
		return raw, status, err
	}
	if _, refreshErr := chatSessionRefresh(ctx); refreshErr != nil {
		return raw, status, err
	}
	return chatHoldAttempt(ctx, action, body)
}

func chatHoldAttempt(ctx context.Context, action string, body any) ([]byte, int, error) {
	origin, err := url.Parse(js.Global().Get("location").Get("origin").String())
	if err != nil || origin.Host == "" || (origin.Scheme != "http" && origin.Scheme != "https") || origin.User != nil {
		return nil, 0, errChatHoldInput
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
	if bearer == "" || (action != "list" && action != "place" && action != "release") {
		return nil, 0, errChatHoldInput
	}
	origin.Path, origin.RawQuery, origin.Fragment = "/api/chat/holds/v1/"+action, "", ""
	if body == nil {
		body = map[string]string{}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, origin.String(), bytes.NewReader(encoded))
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Authorization", "Bearer "+bearer)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, response.StatusCode, errors.New("chatui: legal holds answered " + strconv.Itoa(response.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	return raw, response.StatusCode, err
}

// chatHoldReadForm reads the named fields of the form that was submitted.
func chatHoldReadForm(event ui.Event) map[string]string {
	out := map[string]string{}
	form := event.JSValue().Get("currentTarget")
	if !form.Truthy() {
		return out
	}
	for _, name := range []string{"id", "conversation", "matter", "reason"} {
		if field := form.Call("querySelector", "[name="+name+"]"); field.Truthy() {
			out[name] = field.Get("value").String()
		}
	}
	return out
}
