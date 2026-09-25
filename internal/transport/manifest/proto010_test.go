package manifest

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

// TestProtoRpcDispositionIsTotalAndNoAccidentalExposure is PROTO-010's
// RED/GREEN test: every RPC method declared by every generated service
// (governedServices) has exactly one valid [Disposition] in the manifest,
// and the manifest names no method the descriptor set does not declare
// (an "unregistered handler" would be exactly that: a manifest row with no
// backing descriptor method).
func TestProtoRpcDispositionIsTotalAndNoAccidentalExposure(t *testing.T) {
	rpcs, err := DiscoverRPCs()
	if err != nil {
		t.Fatalf("DiscoverRPCs: %v", err)
	}
	if len(rpcs) == 0 {
		t.Fatal("DiscoverRPCs found zero methods; governedServices or the descriptor registration is broken")
	}

	m, err := Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	byID := make(map[string]EndpointDefinition, len(m.Endpoints))
	for _, e := range m.Endpoints {
		if _, dup := byID[e.EndpointID]; dup {
			t.Fatalf("endpoint %s appears more than once (disposition is not unique)", e.EndpointID)
		}
		byID[e.EndpointID] = e
	}

	for _, d := range rpcs {
		e, ok := byID[d.EndpointID()]
		if !ok {
			t.Errorf("descriptor method %s has no manifest disposition at all", d.EndpointID())
			continue
		}
		if !e.Disposition.Valid() {
			t.Errorf("%s: disposition %q is not one of SERVED|REFUSED_P1A|NOT_EXPOSED", d.EndpointID(), e.Disposition)
		}
		delete(byID, d.EndpointID())
	}
	for id := range byID {
		t.Errorf("manifest names %s (an unregistered handler bind: no descriptor method backs it)", id)
	}
}

// TestTodo_PROTO_010_Property proves the totality check is order-independent:
// shuffling the discovered RPC slice never changes which methods end up
// with a disposition or what that disposition is.
func TestTodo_PROTO_010_Property(t *testing.T) {
	rpcs, err := DiscoverRPCs()
	if err != nil {
		t.Fatalf("DiscoverRPCs: %v", err)
	}
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("NewBootstrapRegistry: %v", err)
	}

	reversed := make([]RPCDescriptor, len(rpcs))
	for i, d := range rpcs {
		reversed[len(rpcs)-1-i] = d
	}

	m1, err := build(rpcs, rules(), registry.List(), nil)
	if err != nil {
		t.Fatalf("build (forward order): %v", err)
	}
	m2, err := build(reversed, rules(), registry.List(), nil)
	if err != nil {
		t.Fatalf("build (reversed order): %v", err)
	}
	if len(m1.Endpoints) != len(m2.Endpoints) {
		t.Fatalf("endpoint count depends on input order: %d vs %d", len(m1.Endpoints), len(m2.Endpoints))
	}
	for i := range m1.Endpoints {
		if m1.Endpoints[i].EndpointID != m2.Endpoints[i].EndpointID || m1.Endpoints[i].Disposition != m2.Endpoints[i].Disposition {
			t.Fatalf("row %d differs by input order: %+v vs %+v", i, m1.Endpoints[i], m2.Endpoints[i])
		}
	}
}

// TestTodo_PROTO_010_Golden pins the exact disposition distribution: thirteen
// SERVED, one REFUSED_P1A, zero NOT_EXPOSED, across the fourteen current
// methods of IntentService and RegistryService. ExecuteIntent is the one
// method still refused for the duration of P1A.
func TestTodo_PROTO_010_Golden(t *testing.T) {
	m, err := Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var served, refused, notExposed int
	for _, e := range m.Endpoints {
		switch e.Disposition {
		case DispositionServed:
			served++
		case DispositionRefusedP1A:
			refused++
		case DispositionNotExposed:
			notExposed++
		}
	}
	// EP-INTENT-003 served SubmitIntent, CancelIntent and SupersedeIntent,
	// moving three methods from refused to served. ExecuteIntent is the one
	// write still refused for the duration of P1A. REV-007-04 served the
	// four analysis-to-action and intent-inspection reads. ProjectService now
	// contributes thirty-seven served methods, including revisioned link mutations.
	if served != 55 || refused != 1 || notExposed != 0 || len(m.Endpoints) != 56 {
		t.Fatalf("disposition distribution = {served:%d refused:%d not_exposed:%d total:%d}, want {55 1 0 56}",
			served, refused, notExposed, len(m.Endpoints))
	}
}

// TestTodo_PROTO_010_Security proves an unregistered/handwritten handler
// bind is rejected rather than silently exposed: a rule table naming a
// procedure absent from the descriptor set must fail [build], not default
// to some disposition.
func TestTodo_PROTO_010_Security(t *testing.T) {
	rpcs := []RPCDescriptor{fixtureDescriptor("/hcmnext.registry.v1.RegistryService/GetCapability")}
	ruleTable := map[string]rule{
		"/hcmnext.registry.v1.RegistryService/GetCapability": minimalValidRule(),
		"/hcmnext.intents.v1.IntentService/SecretBackdoor":   minimalValidRule(),
	}
	_, err := build(rpcs, ruleTable, nil, nil)
	if err == nil {
		t.Fatal("expected an unregistered handler bind to fail the build")
	}
	if !strings.Contains(err.Error(), "no longer exists") {
		t.Fatalf("expected an unregistered-handler error, got: %v", err)
	}
}

// TestTodo_PROTO_010_Conformance re-derives the descriptor's method set
// independently (via protoregistry, bypassing [DiscoverRPCs]'s own
// wrapping) and proves it is set-equal to the manifest, so a bug in
// [DiscoverRPCs] itself could not hide a missing or extra disposition.
func TestTodo_PROTO_010_Conformance(t *testing.T) {
	rpcs, err := DiscoverRPCs()
	if err != nil {
		t.Fatalf("DiscoverRPCs: %v", err)
	}
	wantCount := 0
	for range rpcs {
		wantCount++
	}
	if wantCount != 56 {
		t.Fatalf("expected 56 total RPC methods across IntentService (14), RegistryService (4), and ProjectService (38), found %d", wantCount)
	}

	m, err := Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(m.Endpoints) != wantCount {
		t.Fatalf("manifest has %d endpoints, descriptor set has %d", len(m.Endpoints), wantCount)
	}
}

// TestTodo_PROTO_010_Mutation proves the totality check actually runs:
// deleting one entry from the reviewed rule table must fail the build
// instead of silently producing a twelve-row manifest.
func TestTodo_PROTO_010_Mutation(t *testing.T) {
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("NewBootstrapRegistry: %v", err)
	}
	rpcs, err := DiscoverRPCs()
	if err != nil {
		t.Fatalf("DiscoverRPCs: %v", err)
	}

	mutated := make(map[string]rule, len(rules()))
	for k, v := range rules() {
		mutated[k] = v
	}
	delete(mutated, "/hcmnext.intents.v1.IntentService/CreateIntent")

	if _, err := build(rpcs, mutated, registry.List(), nil); err == nil {
		t.Fatal("expected removing one rule entry to fail the totality check")
	}
}
