package oidc_test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/authn/oidc"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	trustfederation "github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

// Fixture identifiers shared across this package's test files.
const (
	tenantAcme   = values.TenantId("acme-corp")
	issuerAcme   = "https://login.acme.invalid/"
	clientIDAcme = "hcmnext-web"
	// An OIDC ID token's audience is the registered OAuth client ID.
	audienceAcme  = clientIDAcme
	redirectURI   = "https://app.hcm-next.invalid/oidc/callback"
	authEndpoint  = "https://login.acme.invalid/authorize"
	tokenEndpoint = "https://login.acme.invalid/token"
)

// baseTime is the fixed instant every test in this package treats as "now"
// unless a test needs to move the clock deliberately.
var baseTime = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

// testFlowSecret is a fixed, sufficiently long HMAC key so PKCE verifier
// derivation is deterministic across a test's BeginAuthorization and
// HandleCallback calls without depending on crypto/rand.
var testFlowSecret = []byte("01234567890123456789012345678901-test-secret")

// b64 is unpadded base64url, matching this package's own wire encoding.
func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// fixedWidth concatenates two big.Int values as fixed-width big-endian byte
// strings -- the JWS ES256 signature encoding.
func fixedWidth(r *big.Int, rWidth int, s *big.Int, sWidth int) []byte {
	out := make([]byte, rWidth+sWidth)
	r.FillBytes(out[:rWidth])
	s.FillBytes(out[rWidth:])
	return out
}

// testKeys bundles one signing keypair per algorithm this package verifies,
// generated once per test binary run (key generation, especially RSA, is
// slow enough that regenerating per test would noticeably slow the suite).
type testKeys struct {
	rsaKey *rsa.PrivateKey
	ecKey  *ecdsa.PrivateKey
	edPub  ed25519.PublicKey
	edPriv ed25519.PrivateKey
}

var (
	sharedKeysOnce sync.Once
	sharedKeys     *testKeys
)

func newTestKeys(t testing.TB) *testKeys {
	t.Helper()
	sharedKeysOnce.Do(func() {
		rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("rsa.GenerateKey: %v", err)
		}
		ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("ecdsa.GenerateKey: %v", err)
		}
		edPub, edPriv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("ed25519.GenerateKey: %v", err)
		}
		sharedKeys = &testKeys{rsaKey: rsaKey, ecKey: ecKey, edPub: edPub, edPriv: edPriv}
	})
	return sharedKeys
}

