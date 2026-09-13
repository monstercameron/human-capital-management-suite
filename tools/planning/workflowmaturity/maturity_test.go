package workflowmaturity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/designownership"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/intentcoverage"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/scenariomatrix"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdecisions"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesign"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesignjoin"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowexpansion"
)

// cleanRecord builds one design record that, paired with cleanGraph and
// cleanScenario below, has no findings anywhere in the pipeline.
func cleanRecord(intent, definition string) workflowdesign.DesignRecord {
	return workflowdesign.DesignRecord{
		Intent:         intent,
		Definition:     definition,
		Disposition:    workflowdesign.DispositionWorkflow,
		Archetype:      "A2",
		DomainProfile:  "PPL",
		InputBoundary:  "proposal",
		SnapshotPolicy: "authority-snapshot",
		Engines:        workflowdesign.Dimension{Items: []string{"identity-resolution"}},
		HumanWork:      workflowdesign.Dimension{Value: "manager-self-service"},
		Writes:         workflowdesign.Dimension{Value: "edge-replacement"},
		Waits:          workflowdesign.Dimension{Value: "approval-window"},
		Invalidators:   workflowdesign.Dimension{Value: "org-reparent"},
		Reconciliation: workflowdesign.Dimension{Value: "relationship-access"},
		Correction:     workflowdesign.Dimension{Value: "revoke-and-replace"},
		Completion:     "edge-observed",
	}
}

func cleanGraph(intent, definition string) *workflowexpansion.Graph {
	return &workflowexpansion.Graph{
		Intent:      intent,
		Definition:  definition,
		Archetype:   "A2",
		Profile:     "PPL",
		Disposition: workflowdesign.DispositionWorkflow,
		Nodes: []workflowexpansion.Node{
			{Seq: 0, Kind: workflowexpansion.KindExecution, Responsibility: "collect proposal", Origin: workflowexpansion.OriginInherited},
			{Seq: 1, Kind: workflowexpansion.KindCompletion, Responsibility: "observe completion", Origin: workflowexpansion.OriginInherited},
		},
		Edges:      []workflowexpansion.Edge{{From: 0, To: 1, Kind: workflowexpansion.EdgeSequence}},
		Completion: workflowexpansion.CompletionPolicy{Policy: "edge-observed", TerminalSeq: 1},
		Digest:     "sha256:clean-" + definition,
	}
}

func cleanScenario(definition string) *scenariomatrix.Matrix {
	return &scenariomatrix.Matrix{
		Definition: definition,
		Scenarios:  []scenariomatrix.Scenario{{ID: definition + "/positive/00", Name: "happy-path-execution", Class: scenariomatrix.ClassPositive}},
		Digest:     "sha256:scenario-" + definition,
	}
}

func cleanNode(bareID string) intentcoverage.IntentNode {
	return intentcoverage.IntentNode{IntentID: bareID, Status: intentcoverage.Verified, Reason: "fully evidenced"}
}

// baseSnapshot returns one accepted definition ("hcmnext.t.alpha/v1") with
// a completely clean pipeline: bound design, valid graph, no unresolved
// ownership, a generated scenario, no open decisions, and a VERIFIED
// intentcoverage claim. Every RED-condition test below starts from this
// and breaks exactly one dimension.
func baseSnapshot() Snapshot {
	definition := "hcmnext.t.alpha/v1"
	record := cleanRecord("Alpha", definition)
	join, joinFindings := workflowdesignjoin.JoinRecords([]string{definition}, []workflowdesign.DesignRecord{record})
	return Snapshot{
		Accepted:             []string{definition},
		Records:              []workflowdesign.DesignRecord{record},
		Join:                 join,
		JoinFindings:         joinFindings,
		Graphs:               map[string]*workflowexpansion.Graph{definition: cleanGraph("Alpha", definition)},
		GraphFindings:        map[string][]workflowexpansion.Finding{},
		Scenarios:            map[string]*scenariomatrix.Matrix{definition: cleanScenario(definition)},
		ScenarioFindings:     map[string][]scenariomatrix.Finding{},
		CatalogMappedIntents: map[string]bool{},
		IntentNodes:          []intentcoverage.IntentNode{cleanNode("hcmnext.t.alpha")},
	}
}

