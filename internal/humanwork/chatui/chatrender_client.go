package chatui

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sync/atomic"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// ReadingSettingsClient uses the same admitted session credential as Chat. Its
// owner supplies a same-origin base and HTTP client; it never derives identity.
type ReadingSettingsClient struct {
	Origin, Bearer string
	Locale         string
	HTTP           *http.Client
	// Session, when set, remembers that the server answered "not available"
	// so the page never asks again in the same session.
	Session *ReadingSession
	// Refresh, when set, gets a fresh credential after the server refused the
	// one the page was opened with (a tab that outlived its session or the
	// server's restart). The refused request is repeated once with it
	// (CHATBUG-087).
	Refresh func(context.Context) (string, error)
}

// ReadingSession is the per-page-session memory of a reading service that is
// not composed on the server (a 200 body of {"available":false,"reason":...}).
type ReadingSession struct{ off atomic.Bool }

// Off reports whether the server has said the service is not available.
func (s *ReadingSession) Off() bool { return s != nil && s.off.Load() }

// unavailable reports whether body is the typed "not available" answer and, if
// so, records it for the session.
func (c ReadingSettingsClient) unavailable(body []byte) bool {
	var probe struct {
		Available *bool `json:"available"`
	}
	if json.Unmarshal(body, &probe) != nil || probe.Available == nil || *probe.Available {
		return false
	}
	if c.Session != nil {
		c.Session.off.Store(true)
	}
	return true
}

func (c ReadingSettingsClient) request(ctx context.Context, method, room string, payload io.Reader) (*http.Response, error) {
	return c.requestEndpoint(ctx, method, "settings", room, payload)
}
func (c ReadingSettingsClient) requestEndpoint(ctx context.Context, method, endpoint, room string, payload io.Reader) (*http.Response, error) {
	if method == http.MethodGet && c.Session.Off() {
		return nil, chatrender.ErrUnavailable
	}
	var body []byte
	if payload != nil {
		var err error
		if body, err = io.ReadAll(payload); err != nil {
			return nil, err
		}
	}
	response, err := c.send(ctx, method, endpoint, room, body, payload != nil)
	// A 403 on a read is the person's role, not the credential; on a write it may
	// be the browser token the server minted for an older page.
	if err != nil || c.Refresh == nil || (response.StatusCode != http.StatusUnauthorized && !(response.StatusCode == http.StatusForbidden && method != http.MethodGet)) {
		return response, err
	}
	// The credential the page holds was refused. Ask for a fresh one once and
	// repeat the request; if none is to be had, the refusal stands.
	bearer, refreshErr := c.Refresh(ctx)
	if refreshErr != nil || bearer == "" || bearer == c.Bearer {
		return response, nil
	}
	response.Body.Close()
	c.Bearer = bearer
	return c.send(ctx, method, endpoint, room, body, payload != nil)
}
func (c ReadingSettingsClient) send(ctx context.Context, method, endpoint, room string, body []byte, hasBody bool) (*http.Response, error) {
	var payload io.Reader
	if hasBody {
		payload = bytes.NewReader(body)
	}
	origin, err := url.Parse(c.Origin)
	if err != nil || origin.Host == "" || (origin.Scheme != "http" && origin.Scheme != "https") || origin.User != nil || c.Bearer == "" || c.HTTP == nil {
		return nil, chatrender.ErrUnavailable
	}
	origin.Path = "/api/chat/renderings/v1/" + endpoint
	origin.RawQuery = ""
	origin.Fragment = ""
	if room != "" {
		q := url.Values{}
		q.Set("conversation", room)
		origin.RawQuery = q.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, origin.String(), payload)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.Bearer)
	request.Header.Set("Accept", "application/json")
	if c.Locale != "" {
		request.Header.Set("Accept-Language", c.Locale)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client.Do(request)
}
func (c ReadingSettingsClient) LoadLanguages(ctx context.Context, room string) (map[string]int, error) {
	if room == "" {
		return map[string]int{}, nil
	}
	response, err := c.requestEndpoint(ctx, http.MethodGet, "languages", room, nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, readingStatusError(response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16384))
	if err != nil {
		return nil, err
	}
	if c.unavailable(raw) {
		return nil, chatrender.ErrUnavailable
	}
	var counts map[string]int
	if err = json.Unmarshal(raw, &counts); err != nil {
		return nil, err
	}
	for _, n := range counts {
		if n < 0 {
			return nil, chatrender.ErrInvalid
		}
	}
	return counts, nil
}
func (c ReadingSettingsClient) Load(ctx context.Context, room string) (chatrender.Preference, error) {
	var pref chatrender.Preference
	response, err := c.request(ctx, http.MethodGet, room, nil)
	if err != nil {
		return pref, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return pref, readingStatusError(response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16384))
	if err != nil {
		return pref, err
	}
	if c.unavailable(raw) {
		return pref, chatrender.ErrUnavailable
	}
	if err = json.Unmarshal(raw, &pref); err != nil {
		return pref, err
	}
	return pref, pref.Validate()
}
func (c ReadingSettingsClient) Save(ctx context.Context, room string, pref chatrender.Preference) error {
	if err := pref.Validate(); err != nil {
		return err
	}
	b, err := json.Marshal(struct {
		Settings chatrender.Preference `json:"settings"`
	}{pref})
	if err != nil {
		return err
	}
	response, err := c.request(ctx, http.MethodPut, room, bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return readingStatusError(response.StatusCode)
	}
	return nil
}
