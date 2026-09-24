// Package oidcsession binds a verified OIDC principal to an AUTHN-004 session
// and issues a short-lived bearer credential for browser requests.
package oidcsession

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/session"
)

const tokenPrefix = "hcm-oidc-session-v1"
const defaultLifetime = 10 * time.Minute
const maxTokenBytes = 16 << 10

var (
	ErrInvalidConfig = errors.New("oidcsession: invalid authority configuration")
	ErrInvalidToken  = errors.New("oidcsession: invalid session credential")
)

// Sessions is the AUTHN-004 session contract needed by this authority.
// Both Manager and PersistentManager implement it; production composition
// must supply PersistentManager.
type Sessions interface {
	Create(context.Context, session.CreateSpec) (session.Record, session.RefreshToken, error)
	Validate(context.Context, session.ID, session.Claim) (session.Record, error)
	Revoke(context.Context, session.ID, string) (session.Record, error)
}

type Config struct {
	Sessions Sessions
	Fallback trust.Verifier
	// SessionTenant maps the principal tenant key to the storage tenant UUID
	// used by AUTHN-004. Nil keeps the identity mapping for memory stores.
	SessionTenant func(values.TenantId) values.TenantId
	Key           []byte
	Issuer        string
	Audience      string
	Now           func() time.Time
	Lifetime      time.Duration
}

type Authority struct {
	sessions      Sessions
	fallback      trust.Verifier
	sessionTenant func(values.TenantId) values.TenantId
	key           []byte
	issuer        string
	audience      string
	now           func() time.Time
	lifetime      time.Duration
}

type claims struct {
	Issuer               string            `json:"iss"`
	Audience             string            `json:"aud"`
	Tenant               string            `json:"tenant"`
	Subject              string            `json:"sub"`
	SubjectKind          string            `json:"sub_kind"`
	OrganizationScopeID  string            `json:"org_scope,omitempty"`
	Roles                []string          `json:"roles,omitempty"`
	AuthorityRefs        []string          `json:"authority_refs,omitempty"`
	Purposes             []string          `json:"purposes,omitempty"`
	AuthenticationMethod string            `json:"authn_method"`
	Assurance            string            `json:"assurance"`
	Session              string            `json:"sid"`
	IssuedAt             int64             `json:"iat"`
	ExpiresAt            int64             `json:"exp"`
	Confirmation         map[string]string `json:"cnf,omitempty"`
	DelegationRefs       []string          `json:"delegation_refs,omitempty"`
}

