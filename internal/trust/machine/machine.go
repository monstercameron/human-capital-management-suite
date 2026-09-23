// Package machine issues and verifies the asymmetric JWT access tokens
// INTAPI-001 serves to registered machine clients. Tokens are standard
// JWTs (RFC 7519) so external systems use any JOSE library: EdDSA
// (Ed25519) signatures from a rotating server key set published as JWKS,
// at most fifteen minutes of lifetime, carrying identity only (subject,
// client, tenant, session, assurance). Roles and scopes resolve on the
// server from the client registry, never from the token.
//
// Client authentication at the token endpoint (RFC 7523 private_key_jwt
// assertions, DPoP proof keys) verifies with the registered client keys
// through [PublicKeyFromJWK], which understands the OKP, RSA and EC
// shapes the registry admits. Everything here is pure: crypto and time,
// no database, no clock beyond the caller-supplied instant.
package machine

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

func bigInt(raw []byte) *big.Int { return new(big.Int).SetBytes(raw) }

func p256() elliptic.Curve { return elliptic.P256() }

// MaxLifetime is the longest access-token lifetime INTAPI-001 issues. A
// longer request is refused, never truncated: truncation would silently
// mint a different credential than the client asked for.
const MaxLifetime = 15 * time.Minute

// KeyAlgs is the closed set of JWS algorithms this package mints or
// verifies: server tokens are always EdDSA; client assertions and DPoP
// proofs may additionally use RS256 or ES256.
const (
	AlgEdDSA = "EdDSA"
	AlgRS256 = "RS256"
	AlgES256 = "ES256"
)

var (
	ErrMalformedToken  = errors.New("machine: malformed token")
	ErrBadSignature    = errors.New("machine: bad signature")
	ErrUnknownKey      = errors.New("machine: unknown key id")
	ErrWrongIssuer     = errors.New("machine: wrong issuer")
	ErrWrongAudience   = errors.New("machine: wrong audience")
	ErrExpiredToken    = errors.New("machine: token is expired or not yet valid")
	ErrLifetimeTooLong = errors.New("machine: lifetime exceeds the fifteen-minute maximum")
	ErrUnknownAlg      = errors.New("machine: unknown algorithm")
	ErrBadKey          = errors.New("machine: unusable key material")
)

var b64 = base64.RawURLEncoding

// Header is the JOSE header this package mints and accepts.
type Header struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
	KID string `json:"kid,omitempty"`
}

// Claims are the access-token claims INTAPI-001 mints. Subject, Client,
// Tenant, Session and Assurance are identity; Confirmation optionally
// sender-constrains the token (RFC 9449 jkt or RFC 8705 x5t#S256).
type Claims struct {
	Issuer       string         `json:"iss"`
	Audience     []string       `json:"aud"`
	Subject      string         `json:"sub"`
	Client       string         `json:"client_id"`
	Tenant       string         `json:"tenant"`
	Session      string         `json:"sid"`
	Assurance    string         `json:"assurance"`
	IssuedAt     int64          `json:"iat"`
	ExpiresAt    int64          `json:"exp"`
	TokenID      string         `json:"jti"`
	Confirmation map[string]any `json:"cnf,omitempty"`
}

func splitCompact(token string) (headerB64, payloadB64, sig []byte, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, nil, nil, fmt.Errorf("%w: want three segments", ErrMalformedToken)
	}
	hb, err := b64.DecodeString(parts[0])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: header: %v", ErrMalformedToken, err)
	}
	pb, err := b64.DecodeString(parts[1])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: payload: %v", ErrMalformedToken, err)
	}
	sig, err = b64.DecodeString(parts[2])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: signature: %v", ErrMalformedToken, err)
	}
	return hb, pb, sig, nil
}

func decodeHeader(raw []byte) (Header, error) {
	var h Header
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&h); err != nil {
		return Header{}, fmt.Errorf("%w: header: %v", ErrMalformedToken, err)
	}
	if h.Typ != "" && h.Typ != "JWT" && h.Typ != "dpop+jwt" {
		return Header{}, fmt.Errorf("%w: type %q", ErrMalformedToken, h.Typ)
	}
	switch h.Alg {
	case AlgEdDSA, AlgRS256, AlgES256:
	default:
		return Header{}, fmt.Errorf("%w: %q", ErrUnknownAlg, h.Alg)
	}
	return h, nil
}

