package machine

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

// ServerKey is one token-signing key: the current key signs, retired keys
// verify only until every token they signed has expired.
type ServerKey struct {
	KID       string
	Private   ed25519.PrivateKey
	Public    ed25519.PublicKey
	RetireAt  time.Time
	PublishTo time.Time
}

// GenerateServerKey mints one Ed25519 signing key publishing until publishTo
// and retiring at retireAt.
func GenerateServerKey(kid string, retireAt, publishTo time.Time) (ServerKey, error) {
	if kid == "" || retireAt.IsZero() || publishTo.IsZero() || !publishTo.After(retireAt) {
		return ServerKey{}, fmt.Errorf("%w: key needs an id, a retirement and a later unpublish instant", ErrBadKey)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return ServerKey{}, fmt.Errorf("%w: %v", ErrBadKey, err)
	}
	return ServerKey{KID: kid, Private: priv, Public: pub, RetireAt: retireAt, PublishTo: publishTo}, nil
}

// IssueRequest is everything the token endpoint resolved before minting: an
// already-authenticated, authorized, still-active client.
type IssueRequest struct {
	Issuer    string
	Audience  []string
	Subject   string
	Client    string
	Tenant    string
	Session   string
	Assurance string
	TokenID   string
	// Confirmation sender-constrains the token when the client proved
	// possession (DPoP jkt or mTLS x5t#S256). Nil means bearer.
	Confirmation map[string]any
	// Lifetime caps at MaxLifetime. Zero or negative is refused.
	Lifetime time.Duration
}

// Issuer signs access tokens with the current server key. It is safe for
// concurrent use; rotation replaces the whole value under mutex.
type Issuer struct {
	mu   sync.Mutex
	keys []ServerKey
	now  func() time.Time
}

// NewIssuer holds keys with at least one current key. now may be nil.
func NewIssuer(keys []ServerKey, now func() time.Time) (*Issuer, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: at least one signing key", ErrBadKey)
	}
	if now == nil {
		now = time.Now
	}
	return &Issuer{keys: append([]ServerKey(nil), keys...), now: now}, nil
}

// Rotate replaces the key set. The first key signs; the rest verify only.
func (is *Issuer) Rotate(keys []ServerKey) error {
	if len(keys) == 0 {
		return fmt.Errorf("%w: at least one signing key", ErrBadKey)
	}
	is.mu.Lock()
	defer is.mu.Unlock()
	is.keys = append([]ServerKey(nil), keys...)
	return nil
}

func (is *Issuer) current() ServerKey { return is.keys[0] }

// Issue mints one access token. A lifetime above MaxLifetime is refused,
// never truncated.
func (is *Issuer) Issue(req IssueRequest) (string, error) {
	if req.Issuer == "" || len(req.Audience) == 0 || req.Subject == "" || req.Client == "" ||
		req.Tenant == "" || req.Session == "" || req.Assurance == "" || req.TokenID == "" {
		return "", fmt.Errorf("%w: identity fields are required", ErrMalformedToken)
	}
	if req.Lifetime <= 0 || req.Lifetime > MaxLifetime {
		return "", fmt.Errorf("%w: %s", ErrLifetimeTooLong, req.Lifetime)
	}
	is.mu.Lock()
	key := is.current()
	is.mu.Unlock()
	now := is.now().UTC()
	claims := Claims{
		Issuer: req.Issuer, Audience: append([]string(nil), req.Audience...),
		Subject: req.Subject, Client: req.Client, Tenant: req.Tenant, Session: req.Session,
		Assurance: req.Assurance, IssuedAt: now.Unix(), ExpiresAt: now.Add(req.Lifetime).Unix(),
		TokenID: req.TokenID, Confirmation: req.Confirmation,
	}
	header, err := json.Marshal(Header{Alg: AlgEdDSA, Typ: "JWT", KID: key.KID})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	input := b64.EncodeToString(header) + "." + b64.EncodeToString(payload)
	sig := ed25519.Sign(key.Private, []byte(input))
	return input + "." + b64.EncodeToString(sig), nil
}

// VerifyRequest binds verification to the listener's expectations.
type VerifyRequest struct {
	Issuer   string
	Audience []string
	// Leeway tolerates clock skew on the validity window. Zero is exact.
	Leeway time.Duration
}

// Verifier checks access tokens against every published server key. It is
// safe for concurrent use.
type Verifier struct {
	mu   sync.Mutex
	keys []ServerKey
	now  func() time.Time
}

// NewVerifier holds keys with at least one entry. now may be nil.
func NewVerifier(keys []ServerKey, now func() time.Time) (*Verifier, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: at least one verification key", ErrBadKey)
	}
	if now == nil {
		now = time.Now
	}
	return &Verifier{keys: append([]ServerKey(nil), keys...), now: now}, nil
}

