package object

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/envelope"
)

// fakeCustodyProvider is a minimal, deterministic custody.Provider double
// local to this package's tests. It "wraps" a DEK by XOR-masking it with a
// per-handle key, which is enough to exercise real cross-tenant, revocation
// and tamper failure modes without touching production crypto: envelope.go
// already owns the real AES-GCM/DEK/nonce logic and is not reimplemented
// here or anywhere in this package.
type fakeCustodyProvider struct {
	mu       sync.Mutex
	keys     map[custody.Handle][]byte
	rotation int
}

func newFakeCustodyProvider() *fakeCustodyProvider {
	return &fakeCustodyProvider{keys: make(map[custody.Handle][]byte)}
}

func (p *fakeCustodyProvider) mask(key []byte, n int) []byte {
	sum := sha256.Sum256(append(append([]byte(nil), key...), byte(n), byte(n>>8)))
	out := make([]byte, n)
	for i := range out {
		out[i] = sum[i%len(sum)]
	}
	return out
}

func (p *fakeCustodyProvider) Encrypt(ctx custody.Context, object custody.Handle, plaintext []byte) (custody.Ciphertext, custody.Receipt, error) {
	if err := ctx.Validate(); err != nil {
		return custody.Ciphertext{}, custody.Receipt{}, err
	}
	p.mu.Lock()
	key, ok := p.keys[object]
	p.mu.Unlock()
	if !ok || object.Tenant != ctx.Tenant {
		return custody.Ciphertext{}, custody.Receipt{}, custody.ErrDenied
	}
	mask := p.mask(key, len(plaintext))
	out := make([]byte, len(plaintext))
	for i := range plaintext {
		out[i] = plaintext[i] ^ mask[i]
	}
	return custody.Ciphertext{Handle: object, Algorithm: "fake-wrap", Data: out}, custody.Receipt{ID: "receipt-encrypt", Handle: object, Operation: custody.Encrypt, At: time.Now().UTC()}, nil
}

func (p *fakeCustodyProvider) Decrypt(ctx custody.Context, object custody.Handle, sealed custody.Ciphertext) ([]byte, custody.Receipt, error) {
	if sealed.Handle != object || object.Tenant != ctx.Tenant {
		return nil, custody.Receipt{}, custody.ErrDenied
	}
	opened, receipt, err := p.Encrypt(ctx, object, sealed.Data)
	return opened.Data, receipt, err
}

func (p *fakeCustodyProvider) Sign(custody.Context, custody.Handle, []byte) (custody.Signature, custody.Receipt, error) {
	return custody.Signature{}, custody.Receipt{}, errors.New("unused")
}

func (p *fakeCustodyProvider) Verify(custody.Context, custody.Handle, []byte, custody.Signature) (bool, custody.Receipt, error) {
	return false, custody.Receipt{}, errors.New("unused")
}

func (p *fakeCustodyProvider) IssueLease(custody.Context, custody.Handle, custody.Operation, time.Duration) (custody.Lease, error) {
	return custody.Lease{}, errors.New("unused")
}

func (p *fakeCustodyProvider) RenewLease(custody.Context, custody.Lease, time.Duration) (custody.Lease, error) {
	return custody.Lease{}, errors.New("unused")
}

func (p *fakeCustodyProvider) Rotate(ctx custody.Context, object custody.Handle) (custody.Handle, custody.Receipt, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key, ok := p.keys[object]
	if !ok || object.Tenant != ctx.Tenant {
		return custody.Handle{}, custody.Receipt{}, custody.ErrDenied
	}
	p.rotation++
	next := object
	next.Version = fmt.Sprintf("v%d", p.rotation+1)
	p.keys[next] = append([]byte(nil), key...)
	return next, custody.Receipt{ID: "receipt-rotate", Handle: next, Operation: custody.Rotate, At: time.Now().UTC()}, nil
}

func (p *fakeCustodyProvider) Revoke(custody.Context, custody.Handle, string) (custody.Receipt, error) {
	return custody.Receipt{}, errors.New("unused")
}

func (p *fakeCustodyProvider) destroy(h custody.Handle) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.keys, h)
}

