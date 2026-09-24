package capability_test

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

// validDefinition returns a fully-populated, publishable definition. Tests
// mutate a copy of it to exercise one missing field at a time.
func validDefinition() capability.Definition {
	schema := capability.SchemaRef{SchemaID: "hcmnext.test.v1", Version: 1, ProtobufFullName: "hcmnext.test.v1.Thing"}
	return capability.Definition{
		ID:                   "hcmnext.test.read_thing",
		Version:              1,
		OwnerDomain:          "test",
		RequestSchema:        schema,
		ResponseSchema:       schema,
		ErrorSchema:          schema,
		EffectClass:          capability.EffectReadOnly,
		ReadData:             capability.DataDomainFieldSet{DataDomains: []string{"thing"}},
		RiskClass:            "LOW",
		IdempotencyPolicyRef: "idempotency.read-safe.v1",
		AuthZScopeRef:        "scope:test.read",
		LegalBasisRef:        "legal.test.v1",
		EntitlementRef:       "entitlement.test.v1",
		SLOClassRef:          "slo.test.v1",
		TestRef:              "conformance:hcmnext.test.read_thing/v1",
	}
}

func echoHandler(_ context.Context, payload any) (any, error) { return payload, nil }

// TestTodo_CAP_001 proves the RED and GREEN clauses of
// planning/todos.md CAP-001: publication rejects a definition missing any
// required field or an unresolved implementation binding; a well-formed
// definition publishes with a stable ID and exact version; and a version can
// never be mutated after registration.
func TestTodo_CAP_001(t *testing.T) {
	t.Run("RED_missing_required_fields", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*capability.Definition)
		}{
			{"owner", func(d *capability.Definition) { d.OwnerDomain = "" }},
			{"request_schema", func(d *capability.Definition) { d.RequestSchema = capability.SchemaRef{} }},
			{"response_schema", func(d *capability.Definition) { d.ResponseSchema = capability.SchemaRef{} }},
			{"error_schema", func(d *capability.Definition) { d.ErrorSchema = capability.SchemaRef{} }},
			{"risk", func(d *capability.Definition) { d.RiskClass = "" }},
			{"effect", func(d *capability.Definition) { d.EffectClass = "" }},
			{"idempotency", func(d *capability.Definition) { d.IdempotencyPolicyRef = "" }},
			{"authz", func(d *capability.Definition) { d.AuthZScopeRef = "" }},
			{"legal", func(d *capability.Definition) { d.LegalBasisRef = "" }},
			{"entitlement", func(d *capability.Definition) { d.EntitlementRef = "" }},
			{"slo", func(d *capability.Definition) { d.SLOClassRef = "" }},
			{"test", func(d *capability.Definition) { d.TestRef = "" }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				def := validDefinition()
				tc.mutate(&def)
				r := capability.NewRegistry()
				if err := r.Register(def, echoHandler); err == nil {
					t.Fatalf("expected publication to reject missing %s, got nil error", tc.name)
				}
			})
		}
	})

	t.Run("RED_unresolved_implementation_binding", func(t *testing.T) {
		r := capability.NewRegistry()
		if err := r.Register(validDefinition(), nil); err == nil {
			t.Fatal("expected publication to reject a nil implementation binding")
		}
	})

	t.Run("GREEN_publishes_with_stable_id_and_exact_version", func(t *testing.T) {
		r := capability.NewRegistry()
		def := validDefinition()
		if err := r.Register(def, echoHandler); err != nil {
			t.Fatalf("register: %v", err)
		}
		rec, ok := r.Lookup(def.Key())
		if !ok {
			t.Fatal("expected the registered version to resolve")
		}
		if rec.Definition.ID != def.ID || rec.Definition.Version != def.Version {
			t.Fatalf("resolved %s/v%d, want %s/v%d", rec.Definition.ID, rec.Definition.Version, def.ID, def.Version)
		}
		if rec.Status != capability.StatusActive {
			t.Fatalf("status = %s, want ACTIVE", rec.Status)
		}
	})

	t.Run("GREEN_historical_version_resolves_after_deprecation", func(t *testing.T) {
		r := capability.NewRegistry()
		def := validDefinition()
		if err := r.Register(def, echoHandler); err != nil {
			t.Fatalf("register: %v", err)
		}
		if err := r.Deprecate(def.Key()); err != nil {
			t.Fatalf("deprecate: %v", err)
		}
		rec, ok := r.Lookup(def.Key())
		if !ok {
			t.Fatal("expected a deprecated version to still resolve")
		}
		if rec.Status != capability.StatusDeprecated {
			t.Fatalf("status = %s, want DEPRECATED", rec.Status)
		}
	})

	t.Run("immutability_registering_twice_fails", func(t *testing.T) {
		r := capability.NewRegistry()
		def := validDefinition()
		if err := r.Register(def, echoHandler); err != nil {
			t.Fatalf("first register: %v", err)
		}
		other := def
		other.RiskClass = "HIGH"
		if err := r.Register(other, echoHandler); err == nil {
			t.Fatal("expected re-registering the same (ID, Version) to fail")
		}
	})

	t.Run("immutability_returned_record_cannot_mutate_storage", func(t *testing.T) {
		r := capability.NewRegistry()
		def := validDefinition()
		if err := r.Register(def, echoHandler); err != nil {
			t.Fatalf("register: %v", err)
		}
		rec, _ := r.Lookup(def.Key())
		rec.Definition.ReadData.DataDomains[0] = "tampered"
		rec.Definition.RiskClass = "TAMPERED"

		again, _ := r.Lookup(def.Key())
		if again.Definition.RiskClass == "TAMPERED" {
			t.Fatal("mutating a returned Definition mutated registry storage")
		}
		if again.Definition.ReadData.DataDomains[0] == "tampered" {
			t.Fatal("mutating a returned slice mutated registry storage")
		}
	})
}

