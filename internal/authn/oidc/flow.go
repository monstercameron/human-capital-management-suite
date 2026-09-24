package oidc

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	trustfederation "github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

// defaultPendingTTL bounds how long a [PendingAuthorization] -- and the
// PKCE code_challenge digest it carries -- may be redeemed before
// [Flow.HandleCallback] refuses it as expired.
const defaultPendingTTL = 10 * time.Minute

// minFlowSecretBytes is the minimum length [FlowConfig.Secret] must be: long
// enough that the HMAC key [deriveVerifier] uses cannot be brute-forced
// offline from an observed (state, verifier) pair.
const minFlowSecretBytes = 32

// FlowConfig configures a [Flow]. Every port is required except Secret and
// TTL, which have safe defaults for a single-process deployment.
type FlowConfig struct {
	// Registry is looked up for the ACTIVE issuer [Flow.BeginAuthorization]
	// binds to and [Flow.HandleCallback] re-resolves before validating an
	// ID token, so a suspension or retirement that happens between the two
	// calls is honored.
	Registry issuerregistry.Store
	// Keys resolves an issuer's current signing keys. Wire it to
	// [issuerregistry.NewTenantResolver] or [issuerregistry.NewResolver]
	// in production so both packages read the same governed material.
	Keys trustfederation.IssuerResolver
	// Clients resolves the [ClientRegistration] bound to each (tenant,
	// issuer) pair.
	Clients ClientSource
	// States persists in-flight [PendingAuthorization] records between
	// [Flow.BeginAuthorization] and [Flow.HandleCallback].
	States StateStore
	// Exchanger performs the token endpoint call.
	Exchanger TokenExchanger
	// Secret is the HMAC key [deriveVerifier] uses. Nil mints a fresh
	// crypto/rand key, which is only correct for a single-process
	// deployment (or a test); see doc.go's "Code verifier custody"
	// section. A non-nil value must be at least [minFlowSecretBytes]
	// bytes.
	Secret []byte
	// TTL bounds a pending authorization's lifetime. Zero means
	// [defaultPendingTTL].
	TTL time.Duration
	// Sink optionally receives every [Evidence] [Flow.HandleCallback]
	// produces. Nil is valid; the evidence is still returned to the
	// caller either way.
	Sink EvidenceSink
}

// Flow is AUTHN-002's authorization-code-with-PKCE state machine. Every
// method is safe for concurrent use once constructed.
type Flow struct {
	registry  issuerregistry.Store
	keys      trustfederation.IssuerResolver
	clients   ClientSource
	states    StateStore
	exchanger TokenExchanger
	secret    []byte
	ttl       time.Duration
	sink      EvidenceSink
}

// NewFlow validates cfg and returns the flow.
func NewFlow(cfg FlowConfig) (*Flow, error) {
	if cfg.Registry == nil {
		return nil, fmt.Errorf("%w: no issuer registry store", ErrInvalidFlowConfig)
	}
	if cfg.Keys == nil {
		return nil, fmt.Errorf("%w: no issuer key resolver", ErrInvalidFlowConfig)
	}
	if cfg.Clients == nil {
		return nil, fmt.Errorf("%w: no client source", ErrInvalidFlowConfig)
	}
	if cfg.States == nil {
		return nil, fmt.Errorf("%w: no pending-authorization store", ErrInvalidFlowConfig)
	}
	if cfg.Exchanger == nil {
		return nil, fmt.Errorf("%w: no token exchanger", ErrInvalidFlowConfig)
	}

	secret := cfg.Secret
	if secret == nil {
		secret = make([]byte, minFlowSecretBytes)
		if _, err := rand.Read(secret); err != nil {
			return nil, fmt.Errorf("oidc: mint flow secret: %w", err)
		}
	} else if len(secret) < minFlowSecretBytes {
		return nil, fmt.Errorf("%w: secret must be at least %d bytes, got %d", ErrInvalidFlowConfig, minFlowSecretBytes, len(secret))
	}

	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = defaultPendingTTL
	}

	return &Flow{
		registry:  cfg.Registry,
		keys:      cfg.Keys,
		clients:   cfg.Clients,
		states:    cfg.States,
		exchanger: cfg.Exchanger,
		secret:    append([]byte(nil), secret...),
		ttl:       ttl,
		sink:      cfg.Sink,
	}, nil
}

