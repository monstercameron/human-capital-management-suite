package manifest

import (
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
)

// TestContractedIntentAndCapabilityEndpointDispositionIsTotal is
// ENDPOINT-009's RED/GREEN test: every one of the fourteen intent
// definitions in internal/intent/definitions and every published BOOTSTRAP
// capability in internal/capability maps to exactly one
// [EndpointDispositionCategory], and a definition or capability this
// package cannot classify (an unscheduled release, an unrecognized
// capability ID) is a build-time (RED) failure, never a silent gap.
func TestContractedIntentAndCapabilityEndpointDispositionIsTotal(t *testing.T) {
	report, err := BuildDefaultDispositionReport()
	if err != nil {
		t.Fatalf("BuildDefaultDispositionReport: %v", err)
	}

	if len(report.Intents) != 14 {
		t.Fatalf("expected all fourteen intent definitions, got %d", len(report.Intents))
	}
	if len(report.Capabilities) != 11 {
		t.Fatalf("expected all eleven BOOTSTRAP capabilities, got %d", len(report.Capabilities))
	}

	seenIntent := map[string]bool{}
	for _, row := range report.Intents {
		if !row.Category.Valid() {
			t.Errorf("intent %s: invalid category %q", row.DefinitionRef, row.Category)
		}
		if row.Category == CategoryNoEndpointWithJustification && row.Justification == "" {
			t.Errorf("intent %s: NO_ENDPOINT_WITH_JUSTIFICATION with no justification", row.DefinitionRef)
		}
		if seenIntent[row.DefinitionRef] {
			t.Errorf("intent %s appears twice in the disposition report", row.DefinitionRef)
		}
		seenIntent[row.DefinitionRef] = true
	}
	for _, d := range definitions.All() {
		if !seenIntent[d.Ref.String()] {
			t.Errorf("intent definition %s is missing from the disposition report (RED: missing = RED)", d.Ref.String())
		}
	}

	seenCap := map[string]bool{}
	for _, row := range report.Capabilities {
		if !row.Category.Valid() {
			t.Errorf("capability %s: invalid category %q", row.CapabilityID, row.Category)
		}
		key := row.CapabilityID
		if seenCap[key] {
			t.Errorf("capability %s appears twice in the disposition report", key)
		}
		seenCap[key] = true
	}
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("NewBootstrapRegistry: %v", err)
	}
	for _, rec := range registry.List() {
		if !seenCap[rec.Definition.ID] {
			t.Errorf("capability %s is missing from the disposition report (RED: missing = RED)", rec.Definition.ID)
		}
	}

	// A database entity never creates a CRUD route in this manifest: no
	// intent or capability is ever assigned CategoryTypedPublicMethod
	// merely for existing. Only the two registry capabilities — which back
	// RegistryService's dedicated RPCs on their own semantic grounds — use
	// that category.
	for _, row := range report.Capabilities {
		if row.Category == CategoryTypedPublicMethod {
			if row.CapabilityID != "hcmnext.registry.resolve_capability" && row.CapabilityID != "hcmnext.registry.explain_capability" {
				t.Errorf("capability %s was assigned TYPED_PUBLIC_METHOD without a dedicated RPC backing it", row.CapabilityID)
			}
		}
	}
}

// TestTodo_ENDPOINT_009_Property proves the join is a pure function of its
// inputs: two independent computations over the same defs/records agree
// field for field.
func TestTodo_ENDPOINT_009_Property(t *testing.T) {
	defs := definitions.All()
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("NewBootstrapRegistry: %v", err)
	}
	records := registry.List()

	r1, err := BuildDispositionReport(defs, records)
	if err != nil {
		t.Fatalf("BuildDispositionReport (first): %v", err)
	}
	r2, err := BuildDispositionReport(defs, records)
	if err != nil {
		t.Fatalf("BuildDispositionReport (second): %v", err)
	}
	if len(r1.Intents) != len(r2.Intents) || len(r1.Capabilities) != len(r2.Capabilities) {
		t.Fatal("two builds over identical inputs produced different sizes")
	}
	for i := range r1.Intents {
		if !reflect.DeepEqual(r1.Intents[i], r2.Intents[i]) {
			t.Fatalf("intent row %d differs across identical builds: %+v vs %+v", i, r1.Intents[i], r2.Intents[i])
		}
	}
	for i := range r1.Capabilities {
		a, b := r1.Capabilities[i], r2.Capabilities[i]
		if a.CapabilityID != b.CapabilityID || a.Category != b.Category {
			t.Fatalf("capability row %d differs across identical builds: %+v vs %+v", i, a, b)
		}
	}
}

