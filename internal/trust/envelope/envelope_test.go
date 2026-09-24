package envelope

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

type provider struct {
	keys map[custody.Handle][]byte
	next int
}

func (p *provider) Encrypt(ctx custody.Context, object custody.Handle, plaintext []byte) (custody.Ciphertext, custody.Receipt, error) {
	if err := ctx.Validate(); err != nil {
		return custody.Ciphertext{}, custody.Receipt{}, err
	}
	key, ok := p.keys[object]
	if !ok || object.Tenant != ctx.Tenant {
		return custody.Ciphertext{}, custody.Receipt{}, custody.ErrDenied
	}
	mask := sha256.Sum256(append(append([]byte(nil), key...), byte(len(plaintext))))
	out := make([]byte, len(plaintext))
	for i := range plaintext {
		out[i] = plaintext[i] ^ mask[i%len(mask)]
	}
	return custody.Ciphertext{Handle: object, Algorithm: "fake-wrap", Data: out}, custody.Receipt{ID: "receipt-encrypt", Handle: object, Operation: custody.Encrypt, At: time.Now().UTC()}, nil
}

func (p *provider) Decrypt(ctx custody.Context, object custody.Handle, sealed custody.Ciphertext) ([]byte, custody.Receipt, error) {
	if sealed.Handle != object || object.Tenant != ctx.Tenant {
		return nil, custody.Receipt{}, custody.ErrDenied
	}
	opened, receipt, err := p.Encrypt(ctx, object, sealed.Data)
	return opened.Data, receipt, err
}
func (p *provider) Sign(custody.Context, custody.Handle, []byte) (custody.Signature, custody.Receipt, error) {
	return custody.Signature{}, custody.Receipt{}, errors.New("unused")
}
func (p *provider) Verify(custody.Context, custody.Handle, []byte, custody.Signature) (bool, custody.Receipt, error) {
	return false, custody.Receipt{}, errors.New("unused")
}
func (p *provider) IssueLease(custody.Context, custody.Handle, custody.Operation, time.Duration) (custody.Lease, error) {
	return custody.Lease{}, errors.New("unused")
}
func (p *provider) RenewLease(custody.Context, custody.Lease, time.Duration) (custody.Lease, error) {
	return custody.Lease{}, errors.New("unused")
}
func (p *provider) Rotate(ctx custody.Context, object custody.Handle) (custody.Handle, custody.Receipt, error) {
	if object.Tenant != ctx.Tenant {
		return custody.Handle{}, custody.Receipt{}, custody.ErrDenied
	}
	p.next++
	next := object
	next.ID = fmt.Sprintf("%s-rotated", object.ID)
	next.Version = fmt.Sprintf("v%d", p.next+1)
	p.keys[next] = append([]byte(nil), p.keys[object]...)
	return next, custody.Receipt{ID: "receipt-rotate", Handle: next, Operation: custody.Rotate, At: time.Now().UTC()}, nil
}
func (p *provider) Revoke(custody.Context, custody.Handle, string) (custody.Receipt, error) {
	return custody.Receipt{}, errors.New("unused")
}

func envelopeFixture(t *testing.T) (*Manager, custody.Context, custody.Context, *provider) {
	t.Helper()
	root := custody.Handle{ID: "root", Kind: custody.Key, Version: "v1", Tenant: "root", Region: "us-east"}
	a := custody.Handle{ID: "tenant-a-kek", Kind: custody.Key, Version: "v1", Tenant: "tenant-a", Region: "us-east"}
	b := custody.Handle{ID: "tenant-b-kek", Kind: custody.Key, Version: "v1", Tenant: "tenant-b", Region: "us-east"}
	p := &provider{keys: map[custody.Handle][]byte{a: []byte("tenant-a-key"), b: []byte("tenant-b-key")}}
	m, err := New(root, p)
	if err != nil {
		t.Fatal(err)
	}
	ctxA := custody.Context{RequestContext: custody.RequestContext{Workload: "w-a", Tenant: "tenant-a", Region: "us-east", Purpose: "test", Destination: "local"}}
	ctxB := custody.Context{RequestContext: custody.RequestContext{Workload: "w-b", Tenant: "tenant-b", Region: "us-east", Purpose: "test", Destination: "local"}}
	if err := m.RegisterTenant(ctxA, "tenant-a", a); err != nil {
		t.Fatal(err)
	}
	if err := m.RegisterTenant(ctxB, "tenant-b", b); err != nil {
		t.Fatal(err)
	}
	return m, ctxA, ctxB, p
}

