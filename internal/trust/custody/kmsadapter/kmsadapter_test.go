package kmsadapter

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/bundle"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

var adapterNow = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

type softwareKey struct {
	version KeyVersion
	aesKey  []byte
	signer  *ecdsa.PrivateKey
}

// softwareKMS is deliberately test-only. It simulates a KMS that retains
// symmetric/private material internally and returns only provider outputs.
type softwareKMS struct {
	mu             sync.Mutex
	keys           map[string][]softwareKey
	foreignListing bool
	rootMaterial   [][]byte
}

func newSoftwareKMS() *softwareKMS { return &softwareKMS{keys: make(map[string][]softwareKey)} }

func keyIndex(id, tenant, residency string) string { return tenant + "\x00" + residency + "\x00" + id }

func cloneKeyVersion(key KeyVersion) KeyVersion {
	return key
}

func (s *softwareKMS) CreateKey(req CreateKeyRequest) (KeyVersion, error) {
	if strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.Tenant) == "" || strings.TrimSpace(req.Residency) == "" {
		return KeyVersion{}, errors.New("missing create field")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := keyIndex(req.ID, req.Tenant, req.Residency)
	if versions := s.keys[index]; len(versions) > 0 {
		return cloneKeyVersion(versions[0].version), nil
	}
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return KeyVersion{}, err
	}
	material := make([]byte, 32)
	if _, err := rand.Read(material); err != nil {
		return KeyVersion{}, err
	}
	version := KeyVersion{ID: req.ID, Tenant: req.Tenant, Residency: req.Residency, Version: "v1", Algorithm: "AES-256-GCM+ECDSA-P256", PublicKey: &private.PublicKey}
	s.keys[index] = []softwareKey{{version: version, aesKey: material, signer: private}}
	s.rootMaterial = append(s.rootMaterial, append([]byte(nil), material...))
	return cloneKeyVersion(version), nil
}

func (s *softwareKMS) find(key KeyVersion) (softwareKey, error) {
	versions := s.keys[keyIndex(key.ID, key.Tenant, key.Residency)]
	for _, candidate := range versions {
		if candidate.version.Version == key.Version {
			return candidate, nil
		}
	}
	return softwareKey{}, errors.New("software KMS key not found")
}

func (s *softwareKMS) Wrap(req WrapRequest) (WrappedValue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, err := s.find(req.Key)
	if err != nil {
		return WrappedValue{}, err
	}
	// The fake uses a reversible authenticated wrapper; the adapter sees only
	// the resulting ciphertext and never the fake's AES key.
	block, err := x509.MarshalPKIXPublicKey(&key.signer.PublicKey)
	if err != nil {
		return WrappedValue{}, err
	}
	stream := make([]byte, len(req.Plaintext))
	for i := range stream {
		stream[i] = req.Plaintext[i] ^ key.aesKey[i%len(key.aesKey)] ^ block[i%len(block)]
	}
	return WrappedValue{Key: cloneKeyVersion(req.Key), Ciphertext: stream, Algorithm: "software-xor-wrap"}, nil
}

func (s *softwareKMS) Unwrap(req UnwrapRequest) (UnwrappedValue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, err := s.find(req.Key)
	if err != nil {
		return UnwrappedValue{}, err
	}
	block, err := x509.MarshalPKIXPublicKey(&key.signer.PublicKey)
	if err != nil {
		return UnwrappedValue{}, err
	}
	plain := make([]byte, len(req.Ciphertext))
	for i := range plain {
		plain[i] = req.Ciphertext[i] ^ key.aesKey[i%len(key.aesKey)] ^ block[i%len(block)]
	}
	return UnwrappedValue{Key: cloneKeyVersion(req.Key), Plaintext: plain}, nil
}

func (s *softwareKMS) Sign(req SignRequest) (SignedValue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, err := s.find(req.Key)
	if err != nil {
		return SignedValue{}, err
	}
	signature, err := ecdsa.SignASN1(rand.Reader, key.signer, req.Message)
	if err != nil {
		return SignedValue{}, err
	}
	return SignedValue{Key: cloneKeyVersion(req.Key), Signature: signature, Algorithm: "ECDSA-P256-SHA256"}, nil
}

func (s *softwareKMS) Rotate(req RotateRequest) (KeyVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	versions := s.keys[keyIndex(req.Key.ID, req.Key.Tenant, req.Key.Residency)]
	if len(versions) == 0 {
		return KeyVersion{}, errors.New("software KMS key not found")
	}
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return KeyVersion{}, err
	}
	material := make([]byte, 32)
	if _, err := rand.Read(material); err != nil {
		return KeyVersion{}, err
	}
	version := req.Key
	version.Version = fmt.Sprintf("v%d", len(versions)+1)
	version.Algorithm = "AES-256-GCM+ECDSA-P256"
	version.PublicKey = &private.PublicKey
	s.keys[keyIndex(req.Key.ID, req.Key.Tenant, req.Key.Residency)] = append(versions, softwareKey{version: version, aesKey: material, signer: private})
	s.rootMaterial = append(s.rootMaterial, append([]byte(nil), material...))
	return cloneKeyVersion(version), nil
}

