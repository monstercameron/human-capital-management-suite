package machineauth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/machine"
)

// assertionClaims is the RFC 7523 client-assertion payload this endpoint
// accepts. Unknown fields are refused: an assertion is a security token,
// not an extensible document.
type assertionClaims struct {
	Issuer    string
	Subject   string
	Audience  []string
	TokenID   string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

func parseAudience(raw json.RawMessage) ([]string, error) {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return []string{single}, nil
	}
	var multi []string
	if err := json.Unmarshal(raw, &multi); err != nil {
		return nil, err
	}
	return multi, nil
}

// parseAssertion splits and decodes a client assertion without verifying
// it. Verification happens against the registry key the header names.
func parseAssertion(token string) (machine.Header, assertionClaims, []byte, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return machine.Header{}, assertionClaims{}, nil, errors.New("three segments are required")
	}
	headerRaw, err := b64decode(parts[0])
	if err != nil {
		return machine.Header{}, assertionClaims{}, nil, err
	}
	var header machine.Header
	if err := json.Unmarshal(headerRaw, &header); err != nil {
		return machine.Header{}, assertionClaims{}, nil, err
	}
	switch header.Alg {
	case machine.AlgEdDSA, machine.AlgRS256, machine.AlgES256:
	default:
		return machine.Header{}, assertionClaims{}, nil, fmt.Errorf("algorithm %q is not admitted", header.Alg)
	}
	if header.KID == "" {
		return machine.Header{}, assertionClaims{}, nil, errors.New("a key id is required")
	}
	payloadRaw, err := b64decode(parts[1])
	if err != nil {
		return machine.Header{}, assertionClaims{}, nil, err
	}
	var raw struct {
		Issuer    string          `json:"iss"`
		Subject   string          `json:"sub"`
		Audience  json.RawMessage `json:"aud"`
		TokenID   string          `json:"jti"`
		IssuedAt  int64           `json:"iat"`
		ExpiresAt int64           `json:"exp"`
	}
	dec := json.NewDecoder(bytesReader(payloadRaw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return machine.Header{}, assertionClaims{}, nil, err
	}
	aud, err := parseAudience(raw.Audience)
	if err != nil {
		return machine.Header{}, assertionClaims{}, nil, err
	}
	sig, err := b64decode(parts[2])
	if err != nil {
		return machine.Header{}, assertionClaims{}, nil, err
	}
	return header, assertionClaims{
		Issuer: raw.Issuer, Subject: raw.Subject, Audience: aud,
		TokenID:   raw.TokenID,
		IssuedAt:  time.Unix(raw.IssuedAt, 0).UTC(),
		ExpiresAt: time.Unix(raw.ExpiresAt, 0).UTC(),
	}, sig, nil
}

// bytesReader, b64decode and b64url are the compact-JWT codecs shared by
// the assertion and proof paths.
func b64decode(s string) ([]byte, error) { return b64Codec.DecodeString(s) }

func b64url(raw []byte) string { return b64Codec.EncodeToString(raw) }

// dpopSkew bounds DPoP proof freshness. Proofs are seconds-old by design;
// a minute covers skew without admitting replays worth keeping.
const dpopSkew = time.Minute

// validateDPoP checks an RFC 9449 DPoP proof bound to this token request
// and returns the confirmation thumbprint. The proof carries its own key,
// so the registry is not consulted: possession of the key is the proof.
func (h *Handler) validateDPoP(w http.ResponseWriter, r *http.Request, proof string, now time.Time) (string, bool) {
	parts := strings.Split(proof, ".")
	if len(parts) != 3 {
		writeOAuthError(w, http.StatusBadRequest, "invalid_dpop_proof", "the DPoP proof is malformed")
		return "", false
	}
	headerRaw, err := b64decode(parts[0])
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_dpop_proof", "the DPoP proof is malformed")
		return "", false
	}
	var header struct {
		Alg string          `json:"alg"`
		Typ string          `json:"typ"`
		JWK json.RawMessage `json:"jwk"`
	}
	if err := json.Unmarshal(headerRaw, &header); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_dpop_proof", "the DPoP proof is malformed")
		return "", false
	}
	if header.Typ != "dpop+jwt" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_dpop_proof", "the proof is not a DPoP proof")
		return "", false
	}
	pub, alg, err := machine.PublicKeyFromJWK(header.JWK)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_dpop_proof", "the proof key is unusable")
		return "", false
	}
	if alg != header.Alg {
		writeOAuthError(w, http.StatusBadRequest, "invalid_dpop_proof", "the proof algorithm does not match its key")
		return "", false
	}
	payloadRaw, err := b64decode(parts[1])
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_dpop_proof", "the DPoP proof is malformed")
		return "", false
	}
	var claims struct {
		Method   string `json:"htm"`
		URL      string `json:"htu"`
		IssuedAt int64  `json:"iat"`
		TokenID  string `json:"jti"`
	}
	dec := json.NewDecoder(bytesReader(payloadRaw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&claims); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_dpop_proof", "the DPoP proof is malformed")
		return "", false
	}
	if claims.Method != http.MethodPost || claims.URL != h.deps.TokenURL {
		writeOAuthError(w, http.StatusBadRequest, "invalid_dpop_proof", "the proof binds another request")
		return "", false
	}
	iat := time.Unix(claims.IssuedAt, 0).UTC()
	if now.Before(iat.Add(-h.deps.Skew)) || iat.Add(dpopSkew+h.deps.Skew).Before(now) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_dpop_proof", "the proof is stale")
		return "", false
	}
	if claims.TokenID == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_dpop_proof", "the proof carries no identifier")
		return "", false
	}
	sig, err := b64decode(parts[2])
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_dpop_proof", "the DPoP proof is malformed")
		return "", false
	}
	if err := machine.VerifyAssertionSignature(alg, pub, proof[:len(parts[0])+1+len(parts[1])], sig); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_dpop_proof", "the proof signature does not verify")
		return "", false
	}
	if h.replay.seen("dpop:"+claims.TokenID, iat.Add(dpopSkew+h.deps.Skew), now) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_dpop_proof", "the proof was already used")
		return "", false
	}
	jkt, err := machine.Thumbprint(header.JWK)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_dpop_proof", "the proof key has no thumbprint")
		return "", false
	}
	return jkt, true
}

func fingerprintBytes(hexFP string) ([]byte, error) {
	raw, err := hex.DecodeString(hexFP)
	if err != nil || len(raw) != 32 {
		return nil, errors.New("a 64-character hex SHA-256 is required")
	}
	return raw, nil
}

func newTokenID() string { return "tok-" + randomHex(16) }

func newSessionID() string { return "mcs-" + randomHex(8) }

func randomHex(n int) string {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		panic("machineauth: randomness unavailable")
	}
	return hex.EncodeToString(raw)
}