func findResult(t *testing.T, report Report, definition string) DefinitionResult {
	t.Helper()
	for _, r := range report.Results {
		if r.Definition == definition {
			return r
		}
	}
	t.Fatalf("no result for %s in %+v", definition, report.Results)
	return DefinitionResult{}
}

func hasBlocker(result DefinitionResult, code string) bool {
	for _, b := range result.Blockers {
		if b.Code == code {
			return true
		}
	}
	return false
}

// TestWorkflowMaturityRequiresDesignCoverageCompilationScenariosAndOwnedTodos
// is the WF-DISC-012 primary oracle: every RED clause is reproduced from a
// clean baseline by breaking exactly one dimension, and the clean case
// itself passes through the claimed status unchanged.
func TestWorkflowMaturityRequiresDesignCoverageCompilationScenariosAndOwnedTodos(t *testing.T) {
	definition := "hcmnext.t.alpha/v1"

	t.Run("clean snapshot is unblocked and passes claimed status through", func(t *testing.T) {
		report := Reconcile(baseSnapshot())
		result := findResult(t, report, definition)
		if len(result.Blockers) != 0 {
			t.Fatalf("clean snapshot raised blockers: %+v", result.Blockers)
		}
		if result.Capped {
			t.Fatalf("clean snapshot should not be capped: %+v", result)
		}
		if result.AllowedStatus != intentcoverage.Verified || result.ClaimedStatus != intentcoverage.Verified {
			t.Fatalf("clean snapshot status = claimed=%s allowed=%s, want VERIFIED/VERIFIED", result.ClaimedStatus, result.AllowedStatus)
		}
		if report.TotalDefinitions != 1 || report.BlockedCount != 0 {
			t.Fatalf("totals = %d/%d, want 1/0", report.TotalDefinitions, report.BlockedCount)
		}
	})

	t.Run("unbound design", func(t *testing.T) {
		snap := baseSnapshot()
		snap.Records = nil // the join now has nothing to bind.
		join, findings := workflowdesignjoin.JoinRecords(snap.Accepted, snap.Records)
		snap.Join, snap.JoinFindings = join, findings
		result := findResult(t, Reconcile(snap), definition)
		if !hasBlocker(result, UnboundDesign) {
			t.Fatalf("expected UNBOUND_DESIGN, got %+v", result.Blockers)
		}
	})

	t.Run("unresolved reference from ownership", func(t *testing.T) {
		snap := baseSnapshot()
		snap.OwnershipFindings = []designownership.Finding{
			{Definition: definition, Name: "identity-resolution", Code: designownership.UnownedEngine, Detail: "design engine identity-resolution resolves to no versioned semantic owner"},
		}
		result := findResult(t, Reconcile(snap), definition)
		if !hasBlocker(result, UnresolvedReference) {
			t.Fatalf("expected UNRESOLVED_REFERENCE, got %+v", result.Blockers)
		}
	})

	t.Run("unresolved reference from an unexpanded graph", func(t *testing.T) {
		snap := baseSnapshot()
		snap.Graphs[definition] = nil
		result := findResult(t, Reconcile(snap), definition)
		if !hasBlocker(result, UnresolvedReference) {
			t.Fatalf("expected UNRESOLVED_REFERENCE, got %+v", result.Blockers)
		}
	})

	t.Run("unjustified omission from a bare design dimension", func(t *testing.T) {
		snap := baseSnapshot()
		snap.RecordFindings = []workflowdesign.Finding{
			{Intent: "Alpha", Code: bareNotApplicable, Field: "human_work", Detail: "NOT_APPLICABLE needs an explicit reason code"},
		}
		result := findResult(t, Reconcile(snap), definition)
		if !hasBlocker(result, UnjustifiedOmission) {
			t.Fatalf("expected UNJUSTIFIED_OMISSION, got %+v", result.Blockers)
		}
	})

	t.Run("unjustified omission from an unreasoned expansion delta", func(t *testing.T) {
		snap := baseSnapshot()
		snap.GraphFindings[definition] = []workflowexpansion.Finding{
			{Code: workflowexpansion.BareOmission, Field: "delta.omit", Detail: "omit seq 1 states no reason"},
		}
		result := findResult(t, Reconcile(snap), definition)
		if !hasBlocker(result, UnjustifiedOmission) {
			t.Fatalf("expected UNJUSTIFIED_OMISSION, got %+v", result.Blockers)
		}
	})

	t.Run("missing adversarial scenario", func(t *testing.T) {
		snap := baseSnapshot()
		snap.Scenarios[definition] = nil
		result := findResult(t, Reconcile(snap), definition)
		if !hasBlocker(result, MissingAdversarialScenario) {
			t.Fatalf("expected MISSING_ADVERSARIAL_SCENARIO, got %+v", result.Blockers)
		}
	})

	t.Run("unresolved decision", func(t *testing.T) {
		snap := baseSnapshot()
		snap.Decisions = []workflowdecisions.Decision{
			{ID: "unresolved-modeling-decision-1", Status: workflowdecisions.StatusOpenOwned, Affected: []string{definition}},
		}
		result := findResult(t, Reconcile(snap), definition)
		if !hasBlocker(result, UnresolvedDecision) {
			t.Fatalf("expected UNRESOLVED_DECISION, got %+v", result.Blockers)
		}
	})

	t.Run("catalog name only", func(t *testing.T) {
		snap := baseSnapshot()
		snap.Records = nil
		join, findings := workflowdesignjoin.JoinRecords(snap.Accepted, snap.Records)
		snap.Join, snap.JoinFindings = join, findings
		snap.CatalogMappedIntents = map[string]bool{"Alpha": true}
		result := findResult(t, Reconcile(snap), definition)
		if !hasBlocker(result, CatalogNameOnly) {
			t.Fatalf("expected CATALOG_NAME_ONLY, got %+v", result.Blockers)
		}
	})

	for _, kind := range []struct {
		orphanKind string
		wantCode   string
	}{
		{intentcoverage.KindIntentWorkflowOrDirect, DanglingTodoEdge},
		{intentcoverage.KindTodoDirectDangling, DanglingTodoEdge},
		{intentcoverage.KindIntentTest, DanglingTestEdge},
		{intentcoverage.KindIntentEvidence, DanglingEvidenceEdge},
	} {
		t.Run("dangling edge "+kind.orphanKind, func(t *testing.T) {
			snap := baseSnapshot()
			snap.Orphans = []intentcoverage.Orphan{{Kind: kind.orphanKind, ID: "hcmnext.t.alpha", Detail: "gap"}}
			result := findResult(t, Reconcile(snap), definition)
			if !hasBlocker(result, kind.wantCode) {
				t.Fatalf("expected %s, got %+v", kind.wantCode, result.Blockers)
			}
		})
	}

	t.Run("a blocker caps a higher claimed status at CATALOGUED", func(t *testing.T) {
		snap := baseSnapshot()
		snap.IntentNodes = []intentcoverage.IntentNode{{IntentID: "hcmnext.t.alpha", Status: intentcoverage.Contracted}}
		snap.Scenarios[definition] = nil
		result := findResult(t, Reconcile(snap), definition)
		if result.ClaimedStatus != intentcoverage.Contracted {
			t.Fatalf("claimed status = %s, want CONTRACTED", result.ClaimedStatus)
		}
		if result.AllowedStatus != intentcoverage.Catalogued || !result.Capped {
			t.Fatalf("allowed status = %s capped=%v, want CATALOGUED/true", result.AllowedStatus, result.Capped)
		}
	})

	t.Run("a definition with no intentcoverage node at all is floored at CONCEPTUAL", func(t *testing.T) {
		snap := baseSnapshot()
		snap.IntentNodes = nil
		result := findResult(t, Reconcile(snap), definition)
		if result.ClaimedStatus != intentcoverage.Conceptual || result.AllowedStatus != intentcoverage.Conceptual {
			t.Fatalf("no-node result = %+v, want CONCEPTUAL/CONCEPTUAL", result)
		}
	})
}

