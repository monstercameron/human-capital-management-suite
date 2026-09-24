package oidc

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	trustfederation "github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

var hardAt = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func TestPKCEHelpers_StableEncodingAndFreshTokens(t *testing.T) {
	t.Parallel()
	secret := []byte("01234567890123456789012345678901")
	verifier := deriveVerifier(secret, "state-1")
	if len(verifier) != 43 || strings.ContainsAny(verifier, "=+/ ") {
		t.Fatalf("deriveVerifier() = %q, want 43-character base64url text", verifier)
	}
	if verifier != deriveVerifier(secret, "state-1") {
		t.Fatalf("deriveVerifier is not deterministic")
	}
	if verifier == deriveVerifier(secret, "state-2") || verifier == deriveVerifier([]byte("different-secret"), "state-1") {
		t.Fatalf("deriveVerifier failed to bind both secret and state")
	}
	wantSum := sha256.Sum256([]byte(verifier))
	if got := codeChallengeS256(verifier); got != base64.RawURLEncoding.EncodeToString(wantSum[:]) {
		t.Fatalf("codeChallengeS256() = %q, want SHA-256 base64url digest", got)
	}
	first, err := randomToken(32)
	if err != nil {
		t.Fatalf("randomToken: %v", err)
	}
	second, err := randomToken(32)
	if err != nil {
		t.Fatalf("randomToken second call: %v", err)
	}
	if len(first) != 43 || len(second) != 43 || first == second {
		t.Fatalf("randomToken results = %q and %q, want two distinct 43-character tokens", first, second)
	}
}

func TestEvidenceAndClientValidation_RejectMalformedBindings(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.FixedZone("offset", -5*60*60))
	ev := newEvidence(values.TenantId("tenant-a"), "https://issuer.example/", "subject", OutcomeDenied, "reason", at)
	if ev.At.Location() != time.UTC || !ev.At.Equal(at) || ev.Outcome != OutcomeDenied || ev.EvidenceID == "" {
		t.Fatalf("newEvidence() = %+v, want UTC evidence with stable id", ev)
	}
	if !strings.HasPrefix(ev.EvidenceID, "ev:oidc:") || newEvidence(ev.Tenant, ev.IssuerURL, ev.Subject, ev.Outcome, ev.Reason, ev.At).EvidenceID != ev.EvidenceID {
		t.Fatalf("newEvidence id is not correctly namespaced or deterministic: %q", ev.EvidenceID)
	}

	base := ClientRegistration{
		Tenant: values.TenantId("tenant-a"), IssuerURL: "https://issuer.example/", ClientID: "client",
		RedirectURI: "https://app.example/callback", AuthorizationEndpoint: "https://issuer.example/authorize",
		TokenEndpoint: "https://issuer.example/token", Scopes: []string{"openid", "profile"},
	}
	cases := []struct {
		name   string
		mutate func(*ClientRegistration)
	}{
		{"missing tenant", func(c *ClientRegistration) { c.Tenant = "" }},
		{"missing issuer", func(c *ClientRegistration) { c.IssuerURL = " " }},
		{"missing client id", func(c *ClientRegistration) { c.ClientID = "\t" }},
		{"missing redirect", func(c *ClientRegistration) { c.RedirectURI = "" }},
		{"relative redirect", func(c *ClientRegistration) { c.RedirectURI = "/callback" }},
		{"missing authorization endpoint", func(c *ClientRegistration) { c.AuthorizationEndpoint = "" }},
		{"relative token endpoint", func(c *ClientRegistration) { c.TokenEndpoint = "token" }},
		{"missing openid scope", func(c *ClientRegistration) { c.Scopes = []string{"profile"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := base
			tc.mutate(&got)
			if err := got.validate(); !errors.Is(err, ErrInvalidClientRegistration) {
				t.Fatalf("validate() error = %v, want ErrInvalidClientRegistration", err)
			}
		})
	}
	if err := base.validate(); err != nil {
		t.Fatalf("valid client rejected: %v", err)
	}
	for name, raw := range map[string]string{"empty": "", "relative": "/x", "bad": "https://"} {
		t.Run("absolute URL "+name, func(t *testing.T) {
			if err := validateAbsoluteURL("endpoint", raw); !errors.Is(err, ErrInvalidClientRegistration) {
				t.Fatalf("validateAbsoluteURL(%q) = %v, want ErrInvalidClientRegistration", raw, err)
			}
		})
	}
}

