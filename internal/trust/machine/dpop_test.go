package machine

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"
)

func dpopKeyForTest(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	jwk := `{"kty":"EC","crv":"P-256","x":"` + b64.EncodeToString(key.X.Bytes()) +
		`","y":"` + b64.EncodeToString(key.Y.Bytes()) + `"}`
	return key, jwk
}

func mintDPoPForTest(t *testing.T, key *ecdsa.PrivateKey, jwk, method, url, ath string, iat time.Time, jti string) string {
	t.Helper()
	header := b64.EncodeToString([]byte(`{"alg":"ES256","typ":"dpop+jwt","jwk":` + jwk + `}`))
	payload := b64.EncodeToString([]byte(`{"htm":"` + method + `","htu":"` + url + `","iat":` +
		itoa64(iat.Unix()) + `,"jti":"` + jti + `","ath":"` + ath + `"}`))
	input := header + "." + payload
	digest := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	der, err := asn1.Marshal(struct {
		R, S *big.Int
	}{r, s})
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + b64.EncodeToString(der)
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// TestDPoPAtUse proves the use-time DPoP helper binds method, URL and
// access token: a fresh proof for this call admits, while a proof for
// another call, another token, or a stale instant is refused.
func TestDPoPAtUse(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	key, jwk := dpopKeyForTest(t)
	const (
		method = "POST"
		url    = "https://cell.example/api/v1/intents"
		token  = "access-token-value"
	)
	sum := sha256.Sum256([]byte(token))
	ath := b64.EncodeToString(sum[:])

	t.Run("fresh proof for this call admits", func(t *testing.T) {
		proof := mintDPoPForTest(t, key, jwk, method, url, ath, now, "dp-use-1")
		got, err := VerifyDPoPProof(proof, token, method, url, now, 0)
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		want, err := Thumbprint([]byte(jwk))
		if err != nil {
			t.Fatal(err)
		}
		if got.KeyThumbprint != want || got.TokenID != "dp-use-1" {
			t.Fatalf("proof = %+v", got)
		}
	})

	t.Run("proof for another call or token is refused", func(t *testing.T) {
		otherMethod := mintDPoPForTest(t, key, jwk, "GET", url, ath, now, "dp-use-2")
		if _, err := VerifyDPoPProof(otherMethod, token, method, url, now, 0); !errors.Is(err, ErrDPoPBinding) {
			t.Fatalf("other method = %v, want ErrDPoPBinding", err)
		}
		otherURL := mintDPoPForTest(t, key, jwk, method, "https://cell.example/other", ath, now, "dp-use-3")
		if _, err := VerifyDPoPProof(otherURL, token, method, url, now, 0); !errors.Is(err, ErrDPoPBinding) {
			t.Fatalf("other url = %v, want ErrDPoPBinding", err)
		}
		otherToken := mintDPoPForTest(t, key, jwk, method, url, b64.EncodeToString([]byte("nope")), now, "dp-use-4")
		if _, err := VerifyDPoPProof(otherToken, token, method, url, now, 0); !errors.Is(err, ErrDPoPBinding) {
			t.Fatalf("other token = %v, want ErrDPoPBinding", err)
		}
	})

	t.Run("stale and forged proofs are refused", func(t *testing.T) {
		stale := mintDPoPForTest(t, key, jwk, method, url, ath, now.Add(-time.Hour), "dp-use-5")
		if _, err := VerifyDPoPProof(stale, token, method, url, now, 0); !errors.Is(err, ErrDPoPStale) {
			t.Fatalf("stale = %v, want ErrDPoPStale", err)
		}
		good := mintDPoPForTest(t, key, jwk, method, url, ath, now, "dp-use-6")
		parts := strings.Split(good, ".")
		// Flip one signature character to another valid one: the payload
		// still parses, so only the signature check can fail.
		flipped := parts[2]
		if flipped[0] == 'A' {
			flipped = "B" + flipped[1:]
		} else {
			flipped = "A" + flipped[1:]
		}
		if _, err := VerifyDPoPProof(parts[0]+"."+parts[1]+"."+flipped, token, method, url, now, 0); !errors.Is(err, ErrDPoPSignature) {
			t.Fatalf("forged = %v, want ErrDPoPSignature", err)
		}
		if _, err := VerifyDPoPProof("garbage", token, method, url, now, 0); !errors.Is(err, ErrDPoPMalformed) {
			t.Fatalf("malformed = %v, want ErrDPoPMalformed", err)
		}
	})
}
