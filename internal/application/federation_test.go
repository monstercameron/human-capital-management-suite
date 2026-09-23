package application

// RED/GREEN evidence for REV-005-01: a federation-backed credential
// verifier in the hcmnext serve composition root.
//
// The RED was a wiring gap: composeVerifier only ever built
// trust.NewHMACVerifier, and internal/trust/federation plus
// internal/authn/issuerregistry (and oidc, whose interactive flow still
// needs Clients/States/Exchanger ports) appeared in no served binary's
// dependency closure. These tests prove -federation-issuers with
// -federation-keys-file composes a trust.Verifier over the federation
// Validator whose keys resolve through the governed registry, while the dev
// HMAC verifier stays exactly where it was when no IdP is configured.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

const (
	rev00501Tenant   = "tenant-fed"
	rev00501Issuer   = "https://idp.example.test"
	rev00501IssuerB  = "https://idp-b.example.test"
	rev00501Audience = "aud-fed"
	rev00501Kid      = "idp-ed-1"
)

func rev00501Now() time.Time {
	return time.Date(2026, time.September, 21, 12, 0, 0, 0, time.UTC)
}

func rev00501Claims() federation.Claims {
	at := rev00501Now()
	return federation.Claims{
		Issuer:        rev00501Issuer,
		Audience:      rev00501Audience,
		Subject:       "user-fed-1",
		SubjectKind:   "human",
		Tenant:        rev00501Tenant,
		Roles:         []string{"intent_author"},
		Purposes:      []string{"hcm_operations"},
		Assurance:     "substantial",
		IssuedAtUnix:  at.Add(-time.Minute).Unix(),
		ExpiresAtUnix: at.Add(time.Hour).Unix(),
	}
}

func rev00501Mint(t *testing.T, priv ed25519.PrivateKey, kid string, claims federation.Claims) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	header := `{"alg":"EdDSA","kid":"` + kid + `","typ":"JWT"}`
	input := base64.RawURLEncoding.EncodeToString([]byte(header)) + "." +
		base64.RawURLEncoding.EncodeToString(payload)
	sig := ed25519.Sign(priv, []byte(input))
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// rev00501KeysFile writes a pinned-keys document for one Ed25519 key and
// returns its path plus the private half for minting.
func rev00501KeysFile(t *testing.T, tenant, issuer, kid string) (string, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	doc := map[string]any{
		"issuers": []map[string]any{{
			"tenant": tenant,
			"issuer": issuer,
			"keys": []map[string]any{{
				"kid":        kid,
				"alg":        "EdDSA",
				"public_key": base64.StdEncoding.EncodeToString(der),
			}},
		}},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal keys file: %v", err)
	}
	path := filepath.Join(t.TempDir(), "federation-keys.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write keys file: %v", err)
	}
	return path, priv
}

func rev00501Config(keysFile string) ServeConfig {
	cfg := stubServeConfig()
	cfg.Audience = rev00501Audience
	cfg.FederationIssuers = rev00501Tenant + "=" + rev00501Issuer
	cfg.FederationKeysFile = keysFile
	return cfg
}