func TestIDTokenParsing_RejectsMalformedAndTrailingJSON(t *testing.T) {
	t.Parallel()
	header := `{"alg":"RS256","kid":"kid-1"}`
	payload := `{"sub":"user"}`
	encode := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	valid := encode(header) + "." + encode(payload) + "." + encode("signature")
	if got, input, body, sig, err := splitCompactJWS(valid, 1000); err != nil || got.Algorithm != "RS256" || got.KeyID != "kid-1" || input == "" || string(body) != payload || string(sig) != "signature" {
		t.Fatalf("valid splitCompactJWS() = header=%+v input=%q payload=%q sig=%q err=%v", got, input, body, sig, err)
	}
	cases := map[string]string{
		"empty":                  "",
		"oversized":              strings.Repeat("x", 11),
		"wrong segment count":    "a.b",
		"empty segment":          "a..c",
		"bad header encoding":    "!.b.c",
		"bad header JSON":        encode("not-json") + ".b.c",
		"unknown header field":   encode(`{"alg":"RS256","kid":"k","extra":1}`) + ".b.c",
		"trailing header JSON":   encode(header+` {"alg":"RS256"}`) + "." + encode(payload) + ".c",
		"bad payload encoding":   encode(header) + ".!.c",
		"bad signature encoding": encode(header) + "." + encode(payload) + ".!",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, _, _, err := splitCompactJWS(raw, 10); !errors.Is(err, ErrIDTokenMalformed) {
				t.Fatalf("splitCompactJWS(%q) = %v, want ErrIDTokenMalformed", raw, err)
			}
		})
	}
	if _, _, _, _, err := splitCompactJWS(valid, 5); !errors.Is(err, ErrIDTokenMalformed) {
		t.Fatalf("size limit error = %v, want ErrIDTokenMalformed", err)
	}
}