// pinnedKey builds the [issuerregistry.PinnedKey] for alg/kid from k's
// fixture material.
func (k *testKeys) pinnedKey(t testing.TB, alg trustfederation.Algorithm, kid string) issuerregistry.PinnedKey {
	t.Helper()
	var pub any
	switch alg {
	case trustfederation.AlgRS256:
		pub = &k.rsaKey.PublicKey
	case trustfederation.AlgES256:
		pub = &k.ecKey.PublicKey
	case trustfederation.AlgEdDSA:
		pub = k.edPub
	default:
		t.Fatalf("pinnedKey: unsupported algorithm %q", alg)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	return issuerregistry.PinnedKey{KeyID: kid, Algorithm: alg, PublicKeyDER: der}
}

// sign produces a compact JWS ID token over claims under alg/kid using k's
// fixture private key material.
func (k *testKeys) sign(t testing.TB, alg trustfederation.Algorithm, kid string, claims map[string]any) string {
	t.Helper()
	header := map[string]string{"alg": string(alg), "kid": kid, "typ": "JWT"}
	hb, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	pb, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	signingInput := b64(hb) + "." + b64(pb)

	var sig []byte
	switch alg {
	case trustfederation.AlgRS256:
		sum := sha256.Sum256([]byte(signingInput))
		sig, err = rsa.SignPKCS1v15(rand.Reader, k.rsaKey, crypto.SHA256, sum[:])
		if err != nil {
			t.Fatalf("rsa.SignPKCS1v15: %v", err)
		}
	case trustfederation.AlgES256:
		sum := sha256.Sum256([]byte(signingInput))
		r, s, err2 := ecdsa.Sign(rand.Reader, k.ecKey, sum[:])
		if err2 != nil {
			t.Fatalf("ecdsa.Sign: %v", err2)
		}
		sig = fixedWidth(r, 32, s, 32)
	case trustfederation.AlgEdDSA:
		sig = ed25519.Sign(k.edPriv, []byte(signingInput))
	default:
		t.Fatalf("sign: unsupported algorithm %q", alg)
	}
	return signingInput + "." + b64(sig)
}

// idTokenClaims returns a standards-shaped, otherwise-valid ID token claim
// set for issuerURL/audience/subject/nonce with a one-hour validity window
// starting at iat, ready for a test to mutate before signing.
func idTokenClaims(issuerURL, audience, subject, nonce string, iat time.Time) map[string]any {
	return map[string]any{
		"iss":   issuerURL,
		"sub":   subject,
		"aud":   audience,
		"iat":   iat.Unix(),
		"exp":   iat.Add(time.Hour).Unix(),
		"nonce": nonce,
	}
}

// issuerFixture bundles a published, ACTIVE [issuerregistry.Issuer] with the
// store it lives in and the [trustfederation.IssuerResolver] a [oidc.Flow]
// resolves its signing keys through.
type issuerFixture struct {
	store    *issuerregistry.MemoryStore
	resolver trustfederation.IssuerResolver
	issuer   issuerregistry.Issuer
}

// newIssuerFixture publishes and activates one issuer revision for
// tenantAcme/issuerAcme carrying the given algorithm set, pinned keys and
// claim mappings.
func newIssuerFixture(t testing.TB, algs []trustfederation.Algorithm, keys []issuerregistry.PinnedKey, mappings []issuerregistry.ClaimMapping, skew time.Duration) issuerFixture {
	t.Helper()
	store := issuerregistry.NewMemoryStore()
	draft := issuerregistry.Issuer{
		Tenant:             tenantAcme,
		IssuerURL:          issuerAcme,
		Audience:           audienceAcme,
		JWKS:               issuerregistry.JWKSSource{Kind: issuerregistry.JWKSSourcePinnedKeys, PinnedKeys: keys},
		Algorithms:         algs,
		ClaimMappings:      mappings,
		ClockSkew:          skew,
		MetadataStaleness:  24 * time.Hour,
		Revision:           1,
		PublisherPrincipal: "publisher-1",
		PublishedAt:        baseTime,
	}
	published, err := issuerregistry.Publish(store, draft)
	if err != nil {
		t.Fatalf("Publish issuer: %v", err)
	}
	if _, err := issuerregistry.Activate(store, published.Ref(), issuerregistry.Evidence{ActedBy: "approver-1", At: baseTime}); err != nil {
		t.Fatalf("Activate issuer: %v", err)
	}
	resolver := issuerregistry.NewTenantResolver(store, tenantAcme, nil)
	return issuerFixture{store: store, resolver: resolver, issuer: published}
}

// newClientSource builds a [oidc.ClientSource] with one registration bound
// to tenantAcme/issuerAcme.
func newClientSource() oidc.ClientSource {
	return oidc.NewStaticClientSource().WithClient(oidc.ClientRegistration{
		Tenant:                tenantAcme,
		IssuerURL:             issuerAcme,
		ClientID:              clientIDAcme,
		RedirectURI:           redirectURI,
		AuthorizationEndpoint: authEndpoint,
		TokenEndpoint:         tokenEndpoint,
		Scopes:                []string{"openid", "profile"},
	})
}

// newFlow builds a [oidc.Flow] wired to a fresh in-memory state store, the
// given issuer fixture and client source, and fake exchanger, with a fixed
// deterministic secret so PKCE derivation is reproducible across a test's
// own BeginAuthorization/HandleCallback calls.
func newFlow(t testing.TB, iss issuerFixture, clients oidc.ClientSource, exchanger oidc.TokenExchanger, sink oidc.EvidenceSink) *oidc.Flow {
	t.Helper()
	flow, err := oidc.NewFlow(oidc.FlowConfig{
		Registry:  iss.store,
		Keys:      iss.resolver,
		Clients:   clients,
		States:    oidc.NewMemoryStateStore(),
		Exchanger: exchanger,
		Secret:    testFlowSecret,
		Sink:      sink,
	})
	if err != nil {
		t.Fatalf("NewFlow: %v", err)
	}
	return flow
}

// fakeCode is one authorization code the [fakeIdP] will honor, mirroring
// what a real identity provider records when it issues a code after the
// user authenticates and consents at the authorization endpoint.
type fakeCode struct {
	codeChallenge string
	redirectURI   string
	clientID      string
	clientSecret  string
	idToken       string
	accessToken   string
}

// fakeIdP is the in-memory identity provider double behind
// [oidc.TokenExchanger]: it performs the same checks RFC 6749/7636 require
// a real token endpoint to perform (one-time code use, redirect_uri and
// client_id binding, optional client secret, PKCE S256 verification) and
// answers with the same OAuth error shape a real IdP would.
type fakeIdP struct {
	mu    sync.Mutex
	codes map[string]fakeCode
}

func newFakeIdP() *fakeIdP { return &fakeIdP{codes: make(map[string]fakeCode)} }

// issueCode registers code as redeemable exactly once against the given
// binding, mirroring what happens out of band (the browser's authorization
// endpoint round trip) before a test drives [oidc.Flow.HandleCallback].
func (f *fakeIdP) issueCode(code string, c fakeCode) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codes[code] = c
}

