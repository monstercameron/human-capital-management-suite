package closurewitness

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	fixtureAsOf = "2026-09-13"
	defAlpha    = "hcmnext.fixture.alpha/v1"
	defBeta     = "hcmnext.fixture.beta/v1"
)

// fixtureDefinition supplies every registry row one definition needs to
// close: a descriptor, a compiled definition, a ceiling row, a model
// binding, an engine responsibility, a published and claimed capability
// with one bound handler and wire method, a typed endpoint disposition and
// manifest row, a scenario matrix, one ticked todo with existing evidence,
// and a coverage row that agrees with it.
func fixtureDefinition(snap *Snapshot, def, display, todo, test string) {
	bare := bareID(def)
	wire := "hcmnext.fixture.v1.FixtureService/" + display
	handler := "internal/transport/fixture.(*server)." + display
	snap.Descriptors = append(snap.Descriptors, DescriptorRow{Definition: def, DisplayName: display, Phase: "GATE_A", RowDigest: "sha256:" + display})
	snap.Catalog = append(snap.Catalog, CatalogRow{Definition: def, DisplayName: display, Release: "P1A"})
	snap.Ceiling = append(snap.Ceiling, CeilingRow{Definition: def, Gate: "P1A", Disposition: "INCLUDE"})
	snap.ModelBindings = append(snap.ModelBindings, ModelBindingRow{Definition: def, Entities: []string{"Worker/v1"}})
	snap.Engines = append(snap.Engines, EngineRow{Intent: bare, Computation: "snapshot", Package: "internal/engines/snapshot"})
	snap.Capabilities = append(snap.Capabilities, CapabilityRow{CapabilityID: bare, Version: 1})
	snap.Claims = append(snap.Claims, ClaimRow{CapabilityID: bare, Version: 1, DefinitionRef: def})
	snap.BindingEntries = append(snap.BindingEntries, BindingEntryRow{CapabilityID: bare, Wire: wire, Handler: handler})
	snap.IntentDispositions = append(snap.IntentDispositions, IntentDispositionRow{Definition: def, Category: categoryTyped, Justification: "dedicated typed route", ServingEndpoints: []string{wire}})
	snap.Endpoints = append(snap.Endpoints, EndpointRow{EndpointID: wire, Disposition: "SERVED", AcceptedDefinitions: []string{def}})
	snap.Scenarios = append(snap.Scenarios, ScenarioRow{Definition: def, ScenarioIDs: []string{display + "-positive", display + "-forbidden"}})
	snap.Todos = append(snap.Todos, TodoRow{ID: todo, Done: true, PrimaryTest: test, EvidenceTests: []string{test}, EvidenceDigest: "sha256:evidence-" + todo, EvidenceDates: []string{"2026-09-01"}})
	snap.TodoClaims = append(snap.TodoClaims, TodoClaimRow{Intent: bare, TodoID: todo, Kind: ClaimDirect})
	snap.ContextTokens = append(snap.ContextTokens, ContextTokenRow{TodoID: todo, Field: "DIRECT", Token: display})
	snap.Coverage = append(snap.Coverage, CoverageRow{Intent: bare, State: maturityDefined, DirectTodos: []string{todo}, Tests: []string{test}})
	snap.TestExists[test] = true
}

// completeSnapshot is the fixture proving COMPLETE is reachable: two
// definitions in one verified product slice, every edge bound, one of them
// leaning on an unexpired evidence-freshness waiver.
func completeSnapshot() Snapshot {
	snap := Snapshot{AsOf: fixtureAsOf, TestExists: map[string]bool{}}
	fixtureDefinition(&snap, defAlpha, "Alpha", "FIX-001", "TestFixtureAlpha")
	fixtureDefinition(&snap, defBeta, "Beta", "FIX-002", "TestFixtureBeta")
	snap.Slices = []SliceRow{{
		SliceID: "fixture", Version: 1, DigestVerified: true,
		Intents:      []string{defAlpha, defBeta},
		Capabilities: []string{bareID(defAlpha), bareID(defBeta)},
	}}
	snap.Waivers = []WaiverRow{{TodoID: "FIX-001", Issue: "missing commit/branch identity", Expiry: "2026-12-31"}}
	return snap
}

func mustCompile(t *testing.T, snap Snapshot) Report {
	t.Helper()
	report, err := Compile(snap)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return report
}

func witnessFor(t *testing.T, report Report, def string) Witness {
	t.Helper()
	var found []Witness
	for _, w := range report.Witnesses {
		if w.Definition == def {
			found = append(found, w)
		}
	}
	if len(found) != 1 {
		t.Fatalf("definition %s has %d witnesses, want exactly 1", def, len(found))
	}
	return found[0]
}

func classState(w Witness, class EdgeClass) EdgeState {
	for _, cs := range w.Classes {
		if cs.Class == class {
			return cs.State
		}
	}
	return ""
}

// hasDefect reports whether defects carry one with exactly this code,
// class and identity.
func hasDefect(defects []Defect, code string, class EdgeClass, identity string) bool {
	for _, d := range defects {
		if d.Code == code && d.Class == class && d.Identity == identity {
			return true
		}
	}
	return false
}

func allDefects(report Report) []Defect {
	out := append([]Defect(nil), report.Orphans...)
	for _, w := range report.Witnesses {
		out = append(out, w.Defects...)
	}
	return out
}

func describeDefects(defects []Defect) string {
	var b strings.Builder
	for _, d := range defects {
		b.WriteString("\n  " + d.Code + " " + d.Identity + " -- " + d.Detail)
	}
	return b.String()
}

// repoRoot walks up from the test's working directory to go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		} else if !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("stat go.mod: %v", statErr)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test working directory")
		}
		dir = parent
	}
}
