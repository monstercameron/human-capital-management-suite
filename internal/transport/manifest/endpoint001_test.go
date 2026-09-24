package manifest

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

// fixtureDescriptor returns a syntactically valid RPCDescriptor for a
// synthetic procedure, using ScopeContext as a stand-in message so [build]
// can be exercised without the real generated services.
func fixtureDescriptor(procedure string) RPCDescriptor {
	service, method, _ := strings.Cut(strings.TrimPrefix(procedure, "/"), "/")
	msg := (&commonv1.ScopeContext{}).ProtoReflect().Descriptor()
	return RPCDescriptor{
		ServiceFullName: service,
		MethodName:      method,
		RequestType:     string(msg.FullName()),
		ResponseType:    string(msg.FullName()),
		Input:           msg,
		Output:          msg,
	}
}

func minimalValidRule() rule {
	return rule{
		owner:             "TEST_OWNER",
		behavior:          IntentBehaviorObserves,
		disposition:       DispositionServed,
		dispositionReason: "fixture",
		httpMethod:        "GET",
		httpPath:          "/v1/fixture",
		idempotencyClass:  IdempotencyReadSafe,
		capabilityRefs:    []string{"test.capability/v1"},
	}
}

// TestEndpointManifestRejectsUnownedUnboundOrHandwrittenRoute is ENDPOINT-001's
// RED/GREEN test: [build] must fail closed on a handwritten (undeclared),
// unowned or unbound route, and must fail on a stale rule row naming a
// method the descriptor set no longer has, rather than silently
// publishing a partial manifest.
func TestEndpointManifestRejectsUnownedUnboundOrHandwrittenRoute(t *testing.T) {
	const procedure = "/fixture.v1.FixtureService/DoThing"
	// realProcedure is a procedure name that already has a
	// requiredFieldPaths entry, so the two acceptance-path subtests below
	// exercise the full pipeline rather than tripping the (unrelated)
	// presence-rule lookup.
	const realProcedure = "/hcmnext.registry.v1.RegistryService/GetCapability"

	t.Run("HandwrittenRouteWithNoRuleEntryIsRejected", func(t *testing.T) {
		rpcs := []RPCDescriptor{fixtureDescriptor(procedure)}
		_, err := build(rpcs, map[string]rule{}, nil, nil)
		if err == nil {
			t.Fatal("expected build to reject a descriptor method with no reviewed rule entry")
		}
		if !strings.Contains(err.Error(), "no reviewed rule entry") {
			t.Fatalf("expected a handwritten-route error, got: %v", err)
		}
	})

	t.Run("UnownedRouteIsRejected", func(t *testing.T) {
		r := minimalValidRule()
		r.owner = ""
		rpcs := []RPCDescriptor{fixtureDescriptor(procedure)}
		_, err := build(rpcs, map[string]rule{procedure: r}, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "no owner") {
			t.Fatalf("expected an unowned-route error, got: %v", err)
		}
	})

	t.Run("UnboundRouteWithNoCapabilityIsRejected", func(t *testing.T) {
		r := minimalValidRule()
		r.capabilityRefs = nil
		rpcs := []RPCDescriptor{fixtureDescriptor(procedure)}
		_, err := build(rpcs, map[string]rule{procedure: r}, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "unbound route") {
			t.Fatalf("expected an unbound-route error, got: %v", err)
		}
	})

	t.Run("InvalidDispositionIsRejected", func(t *testing.T) {
		r := minimalValidRule()
		r.disposition = Disposition("MAYBE")
		rpcs := []RPCDescriptor{fixtureDescriptor(procedure)}
		_, err := build(rpcs, map[string]rule{procedure: r}, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "invalid disposition") {
			t.Fatalf("expected an invalid-disposition error, got: %v", err)
		}
	})

	t.Run("StaleRuleRowNamingAVanishedMethodIsRejected", func(t *testing.T) {
		rpcs := []RPCDescriptor{fixtureDescriptor(realProcedure)}
		ruleTable := map[string]rule{
			realProcedure: minimalValidRule(),
			"/fixture.v1.FixtureService/RetiredMethod": minimalValidRule(),
		}
		_, err := build(rpcs, ruleTable, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "no longer exists") {
			t.Fatalf("expected a stale-manifest-row error, got: %v", err)
		}
	})

	t.Run("OwnedBoundReviewedRouteIsAccepted", func(t *testing.T) {
		rpcs := []RPCDescriptor{fixtureDescriptor(realProcedure)}
		m, err := build(rpcs, map[string]rule{realProcedure: minimalValidRule()}, nil, nil)
		if err != nil {
			t.Fatalf("expected a fully reviewed route to build cleanly, got: %v", err)
		}
		if len(m.Endpoints) != 1 || m.Endpoints[0].OwnerDomain != "TEST_OWNER" {
			t.Fatalf("unexpected manifest: %+v", m.Endpoints)
		}
	})

	t.Run("ProductionManifestBuildsCleanly", func(t *testing.T) {
		if _, err := Build(); err != nil {
			t.Fatalf("Build() against the real descriptor/capability/definition sources failed: %v", err)
		}
	})
}

