package oidc_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/authn/oidc"
	trustfederation "github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

// idTokenScenario drives one full BeginAuthorization/HandleCallback round
// trip, letting the caller mutate the ID token's claims (and optionally the
// signing algorithm/key) before it is signed, so idtoken.go's validation
// rules (unexported) are each exercised through the one public entry point
// that calls them: [oidc.Flow.HandleCallback].
func idTokenScenario(t *testing.T, fixture issuerFixture, keys *testKeys, alg trustfederation.Algorithm, kid string, mutate func(claims map[string]any)) error {
	t.Helper()
	idp := newFakeIdP()
	flow := newFlow(t, fixture, newClientSource(), idp, nil)

	req, err := flow.BeginAuthorization(context.Background(), tenantAcme, issuerAcme, baseTime)
	if err != nil {
		t.Fatalf("BeginAuthorization: %v", err)
	}
	challenge := queryValue(t, req.URL, "code_challenge")
	nonce := queryValue(t, req.URL, "nonce")
	claims := idTokenClaims(issuerAcme, audienceAcme, "user-1", nonce, baseTime)
	if mutate != nil {
		mutate(claims)
	}
	idToken := keys.sign(t, alg, kid, claims)
	idp.issueCode("code-1", fakeCode{codeChallenge: challenge, redirectURI: redirectURI, clientID: clientIDAcme, idToken: idToken, accessToken: "at-1"})

	_, _, err = flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: "code-1", Now: baseTime.Add(time.Second)})
	return err
}

