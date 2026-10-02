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

var errChatmapRequestInput = errors.New("chatui: location request is invalid")

// chatmapRawRequest asks one location action with the page's admitted session
// credential and answers the body and the status, so a caller can tell a refusal
// from a failure. When the server refuses the credential (a tab that outlived its
// session) it gets a fresh one and asks once more, as the translation settings do.
func chatmapRawRequest(ctx context.Context, action string, body any) ([]byte, int, error) {
	raw, status, err := chatmapRawAttempt(ctx, action, body)
	if status != http.StatusUnauthorized && !(status == http.StatusForbidden && body != nil) {
		return raw, status, err
	}
	if _, refreshErr := chatSessionRefresh(ctx); refreshErr != nil {
		return raw, status, err
	}
	return chatmapRawAttempt(ctx, action, body)
}

func chatmapRawAttempt(ctx context.Context, action string, body any) ([]byte, int, error) {
	origin, err := url.Parse(js.Global().Get("location").Get("origin").String())
	if err != nil || origin.Host == "" || (origin.Scheme != "http" && origin.Scheme != "https") || origin.User != nil {
		return nil, 0, errChatmapRequestInput
	}
	bearer := ""
	island := js.Global().Get("document").Call("getElementById", "journey-config")
	if island.Truthy() {
		var session struct {
			Bearer string `json:"bearer"`
		}
		if json.Unmarshal([]byte(island.Get("textContent").String()), &session) == nil {
			bearer = session.Bearer
		}
	}
	if bearer == "" {
		return nil, 0, errChatmapRequestInput
	}
	origin.Path = "/api/chat/locations/v1/" + action
	origin.RawQuery, origin.Fragment = "", ""
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
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, response.StatusCode, errors.New("chatui: location request answered " + strconv.Itoa(response.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return raw, response.StatusCode, err
}
