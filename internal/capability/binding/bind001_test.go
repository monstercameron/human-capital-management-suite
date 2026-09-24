package binding

import (
	"context"
	"strings"
	"testing"

	model "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/model"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/modelbinding"
)

// ---------------------------------------------------------------------
// fixtures
//
// The fixtures below are deliberately not the real tables: BIND-001's
// binding rules must be provable against a controlled input, so that a
// change to the live registry can never quietly turn a rejection test into
// a vacuous pass.
// ---------------------------------------------------------------------

const (
	fixtureCapabilityID = "fixture.people.explain_worker_state"
	fixtureDefinition   = "fixture.people.explain_worker_state/v1"
	fixtureService      = "fixture.v1.FixtureService"
)

func fixtureWire() []WireMethod {
	return []WireMethod{
		{ServiceFullName: fixtureService, MethodName: "ExplainWorkerState"},
		{ServiceFullName: fixtureService, MethodName: "OtherMethod"},
		{ServiceFullName: fixtureService, MethodName: "WatchWorkerState", Streaming: true},
	}
}

func fixtureRecord(id string) capability.Record {
	return capability.Record{
		Definition: capability.Definition{
			ID:          id,
			Version:     1,
			OwnerDomain: "people",
			EffectClass: capability.EffectReadOnly,
		},
		Status: capability.StatusActive,
		Digest: "fixture-digest",
	}
}

func fixtureHandler(name string) HandlerSymbol {
	return HandlerSymbol{PackagePath: "internal/fixture", Receiver: "*handlers", Name: name}
}

func fixtureIndex(names ...string) HandlerIndex {
	index := HandlerIndex{}
	for _, n := range names {
		index[fixtureHandler(n).Ref()] = true
	}
	return index
}

func fixtureModels() modelbinding.Table {
	return modelbinding.Table{Bindings: []modelbinding.ModelBinding{{
		Definition: intent.Ref{TypeID: "fixture.people.explain_worker_state", Version: 1},
		Entities: []model.EntityMeta{
			{Name: "Worker", Version: 1},
			{Name: "Employment", Version: 1},
		},
		ReadProperties:  []model.PropertyMeta{{Ref: "employment.status"}, {Ref: "worker.identity"}},
		WriteProperties: []model.PropertyMeta{{Ref: "worker.display_name"}},
	}}}
}

// fixtureClaim is the one claim that binds cleanly: one wire method, one
// handler, one resolvable definition.
func fixtureClaim() Claim {
	return Claim{
		CapabilityID:      fixtureCapabilityID,
		CapabilityVersion: 1,
		DefinitionRef:     fixtureDefinition,
		WireMethods:       []string{fixtureService + "/ExplainWorkerState"},
		Handlers:          []HandlerSymbol{fixtureHandler("explainWorkerState")},
		Rationale:         "fixture",
	}
}

// fixtureBuild runs BuildFrom over the fixture world with one claim, and
// with the wire methods the claim does not use marked as expected-unbound so
// the assertions stay about the claim under test.
func fixtureBuild(t *testing.T, claim Claim, index HandlerIndex) Table {
	t.Helper()
	return BuildFrom(
		[]capability.Record{fixtureRecord(fixtureCapabilityID)},
		fixtureWire(),
		[]Claim{claim},
		fixtureModels(),
		index,
	)
}

