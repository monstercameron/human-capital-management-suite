package issuerregistry

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/bundle"
	trustfederation "github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

func TestResolver_RejectsNilAndSkipsInactiveTenantsUntilOneResolves(t *testing.T) {
	if _, err := NewResolver(nil, nil); !errors.Is(err, ErrInvalidIssuer) {
		t.Fatalf("NewResolver(nil) = %v, want ErrInvalidIssuer", err)
	}
	if _, err := NewResolver(registryFailureStore{}, nil); err == nil {
		t.Fatal("NewResolver accepted a store without a cross-tenant index")
	}
	//lint:ignore SA1012 deliberate nil context: this hardening test proves a nil store plus nil context fails closed. owner=identity-platform expires=2027-03-24
	if _, err := NewTenantResolver(nil, tenantForRegistryTest, nil).ResolveIssuerKeys(nil, "issuer"); err == nil {
		t.Fatal("nil tenant resolver store succeeded")
	}

	store := NewMemoryStore()
	first := registryValidIssuer(t)
	if _, err := Publish(store, first); err != nil {
		t.Fatalf("Publish first: %v", err)
	}
	second := first
	second.Tenant = "another-tenant"
	if _, err := Publish(store, second); err != nil {
		t.Fatalf("Publish second: %v", err)
	}
	if _, err := Activate(store, second.Ref(), Evidence{ActedBy: "approver", At: second.PublishedAt.Add(time.Minute)}); err != nil {
		t.Fatalf("Activate second: %v", err)
	}
	resolver, err := NewResolver(store, nil)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	//lint:ignore SA1012 deliberate nil context: the resolver must tolerate a nil context when its store is healthy. owner=identity-platform expires=2027-03-24
	keys, err := resolver.ResolveIssuerKeys(nil, first.IssuerURL)
	if err != nil || len(keys) != 1 || keys[0].ID != "kid" {
		t.Fatalf("ResolveIssuerKeys = %+v, err=%v, want active second tenant key", keys, err)
	}
}

func TestIssuerSigningKeys_RejectsMalformedKindsAndBundles(t *testing.T) {
	issuer := registryValidIssuer(t)
	issuer.JWKS.PinnedKeys[0].PublicKeyDER = []byte("bad")
	if _, err := issuerSigningKeys(issuer, nil); err == nil {
		t.Fatal("malformed pinned key was accepted")
	}
	issuer.JWKS = JWKSSource{Kind: JWKSSourcePinnedBundle, BundleRef: "ref", BundleVersion: 1}
	if _, err := issuerSigningKeys(issuer, nil); err == nil {
		t.Fatal("bundle without source was accepted")
	}
	if _, err := issuerSigningKeys(Issuer{IssuerURL: issuer.IssuerURL, JWKS: JWKSSource{Kind: "unknown"}}, nil); err == nil {
		t.Fatal("unknown JWKS kind was accepted")
	}
	if _, err := bundleSigningKeys(nil); err == nil {
		t.Fatal("nil bundle was accepted")
	}
	unsupported := &bundle.Bundle{Version: 4, Roots: []bundle.PinnedCert{{Cert: &x509.Certificate{PublicKey: "unsupported"}}}}
	if _, err := bundleSigningKeys(unsupported); err == nil {
		t.Fatal("bundle with no supported keys was accepted")
	}
}

func TestAlgorithmForPublicKey_MapsOnlySupportedKeyTypes(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	p384Key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey(P384): %v", err)
	}
	edPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	tests := []struct {
		name string
		pub  any
		alg  trustfederation.Algorithm
		ok   bool
	}{
		{name: "rsa", pub: &rsaKey.PublicKey, alg: trustfederation.AlgRS256, ok: true},
		{name: "p256", pub: &ecKey.PublicKey, alg: trustfederation.AlgES256, ok: true},
		{name: "p384 rejected", pub: &p384Key.PublicKey, ok: false},
		{name: "ed25519", pub: edPub, alg: trustfederation.AlgEdDSA, ok: true},
		{name: "other", pub: struct{}{}, ok: false},
		{name: "nil", pub: nil, ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alg, ok := algorithmForPublicKey(tt.pub)
			if ok != tt.ok || alg != tt.alg {
				t.Fatalf("algorithmForPublicKey(%T) = %q, %v; want %q, %v", tt.pub, alg, ok, tt.alg, tt.ok)
			}
		})
	}
}