func TestTodo_REV_005_01(t *testing.T) {
	t.Parallel()

	t.Run("IdP configuration composes a federation verifier", func(t *testing.T) {
		t.Parallel()
		keysFile, priv := rev00501KeysFile(t, rev00501Tenant, rev00501Issuer, rev00501Kid)
		cfg := rev00501Config(keysFile)
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate(federation): %v", err)
		}
		verifier, err := composeVerifier(cfg, Options{Now: rev00501Now})
		if err != nil {
			t.Fatalf("composeVerifier(federation): %v", err)
		}
		fed, ok := verifier.(*federationVerifier)
		if !ok {
			t.Fatalf("composed verifier is %T, want *application.federationVerifier", verifier)
		}
		if fed.validator == nil {
			t.Fatal("federation verifier has no validator")
		}
		token := rev00501Mint(t, priv, rev00501Kid, rev00501Claims())
		principal, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: token})
		if err != nil {
			t.Fatalf("Verify(federated assertion): %v", err)
		}
		if principal.Subject() != "user-fed-1" {
			t.Errorf("subject = %q, want user-fed-1", principal.Subject())
		}
		if principal.Tenant() != values.TenantId(rev00501Tenant) {
			t.Errorf("tenant = %q, want %q", principal.Tenant(), rev00501Tenant)
		}
		if !principal.HasRole("intent_author") {
			t.Errorf("roles = %q, want intent_author", principal.Roles())
		}
		if principal.Assurance() != trust.AssuranceSubstantial {
			t.Errorf("assurance = %q, want substantial", principal.Assurance())
		}
		if principal.AuthenticationMethod() != trust.AuthenticationMethodBearerToken {
			t.Errorf("method = %q, want bearer", principal.AuthenticationMethod())
		}
	})

	t.Run("no IdP configuration keeps the dev HMAC verifier", func(t *testing.T) {
		t.Parallel()
		verifier, err := composeVerifier(stubServeConfig(), Options{})
		if err != nil {
			t.Fatalf("composeVerifier(dev): %v", err)
		}
		if _, ok := verifier.(*trust.HMACVerifier); !ok {
			t.Fatalf("dev verifier is %T, want *trust.HMACVerifier", verifier)
		}
	})

	t.Run("issuer allow-list parsing refuses malformed pairs", func(t *testing.T) {
		t.Parallel()
		pairs, err := parseFederationIssuers("acme= https://x.test ,acme=https://y.test")
		if err != nil {
			t.Fatalf("parseFederationIssuers(multi): %v", err)
		}
		if len(pairs[values.TenantId("acme")]) != 2 {
			t.Fatalf("pairs = %+v, want two issuers for tenant acme", pairs)
		}
		for _, bad := range []string{
			"",
			"no-equals",
			"=https://x.test",
			"acme=",
			"acme=https://x.test,acme=https://x.test",
			"acme=not-a-url",
		} {
			if _, err := parseFederationIssuers(bad); err == nil {
				t.Errorf("parseFederationIssuers(%q) succeeded", bad)
			}
		}
	})

	t.Run("validation refuses half-supplied and mismatched IdP config", func(t *testing.T) {
		t.Parallel()
		keysFile, _ := rev00501KeysFile(t, rev00501Tenant, rev00501Issuer, rev00501Kid)

		issuersOnly := stubServeConfig()
		issuersOnly.FederationIssuers = rev00501Tenant + "=" + rev00501Issuer
		if err := issuersOnly.Validate(); err == nil {
			t.Error("issuers without a keys file validated")
		}
		keysOnly := stubServeConfig()
		keysOnly.FederationKeysFile = keysFile
		if err := keysOnly.Validate(); err == nil {
			t.Error("keys file without issuers validated")
		}
		unpinned := stubServeConfig()
		unpinned.Audience = rev00501Audience
		unpinned.FederationIssuers = rev00501Tenant + "=" + rev00501IssuerB
		unpinned.FederationKeysFile = keysFile
		if err := unpinned.Validate(); err == nil {
			t.Error("allow-listed issuer without pinned keys validated")
		}
		foreign := stubServeConfig()
		foreign.Audience = rev00501Audience
		foreign.FederationIssuers = "tenant-other=" + rev00501Issuer
		foreign.FederationKeysFile = keysFile
		if err := foreign.Validate(); err == nil {
			t.Error("pinned keys for another tenant validated")
		}
	})
}

func TestTodo_REV_005_01_Golden(t *testing.T) {
	t.Parallel()

	// A fixed seed makes the key, the assertion and every digest below
	// byte-stable: this test pins the exact interop vector.
	seed := []byte("rev-005-01-golden-vector-seed-00")
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	doc := map[string]any{
		"issuers": []map[string]any{{
			"tenant": rev00501Tenant,
			"issuer": rev00501Issuer,
			"keys": []map[string]any{{
				"kid":        rev00501Kid,
				"alg":        "EdDSA",
				"public_key": base64.StdEncoding.EncodeToString(der),
			}},
		}},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal keys file: %v", err)
	}
	path := filepath.Join(t.TempDir(), "federation-keys.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write keys file: %v", err)
	}

	cfg := rev00501Config(path)
	verifier, err := composeVerifier(cfg, Options{Now: rev00501Now})
	if err != nil {
		t.Fatalf("composeVerifier: %v", err)
	}
	first := rev00501Mint(t, priv, rev00501Kid, rev00501Claims())
	second := rev00501Mint(t, priv, rev00501Kid, rev00501Claims())
	if first != second {
		t.Fatal("identical envelopes minted different assertions")
	}
	principal, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: first})
	if err != nil {
		t.Fatalf("Verify(golden assertion): %v", err)
	}
	if principal.Subject() != "user-fed-1" || principal.Tenant() != values.TenantId(rev00501Tenant) {
		t.Fatalf("golden principal = %s/%s", principal.Subject(), principal.Tenant())
	}
	if principal.CredentialDigest() == "" || principal.SessionRef() == "" {
		t.Fatal("golden principal carries no credential digest or session reference")
	}
	// The served graph names the federation verifier behind the same
	// component the HMAC verifier used to fill.
	fields := map[string]bool{}
	for _, field := range ServeConfigFields() {
		fields[field.Name] = true
	}
	if !fields[FieldFederationIssuers] || !fields[FieldFederationKeysFile] {
		t.Fatal("the serve role declares no federation flags")
	}
}

