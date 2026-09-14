package closurewitness

import (
	"strings"
	"testing"
)

func TestSummaryIsGeneratedFromWitnesses(t *testing.T) {
	report := mustCompile(t, goldenSnapshot())
	summary := Summary(report)
	for _, want := range []string{
		"closure witnesses as of " + fixtureAsOf + ": " + ResultIncomplete,
		"definitions=3 complete=1 incomplete=2 orphan_edges=1",
		"ENDPOINT    ABSENT=1 AGGREGATE_ONLY=1 BOUND=1\n",
		"defect EDGE_ORPHAN=1\n",
		ResultComplete + " " + defAlpha + " defects=0 expiry=2026-12-31\n",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary lacks %q:\n%s", want, summary)
		}
	}
}

func TestVerifyRefusesAnUnusableSnapshot(t *testing.T) {
	report := mustCompile(t, completeSnapshot())
	bad := completeSnapshot()
	bad.AsOf = "yesterday"
	got := Verify(bad, report)
	if len(got) != 1 || !strings.Contains(got[0], "as-of") {
		t.Fatalf("Verify with an unusable as-of = %v", got)
	}
}

func TestVerifyRejectsAWitnessForAnUnknownDefinition(t *testing.T) {
	report := mustCompile(t, goldenSnapshot())
	got := strings.Join(Verify(completeSnapshot(), report), "\n")
	if !strings.Contains(got, "hcmnext.fixture.gamma/v1 is not a source-bound definition") {
		t.Fatalf("a witness outside the snapshot passed: %q", got)
	}
}

func TestReleaseToGateJoin(t *testing.T) {
	for release, gate := range map[string]string{"P1A": "P1A", "P1B": "P1B", "CONFORMANCE": "NONE", "UNSPECIFIED": ""} {
		if got := gateForRelease(release); got != gate {
			t.Errorf("gateForRelease(%s) = %q, want %q", release, got, gate)
		}
	}
	s := completeSnapshot()
	s.Catalog[0].Release = "UNSPECIFIED"
	w := witnessFor(t, mustCompile(t, s), defAlpha)
	if !hasDefect(w.Defects, DefectStale, ClassPhaseGate, "PHASE_GATE|"+defAlpha+"|release|UNSPECIFIED") {
		t.Fatalf("an unjoinable release was not reported:%s", describeDefects(w.Defects))
	}
}

