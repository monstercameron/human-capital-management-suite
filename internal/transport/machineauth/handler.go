// Package machineauth serves the INTAPI-001 OAuth 2.0 client-credentials
// token endpoint and the token-signing JWKS document. It speaks only ports:
// the partnerapp.ClientRegistry for client authority, the machine.Issuer
// for minting, and machine key verification for client assertions. Storage,
// cryptography policy and server wiring all live outside this package.
//
// Client authentication is RFC 7523 private_key_jwt (assertion signed by a
// registered client key) or a mutually authenticated TLS client whose
// terminator forwards the verified certificate fingerprint in
// X-HCM-TLS-Client-SHA256 (the terminator contract INTAPI-004 owns: the
// edge must overwrite that header, never forward a client-supplied one).
// The source address comes from X-HCM-Source-IP under the same contract,
// else the connection's remote address.
package machineauth

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	app "github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/machine"
)

// Terminator headers. The edge overwrites both; a client-supplied value
// must never reach this handler in production.
const (
	HeaderTLSCertSHA256 = "X-HCM-TLS-Client-SHA256"
	HeaderSourceIP      = "X-HCM-Source-IP"
	HeaderDPoP          = "DPoP"
)

// clientAssertionType is the only client authentication mechanism this
// endpoint accepts beside mutual TLS.
const clientAssertionType = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"

// maxAssertionLifetime bounds a client assertion window (spec: at most
// five minutes).
const maxAssertionLifetime = 5 * time.Minute

// maxFormBytes bounds the token request body.
const maxFormBytes = 64 << 10

// Dependencies configures the token and JWKS handlers.
type Dependencies struct {
	Registry  app.MachineClientRegistry
	Issuer    *machine.Issuer
	Verifier  *machine.Verifier
	Audiences []string
	// TokenIssuer is the iss claim minted access tokens carry.
	TokenIssuer string
	// TokenURL is the endpoint's own URL: the required aud of client
	// assertions and htu of DPoP proofs.
	TokenURL string
	// Lifetime is the access-token lifetime. Zero means fifteen minutes.
	Lifetime time.Duration
	// Skew tolerates clock skew on assertion windows. Zero means none.
	Skew time.Duration
	Now  func() time.Time
}

func (d Dependencies) now() time.Time {
	if d.Now == nil {
		return time.Now().UTC()
	}
	return d.Now().UTC()
}

func (d Dependencies) lifetime() time.Duration {
	if d.Lifetime <= 0 {
		return machine.MaxLifetime
	}
	return min(d.Lifetime, machine.MaxLifetime)
}

// OAuthError is the RFC 6749 section 5.2 error body.
type OAuthError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// TokenResponse is the RFC 6749 section 5.1 success body.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope,omitempty"`
}

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(OAuthError{Error: code, ErrorDescription: description})
}

// replayCache refuses assertion and proof reuse: every jti is accepted at
// most once while unexpired. Single-replica memory; a replica restart
// forgets spent jtis, so a restarted replica re-admits an assertion whose
// window has not elapsed yet. Bearer-token replay across replicas is
// INTAPI-002's durable cache, not this one.
type replayCache struct {
	mu   sync.Mutex
	used map[string]time.Time
}

func (c *replayCache) seen(jti string, expires time.Time, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.used == nil {
		c.used = make(map[string]time.Time)
	}
	for k, exp := range c.used {
		if !now.Before(exp) {
			delete(c.used, k)
		}
	}
	if exp, ok := c.used[jti]; ok && now.Before(exp) {
		return true
	}
	c.used[jti] = expires
	return false
}

// Handler serves POST /oauth2/token and GET /.well-known/jwks.json.
type Handler struct {
	deps   Dependencies
	replay *replayCache
}

// NewHandler validates deps and returns the handler.
func NewHandler(deps Dependencies) (*Handler, error) {
	if deps.Registry == nil {
		return nil, errors.New("machineauth: a client registry is required")
	}
	if deps.Issuer == nil {
		return nil, errors.New("machineauth: a token issuer is required")
	}
	if deps.TokenIssuer == "" || deps.TokenURL == "" || len(deps.Audiences) == 0 {
		return nil, errors.New("machineauth: token issuer, token URL and audiences are required")
	}
	if _, err := url.ParseRequestURI(deps.TokenURL); err != nil {
		return nil, fmt.Errorf("machineauth: token URL: %w", err)
	}
	return &Handler{deps: deps, replay: &replayCache{}}, nil
}

