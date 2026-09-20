package envelope

import (
	"errors"
	"testing"
)

// TestTodo_REV_036_02 probes the envelope AAD gaps the review claims: an
// unknown header Version or Algorithm, a tampered DEK id, a swapped DEK
// wrapper and a cross-tenant envelope must all fail closed, with
// authentication and unverifiable errors mapping to ErrInvalidCiphertext.
func TestTodo_REV_036_02(t *testing.T) {
	m, ctxA, ctxB, _ := envelopeFixture(t)
	env, _, err := m.Encrypt(ctxA, "object-1", []byte("sensitive payroll"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	cases := map[string]func(Envelope) Envelope{
		"unknown-version":   func(e Envelope) Envelope { e.Header.Version = 999; return e },
		"unknown-algorithm": func(e Envelope) Envelope { e.Header.Algorithm = "XOR-ROT13"; return e },
		"tampered-dekid":    func(e Envelope) Envelope { e.Header.DEKID = "dek-ffff"; return e },
		"tampered-nonce":    func(e Envelope) Envelope { e.Header.Nonce[0] ^= 0xff; return e },
		"tampered-data":     func(e Envelope) Envelope { e.Data[0] ^= 0xff; return e },
	}
	for name, tamper := range cases {
		if _, _, err := m.Decrypt(ctxA, tamper(env)); !errors.Is(err, ErrInvalidCiphertext) {
			t.Errorf("%s: Decrypt err = %v, want ErrInvalidCiphertext", name, err)
		}
	}
	other, _, err := m.Encrypt(ctxA, "object-2", []byte("other"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	swapped := env
	swapped.WrappedDEK = other.WrappedDEK
	if _, _, err := m.Decrypt(ctxA, swapped); !errors.Is(err, ErrInvalidCiphertext) {
		t.Errorf("swapped-wrapper: Decrypt err = %v, want ErrInvalidCiphertext", err)
	}
	if _, _, err := m.Decrypt(ctxB, env); err == nil {
		t.Error("cross-tenant: Decrypt succeeded with another tenant context")
	}
	if _, _, err := m.Decrypt(ctxA, Envelope{}); !errors.Is(err, ErrInvalidCiphertext) {
		t.Errorf("empty: Decrypt err = %v, want ErrInvalidCiphertext", err)
	}
}

// TestTodo_REV_036_02_Security proves the unwrap path used by rotation and
// rewrap enforces the same header binding: a wrapper rebound to a different
// KEK version never opens, even when the header still names the old one.
func TestTodo_REV_036_02_Security(t *testing.T) {
	m, ctxA, _, _ := envelopeFixture(t)
	env, _, err := m.Encrypt(ctxA, "object-1", []byte("sensitive payroll"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	rotated, _, err := m.RotateKEK(ctxA, []Envelope{env})
	if err != nil {
		t.Fatalf("RotateKEK: %v", err)
	}
	if len(rotated) != 1 {
		t.Fatalf("RotateKEK returned %d envelopes, want 1", len(rotated))
	}
	rebound := env
	rebound.WrappedDEK = rotated[0].WrappedDEK
	if _, _, err := m.Decrypt(ctxA, rebound); !errors.Is(err, ErrInvalidCiphertext) {
		t.Errorf("rebound-wrapper: Decrypt err = %v, want ErrInvalidCiphertext", err)
	}
	if _, _, err := m.Rewrap(ctxA, rebound); !errors.Is(err, ErrInvalidCiphertext) {
		t.Errorf("rebound-rewrap: Rewrap err = %v, want ErrInvalidCiphertext", err)
	}
}