// TestRuleBranchesNameExactIdentities covers the rule branches the matrix
// mutants do not: each case plants one registry shape and requires its
// exact defect.
func TestRuleBranchesNameExactIdentities(t *testing.T) {
	alphaBare := bareID(defAlpha)
	cases := []mutant{
		{name: "empty source digest", mutate: func(s *Snapshot) { s.Descriptors[0].RowDigest = "" },
			code: DefectStale, class: ClassSource, identity: "SOURCE|" + defAlpha + "|row_digest", definition: defAlpha},
		{name: "duplicate compiled definition", mutate: func(s *Snapshot) { s.Catalog = append(s.Catalog, s.Catalog[0]) },
			code: DefectDuplicate, class: ClassSource, identity: "SOURCE|" + defAlpha + "|catalog", definition: defAlpha},
		{name: "descriptor names no phase", mutate: func(s *Snapshot) { s.Descriptors[0].Phase = "" },
			code: DefectAbsent, class: ClassPhaseGate, identity: "PHASE_GATE|" + defAlpha + "|descriptor_phase", definition: defAlpha},
		{name: "ceiling row missing", mutate: func(s *Snapshot) { s.Ceiling = s.Ceiling[1:] },
			code: DefectReverseAbsent, class: ClassPhaseGate, identity: "PHASE_GATE|" + defAlpha + "|ceiling", definition: defAlpha},
		{name: "ceiling row duplicated", mutate: func(s *Snapshot) { s.Ceiling = append(s.Ceiling, s.Ceiling[0]) },
			code: DefectDuplicate, class: ClassPhaseGate, identity: "PHASE_GATE|" + defAlpha + "|ceiling", definition: defAlpha},
		{name: "no model binding at all", mutate: func(s *Snapshot) { s.ModelBindings = s.ModelBindings[1:] },
			code: DefectAbsent, class: ClassModel, identity: "MODEL|" + defAlpha, definition: defAlpha},
		{name: "model binding with no entity", mutate: func(s *Snapshot) { s.ModelBindings[0].Entities = nil },
			code: DefectAbsent, class: ClassModel, identity: "MODEL|" + defAlpha + "|entities", definition: defAlpha},
		{name: "two model bindings", mutate: func(s *Snapshot) { s.ModelBindings = append(s.ModelBindings, s.ModelBindings[0]) },
			code: DefectDuplicate, class: ClassModel, identity: "MODEL|" + defAlpha, definition: defAlpha},
		{name: "no engine responsibility", mutate: func(s *Snapshot) { s.Engines = s.Engines[1:] },
			code: DefectAbsent, class: ClassEngine, identity: "ENGINE|" + defAlpha, definition: defAlpha},
		{name: "engine computation has two owners", mutate: func(s *Snapshot) { s.Engines[0].Findings = []string{"DUPLICATE_RESPONSIBILITY"} },
			code: DefectDuplicate, class: ClassEngine, identity: "ENGINE|" + defAlpha + "|internal/engines/snapshot|snapshot", definition: defAlpha},
		{name: "engine package missing", mutate: func(s *Snapshot) { s.Engines[0].Findings = []string{"MISSING_PACKAGE"} },
			code: DefectStale, class: ClassEngine, identity: "ENGINE|" + defAlpha + "|internal/engines/snapshot|snapshot", definition: defAlpha},
		{name: "no capability claim", mutate: func(s *Snapshot) { s.Claims = s.Claims[1:] },
			code: DefectAbsent, class: ClassHandler, identity: "HANDLER|" + defAlpha, definition: defAlpha},
		{name: "two capabilities claim the definition", mutate: func(s *Snapshot) {
			s.Claims = append(s.Claims, ClaimRow{CapabilityID: "hcmnext.fixture.alpha_extra", Version: 1, DefinitionRef: defAlpha})
		}, code: DefectDuplicate, class: ClassCapability, identity: "CAPABILITY|" + defAlpha, definition: defAlpha},
		{name: "one capability claimed twice", mutate: func(s *Snapshot) {
			s.Claims = append(s.Claims, ClaimRow{CapabilityID: alphaBare, Version: 1, DefinitionRef: defAlpha})
		}, code: DefectDuplicate, class: ClassCapability, identity: "CAPABILITY|" + alphaBare + "|claims", definition: defAlpha},
		{name: "capability published at another version", mutate: func(s *Snapshot) { s.Capabilities[0].Version = 2 },
			code: DefectStale, class: ClassCapability, identity: "CAPABILITY|" + defAlpha + "|" + alphaBare + "/v1", definition: defAlpha},
		{name: "no handler", mutate: func(s *Snapshot) {
			s.BindingEntries = s.BindingEntries[1:]
			s.BindingGaps = []BindingGapRow{{Kind: "NO_HANDLER", CapabilityID: alphaBare}, {Kind: "NO_WIRE_METHOD", CapabilityID: alphaBare}}
		}, code: DefectAbsent, class: ClassHandler, identity: "HANDLER|" + alphaBare, definition: defAlpha},
		{name: "no wire method", mutate: func(s *Snapshot) {
			s.BindingEntries = s.BindingEntries[1:]
			s.BindingGaps = []BindingGapRow{{Kind: "NO_HANDLER", CapabilityID: alphaBare}, {Kind: "NO_WIRE_METHOD", CapabilityID: alphaBare}}
		}, code: DefectAbsent, class: ClassEndpoint, identity: "ENDPOINT|" + alphaBare, definition: defAlpha},
		{name: "dangling handler symbol", mutate: func(s *Snapshot) {
			s.BindingEntries = s.BindingEntries[1:]
			s.BindingGaps = []BindingGapRow{{Kind: "MISSING_HANDLER_SYMBOL", CapabilityID: alphaBare, Subject: "pkg.gone"}}
		}, code: DefectStale, class: ClassHandler, identity: "HANDLER|" + alphaBare + "|pkg.gone", definition: defAlpha},
		{name: "blocked entry proves no wire method", mutate: func(s *Snapshot) {
			s.BindingEntries = s.BindingEntries[1:]
			s.BindingGaps = []BindingGapRow{{Kind: "MISSING_HANDLER_SYMBOL", CapabilityID: alphaBare, Subject: "pkg.gone"}}
		}, code: DefectAbsent, class: ClassEndpoint, identity: "ENDPOINT|" + alphaBare + "|entry", definition: defAlpha},
		{name: "unknown wire method", mutate: func(s *Snapshot) {
			s.BindingEntries = s.BindingEntries[1:]
			s.BindingGaps = []BindingGapRow{{Kind: "UNKNOWN_WIRE_METHOD", CapabilityID: alphaBare, Subject: "svc/Gone"}}
		}, code: DefectAbsent, class: ClassHandler, identity: "HANDLER|" + alphaBare + "|entry", definition: defAlpha},
		{name: "claim has no model binding", mutate: func(s *Snapshot) {
			s.BindingGaps = []BindingGapRow{{Kind: "NO_MODEL_BINDING", CapabilityID: alphaBare, Subject: defAlpha}}
		}, code: DefectStale, class: ClassModel, identity: "MODEL|" + defAlpha + "|capability:" + alphaBare, definition: defAlpha},
		{name: "claim without capability gap", mutate: func(s *Snapshot) {
			s.BindingGaps = []BindingGapRow{{Kind: "CLAIM_WITHOUT_CAPABILITY", CapabilityID: alphaBare, Subject: alphaBare + "/v9"}}
		}, code: DefectStale, class: ClassCapability, identity: "CAPABILITY|" + alphaBare + "|CLAIM_WITHOUT_CAPABILITY|" + alphaBare + "/v9", definition: defAlpha},
		{name: "two bound entries", mutate: func(s *Snapshot) { s.BindingEntries = append(s.BindingEntries, s.BindingEntries[0]) },
			code: DefectDuplicate, class: ClassHandler, identity: "HANDLER|" + alphaBare + "|entries", definition: defAlpha},
		{name: "no endpoint disposition", mutate: func(s *Snapshot) { s.IntentDispositions = s.IntentDispositions[1:] },
			code: DefectReverseAbsent, class: ClassEndpoint, identity: "ENDPOINT|" + defAlpha + "|disposition", definition: defAlpha},
		{name: "two endpoint dispositions", mutate: func(s *Snapshot) { s.IntentDispositions = append(s.IntentDispositions, s.IntentDispositions[0]) },
			code: DefectDuplicate, class: ClassEndpoint, identity: "ENDPOINT|" + defAlpha + "|disposition", definition: defAlpha},
		{name: "unjustified no-endpoint disposition", mutate: func(s *Snapshot) {
			s.IntentDispositions[0] = IntentDispositionRow{Definition: defAlpha, Category: categoryNoEndpoint}
		}, code: DefectAbsent, class: ClassEndpoint, identity: "ENDPOINT|" + defAlpha + "|justification", definition: defAlpha},
		{name: "typed disposition with two routes", mutate: func(s *Snapshot) {
			s.IntentDispositions[0].ServingEndpoints = append(s.IntentDispositions[0].ServingEndpoints, "hcmnext.fixture.v1.FixtureService/Beta")
		}, code: DefectDuplicate, class: ClassEndpoint, identity: "ENDPOINT|" + defAlpha + "|typed", definition: defAlpha},
		{name: "typed disposition with no route", mutate: func(s *Snapshot) { s.IntentDispositions[0].ServingEndpoints = nil },
			code: DefectAbsent, class: ClassEndpoint, identity: "ENDPOINT|" + defAlpha + "|typed", definition: defAlpha},
		{name: "manifest row does not accept the definition", mutate: func(s *Snapshot) { s.Endpoints[0].AcceptedDefinitions = []string{defBeta} },
			code: DefectStale, class: ClassEndpoint, identity: "ENDPOINT|" + defAlpha + "|hcmnext.fixture.v1.FixtureService/Alpha", definition: defAlpha},
		{name: "unknown disposition category", mutate: func(s *Snapshot) { s.IntentDispositions[0].Category = "SOMETIMES" },
			code: DefectStale, class: ClassEndpoint, identity: "ENDPOINT|" + defAlpha + "|category|SOMETIMES", definition: defAlpha},
		{name: "two scenario matrices", mutate: func(s *Snapshot) { s.Scenarios = append(s.Scenarios, s.Scenarios[0]) },
			code: DefectDuplicate, class: ClassScenario, identity: "SCENARIO|" + defAlpha, definition: defAlpha},
		{name: "empty scenario matrix", mutate: func(s *Snapshot) { s.Scenarios[0].ScenarioIDs = nil },
			code: DefectAbsent, class: ClassScenario, identity: "SCENARIO|" + defAlpha + "|scenarios", definition: defAlpha},
		{name: "scenario generation finding", mutate: func(s *Snapshot) { s.Scenarios[0].Findings = []string{"MISSING_TEMPLATE: x"} },
			code: DefectStale, class: ClassScenario, identity: "SCENARIO|" + defAlpha + "|MISSING_TEMPLATE: x", definition: defAlpha},
		{name: "no todo at all", mutate: func(s *Snapshot) { s.TodoClaims = s.TodoClaims[1:] },
			code: DefectAbsent, class: ClassTodo, identity: "TODO|" + defAlpha, definition: defAlpha},
		{name: "claimed todo absent from backlog", mutate: func(s *Snapshot) { s.Todos = s.Todos[1:] },
			code: DefectStale, class: ClassTodo, identity: "TODO|" + defAlpha + "|FIX-001", definition: defAlpha},
		{name: "backlog todo id duplicated", mutate: func(s *Snapshot) { s.Todos = append(s.Todos, s.Todos[0]) },
			code: DefectDuplicate, class: ClassTodo, identity: "TODO|" + defAlpha + "|FIX-001", definition: defAlpha},
		{name: "coverage registry row missing", mutate: func(s *Snapshot) { s.Coverage = s.Coverage[1:] },
			code: DefectReverseAbsent, class: ClassTodo, identity: "TODO|" + defAlpha + "|coverage_registry", definition: defAlpha},
		{name: "coverage registry row duplicated", mutate: func(s *Snapshot) { s.Coverage = append(s.Coverage, s.Coverage[0]) },
			code: DefectDuplicate, class: ClassTodo, identity: "TODO|" + defAlpha + "|coverage_registry", definition: defAlpha},
		{name: "coverage registry misses a fresh claim", mutate: func(s *Snapshot) { s.Coverage[0].DirectTodos = nil },
			code: DefectStale, class: ClassTodo, identity: "TODO|" + defAlpha + "|FIX-001|coverage_registry", definition: defAlpha},
		{name: "coverage registry misses a cited test", mutate: func(s *Snapshot) { s.Coverage[0].Tests = nil },
			code: DefectStale, class: ClassTest, identity: "TEST|" + defAlpha + "|TestFixtureAlpha|coverage_registry", definition: defAlpha},
		{name: "coverage registry lists an uncited test", mutate: func(s *Snapshot) { s.Coverage[0].Tests = append(s.Coverage[0].Tests, "TestOld") },
			code: DefectStale, class: ClassTest, identity: "TEST|" + defAlpha + "|TestOld|coverage_registry", definition: defAlpha},
		{name: "primary test missing", mutate: func(s *Snapshot) { s.Todos[0].PrimaryTest = "TestNeverWritten" },
			code: DefectStale, class: ClassTest, identity: "TEST|" + defAlpha + "|FIX-001|TestNeverWritten", definition: defAlpha},
		{name: "no evidence digest", mutate: func(s *Snapshot) { s.Todos[0].EvidenceDigest = "" },
			code: DefectAbsent, class: ClassEvidence, identity: "EVIDENCE|" + defAlpha + "|FIX-001|digest", definition: defAlpha},
		{name: "waiver with no valid expiry", mutate: func(s *Snapshot) { s.Waivers[0].Expiry = "someday" },
			code: DefectStale, class: ClassEvidence, identity: "EVIDENCE|" + defAlpha + "|FIX-001|waiver|missing commit/branch identity", definition: defAlpha},
		{name: "no test survives", mutate: func(s *Snapshot) {
			delete(s.TestExists, "TestFixtureAlpha")
		}, code: DefectAbsent, class: ClassTest, identity: "TEST|" + defAlpha, definition: defAlpha},
	}
	orphans := []mutant{
		{name: "compiled definition without source", mutate: func(s *Snapshot) { s.Catalog = append(s.Catalog, CatalogRow{Definition: "x/v1"}) },
			code: DefectOrphan, class: ClassSource, identity: "SOURCE|x/v1|catalog"},
		{name: "ceiling row without source", mutate: func(s *Snapshot) { s.Ceiling = append(s.Ceiling, CeilingRow{Definition: "x/v1"}) },
			code: DefectOrphan, class: ClassPhaseGate, identity: "PHASE_GATE|x/v1|ceiling"},
		{name: "model binding without source", mutate: func(s *Snapshot) { s.ModelBindings = append(s.ModelBindings, ModelBindingRow{Definition: "x/v1"}) },
			code: DefectOrphan, class: ClassModel, identity: "MODEL|x/v1"},
		{name: "model gap without source", mutate: func(s *Snapshot) {
			s.ModelGaps = append(s.ModelGaps, ModelGapRow{Definition: "x/v1", Element: "read_property"})
		},
			code: DefectOrphan, class: ClassModel, identity: "MODEL|x/v1|read_property"},
		{name: "engine without source", mutate: func(s *Snapshot) {
			s.Engines = append(s.Engines, EngineRow{Intent: "x", Computation: "c", Package: "p"})
		},
			code: DefectOrphan, class: ClassEngine, identity: "ENGINE|x|p|c"},
		{name: "claim naming no definition", mutate: func(s *Snapshot) { s.Claims = append(s.Claims, ClaimRow{CapabilityID: "cap.x", Version: 1}) },
			code: DefectOrphan, class: ClassCapability, identity: "CAPABILITY|cap.x|definition"},
		{name: "claim naming an unknown definition", mutate: func(s *Snapshot) {
			s.Claims = append(s.Claims, ClaimRow{CapabilityID: "cap.y", Version: 1, DefinitionRef: "y/v1"})
		}, code: DefectOrphan, class: ClassCapability, identity: "CAPABILITY|cap.y|y/v1"},
		{name: "published capability without claim", mutate: func(s *Snapshot) {
			s.Capabilities = append(s.Capabilities, CapabilityRow{CapabilityID: "cap.z", Version: 1})
		},
			code: DefectOrphan, class: ClassCapability, identity: "CAPABILITY|cap.z|claim"},
		{name: "unbound wire method", mutate: func(s *Snapshot) {
			s.BindingGaps = append(s.BindingGaps, BindingGapRow{Kind: "WIRE_METHOD_UNBOUND", Subject: "svc/M", OwnerTodo: "PROTO-010"})
		},
			code: DefectOrphan, class: ClassEndpoint, identity: "ENDPOINT|WIRE_METHOD_UNBOUND||svc/M"},
		{name: "handler bound twice", mutate: func(s *Snapshot) {
			s.BindingGaps = append(s.BindingGaps, BindingGapRow{Kind: "HANDLER_BOUND_TWICE", Subject: "pkg.h"})
		},
			code: DefectOrphan, class: ClassHandler, identity: "HANDLER|HANDLER_BOUND_TWICE||pkg.h"},
		{name: "unclaimed capability's model gap", mutate: func(s *Snapshot) {
			s.BindingGaps = append(s.BindingGaps, BindingGapRow{Kind: "NO_MODEL_BINDING", CapabilityID: "cap.r"})
		},
			code: DefectOrphan, class: ClassModel, identity: "MODEL|NO_MODEL_BINDING|cap.r|"},
		{name: "unclaimed capability gap", mutate: func(s *Snapshot) {
			s.BindingGaps = append(s.BindingGaps, BindingGapRow{Kind: "UNCLAIMED_CAPABILITY", CapabilityID: "cap.u"})
		},
			code: DefectOrphan, class: ClassCapability, identity: "CAPABILITY|UNCLAIMED_CAPABILITY|cap.u|"},
		{name: "disposition without source", mutate: func(s *Snapshot) {
			s.IntentDispositions = append(s.IntentDispositions, IntentDispositionRow{Definition: "x/v1"})
		},
			code: DefectOrphan, class: ClassEndpoint, identity: "ENDPOINT|x/v1|disposition"},
		{name: "endpoint accepting an unknown definition", mutate: func(s *Snapshot) {
			s.Endpoints[0].AcceptedDefinitions = append(s.Endpoints[0].AcceptedDefinitions, "x/v1")
		},
			code: DefectOrphan, class: ClassEndpoint, identity: "ENDPOINT|hcmnext.fixture.v1.FixtureService/Alpha|x/v1"},
		{name: "served endpoint accepting nothing", mutate: func(s *Snapshot) {
			s.Endpoints = append(s.Endpoints, EndpointRow{EndpointID: "svc/Discover", Disposition: "SERVED"})
		},
			code: DefectOrphan, class: ClassEndpoint, identity: "ENDPOINT|svc/Discover"},
		{name: "scenario matrix without source", mutate: func(s *Snapshot) { s.Scenarios = append(s.Scenarios, ScenarioRow{Definition: "x/v1"}) },
			code: DefectOrphan, class: ClassScenario, identity: "SCENARIO|x/v1"},
		{name: "todo claim on an unknown intent", mutate: func(s *Snapshot) {
			s.TodoClaims = append(s.TodoClaims, TodoClaimRow{Intent: "x", TodoID: "FIX-009", Kind: ClaimDirect})
		},
			code: DefectOrphan, class: ClassTodo, identity: "TODO|FIX-009|x"},
		{name: "coverage row for an unknown intent", mutate: func(s *Snapshot) { s.Coverage = append(s.Coverage, CoverageRow{Intent: "x"}) },
			code: DefectOrphan, class: ClassTodo, identity: "TODO|coverage_registry|x"},
	}
	base := allDefects(mustCompile(t, completeSnapshot()))
	for _, m := range append(cases, orphans...) {
		t.Run(m.name, func(t *testing.T) {
			if hasDefect(base, m.code, m.class, m.identity) {
				t.Fatalf("base fixture already carries %s", m.identity)
			}
			s := completeSnapshot()
			m.mutate(&s)
			report := mustCompile(t, s)
			pool := report.Orphans
			if m.definition != "" {
				pool = witnessFor(t, report, m.definition).Defects
			}
			if !hasDefect(pool, m.code, m.class, m.identity) {
				t.Fatalf("want %s %s; got:%s", m.code, m.identity, describeDefects(allDefects(report)))
			}
		})
	}
}

