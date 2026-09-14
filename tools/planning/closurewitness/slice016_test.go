package closurewitness

import (
	"bytes"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mutant is one registry defect planted into the complete fixture, with
// the exact defect it must raise.
type mutant struct {
	name     string
	mutate   func(*Snapshot)
	code     string
	class    EdgeClass
	identity string
	// definition is the witness the defect lands on; empty for a
	// report-level orphan.
	definition string
}

func dropDefinition[T any](rows []T, keep func(T) bool) []T {
	var out []T
	for _, row := range rows {
		if keep(row) {
			out = append(out, row)
		}
	}
	return out
}

func mutants() []mutant {
	alphaBare := bareID(defAlpha)
	alphaWire := "hcmnext.fixture.v1.FixtureService/Alpha"
	return []mutant{
		{
			name:   "slice no longer names the definition",
			mutate: func(s *Snapshot) { s.Slices[0].Intents = []string{defBeta} },
			code:   DefectAbsent, class: ClassSlice, identity: "SLICE|" + defAlpha, definition: defAlpha,
		},
		{
			name: "a second slice claims the definition",
			mutate: func(s *Snapshot) {
				s.Slices = append(s.Slices, SliceRow{SliceID: "other", Version: 1, DigestVerified: true, Intents: []string{defAlpha}, Capabilities: []string{alphaBare}})
			},
			code: DefectDuplicate, class: ClassSlice, identity: "SLICE|" + defAlpha + "|fixture,other", definition: defAlpha,
		},
		{
			name:   "slice registry digest no longer verifies",
			mutate: func(s *Snapshot) { s.Slices[0].DigestVerified = false },
			code:   DefectStale, class: ClassSlice, identity: "SLICE|fixture|digest", definition: defAlpha,
		},
		{
			name:   "slice names the definition but not its capability",
			mutate: func(s *Snapshot) { s.Slices[0].Capabilities = []string{bareID(defBeta)} },
			code:   DefectStale, class: ClassSlice, identity: "SLICE|fixture|" + alphaBare, definition: defAlpha,
		},
		{
			name:   "source descriptor row duplicated",
			mutate: func(s *Snapshot) { s.Descriptors = append(s.Descriptors, s.Descriptors[0]) },
			code:   DefectDuplicate, class: ClassSource, identity: "SOURCE|" + defAlpha + "|descriptor", definition: defAlpha,
		},
		{
			name: "compiled definition missing",
			mutate: func(s *Snapshot) {
				s.Catalog = dropDefinition(s.Catalog, func(c CatalogRow) bool { return c.Definition != defAlpha })
			},
			code: DefectReverseAbsent, class: ClassSource, identity: "SOURCE|" + defAlpha + "|catalog", definition: defAlpha,
		},
		{
			name:   "compiled display name drifts from the source",
			mutate: func(s *Snapshot) { s.Catalog[0].DisplayName = "AlphaRenamed" },
			code:   DefectStale, class: ClassSource, identity: "SOURCE|" + defAlpha + "|display_name", definition: defAlpha,
		},
		{
			name:   "scope ceiling gate disagrees with the compiled release",
			mutate: func(s *Snapshot) { s.Ceiling[0].Gate = "P1B" },
			code:   DefectStale, class: ClassPhaseGate, identity: "PHASE_GATE|" + defAlpha + "|gate|P1B", definition: defAlpha,
		},
		{
			name: "model binding no longer resolves",
			mutate: func(s *Snapshot) {
				s.ModelBindings = dropDefinition(s.ModelBindings, func(m ModelBindingRow) bool { return m.Definition != defAlpha })
				s.ModelGaps = append(s.ModelGaps, ModelGapRow{Definition: defAlpha, Element: "aggregate_root", Detail: "Worker/v9 is not in the generated model registry"})
			},
			code: DefectStale, class: ClassModel, identity: "MODEL|" + defAlpha + "|aggregate_root", definition: defAlpha,
		},
		{
			name:   "engine declared but not wired",
			mutate: func(s *Snapshot) { s.Engines[0].Findings = []string{"IMPLICIT"} },
			code:   DefectReverseAbsent, class: ClassEngine, identity: "ENGINE|" + defAlpha + "|internal/engines/snapshot|snapshot", definition: defAlpha,
		},
		{
			name: "claimed capability no longer published",
			mutate: func(s *Snapshot) {
				s.Capabilities = dropDefinition(s.Capabilities, func(c CapabilityRow) bool { return c.CapabilityID != alphaBare })
			},
			code: DefectReverseAbsent, class: ClassCapability, identity: "CAPABILITY|" + defAlpha + "|" + alphaBare + "/v1", definition: defAlpha,
		},
		{
			name: "two typed handlers answer the capability",
			mutate: func(s *Snapshot) {
				s.BindingEntries = dropDefinition(s.BindingEntries, func(e BindingEntryRow) bool { return e.CapabilityID != alphaBare })
				s.BindingGaps = append(s.BindingGaps, BindingGapRow{Kind: "AMBIGUOUS_HANDLER", CapabilityID: alphaBare, Subject: "pkg.(*a).Alpha,pkg.(*b).Alpha", OwnerTodo: "BIND-001"})
			},
			code: DefectDuplicate, class: ClassHandler, identity: "HANDLER|" + alphaBare + "|pkg.(*a).Alpha,pkg.(*b).Alpha", definition: defAlpha,
		},
		{
			name:   "endpoint reachable only through generic lifecycle routes",
			mutate: func(s *Snapshot) { s.IntentDispositions[0].Category = categoryGeneric },
			code:   DefectAggregateOnly, class: ClassEndpoint, identity: "ENDPOINT|" + defAlpha + "|generic_lifecycle", definition: defAlpha,
		},
		{
			name: "serving endpoint absent from the endpoint manifest",
			mutate: func(s *Snapshot) {
				s.Endpoints = dropDefinition(s.Endpoints, func(e EndpointRow) bool { return e.EndpointID != alphaWire })
			},
			code: DefectStale, class: ClassEndpoint, identity: "ENDPOINT|" + defAlpha + "|" + alphaWire, definition: defAlpha,
		},
		{
			name: "scenario matrix missing",
			mutate: func(s *Snapshot) {
				s.Scenarios = dropDefinition(s.Scenarios, func(r ScenarioRow) bool { return r.Definition != defAlpha })
			},
			code: DefectAbsent, class: ClassScenario, identity: "SCENARIO|" + defAlpha, definition: defAlpha,
		},
		{
			name:   "bound todo retired",
			mutate: func(s *Snapshot) { s.Todos[0].Retired = true },
			code:   DefectStale, class: ClassTodo, identity: "TODO|" + defAlpha + "|FIX-001", definition: defAlpha,
		},
		{
			name:   "todo reaches the definition only by domain",
			mutate: func(s *Snapshot) { s.TodoClaims[0].Kind = ClaimDomain },
			code:   DefectAggregateOnly, class: ClassTodo, identity: "TODO|" + defAlpha + "|domain_only", definition: defAlpha,
		},
		{
			name:   "checked-in coverage registry names a claim todos.md no longer makes",
			mutate: func(s *Snapshot) { s.Coverage[0].DirectTodos = []string{"FIX-001", "FIX-999"} },
			code:   DefectStale, class: ClassTodo, identity: "TODO|" + defAlpha + "|FIX-999|coverage_registry", definition: defAlpha,
		},
		{
			name:   "evidence test deleted from the tree",
			mutate: func(s *Snapshot) { delete(s.TestExists, "TestFixtureAlpha") },
			code:   DefectStale, class: ClassTest, identity: "TEST|" + defAlpha + "|FIX-001|TestFixtureAlpha", definition: defAlpha,
		},
		{
			name:   "ticked todo names no evidence test",
			mutate: func(s *Snapshot) { s.Todos[0].EvidenceTests = nil },
			code:   DefectAbsent, class: ClassEvidence, identity: "EVIDENCE|" + defAlpha + "|FIX-001", definition: defAlpha,
		},
		{
			name:   "evidence waiver expired",
			mutate: func(s *Snapshot) { s.Waivers[0].Expiry = "2026-09-01" },
			code:   DefectStale, class: ClassEvidence, identity: "EVIDENCE|" + defAlpha + "|FIX-001|waiver|missing commit/branch identity", definition: defAlpha,
		},
		{
			name: "every bound test also serves another definition",
			mutate: func(s *Snapshot) {
				s.Todos[1].EvidenceTests = []string{"TestFixtureAlpha"}
				s.Todos[1].PrimaryTest = "TestFixtureAlpha"
				s.Coverage[1].Tests = []string{"TestFixtureAlpha"}
			},
			code: DefectAggregateOnly, class: ClassTest, identity: "TEST|" + defAlpha + "|shared", definition: defAlpha,
		},
		{
			name:   "slice names a definition that is not source-bound",
			mutate: func(s *Snapshot) { s.Slices[0].Intents = append(s.Slices[0].Intents, "hcmnext.fixture.ghost/v1") },
			code:   DefectOrphan, class: ClassSlice, identity: "SLICE|fixture|hcmnext.fixture.ghost/v1",
		},
		{
			name: "INTENT CONTEXT names an unknown definition",
			mutate: func(s *Snapshot) {
				s.ContextTokens = append(s.ContextTokens, ContextTokenRow{TodoID: "FIX-003", Field: "DIRECT", Token: "Ghost"})
			},
			code: DefectOrphan, class: ClassTodo, identity: "TODO|FIX-003|DIRECT|Ghost",
		},
		{
			name:   "scenario registry unavailable",
			mutate: func(s *Snapshot) { s.Unsourced = []EdgeClass{ClassScenario} },
			code:   DefectUnsourced, class: ClassScenario, identity: "SCENARIO|" + defAlpha + "|registry", definition: defAlpha,
		},
	}
}

// TestPerIntentClosureWitnessCrossJoinIsTotalUniqueAndCurrent is SLICE-016's
// PRIMARY test: a fully bound fixture reaches COMPLETE with exactly one
// digest-backed witness per source-bound definition carrying forward and
// reverse edges for every class; adding an unbound or duplicated source row
// keeps the cross-join total and unique; and a waiver that has since expired
// or a checked-in registry that no longer matches its source makes the
// witness incomplete, so a witness is current rather than historical.
func TestPerIntentClosureWitnessCrossJoinIsTotalUniqueAndCurrent(t *testing.T) {
	report := mustCompile(t, completeSnapshot())
	if report.Result != ResultComplete || len(report.Orphans) != 0 {
		t.Fatalf("complete fixture compiled %s with defects:%s", report.Result, describeDefects(allDefects(report)))
	}
	if len(report.Witnesses) != 2 || report.Totals.Complete != 2 || report.Totals.Incomplete != 0 {
		t.Fatalf("totals = %+v over %d witnesses, want 2 complete", report.Totals, len(report.Witnesses))
	}
	for _, def := range []string{defAlpha, defBeta} {
		w := witnessFor(t, report, def)
		if w.Result != ResultComplete {
			t.Fatalf("%s result %s:%s", def, w.Result, describeDefects(w.Defects))
		}
		if !strings.HasPrefix(w.Digest, "sha256:") || w.Digest != witnessDigest(w) {
			t.Errorf("%s digest %q does not bind its content", def, w.Digest)
		}
		if w.Source.Registry != RegistryDescriptors || w.Source.RowDigest == "" {
			t.Errorf("%s source binding = %+v", def, w.Source)
		}
		if w.Phase.DescriptorPhase != "GATE_A" || w.Phase.Release != "P1A" || w.Phase.CeilingGate != "P1A" || w.Phase.ClaimedMaturity != maturityDefined {
			t.Errorf("%s phase binding = %+v", def, w.Phase)
		}
		if w.EndpointDisposition.Category != categoryTyped || len(w.EndpointDisposition.ServingEndpoints) != 1 {
			t.Errorf("%s endpoint disposition = %+v", def, w.EndpointDisposition)
		}
		if len(w.Tests) != 1 || len(w.Evidence) != 1 || len(w.Evidence[0].Tests) != 1 {
			t.Errorf("%s test/evidence outputs = %v / %+v", def, w.Tests, w.Evidence)
		}
		forward, reverse := map[EdgeClass]bool{}, map[EdgeClass]bool{}
		for _, e := range w.Edges {
			if e.Direction == Forward {
				forward[e.Class] = true
			} else {
				reverse[e.Class] = true
			}
		}
		for _, cs := range w.Classes {
			if cs.State != StateBound {
				t.Errorf("%s class %s = %s, want BOUND", def, cs.Class, cs.State)
			}
			if !forward[cs.Class] && !reverse[cs.Class] {
				t.Errorf("%s class %s is BOUND with no edge", def, cs.Class)
			}
		}
		for _, class := range []EdgeClass{ClassSource, ClassPhaseGate, ClassSlice, ClassEngine, ClassCapability, ClassEndpoint, ClassTodo, ClassTest} {
			if !forward[class] || !reverse[class] {
				t.Errorf("%s class %s lacks a forward (%v) or reverse (%v) edge", def, class, forward[class], reverse[class])
			}
		}
	}
	if got := witnessFor(t, report, defAlpha).Expiry; got != "2026-12-31" {
		t.Errorf("alpha expiry = %s, want the waiver's 2026-12-31", got)
	}
	if got := witnessFor(t, report, defBeta).Expiry; got != NoWaiver {
		t.Errorf("beta expiry = %s, want %s", got, NoWaiver)
	}
	if mismatches := Verify(completeSnapshot(), report); len(mismatches) != 0 {
		t.Fatalf("Verify on a fresh report: %v", mismatches)
	}

	// Total and unique: an unbound source row gets its own witness, and a
	// duplicated source row never yields a second one.
	snap := completeSnapshot()
	gamma := "hcmnext.fixture.gamma/v1"
	snap.Descriptors = append(snap.Descriptors, DescriptorRow{Definition: gamma, DisplayName: "Gamma", Phase: "GATE_A", RowDigest: "sha256:gamma"}, snap.Descriptors[0])
	total := mustCompile(t, snap)
	if len(total.Witnesses) != 3 {
		t.Fatalf("got %d witnesses for 3 distinct source rows", len(total.Witnesses))
	}
	g := witnessFor(t, total, gamma)
	if g.Result != ResultIncomplete {
		t.Fatalf("an unbound definition compiled %s", g.Result)
	}
	for _, cs := range g.Classes {
		if cs.Class != ClassSource && cs.State == StateBound {
			t.Errorf("unbound definition reports class %s BOUND", cs.Class)
		}
	}
	for _, want := range []struct {
		code     string
		class    EdgeClass
		identity string
	}{
		{DefectReverseAbsent, ClassSource, "SOURCE|" + gamma + "|catalog"},
		{DefectAbsent, ClassSlice, "SLICE|" + gamma},
		{DefectAbsent, ClassCapability, "CAPABILITY|" + gamma},
		{DefectReverseAbsent, ClassEndpoint, "ENDPOINT|" + gamma + "|disposition"},
		{DefectAbsent, ClassTodo, "TODO|" + gamma},
		{DefectAbsent, ClassEvidence, "EVIDENCE|" + gamma},
	} {
		if !hasDefect(g.Defects, want.code, want.class, want.identity) {
			t.Errorf("unbound definition lacks %s %s:%s", want.code, want.identity, describeDefects(g.Defects))
		}
	}
	if !hasDefect(witnessFor(t, total, defAlpha).Defects, DefectDuplicate, ClassSource, "SOURCE|"+defAlpha+"|descriptor") {
		t.Error("a duplicated source row did not raise EDGE_DUPLICATE on its one witness")
	}
	if total.Totals.Definitions != 3 || total.Totals.Complete != 1 || total.Totals.Incomplete != 2 || total.Result != ResultIncomplete {
		t.Errorf("totals = %+v result %s, want 3/1/2 incomplete", total.Totals, total.Result)
	}

	// Current: the same fixture judged after the waiver lapses is stale.
	later := completeSnapshot()
	later.AsOf = "2027-01-01"
	lapsed := mustCompile(t, later)
	if !hasDefect(witnessFor(t, lapsed, defAlpha).Defects, DefectStale, ClassEvidence, "EVIDENCE|"+defAlpha+"|FIX-001|waiver|missing commit/branch identity") {
		t.Errorf("expired waiver not reported:%s", describeDefects(allDefects(lapsed)))
	}
	if witnessFor(t, lapsed, defBeta).Result != ResultComplete {
		t.Error("beta leans on no waiver and must stay complete as of a later date")
	}

	for _, d := range allDefects(total) {
		if d.Result != ResultIncomplete || d.Identity == "" || !d.Class.Valid() {
			t.Errorf("defect %+v lacks the SLICE_CLOSURE_INCOMPLETE result, a valid class or an identity", d)
		}
	}
}

// TestTodo_SLICE_016_Property proves the witness set is a pure function of
// registry content: any permutation of every input row list compiles to
// byte-identical output, and totals always reconcile with the witnesses.
func TestTodo_SLICE_016_Property(t *testing.T) {
	fixtures := map[string]func() Snapshot{
		"complete": completeSnapshot,
		"every mutant at once": func() Snapshot {
			s := completeSnapshot()
			for _, m := range mutants() {
				if m.class == ClassScenario && m.code == DefectUnsourced {
					continue
				}
				m.mutate(&s)
			}
			return s
		},
	}
	for name, build := range fixtures {
		t.Run(name, func(t *testing.T) {
			want, err := MarshalReport(mustCompile(t, build()))
			if err != nil {
				t.Fatal(err)
			}
			for seed := int64(1); seed <= 25; seed++ {
				snap := build()
				shuffleSnapshot(rand.New(rand.NewSource(seed)), &snap)
				report := mustCompile(t, snap)
				got, err := MarshalReport(report)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("seed %d: permuted input changed the report bytes", seed)
				}
				assertTotalsReconcile(t, report)
			}
		})
	}
}