// PublicKeyFromJWK parses the public half of a registry JWK. Only public
// key parameters are read; a JWK carrying private material is refused.
func PublicKeyFromJWK(raw json.RawMessage) (crypto.PublicKey, string, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, "", fmt.Errorf("%w: JWK: %v", ErrBadKey, err)
	}
	for _, priv := range []string{"d", "p", "q", "dp", "dq", "qi", "oth", "k"} {
		if _, ok := m[priv]; ok {
			return nil, "", fmt.Errorf("%w: private material", ErrBadKey)
		}
	}
	str := func(k string) (string, bool) {
		v, ok := m[k].(string)
		return v, ok && v != ""
	}
	kty, _ := str("kty")
	switch kty {
	case "OKP":
		crv, _ := str("crv")
		x, ok := str("x")
		if crv != "Ed25519" || !ok {
			return nil, "", fmt.Errorf("%w: only Ed25519 OKP keys", ErrBadKey)
		}
		raw, err := b64.DecodeString(x)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			return nil, "", fmt.Errorf("%w: bad Ed25519 x", ErrBadKey)
		}
		return ed25519.PublicKey(raw), AlgEdDSA, nil
	case "RSA":
		n, okN := str("n")
		e, okE := str("e")
		if !okN || !okE {
			return nil, "", fmt.Errorf("%w: RSA needs n and e", ErrBadKey)
		}
		nb, err := b64.DecodeString(n)
		if err != nil {
			return nil, "", fmt.Errorf("%w: bad RSA n", ErrBadKey)
		}
		eb, err := b64.DecodeString(e)
		if err != nil {
			return nil, "", fmt.Errorf("%w: bad RSA e", ErrBadKey)
		}
		nInt := bigInt(nb)
		eInt := bigInt(eb)
		if !eInt.IsInt64() {
			return nil, "", fmt.Errorf("%w: bad RSA e", ErrBadKey)
		}
		return &rsa.PublicKey{N: nInt, E: int(eInt.Int64())}, AlgRS256, nil
	case "EC":
		crv, _ := str("crv")
		x, okX := str("x")
		y, okY := str("y")
		if crv != "P-256" || !okX || !okY {
			return nil, "", fmt.Errorf("%w: only P-256 EC keys", ErrBadKey)
		}
		xb, err := b64.DecodeString(x)
		if err != nil {
			return nil, "", fmt.Errorf("%w: bad EC x", ErrBadKey)
		}
		yb, err := b64.DecodeString(y)
		if err != nil {
			return nil, "", fmt.Errorf("%w: bad EC y", ErrBadKey)
		}
		return &ecdsa.PublicKey{Curve: p256(), X: bigInt(xb), Y: bigInt(yb)}, AlgES256, nil
	default:
		return nil, "", fmt.Errorf("%w: kty %q", ErrBadKey, kty)
	}
}

// Thumbprint returns the RFC 7638 JWK SHA-256 thumbprint of raw: the stable
// key identifier DPoP binds and JWKS publishes.
func Thumbprint(raw json.RawMessage) (string, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", fmt.Errorf("%w: JWK: %v", ErrBadKey, err)
	}
	kty, _ := m["kty"].(string)
	var required []string
	switch kty {
	case "RSA":
		required = []string{"e", "kty", "n"}
	case "EC":
		required = []string{"crv", "kty", "x", "y"}
	case "OKP":
		required = []string{"crv", "kty", "x"}
	default:
		return "", fmt.Errorf("%w: kty %q", ErrBadKey, kty)
	}
	ordered := make(map[string]any, len(required))
	for _, k := range required {
		v, ok := m[k].(string)
		if !ok || v == "" {
			return "", fmt.Errorf("%w: missing %s", ErrBadKey, k)
		}
		ordered[k] = v
	}
	canonical, err := json.Marshal(ordered)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrBadKey, err)
	}
	// json.Marshal sorts map keys, which is the RFC 7638 ordering.
	sum := sha256.Sum256(canonical)
	return b64.EncodeToString(sum[:]), nil
}

// VerifyAssertionSignature checks a client-assertion or DPoP-proof
// signature over signingInput. It is the registry path's entry point:
// callers resolve the key from the client registry first.
func VerifyAssertionSignature(alg string, key crypto.PublicKey, signingInput string, sig []byte) error {
	return verifySignature(alg, key, []byte(signingInput), sig)
}

func verifySignature(alg string, key crypto.PublicKey, signingInput, sig []byte) error {
	switch alg {
	case AlgEdDSA:
		pub, ok := key.(ed25519.PublicKey)
		if !ok || len(pub) != ed25519.PublicKeySize {
			return fmt.Errorf("%w: not an Ed25519 key", ErrBadKey)
		}
		if !ed25519.Verify(pub, signingInput, sig) {
			return ErrBadSignature
		}
		return nil
	case AlgRS256:
		pub, ok := key.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("%w: not an RSA key", ErrBadKey)
		}
		digest := sha256.Sum256(signingInput)
		if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig); err != nil {
			return ErrBadSignature
		}
		return nil
	case AlgES256:
		pub, ok := key.(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("%w: not a P-256 key", ErrBadKey)
		}
		digest := sha256.Sum256(signingInput)
		if !ecdsa.VerifyASN1(pub, digest[:], sig) {
			return ErrBadSignature
		}
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrUnknownAlg, alg)
	}
}