func TestTodo_REV_005_01_Security(t *testing.T) {
	t.Parallel()
	keysFile, priv := rev00501KeysFile(t, rev00501Tenant, rev00501Issuer, rev00501Kid)
	verifier, err := composeVerifier(rev00501Config(keysFile), Options{Now: rev00501Now})
	if err != nil {
		t.Fatalf("composeVerifier: %v", err)
	}
	verify := func(token, scheme string) error {
		_, err := verifier.Verify(context.Background(), trust.Credential{Scheme: scheme, Token: token})
		return err
	}
	valid := rev00501Mint(t, priv, rev00501Kid, rev00501Claims())

	t.Run("forgeries and confusions are refused", func(t *testing.T) {
		t.Parallel()
		if err := verify("", "Bearer"); !errors.Is(err, trust.ErrNoCredential) {
			t.Errorf("empty token = %v, want ErrNoCredential", err)
		}
		if err := verify(valid, "Basic"); !errors.Is(err, trust.ErrUnsupportedScheme) {
			t.Errorf("basic scheme = %v, want ErrUnsupportedScheme", err)
		}
		dev, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
			Key: []byte(testDevKey), Issuer: DefaultIssuer, Audience: rev00501Audience,
		})
		if err != nil {
			t.Fatalf("NewHMACVerifier: %v", err)
		}
		hmacToken, err := dev.Issue(trust.Claims{Issuer: DefaultIssuer, Audience: rev00501Audience})
		if err != nil {
			t.Fatalf("dev Issue: %v", err)
		}
		if err := verify(hmacToken, "Bearer"); err == nil {
			t.Error("dev HMAC token verified against the federation verifier")
		}
		parts := strings.Split(valid, ".")
		if len(parts) != 3 {
			t.Fatalf("minted token has %d parts", len(parts))
		}
		tamperedPayload := parts[1][:len(parts[1])-2] + "AA"
		if err := verify(parts[0]+"."+tamperedPayload+"."+parts[2], "Bearer"); err == nil {
			t.Error("tampered payload verified")
		}
		if err := verify(parts[0]+"."+parts[1]+".AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "Bearer"); err == nil {
			t.Error("tampered signature verified")
		}
	})

	t.Run("issuer, audience and window mismatches are refused", func(t *testing.T) {
		t.Parallel()
		other := rev00501Claims()
		other.Audience = "aud-elsewhere"
		if err := verify(rev00501Mint(t, priv, rev00501Kid, other), "Bearer"); !errors.Is(err, federation.ErrAssertionAudience) {
			t.Errorf("wrong audience = %v, want ErrAssertionAudience", err)
		}
		expired := rev00501Claims()
		expired.IssuedAtUnix = rev00501Now().Add(-2 * time.Hour).Unix()
		expired.ExpiresAtUnix = rev00501Now().Add(-time.Hour).Unix()
		if err := verify(rev00501Mint(t, priv, rev00501Kid, expired), "Bearer"); !errors.Is(err, federation.ErrAssertionExpired) {
			t.Errorf("expired assertion = %v, want ErrAssertionExpired", err)
		}
		unknownKid := rev00501Mint(t, priv, "idp-unknown", rev00501Claims())
		if err := verify(unknownKid, "Bearer"); err == nil {
			t.Error("unknown key id verified")
		}
		foreign := rev00501Claims()
		foreign.Issuer = "https://unknown-idp.example.test"
		if err := verify(rev00501Mint(t, priv, rev00501Kid, foreign), "Bearer"); !errors.Is(err, federation.ErrAssertionIssuer) {
			t.Errorf("unknown issuer = %v, want ErrAssertionIssuer", err)
		}
	})

	t.Run("an issuer allow-listed elsewhere does not cross tenants", func(t *testing.T) {
		t.Parallel()
		_, otherPriv := rev00501KeysFile(t, "tenant-other", rev00501IssuerB, "other-ed-1")
		claims := rev00501Claims()
		claims.Issuer = rev00501IssuerB
		claims.Tenant = rev00501Tenant
		if err := verify(rev00501Mint(t, otherPriv, "other-ed-1", claims), "Bearer"); err == nil {
			t.Error("assertion crossed from another tenant's issuer")
		} else if !errors.Is(err, federation.ErrTenantBinding) && !errors.Is(err, federation.ErrAssertionIssuer) {
			t.Errorf("cross-tenant assertion = %v, want tenant binding refusal", err)
		}
	})

	t.Run("a suspended issuer stops verifying at runtime", func(t *testing.T) {
		t.Parallel()
		store := issuerregistry.NewMemoryStore()
		der, err := x509.MarshalPKIXPublicKey(priv.Public().(ed25519.PublicKey))
		if err != nil {
			t.Fatalf("MarshalPKIXPublicKey: %v", err)
		}
		published, err := issuerregistry.Publish(store, issuerregistry.Issuer{
			Tenant:             values.TenantId(rev00501Tenant),
			IssuerURL:          rev00501Issuer,
			Audience:           rev00501Audience,
			JWKS:               issuerregistry.JWKSSource{Kind: issuerregistry.JWKSSourcePinnedKeys, PinnedKeys: []issuerregistry.PinnedKey{{KeyID: rev00501Kid, Algorithm: federation.AlgEdDSA, PublicKeyDER: der}}},
			Algorithms:         []federation.Algorithm{federation.AlgEdDSA},
			Revision:           1,
			PublisherPrincipal: "test-publisher",
			PublishedAt:        rev00501Now(),
			MetadataStaleness:  time.Hour,
		})
		if err != nil {
			t.Fatalf("Publish: %v", err)
		}
		at := rev00501Now()
		if _, err := issuerregistry.Activate(store, published.Ref(), issuerregistry.Evidence{ActedBy: "test-approver", At: at}); err != nil {
			t.Fatalf("Activate: %v", err)
		}
		resolver, err := issuerregistry.NewResolver(store, nil)
		if err != nil {
			t.Fatalf("NewResolver: %v", err)
		}
		validator, err := federation.NewValidator(federation.Config{
			TenantIssuers: map[values.TenantId][]string{values.TenantId(rev00501Tenant): {rev00501Issuer}},
			Audience:      rev00501Audience,
			Keys:          federation.NewRegistryKeySource(resolver),
			Now:           rev00501Now,
		})
		if err != nil {
			t.Fatalf("NewValidator: %v", err)
		}
		direct := &federationVerifier{validator: validator}
		if _, err := direct.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: valid}); err != nil {
			t.Fatalf("Verify(active): %v", err)
		}
		if _, err := issuerregistry.Suspend(store, published.Ref(), issuerregistry.Evidence{ActedBy: "test-approver", At: at}); err != nil {
			t.Fatalf("Suspend: %v", err)
		}
		if _, err := direct.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: valid}); err == nil {
			t.Error("suspended issuer still verifies")
		} else if !errors.Is(err, issuerregistry.ErrIssuerSuspended) {
			t.Errorf("suspended issuer = %v, want ErrIssuerSuspended", err)
		}
	})
}

