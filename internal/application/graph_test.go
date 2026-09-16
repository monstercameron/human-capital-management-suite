package application

import (
	"strings"
	"testing"
)

// TestGraphCanonicalIsDeterministicAndOrderIndependent proves the digest
// describes the composition rather than the order the composition happened to
// record it in.
func TestGraphCanonicalIsDeterministicAndOrderIndependent(t *testing.T) {
	forward := newGraphBuilder(RoleServe)
	forward.add("a", KindPort, nil)
	forward.add("b", KindAdapter, "value", "a")
	reverse := newGraphBuilder(RoleServe)
	reverse.add("b", KindAdapter, "value", "a")
	reverse.add("a", KindPort, nil)

	if forward.graph().Canonical() != reverse.graph().Canonical() {
		t.Errorf("canonical form depends on recording order:\n%s\nvs\n%s",
			forward.graph().Canonical(), reverse.graph().Canonical())
	}
	if forward.graph().Digest() != reverse.graph().Digest() {
		t.Error("digest depends on recording order")
	}

	multi := newGraphBuilder(RoleServe)
	multi.add("a", KindPort, nil)
	multi.add("z", KindPort, nil)
	multi.add("b", KindAdapter, "value", "z", "a")
	if got := multi.graph().Canonical(); !strings.Contains(got, "b|adapter|string|a,z") {
		t.Errorf("dependencies were not sorted:\n%s", got)
	}
	if names := multi.graph().Names(); len(names) != 3 || names[0] != "a" || names[2] != "z" {
		t.Errorf("Names() = %v, want the sorted component names", names)
	}
	if _, ok := multi.graph().Component("b"); !ok {
		t.Error("Component(\"b\") reported absent")
	}
	if _, ok := multi.graph().Component("absent"); ok {
		t.Error("Component(\"absent\") reported present")
	}
}

// TestGraphRecordsAbsenceAsAFact is the assertion behind "<nil>": telemetry
// that is off and an executor that is absent are things the composition
// decided, and a digest that ignored them would call a P1A cell and a P1B
// cell the same composition.
func TestGraphRecordsAbsenceAsAFact(t *testing.T) {
	var nilPointer *Component
	var nilInterface any
	var nilFunc func() error
	for _, value := range []any{nil, nilPointer, nilInterface, nilFunc, map[string]string(nil), []string(nil)} {
		if got := implName(value); got != "<nil>" {
			t.Errorf("implName(%T) = %q, want <nil>", value, got)
		}
	}
	if got := implName(func(int) error { return nil }); got != "func(int) error" {
		t.Errorf("implName of a function = %q, want its signature, which is the only stable part", got)
	}
	if got := implName(ServeConfig{}); got != "application.ServeConfig" {
		t.Errorf("implName(ServeConfig{}) = %q", got)
	}

	present := newGraphBuilder(RoleServe)
	present.add("x", KindPort, "value")
	absent := newGraphBuilder(RoleServe)
	absent.add("x", KindPort, nil)
	if present.graph().Digest() == absent.graph().Digest() {
		t.Error("a present adapter and an absent one digest the same")
	}
}

// TestGraphValidateRejectsAnIncoherentComposition guards the description
// itself: a composition root that can describe itself incoherently cannot be
// trusted to describe itself at all.
func TestGraphValidateRejectsAnIncoherentComposition(t *testing.T) {
	cases := []struct {
		name    string
		graph   Graph
		wantSub string
	}{
		{"unnamed", Graph{Components: []Component{{Kind: KindPort}}}, "unnamed"},
		{"duplicate", Graph{Components: []Component{
			{Name: "a", Kind: KindPort}, {Name: "a", Kind: KindPort},
		}}, "twice"},
		{"unknown kind", Graph{Components: []Component{{Name: "a", Kind: "invented"}}}, "unknown kind"},
		{"dangling dependency", Graph{Components: []Component{
			{Name: "a", Kind: KindPort, DependsOn: []string{"missing"}},
		}}, "absent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.graph.Validate()
			if err == nil {
				t.Fatalf("Validate accepted %+v", tc.graph)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("Validate error = %q, want it to say %q", err, tc.wantSub)
			}
		})
	}
	ok := Graph{Role: RoleServe, Components: []Component{
		{Name: "a", Kind: KindPort},
		{Name: "b", Kind: KindAdapter, DependsOn: []string{"a"}},
	}}
	if err := ok.Validate(); err != nil {
		t.Errorf("Validate on a coherent graph: %v", err)
	}
}