func TestIDTokenPrimitiveChecks_BoundariesAndShapes(t *testing.T) {
	t.Parallel()
	at := time.Unix(100, 0).UTC()
	window := trustfederation.SigningKey{NotBefore: at, NotAfter: at.Add(time.Hour)}
	for name, tc := range map[string]struct {
		at   time.Time
		want bool
	}{
		"before":           {at: at.Add(-time.Nanosecond), want: false},
		"at not before":    {at: at, want: true},
		"before not after": {at: at.Add(time.Hour - time.Nanosecond), want: true},
		"at not after":     {at: at.Add(time.Hour), want: false},
		"after":            {at: at.Add(2 * time.Hour), want: false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := keyWithinWindow(window, tc.at); got != tc.want {
				t.Fatalf("keyWithinWindow(%s) = %v, want %v", name, got, tc.want)
			}
		})
	}
	if !keyWithinWindow(trustfederation.SigningKey{}, at) {
		t.Fatalf("zero validity window should be unbounded")
	}
	if got := computeAtHash("access-token"); got == "" || got != computeAtHash("access-token") || got == computeAtHash("other-token") {
		t.Fatalf("computeAtHash is not deterministic and input-bound: %q", got)
	}
	for _, tc := range []struct {
		a, b string
		want bool
	}{{"same", "same", true}, {"same", "different", false}, {"short", "longer", false}} {
		if got := constantTimeStringsEqual(tc.a, tc.b); got != tc.want {
			t.Fatalf("constantTimeStringsEqual(%q,%q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
	for name, raw := range map[string]string{"string": `"client"`, "array": `["client","other"]`, "number": `42`, "object": `{}`} {
		t.Run("audience "+name, func(t *testing.T) {
			var got stringOrList
			err := json.Unmarshal([]byte(raw), &got)
			if name == "number" || name == "object" {
				if err == nil {
					t.Fatalf("UnmarshalJSON(%s) succeeded, want error", raw)
				}
				return
			}
			if err != nil || len(got) == 0 {
				t.Fatalf("UnmarshalJSON(%s) = %v, %v", raw, got, err)
			}
		})
	}
}

func TestValidateIDToken_RefusalMatrix(t *testing.T) {
	t.Parallel()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	issuer := issuerregistry.Issuer{Tenant: values.TenantId("tenant-a"), IssuerURL: "https://issuer.example/", Audience: "client", Algorithms: []trustfederation.Algorithm{trustfederation.AlgRS256}}
	baseClaims := func() map[string]any {
		return map[string]any{"iss": issuer.IssuerURL, "sub": "user", "aud": issuer.Audience, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "nonce": "nonce"}
	}
	validKey := trustfederation.SigningKey{ID: "kid-1", Algorithm: trustfederation.AlgRS256, Public: &priv.PublicKey}
	sign := func(header string, claims any) string { return hardSignedToken(t, priv, header, claims) }
	cases := []struct {
		name  string
		token func() string
		keys  trustfederation.IssuerResolver
		iss   issuerregistry.Issuer
		want  error
	}{
		{"bad typ", func() string { return sign(`{"alg":"RS256","kid":"kid-1","typ":"JWS"}`, baseClaims()) }, &hardResolver{keys: []trustfederation.SigningKey{validKey}}, issuer, ErrIDTokenMalformed},
		{"unsupported algorithm", func() string { return sign(`{"alg":"HS256","kid":"kid-1"}`, baseClaims()) }, &hardResolver{keys: []trustfederation.SigningKey{validKey}}, issuer, ErrUnsupportedAlgorithm},
		{"issuer algorithm not declared", func() string { return sign(`{"alg":"RS256","kid":"kid-1"}`, baseClaims()) }, &hardResolver{keys: []trustfederation.SigningKey{validKey}}, issuerWithAlgorithms(issuer, trustfederation.AlgES256), ErrUnsupportedAlgorithm},
		{"missing key id", func() string { return sign(`{"alg":"RS256"}`, baseClaims()) }, &hardResolver{keys: []trustfederation.SigningKey{validKey}}, issuer, ErrIDTokenMalformed},
		{"nil resolver", func() string { return sign(`{"alg":"RS256","kid":"kid-1"}`, baseClaims()) }, nil, issuer, ErrKeyNotFound},
		{"resolver failure", func() string { return sign(`{"alg":"RS256","kid":"kid-1"}`, baseClaims()) }, &hardResolver{err: errors.New("key backend down")}, issuer, ErrKeyNotFound},
		{"unknown key", func() string { return sign(`{"alg":"RS256","kid":"unknown"}`, baseClaims()) }, &hardResolver{keys: []trustfederation.SigningKey{validKey}}, issuer, ErrKeyNotFound},
		{"algorithm downgrade", func() string { return sign(`{"alg":"RS256","kid":"kid-1"}`, baseClaims()) }, &hardResolver{keys: []trustfederation.SigningKey{{ID: "kid-1", Algorithm: trustfederation.AlgES256, Public: &priv.PublicKey}}}, issuer, ErrAlgorithmDowngrade},
		{"expired key", func() string { return sign(`{"alg":"RS256","kid":"kid-1"}`, baseClaims()) }, &hardResolver{keys: []trustfederation.SigningKey{{ID: "kid-1", Algorithm: trustfederation.AlgRS256, Public: &priv.PublicKey, NotAfter: now}}}, issuer, ErrKeyExpired},
		{"invalid signature", func() string {
			raw := sign(`{"alg":"RS256","kid":"kid-1"}`, baseClaims())
			return raw[:len(raw)-3] + "bad"
		}, &hardResolver{keys: []trustfederation.SigningKey{validKey}}, issuer, ErrSignatureInvalid},
		{"invalid claims JSON", func() string { return hardSignedToken(t, priv, `{"alg":"RS256","kid":"kid-1"}`, "not-json") }, &hardResolver{keys: []trustfederation.SigningKey{validKey}}, issuer, ErrIDTokenMalformed},
		{"wrong issuer", func() string {
			c := baseClaims()
			c["iss"] = "https://other.example/"
			return sign(`{"alg":"RS256","kid":"kid-1"}`, c)
		}, &hardResolver{keys: []trustfederation.SigningKey{validKey}}, issuer, ErrWrongIssuer},
		{"wrong audience", func() string { c := baseClaims(); c["aud"] = "other"; return sign(`{"alg":"RS256","kid":"kid-1"}`, c) }, &hardResolver{keys: []trustfederation.SigningKey{validKey}}, issuer, ErrWrongAudience},
		{"missing validity", func() string { c := baseClaims(); delete(c, "exp"); return sign(`{"alg":"RS256","kid":"kid-1"}`, c) }, &hardResolver{keys: []trustfederation.SigningKey{validKey}}, issuer, ErrIDTokenExpired},
		{"at hash without access token", func() string {
			c := baseClaims()
			c["at_hash"] = computeAtHash("access")
			return sign(`{"alg":"RS256","kid":"kid-1"}`, c)
		}, &hardResolver{keys: []trustfederation.SigningKey{validKey}}, issuer, ErrAccessTokenMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateIDToken(context.Background(), tc.keys, tc.iss, tc.iss.Audience, tc.token(), "nonce", "", now)
			if !errors.Is(err, tc.want) {
				t.Fatalf("validateIDToken() = %v, want errors.Is(...,%v)", err, tc.want)
			}
		})
	}
	result, err := validateIDToken(context.Background(), &hardResolver{keys: []trustfederation.SigningKey{validKey}}, issuer, issuer.Audience, sign(`{"alg":"RS256","kid":"kid-1"}`, baseClaims()), "nonce", "access", now)
	if err != nil || result.standard.Subject != "user" || len(result.raw) == 0 {
		t.Fatalf("valid validateIDToken() = %+v,%v", result, err)
	}
}

func issuerWithAlgorithms(issuer issuerregistry.Issuer, algorithms ...trustfederation.Algorithm) issuerregistry.Issuer {
	issuer.Algorithms = algorithms
	return issuer
}

func hardSignedToken(t *testing.T, priv *rsa.PrivateKey, header string, claims any) string {
	t.Helper()
	headerPart := base64.RawURLEncoding.EncodeToString([]byte(header))
	var payload []byte
	var err error
	if text, ok := claims.(string); ok {
		payload = []byte(text)
	} else {
		payload, err = json.Marshal(claims)
	}
	if err != nil {
		t.Fatal(err)
	}
	payloadPart := base64.RawURLEncoding.EncodeToString(payload)
	input := headerPart + "." + payloadPart
	sum := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

type hardResolver struct {
	keys []trustfederation.SigningKey
	err  error
}

func (r *hardResolver) ResolveIssuerKeys(context.Context, string) ([]trustfederation.SigningKey, error) {
	return r.keys, r.err
}

func TestVerifyIDTokenSignature_AllAlgorithmsAndFailures(t *testing.T) {
	t.Parallel()
	input := "header.payload"
	sum := sha256.Sum256([]byte(input))
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaSig, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	r, s, err := ecdsa.Sign(rand.Reader, ecdsaKey, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	ecdsaSig := append(leftPad(r, 32), leftPad(s, 32)...)
	edPub, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	edSig := ed25519.Sign(edPriv, []byte(input))
	cases := []struct {
		name string
		alg  trustfederation.Algorithm
		pub  crypto.PublicKey
		sig  []byte
		want error
	}{
		{"RS256 valid", trustfederation.AlgRS256, &rsaKey.PublicKey, rsaSig, nil},
		{"RS256 bad signature", trustfederation.AlgRS256, &rsaKey.PublicKey, []byte("bad"), ErrSignatureInvalid},
		{"RS256 wrong key type", trustfederation.AlgRS256, edPub, rsaSig, ErrUnsupportedAlgorithm},
		{"ES256 valid", trustfederation.AlgES256, &ecdsaKey.PublicKey, ecdsaSig, nil},
		{"ES256 bad length", trustfederation.AlgES256, &ecdsaKey.PublicKey, []byte("bad"), ErrSignatureInvalid},
		{"ES256 wrong curve", trustfederation.AlgES256, mustECDSAKey(t, elliptic.P384()), ecdsaSig, ErrUnsupportedAlgorithm},
		{"ES256 wrong key type", trustfederation.AlgES256, &rsaKey.PublicKey, ecdsaSig, ErrUnsupportedAlgorithm},
		{"EdDSA valid", trustfederation.AlgEdDSA, edPub, edSig, nil},
		{"EdDSA bad length", trustfederation.AlgEdDSA, edPub, []byte("bad"), ErrSignatureInvalid},
		{"EdDSA wrong key type", trustfederation.AlgEdDSA, &rsaKey.PublicKey, edSig, ErrUnsupportedAlgorithm},
		{"unknown algorithm", trustfederation.Algorithm("none"), edPub, edSig, ErrUnsupportedAlgorithm},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyIDTokenSignature(tc.alg, tc.pub, input, tc.sig)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("verifyIDTokenSignature() = %v, want nil", err)
				}
			} else if !errors.Is(err, tc.want) {
				t.Fatalf("verifyIDTokenSignature() = %v, want errors.Is(...,%v)", err, tc.want)
			}
		})
	}
}

