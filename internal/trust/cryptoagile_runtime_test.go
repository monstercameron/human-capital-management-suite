package trust

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/cryptoagile"
)

// TestTodo_CRYPTO_001_Integration proves the shipped trust package reaches
// cryptoagile through its production composition seam, not only through the
// cryptoagile package's own tests.
func TestTodo_CRYPTO_001_Integration(t *testing.T) {
	keys := cryptoagile.NewFakeKeySource()
	if err := keys.AddEd25519("old", servedCryptoSeed(3)); err != nil {
		t.Fatal(err)
	}
	if err := keys.AddEd25519("new", servedCryptoSeed(4)); err != nil {
		t.Fatal(err)
	}
	plan := cryptoagile.MigrationPlan{Windows: []cryptoagile.Window{
		{Start: servedCryptoDay(0), End: servedCryptoDay(10), ActiveSuiteID: "old"},
		{Start: servedCryptoDay(10), End: servedCryptoDay(20), ActiveSuiteID: "new", DualSuiteID: "old"},
		{Start: servedCryptoDay(20), ActiveSuiteID: "new"},
	}}
	runtime, err := NewCryptoAgilityRuntime(plan, []cryptoagile.AlgorithmSuite{
		{ID: "old", Kind: cryptoagile.KindSignature, Status: cryptoagile.StatusDual, ActivatedAt: servedCryptoDay(0)},
		{ID: "new", Kind: cryptoagile.KindSignature, Status: cryptoagile.StatusActive, ActivatedAt: servedCryptoDay(10)},
	}, keys, func() time.Time { return servedCryptoDay(15) })
	if err != nil {
		t.Fatal(err)
	}
	envs, err := runtime.Signer.SignAll([]byte("served-ledger-evidence"))
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 2 {
		t.Fatalf("served SignAll returned %d envelopes, want 2", len(envs))
	}
	if _, err := runtime.Verifier.VerifyAny([]byte("served-ledger-evidence"), envs); err != nil {
		t.Fatalf("served VerifyAny: %v", err)
	}
	if _, err := runtime.Registry.Transition("old", cryptoagile.StatusRetired, servedCryptoDay(20)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Verifier.Verify([]byte("served-ledger-evidence"), envs[1]); err == nil {
		t.Fatal("served verifier accepted retired suite")
	} else {
		var retired *cryptoagile.RetiredSuiteError
		if !errors.As(err, &retired) {
			t.Fatalf("retired verification error = %v, want RetiredSuiteError", err)
		}
	}
}

func servedCryptoDay(day int) time.Time {
	return time.Date(2026, time.January, 1+day, 0, 0, 0, 0, time.UTC)
}

func servedCryptoSeed(n byte) []byte {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = n + byte(i)
	}
	return seed
}