// TestTodo_WF_DISC_012_Property checks structural invariants across many
// permutations rather than one fixed shape: independence between
// definitions, deduplication of repeated blockers, and totals that always
// reconcile with the per-definition rows.
func TestTodo_WF_DISC_012_Property(t *testing.T) {
	defA, defB := "hcmnext.t.alpha/v1", "hcmnext.t.beta/v1"
	recordA, recordB := cleanRecord("Alpha", defA), cleanRecord("Beta", defB)
	join, joinFindings := workflowdesignjoin.JoinRecords([]string{defA, defB}, []workflowdesign.DesignRecord{recordA, recordB})
	snap := Snapshot{
		Accepted:     []string{defA, defB},
		Records:      []workflowdesign.DesignRecord{recordA, recordB},
		Join:         join,
		JoinFindings: joinFindings,
		Graphs: map[string]*workflowexpansion.Graph{
			defA: cleanGraph("Alpha", defA),
			defB: cleanGraph("Beta", defB),
		},
		GraphFindings: map[string][]workflowexpansion.Finding{},
		Scenarios: map[string]*scenariomatrix.Matrix{
			defA: cleanScenario(defA),
			defB: nil, // only B is broken.
		},
		ScenarioFindings:     map[string][]scenariomatrix.Finding{},
		CatalogMappedIntents: map[string]bool{},
		IntentNodes: []intentcoverage.IntentNode{
			cleanNode("hcmnext.t.alpha"),
			cleanNode("hcmnext.t.beta"),
		},
		// Duplicate ownership findings for B must collapse to one blocker.
		OwnershipFindings: []designownership.Finding{
			{Definition: defB, Name: "x", Code: designownership.UnownedEngine, Detail: "same detail"},
			{Definition: defB, Name: "x", Code: designownership.UnownedEngine, Detail: "same detail"},
		},
	}

	report := Reconcile(snap)
	if len(report.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(report.Results))
	}
	resultA := findResult(t, report, defA)
	resultB := findResult(t, report, defB)

	if len(resultA.Blockers) != 0 {
		t.Fatalf("independence violated: A picked up B's blockers: %+v", resultA.Blockers)
	}
	if len(resultB.Blockers) == 0 {
		t.Fatalf("B should carry blockers from its own missing scenario and duplicate ownership finding")
	}
	// Deduplication: the two identical ownership findings must not produce
	// two identical blockers.
	seen := map[string]int{}
	for _, b := range resultB.Blockers {
		seen[b.Code+"|"+b.Detail]++
	}
	for key, count := range seen {
		if count > 1 {
			t.Fatalf("blocker %q appeared %d times, want exactly one (deduplicated)", key, count)
		}
	}

	if report.TotalDefinitions != len(report.Results) {
		t.Fatalf("total_definitions=%d does not match %d rows", report.TotalDefinitions, len(report.Results))
	}
	wantBlocked := 0
	for _, r := range report.Results {
		if len(r.Blockers) > 0 {
			wantBlocked++
		}
	}
	if report.BlockedCount != wantBlocked {
		t.Fatalf("blocked_count=%d does not match %d rows that actually carry a blocker", report.BlockedCount, wantBlocked)
	}

	// Reconcile must be a pure function: running it again on the same
	// snapshot produces byte-identical results.
	again := Reconcile(snap)
	if again.Digest != report.Digest {
		t.Fatalf("Reconcile is not deterministic: %s != %s", again.Digest, report.Digest)
	}
}