// TestTodo_ENDPOINT_001_Property proves determinism: two independent Build
// calls produce byte-identical canonical JSON and an identical digest, and
// a deep-equal manifest value, regardless of internal map-iteration order.
func TestTodo_ENDPOINT_001_Property(t *testing.T) {
	m1, err := Build()
	if err != nil {
		t.Fatalf("Build (first): %v", err)
	}
	m2, err := Build()
	if err != nil {
		t.Fatalf("Build (second): %v", err)
	}
	if !reflect.DeepEqual(m1, m2) {
		t.Fatal("two Build() calls produced different manifests")
	}

	j1, err := m1.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON (first): %v", err)
	}
	j2, err := m2.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON (second): %v", err)
	}
	if string(j1) != string(j2) {
		t.Fatal("two Build() calls produced different canonical JSON byte-for-byte")
	}

	d1, err := m1.Digest()
	if err != nil {
		t.Fatalf("Digest (first): %v", err)
	}
	d2, err := m2.Digest()
	if err != nil {
		t.Fatalf("Digest (second): %v", err)
	}
	if d1 != d2 {
		t.Fatalf("digest differs across identical builds: %s vs %s", d1, d2)
	}

	// Endpoints must already be sorted ascending by EndpointID: a caller
	// diffing two manifests should never see spurious reordering.
	if !sort.SliceIsSorted(m1.Endpoints, func(i, j int) bool { return m1.Endpoints[i].EndpointID < m1.Endpoints[j].EndpointID }) {
		t.Fatal("manifest endpoints are not sorted by EndpointID")
	}
}

