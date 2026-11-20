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

var errChatRetentionPreviewInput = errors.New("chatui: the retention preview needs a signed-in page")

// chatRetentionPreviewCall asks the server once and answers the status so the
// panel can tell a refusal from a failure.
func chatRetentionPreviewCall(done func(ChatRetentionPreviewData, int, error)) {
	go func() {
		var data ChatRetentionPreviewData
		ctx := context.Background()
		raw, status, err := chatRetentionPreviewAttempt(ctx)
		if status == http.StatusUnauthorized {
			// A tab that outlived its session gets a fresh credential and one more try.
			if _, refreshErr := chatSessionRefresh(ctx); refreshErr == nil {
				raw, status, err = chatRetentionPreviewAttempt(ctx)
			}
		}
		if err == nil {
			err = json.Unmarshal(raw, &data)
		}
		done(data, status, err)
	}()
}

func chatRetentionPreviewAttempt(ctx context.Context) ([]byte, int, error) {
	origin, err := url.Parse(js.Global().Get("location").Get("origin").String())
	if err != nil || origin.Host == "" || (origin.Scheme != "http" && origin.Scheme != "https") || origin.User != nil {
		return nil, 0, errChatRetentionPreviewInput
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
		return nil, 0, errChatRetentionPreviewInput
	}
	origin.Path, origin.RawQuery, origin.Fragment = "/api/chat/retention/v1/preview", "", ""
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, origin.String(), bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Authorization", "Bearer "+bearer)
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, response.StatusCode, errors.New("chatui: retention preview answered " + strconv.Itoa(response.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return raw, response.StatusCode, err
}