func leftPad(v *big.Int, width int) []byte {
	out := make([]byte, width)
	b := v.Bytes()
	copy(out[width-len(b):], b)
	return out
}

func mustECDSAKey(t *testing.T, curve elliptic.Curve) *ecdsa.PublicKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &key.PublicKey
}

func TestClaimHelpers_ClosedVocabulariesAndMappings(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"human", "service", "agent", "integration"} {
		if kind, ok := parseSubjectKind(raw); !ok || kind == 0 {
			t.Fatalf("parseSubjectKind(%q) = %v, %v", raw, kind, ok)
		}
	}
	if _, ok := parseSubjectKind("workload"); ok {
		t.Fatalf("unknown subject kind accepted")
	}
	for _, raw := range []string{"low", "substantial", "high"} {
		if assurance, ok := parseAssurance(raw); !ok || assurance == 0 {
			t.Fatalf("parseAssurance(%q) = %v, %v", raw, assurance, ok)
		}
	}
	if _, ok := parseAssurance("maximum"); ok {
		t.Fatalf("unknown assurance accepted")
	}
	if got, err := decodeClaimString(json.RawMessage(`"value"`)); err != nil || got != "value" {
		t.Fatalf("decodeClaimString string = %q,%v", got, err)
	}
	if _, err := decodeClaimString(json.RawMessage(`123`)); err == nil {
		t.Fatalf("decodeClaimString number succeeded")
	}
	for raw, want := range map[string][]string{`["a","b"]`: {"a", "b"}, `"a  b"`: {"a", "b"}} {
		got, err := decodeClaimStringSlice(json.RawMessage(raw))
		if err != nil || fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("decodeClaimStringSlice(%s) = %v,%v, want %v", raw, got, err, want)
		}
	}
	if _, err := decodeClaimStringSlice(json.RawMessage(`{"bad":true}`)); err == nil {
		t.Fatalf("decodeClaimStringSlice object succeeded")
	}

	var spec trust.PrincipalSpec
	mappings := []struct {
		target issuerregistry.PrincipalField
		value  string
		check  func() bool
	}{
		{issuerregistry.PrincipalFieldSubject, `"subject-2"`, func() bool { return spec.Subject == "subject-2" }},
		{issuerregistry.PrincipalFieldSubjectKind, `"service"`, func() bool { return spec.SubjectKind == trust.SubjectKindService }},
		{issuerregistry.PrincipalFieldOrganizationScopeID, `"org"`, func() bool { return spec.OrganizationScopeID == "org" }},
		{issuerregistry.PrincipalFieldRoles, `["admin"]`, func() bool { return len(spec.Roles) == 1 && spec.Roles[0] == "admin" }},
		{issuerregistry.PrincipalFieldAuthorityRefs, `"authority:x"`, func() bool { return len(spec.AuthorityRefs) == 1 }},
		{issuerregistry.PrincipalFieldPurposes, `"read write"`, func() bool { return len(spec.Purposes) == 2 }},
		{issuerregistry.PrincipalFieldAssurance, `"high"`, func() bool { return spec.Assurance == trust.AssuranceHigh }},
		{issuerregistry.PrincipalFieldDelegationRefs, `["delegation:x"]`, func() bool { return len(spec.DelegationRefs) == 1 }},
	}
	for _, tc := range mappings {
		if err := applyClaimMapping(&spec, tc.target, json.RawMessage(tc.value)); err != nil || !tc.check() {
			t.Fatalf("applyClaimMapping(%q) = %v, spec=%+v", tc.target, err, spec)
		}
	}
	for _, tc := range []struct {
		target issuerregistry.PrincipalField
		value  string
	}{
		{issuerregistry.PrincipalFieldSubjectKind, `"unknown"`},
		{issuerregistry.PrincipalFieldAssurance, `"unknown"`},
		{issuerregistry.PrincipalFieldRoles, `42`},
		{issuerregistry.PrincipalField("future"), `"value"`},
	} {
		if err := applyClaimMapping(&spec, tc.target, json.RawMessage(tc.value)); err == nil {
			t.Fatalf("applyClaimMapping(%q) accepted invalid input", tc.target)
		}
	}
	issuer := issuerregistry.Issuer{Tenant: values.TenantId("tenant-a"), ClaimMappings: []issuerregistry.ClaimMapping{{SourceClaim: "role", Target: issuerregistry.PrincipalFieldRoles}}}
	std := stdIDTokenClaims{Subject: "verified-sub", IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(2, 0)}
	got, err := mapClaims(issuer, map[string]json.RawMessage{"role": json.RawMessage(`["reader"]`)}, std, "cred", "session")
	if err != nil || got.Subject != "verified-sub" || len(got.Roles) != 1 {
		t.Fatalf("mapClaims defaults/mapping = %+v,%v", got, err)
	}
	if _, err := mapClaims(issuer, nil, stdIDTokenClaims{}, "cred", "session"); !errors.Is(err, ErrClaimMapping) {
		t.Fatalf("mapClaims missing subject = %v", err)
	}
}

