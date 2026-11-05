package coveragematrix

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/traceability"
)

func peopleCapability() Item {
	return Item{Kind: KindCapability, ID: "hcmnext.people.explain_worker_state", OwnerDomain: "people"}
}

func peopleIntent() Item {
	return Item{Kind: KindIntent, ID: "hcmnext.people.explain_worker_state", DisplayName: "ExplainWorkerState", OwnerDomain: "PEOPLE"}
}

// TestPilotCoverageRejectsImpliedOrMissing is the primary red/green test
// for GOV-011.
func TestPilotCoverageRejectsImpliedOrMissing(t *testing.T) {
	t.Run("RED: an item with zero claims of any kind is MISSING and rejected", func(t *testing.T) {
		item := peopleCapability()
		entries, violations := Build([]Item{item}, nil, map[string]IntentContext{}, map[string]bool{})
		if len(entries) != 1 || entries[0].State != StateMissing {
			t.Fatalf("expected MISSING, got %+v", entries)
		}
		if len(violations) != 0 {
			t.Fatalf("expected zero evidence violations, got %v", violations)
		}
		cov := CheckCoverage(entries)
		if len(cov) != 1 || cov[0].State != StateMissing {
			t.Fatalf("expected one MISSING coverage violation, got %v", cov)
		}
	})

	t.Run("RED: an item only mentioned by domain (SETS) with no direct claim is IMPLIED and rejected", func(t *testing.T) {
		item := peopleCapability()
		contexts := map[string]IntentContext{
			"X-001": {Sets: []string{"BI.PEOPLE"}},
		}
		entries, _ := Build([]Item{item}, nil, contexts, map[string]bool{})
		if entries[0].State != StateImplied {
			t.Fatalf("expected IMPLIED, got %s", entries[0].State)
		}
		cov := CheckCoverage(entries)
		if len(cov) != 1 || cov[0].State != StateImplied {
			t.Fatalf("expected one IMPLIED coverage violation, got %v", cov)
		}
	})

	t.Run("RED: a Done claimant citing a nonexistent test is an evidence violation even though the item is claimed", func(t *testing.T) {
		item := peopleIntent()
		contexts := map[string]IntentContext{
			"X-002": {Intents: []string{item.ID}},
		}
		todos := []todoregistry.Todo{
			{ID: "X-002", Done: true, Evidence: "`TestDoesNotExist` in `internal/x`"},
		}
		entries, violations := Build([]Item{item}, todos, contexts, map[string]bool{})
		if entries[0].State != StatePartial {
			t.Fatalf("expected PARTIAL (claimed, evidence not closed), got %s", entries[0].State)
		}
		if len(violations) != 1 || violations[0].Test != "TestDoesNotExist" {
			t.Fatalf("expected one evidence violation naming TestDoesNotExist, got %v", violations)
		}
		if len(CheckCoverage(entries)) != 0 {
			t.Errorf("PARTIAL must not itself be a coverage violation (the evidence violation is the failure signal)")
		}
	})

	t.Run("GREEN: a claimed item whose Done claimant is not yet closed by a real test is PARTIAL and accepted", func(t *testing.T) {
		item := peopleIntent()
		contexts := map[string]IntentContext{
			"X-003": {Intents: []string{item.ID}},
		}
		todos := []todoregistry.Todo{{ID: "X-003", Done: false}}
		entries, violations := Build([]Item{item}, todos, contexts, map[string]bool{})
		if entries[0].State != StatePartial {
			t.Fatalf("expected PARTIAL, got %s", entries[0].State)
		}
		if len(violations) != 0 {
			t.Fatalf("expected zero evidence violations, got %v", violations)
		}
		if len(CheckCoverage(entries)) != 0 {
			t.Errorf("PARTIAL must pass CheckCoverage")
		}
	})

	t.Run("GREEN: a claimed item with a Done claimant citing a real test is DEFINED and accepted", func(t *testing.T) {
		item := peopleIntent()
		contexts := map[string]IntentContext{
			"X-004": {Intents: []string{item.ID}},
		}
		todos := []todoregistry.Todo{{ID: "X-004", Done: true, Evidence: "`TestReal` in `internal/x`"}}
		entries, violations := Build([]Item{item}, todos, contexts, map[string]bool{"TestReal": true})
		if entries[0].State != StateDefined {
			t.Fatalf("expected DEFINED, got %s", entries[0].State)
		}
		if len(violations) != 0 {
			t.Fatalf("expected zero evidence violations, got %v", violations)
		}
		if entries[0].Tests[0] != "TestReal" {
			t.Errorf("expected Tests to record TestReal, got %v", entries[0].Tests)
		}
		if len(CheckCoverage(entries)) != 0 {
			t.Errorf("DEFINED must pass CheckCoverage")
		}
	})

	t.Run("GREEN: a DIRECT claim via `DIRECT=<DisplayName>` counts the same as an `INTENTS=` ref", func(t *testing.T) {
		item := peopleIntent()
		contexts := map[string]IntentContext{
			"X-005": {Direct: []string{item.DisplayName}},
		}
		todos := []todoregistry.Todo{{ID: "X-005", Done: true, Evidence: "`TestReal` in `internal/x`"}}
		entries, _ := Build([]Item{item}, todos, contexts, map[string]bool{"TestReal": true})
		if entries[0].State != StateDefined {
			t.Fatalf("expected DEFINED via DIRECT=, got %s", entries[0].State)
		}
	})

	t.Run("GREEN: a capability inherits its sibling intent's DIRECT= claim by shared ID", func(t *testing.T) {
		cap := peopleCapability()
		intent := peopleIntent() // same ID as cap, DisplayName set
		contexts := map[string]IntentContext{
			"X-006": {Direct: []string{intent.DisplayName}},
		}
		todos := []todoregistry.Todo{{ID: "X-006", Done: true, Evidence: "`TestReal` in `internal/x`"}}
		entries, _ := Build([]Item{cap, intent}, todos, contexts, map[string]bool{"TestReal": true})
		for _, e := range entries {
			if e.State != StateDefined {
				t.Errorf("expected %s %s DEFINED via shared-ID DIRECT propagation, got %s", e.Item.Kind, e.Item.ID, e.State)
			}
		}
	})

	t.Run("ParseIntentContexts extracts SETS, DIRECT and INTENTS fields exactly", func(t *testing.T) {
		content := "- [ ] `X-007` **[P0][TERRA] Example.**\n" +
			"  - **INTENT CONTEXT:** `ROLE=DIRECT; SETS=BI.PEOPLE,BI.REWARDS; INTENTS=hcmnext.people.promote_worker/v1; FAMILY=CHANGE_REQUEST; WHY=example`.\n"
		contexts := ParseIntentContexts(content)
		ic, ok := contexts["X-007"]
		if !ok {
			t.Fatalf("expected an INTENT CONTEXT for X-007")
		}
		if len(ic.Sets) != 2 || ic.Sets[0] != "BI.PEOPLE" || ic.Sets[1] != "BI.REWARDS" {
			t.Errorf("Sets = %v", ic.Sets)
		}
		if len(ic.Intents) != 1 || ic.Intents[0] != "hcmnext.people.promote_worker" {
			t.Errorf("Intents = %v, want version suffix stripped", ic.Intents)
		}
	})

	t.Run("ParseIntentContexts treats DIRECT=none and SETS=BI.ALL as ordinary tokens", func(t *testing.T) {
		content := "- [x] `X-008` **[P0][LUNA] Example.**\n" +
			"  - **INTENT CONTEXT:** `ROLE=GOVERNANCE; SETS=BI.ALL; DIRECT=none; WHY=govern`.\n"
		contexts := ParseIntentContexts(content)
		ic := contexts["X-008"]
		if len(ic.Direct) != 0 {
			t.Errorf("expected DIRECT=none to parse as zero claims, got %v", ic.Direct)
		}
		if len(ic.Sets) != 1 || ic.Sets[0] != "BI.ALL" {
			t.Errorf("Sets = %v", ic.Sets)
		}
	})

	t.Run("ToYAML is deterministic across repeated calls", func(t *testing.T) {
		items, err := AllItems()
		if err != nil {
			t.Fatalf("AllItems: %v", err)
		}
		entries, _ := Build(items, nil, map[string]IntentContext{}, map[string]bool{})
		a, err := ToYAML(entries)
		if err != nil {
			t.Fatalf("ToYAML: %v", err)
		}
		b, err := ToYAML(entries)
		if err != nil {
			t.Fatalf("ToYAML: %v", err)
		}
		if string(a) != string(b) {
			t.Errorf("ToYAML is not deterministic")
		}
	})

	t.Run("real corpus: every compiled-in capability and intent is covered or an accepted, time-boxed boundary", func(t *testing.T) {
		root := filepath.Join("..", "..", "..")
		content, err := os.ReadFile(filepath.Join(root, "planning", "archive", "todos-2026-10-22.md"))
		if err != nil {
			t.Fatalf("read todos.md: %v", err)
		}
		todos, parseErrs := todoregistry.ParseTodos(string(content))
		if len(parseErrs) > 0 {
			t.Fatalf("ParseTodos returned errors: %v", parseErrs)
		}
		contexts := ParseIntentContexts(string(content))

		items, err := AllItems()
		if err != nil {
			t.Fatalf("AllItems: %v", err)
		}

		existingTests, err := traceability.ScanTestNames(root)
		if err != nil {
			t.Fatalf("scan test names: %v", err)
		}

		entries, evidenceViolations := Build(items, todos, contexts, existingTests)
		if len(evidenceViolations) != 0 {
			t.Errorf("found %d evidence violation(s) naming a nonexistent test:", len(evidenceViolations))
			for _, v := range evidenceViolations {
				t.Errorf("  %s", v)
			}
		}

		knownGaps, err := LoadKnownGaps(filepath.Join(root, "definitions", "planning", "known-defects.yaml"))
		if err != nil {
			t.Fatalf("LoadKnownGaps: %v", err)
		}
		allowed := make(map[string]bool, len(knownGaps))
		for _, g := range knownGaps {
			if g.Owner == "" || g.Expiry == "" {
				t.Errorf("known-defects.yaml coverage entry %s has no owner/expiry", g.ID)
				continue
			}
			expiry, err := time.Parse("2006-01-02", g.Expiry)
			if err != nil {
				t.Errorf("known-defects.yaml coverage entry %s has an unparseable expiry %q: %v", g.ID, g.Expiry, err)
				continue
			}
			if time.Now().After(expiry) {
				t.Errorf("known-defects.yaml coverage entry %s expired on %s; renew or resolve it", g.ID, g.Expiry)
				continue
			}
			allowed[g.ID] = true
		}

		var unresolved []CoverageViolation
		for _, v := range CheckCoverage(entries) {
			if !allowed[v.Item.ID] {
				unresolved = append(unresolved, v)
			}
		}
		if len(unresolved) != 0 {
			t.Errorf("found %d unallowed coverage violation(s) in the real corpus:", len(unresolved))
			for _, v := range unresolved {
				t.Errorf("  %s", v)
			}
		}
	})

	t.Run("generated definitions/planning/capability-coverage.yaml matches the real corpus (regenerate with HCMNEXT_UPDATE_GOLDEN=1)", func(t *testing.T) {
		root := filepath.Join("..", "..", "..")
		content, err := os.ReadFile(filepath.Join(root, "planning", "archive", "todos-2026-10-22.md"))
		if err != nil {
			t.Fatalf("read todos.md: %v", err)
		}
		todos, parseErrs := todoregistry.ParseTodos(string(content))
		if len(parseErrs) > 0 {
			t.Fatalf("ParseTodos returned errors: %v", parseErrs)
		}
		contexts := ParseIntentContexts(string(content))
		items, err := AllItems()
		if err != nil {
			t.Fatalf("AllItems: %v", err)
		}
		existingTests, err := traceability.ScanTestNames(root)
		if err != nil {
			t.Fatalf("scan test names: %v", err)
		}
		entries, _ := Build(items, todos, contexts, existingTests)
		want, err := ToYAML(entries)
		if err != nil {
			t.Fatalf("ToYAML: %v", err)
		}

		outPath := filepath.Join(root, "definitions", "planning", "capability-coverage.yaml")
		if os.Getenv("HCMNEXT_UPDATE_GOLDEN") == "1" {
			if err := os.WriteFile(outPath, want, 0o644); err != nil {
				t.Fatalf("writing %s: %v", outPath, err)
			}
			t.Logf("wrote updated capability-coverage.yaml")
			return
		}

		got, err := os.ReadFile(outPath)
		if err != nil {
			t.Fatalf("read %s: %v (regenerate with HCMNEXT_UPDATE_GOLDEN=1)", outPath, err)
		}
		if string(got) != string(want) {
			t.Errorf("definitions/planning/capability-coverage.yaml is stale; regenerate with:\n  HCMNEXT_UPDATE_GOLDEN=1 go test ./tools/planning/coveragematrix/... -run TestPilotCoverageRejectsImpliedOrMissing")
		}
	})
}

