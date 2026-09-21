// Package oauthcc is a standard-library OAuth 2.0 client-credentials token
// source (RFC 6749 section 4.4). It caches one access token, refreshes it a
// configurable skew before expiry, and collapses concurrent refreshes into a
// single token request. golang.org/x/oauth2 is deliberately not used; see
// definitions/architecture/oidc-oauth2-qualification.yaml.
//
// The client secret and access tokens never appear in errors or in the
// String/GoString rendering of any value in this package.
package oauthcc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultSkew is how long before expires_at a cached token is treated
	// as stale, so a token is never presented in its last moments of life.
	DefaultSkew = 30 * time.Second
	// MaxResponseBytes bounds the token endpoint response body.
	MaxResponseBytes = 64 << 10
	// DefaultFetchTimeout bounds one token request.
	DefaultFetchTimeout = 30 * time.Second

	redacted = "[REDACTED]"

	// maxExpiresIn caps a provider's expires_in (one day) so a bogus value
	// cannot overflow time.Duration or pin a token forever.
	maxExpiresIn = 24 * 60 * 60
)

// Doer is the HTTP surface the token source needs. *http.Client and the
// transport package's safe client satisfy it.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Config wires a TokenSource.
type Config struct {
	// TokenURL is the provider's token endpoint (absolute http or https).
	TokenURL string
	// ClientID and ClientSecret authenticate the client with HTTP Basic.
	ClientID     string
	ClientSecret string
	// Scope is the space-delimited scope requested; empty omits it.
	Scope string
	// Client performs the token request. Nil uses a fresh http.Client that
	// does not follow redirects.
	Client Doer
	// Skew is subtracted from expiry when deciding a token is stale
	// (default DefaultSkew; negative is treated as zero).
	Skew time.Duration
	// FetchTimeout bounds one token request (default DefaultFetchTimeout).
	FetchTimeout time.Duration
	// Now is the clock (default time.Now).
	Now func() time.Time
}

// String redacts the client secret.
func (c Config) String() string {
	return fmt.Sprintf("oauthcc.Config{TokenURL:%q, ClientID:%q, ClientSecret:%s, Scope:%q}", c.TokenURL, c.ClientID, redactIfSet(c.ClientSecret), c.Scope)
}

// GoString redacts the client secret under %#v.
func (c Config) GoString() string { return c.String() }

func redactIfSet(s string) string {
	if s == "" {
		return `""`
	}
	return redacted
}

// TokenError is a token endpoint error response (RFC 6749 section 5.2) or a
// non-200 status. Code is the OAuth error code when the body carried one.
// The provider's error_description is deliberately not retained.
type TokenError struct {
	Status int
	Code   string
}

func (e *TokenError) Error() string {
	if e.Code == "" {
		return "oauthcc: token endpoint returned status " + strconv.Itoa(e.Status)
	}
	return "oauthcc: token endpoint returned status " + strconv.Itoa(e.Status) + " (" + e.Code + ")"
}

// TokenSource hands out a cached client-credentials access token. It is safe
// for concurrent use.
type TokenSource struct {
	tokenURL     string
	clientID     string
	clientSecret string
	scope        string
	client       Doer
	skew         time.Duration
	fetchTimeout time.Duration
	now          func() time.Time

	mu        sync.Mutex
	token     string
	expiresAt time.Time
	inflight  *call
}

// call is one in-flight token request shared by every waiter.
type call struct {
	done  chan struct{}
	token string
	err   error
}

// New validates cfg and returns a TokenSource.
func New(cfg Config) (*TokenSource, error) {
	u, err := url.Parse(cfg.TokenURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return nil, errors.New("oauthcc: TokenURL must be an absolute http(s) URL without userinfo")
	}
	if cfg.ClientID == "" {
		return nil, errors.New("oauthcc: ClientID is required")
	}
	if cfg.ClientSecret == "" {
		return nil, errors.New("oauthcc: ClientSecret is required")
	}
	ts := &TokenSource{
		tokenURL:     cfg.TokenURL,
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		scope:        cfg.Scope,
		client:       cfg.Client,
		skew:         cfg.Skew,
		fetchTimeout: cfg.FetchTimeout,
		now:          cfg.Now,
	}
	// Redirects are never followed: a 3xx from the token endpoint surfaces
	// as a *TokenError rather than re-posting the client credentials to
	// another location. A caller's *http.Client is copied, not mutated.
	switch c := ts.client.(type) {
	case nil:
		ts.client = &http.Client{CheckRedirect: noFollow}
	case *http.Client:
		cp := *c
		cp.CheckRedirect = noFollow
		ts.client = &cp
	}
	if ts.skew == 0 {
		ts.skew = DefaultSkew
	}
	if ts.skew < 0 {
		ts.skew = 0
	}
	if ts.fetchTimeout <= 0 {
		ts.fetchTimeout = DefaultFetchTimeout
	}
	if ts.now == nil {
		ts.now = time.Now
	}
	return ts, nil
}