// TestTodo_ENDPOINT_001_Golden pins the exact eighteen-row manifest this
// change generates against a fixture transcribed from
// planning/specs/http-grpc-endpoint-contract.md's initial endpoint
// inventory and internal/transport/validate.go's requiredFields table (read
// 2026-09-03, HEAD 94610e8). Any row's disposition, HTTP binding or
// required-field-path set changing without a matching change to this golden
// fixture fails here first.
func TestTodo_ENDPOINT_001_Golden(t *testing.T) {
	type want struct {
		disposition Disposition
		httpMethod  string
		httpPath    string
		required    []string
	}
	golden := map[string]want{
		"hcmnext.intents.v1.IntentService/CreateIntent":          {DispositionServed, "POST", "/v1/intents", []string{"definition.intent_type_id", "idempotency_key", "request.schema.schema_id"}},
		"hcmnext.intents.v1.IntentService/GetIntent":             {DispositionServed, "GET", "/v1/intents/{intent}", []string{"intent_id"}},
		"hcmnext.intents.v1.IntentService/ListIntents":           {DispositionServed, "GET", "/v1/intents", []string{}},
		"hcmnext.intents.v1.IntentService/SimulateIntent":        {DispositionServed, "POST", "/v1/intents/{intent}:simulate", []string{"intent_id"}},
		"hcmnext.intents.v1.IntentService/ExecuteIntent":         {DispositionRefusedP1A, "POST", "/v1/intents/{intent}:execute", []string{"idempotency_key", "intent_id", "approval.proposal_revision_id", "approval.approval_ref"}},
		"hcmnext.intents.v1.IntentService/SubmitIntent":          {DispositionServed, "POST", "/v1/intents/{intent}:submit", []string{"idempotency_key", "intent_id", "proposal_revision_id"}},
		"hcmnext.intents.v1.IntentService/CancelIntent":          {DispositionServed, "POST", "/v1/intents/{intent}:cancel", []string{"idempotency_key", "intent_id", "reason_ref"}},
		"hcmnext.intents.v1.IntentService/SupersedeIntent":       {DispositionServed, "POST", "/v1/intents/{intent}:supersede", []string{"definition.intent_type_id", "idempotency_key", "reason_ref", "superseded_intent_id"}},
		"hcmnext.intents.v1.IntentService/ExplainIntent":         {DispositionServed, "GET", "/v1/intents/{intent}/explanation", []string{"intent_id"}},
		"hcmnext.intents.v1.IntentService/ListIntentTimeline":    {DispositionServed, "GET", "/v1/intents/{intent}/timeline", []string{"intent_id"}},
		"hcmnext.intents.v1.IntentService/RecommendIntentAction": {DispositionServed, "POST", "/hcmnext.intents.v1.IntentService/RecommendIntentAction", []string{"action.capability_ref", "analysis", "governance", "organization_id", "population", "purpose", "simulation", "tenant_id"}},
		"hcmnext.intents.v1.IntentService/GetIntentDeepLink":     {DispositionServed, "POST", "/hcmnext.intents.v1.IntentService/GetIntentDeepLink", []string{"intent_id"}},
		"hcmnext.intents.v1.IntentService/InspectIntentFields":   {DispositionServed, "POST", "/hcmnext.intents.v1.IntentService/InspectIntentFields", []string{"intent_id"}},
		"hcmnext.intents.v1.IntentService/ExportIntentFields":    {DispositionServed, "POST", "/hcmnext.intents.v1.IntentService/ExportIntentFields", []string{"intent_id", "purpose"}},

		"hcmnext.registry.v1.RegistryService/ListIntentDefinitions": {DispositionServed, "GET", "/v1/intent-definitions", []string{}},
		"hcmnext.registry.v1.RegistryService/GetIntentDefinition":   {DispositionServed, "GET", "/v1/intent-definitions/{intent_definition}", []string{"definition.intent_type_id"}},
		"hcmnext.registry.v1.RegistryService/ListCapabilities":      {DispositionServed, "GET", "/v1/capabilities", []string{}},
		"hcmnext.registry.v1.RegistryService/GetCapability":         {DispositionServed, "GET", "/v1/capabilities/{capability}", []string{"capability_id"}},
	}

	m, err := Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(m.Endpoints) != len(golden) {
		t.Fatalf("expected %d endpoints, got %d", len(golden), len(m.Endpoints))
	}
	for _, e := range m.Endpoints {
		w, ok := golden[e.EndpointID]
		if !ok {
			t.Errorf("unexpected endpoint in manifest: %s", e.EndpointID)
			continue
		}
		if e.Disposition != w.disposition {
			t.Errorf("%s: disposition = %s, want %s", e.EndpointID, e.Disposition, w.disposition)
		}
		if e.HTTPMethod != w.httpMethod || e.HTTPPathTemplate != w.httpPath {
			t.Errorf("%s: http = %s %s, want %s %s", e.EndpointID, e.HTTPMethod, e.HTTPPathTemplate, w.httpMethod, w.httpPath)
		}
		gotRequired := append([]string(nil), e.RequiredFieldPaths...)
		sort.Strings(gotRequired)
		wantRequired := append([]string(nil), w.required...)
		sort.Strings(wantRequired)
		if !reflect.DeepEqual(gotRequired, wantRequired) {
			t.Errorf("%s: required field paths = %v, want %v (diff against internal/transport/validate.go's requiredFields)", e.EndpointID, gotRequired, wantRequired)
		}
	}
}

// TestTodo_ENDPOINT_001_Security proves the manifest never lets a P1A-refused,
// write-shaped method present itself as servable, and that every served
// method carries a non-empty authentication/authorization binding — a
// caller can never mistake "exists on the wire" for "callable today".
func TestTodo_ENDPOINT_001_Security(t *testing.T) {
	m, err := Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Write-shaped methods split into two sets, and the point of this test is
	// that a caller can tell which is which by reading the manifest alone.
	// ExecuteIntent is still refused for the duration of P1A. The three
	// intent-lifecycle writes were served by EP-INTENT-003, and a manifest
	// that kept calling them refused would send a working endpoint's callers
	// away — the failure mode this assertion now also covers.
	refusedWrites := map[string]bool{
		"hcmnext.intents.v1.IntentService/ExecuteIntent": true,
	}
	servedWrites := map[string]bool{
		"hcmnext.intents.v1.IntentService/SubmitIntent":    true,
		"hcmnext.intents.v1.IntentService/CancelIntent":    true,
		"hcmnext.intents.v1.IntentService/SupersedeIntent": true,
	}
	seenServedWrites := 0
	for _, e := range m.Endpoints {
		if refusedWrites[e.EndpointID] {
			if e.Disposition != DispositionRefusedP1A {
				t.Errorf("%s: write-shaped method must be REFUSED_P1A, got %s", e.EndpointID, e.Disposition)
			}
			if len(e.AcceptedIntentDefinitionRefs) != 0 {
				t.Errorf("%s: a refused method must accept zero intent definitions, got %v", e.EndpointID, e.AcceptedIntentDefinitionRefs)
			}
			if e.DispositionReason == "" || !strings.Contains(e.DispositionReason, "P1A") {
				t.Errorf("%s: expected a P1A-citing disposition reason, got %q", e.EndpointID, e.DispositionReason)
			}
		}
		if servedWrites[e.EndpointID] {
			seenServedWrites++
			if e.Disposition != DispositionServed {
				t.Errorf("%s: this write is implemented and must be SERVED, got %s", e.EndpointID, e.Disposition)
			}
			if len(e.AcceptedIntentDefinitionRefs) == 0 {
				t.Errorf("%s: a served lifecycle write must publish the definitions it accepts, got none", e.EndpointID)
			}
			if strings.Contains(e.DispositionReason, "refused") {
				t.Errorf("%s: a served method must not carry a refusal reason, got %q", e.EndpointID, e.DispositionReason)
			}
			if e.IdempotencyClass != IdempotencyKey {
				t.Errorf("%s: a served governed write must be idempotency-key deduped, got %s", e.EndpointID, e.IdempotencyClass)
			}
		}
		if e.AuthnAssurance == "" {
			t.Errorf("%s: has no authentication assurance floor (anonymous route)", e.EndpointID)
		}
		if e.AuthzAction == "" {
			t.Errorf("%s: has no authorization action", e.EndpointID)
		}
		if len(e.CapabilityRefs) == 0 {
			t.Errorf("%s: has no capability binding", e.EndpointID)
		}
	}
	// Without this the served-write assertions above would pass vacuously if a
	// method were renamed out of the manifest.
	if seenServedWrites != len(servedWrites) {
		t.Errorf("saw %d served governed writes in the manifest, want %d", seenServedWrites, len(servedWrites))
	}
}