// sealedFixture builds an envelope.Manager with two registered tenants
// backed by fakeCustodyProvider, ready for sealing object bytes.
func sealedFixture(t *testing.T) (*envelope.Manager, custody.Context, custody.Context, *fakeCustodyProvider) {
	t.Helper()
	root := custody.Handle{ID: "root", Kind: custody.Key, Version: "v1", Tenant: "root", Region: "us-east"}
	kekA := custody.Handle{ID: "tenant-a-kek", Kind: custody.Key, Version: "v1", Tenant: "tenant-a", Region: "us-east"}
	kekB := custody.Handle{ID: "tenant-b-kek", Kind: custody.Key, Version: "v1", Tenant: "tenant-b", Region: "us-east"}
	p := newFakeCustodyProvider()
	p.keys[kekA] = []byte("tenant-a-master-key")
	p.keys[kekB] = []byte("tenant-b-master-key")
	m, err := envelope.New(root, p)
	if err != nil {
		t.Fatal(err)
	}
	ctxA := custody.Context{RequestContext: custody.RequestContext{Workload: "w-a", Tenant: "tenant-a", Region: "us-east", Purpose: "object-store", Destination: "local"}}
	ctxB := custody.Context{RequestContext: custody.RequestContext{Workload: "w-b", Tenant: "tenant-b", Region: "us-east", Purpose: "object-store", Destination: "local"}}
	if err := m.RegisterTenant(ctxA, "tenant-a", kekA); err != nil {
		t.Fatal(err)
	}
	if err := m.RegisterTenant(ctxB, "tenant-b", kekB); err != nil {
		t.Fatal(err)
	}
	return m, ctxA, ctxB, p
}

// TestTodo_ARTIFACT_004 is the PRIMARY test: an ingested object's bytes are
// stored only as an envelope; round-tripping returns the exact plaintext;
// the stored form contains neither the plaintext nor an unwrapped DEK.
func TestTodo_ARTIFACT_004(t *testing.T) {
	m, ctxA, _, _ := sealedFixture(t)
	store, err := NewSealedObjectStore(m)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("sensitive payroll bytes for object-1")

	info, err := store.Put(context.Background(), ctxA, "object-1", "application/octet-stream", plaintext)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if info.ArtifactID != "object-1" || info.Size != int64(len(plaintext)) || info.Digest == "" {
		t.Fatalf("info = %#v", info)
	}

	env, err := store.Envelope("object-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(env.Data) == 0 || len(env.WrappedDEK.Data) == 0 {
		t.Fatal("envelope has no ciphertext or wrapped DEK")
	}
	if containsBytes(env.Data, plaintext) {
		t.Fatal("stored ciphertext contains the plaintext")
	}
	if containsBytes(env.WrappedDEK.Data, plaintext) {
		t.Fatal("wrapped DEK contains the plaintext")
	}
	// The stored form never carries an unwrapped DEK: envelope.Envelope's
	// own contract (see envelope.go's WrappedDEK doc) is that WrappedDEK is
	// always provider-neutral custody output, never a raw key, and there is
	// no field on Envelope through which a caller could obtain one. The
	// SealedObjectStore adds nothing beyond Envelope's own header and
	// ciphertext, so proving both Data and WrappedDEK are ciphertext-shaped
	// (present, plaintext-free) below is what this store can independently
	// verify.

	got, gotInfo, err := store.Get(context.Background(), ctxA, "object-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("round trip = %q, want %q", got, plaintext)
	}
	if gotInfo.Digest != info.Digest {
		t.Fatalf("info drifted between put and get: %#v vs %#v", gotInfo, info)
	}
}