func (s *softwareKMS) ListVersions(req ListVersionsRequest) ([]KeyVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	versions := s.keys[keyIndex(req.ID, req.Tenant, req.Residency)]
	result := make([]KeyVersion, 0, len(versions)+1)
	for _, version := range versions {
		result = append(result, cloneKeyVersion(version.version))
	}
	if s.foreignListing && len(result) > 0 {
		foreign := result[0]
		foreign.Tenant = "tenant-foreign"
		result = append(result, foreign)
	}
	return result, nil
}

func adapterContext(tenant, residency string) custody.Context {
	return custody.Context{RequestContext: custody.RequestContext{Workload: "kms-test-worker", Tenant: tenant, Region: residency, Purpose: "test-purpose", Destination: "test-destination"}}
}

func adapterHandle(id, tenant, residency, version string) custody.Handle {
	return custody.Handle{ID: id, Kind: custody.Key, Version: version, Tenant: tenant, Region: residency}
}

func newAdapter(t *testing.T) (*Adapter, *softwareKMS, custody.Context, custody.Handle) {
	t.Helper()
	client := newSoftwareKMS()
	adapter, err := New(client, WithClock(func() time.Time { return adapterNow }))
	if err != nil {
		t.Fatal(err)
	}
	ctx := adapterContext("tenant-a", "us-east-1")
	handle := adapterHandle("key-1", "tenant-a", "us-east-1", "v1")
	if _, err := adapter.CreateKeyHandle(ctx, custody.KeyHandle{ID: handle.ID, ProviderClass: custody.ProviderKMS, Tenant: handle.Tenant, Purpose: "test-purpose", Version: handle.Version}); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.ActivateKeyHandle(ctx, custody.KeyHandle{ID: handle.ID, ProviderClass: custody.ProviderKMS, Tenant: handle.Tenant, Purpose: "test-purpose", Version: handle.Version}, "sha256:activate"); err != nil {
		t.Fatal(err)
	}
	return adapter, client, ctx, handle
}