// Routes returns the mux serving both endpoints.
func (h *Handler) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", h.HandleToken)
	mux.HandleFunc("GET /.well-known/jwks.json", h.HandleJWKS)
	return mux
}

// HandleJWKS publishes the token-signing keys. A retired key stays
// published until every token it signed has expired.
func (h *Handler) HandleJWKS(w http.ResponseWriter, r *http.Request) {
	set, err := h.deps.Issuer.Publish(h.deps.now())
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "temporarily_unavailable", "no signing keys are published")
		return
	}
	raw, err := json.Marshal(set)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "temporarily_unavailable", "the key set could not be rendered")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// HandleToken issues an access token for the client_credentials grant.
func (h *Handler) HandleToken(w http.ResponseWriter, r *http.Request) {
	now := h.deps.now()
	if r.Method != http.MethodPost {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "the token endpoint accepts POST only")
		return
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "the token request is form-encoded")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "the token request could not be read")
		return
	}
	if r.Form.Get("grant_type") != "client_credentials" {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "only client_credentials is issued here")
		return
	}

	tenant := strings.TrimSpace(r.Form.Get("tenant"))
	if tenant == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "tenant is required")
		return
	}

	client, authenticated := h.authenticate(w, r, tenant, now)
	if !authenticated {
		return
	}
	if err := app.AuthorizeMachineClientUse(client, app.MachineClientUseRequest{SourceIP: sourceIP(r), At: now}); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the client may not authenticate now")
		return
	}

	scopes, ok := intersectScopes(w, client.Scopes, r.Form.Get("scope"))
	if !ok {
		return
	}

	confirmation, tokenType, ok := h.senderConstraint(w, r, now)
	if !ok {
		return
	}
	// INTAPI-002: a write-capable client never leaves with a Bearer [REDACTED]
	// stolen bearer could write from anywhere. It must prove possession up
	// front (DPoP proof or mutual-TLS binding) so the issued token carries
	// the confirmation its later calls match against.
	if confirmation == nil && app.MachineClientGrantsWrite(client.Scopes) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "write-capable clients must present a DPoP proof or mutual-TLS binding")
		return
	}

	lifetime := h.deps.lifetime()
	token, err := h.deps.Issuer.Issue(machine.IssueRequest{
		Issuer: h.deps.TokenIssuer, Audience: append([]string(nil), h.deps.Audiences...),
		Subject: client.ClientID, Client: client.ClientID, Tenant: client.Tenant,
		Session: newSessionID(), Assurance: "substantial", TokenID: newTokenID(),
		Confirmation: confirmation, Lifetime: lifetime,
	})
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "temporarily_unavailable", "the token could not be issued")
		return
	}
	if err := h.deps.Registry.RecordClientUse(r.Context(), tenant, client.ClientID, now); err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "temporarily_unavailable", "the use could not be recorded")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(TokenResponse{
		AccessToken: token, TokenType: tokenType,
		ExpiresIn: int(lifetime / time.Second), Scope: strings.Join(scopes, " "),
	})
}

// authenticate resolves the client by assertion or mutual TLS. It reports
// whether handling may continue; refusals are already written.
func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request, tenant string, now time.Time) (app.MachineClient, bool) {
	if assertion := strings.TrimSpace(r.Form.Get("client_assertion")); assertion != "" {
		return h.authenticateAssertion(w, r, tenant, assertion, now)
	}
	return h.authenticateMTLS(w, r, tenant)
}