func TestFlowAuthorizationURLAndCallbackRefusals(t *testing.T) {
	t.Parallel()
	client := ClientRegistration{ClientID: "client", RedirectURI: "https://app.example/cb", AuthorizationEndpoint: "https://issuer.example/authorize?fixed=1", Scopes: []string{"openid", "profile"}}
	raw, err := buildAuthorizationURL(client, "state", "nonce", "challenge")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	for key, want := range map[string]string{"fixed": "1", "response_type": "code", "client_id": "client", "redirect_uri": client.RedirectURI, "scope": "openid profile", "state": "state", "nonce": "nonce", "code_challenge": "challenge", "code_challenge_method": "S256"} {
		if q.Get(key) != want {
			t.Fatalf("authorization query %s = %q, want %q", key, q.Get(key), want)
		}
	}
	if _, err := buildAuthorizationURL(ClientRegistration{AuthorizationEndpoint: "://bad"}, "s", "n", "c"); !errors.Is(err, ErrInvalidClientRegistration) {
		t.Fatalf("invalid endpoint error = %v", err)
	}

	registry := &hardRegistry{issuer: issuerregistry.Issuer{Tenant: values.TenantId("tenant-a"), IssuerURL: "https://issuer.example/", Audience: "client", Algorithms: []trustfederation.Algorithm{trustfederation.AlgRS256}}, active: true}
	clients := &hardClientSource{client: ClientRegistration{Tenant: values.TenantId("tenant-a"), IssuerURL: registry.issuer.IssuerURL, ClientID: "client", RedirectURI: "https://app.example/cb"}}
	basePending := PendingAuthorization{Tenant: registry.issuer.Tenant, IssuerURL: registry.issuer.IssuerURL, ClientID: clients.client.ClientID, RedirectURI: clients.client.RedirectURI, State: "state", CodeChallengeDigest: codeChallengeS256(deriveVerifier([]byte("01234567890123456789012345678901"), "state")), ExpiresAt: time.Now().Add(time.Hour)}
	badPKCEPending := basePending
	badPKCEPending.CodeChallengeDigest = "wrong-challenge"
	newHardFlow := func(states StateStore, reg issuerregistry.Store, source ClientSource, exchanger TokenExchanger) *Flow {
		return &Flow{registry: reg, keys: nil, clients: source, states: states, exchanger: exchanger, secret: []byte("01234567890123456789012345678901"), ttl: time.Minute}
	}
	for _, tc := range []struct {
		name string
		flow *Flow
		want error
	}{
		{"state store failure", newHardFlow(&hardStateStore{err: errors.New("state backend down")}, registry, clients, hardExchanger{}), errors.New("state backend down")},
		{"issuer lookup failure", newHardFlow(&hardStateStore{pending: basePending, found: true}, &hardRegistry{err: errors.New("registry down")}, clients, hardExchanger{}), errors.New("registry down")},
		{"client lookup failure", newHardFlow(&hardStateStore{pending: basePending, found: true}, registry, &hardClientSource{err: errors.New("client backend down")}, hardExchanger{}), errors.New("client backend down")},
		{"pkce derivation mismatch", newHardFlow(&hardStateStore{pending: badPKCEPending, found: true}, registry, clients, hardExchanger{}), ErrPKCEDerivationMismatch},
		{"grant rejected", newHardFlow(&hardStateStore{pending: basePending, found: true}, registry, clients, hardExchanger{response: TokenResponse{Error: "invalid_grant"}}), ErrGrantRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ev, err := tc.flow.HandleCallback(context.Background(), CallbackParams{State: "state", Code: "code", Now: time.Now()})
			if tc.want == ErrPKCEDerivationMismatch || tc.want == ErrGrantRejected {
				if !errors.Is(err, tc.want) {
					t.Fatalf("HandleCallback error = %v, want %v", err, tc.want)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want.Error()) {
				t.Fatalf("HandleCallback error = %v, want %v", err, tc.want)
			}
			if ev.Outcome != OutcomeDenied || ev.Reason == "" {
				t.Fatalf("denial evidence = %+v", ev)
			}
		})
	}
}