func shuffleSnapshot(r *rand.Rand, s *Snapshot) {
	shuffle := func(n int, swap func(i, j int)) { r.Shuffle(n, swap) }
	shuffle(len(s.Descriptors), func(i, j int) { s.Descriptors[i], s.Descriptors[j] = s.Descriptors[j], s.Descriptors[i] })
	shuffle(len(s.Catalog), func(i, j int) { s.Catalog[i], s.Catalog[j] = s.Catalog[j], s.Catalog[i] })
	shuffle(len(s.Ceiling), func(i, j int) { s.Ceiling[i], s.Ceiling[j] = s.Ceiling[j], s.Ceiling[i] })
	shuffle(len(s.Slices), func(i, j int) { s.Slices[i], s.Slices[j] = s.Slices[j], s.Slices[i] })
	for k := range s.Slices {
		in := s.Slices[k].Intents
		shuffle(len(in), func(i, j int) { in[i], in[j] = in[j], in[i] })
	}
	shuffle(len(s.ModelBindings), func(i, j int) { s.ModelBindings[i], s.ModelBindings[j] = s.ModelBindings[j], s.ModelBindings[i] })
	shuffle(len(s.ModelGaps), func(i, j int) { s.ModelGaps[i], s.ModelGaps[j] = s.ModelGaps[j], s.ModelGaps[i] })
	shuffle(len(s.Engines), func(i, j int) { s.Engines[i], s.Engines[j] = s.Engines[j], s.Engines[i] })
	shuffle(len(s.Capabilities), func(i, j int) { s.Capabilities[i], s.Capabilities[j] = s.Capabilities[j], s.Capabilities[i] })
	shuffle(len(s.Claims), func(i, j int) { s.Claims[i], s.Claims[j] = s.Claims[j], s.Claims[i] })
	shuffle(len(s.BindingEntries), func(i, j int) { s.BindingEntries[i], s.BindingEntries[j] = s.BindingEntries[j], s.BindingEntries[i] })
	shuffle(len(s.BindingGaps), func(i, j int) { s.BindingGaps[i], s.BindingGaps[j] = s.BindingGaps[j], s.BindingGaps[i] })
	shuffle(len(s.IntentDispositions), func(i, j int) {
		s.IntentDispositions[i], s.IntentDispositions[j] = s.IntentDispositions[j], s.IntentDispositions[i]
	})
	shuffle(len(s.Endpoints), func(i, j int) { s.Endpoints[i], s.Endpoints[j] = s.Endpoints[j], s.Endpoints[i] })
	shuffle(len(s.Scenarios), func(i, j int) { s.Scenarios[i], s.Scenarios[j] = s.Scenarios[j], s.Scenarios[i] })
	shuffle(len(s.Todos), func(i, j int) { s.Todos[i], s.Todos[j] = s.Todos[j], s.Todos[i] })
	shuffle(len(s.TodoClaims), func(i, j int) { s.TodoClaims[i], s.TodoClaims[j] = s.TodoClaims[j], s.TodoClaims[i] })
	shuffle(len(s.ContextTokens), func(i, j int) { s.ContextTokens[i], s.ContextTokens[j] = s.ContextTokens[j], s.ContextTokens[i] })
	shuffle(len(s.Coverage), func(i, j int) { s.Coverage[i], s.Coverage[j] = s.Coverage[j], s.Coverage[i] })
	shuffle(len(s.Waivers), func(i, j int) { s.Waivers[i], s.Waivers[j] = s.Waivers[j], s.Waivers[i] })
}