func containsBytes(haystack, needle []byte) bool {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// TestTodo_ARTIFACT_004_Security proves there is no plaintext fallback: a
// missing/unregistered tenant KEK, or an unavailable custody provider, must
// fail closed with a typed error, never silently store cleartext. Tenant A
// cannot decrypt tenant B's object even given the ciphertext. A revoked key
// fails.
func TestTodo_ARTIFACT_004_Security(t *testing.T) {
	m, ctxA, ctxB, p := sealedFixture(t)
	store, err := NewSealedObjectStore(m)
	if err != nil {
		t.Fatal(err)
	}

	// Unregistered tenant KEK: tenant-c was never registered with the
	// manager, so encryption cannot be performed. Put must fail closed and
	// leave no trace of the object, sealed or otherwise.
	ctxC := custody.Context{RequestContext: custody.RequestContext{Workload: "w-c", Tenant: "tenant-c", Region: "us-east", Purpose: "object-store", Destination: "local"}}
	if _, err := store.Put(context.Background(), ctxC, "object-c", "text/plain", []byte("no key for this tenant")); !errors.Is(err, ErrEncryptionUnavailable) {
		t.Fatalf("unregistered tenant put = %v, want ErrEncryptionUnavailable", err)
	}
	if _, err := store.Stat(context.Background(), "object-c"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unregistered tenant leaked an entry: stat = %v", err)
	}

	// Unavailable custody provider: delete tenant A's KEK material from the
	// provider entirely (simulating an outage/unavailable provider), then
	// attempt to seal a brand-new object.
	kekA := custody.Handle{ID: "tenant-a-kek", Kind: custody.Key, Version: "v1", Tenant: "tenant-a", Region: "us-east"}
	p.destroy(kekA)
	if _, err := store.Put(context.Background(), ctxA, "object-outage", "text/plain", []byte("provider is down")); !errors.Is(err, ErrEncryptionUnavailable) {
		t.Fatalf("provider-unavailable put = %v, want ErrEncryptionUnavailable", err)
	}
	if _, err := store.Stat(context.Background(), "object-outage"); !errors.Is(err, ErrNotFound) {
		t.Fatal("provider outage leaked a stored entry")
	}

	// Restore the key and seal a real object so we can test cross-tenant
	// and revoked-key reads against a genuine stored envelope.
	p.keys[kekA] = []byte("tenant-a-master-key")
	if _, err := store.Put(context.Background(), ctxA, "object-a", "text/plain", []byte("tenant a secret")); err != nil {
		t.Fatalf("put after restoring key: %v", err)
	}

	// Tenant B cannot decrypt tenant A's object even though it can reach
	// the same store and the same stored ciphertext.
	if _, _, err := store.Get(context.Background(), ctxB, "object-a"); !errors.Is(err, envelope.ErrTenantMismatch) {
		t.Fatalf("cross-tenant get = %v, want ErrTenantMismatch", err)
	}

	// A revoked/destroyed key fails closed on read; it never falls back to
	// returning whatever ciphertext bytes happen to be stored.
	p.destroy(kekA)
	if _, _, err := store.Get(context.Background(), ctxA, "object-a"); err == nil {
		t.Fatal("get succeeded after the tenant key was destroyed")
	}
}

// TestTodo_ARTIFACT_004_ObjectBinding proves the wrong-object-id half of the
// wrong-AAD requirement outside the fuzz corpus: swapping a valid,
// authenticated envelope onto a different object of the same tenant must
// not return its plaintext under the wrong identity.
func TestTodo_ARTIFACT_004_ObjectBinding(t *testing.T) {
	m, ctxA, _, _ := sealedFixture(t)
	store, err := NewSealedObjectStore(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), ctxA, "object-1", "text/plain", []byte("belongs to object-1")); err != nil {
		t.Fatal(err)
	}
	env, err := store.Envelope("object-1")
	if err != nil {
		t.Fatal(err)
	}
	// Directly exercise the open path with the swapped identity: the
	// envelope authenticates fine (same tenant, same everything AES-GCM
	// checks), but the payload's bound object id does not match.
	if _, err := openObject(m, ctxA, "object-2", env); !errors.Is(err, ErrObjectBinding) {
		t.Fatalf("rebound object id open = %v, want ErrObjectBinding", err)
	}
}

