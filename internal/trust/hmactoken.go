package trust

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// tokenPrefix tags the deterministic development token format so that a token
// of a different scheme can never be mistaken for one of these.
const tokenPrefix = "hcmn1"

// maxTokenBytesDefault bounds credential parsing before any allocation that
// depends on the credential's own length.
const maxTokenBytesDefault = 8 << 10

// tokenEncoding is strict, unpadded base64url: one byte string has exactly one
// spelling, so a token cannot be re-encoded into a different-looking token that
// still verifies.
var tokenEncoding = base64.RawURLEncoding.Strict()

// Claims is the payload of a development/test bearer token. It mirrors the
// server-resolved fields of a [PrincipalSpec]; a federation adapter added
// under TRUST-002 produces the same shape from an OIDC ID token or a SAML
// assertion without any downstream change.
//
// Field names are the canonical JSON keys. Decoding is strict: an unknown key
// is a rejected credential, never a silently ignored one.
type Claims struct {
	Issuer               string   `json:"iss"`
	Audience             string   `json:"aud"`
	Subject              string   `json:"sub"`
	SubjectKind          string   `json:"sub_kind"`
	Tenant               string   `json:"tenant"`
	OrganizationScopeID  string   `json:"org_scope,omitempty"`
	Roles                []string `json:"roles,omitempty"`
	AuthorityRefs        []string `json:"authority_refs,omitempty"`
	Purposes             []string `json:"purposes,omitempty"`
	AuthenticationMethod string   `json:"authn_method"`
	Assurance            string   `json:"assurance"`
	SessionRef           string   `json:"session_ref"`
	DelegationRefs       []string `json:"delegation_refs,omitempty"`
	IssuedAtUnix         int64    `json:"iat"`
	ExpiresAtUnix        int64    `json:"exp"`
}

// HMACVerifierConfig configures [NewHMACVerifier].
type HMACVerifierConfig struct {
	// Key is the shared signing key. It must be at least 32 bytes.
	Key []byte
	// Issuer is the only issuer this verifier accepts.
	Issuer string
	// Audience is the audience this listener answers to. A credential's
	// [Credential.Audience] overrides it per call when set, which is how one
	// process serves several listeners.
	Audience string
	// Now supplies the current time. Nil means time.Now.
	Now func() time.Time
	// Leeway tolerates clock skew on the validity window. Zero means no
	// leeway, which is the correct default for a hermetic test.
	Leeway time.Duration
	// MaxTokenBytes bounds credential length. Zero means 8 KiB.
	MaxTokenBytes int
}

// MaxDevLifetime is the longest validity window a development token may
// carry. Development tokens are contained by the local-dev profile, not by
// short lifetimes, but a window above this ceiling is refused outright:
// nothing mints a development credential that outlives a day. Machine
// tokens carry their own fifteen-minute cap in the machine package.
const MaxDevLifetime = 24 * time.Hour

// HMACVerifier is the deterministic development and test [Verifier]: an
// HMAC-SHA256 signed bearer token whose payload is a strict JSON [Claims].
//
// It is deliberately boring. Its job is to make the trusted-context machinery
// exercisable end to end without standing up an identity provider, while
// enforcing every rule a real verifier must enforce: signature, issuer,
// audience, validity window and assurance evidence.
type HMACVerifier struct {
	key           []byte
	issuer        string
	audience      string
	now           func() time.Time
	leeway        time.Duration
	maxTokenBytes int
}

// Configuration errors for [NewHMACVerifier].
var (
	ErrVerifierKey      = errors.New("trust: hmac verifier needs a key of at least 32 bytes")
	ErrVerifierIssuer   = errors.New("trust: hmac verifier needs an issuer")
	ErrVerifierAudience = errors.New("trust: hmac verifier needs an audience")
)

// NewHMACVerifier validates cfg and returns the verifier.
func NewHMACVerifier(cfg HMACVerifierConfig) (*HMACVerifier, error) {
	if len(cfg.Key) < 32 {
		return nil, ErrVerifierKey
	}
	if cfg.Issuer == "" {
		return nil, ErrVerifierIssuer
	}
	if cfg.Audience == "" {
		return nil, ErrVerifierAudience
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	maxBytes := cfg.MaxTokenBytes
	if maxBytes <= 0 {
		maxBytes = maxTokenBytesDefault
	}
	key := make([]byte, len(cfg.Key))
	copy(key, cfg.Key)
	return &HMACVerifier{
		key:           key,
		issuer:        cfg.Issuer,
		audience:      cfg.Audience,
		now:           now,
		leeway:        cfg.Leeway,
		maxTokenBytes: maxBytes,
	}, nil
}

// Issue signs claims and returns the bearer token text. It exists so that
// development harnesses and tests can mint a credential without a second
// implementation of the token format; production credentials come from the
// federation adapters, not from here.
func (v *HMACVerifier) Issue(claims Claims) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("trust: encoding claims: %w", err)
	}
	body := tokenEncoding.EncodeToString(payload)
	return tokenPrefix + "." + body + "." + tokenEncoding.EncodeToString(v.sign(body)), nil
}