// serveGoldenCanonical is the composed serve-role dependency graph, with
// persistence and admission swapped for stubs so the text depends on the
// composition rather than on a database.
//
// It is written out in full rather than digested to a constant because the
// point of the golden is to be read: a diff here is the review, and a change
// nobody meant to make (a domain input that quietly became a different
// corpus, a transport that grew a dependency, an execution component that
// stopped being absent by default) shows up as a line.
const serveGoldenCanonical = `role=serve
capability-gateway|governance|*capability.Gateway|capability-registry
capability-registry|registry|*capability.Registry|cell
cell|registry|*app.Cell|credential-verifier,evidence-sink,intent-store,legal-evidence-verifier,pay-band-catalog,proposal-executor,telemetry-provider
config|config|application.ServeConfig|
credential-verifier|adapter|application.stubVerifier|config
database-pool|adapter|<nil>|
discovery-document|registry|*manifest.DiscoveryDocument|cell
domain-inputs|port|*app.FixtureInputs|
evidence-sink|registry|*evidencestore.Store|
execution-authority|governance|<nil>|config,database-pool,evidence-sink
grpc-surface|transport|*grpc.Server|cell,workflow-instance-reader
http-edge|transport|http.HandlerFunc|cell,grpc-surface
incumbent-connector|adapter|*fakeincumbent.Incumbent|
intent-definitions|registry|*intent.Registry|cell
intent-service|engine|*app.IntentService|cell
intent-store|adapter|*application.stubStore|config,database-pool
journey-engine|workflow|<nil>|cell
legal-evidence-verifier|adapter|<nil>|config,database-pool
observation-store|adapter|*observe.MemoryStore|
pay-band-catalog|port|<nil>|
presentation-preferences|adapter|*preferencestore.Store|database-pool
proposal-executor|workflow|<nil>|execution-authority
role-access|adapter|*roleaccessstore.Store|database-pool
schema-migrator|adapter|<nil>|
shutdown:shutdown-telemetry|shutdown|func(context.Context) error|telemetry-provider
shutdown:stop-grpc-surface|shutdown|func(context.Context) error|grpc-surface
shutdown:stop-http-edge|shutdown|func(context.Context) error|http-edge
telemetry-provider|adapter|<nil>|config
transaction-history|port|*app.ledgerTransactions|
trusted-clock|engine|*timeauth.Monitor|
work-item-queue-reader|port|app.workItemQueueReader|database-pool
worker-facts|port|*fixtures.MemoryWorkerFacts|
workflow-instance-reader|port|app.workflowInstanceReader|database-pool
workflow-resolver|workflow|<nil>|execution-authority
workflow-versions|workflow|<nil>|execution-authority
workload:grpc-surface|workload|func(context.Context) error|grpc-surface
workload:http-edge|workload|func(context.Context) error|http-edge
`

// TestTodo_ARCH_GO_020_Golden is the ARCH-GO-020 GOLDEN: the composed
// dependency graph as a deterministic digest.
//
// Two things are asserted. The graph is coherent and matches the reviewed
// text above, so any change to what the serve role is made of is a visible
// diff. And the digest is a function of the composition only: composing twice
// on different ports produces the same digest, while swapping one adapter
// produces a different one - which is what makes it usable as a claim that a
// deployment is the composition that was reviewed.
func TestTodo_ARCH_GO_020_Golden(t *testing.T) {
	first, _, _ := composeStub(t, stubServeConfig())
	graph := first.Graph()
	if err := graph.Validate(); err != nil {
		t.Fatalf("the composed graph describes itself incoherently: %v", err)
	}
	if got := graph.Canonical(); got != serveGoldenCanonical {
		t.Errorf("the composed serve graph changed.\n got:\n%s\nwant:\n%s", got, serveGoldenCanonical)
	}
	if graph.Role != RoleServe {
		t.Errorf("graph.Role = %q, want %q", graph.Role, RoleServe)
	}

	// A second composition on different ports, with a different tenant and a
	// different cell id, is the same composition.
	cfg := stubServeConfig()
	cfg.CellID = "cell-somewhere-else"
	cfg.GRPCListen = "127.0.0.1:0"
	second, _, _ := composeStub(t, cfg)
	if second.Graph().Digest() != graph.Digest() {
		t.Errorf("two identical compositions digested differently:\n%s\nvs\n%s",
			second.Graph().Canonical(), graph.Canonical())
	}

	// Swapping one adapter is a different composition.
	third, _, _ := composeStub(t, stubServeConfig(), WithVerifier(secondStubVerifier{}))
	if third.Graph().Digest() == graph.Digest() {
		t.Error("swapping the credential verifier did not change the graph digest")
	}
	swapped, ok := third.Graph().Component(ComponentCredentialVerifier)
	if !ok {
		t.Fatal("the swapped composition records no verifier at all")
	}
	if !strings.Contains(swapped.Impl, "secondStubVerifier") {
		t.Errorf("the graph records %q, not the adapter that was actually composed", swapped.Impl)
	}
}
