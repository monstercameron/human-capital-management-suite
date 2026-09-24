package envelope

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

func TestConstructorsRootAndTenantRegistrationValidation(t *testing.T) {
	root := custody.Handle{ID: "root", Kind: custody.Key, Version: "v1", Tenant: "root", Region: "us-east"}
	if _, err := New(root, nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil provider = %v", err)
	}
	if _, err := New(custody.Handle{}, &provider{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid root = %v", err)
	}
	m, ctxA, _, _ := envelopeFixture(t)
	if got := m.Root(); got != root {
		t.Fatalf("Root() = %+v, want %+v", got, root)
	}
	var nilManager *Manager
	if nilManager.Root() != (custody.Handle{}) || !errors.Is(nilManager.RegisterTenant(ctxA, "tenant-a", custody.Handle{}), ErrInvalidRequest) {
		t.Fatal("nil manager did not fail closed")
	}
	key := custody.Handle{ID: "key", Kind: custody.Key, Version: "v1", Tenant: "tenant-a", Region: "us-east"}
	badCtx := ctxA
	badCtx.Purpose = ""
	if err := m.RegisterTenant(badCtx, "tenant-a", key); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid context registration = %v", err)
	}
	if err := m.RegisterTenant(ctxA, "other", key); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("tenant mismatch registration = %v", err)
	}
	wrongKind := key
	wrongKind.Kind = custody.Secret
	if err := m.RegisterTenant(ctxA, "tenant-a", wrongKind); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("wrong kind registration = %v", err)
	}
	wrongRegion := key
	wrongRegion.Region = "other"
	if err := m.RegisterTenant(ctxA, "tenant-a", wrongRegion); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("wrong region registration = %v", err)
	}
	if err := m.RegisterTenantKEK(ctxA, "tenant-a", key); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("duplicate registration = %v", err)
	}
	if _, err := NewManager(root, &provider{}); err != nil {
		t.Fatalf("NewManager valid = %v", err)
	}
}

func TestEncryptDecryptOpenAndMalformedEnvelopeBranches(t *testing.T) {
	m, ctxA, ctxB, _ := envelopeFixture(t)
	var nilManager *Manager
	if _, _, err := nilManager.Encrypt(ctxA, "id", []byte("x")); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil Encrypt = %v", err)
	}
	badCtx := ctxA
	badCtx.Destination = ""
	if _, _, err := m.Encrypt(badCtx, "id", []byte("x")); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid Encrypt context = %v", err)
	}
	if _, _, err := m.Encrypt(ctxA, " ", []byte("x")); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty object id = %v", err)
	}
	missing := ctxA
	missing.Tenant = "missing"
	if _, _, err := m.Encrypt(missing, "id", []byte("x")); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("missing key = %v", err)
	}
	env, _, err := m.Seal(ctxA, "id", []byte("plaintext"))
	if err != nil {
		t.Fatal(err)
	}
	opened, _, err := m.Open(ctxA, env, "id")
	if err != nil || string(opened) != "plaintext" {
		t.Fatalf("Open = %q, %v", opened, err)
	}
	if _, _, err := m.Decrypt(ctxB, env, "id"); !errors.Is(err, ErrTenantMismatch) {
		t.Fatalf("cross-tenant Decrypt = %v", err)
	}
	malformed := []struct {
		name string
		mut  func(*Envelope)
	}{
		{"version", func(e *Envelope) { e.Header.Version = 99 }}, {"tenant blank", func(e *Envelope) { e.Header.Tenant = " " }}, {"kek id blank", func(e *Envelope) { e.Header.KEKID = "" }}, {"kek version blank", func(e *Envelope) { e.Header.KEKVersion = "" }}, {"dek id blank", func(e *Envelope) { e.Header.DEKID = "" }}, {"algorithm", func(e *Envelope) { e.Header.Algorithm = "other" }}, {"nonce", func(e *Envelope) { e.Header.Nonce = nil }}, {"data", func(e *Envelope) { e.Data = nil }}, {"wrapped handle", func(e *Envelope) { e.WrappedDEK.Handle = custody.Handle{} }}, {"wrapped data", func(e *Envelope) { e.WrappedDEK.Data = nil }},
	}
	for _, tc := range malformed {
		t.Run(tc.name, func(t *testing.T) {
			copyEnv := env
			tc.mut(&copyEnv)
			if _, _, err := m.Decrypt(ctxA, copyEnv, "id"); !errors.Is(err, ErrInvalidCiphertext) {
				t.Fatalf("malformed Decrypt = %v", err)
			}
		})
	}
	wrongWrapper := env
	wrongWrapper.WrappedDEK.Handle.ID = "different"
	if _, _, err := m.Decrypt(ctxA, wrongWrapper, "id"); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("wrapper/header mismatch = %v", err)
	}
	tampered := env
	tampered.Header.Tenant = "tenant-b"
	if _, _, err := m.Decrypt(ctxA, tampered, "id"); !errors.Is(err, ErrTenantMismatch) {
		t.Fatalf("tenant tamper = %v", err)
	}
	tampered = env
	tampered.Data[0] ^= 1
	if _, _, err := m.Decrypt(ctxA, tampered, "id"); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("data tamper = %v", err)
	}
}

