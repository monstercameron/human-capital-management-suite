package oidc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// CallbackParams is the redirect callback's query parameters, decoded by the
// caller (an HTTP handler outside this package) and handed in as plain
// values -- this package never parses an *http.Request itself, keeping it
// free of any transport framework dependency.
type CallbackParams struct {
	// State and Code are the callback's "state" and "code" query
	// parameters.
	State string
	Code  string
	// Error and ErrorDescription are the callback's "error" and
	// "error_description" query parameters, present instead of Code when
	// the identity provider declined the authorization request (for
	// example the user denied consent).
	Error            string
	ErrorDescription string
	// Now is the caller's clock reading for this callback.
	Now time.Time
}

// HandleCallback completes one authorization attempt: it consumes the
// pending authorization named by params.State (refusing an unknown or
// already-consumed state, or one whose TTL has elapsed), re-derives this
// attempt's PKCE code_verifier, exchanges the code for tokens through the
// injected [TokenExchanger], validates the returned ID token against the
// (re-resolved, still-ACTIVE) issuer's verification material, maps its
// claims through the issuer's governed [issuerregistry.ClaimMapping] list,
// and returns the resulting [trust.Principal].
//
// It always returns an [Evidence] record, even on refusal, and -- when a
// [FlowConfig.Sink] is configured -- has already handed that same record to
// it before returning. Every refusal returns the same [Evidence]/error
// pair, so a caller cannot observe a partially-completed flow.
func (f *Flow) HandleCallback(ctx context.Context, params CallbackParams) (*trust.Principal, Evidence, error) {
	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()

	if params.Error != "" {
		return f.deny("", "", "", now, fmt.Errorf("%w: %s: %s", ErrAuthorizationDenied, params.Error, params.ErrorDescription))
	}
	if params.State == "" || len(params.State) > 128 || params.Code == "" || len(params.Code) > 4096 {
		return f.deny("", "", "", now, ErrCallbackMalformed)
	}

	pending, found, err := f.states.Take(ctx, params.State)
	if err != nil {
		return f.deny("", "", "", now, err)
	}
	if !found {
		return f.deny("", "", "", now, ErrReplayedOrUnknownState)
	}
	if now.After(pending.ExpiresAt) {
		return f.deny(pending.Tenant, pending.IssuerURL, "", now, ErrVerifierExpired)
	}

	issuer, err := issuerregistry.Lookup(f.registry, pending.Tenant, pending.IssuerURL)
	if err != nil {
		return f.deny(pending.Tenant, pending.IssuerURL, "", now, err)
	}
	client, found, err := f.clients.LookupClient(pending.Tenant, pending.IssuerURL)
	if err != nil {
		return f.deny(pending.Tenant, pending.IssuerURL, "", now, err)
	}
	if !found {
		return f.deny(pending.Tenant, pending.IssuerURL, "", now, ErrClientNotRegistered)
	}
	if client.Tenant != pending.Tenant || client.IssuerURL != pending.IssuerURL || client.ClientID != pending.ClientID || client.RedirectURI != pending.RedirectURI {
		return f.deny(pending.Tenant, pending.IssuerURL, "", now, ErrClientNotRegistered)
	}
	if client.ClientID != issuer.Audience {
		return f.deny(pending.Tenant, pending.IssuerURL, "", now, ErrWrongAudience)
	}

	verifier := deriveVerifier(f.secret, pending.State)
	if codeChallengeS256(verifier) != pending.CodeChallengeDigest {
		// Only reachable if the flow's secret changed between
		// BeginAuthorization and HandleCallback -- see FlowConfig.Secret's
		// doc comment.
		return f.deny(pending.Tenant, pending.IssuerURL, "", now, ErrPKCEDerivationMismatch)
	}

	tokenResp, err := f.exchanger.Exchange(ctx, TokenRequest{
		TokenEndpoint: client.TokenEndpoint,
		Code:          params.Code,
		RedirectURI:   pending.RedirectURI,
		ClientID:      client.ClientID,
		ClientSecret:  client.ClientSecret,
		CodeVerifier:  verifier,
	})
	if err != nil {
		return f.deny(pending.Tenant, pending.IssuerURL, "", now, err)
	}
	if tokenResp.Error != "" {
		return f.deny(pending.Tenant, pending.IssuerURL, "", now, fmt.Errorf("%w: %s: %s", ErrGrantRejected, tokenResp.Error, tokenResp.ErrorDescription))
	}
	if tokenResp.IDToken == "" {
		return f.deny(pending.Tenant, pending.IssuerURL, "", now, ErrMissingIDToken)
	}

	result, err := validateIDToken(ctx, f.keys, issuer, client.ClientID, tokenResp.IDToken, pending.Nonce, tokenResp.AccessToken, now)
	if err != nil {
		return f.deny(pending.Tenant, pending.IssuerURL, "", now, err)
	}

	digestBytes := sha256.Sum256([]byte(tokenResp.IDToken))
	credentialDigest := "cred:sha256:" + hex.EncodeToString(digestBytes[:])
	// This package authenticates the identity; it does not itself run a
	// server-side session (AUTHN-004). Until that exists, the session
	// reference is a stable, credential-derived placeholder, mirroring
	// internal/trust/federation.Validator.Validate's identical choice and
	// for the identical reason: it satisfies trust.NewPrincipal's
	// printable, non-empty SessionRef invariant without ever inventing a
	// caller-suppliable value.
	sessionSeed := sha256.Sum256([]byte("oidc-pending-session:" + tokenResp.IDToken))
	sessionRef := "oidc:" + hex.EncodeToString(sessionSeed[:16])

	spec, err := mapClaims(issuer, result.raw, result.standard, credentialDigest, sessionRef)
	if err != nil {
		return f.deny(pending.Tenant, pending.IssuerURL, "", now, err)
	}
	principal, err := trust.NewPrincipal(spec)
	if err != nil {
		return f.deny(pending.Tenant, pending.IssuerURL, spec.Subject, now, fmt.Errorf("%w: %v", ErrClaimMapping, err))
	}

	ev := newEvidence(pending.Tenant, pending.IssuerURL, principal.Subject(), OutcomeSuccess, "", now)
	f.record(ev)
	return principal, ev, nil
}

// deny builds and records a [OutcomeDenied] [Evidence] for reason and
// returns it alongside a nil principal and reason itself as the error.
func (f *Flow) deny(tenant values.TenantId, issuerURL, subject string, now time.Time, reason error) (*trust.Principal, Evidence, error) {
	ev := newEvidence(tenant, issuerURL, subject, OutcomeDenied, reason.Error(), now)
	f.record(ev)
	return nil, ev, reason
}

func (f *Flow) record(ev Evidence) {
	if f.sink != nil {
		f.sink.Record(ev)
	}
}