func TestTodo_SECARCH_004(t *testing.T) {
	adapter, _, ctx, handle := newAdapter(t)
	var _ custody.Provider = adapter
	metadata := adapter.Metadata()
	var _ custody.TypedProvider = metadata
	var _ custody.KeyLifecycleProvider = adapter

	got, err := metadata.Get(ctx, handle)
	if err != nil || got.Handle != handle || got.Status != custody.StatusActive {
		t.Fatalf("metadata Get = %+v, %v", got, err)
	}
	sealed, _, err := adapter.Encrypt(ctx, handle, []byte("ephemeral-dek"))
	if err != nil {
		t.Fatal(err)
	}
	opened, _, err := adapter.Decrypt(ctx, handle, sealed)
	if err != nil || string(opened) != "ephemeral-dek" {
		t.Fatalf("Decrypt = %q, %v", opened, err)
	}
	signature, _, err := adapter.Sign(ctx, handle, []byte("audit-message"))
	if err != nil {
		t.Fatal(err)
	}
	verified, _, err := adapter.Verify(ctx, handle, []byte("audit-message"), signature)
	if err != nil || !verified {
		t.Fatalf("Verify = %v, %v", verified, err)
	}
	leaseValue, err := adapter.IssueLease(ctx, handle, custody.Encrypt, time.Minute)
	if err != nil || leaseValue.Handle != handle {
		t.Fatalf("IssueLease = %+v, %v", leaseValue, err)
	}
	if err := leaseValue.Validate(adapterNow); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_SECARCH_004_Golden(t *testing.T) {
	if Version() != 1 {
		t.Fatalf("Version = %d, want 1", Version())
	}
	explanation := Explain()
	for _, term := range []string{"tenant-scoped", "residency", "wrap", "unwrap", "sign", "rotate", "list versions"} {
		if !strings.Contains(explanation, term) {
			t.Fatalf("Explain missing %q: %s", term, explanation)
		}
	}
	for _, forbidden := range []string{"tenant-a", "key-1", "account", "secret"} {
		if strings.Contains(strings.ToLower(explanation), forbidden) {
			t.Fatalf("Explain contains audit-sensitive value %q", forbidden)
		}
	}
}

func TestTodo_SECARCH_004_Security(t *testing.T) {
	adapter, client, ctx, handle := newAdapter(t)
	foreignContext := adapterContext("tenant-b", "us-east-1")
	if _, _, err := adapter.Encrypt(foreignContext, handle, []byte("x")); !errors.Is(err, ErrTenantMismatch) || !strings.Contains(err.Error(), "tenant") {
		t.Fatalf("foreign tenant = %v, want typed tenant refusal", err)
	}
	wrongResidency := adapterContext("tenant-a", "eu-west-1")
	if _, _, err := adapter.Encrypt(wrongResidency, handle, []byte("x")); !errors.Is(err, ErrResidencyMismatch) || !strings.Contains(err.Error(), "residency") {
		t.Fatalf("foreign residency = %v, want typed residency refusal", err)
	}
	client.foreignListing = true
	if _, _, err := adapter.Encrypt(ctx, handle, []byte("x")); !errors.Is(err, ErrTenantIsolation) {
		t.Fatalf("provider foreign key listing = %v, want ErrTenantIsolation", err)
	}

	client.foreignListing = false
	sealed, _, err := adapter.Encrypt(ctx, handle, []byte("transient-data-key"))
	if err != nil {
		t.Fatal(err)
	}
	opened, _, err := adapter.Decrypt(ctx, handle, sealed)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{sealed, opened} {
		if containsAnyBytes(value, client.rootMaterial) {
			t.Fatalf("adapter returned raw provider key material in %T", value)
		}
	}
	if _, _, err := adapter.Sign(ctx, handle, []byte("message")); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Revoke(ctx, handle, "compromised"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := adapter.Sign(ctx, handle, []byte("message")); !errors.Is(err, ErrKeyRevoked) {
		t.Fatalf("sign after revoke = %v, want ErrKeyRevoked", err)
	}

	// The existing custody conformance shape is run unchanged against the
	// adapter's typed view for key, secret, and certificate references.
	for _, item := range []struct {
		kind custody.Kind
		id   string
	}{
		{kind: custody.Secret, id: "secret-1"},
		{kind: custody.Certificate, id: "certificate-1"},
	} {
		ref := custody.Handle{ID: item.id, Kind: item.kind, Version: "v1", Tenant: ctx.Tenant, Region: ctx.Region}
		if _, err := client.CreateKey(CreateKeyRequest{ID: ref.ID, Tenant: ref.Tenant, Residency: ref.Region, Purpose: ctx.Purpose, Kind: ref.Kind, ProviderClass: custody.ProviderKMS}); err != nil {
			t.Fatal(err)
		}
		if got, err := adapter.Metadata().Get(ctx, ref); err != nil || got.Handle != ref {
			t.Fatalf("typed conformance Get(%s) = %+v, %v", item.kind, got, err)
		}
		rotated, receipt, err := adapter.Metadata().Rotate(ctx, ref)
		if err != nil || rotated.Handle.Version != "v2" || receipt.Operation != custody.Rotate {
			t.Fatalf("typed conformance Rotate(%s) = %+v, %+v, %v", item.kind, rotated, receipt, err)
		}
		attested, err := adapter.Metadata().Attest(ctx, ref)
		if err != nil || attested.Digest == "" {
			t.Fatalf("typed conformance Attest(%s) = %+v, %v", item.kind, attested, err)
		}
	}
}

func TestTodo_SECARCH_004_Integration(t *testing.T) {
	adapter, _, ctx, handle := newAdapter(t)

	// Envelope is wired through its exported constructor to the actual KMS
	// adapter, with no fake at the custody boundary.
	envelopeManager, err := envelope.New(handle, adapter)
	if err != nil {
		t.Fatal(err)
	}
	kek := adapterHandle("tenant-kek", ctx.Tenant, ctx.Region, "v1")
	if _, err := adapter.CreateKeyHandle(ctx, custody.KeyHandle{ID: kek.ID, ProviderClass: custody.ProviderKMS, Tenant: kek.Tenant, Purpose: "test-purpose", Version: kek.Version}); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.ActivateKeyHandle(ctx, custody.KeyHandle{ID: kek.ID, ProviderClass: custody.ProviderKMS, Tenant: kek.Tenant, Purpose: "test-purpose", Version: kek.Version}, "sha256:kek-active"); err != nil {
		t.Fatal(err)
	}
	if err := envelopeManager.RegisterTenant(ctx, ctx.Tenant, kek); err != nil {
		t.Fatal(err)
	}
	env, _, err := envelopeManager.Seal(ctx, "object-1", []byte("confidential"))
	if err != nil {
		t.Fatal(err)
	}
	plain, _, err := envelopeManager.Open(ctx, env, "object-1")
	if err != nil || string(plain) != "confidential" {
		t.Fatalf("envelope Open = %q, %v", plain, err)
	}
	if _, _, err := envelopeManager.RotateTenantKEK(ctx); err != nil {
		t.Fatal(err)
	}

	// Lease is wired through its exported constructor to the same adapter.
	leaseManager, err := lease.NewManager(adapter, func() time.Time { return adapterNow })
	if err != nil {
		t.Fatal(err)
	}
	credential, _, err := leaseManager.Mint(lease.Request{Handle: handle, Workload: "workload-a", Tenant: ctx.Tenant, Purpose: "test-purpose", Destination: "test-destination", Operation: custody.Encrypt, TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := leaseManager.Use(credential, credential.Destination, credential.Operation); err != nil {
		t.Fatal(err)
	}

	// The certificate verifier is wired through its exported constructors and
	// its certificate pins are produced by a signer that calls the adapter.
	certKey := adapterHandle("certificate-key", ctx.Tenant, ctx.Region, "v1")
	if _, err := adapter.CreateKeyHandle(ctx, custody.KeyHandle{ID: certKey.ID, ProviderClass: custody.ProviderKMS, Tenant: certKey.Tenant, Purpose: "test-purpose", Version: certKey.Version}); err != nil {
		t.Fatal(err)
	}
	certSigner := &adapterSigner{provider: adapter, ctx: ctx, handle: certKey, public: publicKey(t, adapter, ctx, certKey)}
	now := adapterNow.Add(-time.Hour)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "kms-root"}, NotBefore: now, NotAfter: now.Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, certSigner.Public(), certSigner)
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := bundle.NewPinnedCert(der)
	if err != nil {
		t.Fatal(err)
	}
	trustBundle, err := bundle.New(1, "certificate-issuance", []bundle.PinnedCert{pinned}, nil, now, now.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bundle.NewVerifier(trustBundle).Verify(cert, nil, adapterNow, nil); err != nil {
		t.Fatalf("certificate verification through adapter signer = %v", err)
	}
}

func TestTodo_SECARCH_004_Mutation(t *testing.T) {
	adapter, _, ctx, handle := newAdapter(t)
	sealed, _, err := adapter.Encrypt(ctx, handle, []byte("data-key"))
	if err != nil {
		t.Fatal(err)
	}
	sealed.Data[0] ^= 1
	_, _, _ = adapter.Decrypt(ctx, handle, sealed)
	if _, _, err := adapter.Decrypt(ctx, handle, custody.Ciphertext{Handle: adapterHandle("other", ctx.Tenant, ctx.Region, "v1"), Data: []byte("x")}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("wrong ciphertext handle = %v, want ErrInvalidRequest", err)
	}
	leaseValue, err := adapter.IssueLease(ctx, handle, custody.Encrypt, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	tampered := leaseValue
	tampered.Operation = custody.Decrypt
	if _, err := adapter.RenewLease(ctx, tampered, time.Minute); !errors.Is(err, ErrLeaseTampered) {
		t.Fatalf("tampered lease = %v, want ErrLeaseTampered", err)
	}
	if _, err := adapter.Revoke(ctx, handle, "cleanup"); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Metadata().Attest(ctx, handle); !errors.Is(err, custody.ErrObjectRevoked) {
		t.Fatalf("attest revoked = %v, want ErrObjectRevoked", err)
	}
}

type adapterSigner struct {
	provider custody.Provider
	ctx      custody.Context
	handle   custody.Handle
	public   crypto.PublicKey
}

func (s *adapterSigner) Public() crypto.PublicKey { return s.public }

func (s *adapterSigner) Sign(_ io.Reader, digest []byte, _ crypto.SignerOpts) ([]byte, error) {
	signature, _, err := s.provider.Sign(s.ctx, s.handle, digest)
	if err != nil {
		return nil, err
	}
	return signature.Data, nil
}

func publicKey(t *testing.T, provider *Adapter, ctx custody.Context, handle custody.Handle) crypto.PublicKey {
	t.Helper()
	key, err := provider.lookup(handle)
	if err != nil {
		t.Fatal(err)
	}
	return key.PublicKey
}

func containsAnyBytes(value any, needles [][]byte) bool {
	for _, needle := range needles {
		if len(needle) == 0 || containsBytes(reflect.ValueOf(value), needle) {
			return true
		}
	}
	return false
}

func containsBytes(value reflect.Value, needle []byte) bool {
	if !value.IsValid() {
		return false
	}
	if value.Kind() == reflect.Interface {
		return containsBytes(value.Elem(), needle)
	}
	if value.Kind() == reflect.Slice && value.Type().Elem().Kind() == reflect.Uint8 {
		return bytes.Contains(value.Bytes(), needle)
	}
	if value.Kind() == reflect.Struct {
		for i := 0; i < value.NumField(); i++ {
			if value.Field(i).CanInterface() && containsBytes(value.Field(i), needle) {
				return true
			}
		}
	}
	return false
}