func assertTotalsReconcile(t *testing.T, report Report) {
	t.Helper()
	totals := report.Totals
	if totals.Definitions != len(report.Witnesses) || totals.Complete+totals.Incomplete != totals.Definitions || totals.Orphans != len(report.Orphans) {
		t.Fatalf("totals %+v do not reconcile with %d witnesses and %d orphans", totals, len(report.Witnesses), len(report.Orphans))
	}
	defectCount := len(report.Orphans)
	for _, w := range report.Witnesses {
		defectCount += len(w.Defects)
		if len(w.Classes) != len(Classes()) {
			t.Fatalf("%s reports %d classes", w.Definition, len(w.Classes))
		}
		for _, cs := range w.Classes {
			defective := false
			for _, d := range w.Defects {
				if d.Class == cs.Class {
					defective = true
				}
			}
			if defective == (cs.State == StateBound || cs.State == StateJustified) {
				t.Fatalf("%s class %s state %s disagrees with its defects", w.Definition, cs.Class, cs.State)
			}
		}
		if (w.Result == ResultComplete) != (len(w.Defects) == 0) {
			t.Fatalf("%s result %s with %d defects", w.Definition, w.Result, len(w.Defects))
		}
	}
	sum := 0
	for _, n := range totals.ByDefectCode {
		sum += n
	}
	if sum != defectCount {
		t.Fatalf("by_defect_code sums to %d, %d defects exist", sum, defectCount)
	}
	for _, class := range Classes() {
		n := 0
		for _, count := range totals.ByClassState[class] {
			n += count
		}
		if n != totals.Definitions {
			t.Fatalf("class %s counts %d states over %d definitions", class, n, totals.Definitions)
		}
	}
}