func (f *fakeIdP) Exchange(_ context.Context, req oidc.TokenRequest) (oidc.TokenResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.codes[req.Code]
	if !ok {
		return oidc.TokenResponse{Error: "invalid_grant", ErrorDescription: "unknown or already used code"}, nil
	}
	delete(f.codes, req.Code) // one-time use: a replay of this code always misses above.
	if c.redirectURI != req.RedirectURI || c.clientID != req.ClientID {
		return oidc.TokenResponse{Error: "invalid_grant", ErrorDescription: "redirect_uri or client_id mismatch"}, nil
	}
	if c.clientSecret != "" && c.clientSecret != req.ClientSecret {
		return oidc.TokenResponse{Error: "invalid_client", ErrorDescription: "client authentication failed"}, nil
	}
	sum := sha256.Sum256([]byte(req.CodeVerifier))
	if b64(sum[:]) != c.codeChallenge {
		return oidc.TokenResponse{Error: "invalid_grant", ErrorDescription: "pkce_verification_failed"}, nil
	}
	return oidc.TokenResponse{AccessToken: c.accessToken, TokenType: "Bearer", IDToken: c.idToken}, nil
}

var _ oidc.TokenExchanger = (*fakeIdP)(nil)

// errExchanger is a [oidc.TokenExchanger] that always fails at the
// transport level (a network error), as opposed to [fakeIdP], which fails
// at the OAuth protocol level.
type errExchanger struct{ err error }

func (e errExchanger) Exchange(context.Context, oidc.TokenRequest) (oidc.TokenResponse, error) {
	return oidc.TokenResponse{}, e.err
}

var _ oidc.TokenExchanger = errExchanger{}

// recordingSink is a [oidc.EvidenceSink] that appends every record it
// receives, for tests asserting on emitted evidence.
type recordingSink struct {
	mu      sync.Mutex
	records []oidc.Evidence
}

func (s *recordingSink) Record(ev oidc.Evidence) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, ev)
}

func (s *recordingSink) all() []oidc.Evidence {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]oidc.Evidence, len(s.records))
	copy(out, s.records)
	return out
}

// computeAtHashForTest independently computes the OIDC Core §3.1.3.6
// at_hash value for accessToken, mirroring idtoken.go's unexported
// computeAtHash so a test can craft an ID token with a correct (or
// deliberately wrong) at_hash without reaching into the package's
// internals.
func computeAtHashForTest(accessToken string) string {
	sum := sha256.Sum256([]byte(accessToken))
	half := sum[:len(sum)/2]
	return b64(half)
}

// swappableClientSource lets a test change which [oidc.ClientSource] backs
// lookups mid-test, simulating a client registration being deprovisioned
// between [oidc.Flow.BeginAuthorization] and [oidc.Flow.HandleCallback].
type swappableClientSource struct {
	mu      sync.Mutex
	current oidc.ClientSource
}

func newSwappableClientSource(initial oidc.ClientSource) *swappableClientSource {
	return &swappableClientSource{current: initial}
}

func (s *swappableClientSource) swap(next oidc.ClientSource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = next
}

func (s *swappableClientSource) LookupClient(tenant values.TenantId, issuerURL string) (oidc.ClientRegistration, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current.LookupClient(tenant, issuerURL)
}

var _ oidc.ClientSource = (*swappableClientSource)(nil)

// queryValue is a small helper extracting one query parameter from a raw
// authorization request URL, failing the test if the URL does not parse.
func queryValue(t testing.TB, rawURL, key string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse authorization URL %q: %v", rawURL, err)
	}
	return u.Query().Get(key)
}