type failingProvider struct {
	*provider
	rotateError, badNext, badDEK bool
}

func (p *failingProvider) Decrypt(ctx custody.Context, object custody.Handle, sealed custody.Ciphertext) ([]byte, custody.Receipt, error) {
	if p.badDEK {
		return []byte("short"), custody.Receipt{ID: "bad"}, nil
	}
	return p.provider.Decrypt(ctx, object, sealed)
}
func (p *failingProvider) Rotate(ctx custody.Context, object custody.Handle) (custody.Handle, custody.Receipt, error) {
	if p.rotateError {
		return custody.Handle{}, custody.Receipt{}, errors.New("rotate failed")
	}
	if p.badNext {
		return object, custody.Receipt{ID: "bad"}, nil
	}
	return p.provider.Rotate(ctx, object)
}

func TestRotationRewrapAndProviderFailureBranches(t *testing.T) {
	m, ctxA, ctxB, p := envelopeFixture(t)
	env, _, err := m.Encrypt(ctxA, "object", []byte("data"))
	if err != nil {
		t.Fatal(err)
	}
	key, ev, err := m.RotateTenantKEK(ctxA)
	if err != nil || key.Version != "v2" || ev.Operation != custody.Rotate {
		t.Fatalf("RotateTenantKEK = %+v, %+v, %v", key, ev, err)
	}
	rewrapped, _, err := m.Rewrap(ctxA, env)
	if err != nil || rewrapped.Header.KEKVersion != "v2" || string(rewrapped.Data) != string(env.Data) {
		t.Fatalf("Rewrap = %+v, %v", rewrapped.Header, err)
	}
	if got, _, err := m.Open(ctxA, rewrapped, "object"); err != nil || string(got) != "data" {
		t.Fatalf("opened rewrapped = %q, %v", got, err)
	}
	if _, _, err := m.Rewrap(ctxB, env); !errors.Is(err, ErrTenantMismatch) {
		t.Fatalf("cross-tenant Rewrap = %v", err)
	}
	unknown := env
	unknown.Header.KEKID = "unknown"
	if _, _, err := m.Rewrap(ctxA, unknown); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("unknown Rewrap = %v", err)
	}
	for _, tc := range []struct {
		name string
		set  func(*failingProvider)
	}{
		{"rotate provider error", func(p *failingProvider) { p.rotateError = true }}, {"invalid next key", func(p *failingProvider) { p.badNext = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fp := &failingProvider{provider: p}
			tc.set(fp)
			root := custody.Handle{ID: "root", Kind: custody.Key, Version: "v1", Tenant: "root", Region: "us-east"}
			manager, err := New(root, fp)
			if err != nil {
				t.Fatal(err)
			}
			kek := custody.Handle{ID: "tenant-c", Kind: custody.Key, Version: "v1", Tenant: "tenant-a", Region: "us-east"}
			if err := manager.RegisterTenant(ctxA, "tenant-a", kek); err != nil {
				t.Fatal(err)
			}
			if _, _, err := manager.RotateKEK(ctxA); !errors.Is(err, ErrKeyRotation) {
				t.Fatalf("RotateKEK = %v", err)
			}
		})
	}
	badDEK := &failingProvider{provider: p, badDEK: true}
	root := custody.Handle{ID: "root", Kind: custody.Key, Version: "v1", Tenant: "root", Region: "us-east"}
	manager, err := New(root, badDEK)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterTenant(ctxA, "tenant-a", custody.Handle{ID: "tenant-a-kek", Kind: custody.Key, Version: "v1", Tenant: "tenant-a", Region: "us-east"}); err != nil {
		t.Fatal(err)
	}
	badEnv, _, err := manager.Encrypt(ctxA, "object", []byte("data"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Decrypt(ctxA, badEnv, "id"); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("invalid DEK = %v", err)
	}
}