func TestTodo_TRUST_028(t *testing.T) {
	m, ctxA, ctxB, _ := envelopeFixture(t)
	env, ev, err := m.Encrypt(ctxA, "object-1", []byte("sensitive payroll"))
	if err != nil {
		t.Fatal(err)
	}
	if ev.DEKID == "" || env.Header.Tenant != "tenant-a" || env.Header.KEKVersion != "v1" || env.Header.DEKID == "" {
		t.Fatalf("incomplete envelope header: %+v", env.Header)
	}
	got, _, err := m.Decrypt(ctxA, env, "object-1")
	if err != nil || string(got) != "sensitive payroll" {
		t.Fatalf("decrypt = %q, %v", got, err)
	}
	if _, _, err := m.Decrypt(ctxB, env, "object-1"); !errors.Is(err, ErrTenantMismatch) {
		t.Fatalf("cross-tenant decrypt = %v, want ErrTenantMismatch", err)
	}
	oldData := append([]byte(nil), env.Data...)
	rotated, _, err := m.RotateKEK(ctxA, []Envelope{env})
	if err != nil {
		t.Fatal(err)
	}
	if len(rotated) != 1 || string(rotated[0].Data) != string(oldData) || rotated[0].Header.DEKID != env.Header.DEKID || rotated[0].Header.KEKVersion == env.Header.KEKVersion {
		t.Fatalf("rotation changed data/dek incorrectly: old=%+v new=%+v", env.Header, rotated[0].Header)
	}
	got, _, err = m.Decrypt(ctxA, rotated[0], "object-1")
	if err != nil || string(got) != "sensitive payroll" {
		t.Fatalf("decrypt after rewrap = %q, %v", got, err)
	}
}

func TestTodo_TRUST_028_Security(t *testing.T) {
	m, ctxA, _, _ := envelopeFixture(t)
	env, _, err := m.Encrypt(ctxA, "object-1", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	tampered := env
	tampered.Header.Tenant = "tenant-b"
	if _, _, err := m.Decrypt(ctxA, tampered, "object-1"); !errors.Is(err, ErrTenantMismatch) {
		t.Fatalf("tenant tamper = %v", err)
	}
	tampered = env
	tampered.Data[0] ^= 1
	if _, _, err := m.Decrypt(ctxA, tampered, "object-1"); err == nil {
		t.Fatal("tampered ciphertext decrypted")
	}
	tampered = env
	tampered.Header.KEKVersion = "v99"
	if _, _, err := m.Decrypt(ctxA, tampered, "object-1"); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("unknown KEK version = %v", err)
	}
}

func TestTodo_TRUST_028_Mutation(t *testing.T) {
	m, ctxA, _, p := envelopeFixture(t)
	env, _, err := m.Encrypt(ctxA, "object-1", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), env.Data...)
	delete(p.keys, env.WrappedDEK.Handle)
	if _, _, err := m.Decrypt(ctxA, env, "object-1"); err == nil {
		t.Fatal("revoked/unavailable KEK unexpectedly opened envelope")
	}
	if string(before) != string(env.Data) {
		t.Fatal("data layer changed during failed unwrap")
	}
}

func FuzzTodo_TRUST_028(f *testing.F) {
	f.Add([]byte("seed"))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, ctxA, _, _ := envelopeFixture(t)
		env, _, err := m.Encrypt(ctxA, "object", data)
		if err != nil {
			t.Fatal(err)
		}
		env.Header.KEKVersion = "tampered"
		_, _, _ = m.Decrypt(ctxA, env, "object")
	})
}

func TestExplain(t *testing.T) {
	if !strings.Contains(Explain(), "tenant KEK") {
		t.Fatal("Explain omits tenant KEK hierarchy")
	}
}