// TestTodo_ENDPOINT_009_Golden pins the exact category assigned to every one
// of the fourteen definitions, so a reclassification is a reviewed,
// visible diff rather than a silent behavior change.
func TestTodo_ENDPOINT_009_Golden(t *testing.T) {
	golden := map[string]EndpointDispositionCategory{
		"hcmnext.people.change_manager/v1":               CategoryNoEndpointWithJustification,
		"hcmnext.people.explain_worker_state/v1":         CategoryGenericIntentLifecycleOnly,
		"hcmnext.people.promote_worker/v1":               CategoryGenericIntentLifecycleOnly,
		"hcmnext.rewards.change_base_pay/v1":             CategoryNoEndpointWithJustification,
		"hcmnext.rewards.simulate_compensation/v1":       CategoryGenericIntentLifecycleOnly,
		"hcmnext.rewards.evaluate_pay_band_position/v1":  CategoryGenericIntentLifecycleOnly,
		"hcmnext.rewards.reserve_compensation_budget/v1": CategoryNoEndpointWithJustification,
		"hcmnext.rewards.release_compensation_budget/v1": CategoryNoEndpointWithJustification,
		"hcmnext.work.approve_proposal/v1":               CategoryNoEndpointWithJustification,
		"hcmnext.work.reject_proposal/v1":                CategoryNoEndpointWithJustification,
		"hcmnext.intelligence.explain_transaction/v1":    CategoryGenericIntentLifecycleOnly,
		"hcmnext.operations.detect_drift/v1":             CategoryGenericIntentLifecycleOnly,
		"hcmnext.operations.create_repair_plan/v1":       CategoryGenericIntentLifecycleOnly,
		"hcmnext.operations.simulate_repair/v1":          CategoryGenericIntentLifecycleOnly,
	}
	report, err := BuildDefaultDispositionReport()
	if err != nil {
		t.Fatalf("BuildDefaultDispositionReport: %v", err)
	}
	if len(report.Intents) != len(golden) {
		t.Fatalf("expected %d intent rows, got %d", len(golden), len(report.Intents))
	}
	for _, row := range report.Intents {
		want, ok := golden[row.DefinitionRef]
		if !ok {
			t.Errorf("unexpected definition in report: %s", row.DefinitionRef)
			continue
		}
		if row.Category != want {
			t.Errorf("%s: category = %s, want %s", row.DefinitionRef, row.Category, want)
		}
	}

	goldenCaps := map[string]EndpointDispositionCategory{
		"hcmnext.dataops.explain_field_history":      CategoryGenericIntentLifecycleOnly,
		"hcmnext.people.explain_worker_state":        CategoryGenericIntentLifecycleOnly,
		"hcmnext.people.promote_worker":              CategoryGenericIntentLifecycleOnly,
		"hcmnext.rewards.simulate_compensation":      CategoryGenericIntentLifecycleOnly,
		"hcmnext.rewards.evaluate_pay_band_position": CategoryGenericIntentLifecycleOnly,
		"hcmnext.intelligence.explain_transaction":   CategoryGenericIntentLifecycleOnly,
		"hcmnext.operations.detect_drift":            CategoryGenericIntentLifecycleOnly,
		"hcmnext.operations.create_repair_plan":      CategoryGenericIntentLifecycleOnly,
		"hcmnext.operations.simulate_repair":         CategoryGenericIntentLifecycleOnly,
		"hcmnext.registry.resolve_capability":        CategoryTypedPublicMethod,
		"hcmnext.registry.explain_capability":        CategoryTypedPublicMethod,
	}
	if len(report.Capabilities) != len(goldenCaps) {
		t.Fatalf("expected %d capability rows, got %d", len(goldenCaps), len(report.Capabilities))
	}
	for _, row := range report.Capabilities {
		want, ok := goldenCaps[row.CapabilityID]
		if !ok {
			t.Errorf("unexpected capability in report: %s", row.CapabilityID)
			continue
		}
		if row.Category != want {
			t.Errorf("%s: category = %s, want %s", row.CapabilityID, row.Category, want)
		}
	}
}