// Verify implements [Verifier].
func (v *HMACVerifier) Verify(_ context.Context, cred Credential) (*Principal, error) {
	if strings.TrimSpace(cred.Token) == "" {
		return nil, ErrNoCredential
	}
	if cred.Scheme != "" && !strings.EqualFold(cred.Scheme, "bearer") {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedScheme, cred.Scheme)
	}
	if len(cred.Token) > v.maxTokenBytes {
		return nil, fmt.Errorf("%w: credential exceeds %d bytes", ErrInvalidCredential, v.maxTokenBytes)
	}

	prefix, rest, ok := strings.Cut(cred.Token, ".")
	if !ok || prefix != tokenPrefix {
		return nil, ErrInvalidCredential
	}
	body, sig, ok := strings.Cut(rest, ".")
	if !ok || body == "" || sig == "" || strings.Contains(sig, ".") {
		return nil, ErrInvalidCredential
	}

	gotSig, err := tokenEncoding.DecodeString(sig)
	if err != nil {
		return nil, ErrInvalidCredential
	}
	if subtle.ConstantTimeCompare(gotSig, v.sign(body)) != 1 {
		return nil, ErrInvalidCredential
	}

	payload, err := tokenEncoding.DecodeString(body)
	if err != nil {
		return nil, ErrInvalidCredential
	}
	var claims Claims
	dec := json.NewDecoder(strings.NewReader(string(payload)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&claims); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCredential, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing content after claims", ErrInvalidCredential)
	}

	if claims.Issuer != v.issuer {
		return nil, fmt.Errorf("%w: %q", ErrWrongIssuer, claims.Issuer)
	}
	wantAudience := v.audience
	if cred.Audience != "" {
		wantAudience = cred.Audience
	}
	if claims.Audience != wantAudience {
		return nil, fmt.Errorf("%w: %q", ErrWrongAudience, claims.Audience)
	}

	issuedAt := time.Unix(claims.IssuedAtUnix, 0).UTC()
	expiresAt := time.Unix(claims.ExpiresAtUnix, 0).UTC()
	if claims.IssuedAtUnix == 0 || claims.ExpiresAtUnix == 0 {
		return nil, fmt.Errorf("%w: validity window is not stated", ErrExpiredCredential)
	}
	now := v.now().UTC()
	if !now.Before(expiresAt.Add(v.leeway)) {
		return nil, fmt.Errorf("%w: expired at %s", ErrExpiredCredential, expiresAt.Format(time.RFC3339))
	}
	if now.Before(issuedAt.Add(-v.leeway)) {
		return nil, fmt.Errorf("%w: not valid until %s", ErrExpiredCredential, issuedAt.Format(time.RFC3339))
	}
	// INTAPI-002: development tokens are profile-contained, not
	// lifetime-free. A window above the dev maximum is refused even when
	// it has not expired yet; machine tokens carry their own
	// fifteen-minute cap in the machine package.
	if expiresAt.Sub(issuedAt) > MaxDevLifetime {
		return nil, fmt.Errorf("%w: window exceeds %s", ErrExpiredCredential, MaxDevLifetime)
	}

	assurance, ok := parseAssurance(claims.Assurance)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrMissingAssurance, claims.Assurance)
	}
	kind, ok := parseSubjectKind(claims.SubjectKind)
	if !ok {
		return nil, fmt.Errorf("%w: subject kind %q", ErrInvalidCredential, claims.SubjectKind)
	}
	method, ok := parseAuthenticationMethod(claims.AuthenticationMethod)
	if !ok {
		return nil, fmt.Errorf("%w: authentication method %q", ErrInvalidCredential, claims.AuthenticationMethod)
	}

	p, err := NewPrincipal(PrincipalSpec{
		Tenant:               values.TenantId(claims.Tenant),
		Subject:              claims.Subject,
		SubjectKind:          kind,
		OrganizationScopeID:  claims.OrganizationScopeID,
		Roles:                claims.Roles,
		AuthorityRefs:        claims.AuthorityRefs,
		Purposes:             claims.Purposes,
		AuthenticationMethod: method,
		Assurance:            assurance,
		SessionRef:           claims.SessionRef,
		DelegationRefs:       claims.DelegationRefs,
		IssuedAt:             issuedAt,
		ExpiresAt:            expiresAt,
		CredentialDigest:     credentialDigest(cred.Token),
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCredential, err)
	}
	return p, nil
}

// sign returns the HMAC-SHA256 tag over the encoded payload.
func (v *HMACVerifier) sign(body string) []byte {
	mac := hmac.New(sha256.New, v.key)
	mac.Write([]byte(tokenPrefix))
	mac.Write([]byte{'.'})
	mac.Write([]byte(body))
	return mac.Sum(nil)
}

// credentialDigest binds a principal to the exact credential presented without
// retaining the credential itself.
func credentialDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "cred:sha256:" + hex.EncodeToString(sum[:])
}

// parseAssurance maps the wire spelling of an assurance level.
func parseAssurance(s string) (Assurance, bool) {
	switch s {
	case "low":
		return AssuranceLow, true
	case "substantial":
		return AssuranceSubstantial, true
	case "high":
		return AssuranceHigh, true
	default:
		return AssuranceUnspecified, false
	}
}

// parseSubjectKind maps the wire spelling of a subject kind.
func parseSubjectKind(s string) (SubjectKind, bool) {
	switch s {
	case "human":
		return SubjectKindHuman, true
	case "service":
		return SubjectKindService, true
	case "agent":
		return SubjectKindAgent, true
	case "integration":
		return SubjectKindIntegration, true
	default:
		return SubjectKindUnspecified, false
	}
}

// parseAuthenticationMethod maps the wire spelling of an authentication method.
func parseAuthenticationMethod(s string) (AuthenticationMethod, bool) {
	switch s {
	case "bearer_token":
		return AuthenticationMethodBearerToken, true
	case "mutual_tls":
		return AuthenticationMethodMutualTLS, true
	default:
		return AuthenticationMethodUnspecified, false
	}
}