func New(cfg Config) (*Authority, error) {
	if cfg.Sessions == nil || len(cfg.Key) < 32 || strings.TrimSpace(cfg.Issuer) == "" || strings.TrimSpace(cfg.Audience) == "" {
		return nil, ErrInvalidConfig
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	lifetime := cfg.Lifetime
	if lifetime <= 0 {
		lifetime = defaultLifetime
	}
	if lifetime > 12*time.Hour {
		return nil, fmt.Errorf("%w: lifetime exceeds session maximum", ErrInvalidConfig)
	}
	mapTenant := cfg.SessionTenant
	if mapTenant == nil {
		mapTenant = func(tenant values.TenantId) values.TenantId { return tenant }
	}
	return &Authority{sessions: cfg.Sessions, fallback: cfg.Fallback, sessionTenant: mapTenant, key: append([]byte(nil), cfg.Key...), issuer: cfg.Issuer, audience: cfg.Audience, now: now, lifetime: lifetime}, nil
}

// Issue creates a durable session family and returns a signed access
// credential. The refresh secret is intentionally discarded: browser access
// credentials expire quickly and require a new OIDC authorization to renew.
func (a *Authority) Issue(ctx context.Context, principal *trust.Principal) (string, error) {
	if a == nil || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Assurance() == trust.AssuranceUnspecified {
		return "", ErrInvalidConfig
	}
	now := a.now().UTC()
	requestedExpiry := now.Add(a.lifetime)
	id, err := session.NewID()
	if err != nil {
		return "", fmt.Errorf("oidcsession: reserve session id: %w", err)
	}
	c := claims{
		Issuer: a.issuer, Audience: a.audience, Tenant: principal.Tenant().String(), Subject: principal.Subject(),
		SubjectKind: principal.SubjectKind().String(), OrganizationScopeID: principal.OrganizationScopeID(),
		Roles: principal.Roles(), AuthorityRefs: principal.AuthorityRefs(), Purposes: principal.Purposes(),
		AuthenticationMethod: principal.AuthenticationMethod().String(), Assurance: principal.Assurance().String(),
		Session: string(id), IssuedAt: now.Unix(), ExpiresAt: requestedExpiry.Unix(),
		Confirmation: principal.Confirmation(), DelegationRefs: principal.DelegationRefs(),
	}
	token, err := a.sign(c)
	if err != nil {
		return "", err
	}
	principalWithSession, err := principalFor(c, token)
	if err != nil {
		return "", err
	}
	_, _, err = a.sessions.Create(ctx, session.CreateSpec{
		ID: id, Tenant: a.sessionTenant(principal.Tenant()), Subject: principal.Subject(),
		PrincipalFingerprint: principalWithSession.Fingerprint(), Assurance: principal.Assurance(),
		IdleTimeout: a.lifetime, AbsoluteTimeout: a.lifetime,
	})
	if err != nil {
		return "", fmt.Errorf("oidcsession: create session: %w", err)
	}
	return token, nil
}

func (a *Authority) Verify(ctx context.Context, cred trust.Credential) (*trust.Principal, error) {
	if a == nil || cred.Token == "" || len(cred.Token) > maxTokenBytes || (cred.Scheme != "" && !strings.EqualFold(cred.Scheme, "bearer")) {
		return nil, ErrInvalidToken
	}
	if cred.Audience != "" && cred.Audience != a.audience {
		return nil, ErrInvalidToken
	}
	if !strings.HasPrefix(cred.Token, tokenPrefix+".") {
		if a.fallback == nil {
			return nil, ErrInvalidToken
		}
		return a.fallback.Verify(ctx, cred)
	}
	c, err := a.decode(cred.Token)
	if err != nil {
		return nil, err
	}
	now := a.now().UTC()
	if c.Issuer != a.issuer || c.Audience != a.audience || c.Subject == "" || c.Session == "" || c.IssuedAt > now.Unix() || c.ExpiresAt <= now.Unix() || c.ExpiresAt-c.IssuedAt > int64(a.lifetime/time.Second)+1 {
		return nil, ErrInvalidToken
	}
	tenant := values.TenantId(c.Tenant)
	assurance, ok := parseAssurance(c.Assurance)
	if !ok || tenant.Validate() != nil {
		return nil, ErrInvalidToken
	}
	principal, err := principalFor(c, cred.Token)
	if err != nil {
		return nil, ErrInvalidToken
	}
	rec, err := a.sessions.Validate(ctx, session.ID(c.Session), session.Claim{Tenant: a.sessionTenant(tenant), Assurance: assurance})
	if err != nil || rec.Subject() != c.Subject || rec.PrincipalFingerprint() != principal.Fingerprint() {
		return nil, ErrInvalidToken
	}
	return principal, nil
}

// Revoke terminates the durable AUTHN-004 family bound to credential. It
// accepts only this authority's signed credential; fallback credentials are
// intentionally outside this session authority.
func (a *Authority) Revoke(ctx context.Context, credential string) error {
	if a == nil || a.sessions == nil {
		return ErrInvalidToken
	}
	principal, err := a.Verify(ctx, trust.Credential{Scheme: "Bearer", Token: credential, Audience: a.audience})
	if err != nil {
		return err
	}
	_, err = a.sessions.Revoke(ctx, session.ID(principal.SessionRef()), session.ReasonManualRevoke)
	return err
}

func principalFor(c claims, token string) (*trust.Principal, error) {
	tenant := values.TenantId(c.Tenant)
	assurance, ok := parseAssurance(c.Assurance)
	if !ok || tenant.Validate() != nil {
		return nil, ErrInvalidToken
	}
	kind, ok := parseSubjectKind(c.SubjectKind)
	if !ok {
		return nil, ErrInvalidToken
	}
	method, ok := parseAuthenticationMethod(c.AuthenticationMethod)
	if !ok {
		return nil, ErrInvalidToken
	}
	sum := sha256.Sum256([]byte(token))
	return trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: tenant, Subject: c.Subject, SubjectKind: kind, OrganizationScopeID: c.OrganizationScopeID,
		Roles: c.Roles, AuthorityRefs: c.AuthorityRefs, Purposes: c.Purposes,
		AuthenticationMethod: method, Assurance: assurance, SessionRef: c.Session,
		IssuedAt: time.Unix(c.IssuedAt, 0).UTC(), ExpiresAt: time.Unix(c.ExpiresAt, 0).UTC(),
		CredentialDigest: "cred:sha256:" + hex.EncodeToString(sum[:]), Confirmation: c.Confirmation, DelegationRefs: c.DelegationRefs,
	})
}

func (a *Authority) sign(c claims) (string, error) {
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	input := tokenPrefix + "." + body
	mac := hmac.New(sha256.New, a.key)
	_, _ = mac.Write([]byte(input))
	return input + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (a *Authority) decode(token string) (claims, error) {
	var c claims
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != tokenPrefix || parts[1] == "" || parts[2] == "" {
		return c, ErrInvalidToken
	}
	sig, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil {
		return c, ErrInvalidToken
	}
	mac := hmac.New(sha256.New, a.key)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return c, ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		return c, ErrInvalidToken
	}
	dec := json.NewDecoder(strings.NewReader(string(payload)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return claims{}, ErrInvalidToken
	}
	return c, nil
}

func parseAssurance(v string) (trust.Assurance, bool) {
	for _, a := range []trust.Assurance{trust.AssuranceLow, trust.AssuranceSubstantial, trust.AssuranceHigh} {
		if a.String() == v {
			return a, true
		}
	}
	return trust.AssuranceUnspecified, false
}
func parseSubjectKind(v string) (trust.SubjectKind, bool) {
	for _, k := range []trust.SubjectKind{trust.SubjectKindHuman, trust.SubjectKindService, trust.SubjectKindAgent, trust.SubjectKindIntegration} {
		if k.String() == v {
			return k, true
		}
	}
	return trust.SubjectKindUnspecified, false
}
func parseAuthenticationMethod(v string) (trust.AuthenticationMethod, bool) {
	for _, m := range []trust.AuthenticationMethod{trust.AuthenticationMethodBearerToken, trust.AuthenticationMethodMutualTLS} {
		if m.String() == v {
			return m, true
		}
	}
	return trust.AuthenticationMethodUnspecified, false
}

var _ trust.Verifier = (*Authority)(nil)
