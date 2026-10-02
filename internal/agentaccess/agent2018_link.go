// Package agentaccess is the server side of the two agent access pages: the
// user's own "My agent access" page (AGENT2-018) and the administrator's
// connection and skill-grant console (AGENT2-019). It composes the agent
// connection registry; it never holds a provider credential and never reaches
// a provider itself. The provider exchange is a function the caller supplies.
package agentaccess

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentconnect"
)

var (
	ErrInvalid       = errors.New("agentaccess: invalid request")
	ErrNotLinkable   = errors.New("agentaccess: connection cannot be linked by a user")
	ErrStateRejected = errors.New("agentaccess: authorization state is unknown, expired, used or belongs to someone else")
)

// Provider is the authorization endpoint an administrator published for one
// user-delegated connection. Only this record can start a link: a URL that
// arrives in a chat message or a query string has no entry here.
type Provider struct {
	ConnectionID string
	Name         string
	AuthorizeURL string
	ClientID     string
	RedirectURL  string
	Scopes       []string
}

// Providers resolves the published provider of a connection.
type Providers interface {
	Provider(tenant, connectionID string) (Provider, bool)
}

// Exchange trades the authorization code and the PKCE verifier for the
// binding of the user's own OAuth grant and the provider's account id. The
// production implementation calls the provider's token endpoint through the
// custody service; tests supply a fake.
type Exchange func(ctx context.Context, provider Provider, code, verifier string) (agentconnect.CredentialBinding, string, error)

// LinkSink records a finished link. *agentconnect.Registry satisfies it.
type LinkSink interface {
	LinkAccount(user agentconnect.UserContext, connectionID string, binding agentconnect.CredentialBinding, externalAccountID string) error
}

// Authorization is what the page receives when a user presses Link account.
type Authorization struct {
	URL                 string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	ExpiresAt           time.Time
}

type pendingLink struct {
	tenant, user, connection string
	verifier                 string
	expires                  time.Time
}

// Linker runs the authorization-code flow with PKCE for one tenant's users.
type Linker struct {
	providers Providers
	sink      LinkSink
	exchange  Exchange
	now       func() time.Time
	random    io.Reader
	ttl       time.Duration

	mu      sync.Mutex
	pending map[string]pendingLink
}

// NewLinker returns a linker. now and random default to the clock and the
// system's secure random source.
func NewLinker(providers Providers, sink LinkSink, exchange Exchange, now func() time.Time, random io.Reader) (*Linker, error) {
	if providers == nil || sink == nil || exchange == nil {
		return nil, fmt.Errorf("%w: providers, sink and exchange are required", ErrInvalid)
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if random == nil {
		random = rand.Reader
	}
	return &Linker{providers: providers, sink: sink, exchange: exchange, now: now, random: random, ttl: 10 * time.Minute, pending: map[string]pendingLink{}}, nil
}

// CodeChallenge is the S256 transform of a PKCE verifier (RFC 7636).
func CodeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (l *Linker) token() (string, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(l.random, raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Start mints a state and a verifier for this user and connection and returns
// the provider URL carrying the S256 challenge. The verifier stays here.
func (l *Linker) Start(user agentconnect.UserContext, connectionID string) (Authorization, error) {
	provider, ok := l.providers.Provider(user.TenantID, connectionID)
	if !ok || strings.TrimSpace(user.UserID) == "" {
		return Authorization{}, ErrNotLinkable
	}
	target, err := url.Parse(provider.AuthorizeURL)
	if err != nil || target.Host == "" || (target.Scheme != "https" && target.Scheme != "http") {
		return Authorization{}, fmt.Errorf("%w: provider has no usable authorization URL", ErrNotLinkable)
	}
	verifier, err := l.token()
	if err != nil {
		return Authorization{}, err
	}
	state, err := l.token()
	if err != nil {
		return Authorization{}, err
	}
	challenge := CodeChallenge(verifier)
	query := target.Query()
	query.Set("response_type", "code")
	query.Set("client_id", provider.ClientID)
	query.Set("redirect_uri", provider.RedirectURL)
	query.Set("scope", strings.Join(provider.Scopes, " "))
	query.Set("state", state)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	target.RawQuery = query.Encode()

	expires := l.now().Add(l.ttl)
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, entry := range l.pending {
		if !entry.expires.After(l.now()) {
			delete(l.pending, key)
		}
	}
	l.pending[stateKey(state)] = pendingLink{tenant: user.TenantID, user: user.UserID, connection: connectionID, verifier: verifier, expires: expires}
	return Authorization{URL: target.String(), State: state, CodeChallenge: challenge, CodeChallengeMethod: "S256", ExpiresAt: expires}, nil
}

func stateKey(state string) string {
	sum := sha256.Sum256([]byte(state))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Complete finishes the flow from the provider's redirect. The state must be
// one Start minted for this same signed-in user, unused and unexpired; it is
// spent before the exchange so a replay cannot link twice. A callback that
// carries no state this server issued (a link someone posted in chat, a
// forged redirect) is refused without a call to the provider.
func (l *Linker) Complete(ctx context.Context, user agentconnect.UserContext, state, code string) (string, error) {
	if strings.TrimSpace(state) == "" || strings.TrimSpace(code) == "" {
		return "", ErrStateRejected
	}
	key := stateKey(state)
	l.mu.Lock()
	entry, ok := l.pending[key]
	switch {
	case !ok:
	case !entry.expires.After(l.now()):
		delete(l.pending, key)
		ok = false
	case subtle.ConstantTimeCompare([]byte(entry.user), []byte(user.UserID)) != 1 || subtle.ConstantTimeCompare([]byte(entry.tenant), []byte(user.TenantID)) != 1:
		// Someone else holding the link cannot finish it, and cannot spend the
		// state the real user still needs.
		ok = false
	default:
		delete(l.pending, key)
	}
	l.mu.Unlock()
	if !ok {
		return "", ErrStateRejected
	}
	provider, ok := l.providers.Provider(entry.tenant, entry.connection)
	if !ok {
		return "", ErrNotLinkable
	}
	binding, account, err := l.exchange(ctx, provider, code, entry.verifier)
	if err != nil {
		return "", err
	}
	if err := l.sink.LinkAccount(user, entry.connection, binding, account); err != nil {
		return "", err
	}
	return entry.connection, nil
}