// goldenSnapshot mixes a complete witness, an incomplete witness and an
// orphan so the pinned bytes cover every report section.
func goldenSnapshot() Snapshot {
	s := completeSnapshot()
	s.IntentDispositions[1].Category = categoryGeneric
	s.Descriptors = append(s.Descriptors, DescriptorRow{Definition: "hcmnext.fixture.gamma/v1", DisplayName: "Gamma", Phase: "GATE_B", RowDigest: "sha256:gamma"})
	s.ContextTokens = append(s.ContextTokens, ContextTokenRow{TodoID: "FIX-003", Field: "INTENTS", Token: "Ghost"})
	return s
}

// TestTodo_SLICE_016_Golden pins the canonical report bytes. Set
// CLOSUREWITNESS_UPDATE_GOLDEN=1 to rewrite testdata/golden.json after a
// reviewed format change.
func TestTodo_SLICE_016_Golden(t *testing.T) {
	got, err := MarshalReport(mustCompile(t, goldenSnapshot()))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "golden.json")
	if os.Getenv("CLOSUREWITNESS_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(got, bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))) {
		t.Fatalf("report bytes differ from %s; got:\n%s", path, got)
	}
	for _, needle := range []string{`"result": "COMPLETE"`, `"result": "SLICE_CLOSURE_INCOMPLETE"`, `"code": "EDGE_ORPHAN"`, `"expiry": "2026-12-31"`} {
		if !bytes.Contains(want, []byte(needle)) {
			t.Errorf("golden lacks %s, so it does not pin that section", needle)
		}
	}
}

