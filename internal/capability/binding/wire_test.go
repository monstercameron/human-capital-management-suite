package binding

import (
	"sort"
	"strings"
	"testing"
)

// TestRegisteredServiceNamesComeFromTheDescriptors proves the four service
// names are read from generated code, not retyped: a rename in the .proto
// would change them here without anyone editing this package.
func TestRegisteredServiceNamesComeFromTheDescriptors(t *testing.T) {
	want := []string{
		"hcmnext.admin.v1.AdminService",
		"hcmnext.intents.v1.IntentService",
		"hcmnext.journey.v1.JourneyService",
		"hcmnext.registry.v1.RegistryService",
	}
	got := RegisteredServiceNames()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("RegisteredServiceNames() = %v, want %v", got, want)
	}
	if !sort.StringsAreSorted(got) {
		t.Error("RegisteredServiceNames() is not sorted")
	}
}

// TestWireMethodsCoverEveryRegisteredService checks the descriptor read
// against the method counts the generated services actually declare. The
// counts are pinned so that adding an RPC to any of the four services
// forces a look at this package rather than silently widening the set a
// capability may bind to.
func TestWireMethodsCoverEveryRegisteredService(t *testing.T) {
	methods := WireMethods()

	perService := map[string]int{}
	for _, m := range methods {
		perService[m.ServiceFullName]++
	}
	want := map[string]int{
		"hcmnext.intents.v1.IntentService":    10,
		"hcmnext.registry.v1.RegistryService": 4,
		"hcmnext.admin.v1.AdminService":       6,
		"hcmnext.journey.v1.JourneyService":   27, // 26 unary + WatchJourney
	}
	for service, n := range want {
		if perService[service] != n {
			t.Errorf("%s declares %d methods, pinned %d", service, perService[service], n)
		}
	}
	if len(perService) != len(want) {
		t.Errorf("WireMethods() covered %d services, want %d", len(perService), len(want))
	}
}

// TestWireMethodsAreSortedAndUnique matters because the digest and the
// unbound-method report both depend on a stable order and on identities
// that cannot collide.
func TestWireMethodsAreSortedAndUnique(t *testing.T) {
	methods := WireMethods()
	seen := map[string]bool{}
	for i, m := range methods {
		if !m.Valid() {
			t.Errorf("method %d is not fully named: %+v", i, m)
		}
		if seen[m.Ref()] {
			t.Errorf("duplicate wire identity %s", m.Ref())
		}
		seen[m.Ref()] = true
		if i > 0 && methods[i-1].Ref() >= m.Ref() {
			t.Errorf("WireMethods() is not strictly sorted at %d: %q then %q", i, methods[i-1].Ref(), m.Ref())
		}
	}
}

// TestWireMethodsMarkTheStreamingMethod proves the streaming flag is read
// from the descriptor's Streams field rather than guessed from the name: a
// capability bound to a stream must be rejected, and that check is only
// worth anything if the flag is real.
func TestWireMethodsMarkTheStreamingMethod(t *testing.T) {
	byRef := WireMethodSet()

	watch, ok := byRef["hcmnext.journey.v1.JourneyService/WatchJourney"]
	if !ok {
		t.Fatal("WatchJourney is missing from the wire method set")
	}
	if !watch.Streaming {
		t.Error("WatchJourney is declared in the descriptor's Streams list but was not marked streaming")
	}

	unary, ok := byRef["hcmnext.intents.v1.IntentService/SimulateIntent"]
	if !ok {
		t.Fatal("SimulateIntent is missing from the wire method set")
	}
	if unary.Streaming {
		t.Error("SimulateIntent is a unary method but was marked streaming")
	}

	streaming := 0
	for _, m := range WireMethods() {
		if m.Streaming {
			streaming++
		}
	}
	if streaming != 1 {
		t.Errorf("the four registered services declare %d streaming methods, pinned 1", streaming)
	}
}

// TestWireMethodSetMatchesWireMethods keeps the two views from drifting.
func TestWireMethodSetMatchesWireMethods(t *testing.T) {
	methods := WireMethods()
	set := WireMethodSet()
	if len(set) != len(methods) {
		t.Fatalf("WireMethodSet has %d entries, WireMethods has %d", len(set), len(methods))
	}
	for _, m := range methods {
		got, ok := set[m.Ref()]
		if !ok {
			t.Errorf("%s is in WireMethods but not in WireMethodSet", m.Ref())
			continue
		}
		if got != m {
			t.Errorf("%s differs between the two views: %+v vs %+v", m.Ref(), got, m)
		}
	}
}