// TestTodo_GOV_011_Golden pins ToYAML's exact rendering for a small fixed
// fixture so a future refactor cannot silently change the on-disk schema.
func TestTodo_GOV_011_Golden(t *testing.T) {
	entries := []Entry{
		{
			Item:   Item{Kind: KindCapability, ID: "hcmnext.people.promote_worker", OwnerDomain: "people"},
			State:  StateDefined,
			Claims: []Claim{{TodoID: "NEXT-005", Kind: ClaimDirect}},
			Tests:  []string{"TestP1APromotionProducesExactEvidenceAndZeroAuthoritativeOrProviderEffect"},
		},
	}
	got, err := ToYAML(entries)
	if err != nil {
		t.Fatalf("ToYAML: %v", err)
	}
	want := "version: 1\n" +
		"items:\n" +
		"  - id: hcmnext.people.promote_worker\n" +
		"    kind: capability\n" +
		"    owner_domain: people\n" +
		"    state: DEFINED\n" +
		"    claims:\n" +
		"      - NEXT-005:DIRECT\n" +
		"    tests:\n" +
		"      - TestP1APromotionProducesExactEvidenceAndZeroAuthoritativeOrProviderEffect\n"
	if string(got) != want {
		t.Errorf("ToYAML output changed:\n got:\n%s\nwant:\n%s", got, want)
	}
}