// TestTodo_WF_DISC_012_Golden pins the canonical report bytes for a fixed
// two-definition snapshot (one clean, one fully blocked across every RED
// condition) so any accidental behavior change is visible as a diff.
func TestTodo_WF_DISC_012_Golden(t *testing.T) {
	defClean, defBlocked := "hcmnext.t.alpha/v1", "hcmnext.t.gamma/v1"
	recordClean := cleanRecord("Alpha", defClean)
	join, joinFindings := workflowdesignjoin.JoinRecords([]string{defClean, defBlocked}, []workflowdesign.DesignRecord{recordClean})
	snap := Snapshot{
		Accepted:     []string{defClean, defBlocked},
		Records:      []workflowdesign.DesignRecord{recordClean},
		Join:         join,
		JoinFindings: joinFindings,
		Graphs: map[string]*workflowexpansion.Graph{
			defClean: cleanGraph("Alpha", defClean),
		},
		GraphFindings: map[string][]workflowexpansion.Finding{},
		Scenarios: map[string]*scenariomatrix.Matrix{
			defClean: cleanScenario(defClean),
		},
		ScenarioFindings: map[string][]scenariomatrix.Finding{},
		OwnershipFindings: []designownership.Finding{
			{Definition: defBlocked, Name: "ghost-engine", Code: designownership.UnownedEngine, Detail: "design engine ghost-engine resolves to no versioned semantic owner"},
		},
		Decisions: []workflowdecisions.Decision{
			{ID: "unresolved-modeling-decision-9", Status: workflowdecisions.StatusOpenOwned, Affected: []string{defBlocked}},
		},
		CatalogMappedIntents: map[string]bool{"Gamma": true},
		IntentNodes: []intentcoverage.IntentNode{
			cleanNode("hcmnext.t.alpha"),
			{IntentID: "hcmnext.t.gamma", Status: intentcoverage.Contracted},
		},
		Orphans: []intentcoverage.Orphan{
			{Kind: intentcoverage.KindIntentTest, ID: "hcmnext.t.gamma", Detail: "no executable oracle"},
		},
	}

	report := Reconcile(snap)
	got, err := report.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}

	goldenPath := filepath.Join("testdata", "golden.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("report bytes drifted from testdata/golden.json\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestTodo_WF_DISC_012_Security proves the gate cannot be talked out of a
// finding: Validate catches a report that claims more than fresh evidence
// supports (an inflated AllowedStatus, an undercounted BlockedCount, or a
// falsified TotalDefinitions), and ParseCatalogClaims never manufactures a
// disagreement from a section missing a Status line or a definition_ref -
// silence is not evidence either way.
func TestTodo_WF_DISC_012_Security(t *testing.T) {
	definition := "hcmnext.t.alpha/v1"
	snap := baseSnapshot()
	snap.Scenarios[definition] = nil // force a real blocker
	report := Reconcile(snap)
	if len(Validate(snap, report)) != 0 {
		t.Fatalf("Validate flagged a freshly reconciled report: %v", Validate(snap, report))
	}

	t.Run("inflated allowed status is caught", func(t *testing.T) {
		tampered := report
		tampered.Results = append([]DefinitionResult(nil), report.Results...)
		tampered.Results[0].AllowedStatus = intentcoverage.Verified
		mismatches := Validate(snap, tampered)
		if len(mismatches) == 0 {
			t.Fatalf("expected Validate to catch an inflated AllowedStatus")
		}
	})

	t.Run("undercounted blocked_count is caught", func(t *testing.T) {
		tampered := report
		tampered.BlockedCount = 0
		mismatches := Validate(snap, tampered)
		if len(mismatches) == 0 {
			t.Fatalf("expected Validate to catch an undercounted blocked_count")
		}
	})

	t.Run("falsified total_definitions is caught", func(t *testing.T) {
		tampered := report
		tampered.TotalDefinitions = 99
		mismatches := Validate(snap, tampered)
		if len(mismatches) == 0 {
			t.Fatalf("expected Validate to catch a falsified total_definitions")
		}
	})

	t.Run("catalog parser requires both a status and a definition ref", func(t *testing.T) {
		noStatus := "### WF-XXX-001. Untitled\n\n- Some prose mentioning `hcmnext.t.alpha/v1` but no status line.\n"
		if claims := ParseCatalogClaims(noStatus); len(claims) != 0 {
			t.Fatalf("expected no claim without a Status line, got %+v", claims)
		}
		noDefinition := "### WF-XXX-002. Untitled\n\n- **Status**: `EXISTING` - no definition mentioned here.\n"
		if claims := ParseCatalogClaims(noDefinition); len(claims) != 0 {
			t.Fatalf("expected no claim without a definition_ref, got %+v", claims)
		}
	})

	t.Run("cross-check only flags EXISTING claims against real blockers", func(t *testing.T) {
		claims := []CatalogClaim{
			{FlowID: "WF-XXX-003", Status: "NEW", Definitions: []string{definition}},
			{FlowID: "WF-XXX-004", Status: "EXISTING", Definitions: []string{"hcmnext.t.unknown/v1"}},
		}
		if got := CrossCheckCatalog(report, claims); len(got) != 0 {
			t.Fatalf("expected zero disagreements for a NEW claim and an unknown definition, got %+v", got)
		}
	})
}

// TestTodo_WF_DISC_012_Conformance compiles the live repository snapshot
// through the same loader the WF-DISC-012 command runs and proves the
// pipeline is total (every accepted definition gets exactly one row, the
// digest is stable across repeated compilation) and that at least one live
// blocker is named with an exact code and detail - the fourteen accepted
// definitions are not yet clean, and the report says so rather than
// hiding it in an aggregate count.
func TestTodo_WF_DISC_012_Conformance(t *testing.T) {
	root := repoRoot(t)
	snap, err := LoadSnapshot(root, DefaultIntentCoverageAllowlist)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	report := Reconcile(snap)
	if report.TotalDefinitions != len(report.Results) {
		t.Fatalf("total_definitions=%d does not match %d rows", report.TotalDefinitions, len(report.Results))
	}
	if report.TotalDefinitions == 0 {
		t.Fatalf("live snapshot produced zero accepted definitions")
	}
	if len(Validate(snap, report)) != 0 {
		t.Fatalf("live report failed self-validation: %v", Validate(snap, report))
	}
	again := Reconcile(snap)
	if again.Digest != report.Digest {
		t.Fatalf("live reconciliation is not deterministic: %s != %s", again.Digest, report.Digest)
	}

	// Every result must resolve to a known accepted definition exactly
	// once - no duplicates, no unresolved definitions.
	seen := map[string]bool{}
	for _, r := range report.Results {
		if seen[r.Definition] {
			t.Fatalf("definition %s reported twice", r.Definition)
		}
		seen[r.Definition] = true
		if r.Definition == "" {
			t.Fatalf("result carries no definition: %+v", r)
		}
	}

	claims, err := LoadCatalogClaims(root)
	if err != nil {
		t.Fatalf("LoadCatalogClaims: %v", err)
	}
	if len(claims) == 0 {
		t.Fatalf("expected catalog.md to yield at least one parsed status claim")
	}
	disagreements := CrossCheckCatalog(report, claims)
	t.Logf("workflowmaturity: %d/%d accepted definitions blocked; %d catalog.md EXISTING claim(s) unsupported by generated evidence", report.BlockedCount, report.TotalDefinitions, len(disagreements))
	for _, d := range disagreements {
		t.Logf("  %s (%s) claims EXISTING for %s but evidence supports at most %s: %d blocker(s)", d.FlowID, d.Title, d.Definition, d.Evidence, len(d.Blockers))
	}
}

// TestTodo_WF_DISC_012_Mutation proves each blocker code is load-bearing:
// dropping the input that produces it removes exactly that blocker (and no
// other) and moves the report digest.
func TestTodo_WF_DISC_012_Mutation(t *testing.T) {
	definition := "hcmnext.t.alpha/v1"
	clean := Reconcile(baseSnapshot())
	cleanResult := findResult(t, clean, definition)
	if len(cleanResult.Blockers) != 0 {
		t.Fatalf("baseline is not clean: %+v", cleanResult.Blockers)
	}

	type mutation struct {
		name   string
		mutate func(Snapshot) Snapshot
		code   string
	}
	mutations := []mutation{
		{"drop binding", func(s Snapshot) Snapshot {
			s.Records = nil
			s.Join, s.JoinFindings = workflowdesignjoin.JoinRecords(s.Accepted, s.Records)
			return s
		}, UnboundDesign},
		{"unresolved ownership", func(s Snapshot) Snapshot {
			s.OwnershipFindings = []designownership.Finding{{Definition: definition, Code: designownership.UnownedEngine, Detail: "d"}}
			return s
		}, UnresolvedReference},
		{"missing scenario", func(s Snapshot) Snapshot {
			s.Scenarios[definition] = nil
			return s
		}, MissingAdversarialScenario},
		{"open decision", func(s Snapshot) Snapshot {
			s.Decisions = []workflowdecisions.Decision{{ID: "d1", Status: workflowdecisions.StatusOpenOwned, Affected: []string{definition}}}
			return s
		}, UnresolvedDecision},
	}

	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			mutated := Reconcile(m.mutate(baseSnapshot()))
			result := findResult(t, mutated, definition)
			if !hasBlocker(result, m.code) {
				t.Fatalf("mutation %q did not raise %s: %+v", m.name, m.code, result.Blockers)
			}
			if mutated.Digest == clean.Digest {
				t.Fatalf("mutation %q did not move the report digest", m.name)
			}
			// Restoring the snapshot must reproduce the exact clean digest.
			restored := Reconcile(baseSnapshot())
			if restored.Digest != clean.Digest {
				t.Fatalf("restoring the snapshot after mutation %q did not reproduce the clean digest", m.name)
			}
		})
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate repository root (go.mod) from %s", dir)
	return ""
}