func TestFlowBeginAuthorization_FailClosedDependencies(t *testing.T) {
	t.Parallel()
	reg := &hardRegistry{issuer: issuerregistry.Issuer{Tenant: values.TenantId("tenant-a"), IssuerURL: "https://issuer.example/", Audience: "client"}, active: true}
	client := &hardClientSource{client: ClientRegistration{Tenant: values.TenantId("tenant-a"), IssuerURL: reg.issuer.IssuerURL, ClientID: "client", RedirectURI: "https://app.example/cb", AuthorizationEndpoint: "https://issuer.example/authorize", TokenEndpoint: "https://issuer.example/token", Scopes: []string{"openid"}}}
	newHardBeginFlow := func(registry issuerregistry.Store, source ClientSource, states StateStore) *Flow {
		return &Flow{registry: registry, clients: source, states: states, exchanger: hardExchanger{}, secret: []byte("01234567890123456789012345678901"), ttl: time.Minute}
	}
	cases := []struct {
		name string
		flow *Flow
		want string
	}{
		{"registry failure", newHardBeginFlow(&hardRegistry{err: errors.New("registry unavailable")}, client, &hardStateStore{}), "registry unavailable"},
		{"client absent", newHardBeginFlow(reg, &hardClientSource{}, &hardStateStore{}), ErrClientNotRegistered.Error()},
		{"client source failure", newHardBeginFlow(reg, &hardClientSource{err: errors.New("client unavailable")}, &hardStateStore{}), "client unavailable"},
		{"client bound to another tenant", newHardBeginFlow(reg, &hardClientSource{client: ClientRegistration{Tenant: values.TenantId("tenant-b"), IssuerURL: reg.issuer.IssuerURL, ClientID: "client"}}, &hardStateStore{}), ErrClientNotRegistered.Error()},
		{"state store failure", newHardBeginFlow(reg, client, &hardStateStore{putErr: errors.New("state unavailable")}), "state unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.flow.BeginAuthorization(context.Background(), values.TenantId("tenant-a"), reg.issuer.IssuerURL, hardAt); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("BeginAuthorization() = %v, want error containing %q", err, tc.want)
			}
		})
	}
	request, err := newHardBeginFlow(reg, client, &hardStateStore{}).BeginAuthorization(context.Background(), reg.issuer.Tenant, reg.issuer.IssuerURL, hardAt)
	if err != nil || request.State == "" || request.Nonce == "" || request.URL == "" || !request.ExpiresAt.Equal(hardAt.Add(time.Minute)) {
		t.Fatalf("valid BeginAuthorization() = %+v,%v", request, err)
	}
}