// TestTodo_SLICE_016_Security proves no input can forge closure: there is
// no result field to set, a claimed maturity cannot promote, an
// allowlisted gap still blocks, lookalike identities never bind, an absent
// registry is not a clean one, and a tampered report fails verification
// even when the attacker recomputes the witness digest.
func TestTodo_SLICE_016_Security(t *testing.T) {
	t.Run("claimed maturity cannot promote an incomplete witness", func(t *testing.T) {
		s := completeSnapshot()
		s.Slices[0].Intents = []string{defBeta}
		w := witnessFor(t, mustCompile(t, s), defAlpha)
		if w.Result != ResultIncomplete || !hasDefect(w.Defects, DefectMaturityOverclaim, ClassPhaseGate, "PHASE_GATE|"+defAlpha+"|maturity|DEFINED") {
			t.Fatalf("DEFINED maturity over an incomplete witness was not flagged:%s", describeDefects(w.Defects))
		}
		base := witnessFor(t, mustCompile(t, completeSnapshot()), defAlpha)
		if hasDefect(base.Defects, DefectMaturityOverclaim, ClassPhaseGate, "PHASE_GATE|"+defAlpha+"|maturity|DEFINED") {
			t.Fatal("a complete witness was flagged as over-claiming")
		}
	})
	t.Run("an allowlisted binding gap still blocks closure", func(t *testing.T) {
		s := completeSnapshot()
		s.BindingEntries = s.BindingEntries[1:]
		s.BindingGaps = []BindingGapRow{{Kind: "AMBIGUOUS_WIRE_METHOD", CapabilityID: bareID(defAlpha), Subject: "a,b", OwnerTodo: "ENDPOINT-009"}}
		w := witnessFor(t, mustCompile(t, s), defAlpha)
		if w.Result != ResultIncomplete || !hasDefect(w.Defects, DefectDuplicate, ClassEndpoint, "ENDPOINT|"+bareID(defAlpha)+"|a,b") {
			t.Fatalf("allowlisted gap did not block:%s", describeDefects(w.Defects))
		}
		if !strings.Contains(describeDefects(w.Defects), "closing owner ENDPOINT-009") {
			t.Error("the defect does not name the gap's closing owner")
		}
	})
	t.Run("lookalike identities never bind", func(t *testing.T) {
		s := completeSnapshot()
		s.TodoClaims[0].Intent = bareID(defAlpha) + " "
		s.ContextTokens[0].Token = "alpha"
		r := mustCompile(t, s)
		w := witnessFor(t, r, defAlpha)
		if !hasDefect(w.Defects, DefectAbsent, ClassTodo, "TODO|"+defAlpha) {
			t.Errorf("a whitespace-padded intent id bound the todo:%s", describeDefects(w.Defects))
		}
		if !hasDefect(r.Orphans, DefectOrphan, ClassTodo, "TODO|FIX-001|DIRECT|alpha") {
			t.Errorf("a case-changed display name was accepted:%s", describeDefects(r.Orphans))
		}
	})
	t.Run("an unsourced registry is never clean", func(t *testing.T) {
		s := completeSnapshot()
		s.Unsourced = []EdgeClass{ClassSlice}
		r := mustCompile(t, s)
		if r.Result != ResultIncomplete {
			t.Fatal("a report with an unsourced class compiled COMPLETE")
		}
		for _, w := range r.Witnesses {
			if classState(w, ClassSlice) != StateUnsourced {
				t.Errorf("%s slice class = %s, want UNSOURCED", w.Definition, classState(w, ClassSlice))
			}
		}
	})
	t.Run("an unusable as-of date is refused", func(t *testing.T) {
		for _, bad := range []string{"", "2026/09/13", "2026-13-01", "2026-09-32", "26-09-13", "2026-09-1x"} {
			s := completeSnapshot()
			s.AsOf = bad
			if _, err := Compile(s); !errors.Is(err, ErrInvalidAsOf) {
				t.Errorf("as-of %q: err = %v, want ErrInvalidAsOf", bad, err)
			}
		}
	})
	t.Run("tampered reports fail verification", func(t *testing.T) {
		honest := mustCompile(t, goldenSnapshot())

		forged := mustCompile(t, goldenSnapshot())
		for i := range forged.Witnesses {
			if forged.Witnesses[i].Result == ResultIncomplete {
				forged.Witnesses[i].Result = ResultComplete
				forged.Witnesses[i].Defects = []Defect{}
				forged.Witnesses[i].Digest = witnessDigest(forged.Witnesses[i])
			}
		}
		forged.Result = ResultComplete
		forged.Digest = digestOf(forged)
		if got := strings.Join(Verify(goldenSnapshot(), forged), "\n"); !strings.Contains(got, "witness hcmnext.fixture.gamma/v1 differs from the fresh compilation") {
			t.Errorf("a re-digested forged witness passed verification: %q", got)
		}

		flipped := honest
		flipped.Result = ResultComplete
		if got := strings.Join(Verify(goldenSnapshot(), flipped), "\n"); !strings.Contains(got, "does not match its own content") {
			t.Errorf("a flipped report result passed verification: %q", got)
		}

		dropped := mustCompile(t, goldenSnapshot())
		dropped.Witnesses = dropped.Witnesses[1:]
		if got := strings.Join(Verify(goldenSnapshot(), dropped), "\n"); !strings.Contains(got, "has no witness") {
			t.Errorf("a dropped witness passed verification: %q", got)
		}

		doubled := mustCompile(t, goldenSnapshot())
		doubled.Witnesses = append(doubled.Witnesses, doubled.Witnesses[0])
		if got := strings.Join(Verify(goldenSnapshot(), doubled), "\n"); !strings.Contains(got, "more than one witness") {
			t.Errorf("a duplicated witness passed verification: %q", got)
		}

		edited := mustCompile(t, goldenSnapshot())
		edited.Witnesses[0].Expiry = "2099-12-31"
		if got := strings.Join(Verify(goldenSnapshot(), edited), "\n"); !strings.Contains(got, "digest does not match its own content") {
			t.Errorf("an edited witness passed verification: %q", got)
		}
	})
}