// authenticateAssertion validates an RFC 7523 private_key_jwt.
func (h *Handler) authenticateAssertion(w http.ResponseWriter, r *http.Request, tenant, assertion string, now time.Time) (app.MachineClient, bool) {
	if typ := r.Form.Get("client_assertion_type"); typ != clientAssertionType {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "a JWT assertion needs the jwt-bearer assertion type")
		return app.MachineClient{}, false
	}
	header, claims, signature, err := parseAssertion(assertion)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the assertion is malformed")
		return app.MachineClient{}, false
	}
	clientID := claims.Subject
	if clientID == "" || claims.Issuer != clientID {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the assertion names no client")
		return app.MachineClient{}, false
	}
	if !audienceContains(claims.Audience, h.deps.TokenURL) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the assertion is for another endpoint")
		return app.MachineClient{}, false
	}
	if claims.TokenID == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the assertion carries no identifier")
		return app.MachineClient{}, false
	}
	if now.Before(claims.IssuedAt.Add(-h.deps.Skew)) || !now.Before(claims.ExpiresAt.Add(h.deps.Skew)) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the assertion is outside its window")
		return app.MachineClient{}, false
	}
	if claims.ExpiresAt.Sub(claims.IssuedAt) > maxAssertionLifetime+h.deps.Skew {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the assertion window exceeds five minutes")
		return app.MachineClient{}, false
	}
	client, err := h.deps.Registry.LoadClient(r.Context(), tenant, clientID)
	if err != nil {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "the client is unknown")
		return app.MachineClient{}, false
	}
	keys, err := h.deps.Registry.LoadClientKeys(r.Context(), tenant, clientID)
	if err != nil {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "the client keys are unavailable")
		return app.MachineClient{}, false
	}
	key, err := app.SelectMachineClientKey(toPortKeys(keys), header.KID, now)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the assertion key is unusable")
		return app.MachineClient{}, false
	}
	pub, alg, err := machine.PublicKeyFromJWK(key.JWK)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the assertion key is unusable")
		return app.MachineClient{}, false
	}
	if alg != header.Alg {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the assertion algorithm does not match the key")
		return app.MachineClient{}, false
	}
	signingInput := assertion[:strings.LastIndex(assertion, ".")]
	if err := machine.VerifyAssertionSignature(alg, pub, signingInput, signature); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the assertion signature does not verify")
		return app.MachineClient{}, false
	}
	if h.replay.seen("assertion:"+claims.TokenID, claims.ExpiresAt, now) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the assertion was already used")
		return app.MachineClient{}, false
	}
	return client, true
}

// authenticateMTLS resolves the client from the terminator-forwarded
// certificate fingerprint and the mTLS client_id form field.
func (h *Handler) authenticateMTLS(w http.ResponseWriter, r *http.Request, tenant string) (app.MachineClient, bool) {
	fingerprint := strings.ToLower(strings.TrimSpace(r.Header.Get(HeaderTLSCertSHA256)))
	clientID := strings.TrimSpace(r.Form.Get("client_id"))
	if fingerprint == "" || clientID == "" {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "private_key_jwt or mutual TLS client authentication is required")
		return app.MachineClient{}, false
	}
	client, err := h.deps.Registry.LoadClient(r.Context(), tenant, clientID)
	if err != nil {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "the client is unknown")
		return app.MachineClient{}, false
	}
	if client.CertFingerprint == "" || subtle.ConstantTimeCompare([]byte(client.CertFingerprint), []byte(fingerprint)) != 1 {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "the certificate is not bound to this client")
		return app.MachineClient{}, false
	}
	return client, true
}

// senderConstraint validates an optional DPoP proof and returns the token
// confirmation and wire token type.
func (h *Handler) senderConstraint(w http.ResponseWriter, r *http.Request, now time.Time) (map[string]any, string, bool) {
	proof := strings.TrimSpace(r.Header.Get(HeaderDPoP))
	if proof == "" {
		if fp := strings.ToLower(strings.TrimSpace(r.Header.Get(HeaderTLSCertSHA256))); fp != "" {
			der, err := fingerprintBytes(fp)
			if err != nil {
				writeOAuthError(w, http.StatusBadRequest, "invalid_request", "the certificate fingerprint is malformed")
				return nil, "", false
			}
			return map[string]any{"x5t#S256": b64url(der)}, "Bearer", true
		}
		return nil, "Bearer", true
	}
	jkt, ok := h.validateDPoP(w, r, proof, now)
	if !ok {
		return nil, "", false
	}
	return map[string]any{"jkt": jkt}, "DPoP", true
}

func intersectScopes(w http.ResponseWriter, granted []string, requested string) ([]string, bool) {
	if strings.TrimSpace(requested) == "" {
		return append([]string(nil), granted...), true
	}
	want := strings.Fields(requested)
	have := make(map[string]bool, len(granted))
	for _, s := range granted {
		have[s] = true
	}
	for _, s := range want {
		if !have[s] {
			writeOAuthError(w, http.StatusBadRequest, "invalid_scope", "the requested scope exceeds the client grant")
			return nil, false
		}
	}
	return want, true
}

func toPortKeys(keys []app.MachineClientKey) []app.MachineClientKey { return keys }

func sourceIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get(HeaderSourceIP)); forwarded != "" {
		if host, _, err := net.SplitHostPort(forwarded); err == nil {
			return host
		}
		return forwarded
	}
	if host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr)); err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func audienceContains(aud []string, want string) bool {
	for _, a := range aud {
		if a == want {
			return true
		}
	}
	return false
}