// hasGap reports whether the table carries a gap of kind about capability.
func hasGap(t Table, kind GapKind, capabilityID string) bool {
	for _, g := range t.Gaps {
		if g.Kind == kind && g.Capability == capabilityID {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------
// PRIMARY
// ---------------------------------------------------------------------

// TestTypedImplementationBindingRejectsDanglingAmbiguousOrSchemaMismatchedHandler
// is BIND-001's primary oracle. Each subtest is one clause of the RED
// condition — "missing/duplicate Go symbol, unregistered capability
// version, wrong request/result descriptor ... publishes or starts; no
// handler may run" — and the last one proves the positive: when all three
// facts are present and unambiguous, and only then, the capability produces
// a binding entry.
func TestTypedImplementationBindingRejectsDanglingAmbiguousOrSchemaMismatchedHandler(t *testing.T) {
	t.Run("dangling Go symbol is rejected", func(t *testing.T) {
		// The claim names a handler that does not exist in the scanned
		// tree. An index that simply lacks it must produce a gap, never a
		// binding.
		table := fixtureBuild(t, fixtureClaim(), fixtureIndex("someOtherHandler"))
		if !hasGap(table, GapMissingHandlerSymbol, fixtureCapabilityID) {
			t.Fatalf("a claim naming a nonexistent handler symbol bound anyway:\n%s", table.Explain())
		}
		if len(table.Entries) != 0 {
			t.Fatalf("a dangling handler produced %d entries; it must produce none", len(table.Entries))
		}
	})

	t.Run("duplicate Go symbols for one capability are rejected", func(t *testing.T) {
		claim := fixtureClaim()
		claim.Handlers = append(claim.Handlers, fixtureHandler("explainWorkerStateV2"))
		table := fixtureBuild(t, claim, fixtureIndex("explainWorkerState", "explainWorkerStateV2"))
		if !hasGap(table, GapAmbiguousHandler, fixtureCapabilityID) {
			t.Fatalf("two typed symbols for one capability bound anyway:\n%s", table.Explain())
		}
		if len(table.Entries) != 0 {
			t.Fatalf("an ambiguous handler produced %d entries; it must produce none", len(table.Entries))
		}
	})

	t.Run("one Go symbol shared by two capabilities is rejected", func(t *testing.T) {
		second := fixtureClaim()
		second.CapabilityID = fixtureCapabilityID + ".sibling"
		table := BuildFrom(
			[]capability.Record{fixtureRecord(fixtureCapabilityID), fixtureRecord(second.CapabilityID)},
			fixtureWire(),
			[]Claim{fixtureClaim(), second},
			fixtureModels(),
			fixtureIndex("explainWorkerState"),
		)
		shared := table.GapsOfKind(GapHandlerBoundTwice)
		if len(shared) != 1 {
			t.Fatalf("expected exactly one HANDLER_BOUND_TWICE gap, got %d:\n%s", len(shared), table.Explain())
		}
		if shared[0].Subject != fixtureHandler("explainWorkerState").Ref() {
			t.Fatalf("the shared-handler gap named %q, want %q", shared[0].Subject, fixtureHandler("explainWorkerState").Ref())
		}
	})

	t.Run("unregistered capability version is rejected", func(t *testing.T) {
		// A claim survives the capability being renamed out from under it.
		// It must be reported, not silently ignored, or an unregistered
		// version looks bound.
		table := BuildFrom(
			nil,
			fixtureWire(),
			[]Claim{fixtureClaim()},
			fixtureModels(),
			fixtureIndex("explainWorkerState"),
		)
		if !hasGap(table, GapClaimWithoutCapability, fixtureCapabilityID) {
			t.Fatalf("a claim for an unpublished capability was accepted:\n%s", table.Explain())
		}
	})

	t.Run("claim pinned to a different published version is rejected", func(t *testing.T) {
		claim := fixtureClaim()
		claim.CapabilityVersion = 2
		table := fixtureBuild(t, claim, fixtureIndex("explainWorkerState"))
		if !hasGap(table, GapClaimWithoutCapability, fixtureCapabilityID) {
			t.Fatalf("a claim for v2 satisfied the published v1 capability:\n%s", table.Explain())
		}
		if len(table.Entries) != 0 {
			t.Fatalf("a mismatched capability version produced %d entries; it must produce none", len(table.Entries))
		}
		gaps := table.GapsOfKind(GapClaimWithoutCapability)
		if len(gaps) != 1 || gaps[0].Subject != fixtureCapabilityID+"/v2" {
			t.Fatalf("version mismatch gap = %+v, want subject %q", gaps, fixtureCapabilityID+"/v2")
		}
	})

	t.Run("unversioned claim is rejected rather than treated as a wildcard", func(t *testing.T) {
		claim := fixtureClaim()
		claim.CapabilityVersion = 0
		table := fixtureBuild(t, claim, fixtureIndex("explainWorkerState"))
		if !hasGap(table, GapClaimWithoutCapability, fixtureCapabilityID) {
			t.Fatalf("an unversioned claim satisfied the published v1 capability:\n%s", table.Explain())
		}
		if len(table.Entries) != 0 {
			t.Fatalf("an unversioned claim produced %d entries; it must produce none", len(table.Entries))
		}
		gaps := table.GapsOfKind(GapClaimWithoutCapability)
		if len(gaps) != 1 || gaps[0].Subject != fixtureCapabilityID+"/v0" {
			t.Fatalf("unversioned claim gap = %+v, want subject %q", gaps, fixtureCapabilityID+"/v0")
		}
	})

	t.Run("a published capability nothing claims is rejected", func(t *testing.T) {
		table := BuildFrom(
			[]capability.Record{fixtureRecord(fixtureCapabilityID)},
			fixtureWire(),
			nil,
			fixtureModels(),
			fixtureIndex("explainWorkerState"),
		)
		if !hasGap(table, GapUnclaimedCapability, fixtureCapabilityID) {
			t.Fatalf("a published capability with no claim bound anyway:\n%s", table.Explain())
		}
	})

	t.Run("wrong wire descriptor is rejected", func(t *testing.T) {
		claim := fixtureClaim()
		claim.WireMethods = []string{fixtureService + "/NoSuchMethod"}
		table := fixtureBuild(t, claim, fixtureIndex("explainWorkerState"))
		if !hasGap(table, GapUnknownWireMethod, fixtureCapabilityID) {
			t.Fatalf("a claim naming a method absent from the descriptors bound anyway:\n%s", table.Explain())
		}
		if !hasGap(table, GapNoWireMethod, fixtureCapabilityID) {
			t.Fatalf("a claim left with zero resolvable methods should also report NO_WIRE_METHOD:\n%s", table.Explain())
		}
	})

	t.Run("two wire descriptors for one capability are rejected", func(t *testing.T) {
		claim := fixtureClaim()
		claim.WireMethods = []string{fixtureService + "/ExplainWorkerState", fixtureService + "/OtherMethod"}
		table := fixtureBuild(t, claim, fixtureIndex("explainWorkerState"))
		if !hasGap(table, GapAmbiguousWireMethod, fixtureCapabilityID) {
			t.Fatalf("two wire methods for one capability bound anyway:\n%s", table.Explain())
		}
	})

	t.Run("a streaming descriptor cannot carry a unary capability", func(t *testing.T) {
		claim := fixtureClaim()
		claim.WireMethods = []string{fixtureService + "/WatchWorkerState"}
		table := fixtureBuild(t, claim, fixtureIndex("explainWorkerState"))
		if !hasGap(table, GapStreamingWireMethod, fixtureCapabilityID) {
			t.Fatalf("a server-streaming method carried a unary capability:\n%s", table.Explain())
		}
	})

	t.Run("an unresolvable model binding is rejected", func(t *testing.T) {
		claim := fixtureClaim()
		claim.DefinitionRef = "fixture.people.nonexistent/v1"
		table := fixtureBuild(t, claim, fixtureIndex("explainWorkerState"))
		if !hasGap(table, GapNoModelBinding, fixtureCapabilityID) {
			t.Fatalf("a definition the model binding does not resolve bound anyway:\n%s", table.Explain())
		}
	})

	t.Run("all three facts present and unambiguous produce exactly one entry", func(t *testing.T) {
		table := fixtureBuild(t, fixtureClaim(), fixtureIndex("explainWorkerState"))
		if len(table.Entries) != 1 {
			t.Fatalf("a clean claim produced %d entries, want 1:\n%s", len(table.Entries), table.Explain())
		}
		entry := table.Entries[0]
		if entry.Capability.ID != fixtureCapabilityID || entry.Capability.Version != 1 {
			t.Errorf("entry names capability %s, want %s/v1", entry.Capability, fixtureCapabilityID)
		}
		if got, want := entry.Wire.Ref(), fixtureService+"/ExplainWorkerState"; got != want {
			t.Errorf("entry wire method = %q, want %q", got, want)
		}
		if got, want := entry.Handler.Ref(), fixtureHandler("explainWorkerState").Ref(); got != want {
			t.Errorf("entry handler = %q, want %q", got, want)
		}
		if got, want := strings.Join(entry.Entities, ","), "Worker/v1,Employment/v1"; got != want {
			t.Errorf("entry entities = %q, want %q", got, want)
		}
		if got, want := strings.Join(entry.ReadProperties, ","), "employment.status,worker.identity"; got != want {
			t.Errorf("entry read properties = %q, want %q", got, want)
		}
		if got, want := strings.Join(entry.WriteProperties, ","), "worker.display_name"; got != want {
			t.Errorf("entry write properties = %q, want %q", got, want)
		}
		// The two fixture methods the claim did not bind must show up as
		// unbound, not vanish: a route no capability stands behind is a
		// finding, not a blank.
		unbound := table.GapsOfKind(GapWireMethodUnbound)
		if len(unbound) != 2 {
			t.Fatalf("expected the 2 unclaimed fixture methods to be reported unbound, got %d:\n%s", len(unbound), table.Explain())
		}
	})
}

// ---------------------------------------------------------------------
// PROPERTY
// ---------------------------------------------------------------------

// TestTodo_BIND_001_Property pins the invariants that must hold for every
// table, whatever the inputs: a capability is either bound or gapped and
// never both, the join is total in both directions, and the output is
// order-independent.
func TestTodo_BIND_001_Property(t *testing.T) {
	table := liveTable(t)

	t.Run("no capability is both bound and gapped", func(t *testing.T) {
		bound := map[string]bool{}
		for _, e := range table.Entries {
			bound[e.Capability.ID] = true
		}
		for _, g := range table.Gaps {
			if g.Capability != "" && bound[g.Capability] {
				t.Errorf("%s has entry %v and gap %s; partial binding is not binding", g.Capability, bound[g.Capability], g.ID())
			}
		}
	})

	t.Run("every published capability is accounted for exactly once", func(t *testing.T) {
		registry, err := capability.NewBootstrapRegistry()
		if err != nil {
			t.Fatalf("NewBootstrapRegistry: %v", err)
		}
		accounted := map[string]bool{}
		for _, e := range table.Entries {
			accounted[e.Capability.ID] = true
		}
		for _, g := range table.Gaps {
			if g.Capability != "" {
				accounted[g.Capability] = true
			}
		}
		for _, rec := range registry.List() {
			if !accounted[rec.Definition.ID] {
				t.Errorf("published capability %s appears in neither the entries nor the gaps; the join is not total", rec.Definition.ID)
			}
		}
	})

	t.Run("every wire method is bound or reported unbound", func(t *testing.T) {
		reported := map[string]bool{}
		for _, e := range table.Entries {
			reported[e.Wire.Ref()] = true
		}
		for _, g := range table.Gaps {
			switch g.Kind {
			case GapWireMethodUnbound:
				reported[g.Subject] = true
			case GapAmbiguousWireMethod:
				for _, ref := range strings.Split(g.Subject, ",") {
					reported[ref] = true
				}
			}
		}
		for _, m := range WireMethods() {
			if !reported[m.Ref()] {
				t.Errorf("wire method %s is neither bound, claimed nor reported unbound", m.Ref())
			}
		}
	})

	t.Run("every gap kind is declared", func(t *testing.T) {
		for _, g := range table.Gaps {
			if !g.Kind.Valid() {
				t.Errorf("gap %s carries undeclared kind %q", g.ID(), g.Kind)
			}
		}
	})

	t.Run("input order does not change the table", func(t *testing.T) {
		registry, err := capability.NewBootstrapRegistry()
		if err != nil {
			t.Fatalf("NewBootstrapRegistry: %v", err)
		}
		models, err := modelbinding.BindCatalog()
		if err != nil {
			t.Fatalf("BindCatalog: %v", err)
		}
		index := liveHandlerIndex(t)

		forward := BuildFrom(registry.List(), WireMethods(), Claims(), models, index)
		reversed := BuildFrom(
			reverseRecords(registry.List()),
			reverseWire(WireMethods()),
			reverseClaims(Claims()),
			models,
			index,
		)
		if forward.Digest() != reversed.Digest() {
			t.Fatalf("reversing the inputs changed the table digest:\n forward:  %s\n reversed: %s",
				forward.Digest(), reversed.Digest())
		}
	})
}

func reverseRecords(in []capability.Record) []capability.Record {
	out := make([]capability.Record, 0, len(in))
	for i := len(in) - 1; i >= 0; i-- {
		out = append(out, in[i])
	}
	return out
}

func reverseWire(in []WireMethod) []WireMethod {
	out := make([]WireMethod, 0, len(in))
	for i := len(in) - 1; i >= 0; i-- {
		out = append(out, in[i])
	}
	return out
}

func reverseClaims(in []Claim) []Claim {
	out := make([]Claim, 0, len(in))
	for i := len(in) - 1; i >= 0; i-- {
		out = append(out, in[i])
	}
	return out
}

// ---------------------------------------------------------------------
// GOLDEN
// ---------------------------------------------------------------------

// goldenLiveDigest pins the binding table computed over the real capability
// registry, the real generated descriptors, the reviewed claim table and
// the real model binding, with handler symbols verified against the live
// tree.
//
// This digest moving is not a test failure to paper over: it means a
// capability was published or retired, an RPC was added or removed from one
// of the four registered services, a claimed handler was renamed, or the
// generated model registry changed a bound property. Any of those is a real
// change to what this system exposes, and re-pinning it is the point at
// which someone looks.
const goldenLiveDigest = "47906f206f420a9f81d296d1707f85c2d0d010ecf1baf20cb48925f9a284d125"

// goldenLiveShape pins the counts, so a digest change reads as "what moved"
// rather than "something moved".
var goldenLiveShape = struct {
	Capabilities int
	WireMethods  int
	Entries      int
	Gaps         int
}{
	Capabilities: 11,
	WireMethods:  66,
	Entries:      0,
	Gaps:         72,
}

func TestTodo_BIND_001_Golden(t *testing.T) {
	table := liveTable(t)

	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("NewBootstrapRegistry: %v", err)
	}
	if got := len(registry.List()); got != goldenLiveShape.Capabilities {
		t.Errorf("published capability count = %d, pinned %d", got, goldenLiveShape.Capabilities)
	}
	if got := len(WireMethods()); got != goldenLiveShape.WireMethods {
		t.Errorf("registered wire method count = %d, pinned %d", got, goldenLiveShape.WireMethods)
	}
	if got := len(table.Entries); got != goldenLiveShape.Entries {
		t.Errorf("bound entry count = %d, pinned %d:\n%s", got, goldenLiveShape.Entries, table.Explain())
	}
	if got := len(table.Gaps); got != goldenLiveShape.Gaps {
		t.Errorf("gap count = %d, pinned %d:\n%s", got, goldenLiveShape.Gaps, table.Explain())
	}
	if got := table.Digest(); got != goldenLiveDigest {
		t.Errorf("live binding table digest = %q, pinned %q; something about what this system publishes or exposes changed:\n%s",
			got, goldenLiveDigest, table.Explain())
	}

	// The digest must actually depend on the table, or pinning it proves
	// nothing.
	perturbed := table
	perturbed.Entries = append([]Entry(nil), table.Entries...)
	perturbed.Entries = append(perturbed.Entries, Entry{
		Capability: capability.Key{ID: "perturbation", Version: 1},
		Wire:       WireMethod{ServiceFullName: fixtureService, MethodName: "ExplainWorkerState"},
		Handler:    fixtureHandler("explainWorkerState"),
	})
	if perturbed.Digest() == table.Digest() {
		t.Errorf("adding an entry did not change the digest; the digest does not cover the table")
	}
}

// ---------------------------------------------------------------------
// INTEGRATION
// ---------------------------------------------------------------------

// TestTodo_BIND_001_Integration invokes every published capability in
// memory through the governed gateway and records what actually came back.
//
// BIND-001's RED clause names this failure explicitly: "a map[string]any
// handler publishes or starts". Today every one of the ten BOOTSTRAP
// capabilities answers with map[string]interface{} — the bootstrapEcho
// placeholder in internal/capability/bootstrap.go — so the assertion is not
// "no capability is untyped" (that would fail the build for a state the
// tree openly documents) but "exactly the capabilities we know are untyped
// are untyped". A capability that becomes typed, or a new one that arrives
// untyped, breaks this test and gets looked at.
func TestTodo_BIND_001_Integration(t *testing.T) {
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("NewBootstrapRegistry: %v", err)
	}
	results := ProbeResultTypes(context.Background(), registry)
	if len(results) != len(registry.List()) {
		t.Fatalf("probed %d capabilities, registry publishes %d", len(results), len(registry.List()))
	}

	for _, r := range results {
		if !r.Invoked {
			t.Errorf("%s was refused before reaching a handler (%s); a published read-only capability must be invocable",
				r.Capability, r.RefusalCode)
		}
	}

	untyped := UntypedResultCapabilities(results)
	want := []string{
		"hcmnext.dataops.explain_field_history/v1",
		"hcmnext.intelligence.explain_transaction/v1",
		"hcmnext.operations.create_repair_plan/v1",
		"hcmnext.operations.detect_drift/v1",
		"hcmnext.operations.simulate_repair/v1",
		"hcmnext.people.explain_worker_state/v1",
		"hcmnext.people.promote_worker/v1",
		"hcmnext.registry.explain_capability/v1",
		"hcmnext.registry.resolve_capability/v1",
		"hcmnext.rewards.evaluate_pay_band_position/v1",
		"hcmnext.rewards.simulate_compensation/v1",
	}
	if strings.Join(untyped, "\n") != strings.Join(want, "\n") {
		t.Errorf("the set of capabilities answering with an untyped map changed\n got:  %v\n want: %v", untyped, want)
	}
}

// ---------------------------------------------------------------------
// CONFORMANCE
// ---------------------------------------------------------------------

// TestTodo_BIND_001_Conformance runs the whole binding check against the
// live tree and fails on any gap the allowlist does not already own, and
// equally on any allowlist entry whose gap has been closed. The allowlist
// is a ratchet: it can only shrink without someone editing this package.
func TestTodo_BIND_001_Conformance(t *testing.T) {
	table := liveTable(t)
	report := CheckConformance(table, Allowlist())

	for _, g := range report.NewGaps {
		t.Errorf("new binding gap with no owner: %s\n  %s", g.ID(), g.Detail)
	}
	for _, e := range report.StaleAllowlist {
		t.Errorf("allowlisted gap %s (owner %s) no longer exists; remove the entry rather than leaving a stale waiver", e.GapID, e.OwnerTodo)
	}
	if !report.OK() {
		t.Fatalf("binding conformance failed:\n%s", report.Explain())
	}

	// Every accepted gap must name a real owning todo: an allowlist entry
	// with no owner is an excuse, not a plan.
	for _, e := range Allowlist() {
		if e.OwnerTodo == "" {
			t.Errorf("allowlist entry %s has no owning todo", e.GapID)
		}
		if e.Rationale == "" {
			t.Errorf("allowlist entry %s has no rationale", e.GapID)
		}
	}
	t.Logf("binding conformance over the live tree:\n%s", report.Explain())
}

// ---------------------------------------------------------------------
// MUTATION
// ---------------------------------------------------------------------

// TestTodo_BIND_001_Mutation applies one mutation at a time to inputs that
// bind cleanly and requires each to be caught. A checker that cannot be
// broken by these is not checking.
func TestTodo_BIND_001_Mutation(t *testing.T) {
	clean := fixtureBuild(t, fixtureClaim(), fixtureIndex("explainWorkerState"))
	if len(clean.Entries) != 1 || len(clean.GapsOfKind(GapWireMethodUnbound)) != 2 {
		t.Fatalf("the mutation baseline is not clean:\n%s", clean.Explain())
	}

	mutations := []struct {
		name    string
		mutate  func(Claim) (Claim, HandlerIndex)
		wantGap GapKind
	}{
		{
			name: "rename the handler in the tree",
			mutate: func(c Claim) (Claim, HandlerIndex) {
				return c, fixtureIndex("explainWorkerStateRenamed")
			},
			wantGap: GapMissingHandlerSymbol,
		},
		{
			name: "delete every handler from the tree",
			mutate: func(c Claim) (Claim, HandlerIndex) {
				return c, fixtureIndex()
			},
			wantGap: GapMissingHandlerSymbol,
		},
		{
			name: "point the claim at a method that does not exist",
			mutate: func(c Claim) (Claim, HandlerIndex) {
				c.WireMethods = []string{fixtureService + "/Renamed"}
				return c, fixtureIndex("explainWorkerState")
			},
			wantGap: GapUnknownWireMethod,
		},
		{
			name: "add a second route to the claim",
			mutate: func(c Claim) (Claim, HandlerIndex) {
				c.WireMethods = append(c.WireMethods, fixtureService+"/OtherMethod")
				return c, fixtureIndex("explainWorkerState")
			},
			wantGap: GapAmbiguousWireMethod,
		},
		{
			name: "drop the definition reference",
			mutate: func(c Claim) (Claim, HandlerIndex) {
				c.DefinitionRef = ""
				return c, fixtureIndex("explainWorkerState")
			},
			wantGap: GapNoModelBinding,
		},
		{
			name: "drop the handler from the claim",
			mutate: func(c Claim) (Claim, HandlerIndex) {
				c.Handlers = nil
				return c, fixtureIndex("explainWorkerState")
			},
			wantGap: GapNoHandler,
		},
		{
			name: "swap the route for a stream",
			mutate: func(c Claim) (Claim, HandlerIndex) {
				c.WireMethods = []string{fixtureService + "/WatchWorkerState"}
				return c, fixtureIndex("explainWorkerState")
			},
			wantGap: GapStreamingWireMethod,
		},
	}

	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			claim, index := m.mutate(fixtureClaim())
			table := fixtureBuild(t, claim, index)
			if !hasGap(table, m.wantGap, fixtureCapabilityID) {
				t.Fatalf("mutation %q survived: expected a %s gap, got:\n%s", m.name, m.wantGap, table.Explain())
			}
			if len(table.Entries) != 0 {
				t.Fatalf("mutation %q still produced %d bound entries", m.name, len(table.Entries))
			}
			if table.Digest() == clean.Digest() {
				t.Fatalf("mutation %q did not change the table digest", m.name)
			}
		})
	}
}