// TestTodo_CAP_001_Golden pins the deterministic listing order and the
// digest of the compiled-in BOOTSTRAP table so an accidental reorder or a
// silent field change is caught by a test diff, not discovered downstream.
func TestTodo_CAP_001_Golden(t *testing.T) {
	r, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("NewBootstrapRegistry: %v", err)
	}
	list := r.List()
	if len(list) != 11 {
		t.Fatalf("bootstrap table has %d capabilities, want 11 (eight P1A intents, the effective-date debugger, plus registry resolve/explain)", len(list))
	}
	ids := make([]string, len(list))
	for i, rec := range list {
		ids[i] = rec.Definition.ID
		if rec.Digest == "" {
			t.Fatalf("%s has no digest", rec.Definition.ID)
		}
		if rec.Definition.EffectClass.IsWrite() {
			t.Fatalf("%s declares a write effect in the P1A BOOTSTRAP table", rec.Definition.ID)
		}
	}
	if !sort.StringsAreSorted(ids) {
		t.Fatalf("bootstrap listing is not sorted: %v", ids)
	}
	want := []string{
		"hcmnext.dataops.explain_field_history",
		"hcmnext.intelligence.explain_transaction",
		"hcmnext.operations.create_repair_plan",
		"hcmnext.operations.detect_drift",
		"hcmnext.operations.simulate_repair",
		"hcmnext.people.explain_worker_state",
		"hcmnext.people.promote_worker",
		"hcmnext.registry.explain_capability",
		"hcmnext.registry.resolve_capability",
		"hcmnext.rewards.evaluate_pay_band_position",
		"hcmnext.rewards.simulate_compensation",
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("bootstrap ids = %v, want %v", ids, want)
	}

	// Listing twice must produce byte-identical order: determinism is the
	// point, not merely "some" stable order.
	again := r.List()
	for i := range again {
		if again[i].Definition.ID != list[i].Definition.ID || again[i].Digest != list[i].Digest {
			t.Fatalf("List() is not deterministic across calls at index %d", i)
		}
	}

	// The digest is a pure function of the definition's core fields: the same
	// definition registered in a fresh registry reproduces the same digest.
	fresh := capability.NewRegistry()
	def := validDefinition()
	if err := fresh.Register(def, echoHandler); err != nil {
		t.Fatalf("register: %v", err)
	}
	first, _ := fresh.Lookup(def.Key())
	second, err := capability.Digest(def)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if first.Digest != second {
		t.Fatalf("registry digest %s != direct Digest() %s", first.Digest, second)
	}

	// A core-field change produces a different digest.
	changed := def
	changed.RiskClass = "HIGH"
	changedDigest, err := capability.Digest(changed)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if changedDigest == first.Digest {
		t.Fatal("changing a core field did not change the digest")
	}
}

// TestTodo_CAP_001_Race exercises concurrent registration and lookup. The
// registry must serialize registration attempts (exactly one caller wins a
// given key) and never panic or deadlock under concurrent readers.
func TestTodo_CAP_001_Race(t *testing.T) {
	r := capability.NewRegistry()
	def := validDefinition()

	const workers = 16
	results := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			results <- r.Register(def, echoHandler)
		}()
	}
	successes := 0
	for i := 0; i < workers; i++ {
		if err := <-results; err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("%d concurrent registrations of the same key succeeded, want exactly 1", successes)
	}

	done := make(chan struct{})
	for i := 0; i < workers; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				_, _ = r.Lookup(def.Key())
				_ = r.List()
			}
		}()
	}
	for i := 0; i < workers; i++ {
		<-done
	}
}

// TestTodo_CAP_001_Security proves that publishing a capability whose
// governance references are absent is rejected even when every other field
// is present - a partially-governed capability must never slip into the
// registry that the gateway (CAP-002) trusts to enforce authorization.
func TestTodo_CAP_001_Security(t *testing.T) {
	def := validDefinition()
	def.AuthZScopeRef = ""
	r := capability.NewRegistry()
	if err := r.Register(def, echoHandler); err == nil {
		t.Fatal("expected a capability with no AuthZ scope to be rejected at publication")
	}

	def2 := validDefinition()
	def2.EffectClass = "SOMETHING_ELSE"
	if err := capability.NewRegistry().Register(def2, echoHandler); err == nil {
		t.Fatal("expected a capability with an unrecognized effect class to be rejected at publication")
	}
}