func TestFlow_HandleCallback_IDTokenValidation(t *testing.T) {
	t.Parallel()
	keys := newTestKeys(t)
	kid := "rsa-1"
	fixture := newIssuerFixture(t,
		[]trustfederation.Algorithm{trustfederation.AlgRS256},
		[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, kid)},
		nil, time.Minute,
	)

	cases := []struct {
		name    string
		mutate  func(map[string]any)
		wantErr error
	}{
		{"wrong issuer", func(c map[string]any) { c["iss"] = "https://attacker.invalid/" }, oidc.ErrWrongIssuer},
		{"wrong OAuth client id", func(c map[string]any) { c["aud"] = "some-other-client" }, oidc.ErrWrongAudience},
		{"single audience with wrong azp", func(c map[string]any) { c["azp"] = "some-other-client" }, oidc.ErrWrongAudience},
		{"expired", func(c map[string]any) {
			c["iat"] = baseTime.Add(-2 * time.Hour).Unix()
			c["exp"] = baseTime.Add(-time.Hour).Unix()
		}, oidc.ErrIDTokenExpired},
		{"exp before iat", func(c map[string]any) { c["exp"] = baseTime.Add(-time.Hour).Unix() }, oidc.ErrIDTokenExpired},
		{"not yet valid (nbf)", func(c map[string]any) { c["nbf"] = baseTime.Add(time.Hour).Unix() }, oidc.ErrIDTokenExpired},
		{"missing sub", func(c map[string]any) { delete(c, "sub") }, oidc.ErrIDTokenMalformed},
		{"wrong nonce", func(c map[string]any) { c["nonce"] = "not-the-issued-nonce" }, oidc.ErrNonceMismatch},
		{"missing nonce", func(c map[string]any) { delete(c, "nonce") }, oidc.ErrNonceMismatch},
		{"multi-audience without azp", func(c map[string]any) { c["aud"] = []string{audienceAcme, "another-client"} }, oidc.ErrWrongAudience},
		{"multi-audience with correct azp", func(c map[string]any) {
			c["aud"] = []string{audienceAcme, "another-client"}
			c["azp"] = audienceAcme
		}, nil},
		{"multi-audience with wrong client azp", func(c map[string]any) {
			c["aud"] = []string{audienceAcme, "another-client"}
			c["azp"] = "some-other-client"
		}, oidc.ErrWrongAudience},
		{"bad at_hash", func(c map[string]any) { c["at_hash"] = "not-the-right-hash" }, oidc.ErrAccessTokenMismatch},
		{"correct at_hash", func(c map[string]any) { c["at_hash"] = computeAtHashForTest("at-1") }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := idTokenScenario(t, fixture, keys, trustfederation.AlgRS256, kid, tc.mutate)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("HandleCallback error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("HandleCallback error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestFlow_HandleCallback_ClockSkewBoundary(t *testing.T) {
	t.Parallel()
	keys := newTestKeys(t)
	kid := "rsa-1"
	skew := 30 * time.Second
	fixture := newIssuerFixture(t,
		[]trustfederation.Algorithm{trustfederation.AlgRS256},
		[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, kid)},
		nil, skew,
	)

	// exp is exactly baseTime; the callback lands 20s later, within the
	// 30s skew bound, and must succeed.
	err := idTokenScenario(t, fixture, keys, trustfederation.AlgRS256, kid, func(c map[string]any) {
		c["iat"] = baseTime.Add(-time.Hour).Unix()
		c["exp"] = baseTime.Add(-20 * time.Second).Unix()
	})
	if err != nil {
		t.Fatalf("HandleCallback within clock skew: %v, want nil", err)
	}
}

func TestFlow_HandleCallback_UnsupportedAlgorithmHeader(t *testing.T) {
	t.Parallel()
	keys := newTestKeys(t)
	kid := "rsa-1"
	// Issuer declares only RS256; the ID token's header claims HS256 (not
	// even in the closed RS256/ES256/EdDSA vocabulary at all).
	fixture := newIssuerFixture(t,
		[]trustfederation.Algorithm{trustfederation.AlgRS256},
		[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, kid)},
		nil, 0,
	)
	idp := newFakeIdP()
	flow := newFlow(t, fixture, newClientSource(), idp, nil)
	req, err := flow.BeginAuthorization(context.Background(), tenantAcme, issuerAcme, baseTime)
	if err != nil {
		t.Fatalf("BeginAuthorization: %v", err)
	}
	challenge := queryValue(t, req.URL, "code_challenge")
	nonce := queryValue(t, req.URL, "nonce")

	// Hand-build a token with alg=HS256 and a garbage signature -- this
	// package must reject it before ever looking at the "signature".
	claims := idTokenClaims(issuerAcme, audienceAcme, "user-1", nonce, baseTime)
	header := `{"alg":"HS256","kid":"` + kid + `","typ":"JWT"}`
	payload, err2 := json.Marshal(claims)
	if err2 != nil {
		t.Fatalf("marshal claims: %v", err2)
	}
	idToken := b64([]byte(header)) + "." + b64(payload) + "." + b64([]byte("not-a-real-signature"))

	idp.issueCode("code-1", fakeCode{codeChallenge: challenge, redirectURI: redirectURI, clientID: clientIDAcme, idToken: idToken, accessToken: "at-1"})
	_, _, err = flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: "code-1", Now: baseTime.Add(time.Second)})
	if !errors.Is(err, oidc.ErrUnsupportedAlgorithm) {
		t.Fatalf("HandleCallback error = %v, want ErrUnsupportedAlgorithm", err)
	}
}

func TestFlow_HandleCallback_UnknownKeyID(t *testing.T) {
	t.Parallel()
	keys := newTestKeys(t)
	fixture := newIssuerFixture(t,
		[]trustfederation.Algorithm{trustfederation.AlgRS256},
		[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, "rsa-1")},
		nil, 0,
	)
	err := idTokenScenario(t, fixture, keys, trustfederation.AlgRS256, "rsa-does-not-exist", nil)
	if !errors.Is(err, oidc.ErrKeyNotFound) {
		t.Fatalf("HandleCallback error = %v, want ErrKeyNotFound", err)
	}
}

func TestFlow_HandleCallback_KeyOutsideValidityWindow(t *testing.T) {
	t.Parallel()
	keys := newTestKeys(t)
	kid := "rsa-1"
	pk := keys.pinnedKey(t, trustfederation.AlgRS256, kid)
	pk.NotBefore = baseTime.Add(-2 * time.Hour)
	pk.NotAfter = baseTime.Add(-time.Hour) // already expired by baseTime
	fixture := newIssuerFixture(t,
		[]trustfederation.Algorithm{trustfederation.AlgRS256},
		[]issuerregistry.PinnedKey{pk},
		nil, 0,
	)
	err := idTokenScenario(t, fixture, keys, trustfederation.AlgRS256, kid, nil)
	if !errors.Is(err, oidc.ErrKeyExpired) {
		t.Fatalf("HandleCallback error = %v, want ErrKeyExpired", err)
	}
}
