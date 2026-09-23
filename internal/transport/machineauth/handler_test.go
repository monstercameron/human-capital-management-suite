package machineauth

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/partnerapp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/machine"
)

const (
	intTestIssuer   = "https://cell.example"
	intTestTokenURL = "https://cell.example/oauth2/token"
	intTestTenant   = "tenant-a"
)

var errFakeUnknownClient = errors.New("fake: unknown client")

// fakeRegistry is an in-memory partnerapp.ClientRegistry. The durable
// proof lives in truststore (TestTodo_INTAPI_001, TestMachineRegistryPort);
// here the registry is a collaborator so the served loop stays hermetic.
type fakeRegistry struct {
	clients map[string]partnerapp.MachineClient
	keys    map[string][]partnerapp.MachineClientKey
	uses    map[string]time.Time
}

func (f *fakeRegistry) LoadClient(_ context.Context, tenant, clientID string) (partnerapp.MachineClient, error) {
	c, ok := f.clients[tenant+"\x00"+clientID]
	if !ok {
		return partnerapp.MachineClient{}, errFakeUnknownClient
	}
	return c, nil
}

func (f *fakeRegistry) LoadClientKeys(_ context.Context, tenant, clientID string) ([]partnerapp.MachineClientKey, error) {
	return append([]partnerapp.MachineClientKey(nil), f.keys[tenant+"\x00"+clientID]...), nil
}

func (f *fakeRegistry) RecordClientUse(_ context.Context, tenant, clientID string, now time.Time) error {
	if f.uses == nil {
		f.uses = make(map[string]time.Time)
	}
	f.uses[tenant+"\x00"+clientID] = now
	return nil
}

type intFixture struct {
	handler  *Handler
	issuer   *machine.Issuer
	verifier *machine.Verifier
	registry *fakeRegistry
	now      time.Time
}

func newIntFixture(t *testing.T) *intFixture {
	t.Helper()
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	current, err := machine.GenerateServerKey("srv-current", now.Add(time.Hour), now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := machine.NewIssuer([]machine.ServerKey{current}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := machine.NewVerifier([]machine.ServerKey{current}, func() time.Time { return now })
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
	return &intFixture{handler: h, issuer: issuer, verifier: verifier, registry: reg, now: now}
}

func (f *intFixture) addClient(t *testing.T, client partnerapp.MachineClient, keys []partnerapp.MachineClientKey) {
	t.Helper()
	if client.Tenant == "" {
		client.Tenant = intTestTenant
	}
	if client.Status == "" {
		client.Status = partnerapp.MachineClientActive
	}
	f.registry.clients[client.Tenant+"\x00"+client.ClientID] = client
	f.registry.keys[client.Tenant+"\x00"+client.ClientID] = keys
}

func edKeyForTest(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey, []byte) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	jwk := []byte(`{"kty":"OKP","crv":"Ed25519","x":"` + b64url(pub) + `"}`)
	return pub, priv, jwk
}

type ecdsaSignature struct {
	R, S *big.Int
}

// mintAssertion builds a private_key_jwt for clientID signed by priv. Every
// call mints a fresh identifier so replay protection never trips across
// subtests sharing one handler.
var assertionSeq atomic.Int64

func mintAssertion(t *testing.T, alg, kid, clientID string, iat, exp time.Time, sign func(input string) []byte) string {
	t.Helper()
	seq := assertionSeq.Add(1)
	header := b64url([]byte(`{"alg":"` + alg + `","typ":"JWT","kid":"` + kid + `"}`))
	payload := b64url([]byte(`{"iss":"` + clientID + `","sub":"` + clientID + `","aud":"` + intTestTokenURL + `","jti":"assert-` + itoa(seq) + `","iat":` + itoa(iat.Unix()) + `,"exp":` + itoa(exp.Unix()) + `}`))
	input := header + "." + payload
	return input + "." + b64url(sign(input))
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func postToken(t *testing.T, mux http.Handler, form url.Values, headers map[string]string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:9999"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	return rec.Code, body
}

func decodeTokenResponse(t *testing.T, body []byte) TokenResponse {
	t.Helper()
	var out TokenResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode token response %q: %v", body, err)
	}
	return out
}

func decodeOAuthError(t *testing.T, body []byte) OAuthError {
	t.Helper()
	var out OAuthError
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode oauth error %q: %v", body, err)
	}
	return out
}

