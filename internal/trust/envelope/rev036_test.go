package envelope

import (
	"errors"
	"strings"
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
	if env.Header.ObjectID != "object-1" {
		t.Fatalf("header object id = %q, want object-1", env.Header.ObjectID)
	}
	if _, _, err := m.Decrypt(ctxA, env, "object-2"); !errors.Is(err, ErrObjectMismatch) {
		t.Fatalf("wrong expected object id = %v, want ErrObjectMismatch", err)
	}
	if _, _, err := m.Decrypt(ctxA, env, "object-1"); err != nil {
		t.Fatalf("matching expected object id: %v", err)
	}
	if _, _, err := m.Decrypt(ctxA, env); !errors.Is(err, ErrObjectMismatch) {
		t.Fatalf("missing expected object id = %v, want ErrObjectMismatch", err)
	}
	cases := map[string]func(Envelope) Envelope{
		"unknown-version":   func(e Envelope) Envelope { e.Header.Version = 999; return e },
		"unknown-algorithm": func(e Envelope) Envelope { e.Header.Algorithm = "XOR-ROT13"; return e },
		"tampered-dekid":    func(e Envelope) Envelope { e.Header.DEKID = "dek-ffff"; return e },
		"tampered-objectid": func(e Envelope) Envelope { e.Header.ObjectID = "object-2"; return e },
		"tampered-nonce":    func(e Envelope) Envelope { e.Header.Nonce[0] ^= 0xff; return e },
		"tampered-data":     func(e Envelope) Envelope { e.Data[0] ^= 0xff; return e },
	}
	for name, tamper := range cases {
		if _, _, err := m.Decrypt(ctxA, tamper(env), "object-1"); !errors.Is(err, ErrInvalidCiphertext) {
			t.Errorf("%s: Decrypt err = %v, want ErrInvalidCiphertext", name, err)
		}
	}
	other, _, err := m.Encrypt(ctxA, "object-2", []byte("other"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	swapped := env
	swapped.WrappedDEK = other.WrappedDEK
	if _, _, err := m.Decrypt(ctxA, swapped, "object-1"); !errors.Is(err, ErrInvalidCiphertext) {
		t.Errorf("swapped-wrapper: Decrypt err = %v, want ErrInvalidCiphertext", err)
	}
	if _, _, err := m.Decrypt(ctxB, env, "object-1"); err == nil {
		t.Error("cross-tenant: Decrypt succeeded with another tenant context")
	}
	if _, _, err := m.Decrypt(ctxA, Envelope{}); !errors.Is(err, ErrInvalidCiphertext) {
		t.Errorf("empty: Decrypt err = %v, want ErrInvalidCiphertext", err)
	}
}

// FuzzTodo_REV_036_02 verifies that an expected object ID different from the
// authenticated header identity is always refused before plaintext is returned.
func FuzzTodo_REV_036_02(f *testing.F) {
	f.Add("object-1", "object-2")
	f.Add("", "object-1")
	f.Add(" ", "object-1")
	f.Fuzz(func(t *testing.T, sealedID, expectedID string) {
		if len(sealedID) > 256 || len(expectedID) > 256 {
			t.Skip()
		}
		m, ctxA, _, _ := envelopeFixture(t)
		env, _, err := m.Encrypt(ctxA, sealedID, []byte("secret"))
		if strings.TrimSpace(sealedID) == "" {
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("Encrypt invalid object ID %q: %v, want ErrInvalidRequest", sealedID, err)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if sealedID == expectedID {
			t.Skip()
		}
		if _, _, err := m.Decrypt(ctxA, env, expectedID); !errors.Is(err, ErrObjectMismatch) {
			t.Fatalf("Decrypt expected ID %q for header ID %q: %v", expectedID, sealedID, err)
		}
	})
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
	if _, _, err := m.Decrypt(ctxA, rebound, "object-1"); !errors.Is(err, ErrInvalidCiphertext) {
		t.Errorf("rebound-wrapper: Decrypt err = %v, want ErrInvalidCiphertext", err)
	}
	if _, _, err := m.Rewrap(ctxA, rebound); !errors.Is(err, ErrInvalidCiphertext) {
		t.Errorf("rebound-rewrap: Rewrap err = %v, want ErrInvalidCiphertext", err)
	}
}
