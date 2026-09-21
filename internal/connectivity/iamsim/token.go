package iamsim

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providertelemetry/providerwire"
)

// tokenBytes is the entropy of one access token. 32 bytes (256 bits) makes
// guessing infeasible; base64url without padding renders it as 43 characters.
const tokenBytes = 32

// knownScopes is the provider's scope vocabulary in canonical order. The
// granted scope string is always emitted in this order so identical grants
// compare equal as strings. It is a function so no caller can mutate it.
func knownScopes() []string { return []string{ScopeRead, ScopeWrite} }

// tokenHash is the SHA-256 of an access token: the only form the server keeps.
type tokenHash [sha256.Size]byte

// tokenRecord is what the server remembers about an issued token. It holds
// no copy of the token itself.
type tokenRecord struct {
	clientID  string
	scopes    []string
	issuedAt  time.Time
	expiresAt time.Time
}

// TokenResponse is the RFC 6749 section 5.1 success body.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	Scope       string `json:"scope"`
}

// OAuthError is the RFC 6749 section 5.2 error body.
type OAuthError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// b64token is the RFC 6750 section 2.1 bearer token syntax.
var b64token = regexp.MustCompile(`^[A-Za-z0-9\-._~+/]+=*$`)

func hashToken(token string) tokenHash { return sha256.Sum256([]byte(token)) }

// logID is the short, non-reversible token identifier used in logs, enough to
// correlate issue and use without ever writing a usable credential.
func (h tokenHash) logID() string { return hex.EncodeToString(h[:6]) }

// handleToken implements POST /oauth2/token for the client credentials grant.
// Order: request syntax, then client authentication, then grant and scope, so
// an unauthenticated caller learns nothing about which grants or scopes the
// client holds.
func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	flaky := s.scenario.TokenEndpointFlakyRate > 0 && s.random() < s.scenario.TokenEndpointFlakyRate
	ttl := s.scenario.tokenTTL()
	s.mu.Unlock()
	if flaky {
		s.log.Info("token endpoint transient failure injected")
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "simulated token endpoint outage")
		return
	}

	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "content type must be application/x-www-form-urlencoded")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxRequestBytes))
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "request body could not be read")
		return
	}
	form, err := url.ParseQuery(string(raw))
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "malformed form body")
		return
	}
	// RFC 6749 section 3.2: parameters must not be repeated.
	for name, values := range form {
		if len(values) > 1 {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "parameter repeated: "+name)
			return
		}
	}

	id, secret, usedBasic, err := clientCredentials(r, form)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if !s.authenticateClient(id, secret) {
		s.log.Warn("token client authentication failed", "basic", usedBasic)
		w.Header().Set("WWW-Authenticate", `Basic realm="`+Realm+`"`)
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "client authentication failed")
		return
	}

	switch grant := form.Get("grant_type"); grant {
	case "":
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "grant_type is required")
		return
	case "client_credentials":
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "only client_credentials is supported")
		return
	}

	scopes, ok := s.grantScopes(form.Get("scope"))
	if !ok {
		writeOAuthError(w, http.StatusBadRequest, "invalid_scope", "requested scope is unknown or not granted to this client")
		return
	}

	buf := make([]byte, tokenBytes)
	if _, err := io.ReadFull(s.tokenRand, buf); err != nil {
		s.log.Error("token entropy unavailable", "err", err)
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "token could not be minted")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	h := hashToken(token)
	now := s.now()
	s.mu.Lock()
	for k, rec := range s.tokens {
		if !now.Before(rec.expiresAt) {
			delete(s.tokens, k)
		}
	}
	s.tokens[h] = tokenRecord{clientID: id, scopes: scopes, issuedAt: now, expiresAt: now.Add(ttl)}
	s.mu.Unlock()

	scope := strings.Join(scopes, " ")
	s.log.Info("token issued", append([]any{"client_id", id, "scope", scope, "token_id", h.logID(), "expires_in_s", int64(ttl / time.Second)},
		wireAttrs(providerwire.FromHeader(r.Header))...)...)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, TokenResponse{AccessToken: token, TokenType: "Bearer", ExpiresIn: int64(ttl / time.Second), Scope: scope})
}

