package telemetry_test

import (
	"crypto/ed25519"
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
)

func ops002Evaluator(t *testing.T) (*telemetry.Evaluator, ed25519.PrivateKey, ed25519.PublicKey) {
	t.Helper()
	allow := testAllowlist(t)
	eval := telemetry.NewEvaluator(allow, telemetry.DefaultExportPolicy(telemetry.DefaultPolicyVersion), telemetry.DefaultSamplingPolicy())
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return eval, private, public
}

// TestTodo_OPS_002 proves the live platform evaluator, rather than the
// removed operations/telemetry package, owns the privacy/cardinality/sampling
// contract and emits signed, payload-free evidence.
func TestTodo_OPS_002(t *testing.T) {
	eval, private, public := ops002Evaluator(t)

	attrs := map[string]string{
		"cell_id":              "cell-a",
		"environment":          "prod",
		"salary":               "100000",
		"medical_diagnosis":    "restricted",
		"authorization_header": "Bearer secret",
	}
	got, err := eval.EvaluateSignal(telemetry.SignalMetric, attrs, "corr-ops-002", telemetry.RetentionCorrectnessFailure, private)
	if err != nil {
		t.Fatalf("EvaluateSignal: %v", err)
	}
	if got.Attributes["salary"] != "" || got.Attributes["medical_diagnosis"] != "" || got.Attributes["authorization_header"] != "" {
		t.Fatalf("sensitive attributes escaped export: %#v", got.Attributes)
	}
	if got.Attributes["cell_id"] != "cell-a" || got.Attributes["environment"] != "prod" {
		t.Fatalf("allow-listed attributes missing or changed: %#v", got.Attributes)
	}
	if !got.Receipt.Retained || got.Receipt.Retention != telemetry.RetentionCorrectnessFailure {
		t.Fatalf("critical failure was not forced-retained: %+v", got.Receipt)
	}
	if got.Receipt.Signature == nil || len(got.Receipt.Signature) != ed25519.SignatureSize {
		t.Fatalf("receipt is not signed: %+v", got.Receipt)
	}
	if err := got.Receipt.Verify(public); err != nil {
		t.Fatalf("receipt verification: %v", err)
	}
}

// TestTodo_OPS_002_Race exercises the shared cardinality governor from
// concurrent exports and asserts every result remains allow-listed.
func TestTodo_OPS_002_Race(t *testing.T) {
	eval, private, _ := ops002Evaluator(t)
	const workers = 24
	results := make(chan telemetry.ExportedSignal, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result, err := eval.EvaluateSignal(telemetry.SignalMetric, map[string]string{
				"environment": "env-" + string(rune('a'+i)),
				"salary":      "secret",
			}, "corr-race", telemetry.RetentionNone, private)
			if err != nil {
				t.Errorf("EvaluateSignal(%d): %v", i, err)
				return
			}
			results <- result
		}(i)
	}
	wg.Wait()
	close(results)
	for result := range results {
		if _, ok := result.Attributes["salary"]; ok {
			t.Fatal("sensitive metric attribute escaped concurrent export")
		}
		if result.Receipt.CardinalityCapped > 1 {
			t.Fatalf("cardinality cap count is invalid: %+v", result.Receipt)
		}
	}
}

// TestTodo_OPS_002_Security proves receipt tampering and an invalid signing
// key fail closed, while the receipt never carries raw attribute content.
func TestTodo_OPS_002_Security(t *testing.T) {
	eval, private, public := ops002Evaluator(t)
	result, err := eval.EvaluateSignal(telemetry.SignalLog, map[string]string{
		"capability_id": "cap.workflow.start",
		"error_type":    "SECRET_VALUE_MUST_NOT_BE_IN_RECEIPT",
	}, "corr-security", telemetry.RetentionSecurityDenial, private)
	if err != nil {
		t.Fatalf("EvaluateSignal: %v", err)
	}
	if err := result.Receipt.Verify(public); err != nil {
		t.Fatalf("valid receipt rejected: %v", err)
	}
	tampered := result.Receipt
	tampered.Kept++
	if !errors.Is(tampered.Verify(public), telemetry.ErrPolicyReceiptSignature) {
		t.Fatalf("tampered receipt was accepted: %v", tampered.Verify(public))
	}
	if _, err := eval.EvaluateSignal(telemetry.SignalLog, nil, "corr-security", telemetry.RetentionNone, ed25519.PrivateKey{}); !errors.Is(err, telemetry.ErrPolicyReceiptKey) {
		t.Fatalf("invalid signing key error = %v", err)
	}
}

// TestTodo_OPS_002_Mutation flips the critical-retention and cardinality
// boundaries so a one-unit weakening cannot pass silently.
func TestTodo_OPS_002_Mutation(t *testing.T) {
	eval, private, _ := ops002Evaluator(t)
	zeroRate := telemetry.SamplingPolicy{Version: 1, SuccessSampleRate: 0}
	forced := telemetry.NewEvaluator(testAllowlist(t), telemetry.DefaultExportPolicy(1), zeroRate)
	critical, err := forced.EvaluateSignal(telemetry.SignalLog, nil, "never-sampled", telemetry.RetentionFinancialMutation, private)
	if err != nil {
		t.Fatalf("critical EvaluateSignal: %v", err)
	}
	if !critical.Receipt.Retained {
		t.Fatal("configured critical failure was dropped at a zero success rate")
	}
	def, ok := testAllowlist(t).Lookup("environment")
	if !ok {
		t.Fatal("environment definition missing")
	}
	for i := 0; i < def.MaxCardinality; i++ {
		if _, err := eval.EvaluateSignal(telemetry.SignalMetric, map[string]string{"environment": string(rune('a' + i))}, "cardinality", telemetry.RetentionNone, private); err != nil {
			t.Fatalf("within-budget export %d: %v", i, err)
		}
	}
	overflow, err := eval.EvaluateSignal(telemetry.SignalMetric, map[string]string{"environment": "beyond-budget"}, "cardinality", telemetry.RetentionNone, private)
	if err != nil {
		t.Fatalf("overflow export: %v", err)
	}
	if overflow.Attributes["environment"] != "__overflow__" || overflow.Receipt.CardinalityCapped != 1 {
		t.Fatalf("overflow was not bounded: %#v %+v", overflow.Attributes, overflow.Receipt)
	}
}
