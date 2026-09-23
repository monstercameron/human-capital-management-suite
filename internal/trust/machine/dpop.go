package machine

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DPoP proof validation at use time (RFC 9449 section 4.3). The token
// endpoint validates the issuance-bound proof itself; this helper validates
// a proof presented alongside an access token on a later call: the proof
// binds this method and URL, is fresh, and its ath binds this access token.
// Replay of the proof identifier is the caller's check: the resource server
// owns the transport it arrived on.

var (
	ErrDPoPMalformed = errors.New("machine: malformed DPoP proof")
	ErrDPoPBinding   = errors.New("machine: DPoP proof does not bind this call")
	ErrDPoPStale     = errors.New("machine: DPoP proof is stale")
	ErrDPoPSignature = errors.New("machine: DPoP proof signature does not verify")
)

// DPoPFreshness bounds proof age at use. Proofs are seconds-old by design.
const DPoPFreshness = time.Minute

// DPoPProof is a validated use-time proof.
type DPoPProof struct {
	KeyThumbprint string
	TokenID       string
	IssuedAt      time.Time
}

// VerifyDPoPProof validates proof for an HTTP method/URL call at now,
// tolerating skew on the proof instant. When accessToken is set, the
// proof's ath must bind it (use time); when empty, the ath check is
// skipped (issuance time, where no token exists yet). Callers enforcing a
// binding must always pass the token.
func VerifyDPoPProof(proof, accessToken, method, url string, now time.Time, skew time.Duration) (DPoPProof, error) {
	var empty DPoPProof
	parts := strings.Split(proof, ".")
	if len(parts) != 3 {
		return empty, fmt.Errorf("%w: three segments are required", ErrDPoPMalformed)
	}
	headerRaw, err := b64.DecodeString(parts[0])
	if err != nil {
		return empty, fmt.Errorf("%w: header", ErrDPoPMalformed)
	}
	var header struct {
		Alg string          `json:"alg"`
		Typ string          `json:"typ"`
		JWK json.RawMessage `json:"jwk"`
	}
	dec := json.NewDecoder(bytes.NewReader(headerRaw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&header); err != nil {
		return empty, fmt.Errorf("%w: header", ErrDPoPMalformed)
	}
	if header.Typ != "dpop+jwt" {
		return empty, fmt.Errorf("%w: not a DPoP proof", ErrDPoPMalformed)
	}
	pub, alg, err := PublicKeyFromJWK(header.JWK)
	if err != nil {
		return empty, err
	}
	if alg != header.Alg {
		return empty, fmt.Errorf("%w: algorithm does not match the key", ErrDPoPBinding)
	}
	payloadRaw, err := b64.DecodeString(parts[1])
	if err != nil {
		return empty, fmt.Errorf("%w: payload", ErrDPoPMalformed)
	}
	var claims struct {
		Method     string `json:"htm"`
		URL        string `json:"htu"`
		IssuedAt   int64  `json:"iat"`
		TokenID    string `json:"jti"`
		AccessHash string `json:"ath"`
	}
	pdec := json.NewDecoder(bytes.NewReader(payloadRaw))
	pdec.DisallowUnknownFields()
	if err := pdec.Decode(&claims); err != nil {
		return empty, fmt.Errorf("%w: payload", ErrDPoPMalformed)
	}
	if claims.Method != method || claims.URL != url {
		return empty, fmt.Errorf("%w: another call", ErrDPoPBinding)
	}
	iat := time.Unix(claims.IssuedAt, 0).UTC()
	if now.Before(iat.Add(-skew)) || iat.Add(DPoPFreshness+skew).Before(now) {
		return empty, fmt.Errorf("%w", ErrDPoPStale)
	}
	if claims.TokenID == "" {
		return empty, fmt.Errorf("%w: no identifier", ErrDPoPMalformed)
	}
	if accessToken != "" {
		sum := sha256.Sum256([]byte(accessToken))
		if claims.AccessHash != b64.EncodeToString(sum[:]) {
			return empty, fmt.Errorf("%w: another token", ErrDPoPBinding)
		}
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil {
		return empty, fmt.Errorf("%w: signature", ErrDPoPMalformed)
	}
	if err := verifySignature(alg, pub, []byte(parts[0]+"."+parts[1]), sig); err != nil {
		return empty, fmt.Errorf("%w", ErrDPoPSignature)
	}
	jkt, err := Thumbprint(header.JWK)
	if err != nil {
		return empty, err
	}
	return DPoPProof{KeyThumbprint: jkt, TokenID: claims.TokenID, IssuedAt: iat}, nil
}