// TestTodo_ENDPOINT_001_Conformance proves the manifest is set-equal to the
// live descriptor's method set: nothing published in the .proto is missing
// from the manifest and nothing in the manifest lacks a real method.
func TestTodo_ENDPOINT_001_Conformance(t *testing.T) {
	rpcs, err := DiscoverRPCs()
	if err != nil {
		t.Fatalf("DiscoverRPCs: %v", err)
	}
	m, err := Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	descriptorIDs := map[string]bool{}
	for _, d := range rpcs {
		descriptorIDs[d.EndpointID()] = true
	}
	manifestIDs := map[string]bool{}
	for _, e := range m.Endpoints {
		manifestIDs[e.EndpointID] = true
	}
	if len(descriptorIDs) != len(manifestIDs) {
		t.Fatalf("descriptor has %d methods, manifest has %d", len(descriptorIDs), len(manifestIDs))
	}
	for id := range descriptorIDs {
		if !manifestIDs[id] {
			t.Errorf("descriptor method %s is missing from the manifest", id)
		}
	}
	for id := range manifestIDs {
		if !descriptorIDs[id] {
			t.Errorf("manifest names %s, which the descriptor does not declare", id)
		}
	}
}

// TestTodo_ENDPOINT_001_Mutation proves the ownership/binding/disposition
// checks in [build] are actually exercised: perturbing one field of one
// otherwise-valid rule must change the outcome, so the checks cannot be
// silently vacuous.
func TestTodo_ENDPOINT_001_Mutation(t *testing.T) {
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("NewBootstrapRegistry: %v", err)
	}
	rpcs, err := DiscoverRPCs()
	if err != nil {
		t.Fatalf("DiscoverRPCs: %v", err)
	}

	base := rules()
	if _, err := build(rpcs, base, registry.List(), nil); err != nil {
		t.Fatalf("baseline rule table failed to build: %v", err)
	}

	mutate := func(name string, f func(map[string]rule)) {
		t.Run(name, func(t *testing.T) {
			mutated := make(map[string]rule, len(base))
			for k, v := range base {
				mutated[k] = v
			}
			f(mutated)
			if _, err := build(rpcs, mutated, registry.List(), nil); err == nil {
				t.Fatal("expected the mutated rule table to fail to build")
			}
		})
	}

	const createIntent = "/hcmnext.intents.v1.IntentService/CreateIntent"
	mutate("ClearOwner", func(m map[string]rule) {
		r := m[createIntent]
		r.owner = ""
		m[createIntent] = r
	})
	mutate("InvalidBehavior", func(m map[string]rule) {
		r := m[createIntent]
		r.behavior = IntentBehavior("SOMETHING_ELSE")
		m[createIntent] = r
	})
	mutate("InvalidIdempotencyClass", func(m map[string]rule) {
		r := m[createIntent]
		r.idempotencyClass = IdempotencyClass("MAYBE")
		m[createIntent] = r
	})
	mutate("DeleteRuleEntirely", func(m map[string]rule) {
		delete(m, createIntent)
	})

	const getCapability = "/hcmnext.registry.v1.RegistryService/GetCapability"
	mutate("ClearNonGenericCapabilityRefs", func(m map[string]rule) {
		r := m[getCapability]
		r.capabilityRefs = nil
		m[getCapability] = r
	})
}
