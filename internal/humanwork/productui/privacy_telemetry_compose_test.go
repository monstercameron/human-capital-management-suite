package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	operationstelemetry "github.com/monstercameron/human-capital-management-suite/internal/operations/telemetry"
)

// WEB-235 audit follow-up: the privacy-telemetry page must render from the
// governed ops telemetry policy, not from hand-built projections. These
// tests compose the projection from the real service type and prove the
// acceptance behavior: exact versioned facts, determinism, authorization
// shape, locale honesty, and fail-closed faults.

func web235ComposedView(t *testing.T, policy operationstelemetry.Policy) View {
	t.Helper()
	projection, err := ComposePrivacyTelemetryPolicy(policy)
	if err != nil {
		t.Fatalf("composing the governed policy: %v", err)
	}
	view := testView(PagePrivacyTelemetry)
	view.PrivacyTelemetry = &projection
	return view
}

// TestTodo_WEB_235_ServiceBacked proves the live page renders exact facts
// derived from the governed policy: version, digest, sample rate,
// cardinality limit and the sorted critical classes.
func TestTodo_WEB_235_ServiceBacked(t *testing.T) {
	policy := operationstelemetry.DefaultPolicy()
	projection, err := ComposePrivacyTelemetryPolicy(policy)
	if err != nil {
		t.Fatalf("composing the default policy: %v", err)
	}
	if !projection.Ready {
		t.Fatal("composed projection is not ready")
	}
	if projection.PolicyVersion != policy.Version || projection.PolicyVersion != operationstelemetry.ContractVersion {
		t.Fatalf("policy version = %d, want contract version %d", projection.PolicyVersion, operationstelemetry.ContractVersion)
	}
	if projection.PolicyDigest != policy.Digest() {
		t.Fatalf("policy digest = %q, want the governed digest %q", projection.PolicyDigest, policy.Digest())
	}
	if projection.SuccessSampleRate != "0.1" {
		t.Fatalf("sample rate = %q, want 0.1", projection.SuccessSampleRate)
	}
	if projection.MetricCardinalityLimit != 256 {
		t.Fatalf("cardinality limit = %d, want 256", projection.MetricCardinalityLimit)
	}
	wantClasses := []string{"ambiguous_result", "correctness_failure", "financial_mutation", "irreversible_effect", "security_denial", "telemetry_pipeline_failure"}
	if len(projection.CriticalFailureClasses) != len(wantClasses) {
		t.Fatalf("critical classes = %v, want %v", projection.CriticalFailureClasses, wantClasses)
	}
	for i, want := range wantClasses {
		if projection.CriticalFailureClasses[i] != want {
			t.Fatalf("critical classes = %v, want sorted %v", projection.CriticalFailureClasses, wantClasses)
		}
	}
	if projection.EvidenceRef != "" {
		t.Fatalf("evidence ref = %q, want empty: the policy view carries no decision receipt", projection.EvidenceRef)
	}

	doc, err := Render(web235ComposedView(t, policy))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-service-state="ready"`, policy.Digest(), "0.1", "256", "security_denial", "telemetry_pipeline_failure"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("service-backed page missing %q", want)
		}
	}
	again, err := Render(web235ComposedView(t, policy))
	if err != nil {
		t.Fatal(err)
	}
	if doc != again {
		t.Fatal("service-backed page renders nondeterministically")
	}
}

// TestTodo_WEB_235_ServiceGolden pins the composed facts digest: the same
// governed policy must project the same bytes on every run.
func TestTodo_WEB_235_ServiceGolden(t *testing.T) {
	projection, err := ComposePrivacyTelemetryPolicy(operationstelemetry.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	canonical := fmt.Sprintf("%d|%s|%s|%d|%s|%t",
		projection.PolicyVersion, projection.PolicyDigest, projection.SuccessSampleRate,
		projection.MetricCardinalityLimit, strings.Join(projection.CriticalFailureClasses, ","),
		projection.Ready)
	sum := sha256.Sum256([]byte(canonical))
	got := hex.EncodeToString(sum[:])
	const want = "48bae85d7afd1c301b952c31135cf7bc5441964a046a913e2c08205390721e78"
	if got != want {
		t.Fatalf("composed facts digest = %s, want %s", got, want)
	}
}

// TestTodo_WEB_235_ServiceSecurity proves the service path cannot render
// raw or hostile values: facts derive only from validated policy fields,
// markup escapes hostile class identifiers, and an invalid policy fails
// closed to the honest fallback.
func TestTodo_WEB_235_ServiceSecurity(t *testing.T) {
	policy := operationstelemetry.DefaultPolicy()
	policy.CriticalFailures = append(policy.CriticalFailures, operationstelemetry.CriticalFailure("</script><img src=x>"))
	projection, err := ComposePrivacyTelemetryPolicy(policy)
	if err != nil {
		t.Fatalf("composing a policy with an odd class identifier: %v", err)
	}
	view := testView(PagePrivacyTelemetry)
	view.PrivacyTelemetry = &projection
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "</script><img src=x>") {
		t.Fatal("service-backed page rendered a hostile class identifier unescaped")
	}

	bad := operationstelemetry.DefaultPolicy()
	bad.MetricCardinalityLimit = 0
	if _, err := ComposePrivacyTelemetryPolicy(bad); err == nil {
		t.Fatal("invalid policy composed without an error")
	}
	fallback := testView(PagePrivacyTelemetry)
	fallbackDoc, err := Render(fallback)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fallbackDoc, "data-service-state=\"ready\"") {
		t.Fatal("fallback page claims the ready state")
	}
}

// TestTodo_WEB_235_ServiceFault proves every invalid policy shape fails
// the composer with an error and no ready projection.
func TestTodo_WEB_235_ServiceFault(t *testing.T) {
	base := operationstelemetry.DefaultPolicy()
	cases := map[string]func(*operationstelemetry.Policy){
		"wrong version":  func(p *operationstelemetry.Policy) { p.Version = operationstelemetry.ContractVersion + 1 },
		"rate above one": func(p *operationstelemetry.Policy) { p.SuccessSampleRate = 2 },
		"negative rate":  func(p *operationstelemetry.Policy) { p.SuccessSampleRate = -0.5 },
		"zero limit":     func(p *operationstelemetry.Policy) { p.MetricCardinalityLimit = 0 },
		"empty class":    func(p *operationstelemetry.Policy) { p.CriticalFailures = append(p.CriticalFailures, " ") },
		"duplicate class": func(p *operationstelemetry.Policy) {
			p.CriticalFailures = append(p.CriticalFailures, p.CriticalFailures[0])
		},
	}
	for name, mutate := range cases {
		policy := base
		policy.CriticalFailures = append([]operationstelemetry.CriticalFailure(nil), base.CriticalFailures...)
		mutate(&policy)
		projection, err := ComposePrivacyTelemetryPolicy(policy)
		if err == nil {
			t.Errorf("%s: composer accepted an invalid policy", name)
		}
		if projection.Ready {
			t.Errorf("%s: composer returned a ready projection for an invalid policy", name)
		}
	}
	if _, err := ComposePrivacyTelemetryPolicy(operationstelemetry.Policy{}); err == nil {
		t.Error("zero policy composed without an error")
	}
}

// TestTodo_WEB_235_ServiceConformance proves the service-backed page keeps
// the registry contract in every locale: no leaked message keys, with the
// same governed facts.
func TestTodo_WEB_235_ServiceConformance(t *testing.T) {
	policy := operationstelemetry.DefaultPolicy()
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(web235ComposedView(t, policy), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "\\u27e6") {
			t.Fatalf("service-backed page leaks a key in %s", code)
		}
		if !strings.Contains(doc, policy.Digest()) {
			t.Fatalf("service-backed page missing the governed digest in %s", code)
		}
	}
}