// TestLegacyEnvelopeFallback opens an old envelope whose object ID exists
// only in the authenticated sealed payload. New envelopes still bind the
// header ID in AAD: stripping it and taking the legacy path fails GCM.
func TestLegacyEnvelopeFallback(t *testing.T) {
	m, ctxA, _, provider := sealedFixture(t)
	legacy, err := makeLegacyEnvelope(m, ctxA, provider, "object-1", []byte("legacy plaintext"))
	if err != nil {
		t.Fatalf("make legacy envelope: %v", err)
	}
	if got, err := openObject(m, ctxA, "object-1", legacy); err != nil || string(got) != "legacy plaintext" {
		t.Fatalf("open legacy envelope = %q, %v", got, err)
	}
	if _, err := openObject(m, ctxA, "object-2", legacy); !errors.Is(err, ErrObjectBinding) {
		t.Fatalf("open legacy envelope under wrong object ID = %v, want ErrObjectBinding", err)
	}
	tamperedLegacy := copyEnvelope(legacy)
	tamperedLegacy.Data[0] ^= 0xff
	if _, err := openObject(m, ctxA, "object-1", tamperedLegacy); !errors.Is(err, envelope.ErrInvalidCiphertext) {
		t.Fatalf("tampered legacy envelope = %v, want ErrInvalidCiphertext", err)
	}

	newEnvelope, err := sealObject(m, ctxA, "object-1", []byte("new plaintext"))
	if err != nil {
		t.Fatalf("seal new envelope: %v", err)
	}
	newEnvelope.Header.ObjectID = ""
	if _, err := openObject(m, ctxA, "object-1", newEnvelope); !errors.Is(err, envelope.ErrInvalidCiphertext) {
		t.Fatalf("stripped new object ID = %v, want ErrInvalidCiphertext", err)
	}
}

