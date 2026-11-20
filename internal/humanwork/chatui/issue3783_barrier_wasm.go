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
	"strings"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var errChatBarrierInput = errors.New("chatui: information barriers need a signed-in page and a filled form")

// chatBarrierCall asks the barrier service and answers the status and the
// server's refusal code, so the page can tell a form it should explain from a
// failure. It returns a cancel for the effect.
func chatBarrierCall(action string, body any, done func(ChatBarrierData, string, int, error)) func() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		var data ChatBarrierData
		raw, code, status, err := chatBarrierRequest(ctx, action, body)
		if err == nil && action == "list" {
			err = json.Unmarshal(raw, &data)
		}
		if ctx.Err() == nil {
			done(data, code, status, err)
		}
	}()
	return cancel
}

// chatBarrierRequest asks once and, when the server refuses the page's
// credential (a tab that outlived its session), gets a fresh one and asks once
// more.
func chatBarrierRequest(ctx context.Context, action string, body any) ([]byte, string, int, error) {
	raw, code, status, err := chatBarrierAttempt(ctx, action, body)
	if status != http.StatusUnauthorized {
		return raw, code, status, err
	}
	if _, refreshErr := chatSessionRefresh(ctx); refreshErr != nil {
		return raw, code, status, err
	}
	return chatBarrierAttempt(ctx, action, body)
}

func chatBarrierAttempt(ctx context.Context, action string, body any) ([]byte, string, int, error) {
	origin, err := url.Parse(js.Global().Get("location").Get("origin").String())
	if err != nil || origin.Host == "" || (origin.Scheme != "http" && origin.Scheme != "https") || origin.User != nil {
		return nil, "", 0, errChatBarrierInput
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
	if bearer == "" || (action != "list" && action != "add" && action != "remove") {
		return nil, "", 0, errChatBarrierInput
	}
	origin.Path, origin.RawQuery, origin.Fragment = "/api/chat/barriers/v1/"+action, "", ""
	if body == nil {
		body = map[string]string{}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, "", 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, origin.String(), bytes.NewReader(encoded))
	if err != nil {
		return nil, "", 0, err
	}
	request.Header.Set("Authorization", "Bearer "+bearer)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, "", 0, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != http.StatusOK {
		var refusal struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(raw, &refusal)
		return nil, refusal.Code, response.StatusCode, errors.New("chatui: information barriers answered " + strconv.Itoa(response.StatusCode))
	}
	return raw, "", response.StatusCode, err
}

// chatBarrierReadAdd reads the add form; both teams are required.
func chatBarrierReadAdd(event ui.Event) (any, error) {
	form := event.JSValue().Get("currentTarget")
	if !form.Truthy() {
		return nil, errChatBarrierInput
	}
	value := func(name string) string {
		field := form.Call("querySelector", "[name="+name+"]")
		if !field.Truthy() {
			return ""
		}
		return strings.TrimSpace(field.Get("value").String())
	}
	a, b := value("groupa"), value("groupb")
	if a == "" || b == "" {
		return nil, errChatBarrierInput
	}
	return map[string]string{"GroupA": a, "GroupB": b}, nil
}

// chatBarrierReadRemove reads the id from the remove form of one barrier.
func chatBarrierReadRemove(event ui.Event) (any, error) {
	form := event.JSValue().Get("currentTarget")
	if !form.Truthy() {
		return nil, errChatBarrierInput
	}
	field := form.Call("querySelector", "[name=id]")
	if !field.Truthy() || strings.TrimSpace(field.Get("value").String()) == "" {
		return nil, errChatBarrierInput
	}
	return map[string]string{"ID": strings.TrimSpace(field.Get("value").String())}, nil
}
