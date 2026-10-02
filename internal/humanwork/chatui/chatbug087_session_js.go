//go:build js && wasm

package chatui

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"sync"
	"syscall/js"
	"time"
)

// chatSessionRefreshPath is the page shell: a GET of it, with the browser's own
// cookies, answers the credential a freshly opened page would carry, and mints a
// fresh browser token cookie on the way.
const chatSessionRefreshPath = "/workspace/journey"

var chatSession struct {
	mu     sync.Mutex
	at     time.Time
	bearer string
}

// chatSessionRefresh gets a fresh credential for the page, writes it into the
// page's configuration island so every Chat client presents it from now on, and
// returns it. Several clients that fail at once share one refresh. When the
// server will not hand one out, the session is over: ErrReadingSignedOut.
func chatSessionRefresh(ctx context.Context) (string, error) {
	chatSession.mu.Lock()
	defer chatSession.mu.Unlock()
	if chatSession.bearer != "" && time.Since(chatSession.at) < 10*time.Second {
		return chatSession.bearer, nil
	}
	origin, err := url.Parse(js.Global().Get("location").Get("origin").String())
	if err != nil || origin.Host == "" || (origin.Scheme != "http" && origin.Scheme != "https") || origin.User != nil {
		return "", ErrReadingSignedOut
	}
	origin.Path, origin.RawQuery, origin.Fragment = chatSessionRefreshPath, "", ""
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, origin.String(), nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "text/html")
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", ErrReadingSignedOut
	}
	page, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return "", err
	}
	bearer, ok := ParseJourneyBearer(string(page))
	if !ok {
		return "", ErrReadingSignedOut
	}
	if island := js.Global().Get("document").Call("getElementById", "journey-config"); island.Truthy() {
		if updated, err := withIslandBearer(island.Get("textContent").String(), bearer); err == nil {
			island.Set("textContent", updated)
		}
	}
	chatSession.at, chatSession.bearer = time.Now(), bearer
	return bearer, nil
}