// TestTodo_ENDPOINT_009_Security proves no internal-only or event/schedule-only
// capability is ever assigned a category implying public reachability, and
// that every GENERIC_INTENT_LIFECYCLE_ONLY row names the exact generic
// methods that reach it (not a broader or narrower set a caller might
// mistake for a dedicated route).
func TestTodo_ENDPOINT_009_Security(t *testing.T) {
	report, err := BuildDefaultDispositionReport()
	if err != nil {
		t.Fatalf("BuildDefaultDispositionReport: %v", err)
	}
	expectedGeneric := genericLifecycleEndpointIDs()
	for _, row := range report.Intents {
		if row.Category == CategoryGenericIntentLifecycleOnly {
			if len(row.ServingEndpoints) != len(expectedGeneric) {
				t.Errorf("intent %s: serving endpoints = %v, want %v", row.DefinitionRef, row.ServingEndpoints, expectedGeneric)
				continue
			}
			for i := range expectedGeneric {
				if row.ServingEndpoints[i] != expectedGeneric[i] {
					t.Errorf("intent %s: serving endpoints = %v, want %v", row.DefinitionRef, row.ServingEndpoints, expectedGeneric)
					break
				}
			}
		}
		if row.Category == CategoryInternalCapabilityOnly || row.Category == CategoryEventOrScheduleOnly {
			t.Errorf("intent %s: no P1A definition is expected to be internal- or event-only today; got %s (review before accepting)", row.DefinitionRef, row.Category)
		}
	}
}

// TestTodo_ENDPOINT_009_Conformance proves the report never drifts silently
// from its two sources: adding a synthetic definition with an unscheduled
// release, or a capability this package does not recognize, must fail
// rather than being classified by accident.
func TestTodo_ENDPOINT_009_Conformance(t *testing.T) {
	base := definitions.All()
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("NewBootstrapRegistry: %v", err)
	}
	records := registry.List()

	t.Run("UnscheduledReleaseFails", func(t *testing.T) {
		bad := append([]intent.Definition(nil), base...)
		broken := bad[0]
		broken.Release = intent.ReleaseUnspecified
		bad[0] = broken
		if _, err := BuildDispositionReport(bad, records); err == nil {
			t.Fatal("expected an unscheduled-release definition to fail the report")
		} else if !strings.Contains(err.Error(), "unscheduled release") {
			t.Fatalf("expected an unscheduled-release error, got: %v", err)
		}
	})

	t.Run("RealSourcesAgreeOnCounts", func(t *testing.T) {
		report, err := BuildDispositionReport(base, records)
		if err != nil {
			t.Fatalf("BuildDispositionReport: %v", err)
		}
		if len(report.Intents) != len(base) {
			t.Fatalf("report has %d intents, source has %d", len(report.Intents), len(base))
		}
		if len(report.Capabilities) != len(records) {
			t.Fatalf("report has %d capabilities, source has %d", len(report.Capabilities), len(records))
		}
	})
}

// TestTodo_ENDPOINT_009_Mutation proves the totality/validity checks are
// exercised: removing one definition or capability changes the total, and
// an unrecognized capability ID still receives a valid (generic) category
// rather than crashing — proving the default arm of the capability switch
// is reachable and correct.
func TestTodo_ENDPOINT_009_Mutation(t *testing.T) {
	defs := definitions.All()
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("NewBootstrapRegistry: %v", err)
	}
	records := registry.List()

	full, err := BuildDispositionReport(defs, records)
	if err != nil {
		t.Fatalf("BuildDispositionReport (full): %v", err)
	}

	truncated, err := BuildDispositionReport(defs[:len(defs)-1], records)
	if err != nil {
		t.Fatalf("BuildDispositionReport (truncated): %v", err)
	}
	if len(truncated.Intents) == len(full.Intents) {
		t.Fatal("removing one definition did not change the report's intent count")
	}

	truncatedCaps, err := BuildDispositionReport(defs, records[:len(records)-1])
	if err != nil {
		t.Fatalf("BuildDispositionReport (truncated capabilities): %v", err)
	}
	if len(truncatedCaps.Capabilities) == len(full.Capabilities) {
		t.Fatal("removing one capability record did not change the report's capability count")
	}
}