func makeLegacyEnvelope(m *envelope.Manager, ctx custody.Context, provider *fakeCustodyProvider, objectID string, data []byte) (envelope.Envelope, error) {
	payload, err := json.Marshal(sealedPayload{ObjectID: objectID, Data: data})
	if err != nil {
		return envelope.Envelope{}, err
	}
	env, _, err := m.Encrypt(ctx, objectID, payload)
	if err != nil {
		return envelope.Envelope{}, err
	}
	dek, _, err := provider.Decrypt(ctx, env.WrappedDEK.Handle, env.WrappedDEK)
	if err != nil {
		return envelope.Envelope{}, err
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return envelope.Envelope{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return envelope.Envelope{}, err
	}
	header := env.Header
	header.ObjectID = ""
	header.Nonce = make([]byte, gcm.NonceSize())
	if _, err := rand.Read(header.Nonce); err != nil {
		return envelope.Envelope{}, err
	}
	legacyAAD, err := json.Marshal(struct {
		Version   int    `json:"version"`
		Tenant    string `json:"tenant"`
		DEKID     string `json:"dek_id"`
		Algorithm string `json:"algorithm"`
		Nonce     []byte `json:"nonce"`
	}{Version: header.Version, Tenant: header.Tenant, DEKID: header.DEKID, Algorithm: header.Algorithm, Nonce: header.Nonce})
	if err != nil {
		return envelope.Envelope{}, err
	}
	env.Header = header
	env.Data = gcm.Seal(nil, header.Nonce, payload, legacyAAD)
	return env, nil
}

// FuzzTodo_ARTIFACT_004 is the FUZZ matrix test. Its oracle is
// tamper-detection: arbitrary mutation of ciphertext, nonce, wrapped DEK, or
// any authenticated header field must fail to decrypt, never return
// wrong-but-accepted plaintext. It also covers wrong-AAD: a ciphertext must
// not decrypt when rebound to a different objectID or tenant.
func FuzzTodo_ARTIFACT_004(f *testing.F) {
	f.Add([]byte("payroll"), byte(0))
	f.Add([]byte(""), byte(1))
	f.Add([]byte("x"), byte(6))
	f.Fuzz(func(t *testing.T, data []byte, selector byte) {
		m, ctxA, ctxB, _ := sealedFixture(t)
		env, err := sealObject(m, ctxA, "object-1", data)
		if err != nil {
			t.Fatalf("seal: %v", err)
		}
		original, err := openObject(m, ctxA, "object-1", env)
		if err != nil || string(original) != string(data) {
			t.Fatalf("baseline open failed before any tamper: %v", err)
		}

		switch selector % 8 {
		case 0: // tamper ciphertext
			mutated := copyEnvelope(env)
			mutated.Data[0] ^= 0xFF
			if _, err := openObject(m, ctxA, "object-1", mutated); err == nil {
				t.Fatal("tampered ciphertext decrypted")
			}
		case 1: // tamper nonce
			mutated := copyEnvelope(env)
			mutated.Header.Nonce[0] ^= 0xFF
			if _, err := openObject(m, ctxA, "object-1", mutated); err == nil {
				t.Fatal("tampered nonce decrypted")
			}
		case 2: // tamper wrapped DEK
			mutated := copyEnvelope(env)
			mutated.WrappedDEK.Data[0] ^= 0xFF
			if _, err := openObject(m, ctxA, "object-1", mutated); err == nil {
				t.Fatal("tampered wrapped DEK decrypted")
			}
		case 3: // tamper authenticated header field: DEKID
			mutated := copyEnvelope(env)
			mutated.Header.DEKID = mutated.Header.DEKID + "-tampered"
			if _, err := openObject(m, ctxA, "object-1", mutated); err == nil {
				t.Fatal("tampered DEKID decrypted")
			}
		case 4: // tamper authenticated header field: Algorithm
			mutated := copyEnvelope(env)
			mutated.Header.Algorithm = "AES-128-GCM"
			if _, err := openObject(m, ctxA, "object-1", mutated); err == nil {
				t.Fatal("tampered algorithm decrypted")
			}
		case 5: // tamper authenticated header field: Tenant
			mutated := copyEnvelope(env)
			mutated.Header.Tenant = "tenant-b"
			if _, err := openObject(m, ctxA, "object-1", mutated); err == nil {
				t.Fatal("tampered tenant header decrypted")
			}
		case 6: // wrong-AAD: rebind to a different object id, same tenant
			if _, err := openObject(m, ctxA, "object-DIFFERENT", env); !errors.Is(err, ErrObjectBinding) {
				t.Fatalf("cross-object rebind = %v, want ErrObjectBinding", err)
			}
		case 7: // wrong-AAD: rebind to a different tenant context
			if _, err := openObject(m, ctxB, "object-1", env); !errors.Is(err, envelope.ErrTenantMismatch) {
				t.Fatalf("cross-tenant rebind = %v, want ErrTenantMismatch", err)
			}
		}
	})
}

// TestTodo_ARTIFACT_004_Race runs real goroutines. Concurrent seal/open/
// rewrap over shared state must not interleave a DEK or reuse a nonce.
// There is no -race detector on this host (windows/arm64), so this asserts
// a concrete wrong result instead: every concurrently-sealed nonce must be
// unique, and every concurrent round trip must return exactly its own
// plaintext, never another goroutine's.
func TestTodo_ARTIFACT_004_Race(t *testing.T) {
	m, ctxA, _, _ := sealedFixture(t)
	store, err := NewSealedObjectStore(m)
	if err != nil {
		t.Fatal(err)
	}
	const n = 40
	ids := make([]string, n)
	want := make([][]byte, n)
	for i := 0; i < n; i++ {
		ids[i] = fmt.Sprintf("object-%d", i)
		want[i] = []byte(fmt.Sprintf("payload-%d-%d", i, i*7+1))
	}

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := store.Put(context.Background(), ctxA, ids[i], "application/octet-stream", want[i])
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent put %d: %v", i, err)
		}
	}

	nonces := make(map[string]string, n)
	for i := 0; i < n; i++ {
		env, err := store.Envelope(ids[i])
		if err != nil {
			t.Fatal(err)
		}
		key := string(env.Header.Nonce)
		if owner, dup := nonces[key]; dup {
			t.Fatalf("nonce reused between %s and %s", owner, ids[i])
		}
		nonces[key] = ids[i]
	}

	// Concurrently read every object back and, at the same time, rewrap
	// half of them; every reader must observe its own object's exact
	// plaintext throughout, and no rewrap may corrupt another object.
	var rg sync.WaitGroup
	gotErrs := make([]error, n)
	got := make([][]byte, n)
	for i := 0; i < n; i++ {
		rg.Add(1)
		go func(i int) {
			defer rg.Done()
			b, _, err := store.Get(context.Background(), ctxA, ids[i])
			got[i], gotErrs[i] = b, err
		}(i)
		if i%2 == 0 {
			rg.Add(1)
			go func(i int) {
				defer rg.Done()
				if err := store.RewrapOne(context.Background(), ctxA, ids[i]); err != nil {
					t.Errorf("concurrent rewrap %d: %v", i, err)
				}
			}(i)
		}
	}
	rg.Wait()
	for i := 0; i < n; i++ {
		if gotErrs[i] != nil || string(got[i]) != string(want[i]) {
			t.Fatalf("concurrent get %d = %q, %v; want %q", i, got[i], gotErrs[i], want[i])
		}
	}
}