// clientCredentials extracts the client identity from exactly one of HTTP
// Basic or the form body. RFC 6749 section 2.3 forbids using more than one
// authentication method in a request, so both present is a syntax error, not
// an authentication failure.
func clientCredentials(r *http.Request, form url.Values) (id, secret string, usedBasic bool, err error) {
	_, formID := form["client_id"]
	_, formSecret := form["client_secret"]
	usedForm := formID || formSecret
	authz := r.Header.Get("Authorization")
	if authz != "" {
		if usedForm {
			return "", "", true, errors.New("multiple client authentication methods")
		}
		scheme, rest, found := strings.Cut(authz, " ")
		if !found || !strings.EqualFold(scheme, "Basic") {
			return "", "", true, errors.New("unsupported authorization scheme")
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(rest))
		if err != nil {
			return "", "", true, errors.New("malformed basic credentials")
		}
		rawID, rawSecret, found := strings.Cut(string(decoded), ":")
		if !found {
			return "", "", true, errors.New("malformed basic credentials")
		}
		// RFC 6749 section 2.3.1: both halves are form-urlencoded before
		// being joined and base64-encoded.
		if id, err = url.QueryUnescape(rawID); err != nil {
			return "", "", true, errors.New("malformed basic credentials")
		}
		if secret, err = url.QueryUnescape(rawSecret); err != nil {
			return "", "", true, errors.New("malformed basic credentials")
		}
		return id, secret, true, nil
	}
	return form.Get("client_id"), form.Get("client_secret"), false, nil
}

// authenticateClient compares the id and the secret in constant time.
// Hashing first equalises lengths, and every comparison always runs (the id,
// and the secret against each valid secret in the rotation set), so neither
// timing nor early exit reveals which half was wrong or which secret matched.
func (s *Server) authenticateClient(id, secret string) bool {
	s.mu.Lock()
	secrets := s.clientSecrets
	s.mu.Unlock()
	if s.clientID == "" || len(secrets) == 0 {
		return false
	}
	gotID, wantID := sha256.Sum256([]byte(id)), sha256.Sum256([]byte(s.clientID))
	idOK := subtle.ConstantTimeCompare(gotID[:], wantID[:])
	gotSecret := sha256.Sum256([]byte(secret))
	secretOK := 0
	for i := range secrets {
		secretOK |= subtle.ConstantTimeCompare(gotSecret[:], secrets[i][:])
	}
	return idOK&secretOK == 1
}

// grantScopes resolves the requested scope string against the client's
// allowance. Empty means everything the client holds; any unknown or
// unallowed scope refuses the whole request rather than silently narrowing,
// so a misconfigured client fails loudly at stage one.
func (s *Server) grantScopes(requested string) ([]string, bool) {
	if strings.TrimSpace(requested) == "" {
		return s.clientScopes, true
	}
	want := strings.Fields(requested)
	for _, sc := range want {
		if !slices.Contains(s.clientScopes, sc) {
			return nil, false
		}
	}
	return normalizeScopes(want), true
}

// normalizeScopes returns the known scopes present in in, deduplicated and in
// canonical order. Unknown values are dropped.
func normalizeScopes(in []string) []string {
	var out []string
	for _, k := range knownScopes() {
		if slices.Contains(in, k) {
			out = append(out, k)
		}
	}
	return out
}

// requireBearer guards an API handler with a bearer token holding at least
// one of accepted. It runs before the handler reads the body or consults
// idempotency state, so an unauthenticated caller can neither probe which
// change refs exist nor spend server work. The first accepted scope is the
// one advertised in the insufficient_scope challenge.
func (s *Server) requireBearer(next http.HandlerFunc, accepted ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+Realm+`"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_request"})
			return
		}
		h := hashToken(token)
		now := s.now()
		s.mu.Lock()
		rec, found := s.tokens[h]
		s.mu.Unlock()
		if !found || !now.Before(rec.expiresAt) {
			reason := "unknown_or_revoked"
			if found {
				reason = "expired"
			}
			s.log.Info("bearer rejected", "token_id", h.logID(), "reason", reason, "path", r.URL.Path)
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+Realm+`", error="invalid_token"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_token"})
			return
		}
		if !slices.ContainsFunc(accepted, func(sc string) bool { return slices.Contains(rec.scopes, sc) }) {
			s.log.Info("bearer lacks scope", "token_id", h.logID(), "client_id", rec.clientID, "need", accepted[0], "path", r.URL.Path)
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+Realm+`", error="insufficient_scope", scope="`+accepted[0]+`"`)
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient_scope"})
			return
		}
		next(w, r)
	}
}

// bearerToken extracts the token from exactly one Authorization header of
// the form "Bearer <b64token>" (scheme case-insensitive per RFC 7235).
func bearerToken(r *http.Request) (string, bool) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return "", false
	}
	scheme, token, found := strings.Cut(values[0], " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || !b64token.MatchString(token) {
		return "", false
	}
	return token, true
}

// RevokeAllTokens invalidates every outstanding access token and reports how
// many were live. Clients must fetch a new token, which is the refresh-on-401
// path the HCM side has to prove.
func (s *Server) RevokeAllTokens() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.tokens)
	s.tokens = make(map[tokenHash]tokenRecord)
	return n
}

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, status, OAuthError{Error: code, ErrorDescription: description})
}
