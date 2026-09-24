package workflow_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

var update = flag.Bool("update", false, "rewrite the checked-in golden vectors")

// echoHandler is the placeholder implementation every fixture capability is
// bound to. The compiler never invokes a handler; a registry simply refuses to
// publish a definition with no binding.
func echoHandler(_ context.Context, payload any) (any, error) { return payload, nil }

// fixtureSchema mirrors the BOOTSTRAP registry's schema naming so a fixture
// capability and the reference workflow agree on schema identity.
func fixtureSchema(id, slot string) capability.SchemaRef {
	return capability.SchemaRef{
		SchemaID:         id + "." + slot + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition",
	}
}

// fixtureCapability returns a publishable read-only capability definition.
func fixtureCapability(id, domain string, effect capability.EffectClass) capability.Definition {
	def := capability.Definition{
		ID:                   id,
		Version:              1,
		OwnerDomain:          domain,
		RequestSchema:        fixtureSchema(id, "request"),
		ResponseSchema:       fixtureSchema(id, "response"),
		ErrorSchema:          fixtureSchema(id, "error"),
		EffectClass:          effect,
		ReadData:             capability.DataDomainFieldSet{DataDomains: []string{domain}},
		RiskClass:            "LOW",
		IdempotencyPolicyRef: "idempotency.read-safe.v1",
		AuthZScopeRef:        "scope:" + domain + ".read",
		LegalBasisRef:        "legal.p1a.observation-only.v1",
		EntitlementRef:       "entitlement.pilot.p1a.v1",
		SLOClassRef:          "slo.interactive.p95-2s.v1",
		TestRef:              "conformance:" + id + "/v1",
	}
	if effect.IsWrite() {
		def.WriteData = capability.DataDomainFieldSet{DataDomains: []string{domain}}
		def.AuthZScopeRef = "scope:" + domain + ".write"
		def.IdempotencyPolicyRef = "idempotency.effect-key.v1"
		def.RiskClass = "HIGH"
	}
	return def
}

// promotionRegistry publishes exactly the capability versions the promotion
// reference workflow binds, with the same identities the BOOTSTRAP table uses.
// Compiling the golden against this fixed table rather than the live bootstrap
// registry keeps the golden a statement about the compiler, not about another
// package's capability catalogue.
func promotionRegistry(t *testing.T) *capability.Registry {
	t.Helper()
	r := capability.NewRegistry()
	domains := map[string]string{
		"hcmnext.people.explain_worker_state":        "people",
		"hcmnext.rewards.simulate_compensation":      "rewards",
		"hcmnext.rewards.evaluate_pay_band_position": "rewards",
		"hcmnext.operations.detect_drift":            "operations",
	}
	for _, key := range workflow.PromotionCapabilities() {
		domain, ok := domains[key.ID]
		if !ok {
			t.Fatalf("fixture registry has no domain for %s", key)
		}
		if err := r.Register(fixtureCapability(key.ID, domain, capability.EffectReadOnly), echoHandler); err != nil {
			t.Fatalf("publish fixture capability %s: %v", key, err)
		}
	}
	return r
}

// mustCompilePromotion compiles the reference workflow and fails the test with
// the full diagnostic set if it does not publish.
func mustCompilePromotion(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	plan, err := workflow.Compile(workflow.PromotionReferenceDefinition(), workflow.Options{
		Phase: workflow.PhaseP1A, Capabilities: promotionRegistry(t), IRSchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("promotion reference must compile: %v", err)
	}
	return plan
}

// diagnostics extracts the typed diagnostic set from a compile error.
func diagnostics(t *testing.T, err error) *workflow.Diagnostics {
	t.Helper()
	if err == nil {
		t.Fatal("expected compilation to be rejected, got no error")
	}
	if !errors.Is(err, workflow.ErrCompile) {
		t.Fatalf("expected a compile rejection, got %v", err)
	}
	var d *workflow.Diagnostics
	if !errors.As(err, &d) {
		t.Fatalf("expected *workflow.Diagnostics, got %T", err)
	}
	return d
}

// mustReject compiles def, requires rejection, and asserts the named code is
// present. It returns the whole diagnostic set so a caller can assert more.
func mustReject(t *testing.T, def workflow.Definition, opts workflow.Options, code string) *workflow.Diagnostics {
	t.Helper()
	plan, err := workflow.Compile(def, opts)
	if plan != nil {
		t.Fatalf("expected no plan when compilation is rejected, got digest %s", plan.Digest())
	}
	d := diagnostics(t, err)
	if !d.Has(code) {
		t.Fatalf("expected diagnostic %s, got %v", code, d.Codes())
	}
	return d
}

// goldenJSON compares v against a checked-in golden file, or rewrites it under
// -update.
func goldenJSON(t *testing.T, name string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden %s: %v", name, err)
	}
	b = append(b, '\n')
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (regenerate with -update): %v", path, err)
	}
	if !bytes.Equal(b, want) {
		t.Fatalf("golden %s drifted\n got:\n%s\nwant:\n%s", path, b, want)
	}
}

// promotionOptions is the compiler configuration the reference workflow uses.
func promotionOptions(t *testing.T) workflow.Options {
	t.Helper()
	return workflow.Options{Phase: workflow.PhaseP1A, Capabilities: promotionRegistry(t), IRSchemaVersion: 1}
}