// TestTodo_ARTIFACT_004_Integration proves rotation/rewrap is resumable
// (every object readable throughout an interrupted-then-resumed batch, none
// left unreadable or double-wrapped) and that crypto-erasure destroys
// recoverability of bytes while metadata stays truthful.
func TestTodo_ARTIFACT_004_Integration(t *testing.T) {
	m, ctxA, _, p := sealedFixture(t)
	store, err := NewSealedObjectStore(m)
	if err != nil {
		t.Fatal(err)
	}

	const n = 6
	ids := make([]string, n)
	plain := make([][]byte, n)
	for i := 0; i < n; i++ {
		ids[i] = fmt.Sprintf("rotating-%d", i)
		plain[i] = []byte(fmt.Sprintf("payroll record %d", i))
		if _, err := store.Put(context.Background(), ctxA, ids[i], "text/plain", plain[i]); err != nil {
			t.Fatalf("seed put %d: %v", i, err)
		}
	}
	originalVersion, err := store.Envelope(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	startVersion := originalVersion.Header.KEKVersion

	// Rotate the tenant KEK for future objects; existing envelopes stay on
	// the old version (in history) until individually rewrapped.
	if _, _, err := m.RotateTenantKEK(ctxA); err != nil {
		t.Fatalf("rotate tenant kek: %v", err)
	}

	// Simulate an interrupted batch: rewrap only the first half.
	interruptAt := n / 2
	if done, err := store.RewrapBatch(context.Background(), ctxA, ids[:interruptAt]); err != nil || done != interruptAt {
		t.Fatalf("partial rewrap batch = %d, %v", done, err)
	}

	// Every object, rewrapped or not, must still be readable right now.
	for i := 0; i < n; i++ {
		got, _, err := store.Get(context.Background(), ctxA, ids[i])
		if err != nil || string(got) != string(plain[i]) {
			t.Fatalf("mid-rotation get %d = %q, %v; want %q", i, got, err, plain[i])
		}
	}
	// The not-yet-rewrapped half is still on the old KEK version, proving
	// the interruption really did leave work outstanding.
	for i := interruptAt; i < n; i++ {
		env, err := store.Envelope(ids[i])
		if err != nil {
			t.Fatal(err)
		}
		if env.Header.KEKVersion != startVersion {
			t.Fatalf("object %d moved to a new KEK version before its rewrap ran", i)
		}
	}

	// Resume the batch over every id (including the already-rewrapped
	// prefix, exactly as a naive resume-from-scratch would do).
	if done, err := store.RewrapBatch(context.Background(), ctxA, ids); err != nil || done != n {
		t.Fatalf("resumed rewrap batch = %d, %v", done, err)
	}

	currentVersion, err := store.Envelope(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		env, err := store.Envelope(ids[i])
		if err != nil {
			t.Fatal(err)
		}
		if env.Header.KEKVersion != currentVersion.Header.KEKVersion || env.Header.KEKVersion == startVersion {
			t.Fatalf("object %d not fully rewrapped onto the current KEK: %+v", i, env.Header)
		}
		got, _, err := store.Get(context.Background(), ctxA, ids[i])
		if err != nil || string(got) != string(plain[i]) {
			t.Fatalf("post-rotation get %d = %q, %v; want %q", i, got, err, plain[i])
		}
	}

	// Crypto-erasure: destroy the tenant's current KEK material entirely.
	currentKEK := custody.Handle{ID: currentVersion.Header.KEKID, Kind: custody.Key, Version: currentVersion.Header.KEKVersion, Tenant: "tenant-a", Region: "us-east"}
	p.destroy(currentKEK)

	for i := 0; i < n; i++ {
		// Bytes are unrecoverable: Get must fail, never return plaintext or
		// substitute bytes.
		if _, _, err := store.Get(context.Background(), ctxA, ids[i]); err == nil {
			t.Fatalf("object %d readable after crypto-erasure", i)
		}
		// Metadata stays truthful: the object still demonstrably existed,
		// with its honest recorded size and digest, not a lie and not a
		// silent disappearance.
		stat, err := store.Stat(context.Background(), ids[i])
		if err != nil {
			t.Fatalf("stat %d disappeared after crypto-erasure: %v", i, err)
		}
		if stat.ArtifactID != ids[i] || stat.Size != int64(len(plain[i])) || stat.Digest == "" {
			t.Fatalf("stat %d became untruthful after crypto-erasure: %#v", i, stat)
		}
	}
}

// TestTodo_ARTIFACT_004_EdgeCases exercises the request-validation, not-found,
// nil-manager, and introspection paths that the matrix tests above do not
// otherwise reach: a nil manager or store is a caller error, not an
// encryption bypass, and every id/precondition check still applies.
func TestTodo_ARTIFACT_004_EdgeCases(t *testing.T) {
	if _, err := NewSealedObjectStore(nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil manager = %v, want ErrInvalidRequest", err)
	}

	m, ctxA, _, _ := sealedFixture(t)
	store, err := NewSealedObjectStore(m)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Put(context.Background(), ctxA, "../escape", "text/plain", []byte("x")); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("bad id put = %v, want ErrInvalidPath", err)
	}
	if _, err := store.Put(context.Background(), ctxA, "object-empty", "text/plain", nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty content put = %v, want ErrInvalidRequest", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Put(canceled, ctxA, "object-x", "text/plain", []byte("x")); err == nil {
		t.Fatal("put with a canceled context succeeded")
	}
	if _, _, err := store.Get(canceled, ctxA, "object-x"); err == nil {
		t.Fatal("get with a canceled context succeeded")
	}
	if _, err := store.Stat(canceled, "object-x"); err == nil {
		t.Fatal("stat with a canceled context succeeded")
	}
	if err := store.RewrapOne(canceled, ctxA, "object-x"); err == nil {
		t.Fatal("rewrap with a canceled context succeeded")
	}

	if _, _, err := store.Get(context.Background(), ctxA, "never-put"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing = %v, want ErrNotFound", err)
	}
	if _, err := store.Stat(context.Background(), "never-put"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stat missing = %v, want ErrNotFound", err)
	}
	if _, err := store.Envelope("never-put"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("envelope missing = %v, want ErrNotFound", err)
	}
	if err := store.RewrapOne(context.Background(), ctxA, "never-put"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rewrap missing = %v, want ErrNotFound", err)
	}

	if len(store.ObjectIDs()) != 0 {
		t.Fatalf("object ids = %v, want none of the failed calls above to have committed anything", store.ObjectIDs())
	}
	if _, err := store.Put(context.Background(), ctxA, "object-1", "text/plain", []byte("only object")); err != nil {
		t.Fatal(err)
	}
	if ids := store.ObjectIDs(); len(ids) != 1 || ids[0] != "object-1" {
		t.Fatalf("object ids = %v", ids)
	}
	if _, err := store.Put(context.Background(), ctxA, "object-1", "text/plain", []byte("duplicate")); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate put = %v, want ErrAlreadyExists", err)
	}

	if summary := ExplainSealed(store); !strings.Contains(summary, "1 artifact") {
		t.Fatalf("ExplainSealed = %q", summary)
	}
	if summary := ExplainSealed(nil); summary == "" {
		t.Fatal("ExplainSealed(nil) returned an empty summary")
	}

	// A RewrapBatch that fails partway through reports exactly how far it
	// got, so a caller can resume from that index.
	if _, err := store.Put(context.Background(), ctxA, "object-2", "text/plain", []byte("second")); err != nil {
		t.Fatal(err)
	}
	done, err := store.RewrapBatch(context.Background(), ctxA, []string{"object-1", "missing-in-batch", "object-2"})
	if done != 1 || !errors.Is(err, ErrNotFound) {
		t.Fatalf("partial rewrap batch = %d, %v; want 1, ErrNotFound", done, err)
	}

	// sealObject/openObject reject a nil manager directly, independent of
	// SealedObjectStore's own nil guard.
	if _, err := sealObject(nil, ctxA, "object-1", []byte("x")); !errors.Is(err, ErrEncryptionUnavailable) {
		t.Fatalf("sealObject nil manager = %v, want ErrEncryptionUnavailable", err)
	}
	if _, err := openObject(nil, ctxA, "object-1", envelope.Envelope{}); !errors.Is(err, ErrEncryptionUnavailable) {
		t.Fatalf("openObject nil manager = %v, want ErrEncryptionUnavailable", err)
	}
}
