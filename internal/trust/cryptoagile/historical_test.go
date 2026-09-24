package cryptoagile

import (
	"errors"
	"strings"
	"testing"
)

func TestEnvelopeVerifier_VerifyHistoricalRetiredSuite(t *testing.T) {
	keys := NewFakeKeySource()
	if err := keys.AddEd25519("old", make([]byte, 32)); err != nil {
		t.Fatalf("AddEd25519: %v", err)
	}
	registry := NewRegistry()
	if err := registry.Register(AlgorithmSuite{ID: "old", Kind: KindSignature, Status: StatusActive, ActivatedAt: day(0)}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	message := []byte("recorded-at=" + day(5).Format("2006-01-02T15:04:05Z07:00") + ";payload=ledger-head")
	env, err := NewEnvelopeSigner(keys).Sign("old", message)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if _, err := registry.Transition("old", StatusRetired, day(10)); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	verifier := NewEnvelopeVerifier(registry, keys)

	if err := verifier.Verify(message, env); err == nil {
		t.Fatal("Verify retired suite = nil, want current-trust refusal")
	} else {
		var retired *RetiredSuiteError
		if !errors.As(err, &retired) {
			t.Fatalf("Verify retired suite = %v, want RetiredSuiteError", err)
		}
	}
	if err := verifier.VerifyHistorical(message, env, day(5)); err != nil {
		t.Fatalf("VerifyHistorical before cutoff: %v", err)
	}
	if err := verifier.VerifyHistorical(message, env, day(10)); !errors.Is(err, ErrSuiteNotValidAtSignedTime) {
		t.Fatalf("VerifyHistorical at cutoff = %v, want ErrSuiteNotValidAtSignedTime", err)
	}
	if err := verifier.VerifyHistorical(message, env, day(-1)); !errors.Is(err, ErrSuiteNotValidAtSignedTime) {
		t.Fatalf("VerifyHistorical before activation = %v, want ErrSuiteNotValidAtSignedTime", err)
	}
	tamperedTime := []byte(strings.Replace(string(message), day(5).Format("2006-01-02T15:04:05Z07:00"), day(4).Format("2006-01-02T15:04:05Z07:00"), 1))
	if err := verifier.VerifyHistorical(tamperedTime, env, day(4)); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("VerifyHistorical tampered signed-time field = %v, want ErrSignatureInvalid", err)
	}
}