func TestHTTPTokenExchanger_ErrorBranches(t *testing.T) {
	t.Parallel()
	for name, handler := range map[string]http.HandlerFunc{
		"malformed success": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("not-json"))
		},
		"error status without error field": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"access_token":"unexpected"}`))
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(handler)
			defer srv.Close()
			_, err := (HTTPTokenExchanger{}).Exchange(context.Background(), TokenRequest{TokenEndpoint: srv.URL, Code: "code", RedirectURI: "https://app.example/cb", ClientID: "client", CodeVerifier: "verifier"})
			if err == nil {
				t.Fatalf("Exchange() succeeded, want error")
			}
		})
	}
	if _, err := (HTTPTokenExchanger{}).Exchange(context.Background(), TokenRequest{TokenEndpoint: "://bad"}); err == nil || !strings.Contains(err.Error(), "build token request") {
		t.Fatalf("invalid endpoint Exchange() = %v, want request-build error", err)
	}
	if _, err := (HTTPTokenExchanger{Client: &http.Client{Transport: hardRoundTripper{}}}).Exchange(context.Background(), TokenRequest{TokenEndpoint: "https://issuer.example/token"}); err == nil || !strings.Contains(err.Error(), "token endpoint request") {
		t.Fatalf("transport failure Exchange() = %v, want endpoint-request error", err)
	}
}

type hardStateStore struct {
	pending PendingAuthorization
	found   bool
	err     error
	putErr  error
}

func (s *hardStateStore) Put(context.Context, PendingAuthorization) error { return s.putErr }
func (s *hardStateStore) Take(context.Context, string) (PendingAuthorization, bool, error) {
	if s.err != nil {
		return PendingAuthorization{}, false, s.err
	}
	p, found := s.pending, s.found
	s.found = false
	return p, found, nil
}

type hardClientSource struct {
	client ClientRegistration
	found  bool
	err    error
}

func (s *hardClientSource) LookupClient(values.TenantId, string) (ClientRegistration, bool, error) {
	if s.err != nil {
		return ClientRegistration{}, false, s.err
	}
	if !s.found && s.client.ClientID != "" {
		s.found = true
	}
	return s.client, s.found, nil
}

type hardExchanger struct {
	response TokenResponse
	err      error
}

type hardRoundTripper struct{}

func (hardRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("transport down")
}

func (e hardExchanger) Exchange(context.Context, TokenRequest) (TokenResponse, error) {
	return e.response, e.err
}

type hardRegistry struct {
	issuer issuerregistry.Issuer
	active bool
	err    error
}

func (s *hardRegistry) PutIssuer(issuerregistry.Issuer) error { return nil }
func (s *hardRegistry) GetIssuer(ref issuerregistry.Ref) (issuerregistry.Issuer, bool, error) {
	if s.err != nil {
		return issuerregistry.Issuer{}, false, s.err
	}
	return s.issuer, s.active && ref == s.issuer.Ref(), nil
}
func (s *hardRegistry) LatestRevision(values.TenantId, string) (uint32, bool, error) {
	return s.issuer.Revision, s.active, s.err
}
func (s *hardRegistry) PutEvent(issuerregistry.StateEvent) error { return nil }
func (s *hardRegistry) LatestEvent(values.TenantId, string) (issuerregistry.StateEvent, bool, error) {
	if s.err != nil {
		return issuerregistry.StateEvent{}, false, s.err
	}
	if !s.active {
		return issuerregistry.StateEvent{}, false, nil
	}
	return issuerregistry.StateEvent{Tenant: s.issuer.Tenant, IssuerURL: s.issuer.IssuerURL, Revision: s.issuer.Revision, To: issuerregistry.StatusActive}, true, nil
}
func (s *hardRegistry) ListEvents(values.TenantId, string) ([]issuerregistry.StateEvent, error) {
	return nil, s.err
}

var _ StateStore = (*hardStateStore)(nil)
var _ ClientSource = (*hardClientSource)(nil)
var _ TokenExchanger = hardExchanger{}
var _ issuerregistry.Store = (*hardRegistry)(nil)