// TestTodo_INTAPI_001_Integration is the served loop: a registered client
// authenticates with a private_key_jwt, the endpoint issues a token the
// served verifier admits as identity, JWKS publishes the signing key, and
// DPoP and mutual-TLS bindings constrain their tokens.
func TestTodo_INTAPI_001_Integration(t *testing.T) {
	f := newIntFixture(t)
	_, priv, jwk := edKeyForTest(t)
	f.addClient(t, partnerapp.MachineClient{
		ClientID: "client-a", Owner: "owner-a", Scopes: []string{"intents.read", "workers.read"},
		Purpose: "sync", IPAllowlist: []string{"127.0.0.1/32"},
	}, []partnerapp.MachineClientKey{{
		ClientID: "client-a", KID: "ck-1", Alg: machine.AlgEdDSA, JWK: jwk,
		NotBefore: f.now.Add(-time.Hour),
	}})
	mux := f.handler.Routes()

	t.Run("assertion authenticates and the served verifier admits the token", func(t *testing.T) {
		assertion := mintAssertion(t, machine.AlgEdDSA, "ck-1", "client-a",
			f.now.Add(-time.Minute), f.now.Add(4*time.Minute),
			func(input string) []byte { return ed25519.Sign(priv, []byte(input)) })
		code, body := postToken(t, mux, url.Values{
			"grant_type":            {"client_credentials"},
			"tenant":                {intTestTenant},
			"client_assertion_type": {clientAssertionType},
			"client_assertion":      {assertion},
		}, nil)
		if code != http.StatusOK {
			t.Fatalf("token status = %d, body = %s", code, body)
		}
		resp := decodeTokenResponse(t, body)
		if resp.TokenType != "Bearer" || resp.AccessToken == "" || resp.ExpiresIn != 900 {
			t.Fatalf("response = %+v", resp)
		}
		if resp.Scope != "intents.read workers.read" {
			t.Fatalf("scope = %q", resp.Scope)
		}
		served := &trust.ServedVerifier{Machine: f.verifier,
			Request: machine.VerifyRequest{Issuer: intTestIssuer, Audience: []string{"hcm-next-api"}}}
		principal, err := served.Verify(t.Context(), trust.Credential{Token: resp.AccessToken})
		if err != nil {
			t.Fatalf("served verification: %v", err)
		}
		if principal.Subject() != "client-a" || string(principal.Tenant()) != intTestTenant ||
			principal.SubjectKind() != trust.SubjectKindIntegration {
			t.Fatalf("principal = %s", principal)
		}
		if len(principal.Roles()) != 0 || len(principal.Purposes()) != 0 {
			t.Fatalf("principal carries authority: %s", principal)
		}
		if use, ok := f.registry.uses[intTestTenant+"\x00client-a"]; !ok || !use.Equal(f.now) {
			t.Fatalf("client use not recorded: %+v", f.registry.uses)
		}
	})

	t.Run("mutual TLS binds its token to the certificate", func(t *testing.T) {
		f.addClient(t, partnerapp.MachineClient{
			ClientID: "client-m", Owner: "owner-m", Scopes: []string{"intents.read"},
			Purpose: "sync", IPAllowlist: []string{"127.0.0.1/32"},
			CertFingerprint: strings.Repeat("ab", 32),
		}, nil)
		code, body := postToken(t, mux, url.Values{
			"grant_type": {"client_credentials"},
			"tenant":     {intTestTenant},
			"client_id":  {"client-m"},
		}, map[string]string{HeaderTLSCertSHA256: strings.Repeat("ab", 32)})
		if code != http.StatusOK {
			t.Fatalf("mtls token status = %d, body = %s", code, body)
		}
		resp := decodeTokenResponse(t, body)
		if resp.TokenType != "Bearer" {
			t.Fatalf("token type = %q, want Bearer", resp.TokenType)
		}
		claims, err := f.verifier.Verify(resp.AccessToken,
			machine.VerifyRequest{Issuer: intTestIssuer, Audience: []string{"hcm-next-api"}})
		if err != nil {
			t.Fatalf("mtls token: %v", err)
		}
		der, err := fingerprintBytes(strings.Repeat("ab", 32))
		if err != nil {
			t.Fatal(err)
		}
		if claims.Confirmation["x5t#S256"] != b64url(der) {
			t.Fatalf("cnf = %v, want the certificate binding", claims.Confirmation)
		}
	})

	t.Run("jwks publishes the signing key the token names", func(t *testing.T) {
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
		if len(set.Keys) != 1 || set.Keys[0].KeyID != "srv-current" {
			t.Fatalf("jwks = %+v", set)
		}
	})

	t.Run("dpop proof receives a sender-constrained token", func(t *testing.T) {
		ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		proofJWK := `{"kty":"EC","crv":"P-256","x":"` + b64url(ecKey.X.Bytes()) + `","y":"` + b64url(ecKey.Y.Bytes()) + `"}`
		proofHeader := b64url([]byte(`{"alg":"ES256","typ":"dpop+jwt","jwk":` + proofJWK + `}`))
		proofPayload := b64url([]byte(`{"htm":"POST","htu":"` + intTestTokenURL + `","iat":` + itoa(f.now.Unix()) + `,"jti":"dpop-1"}`))
		proofInput := proofHeader + "." + proofPayload
		digest := sha256.Sum256([]byte(proofInput))
		r, s, err := ecdsa.Sign(rand.Reader, ecKey, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		der, err := asn1.Marshal(ecdsaSignature{R: r, S: s})
		if err != nil {
			t.Fatal(err)
		}
		proof := proofInput + "." + b64url(der)
		assertion := mintAssertion(t, machine.AlgEdDSA, "ck-1", "client-a",
			f.now.Add(-time.Minute), f.now.Add(4*time.Minute),
			func(input string) []byte { return ed25519.Sign(priv, []byte(input)) })
		code, body := postToken(t, mux, url.Values{
			"grant_type":            {"client_credentials"},
			"tenant":                {intTestTenant},
			"client_assertion_type": {clientAssertionType},
			"client_assertion":      {assertion},
		}, map[string]string{HeaderDPoP: proof})
		if code != http.StatusOK {
			t.Fatalf("dpop token status = %d, body = %s", code, body)
		}
		resp := decodeTokenResponse(t, body)
		if resp.TokenType != "DPoP" {
			t.Fatalf("token type = %q, want DPoP", resp.TokenType)
		}
		claims, err := f.verifier.Verify(resp.AccessToken,
			machine.VerifyRequest{Issuer: intTestIssuer, Audience: []string{"hcm-next-api"}})
		if err != nil {
			t.Fatalf("dpop token: %v", err)
		}
		jkt, _ := machine.Thumbprint([]byte(proofJWK))
		if claims.Confirmation["jkt"] != jkt {
			t.Fatalf("cnf = %v, want jkt %s", claims.Confirmation, jkt)
		}
	})
}