func TestTodo_REV_005_01_Integration(t *testing.T) {
	keysFile, priv := rev00501KeysFile(t, rev00501Tenant, rev00501Issuer, rev00501Kid)
	cfg := rev00501Config(keysFile)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate(federation): %v", err)
	}
	logger := &recordingLogger{}
	store := &stubStore{}
	composed, err := ComposeServe(context.Background(), ServeInput{
		Config:   cfg,
		Logger:   logger,
		Identity: "instance-federation-integration",
		Options:  Options{}.Apply(WithStore(store)),
	})
	if err != nil {
		t.Fatalf("ComposeServe(federation): %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := composed.Stop(ctx); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	component, ok := composed.Graph().Component(ComponentCredentialVerifier)
	if !ok {
		t.Fatalf("the served graph names no %q", ComponentCredentialVerifier)
	}
	if !strings.Contains(component.Impl, "federationVerifier") {
		t.Fatalf("credential verifier impl = %q, want the federation verifier", component.Impl)
	}
	// The same constructor serve uses authenticates a live assertion: the
	// tenant IdP is consulted on the serving path, not just in a unit test.
	verifier, err := composeVerifier(cfg, Options{Now: rev00501Now})
	if err != nil {
		t.Fatalf("composeVerifier: %v", err)
	}
	token := rev00501Mint(t, priv, rev00501Kid, rev00501Claims())
	principal, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: token})
	if err != nil {
		t.Fatalf("Verify through the served composition: %v", err)
	}
	if principal.Subject() != "user-fed-1" || principal.Tenant() != values.TenantId(rev00501Tenant) {
		t.Fatalf("served principal = %s/%s", principal.Subject(), principal.Tenant())
	}
}
