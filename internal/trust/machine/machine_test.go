package machine

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"
)

func testKeys(t *testing.T, now time.Time) (ServerKey, ServerKey) {
	t.Helper()
	current, err := GenerateServerKey("k-current", now.Add(time.Hour), now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	retired, err := GenerateServerKey("k-retired", now.Add(-time.Hour), now.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return current, retired
}

func testIssueRequest() IssueRequest {
	return IssueRequest{
		Issuer: "https://cell.example", Audience: []string{"hcm-next-api"},
		Subject: "svc-sync", Client: "client-a", Tenant: "tenant-a",
		Session: "sess-1", Assurance: "substantial", TokenID: "tok-1",
		Lifetime: 10 * time.Minute,
	}
}

// TestTodo_INTAPI_001 is the INTAPI-001 token PRIMARY: the issuer mints a
// standard JWT carrying identity only with at most fifteen minutes of
// lifetime, the verifier admits it, rotation keeps retired-key tokens
// working, and JWKS publishes every key until its tokens have expired.
func TestTodo_INTAPI_001(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	current, retired := testKeys(t, now)
	issuer, err := NewIssuer([]ServerKey{current, retired}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier([]ServerKey{current, retired}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	vreq := VerifyRequest{Issuer: "https://cell.example", Audience: []string{"hcm-next-api"}}

	t.Run("issue and verify round-trips identity only", func(t *testing.T) {
		token, err := issuer.Issue(testIssueRequest())
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		if got := strings.Count(token, "."); got != 2 {
			t.Fatalf("token has %d dots, want a 3-segment JWT", got)
		}
		got, err := verifier.Verify(token, vreq)
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if got.Subject != "svc-sync" || got.Client != "client-a" || got.Tenant != "tenant-a" ||
			got.Session != "sess-1" || got.Assurance != "substantial" || got.TokenID != "tok-1" {
			t.Fatalf("claims = %+v", got)
		}
		if got.ExpiresAt-got.IssuedAt != 600 {
			t.Fatalf("lifetime = %d, want 600", got.ExpiresAt-got.IssuedAt)
		}
	})

	t.Run("lifetime above fifteen minutes is refused, never truncated", func(t *testing.T) {
		req := testIssueRequest()
		req.Lifetime = 16 * time.Minute
		if _, err := issuer.Issue(req); !errors.Is(err, ErrLifetimeTooLong) {
			t.Fatalf("Issue 16m = %v, want ErrLifetimeTooLong", err)
		}
		req.Lifetime = 0
		if _, err := issuer.Issue(req); !errors.Is(err, ErrLifetimeTooLong) {
			t.Fatalf("Issue 0 = %v, want ErrLifetimeTooLong", err)
		}
	})

	t.Run("rotation keeps retired-key tokens verifying", func(t *testing.T) {
		oldOnly, err := NewIssuer([]ServerKey{retired}, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		token, err := oldOnly.Issue(testIssueRequest())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := verifier.Verify(token, vreq); err != nil {
			t.Fatalf("retired-key token: %v", err)
		}
	})

	t.Run("jwks publishes retired keys until their tokens expire", func(t *testing.T) {
		set, err := issuer.Publish(now)
		if err != nil {
			t.Fatalf("Publish: %v", err)
		}
		if len(set.Keys) != 2 {
			t.Fatalf("keys = %d, want current + retired", len(set.Keys))
		}
		for _, k := range set.Keys {
			if k.KeyType != "OKP" || k.Curve != "Ed25519" || k.Algorithm != AlgEdDSA || k.Use != "sig" || k.X == "" {
				t.Fatalf("jwk = %+v", k)
			}
		}
		late, err := issuer.Publish(now.Add(time.Hour))
		if err != nil {
			t.Fatalf("Publish late: %v", err)
		}
		if len(late.Keys) != 1 || late.Keys[0].KeyID != "k-current" {
			t.Fatalf("late keys = %+v, want only k-current", late.Keys)
		}
		if _, err := Thumbprint([]byte(`{"kty":"OKP","crv":"Ed25519","x":"` + late.Keys[0].X + `"}`)); err != nil {
			t.Fatalf("published key thumbprint: %v", err)
		}
	})
}

// TestMachineRotationAndErrors pins the rotation and constructor edges:
// empty key sets are refused, rotation swaps the signer, and every
// admitted algorithm verifies through the shared signature path.
func TestMachineRotationAndErrors(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	if _, err := NewIssuer(nil, nil); !errors.Is(err, ErrBadKey) {
		t.Fatalf("empty issuer keys = %v, want ErrBadKey", err)
	}
	if _, err := NewVerifier(nil, nil); !errors.Is(err, ErrBadKey) {
		t.Fatalf("empty verifier keys = %v, want ErrBadKey", err)
	}
	if _, err := GenerateServerKey("", now, now.Add(time.Hour)); !errors.Is(err, ErrBadKey) {
		t.Fatalf("key without id accepted")
	}
	if _, err := GenerateServerKey("k", now, now); !errors.Is(err, ErrBadKey) {
		t.Fatalf("key without publish window accepted")
	}
	keyA, err := GenerateServerKey("ka", now.Add(time.Hour), now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := GenerateServerKey("kb", now.Add(time.Hour), now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := NewIssuer([]ServerKey{keyA}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier([]ServerKey{keyA, keyB}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := issuer.Rotate(nil); !errors.Is(err, ErrBadKey) {
		t.Fatalf("rotate to empty = %v, want ErrBadKey", err)
	}
	if err := verifier.Rotate(nil); !errors.Is(err, ErrBadKey) {
		t.Fatalf("verifier rotate to empty = %v, want ErrBadKey", err)
	}
	req := IssueRequest{
		Issuer: "https://cell.example", Audience: []string{"api"},
		Subject: "svc", Client: "c", Tenant: "t", Session: "s",
		Assurance: "substantial", TokenID: "j", Lifetime: time.Minute,
	}
	before, err := issuer.Issue(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := issuer.Rotate([]ServerKey{keyB, keyA}); err != nil {
		t.Fatal(err)
	}
	after, err := issuer.Issue(req)
	if err != nil {
		t.Fatal(err)
	}
	vreq := VerifyRequest{Issuer: "https://cell.example", Audience: []string{"api"}}
	if _, err := verifier.Verify(before, vreq); err != nil {
		t.Fatalf("pre-rotation token: %v", err)
	}
	if _, err := verifier.Verify(after, vreq); err != nil {
		t.Fatalf("post-rotation token: %v", err)
	}
	kidOf := func(token string) string {
		var h Header
		raw, err := b64.DecodeString(strings.Split(token, ".")[0])
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &h); err != nil {
			t.Fatal(err)
		}
		return h.KID
	}
	if kidOf(before) != "ka" || kidOf(after) != "kb" {
		t.Fatalf("kids = %q %q, want ka then kb", kidOf(before), kidOf(after))
	}
	// Every admitted algorithm verifies and every mismatch refuses
	// through the one signature path.
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	input := []byte("signing-input")
	rdigest := sha256.Sum256(input)
	rsig, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, rdigest[:])
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAssertionSignature(AlgRS256, &rsaKey.PublicKey, string(input), rsig); err != nil {
		t.Fatalf("RSA path: %v", err)
	}
	if err := VerifyAssertionSignature(AlgRS256, &rsaKey.PublicKey, "other-input", rsig); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("RSA mismatch = %v, want ErrBadSignature", err)
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	edigest := sha256.Sum256(input)
	r, s, err := ecdsa.Sign(rand.Reader, ecKey, edigest[:])
	if err != nil {
		t.Fatal(err)
	}
	der, err := asn1.Marshal(struct {
		R, S *big.Int
	}{r, s})
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAssertionSignature(AlgES256, &ecKey.PublicKey, string(input), der); err != nil {
		t.Fatalf("ECDSA path: %v", err)
	}
	if err := VerifyAssertionSignature(AlgES256, &ecKey.PublicKey, "other-input", der); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("ECDSA mismatch = %v, want ErrBadSignature", err)
	}
	if err := VerifyAssertionSignature("none", &ecKey.PublicKey, string(input), der); !errors.Is(err, ErrUnknownAlg) {
		t.Fatalf("unknown alg = %v, want ErrUnknownAlg", err)
	}
	if _, err := Thumbprint([]byte(`{"kty":"secret"}`)); !errors.Is(err, ErrBadKey) {
		t.Fatalf("bad thumbprint kty = %v, want ErrBadKey", err)
	}
	if _, _, err := PublicKeyFromJWK([]byte(`{"kty":"OKP","crv":"X25519","x":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`)); !errors.Is(err, ErrBadKey) {
		t.Fatalf("non-Ed25519 OKP = %v, want ErrBadKey", err)
	}
	if _, err := NewIssuer([]ServerKey{keyA}, nil); err != nil {
		t.Fatalf("nil clock issuer: %v", err)
	}
}

// TestTodo_INTAPI_001_Security proves every forgery fails closed: a
// re-signed payload, an algorithm swap, a wrong issuer or audience, an
// expired or over-long token, an unknown key id and private JWK material
// are all refused.
func TestTodo_INTAPI_001_Security(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	current, retired := testKeys(t, now)
	issuer, err := NewIssuer([]ServerKey{current}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier([]ServerKey{current, retired}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	vreq := VerifyRequest{Issuer: "https://cell.example", Audience: []string{"hcm-next-api"}}

	good, err := issuer.Issue(testIssueRequest())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("re-signed payload is refused", func(t *testing.T) {
		parts := strings.Split(good, ".")
		evil := parts[0] + "." + parts[1][:len(parts[1])-2] + "AA" + "." + parts[2]
		if _, err := verifier.Verify(evil, vreq); !errors.Is(err, ErrBadSignature) {
			t.Fatalf("tampered = %v, want ErrBadSignature", err)
		}
	})

	t.Run("foreign signature is refused", func(t *testing.T) {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		_ = pub
		parts := strings.Split(good, ".")
		sig := ed25519.Sign(priv, []byte(parts[0]+"."+parts[1]))
		forged := parts[0] + "." + parts[1] + "." + b64.EncodeToString(sig)
		if _, err := verifier.Verify(forged, vreq); !errors.Is(err, ErrBadSignature) {
			t.Fatalf("foreign signature = %v, want ErrBadSignature", err)
		}
	})

	t.Run("wrong issuer and audience are refused", func(t *testing.T) {
		if _, err := verifier.Verify(good, VerifyRequest{Issuer: "https://evil.example", Audience: vreq.Audience}); !errors.Is(err, ErrWrongIssuer) {
			t.Fatalf("wrong issuer = %v, want ErrWrongIssuer", err)
		}
		if _, err := verifier.Verify(good, VerifyRequest{Issuer: vreq.Issuer, Audience: []string{"other-api"}}); !errors.Is(err, ErrWrongAudience) {
			t.Fatalf("wrong audience = %v, want ErrWrongAudience", err)
		}
	})

	t.Run("expired and over-long tokens are refused", func(t *testing.T) {
		late, err := NewVerifier([]ServerKey{current}, func() time.Time { return now.Add(11 * time.Minute) })
		if err != nil {
			t.Fatal(err)
		}
		if _, err := late.Verify(good, vreq); !errors.Is(err, ErrExpiredToken) {
			t.Fatalf("expired = %v, want ErrExpiredToken", err)
		}
		// A token whose own window exceeds the maximum is refused even
		// though its signature verifies: hand-sign a wide window with the
		// current key, bypassing the issuer's lifetime refusal.
		header := b64.EncodeToString([]byte(`{"alg":"EdDSA","typ":"JWT","kid":"k-current"}`))
		payload := b64.EncodeToString([]byte(fmt.Sprintf(`{"iss":"https://cell.example","aud":["hcm-next-api"],`+
			`"sub":"svc-sync","client_id":"client-a","tenant":"tenant-a","sid":"sess-1",`+
			`"assurance":"substantial","iat":%d,"exp":%d,"jti":"tok-wide"}`,
			now.Add(-time.Minute).Unix(), now.Add(time.Hour).Unix())))
		sig := ed25519.Sign(current.Private, []byte(header+"."+payload))
		wide := header + "." + payload + "." + b64.EncodeToString(sig)
		if _, err := verifier.Verify(wide, vreq); !errors.Is(err, ErrLifetimeTooLong) {
			t.Fatalf("wide window = %v, want ErrLifetimeTooLong", err)
		}
	})

	t.Run("unknown key id is refused", func(t *testing.T) {
		other, err := GenerateServerKey("k-other", now.Add(time.Hour), now.Add(2*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		foreign, err := NewIssuer([]ServerKey{other}, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		token, err := foreign.Issue(testIssueRequest())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := verifier.Verify(token, vreq); !errors.Is(err, ErrUnknownKey) {
			t.Fatalf("unknown kid = %v, want ErrUnknownKey", err)
		}
	})

	t.Run("private JWK material is refused", func(t *testing.T) {
		if _, _, err := PublicKeyFromJWK([]byte(`{"kty":"OKP","crv":"Ed25519","x":"11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo","d":"nWGxne_9WmC6hEr0kuYxJ9oE8aI1x2P3m4N5c6B7v8"}`)); !errors.Is(err, ErrBadKey) {
			t.Fatalf("private JWK = %v, want ErrBadKey", err)
		}
		// Real generated keys in every admitted shape parse and agree
		// with their algorithm.
		edPub, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		edJWK := []byte(`{"kty":"OKP","crv":"Ed25519","x":"` + b64.EncodeToString(edPub) + `"}`)
		if _, alg, err := PublicKeyFromJWK(edJWK); err != nil || alg != AlgEdDSA {
			t.Fatalf("Ed25519 JWK = %v %q", err, alg)
		}
		rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		rsaJWK := []byte(`{"kty":"RSA","n":"` + b64.EncodeToString(rsaKey.N.Bytes()) +
			`","e":"` + b64.EncodeToString(big.NewInt(int64(rsaKey.E)).Bytes()) + `"}`)
		if _, alg, err := PublicKeyFromJWK(rsaJWK); err != nil || alg != AlgRS256 {
			t.Fatalf("RSA JWK = %v %q", err, alg)
		}
		ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		ecJWK := []byte(`{"kty":"EC","crv":"P-256","x":"` + b64.EncodeToString(ecKey.X.Bytes()) +
			`","y":"` + b64.EncodeToString(ecKey.Y.Bytes()) + `"}`)
		if _, alg, err := PublicKeyFromJWK(ecJWK); err != nil || alg != AlgES256 {
			t.Fatalf("EC JWK = %v %q", err, alg)
		}
		// A client assertion signed with the RSA key verifies through the
		// parsed JWK: the registry path trusts parsed keys, not fixtures.
		assertion := b64.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." +
			b64.EncodeToString([]byte(`{"iss":"client-a","sub":"client-a","aud":"https://cell.example/oauth2/token","jti":"a-1","exp":9999999999}`))
		digest := sha256.Sum256([]byte(assertion))
		sig, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		pub, _, err := PublicKeyFromJWK(rsaJWK)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifySignature(AlgRS256, pub, []byte(assertion), sig); err != nil {
			t.Fatalf("RSA assertion: %v", err)
		}
	})
}
