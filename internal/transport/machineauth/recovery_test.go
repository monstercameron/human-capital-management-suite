package machineauth

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/partnerapp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/machine"
)

// TestTodo_INTAPI_001_Recovery proves signing-key rotation never breaks
// issued tokens: a token minted under the retiring key still verifies
// after rotation, JWKS keeps publishing the retired key until its tokens
// have expired, and dropping the retired key from the verifier ends its
// tokens without touching the new key's.
func TestTodo_INTAPI_001_Recovery(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	keyA, err := machine.GenerateServerKey("srv-a", now.Add(-time.Minute), now.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := machine.GenerateServerKey("srv-b", now.Add(time.Hour), now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := machine.NewIssuer([]machine.ServerKey{keyA}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := machine.NewVerifier([]machine.ServerKey{keyA}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	reg := &fakeRegistry{clients: make(map[string]partnerapp.MachineClient), keys: make(map[string][]partnerapp.MachineClientKey)}
	h, err := NewHandler(Dependencies{
		Registry: reg, Issuer: issuer, Verifier: verifier,
		Audiences: []string{"hcm-next-api"}, TokenIssuer: intTestIssuer, TokenURL: intTestTokenURL,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, priv, jwk := edKeyForTest(t)
	reg.clients[intTestTenant+"\x00client-r"] = partnerapp.MachineClient{
		Tenant: intTestTenant, ClientID: "client-r", Owner: "owner-r",
		Status: partnerapp.MachineClientActive, Scopes: []string{"intents.read"},
		Purpose: "sync", IPAllowlist: []string{"127.0.0.1/32"},
	}
	reg.keys[intTestTenant+"\x00client-r"] = []partnerapp.MachineClientKey{{
		ClientID: "client-r", KID: "ck-r", Alg: machine.AlgEdDSA, JWK: jwk,
		NotBefore: now.Add(-time.Hour),
	}}
	mux := h.Routes()
	issue := func() string {
		t.Helper()
		assertion := mintAssertion(t, machine.AlgEdDSA, "ck-r", "client-r",
			now.Add(-time.Minute), now.Add(4*time.Minute),
			func(input string) []byte { return ed25519.Sign(priv, []byte(input)) })
		code, body := postToken(t, mux, url.Values{
			"grant_type":            {"client_credentials"},
			"tenant":                {intTestTenant},
			"client_assertion_type": {clientAssertionType},
			"client_assertion":      {assertion},
		}, nil)
		if code != http.StatusOK {
			t.Fatalf("issue status = %d, body = %s", code, body)
		}
		return decodeTokenResponse(t, body).AccessToken
	}
	verify := func(token string) error {
		_, err := verifier.Verify(token,
			machine.VerifyRequest{Issuer: intTestIssuer, Audience: []string{"hcm-next-api"}})
		return err
	}
	jwksKids := func() []string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("jwks status = %d", rec.Code)
		}
		var set machine.KeySet
		if err := json.Unmarshal(rec.Body.Bytes(), &set); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, k := range set.Keys {
			out = append(out, k.KeyID)
		}
		return out
	}

	before := issue()
	if err := verify(before); err != nil {
		t.Fatalf("pre-rotation token: %v", err)
	}

	// Rotate: B signs, A verifies only. The old token keeps working and
	// JWKS still publishes A until its tokens expire.
	if err := issuer.Rotate([]machine.ServerKey{keyB, keyA}); err != nil {
		t.Fatal(err)
	}
	if err := verifier.Rotate([]machine.ServerKey{keyB, keyA}); err != nil {
		t.Fatal(err)
	}
	if err := verify(before); err != nil {
		t.Fatalf("retired-key token after rotation: %v", err)
	}
	after := issue()
	if err := verify(after); err != nil {
		t.Fatalf("new-key token: %v", err)
	}
	kids := jwksKids()
	if len(kids) != 2 {
		t.Fatalf("jwks kids = %v, want both keys while A honors outstanding tokens", kids)
	}

	// Completing the rotation drops A from verification: its tokens end,
	// B's are untouched.
	if err := verifier.Rotate([]machine.ServerKey{keyB}); err != nil {
		t.Fatal(err)
	}
	if err := verify(before); err == nil {
		t.Fatal("retired-key token verifies after the rotation completed")
	}
	if err := verify(after); err != nil {
		t.Fatalf("new-key token after completion: %v", err)
	}
}