// AuthorizationRequest is what [Flow.BeginAuthorization] hands the caller to
// redirect the user's browser to.
type AuthorizationRequest struct {
	// URL is the complete authorization endpoint URL, including
	// response_type, client_id, redirect_uri, scope, state, nonce,
	// code_challenge and code_challenge_method=S256.
	URL string
	// State and Nonce are returned for observability/logging only -- they
	// are already embedded in URL and already persisted in the
	// [PendingAuthorization] [Flow.HandleCallback] will look up; a caller
	// never needs to pass them back explicitly.
	State string
	Nonce string
	// ExpiresAt is when the pending authorization -- and therefore this
	// authorization attempt -- stops being redeemable.
	ExpiresAt time.Time
}

// BeginAuthorization looks up the ACTIVE issuer and its bound
// [ClientRegistration] for (tenant, issuerURL), mints a fresh state and
// nonce from crypto/rand, derives this attempt's PKCE verifier and S256
// challenge, persists a [PendingAuthorization] carrying only the challenge
// digest, and returns the authorization request URL to redirect the user's
// browser to.
//
// now is the caller's clock reading; it stamps the pending authorization's
// TTL window and is never read from a request.
func (f *Flow) BeginAuthorization(ctx context.Context, tenant values.TenantId, issuerURL string, now time.Time) (AuthorizationRequest, error) {
	if now.IsZero() {
		return AuthorizationRequest{}, fmt.Errorf("oidc: BeginAuthorization requires a non-zero now")
	}
	issuer, err := issuerregistry.Lookup(f.registry, tenant, issuerURL)
	if err != nil {
		return AuthorizationRequest{}, err
	}
	client, found, err := f.clients.LookupClient(tenant, issuerURL)
	if err != nil {
		return AuthorizationRequest{}, err
	}
	if !found {
		return AuthorizationRequest{}, fmt.Errorf("%w: tenant %q issuer %q", ErrClientNotRegistered, tenant, issuerURL)
	}
	if client.Tenant != tenant || client.IssuerURL != issuer.IssuerURL {
		return AuthorizationRequest{}, fmt.Errorf("%w: registration is bound to a different tenant or issuer", ErrClientNotRegistered)
	}
	if client.ClientID != issuer.Audience {
		return AuthorizationRequest{}, fmt.Errorf("%w: configured OIDC audience must equal the registered client id", ErrWrongAudience)
	}
	if err := client.validate(); err != nil {
		return AuthorizationRequest{}, err
	}

	state, err := randomToken(stateNonceBytes)
	if err != nil {
		return AuthorizationRequest{}, err
	}
	nonce, err := randomToken(stateNonceBytes)
	if err != nil {
		return AuthorizationRequest{}, err
	}
	verifier := deriveVerifier(f.secret, state)
	challenge := codeChallengeS256(verifier)

	now = now.UTC()
	pending := PendingAuthorization{
		Tenant:              tenant,
		IssuerURL:           issuer.IssuerURL,
		ClientID:            client.ClientID,
		RedirectURI:         client.RedirectURI,
		State:               state,
		Nonce:               nonce,
		CodeChallengeDigest: challenge,
		CreatedAt:           now,
		ExpiresAt:           now.Add(f.ttl),
	}
	if err := f.states.Put(ctx, pending); err != nil {
		return AuthorizationRequest{}, err
	}

	authURL, err := buildAuthorizationURL(client, state, nonce, challenge)
	if err != nil {
		return AuthorizationRequest{}, err
	}
	return AuthorizationRequest{URL: authURL, State: state, Nonce: nonce, ExpiresAt: pending.ExpiresAt}, nil
}

// buildAuthorizationURL renders the authorization request query string onto
// client's pinned authorization endpoint. redirect_uri always comes from
// client, never from any value a caller supplies to [Flow.BeginAuthorization]
// -- see doc.go's "Issuer records only pin verification material, not
// endpoints" section.
func buildAuthorizationURL(client ClientRegistration, state, nonce, challenge string) (string, error) {
	u, err := url.Parse(client.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("%w: authorization endpoint %q: %v", ErrInvalidClientRegistration, client.AuthorizationEndpoint, err)
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", client.ClientID)
	q.Set("redirect_uri", client.RedirectURI)
	q.Set("scope", strings.Join(client.Scopes, " "))
	q.Set("state", state)
	q.Set("nonce", nonce)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()
	return u.String(), nil
}