// String redacts the client secret and never renders the cached token.
func (ts *TokenSource) String() string {
	if ts == nil {
		return "oauthcc.TokenSource(nil)"
	}
	return fmt.Sprintf("oauthcc.TokenSource{TokenURL:%q, ClientID:%q, ClientSecret:%s, Scope:%q}", ts.tokenURL, ts.clientID, redacted, ts.scope)
}

// GoString redacts under %#v.
func (ts *TokenSource) GoString() string { return ts.String() }

// Token returns the cached access token while it is fresh (now before
// expires_at minus skew) and otherwise fetches a new one. Concurrent callers
// during a refresh share a single token request. The request itself is not
// bound to any one caller's context, so a caller that gives up does not fail
// the others; each caller stops waiting when its own ctx ends.
func (ts *TokenSource) Token(ctx context.Context) (string, error) {
	if ctx == nil {
		return "", errors.New("oauthcc: context is required")
	}
	ts.mu.Lock()
	if ts.token != "" && ts.now().Before(ts.expiresAt.Add(-ts.skew)) {
		tok := ts.token
		ts.mu.Unlock()
		return tok, nil
	}
	c := ts.inflight
	if c == nil {
		c = &call{done: make(chan struct{})}
		ts.inflight = c
		go ts.refresh(context.WithoutCancel(ctx), c)
	}
	ts.mu.Unlock()
	select {
	case <-c.done:
		return c.token, c.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Invalidate drops the cached token only if it is still stale — the token
// the caller saw rejected. A token minted by a racing refresh is kept.
func (ts *TokenSource) Invalidate(stale string) {
	if stale == "" {
		return
	}
	ts.mu.Lock()
	if ts.token == stale {
		ts.token = ""
		ts.expiresAt = time.Time{}
	}
	ts.mu.Unlock()
}

func (ts *TokenSource) refresh(ctx context.Context, c *call) {
	ctx, cancel := context.WithTimeout(ctx, ts.fetchTimeout)
	defer cancel()
	issuedAt := ts.now()
	tok, ttl, err := ts.fetch(ctx)
	ts.mu.Lock()
	if err == nil {
		ts.token = tok
		ts.expiresAt = issuedAt.Add(ttl)
	}
	ts.inflight = nil
	c.token, c.err = tok, err
	ts.mu.Unlock()
	close(c.done)
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	Scope       string `json:"scope"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func noFollow(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// basicAuth encodes the client credentials per RFC 6749 section 2.3.1: each
// half is application/x-www-form-urlencoded before joining and base64.
func basicAuth(id, secret string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(url.QueryEscape(id)+":"+url.QueryEscape(secret)))
}

func (ts *TokenSource) fetch(ctx context.Context) (string, time.Duration, error) {
	form := url.Values{"grant_type": {"client_credentials"}}
	if ts.scope != "" {
		form.Set("scope", ts.scope)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, errors.New("oauthcc: token request could not be built")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", basicAuth(ts.clientID, ts.clientSecret))
	resp, err := ts.client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", 0, fmt.Errorf("oauthcc: token request: %w", ctxErr)
		}
		// The transport error is not wrapped verbatim: only its class is
		// kept so nothing from the request can leak through it.
		return "", 0, errors.New("oauthcc: token request failed in transport")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return "", 0, errors.New("oauthcc: token response could not be read")
	}
	if len(body) > MaxResponseBytes {
		return "", 0, errors.New("oauthcc: token response exceeds size limit")
	}
	if resp.StatusCode != http.StatusOK {
		var e errorResponse
		_ = json.Unmarshal(body, &e)
		return "", 0, &TokenError{Status: resp.StatusCode, Code: sanitizeCode(e.Error)}
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", 0, errors.New("oauthcc: token response is not valid JSON")
	}
	if tr.AccessToken == "" {
		return "", 0, errors.New("oauthcc: token response has no access_token")
	}
	if !strings.EqualFold(tr.TokenType, "Bearer") {
		return "", 0, errors.New("oauthcc: token response has unsupported token_type")
	}
	if tr.ExpiresIn <= 0 {
		return "", 0, errors.New("oauthcc: token response has no positive expires_in")
	}
	if tr.ExpiresIn > maxExpiresIn {
		tr.ExpiresIn = maxExpiresIn // avoid Duration overflow; still refreshes eventually
	}
	return tr.AccessToken, time.Duration(tr.ExpiresIn) * time.Second, nil
}

// sanitizeCode keeps an OAuth error code only if it has the RFC 6749
// section 5.2 shape (printable ASCII, no quote or backslash) and is short.
func sanitizeCode(code string) string {
	if len(code) > 64 {
		return ""
	}
	for i := 0; i < len(code); i++ {
		ch := code[i]
		if ch < 0x20 || ch > 0x7e || ch == '"' || ch == '\\' {
			return ""
		}
	}
	return code
}