// TestTodo_SLICE_016_Conformance runs against the live registries: exactly
// one witness per source-bound definition, set-equal to the compiled
// catalog, byte-identical across two runs, verified, and agreeing with the
// owning registries on facts this package does not decide itself.
func TestTodo_SLICE_016_Conformance(t *testing.T) {
	root := repoRoot(t)
	asOf := time.Now().UTC().Format("2006-01-02")
	snap, err := LoadSnapshot(root, asOf)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	report := mustCompile(t, snap)
	first, err := MarshalReport(report)
	if err != nil {
		t.Fatal(err)
	}
	again, err := LoadSnapshot(root, asOf)
	if err != nil {
		t.Fatalf("second LoadSnapshot: %v", err)
	}
	second, err := MarshalReport(mustCompile(t, again))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("two runs over unchanged registries produced different bytes")
	}
	if mismatches := Verify(snap, report); len(mismatches) != 0 {
		t.Fatalf("Verify on the live report: %v", mismatches)
	}

	if len(snap.Descriptors) == 0 || len(report.Witnesses) != len(snap.Descriptors) || len(snap.Catalog) != len(snap.Descriptors) {
		t.Fatalf("%d witnesses for %d descriptors and %d compiled definitions", len(report.Witnesses), len(snap.Descriptors), len(snap.Catalog))
	}
	catalog := map[string]bool{}
	for _, c := range snap.Catalog {
		catalog[c.Definition] = true
	}
	for _, w := range report.Witnesses {
		if !catalog[w.Definition] {
			t.Errorf("witness %s has no compiled definition", w.Definition)
		}
		if w.Result == ResultIncomplete && len(w.Defects) == 0 {
			t.Errorf("witness %s is incomplete without a named defect", w.Definition)
		}
	}
	assertTotalsReconcile(t, report)
	assertNoUnsourcedLive(t, snap)

	generic := map[string]bool{}
	for _, d := range snap.IntentDispositions {
		w := witnessFor(t, report, d.Definition)
		switch d.Category {
		case categoryGeneric:
			generic[d.Definition] = true
			if !hasDefect(w.Defects, DefectAggregateOnly, ClassEndpoint, "ENDPOINT|"+d.Definition+"|generic_lifecycle") {
				t.Errorf("%s is GENERIC_INTENT_LIFECYCLE_ONLY in the manifest but its witness does not say aggregate-only", d.Definition)
			}
		case categoryNoEndpoint:
			if got := classState(w, ClassEndpoint); got != StateJustified {
				t.Errorf("%s has a justified no-endpoint disposition but its endpoint class is %s", d.Definition, got)
			}
		}
	}
	for _, claim := range snap.Claims {
		if !generic[claim.DefinitionRef] {
			continue
		}
		for _, gap := range snap.BindingGaps {
			if gap.CapabilityID == claim.CapabilityID && gap.Kind == "AMBIGUOUS_WIRE_METHOD" &&
				!hasDefect(witnessFor(t, report, claim.DefinitionRef).Defects, DefectAggregateOnly, ClassEndpoint, "ENDPOINT|"+gap.CapabilityID+"|"+gap.Subject) {
				t.Errorf("binding gap %s|%s is missing from %s's witness", gap.Kind, gap.Subject, claim.DefinitionRef)
			}
		}
	}
	for _, s := range snap.Slices {
		for _, def := range s.Intents {
			if w := witnessFor(t, report, def); classState(w, ClassSlice) == StateAbsent {
				t.Errorf("%s is named by slice %s but its witness reports the slice edge absent", def, s.SliceID)
			}
		}
	}

	t.Logf("live closure as of %s: %d definitions, %d complete, %d incomplete, %d orphan edges; by class: %v; by defect: %v",
		asOf, report.Totals.Definitions, report.Totals.Complete, report.Totals.Incomplete, report.Totals.Orphans,
		report.Totals.ByClassState, report.Totals.ByDefectCode)
}

