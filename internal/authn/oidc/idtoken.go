package oidc

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry"
	trustfederation "github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

// maxIDTokenBytes bounds ID token parsing before any allocation that
// depends on the token's own length, matching internal/trust/federation's
// own default assertion size bound.
const maxIDTokenBytes = 16 << 10

// joseHeader is the JWS protected header this package accepts for an ID
// token. Decoding is strict and the field set is closed, mirroring
// internal/trust/federation's own joseHeader -- duplicated rather than
// imported because that type is unexported; see doc.go.
type joseHeader struct {
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`
	Type      string `json:"typ,omitempty"`
}

// splitCompactJWS splits a compact JWS into its decoded header, the exact
// still-encoded signing input (header.payload), the decoded payload and the
// decoded signature. This duplicates internal/trust/federation.splitJWS
// (unexported there); see doc.go for why.
func splitCompactJWS(raw string, maxBytes int) (h joseHeader, signingInput string, payload []byte, sig []byte, err error) {
	if raw == "" {
		return joseHeader{}, "", nil, nil, fmt.Errorf("%w: empty", ErrIDTokenMalformed)
	}
	if len(raw) > maxBytes {
		return joseHeader{}, "", nil, nil, fmt.Errorf("%w: exceeds %d bytes", ErrIDTokenMalformed, maxBytes)
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return joseHeader{}, "", nil, nil, fmt.Errorf("%w: expected 3 segments, got %d", ErrIDTokenMalformed, len(parts))
	}
	if parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return joseHeader{}, "", nil, nil, fmt.Errorf("%w: empty segment", ErrIDTokenMalformed)
	}

	headerBytes, err := b64.DecodeString(parts[0])
	if err != nil {
		return joseHeader{}, "", nil, nil, fmt.Errorf("%w: header encoding: %v", ErrIDTokenMalformed, err)
	}
	dec := json.NewDecoder(strings.NewReader(string(headerBytes)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&h); err != nil {
		return joseHeader{}, "", nil, nil, fmt.Errorf("%w: header: %v", ErrIDTokenMalformed, err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return joseHeader{}, "", nil, nil, fmt.Errorf("%w: trailing content after header", ErrIDTokenMalformed)
	}

	payload, err = b64.DecodeString(parts[1])
	if err != nil {
		return joseHeader{}, "", nil, nil, fmt.Errorf("%w: payload encoding: %v", ErrIDTokenMalformed, err)
	}
	sig, err = b64.DecodeString(parts[2])
	if err != nil {
		return joseHeader{}, "", nil, nil, fmt.Errorf("%w: signature encoding: %v", ErrIDTokenMalformed, err)
	}
	return h, parts[0] + "." + parts[1], payload, sig, nil
}

// verifyIDTokenSignature checks sig over signingInput under alg using pub.
// It duplicates internal/trust/federation.verifySignature's three cases
// (unexported there; see doc.go) and fails closed identically: a key of the
// wrong concrete type for alg, or a signature of the wrong length, is
// rejected the same as a cryptographically wrong signature.
func verifyIDTokenSignature(alg trustfederation.Algorithm, pub crypto.PublicKey, signingInput string, sig []byte) error {
	switch alg {
	case trustfederation.AlgRS256:
		key, ok := pub.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("%w: RS256 needs an RSA public key", ErrUnsupportedAlgorithm)
		}
		sum := sha256.Sum256([]byte(signingInput))
		if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig); err != nil {
			return ErrSignatureInvalid
		}
		return nil
	case trustfederation.AlgES256:
		key, ok := pub.(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("%w: ES256 needs an ECDSA public key", ErrUnsupportedAlgorithm)
		}
		if key.Curve != elliptic.P256() {
			return fmt.Errorf("%w: ES256 needs a P-256 key", ErrUnsupportedAlgorithm)
		}
		if len(sig) != 64 {
			return fmt.Errorf("%w: ES256 signature must be 64 bytes, got %d", ErrSignatureInvalid, len(sig))
		}
		r := new(big.Int).SetBytes(sig[:32])
		s := new(big.Int).SetBytes(sig[32:])
		sum := sha256.Sum256([]byte(signingInput))
		if !ecdsa.Verify(key, sum[:], r, s) {
			return ErrSignatureInvalid
		}
		return nil
	case trustfederation.AlgEdDSA:
		key, ok := pub.(ed25519.PublicKey)
		if !ok {
			return fmt.Errorf("%w: EdDSA needs an Ed25519 public key", ErrUnsupportedAlgorithm)
		}
		if len(sig) != ed25519.SignatureSize {
			return fmt.Errorf("%w: EdDSA signature must be %d bytes, got %d", ErrSignatureInvalid, ed25519.SignatureSize, len(sig))
		}
		if !ed25519.Verify(key, []byte(signingInput), sig) {
			return ErrSignatureInvalid
		}
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedAlgorithm, alg)
	}
}

// keyWithinWindow reports whether k is inside its own declared
// NotBefore/NotAfter validity window at "at". It duplicates
// [trustfederation.SigningKey]'s unexported validAt method against that
// type's exported fields.
func keyWithinWindow(k trustfederation.SigningKey, at time.Time) bool {
	if !k.NotBefore.IsZero() && at.Before(k.NotBefore) {
		return false
	}
	if !k.NotAfter.IsZero() && !at.Before(k.NotAfter) {
		return false
	}
	return true
}

// stringOrList decodes an OIDC claim that RFC 7519/OIDC Core allow to be
// either a single JSON string or a JSON array of strings -- exactly the
// shape the "aud" claim takes.
type stringOrList []string

func (s *stringOrList) UnmarshalJSON(b []byte) error {
	var single string
	if err := json.Unmarshal(b, &single); err == nil {
		*s = []string{single}
		return nil
	}
	var list []string
	if err := json.Unmarshal(b, &list); err != nil {
		return fmt.Errorf("value is neither a JSON string nor an array of strings")
	}
	*s = list
	return nil
}

// stdIDTokenClaims is the set of OIDC Core standard claims this package's ID
// token validation inspects directly. Custom/tenant-specific claims are
// read separately from the raw decoded claim map (see claims.go) through
// the issuer's governed [issuerregistry.ClaimMapping] list, never from this
// struct, which is intentionally closed to exactly the claims OIDC Core
// itself defines the meaning of.
type stdIDTokenClaims struct {
	Issuer          string       `json:"iss"`
	Subject         string       `json:"sub"`
	Audience        stringOrList `json:"aud"`
	ExpiresAtUnix   int64        `json:"exp"`
	IssuedAtUnix    int64        `json:"iat"`
	NotBeforeUnix   int64        `json:"nbf,omitempty"`
	Nonce           string       `json:"nonce,omitempty"`
	AtHash          string       `json:"at_hash,omitempty"`
	AuthorizedParty string       `json:"azp,omitempty"`

	IssuedAt  time.Time `json:"-"`
	ExpiresAt time.Time `json:"-"`
}

// computeAtHash implements OpenID Connect Core §3.1.3.6's at_hash: the
// base64url encoding of the leftmost half of the SHA-256 hash of the ASCII
// octets of accessToken. OIDC Core defines the hash algorithm to match the
// ID token's signing algorithm's associated hash (SHA-256 for RS256/ES256);
// it does not define one for EdDSA. This package uses SHA-256 for all three
// of its closed algorithms, matching common practice and every other use of
// SHA-256 in internal/trust/federation's own RS256/ES256 verification.
func computeAtHash(accessToken string) string {
	sum := sha256.Sum256([]byte(accessToken))
	half := sum[:len(sum)/2]
	return b64.EncodeToString(half)
}

// constantTimeStringsEqual reports whether a and b are equal, comparing in
// constant time when their lengths match (mismatched lengths are safe to
// short-circuit: length alone is not the secret being protected here).
func constantTimeStringsEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// idTokenResult is what [validateIDToken] hands back once every check has
// passed: the fully decoded raw claim map (for claims.go's governed claim
// mapping) alongside the standard claims this package itself validated.
type idTokenResult struct {
	raw      map[string]json.RawMessage
	standard stdIDTokenClaims
}

// validateIDToken verifies rawToken as a compact JWS ID token against
// issuer's pinned material and governance bounds, and checks every OIDC
// Core validation rule this flow is responsible for: signature and
// algorithm (including issuer-declared-algorithm-set downgrade
// resistance), issuer, audience, validity window with the issuer's own
// clock-skew bound, nonce, and at_hash when the token declares one.
//
// Validation order matters for the security posture, not only for error
// messages: the header is decoded only far enough to learn the algorithm
// and key id, the algorithm is checked against both the closed
// RS256/ES256/EdDSA vocabulary and this specific issuer's declared set
// before any key is resolved, the signature is verified against a key
// [keys] resolved for this exact issuer, and only afterward are the
// remaining claims (issuer, audience, validity window, nonce, at_hash)
// trusted.
func validateIDToken(ctx context.Context, keys trustfederation.IssuerResolver, issuer issuerregistry.Issuer, expectedClientID, rawToken, expectedNonce, accessToken string, now time.Time) (idTokenResult, error) {
	h, signingInput, payload, sig, err := splitCompactJWS(rawToken, maxIDTokenBytes)
	if err != nil {
		return idTokenResult{}, err
	}
	if h.Type != "" && h.Type != "JWT" {
		return idTokenResult{}, fmt.Errorf("%w: unexpected typ %q", ErrIDTokenMalformed, h.Type)
	}
	alg := trustfederation.Algorithm(h.Algorithm)
	if !alg.Valid() {
		return idTokenResult{}, fmt.Errorf("%w: %q is not in the RS256/ES256/EdDSA vocabulary", ErrUnsupportedAlgorithm, h.Algorithm)
	}
	if !slices.Contains(issuer.Algorithms, alg) {
		return idTokenResult{}, fmt.Errorf("%w: issuer %q does not declare %q", ErrUnsupportedAlgorithm, issuer.IssuerURL, alg)
	}
	if h.KeyID == "" {
		return idTokenResult{}, fmt.Errorf("%w: no key id", ErrIDTokenMalformed)
	}

	if keys == nil {
		return idTokenResult{}, fmt.Errorf("%w: no issuer key resolver configured", ErrKeyNotFound)
	}
	resolved, err := keys.ResolveIssuerKeys(ctx, issuer.IssuerURL)
	if err != nil {
		return idTokenResult{}, fmt.Errorf("%w: %v", ErrKeyNotFound, err)
	}
	var key *trustfederation.SigningKey
	for i := range resolved {
		if resolved[i].ID == trustfederation.KeyID(h.KeyID) {
			key = &resolved[i]
			break
		}
	}
	if key == nil {
		return idTokenResult{}, fmt.Errorf("%w: issuer %q key %q", ErrKeyNotFound, issuer.IssuerURL, h.KeyID)
	}
	if key.Algorithm != alg {
		return idTokenResult{}, fmt.Errorf("%w: header declares %q, resolved key is %q", ErrAlgorithmDowngrade, alg, key.Algorithm)
	}
	if !keyWithinWindow(*key, now) {
		return idTokenResult{}, fmt.Errorf("%w: key %q for issuer %q", ErrKeyExpired, h.KeyID, issuer.IssuerURL)
	}
	if err := verifyIDTokenSignature(alg, key.Public, signingInput, sig); err != nil {
		return idTokenResult{}, err
	}

	// Everything below this line trusts the claims: the signature over
	// them has verified against a key bound to the exact issuer they
	// claim, under an algorithm both the closed vocabulary and this
	// issuer's own configuration permit.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return idTokenResult{}, fmt.Errorf("%w: claims: %v", ErrIDTokenMalformed, err)
	}
	var std stdIDTokenClaims
	if err := json.Unmarshal(payload, &std); err != nil {
		return idTokenResult{}, fmt.Errorf("%w: claims: %v", ErrIDTokenMalformed, err)
	}
	std.IssuedAt = time.Unix(std.IssuedAtUnix, 0).UTC()
	std.ExpiresAt = time.Unix(std.ExpiresAtUnix, 0).UTC()

	if std.Issuer == "" || std.Issuer != issuer.IssuerURL {
		return idTokenResult{}, fmt.Errorf("%w: token names %q, flow began with %q", ErrWrongIssuer, std.Issuer, issuer.IssuerURL)
	}
	if strings.TrimSpace(std.Subject) == "" {
		return idTokenResult{}, fmt.Errorf("%w: sub", ErrIDTokenMalformed)
	}
	if expectedClientID == "" || expectedClientID != issuer.Audience || len(std.Audience) == 0 || !slices.Contains(std.Audience, expectedClientID) {
		return idTokenResult{}, fmt.Errorf("%w: %v does not contain configured client id %q", ErrWrongAudience, []string(std.Audience), expectedClientID)
	}
	if std.AuthorizedParty != "" && std.AuthorizedParty != expectedClientID {
		return idTokenResult{}, fmt.Errorf("%w: azp names %q, configured client id is %q", ErrWrongAudience, std.AuthorizedParty, expectedClientID)
	}
	if len(std.Audience) > 1 && std.AuthorizedParty != expectedClientID {
		return idTokenResult{}, fmt.Errorf("%w: multiple audiences require azp == configured client id %q, got %q", ErrWrongAudience, expectedClientID, std.AuthorizedParty)
	}
	if std.IssuedAtUnix == 0 || std.ExpiresAtUnix == 0 {
		return idTokenResult{}, fmt.Errorf("%w: validity window is not stated", ErrIDTokenExpired)
	}
	if !std.ExpiresAt.After(std.IssuedAt) {
		return idTokenResult{}, fmt.Errorf("%w: exp does not follow iat", ErrIDTokenExpired)
	}
	leeway := issuer.ClockSkew
	if !now.Before(std.ExpiresAt.Add(leeway)) {
		return idTokenResult{}, fmt.Errorf("%w: expired at %s", ErrIDTokenExpired, std.ExpiresAt.Format(time.RFC3339))
	}
	notBefore := std.IssuedAt
	if std.NotBeforeUnix != 0 {
		notBefore = time.Unix(std.NotBeforeUnix, 0).UTC()
	}
	if now.Before(notBefore.Add(-leeway)) {
		return idTokenResult{}, fmt.Errorf("%w: not valid until %s", ErrIDTokenExpired, notBefore.Format(time.RFC3339))
	}

	if expectedNonce == "" || !constantTimeStringsEqual(std.Nonce, expectedNonce) {
		return idTokenResult{}, ErrNonceMismatch
	}

	if std.AtHash != "" {
		if accessToken == "" {
			return idTokenResult{}, fmt.Errorf("%w: id token declares at_hash but no access token was returned", ErrAccessTokenMismatch)
		}
		if !constantTimeStringsEqual(std.AtHash, computeAtHash(accessToken)) {
			return idTokenResult{}, ErrAccessTokenMismatch
		}
	}

	return idTokenResult{raw: raw, standard: std}, nil
}
