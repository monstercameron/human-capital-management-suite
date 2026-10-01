package cryptoagile

import (
	"errors"
	"testing"
	"time"
)

func TestCryptoAgilityRuntimeRejectsUnknownPlanSuite(t *testing.T) {
	plan := MigrationPlan{Windows: []Window{{Start: runtimeDay(0), ActiveSuiteID: "missing"}}}
	keys := NewFakeKeySource()
	if _, err := NewRuntime(plan, nil, keys, func() time.Time { return runtimeDay(1) }); !errors.Is(err, ErrRuntimeSuite) {
		t.Fatalf("NewRuntime unknown suite = %v, want ErrRuntimeSuite", err)
	}
}

func TestCryptoAgilityRuntimeRejectsDuplicateConfiguration(t *testing.T) {
	suite := AlgorithmSuite{ID: "suite", Kind: KindSignature, Status: StatusActive, ActivatedAt: runtimeDay(0)}
	plan := MigrationPlan{Windows: []Window{{Start: runtimeDay(0), ActiveSuiteID: suite.ID}}}
	if _, err := NewRuntime(plan, []AlgorithmSuite{suite, suite}, NewFakeKeySource(), nil); !errors.Is(err, ErrDuplicateSuite) {
		t.Fatalf("NewRuntime duplicate suite = %v, want ErrDuplicateSuite", err)
	}
}

// TestTodo_CRYPTO_001_Mutation proves that the runtime keeps the signed
// envelope bound to its suite id and refuses the old suite after retirement.
func TestTodo_CRYPTO_001_Mutation(t *testing.T) {
	keys := NewFakeKeySource()
	if err := keys.AddEd25519("old", runtimeSeed(1)); err != nil {
		t.Fatal(err)
	}
	if err := keys.AddEd25519("new", runtimeSeed(2)); err != nil {
		t.Fatal(err)
	}
	plan := MigrationPlan{Windows: []Window{
		{Start: runtimeDay(0), End: runtimeDay(10), ActiveSuiteID: "old"},
		{Start: runtimeDay(10), End: runtimeDay(20), ActiveSuiteID: "new", DualSuiteID: "old"},
		{Start: runtimeDay(20), ActiveSuiteID: "new"},
	}}
	runtime, err := NewRuntime(plan, []AlgorithmSuite{
		{ID: "old", Kind: KindSignature, Status: StatusDual, ActivatedAt: runtimeDay(0)},
		{ID: "new", Kind: KindSignature, Status: StatusActive, ActivatedAt: runtimeDay(10)},
	}, keys, func() time.Time { return runtimeDay(15) })
	if err != nil {
		t.Fatal(err)
	}
	envs, err := runtime.Signer.SignAll([]byte("immutable-ledger-evidence"))
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 2 {
		t.Fatalf("SignAll returned %d envelopes, want 2", len(envs))
	}
	original := append([]byte(nil), envs[0].Signature...)
	mutated := envs[0]
	mutated.SuiteID = "old"
	if err := runtime.Verifier.Verify([]byte("immutable-ledger-evidence"), mutated); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("relabeled envelope = %v, want ErrSignatureInvalid", err)
	}
	if string(envs[0].Signature) != string(original) {
		t.Fatal("verifying a relabeled copy mutated the recorded signature")
	}
	if _, err := runtime.Registry.Transition("old", StatusRetired, runtimeDay(20)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Verifier.Verify([]byte("immutable-ledger-evidence"), envs[1]); err == nil {
		t.Fatal("retired suite verified after cutoff")
	}
}

func runtimeDay(day int) time.Time {
	return time.Date(2026, time.January, 1+day, 0, 0, 0, 0, time.UTC)
}

func runtimeSeed(n byte) []byte {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = n + byte(i)
	}
	return seed
}