// assertNoUnsourcedLive guards the loader: every registry this package
// reads exists in the live tree today, so an UNSOURCED class would mean a
// path broke, not that the repository lacks the registry.
func assertNoUnsourcedLive(t *testing.T, snap Snapshot) {
	t.Helper()
	if len(snap.Unsourced) != 0 {
		t.Errorf("live registries reported unsourced classes %v", snap.Unsourced)
	}
	if len(snap.TestExists) == 0 || len(snap.Todos) == 0 || len(snap.Endpoints) == 0 || len(snap.Engines) == 0 || len(snap.Scenarios) == 0 {
		t.Errorf("a live registry loaded empty: tests=%d todos=%d endpoints=%d engines=%d scenarios=%d",
			len(snap.TestExists), len(snap.Todos), len(snap.Endpoints), len(snap.Engines), len(snap.Scenarios))
	}
}

// TestTodo_SLICE_016_Mutation plants each defect into the complete fixture
// and proves the fixture really lacked it, the mutant raises exactly that
// code, class and identity, the affected witness drops to incomplete and
// the report digest moves.
func TestTodo_SLICE_016_Mutation(t *testing.T) {
	base := mustCompile(t, completeSnapshot())
	if base.Result != ResultComplete {
		t.Fatalf("base fixture is not complete:%s", describeDefects(allDefects(base)))
	}
	for _, m := range mutants() {
		t.Run(m.name, func(t *testing.T) {
			if hasDefect(allDefects(base), m.code, m.class, m.identity) {
				t.Fatalf("the base fixture already carries %s %s; the mutant would prove nothing", m.code, m.identity)
			}
			s := completeSnapshot()
			m.mutate(&s)
			report := mustCompile(t, s)
			pool := report.Orphans
			if m.definition != "" {
				w := witnessFor(t, report, m.definition)
				pool = w.Defects
				if w.Result != ResultIncomplete {
					t.Errorf("witness %s stayed %s", m.definition, w.Result)
				}
				if w.Digest == witnessFor(t, base, m.definition).Digest {
					t.Error("the witness digest did not move")
				}
			}
			if !hasDefect(pool, m.code, m.class, m.identity) {
				t.Fatalf("want %s %s %s; got:%s", m.code, m.class, m.identity, describeDefects(allDefects(report)))
			}
			if report.Result != ResultIncomplete || report.Digest == base.Digest {
				t.Errorf("report result %s digest moved=%v", report.Result, report.Digest != base.Digest)
			}
		})
	}
}
