package version_test

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

var update = flag.Bool("update", false, "rewrite the checked-in golden vectors")

// echoHandler is the placeholder implementation every fixture capability is
// bound to. Nothing in this package ever invokes a capability handler.
func echoHandler(_ context.Context, payload any) (any, error) { return payload, nil }

// fixtureSchema mirrors internal/workflow's own fixture and bootstrap schema
// naming ("<id>.<slot>/v1"), so a fixture capability agrees with what
// [workflow.PromotionReferenceDefinition] names on its nodes.
func fixtureSchema(id, slot string) capability.SchemaRef {
	return capability.SchemaRef{
		SchemaID:         id + "." + slot + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition",
	}
}

func fixtureCapability(id, domain string) capability.Definition {
	return capability.Definition{
		ID:                   id,
		Version:              1,
		OwnerDomain:          domain,
		RequestSchema:        fixtureSchema(id, "request"),
		ResponseSchema:       fixtureSchema(id, "response"),
		ErrorSchema:          fixtureSchema(id, "error"),
		EffectClass:          capability.EffectReadOnly,
		ReadData:             capability.DataDomainFieldSet{DataDomains: []string{domain}},
		RiskClass:            "LOW",
		IdempotencyPolicyRef: "idempotency.read-safe.v1",
		AuthZScopeRef:        "scope:" + domain + ".read",
		LegalBasisRef:        "legal.p1a.observation-only.v1",
		EntitlementRef:       "entitlement.pilot.p1a.v1",
		SLOClassRef:          "slo.interactive.p95-2s.v1",
		TestRef:              "conformance:" + id + "/v1",
	}
}

// promotionCapabilityDomains names the owning domain of each capability the
// promotion reference workflow binds, so a fixture's AuthZScopeRef
// ("scope:<domain>.read") agrees with each node's declared authority scope
// (internal/workflow/promotion.go's promotionNodes).
var promotionCapabilityDomains = map[string]string{
	"hcmnext.people.explain_worker_state":        "people",
	"hcmnext.rewards.simulate_compensation":      "rewards",
	"hcmnext.rewards.evaluate_pay_band_position": "rewards",
	"hcmnext.operations.detect_drift":            "operations",
}

// promotionRegistry publishes exactly the capability versions the promotion
// reference workflow binds, read-only, so [workflow.CompilePromotionReference]
// compiles under [workflow.PhaseP1A].
func promotionRegistry(t *testing.T) *capability.Registry {
	t.Helper()
	r := capability.NewRegistry()
	for _, key := range workflow.PromotionCapabilities() {
		domain, ok := promotionCapabilityDomains[key.ID]
		if !ok {
			t.Fatalf("fixture registry has no domain for %s", key)
		}
		if err := r.Register(fixtureCapability(key.ID, domain), echoHandler); err != nil {
			t.Fatalf("publish fixture capability %s: %v", key, err)
		}
	}
	return r
}

func promotionOptions(t *testing.T) workflow.Options {
	t.Helper()
	return workflow.Options{Phase: workflow.PhaseP1A, Capabilities: promotionRegistry(t), IRSchemaVersion: 1}
}

// mustCompilePromotion compiles the reference workflow and fails the test
// with the full diagnostic set if it does not publish.
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

// fixedTime returns a deterministic instant so golden fixtures are stable.
func fixedTime() time.Time {
	return time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
}

// goldenJSON compares v against a checked-in golden file, or rewrites it
// under -update. It mirrors internal/workflow's own helpers_test.go.
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
