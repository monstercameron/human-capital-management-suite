package telemetry_test

import (
	"fmt"
	"sync"
	"testing"

	operationstelemetry "github.com/monstercameron/human-capital-management-suite/internal/operations/telemetry"
	platformtelemetry "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
)

func liveEvaluator(t *testing.T, sampling platformtelemetry.SamplingPolicy) *platformtelemetry.Evaluator {
	t.Helper()
	allow, err := platformtelemetry.NewAllowlist(
		platformtelemetry.AttributeDefinition{Key: "outcome", Class: platformtelemetry.ClassOperationalPublic, Signals: []platformtelemetry.SignalKind{platformtelemetry.SignalMetric, platformtelemetry.SignalLog}, MaxCardinality: 8},
		platformtelemetry.AttributeDefinition{Key: "environment", Class: platformtelemetry.ClassOperationalPublic, Signals: []platformtelemetry.SignalKind{platformtelemetry.SignalMetric}, MaxCardinality: 8},
	)
	if err != nil {
		t.Fatal(err)
	}
	return platformtelemetry.NewEvaluator(allow, platformtelemetry.DefaultExportPolicy(1), sampling)
}

func TestTodo_OPS_002(t *testing.T) {
	policy := operationstelemetry.DefaultPolicy()
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	eval := liveEvaluator(t, platformtelemetry.SamplingPolicy{Version: 1, SuccessSampleRate: 0})

	if got := eval.EvaluateAttribute(platformtelemetry.SignalMetric, "salary", "125000"); got.Kept || got.Value != "" {
		t.Fatalf("sensitive metric attribute escaped: %+v", got)
	}
	kept := eval.EvaluateAttribute(platformtelemetry.SignalMetric, "outcome", "SUCCESS")
	if !kept.Kept || kept.Value != "SUCCESS" || kept.Class != platformtelemetry.ClassOperationalPublic {
		t.Fatalf("allowlisted metric attribute=%+v", kept)
	}
	decision := eval.Decide("request-opaque-1", platformtelemetry.RetentionFinancialMutation)
	if !decision.Retained || decision.Receipt.PolicyVersion != operationstelemetry.ContractVersion || decision.Receipt.Signature == "" || decision.Receipt.PolicyDigest == "" {
		t.Fatalf("critical decision lacks policy receipt: %+v", decision)
	}
}

func TestTodo_OPS_002_Race(t *testing.T) {
	eval := liveEvaluator(t, platformtelemetry.SamplingPolicy{Version: 1, SuccessSampleRate: 0.1})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = eval.EvaluateAttribute(platformtelemetry.SignalMetric, "environment", fmt.Sprintf("env-%d", i))
			got := eval.Decide(fmt.Sprintf("request-%d", i), platformtelemetry.RetentionSecurityDenial)
			if !got.Retained || got.Receipt.Signature == "" {
				t.Errorf("critical telemetry was not retained: %+v", got)
			}
		}(i)
	}
	wg.Wait()
	if got := eval.Cardinality.DistinctCount("environment"); got > 8 {
		t.Fatalf("metric cardinality=%d, want <= 8", got)
	}
}

func TestTodo_OPS_002_Security(t *testing.T) {
	eval := liveEvaluator(t, platformtelemetry.SamplingPolicy{Version: 1, SuccessSampleRate: 1})
	for _, key := range []string{"salary", "medical_diagnosis", "bank_routing_number", "case_file", "prompt_text", "payload"} {
		got := eval.EvaluateAttribute(platformtelemetry.SignalLog, key, "secret-value")
		if got.Kept || got.Value != "" || got.DropReason == "" {
			t.Fatalf("prohibited key %q escaped: %+v", key, got)
		}
	}
	if got := eval.PolicyReceipt(); got.Signature == "" || got.PolicyDigest == "" || got.Decision != "evaluator_ready" {
		t.Fatalf("policy receipt=%+v", got)
	}
}

func TestTodo_OPS_002_Mutation(t *testing.T) {
	allow, err := platformtelemetry.NewAllowlist(platformtelemetry.AttributeDefinition{
		Key: "over_budget", Class: platformtelemetry.ClassOperationalPublic,
		Signals: []platformtelemetry.SignalKind{platformtelemetry.SignalMetric}, MaxCardinality: 257,
	})
	if err != nil {
		t.Fatal(err)
	}
	eval := platformtelemetry.NewEvaluator(allow, platformtelemetry.DefaultExportPolicy(1), platformtelemetry.DefaultSamplingPolicy())
	if got := eval.EvaluateAttribute(platformtelemetry.SignalMetric, "over_budget", "value"); got.Kept || got.DropReason != "policy_cardinality_budget_exceeded" {
		t.Fatalf("over-budget metric policy boundary=%+v", got)
	}
	critical := eval.Decide("mutation-boundary", platformtelemetry.RetentionTelemetryPipelineFailure)
	if !critical.Retained || critical.Receipt.FailureClass != operationstelemetry.TelemetryPipelineFailure {
		t.Fatalf("critical failure retention boundary=%+v", critical)
	}
}