func TestJustifiedDispositionIsNotADefect(t *testing.T) {
	s := completeSnapshot()
	s.IntentDispositions[0] = IntentDispositionRow{Definition: defAlpha, Category: categoryEvent, Justification: "SCHEDULE initiator only"}
	w := witnessFor(t, mustCompile(t, s), defAlpha)
	if got := classState(w, ClassEndpoint); got != StateJustified {
		t.Fatalf("endpoint class = %s, want JUSTIFIED_ABSENT:%s", got, describeDefects(w.Defects))
	}
	if w.Result != ResultComplete {
		t.Fatalf("a justified disposition alone blocked closure:%s", describeDefects(w.Defects))
	}
}

func TestUnsourcedClassesSuppressTheirOwnRuleNoise(t *testing.T) {
	s := Snapshot{AsOf: fixtureAsOf, TestExists: map[string]bool{}, Unsourced: Classes()}
	s.Descriptors = []DescriptorRow{{Definition: defAlpha, DisplayName: "Alpha", Phase: "GATE_A", RowDigest: "sha256:a"}}
	s.Catalog = []CatalogRow{{Definition: "orphan/v1"}}
	report := mustCompile(t, s)
	w := witnessFor(t, report, defAlpha)
	if len(w.Defects) != len(Classes()) {
		t.Fatalf("want exactly one UNSOURCED defect per class, got:%s", describeDefects(w.Defects))
	}
	for _, d := range w.Defects {
		if d.Code != DefectUnsourced {
			t.Errorf("unsourced class emitted %s", d.Code)
		}
	}
	if len(report.Orphans) != 0 {
		t.Errorf("orphans reported for unsourced classes:%s", describeDefects(report.Orphans))
	}
}

func TestValidDate(t *testing.T) {
	for _, ok := range []string{"2026-09-13", "2000-01-01", "2099-12-31"} {
		if !validDate(ok) {
			t.Errorf("validDate(%q) = false", ok)
		}
	}
	for _, bad := range []string{"2026-00-10", "2026-01-00", "2026-9-13", "abcd-ef-gh"} {
		if validDate(bad) {
			t.Errorf("validDate(%q) = true", bad)
		}
	}
}