// Rotate replaces the key set.
func (v *Verifier) Rotate(keys []ServerKey) error {
	if len(keys) == 0 {
		return fmt.Errorf("%w: at least one verification key", ErrBadKey)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.keys = append([]ServerKey(nil), keys...)
	return nil
}

// Verify authenticates one access token and returns its identity claims.
// The returned principal carries no authority: roles and scopes resolve on
// the server from the client registry.
func (v *Verifier) Verify(token string, req VerifyRequest) (Claims, error) {
	hb, pb, sig, err := splitCompact(token)
	if err != nil {
		return Claims{}, err
	}
	h, err := decodeHeader(hb)
	if err != nil {
		return Claims{}, err
	}
	if h.Alg != AlgEdDSA {
		return Claims{}, fmt.Errorf("%w: server tokens are EdDSA", ErrUnknownAlg)
	}
	v.mu.Lock()
	keys := append([]ServerKey(nil), v.keys...)
	v.mu.Unlock()
	var matched *ServerKey
	if h.KID != "" {
		for i := range keys {
			if keys[i].KID == h.KID {
				matched = &keys[i]
				break
			}
		}
		if matched == nil {
			return Claims{}, fmt.Errorf("%w: %q", ErrUnknownKey, h.KID)
		}
	}
	now := v.now().UTC()
	candidates := keys
	if matched != nil {
		candidates = []ServerKey{*matched}
	}
	verified := false
	for _, k := range candidates {
		if len(k.Public) != ed25519.PublicKeySize {
			continue
		}
		if ed25519.Verify(k.Public, []byte(b64.EncodeToString(hb)+"."+b64.EncodeToString(pb)), sig) {
			verified = true
			break
		}
	}
	if !verified {
		return Claims{}, ErrBadSignature
	}
	var c Claims
	dec := json.NewDecoder(bytesReader(pb))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(&c); err != nil {
		return Claims{}, fmt.Errorf("%w: payload: %v", ErrMalformedToken, err)
	}
	if req.Issuer != "" && c.Issuer != req.Issuer {
		return Claims{}, fmt.Errorf("%w: %q", ErrWrongIssuer, c.Issuer)
	}
	if len(req.Audience) > 0 && !slices.ContainsFunc(req.Audience, func(a string) bool {
		return slices.Contains(c.Audience, a)
	}) {
		return Claims{}, fmt.Errorf("%w: %q", ErrWrongAudience, c.Audience)
	}
	iat := time.Unix(c.IssuedAt, 0).UTC()
	exp := time.Unix(c.ExpiresAt, 0).UTC()
	if now.Before(iat.Add(-req.Leeway)) || !now.Before(exp.Add(req.Leeway)) {
		return Claims{}, fmt.Errorf("%w", ErrExpiredToken)
	}
	if exp.Sub(iat) > MaxLifetime {
		return Claims{}, fmt.Errorf("%w", ErrLifetimeTooLong)
	}
	if c.Subject == "" || c.Client == "" || c.Tenant == "" || c.Session == "" || c.TokenID == "" {
		return Claims{}, fmt.Errorf("%w: identity fields", ErrMalformedToken)
	}
	return c, nil
}

var ErrNoPublicKeys = errors.New("machine: no keys to publish")

// JWK is the public half of one server key in JWKS form.
type JWK struct {
	KeyType   string `json:"kty"`
	Curve     string `json:"crv"`
	KeyID     string `json:"kid"`
	Algorithm string `json:"alg"`
	Use       string `json:"use"`
	X         string `json:"x"`
}

// KeySet is a JWKS document.
type KeySet struct {
	Keys []JWK `json:"keys"`
}

// Publish returns the JWKS document: every key whose PublishTo still
// covers now, so a retired key stays published until every token it signed
// has expired.
func (is *Issuer) Publish(now time.Time) (KeySet, error) {
	is.mu.Lock()
	defer is.mu.Unlock()
	out := KeySet{}
	for _, k := range is.keys {
		if len(k.Public) != ed25519.PublicKeySize || k.KID == "" {
			continue
		}
		if !now.Before(k.PublishTo) {
			continue
		}
		out.Keys = append(out.Keys, JWK{
			KeyType: "OKP", Curve: "Ed25519", KeyID: k.KID,
			Algorithm: AlgEdDSA, Use: "sig", X: b64.EncodeToString(k.Public),
		})
	}
	if len(out.Keys) == 0 {
		return KeySet{}, ErrNoPublicKeys
	}
	return out, nil
}
