package runtimedecision_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/runtimedecision"
)

// recordPath is the real WF-RUN-000 decision record this package guards.
func recordPath(root string) string {
	return filepath.Join(root, "definitions", "runtime", "durable-runtime-decision.yaml")
}

// minimalCompleteYAML is a small, self-contained decision record that
// satisfies every completeness rule Validate enforces, used as the GREEN
// baseline that individual RED cases mutate one field at a time. Its EXISTS
// evidence path and FIXTURE tests point at files this test creates in a temp
// repo root, not at the real repository.
const minimalCompleteYAML = `
schema_version: 2
todo_id: WF-RUN-000
title: "test record"
status: DECIDED
decision_date: "2026-09-15"
owners:
  - name: someone
    role: owner
review_by: "2027-03-15"
non_negotiables:
  - id: NN1
    description: "ledger outside engine history"
  - id: NN2
    description: "tenant isolation and fencing"
  - id: NN3
    description: "inspectable typed projections"
  - id: NN4
    description: "safe-point pause and version pinning"
candidates:
  - name: "In-house"
    kind: BUILD
    selected: true
    summary: "build it"
    evaluation:
      - non_negotiable: NN1
        result: PASS
        evidence_kind: FIXTURE
        reason: "reason 1"
        tests:
          - name: TestFixtureNN1
            path: fixture/nn_test.go
      - non_negotiable: NN2
        result: PASS
        evidence_kind: FIXTURE
        reason: "reason 2"
        tests:
          - name: TestFixtureNN2
            path: fixture/nn_test.go
      - non_negotiable: NN3
        result: PASS
        evidence_kind: FIXTURE
        reason: "reason 3"
        tests:
          - name: TestFixtureNN3
            path: fixture/nn_test.go
      - non_negotiable: NN4
        result: PASS
        evidence_kind: FIXTURE
        reason: "reason 4"
        tests:
          - name: TestFixtureNN4
            path: fixture/nn_test.go
    evidence:
      - description: "evidence file"
        path: "EVIDENCE_FILE.md"
        status: EXISTS
  - name: "Temporal"
    kind: ADOPT
    module: "go.temporal.io/sdk"
    version: "not pinned"
    selected: false
    summary: "adopt it"
    evaluation:
      - non_negotiable: NN1
        result: PASS
        evidence_kind: DOCUMENTED
        reason: "history is the engine's own store"
        sources: ["https://docs.temporal.io/temporal-service/persistence"]
        assessed_date: "2026-09-15"
      - non_negotiable: NN2
        result: PARTIAL
        evidence_kind: DOCUMENTED
        reason: "namespace isolation only"
        sources: ["https://docs.temporal.io/namespaces"]
        assessed_date: "2026-09-15"
      - non_negotiable: NN3
        result: FAIL
        evidence_kind: DOCUMENTED
        reason: "not the typed projections"
        sources: ["https://docs.temporal.io/visibility"]
        assessed_date: "2026-09-15"
      - non_negotiable: NN4
        result: PARTIAL
        evidence_kind: DOCUMENTED
        reason: "pinning yes, safe-point pause no"
        sources: ["https://docs.temporal.io/worker-versioning"]
        assessed_date: "2026-09-15"
    evidence:
      - description: "spike pending"
        status: PENDING
        pending_todo: WF-RUN-000
selected_option:
  candidate: "In-house"
  choice: "BUILD"
  rationale: "cheapest option that already exists"
  signed_by: "someone (owner)"
  signed_date: "2026-09-15"
rejected_options:
  - candidate: "Temporal"
    reason: "fails NN3 by documentation"
consequences:
  - todo: WF-RUN-001
    impact: "build the instance table"
reevaluations:
  - date: "2026-09-03"
    kind: INITIAL
    recorded_after_code: false
    statement: "initial decision"
    outcome: "BUILD"
  - date: "2026-09-15"
    kind: RETROACTIVE
    recorded_after_code: true
    code_landed: ["fixture"]
    statement: "recorded after the gated code landed"
    outcome: "BUILD"
reevaluation_trigger:
  description: "re-run before the next gated code"
  gating_todo: WF-RUN-000
rollback_plan: "delete the in-house code"
`

const fixtureTestSource = `package fixture

import "testing"

func TestFixtureNN1(t *testing.T) {}
func TestFixtureNN2(t *testing.T) {}
func TestFixtureNN3(t *testing.T) {}
func TestFixtureNN4(t *testing.T) {}
`

// writeRepoWithRecord creates a temp directory containing go.mod (so it
// looks like a repo root), the evidence file and fixture test file the
// minimal record names, and returns the temp root plus the record's path.
func writeRepoWithRecord(t *testing.T, yamlContent string) (repoRoot, path string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/fixture\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatalf("writing fixture go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "EVIDENCE_FILE.md"), []byte("evidence\n"), 0o644); err != nil {
		t.Fatalf("writing fixture evidence file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "fixture"), 0o755); err != nil {
		t.Fatalf("creating fixture dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixture", "nn_test.go"), []byte(fixtureTestSource), 0o644); err != nil {
		t.Fatalf("writing fixture test file: %v", err)
	}
	recPath := filepath.Join(dir, "record.yaml")
	if err := os.WriteFile(recPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("writing fixture record: %v", err)
	}
	return dir, recPath
}

func mustContainFinding(t *testing.T, res runtimedecision.Result, substr string) {
	t.Helper()
	for _, f := range res.Findings {
		if strings.Contains(f, substr) {
			return
		}
	}
	t.Fatalf("expected a finding containing %q, got: %v", substr, res.Findings)
}

// mutate applies one exact replacement to the minimal fixture and fails the
// test if the target text is absent, so a RED case can never silently
// validate the unmodified GREEN baseline.
func mutate(t *testing.T, old, replacement string) string {
	t.Helper()
	if !strings.Contains(minimalCompleteYAML, old) {
		t.Fatalf("test fixture setup: %q not found in the minimal record", old)
	}
	return strings.Replace(minimalCompleteYAML, old, replacement, 1)
}

// expectRejected validates content and requires a finding containing substr.
func expectRejected(t *testing.T, content, substr string) {
	t.Helper()
	root, path := writeRepoWithRecord(t, content)
	res, err := runtimedecision.ValidateFile(path, root)
	if err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
	if res.OK {
		t.Fatalf("expected the mutated record to be rejected (want finding %q)", substr)
	}
	if res.Error() == nil {
		t.Fatalf("a rejected Result must render a non-nil Error")
	}
	mustContainFinding(t, res, substr)
}

// TestDurableRuntimeAdoptionDecisionRecordIsCompleteAndEvidenced is the
// WF-RUN-000 primary test: it proves RED (a synthetic record that lacks a
// candidate, a real PASS/FAIL/PARTIAL result per non-negotiable, honest
// evidence for that result, a signed choice or a truthful re-evaluation
// history is rejected with the offending finding named) and GREEN (the
// minimal well-formed fixture, and the real
// definitions/runtime/durable-runtime-decision.yaml record, validate clean).
func TestDurableRuntimeAdoptionDecisionRecordIsCompleteAndEvidenced(t *testing.T) {
	t.Run("GREEN_minimal_fixture_validates", func(t *testing.T) {
		root, path := writeRepoWithRecord(t, minimalCompleteYAML)
		res, err := runtimedecision.ValidateFile(path, root)
		if err != nil {
			t.Fatalf("ValidateFile: %v", err)
		}
		if !res.OK || res.Error() != nil {
			t.Fatalf("expected the minimal complete fixture to validate clean, got findings: %v", res.Findings)
		}
	})

	t.Run("GREEN_real_record_validates", func(t *testing.T) {
		root := repopath.RootDir()
		res, err := runtimedecision.ValidateFile(recordPath(root), root)
		if err != nil {
			t.Fatalf("ValidateFile: %v", err)
		}
		if !res.OK {
			t.Fatalf("definitions/runtime/durable-runtime-decision.yaml is not complete/evidenced: %v", res.Findings)
		}
	})

	t.Run("RED_unreadable_record", func(t *testing.T) {
		if _, err := runtimedecision.ValidateFile(filepath.Join(t.TempDir(), "absent.yaml"), t.TempDir()); err == nil {
			t.Fatalf("expected a missing record file to be a load error")
		}
	})

	t.Run("RED_unknown_field_is_a_load_error", func(t *testing.T) {
		root, path := writeRepoWithRecord(t, mutate(t, "status: DECIDED\n", "status: DECIDED\nfixture_scores: UNKNOWN\n"))
		if _, err := runtimedecision.ValidateFile(path, root); err == nil || !strings.Contains(err.Error(), "fixture_scores") {
			t.Fatalf("expected strict decoding to refuse an unrecognized key, got %v", err)
		}
	})

	t.Run("RED_no_candidates", func(t *testing.T) {
		start := strings.Index(minimalCompleteYAML, "candidates:\n")
		end := strings.Index(minimalCompleteYAML, "selected_option:")
		if start < 0 || end < 0 || end <= start {
			t.Fatalf("test fixture setup: could not locate candidates block")
		}
		expectRejected(t, minimalCompleteYAML[:start]+"candidates: []\n"+minimalCompleteYAML[end:], "at least one evaluated candidate is required")
	})

	t.Run("RED_missing_non_negotiable_result", func(t *testing.T) {
		expectRejected(t, mutate(t, `      - non_negotiable: NN4
        result: PASS
        evidence_kind: FIXTURE
        reason: "reason 4"
        tests:
          - name: TestFixtureNN4
            path: fixture/nn_test.go
`, ""), `missing an evaluation result for non-negotiable "NN4"`)
	})

	t.Run("RED_unknown_result_is_not_a_decision", func(t *testing.T) {
		expectRejected(t, mutate(t, "result: FAIL\n", "result: UNKNOWN\n"), `evaluation[NN3].result "UNKNOWN" is not a decision`)
	})

	t.Run("RED_pending_result_is_not_a_decision", func(t *testing.T) {
		expectRejected(t, mutate(t, "result: PARTIAL\n        evidence_kind: DOCUMENTED\n        reason: \"pinning", "result: PENDING\n        evidence_kind: DOCUMENTED\n        reason: \"pinning"), `evaluation[NN4].result "PENDING" is not a decision`)
	})

	t.Run("RED_invalid_evaluation_result", func(t *testing.T) {
		expectRejected(t, mutate(t, "result: PASS\n        evidence_kind: FIXTURE\n        reason: \"reason 1\"", "result: MAYBE\n        evidence_kind: FIXTURE\n        reason: \"reason 1\""), `"MAYBE" is not a decision`)
	})

	t.Run("RED_duplicate_evaluation", func(t *testing.T) {
		expectRejected(t, mutate(t, "      - non_negotiable: NN2\n        result: PASS", "      - non_negotiable: NN1\n        result: PASS"), `evaluation[NN1] is duplicated`)
	})

	t.Run("RED_missing_evidence_kind", func(t *testing.T) {
		expectRejected(t, mutate(t, "        evidence_kind: FIXTURE\n        reason: \"reason 2\"", "        reason: \"reason 2\""), `evaluation[NN2].evidence_kind "" is not FIXTURE or DOCUMENTED`)
	})

	t.Run("RED_fixture_without_tests", func(t *testing.T) {
		expectRejected(t, mutate(t, "        tests:\n          - name: TestFixtureNN3\n            path: fixture/nn_test.go\n", ""), "evaluation[NN3] evidence_kind FIXTURE requires at least one test")
	})

	t.Run("RED_fixture_test_not_declared", func(t *testing.T) {
		expectRejected(t, mutate(t, "name: TestFixtureNN2", "name: TestInventedBenchmark"), `fixture test TestInventedBenchmark is not declared in "fixture/nn_test.go"`)
	})

	t.Run("RED_fixture_test_file_missing", func(t *testing.T) {
		expectRejected(t, mutate(t, "name: TestFixtureNN1\n            path: fixture/nn_test.go", "name: TestFixtureNN1\n            path: fixture/absent_test.go"), `path "fixture/absent_test.go" cannot be read`)
	})

	t.Run("RED_fixture_path_not_a_test_file", func(t *testing.T) {
		expectRejected(t, mutate(t, "name: TestFixtureNN1\n            path: fixture/nn_test.go", "name: TestFixtureNN1\n            path: EVIDENCE_FILE.md"), `is not a _test.go file`)
	})

	t.Run("RED_fixture_name_not_a_test", func(t *testing.T) {
		expectRejected(t, mutate(t, "name: TestFixtureNN1", "name: helperNN1"), `fixture test "helperNN1" is not a Go test name`)
	})

	t.Run("RED_documented_without_sources", func(t *testing.T) {
		expectRejected(t, mutate(t, `        sources: ["https://docs.temporal.io/namespaces"]
`, ""), "evaluation[NN2] evidence_kind DOCUMENTED requires at least one cited source")
	})

	t.Run("RED_documented_source_not_https", func(t *testing.T) {
		expectRejected(t, mutate(t, "https://docs.temporal.io/namespaces", "docs.temporal.io/namespaces"), `source "docs.temporal.io/namespaces" is not an https URL`)
	})

	t.Run("RED_documented_without_assessed_date", func(t *testing.T) {
		expectRejected(t, mutate(t, `        sources: ["https://docs.temporal.io/visibility"]
        assessed_date: "2026-09-15"
`, `        sources: ["https://docs.temporal.io/visibility"]
`), "evaluation[NN3] evidence_kind DOCUMENTED requires assessed_date")
	})

	t.Run("RED_documented_claims_fixture_tests", func(t *testing.T) {
		expectRejected(t, mutate(t, `        assessed_date: "2026-09-15"
    evidence:
      - description: "spike pending"`, `        assessed_date: "2026-09-15"
        tests:
          - name: TestFixtureNN4
            path: fixture/nn_test.go
    evidence:
      - description: "spike pending"`), "is DOCUMENTED but names fixture tests")
	})

	t.Run("RED_selected_candidate_documented_only", func(t *testing.T) {
		expectRejected(t, mutate(t, "        evidence_kind: FIXTURE\n        reason: \"reason 1\"\n        tests:\n          - name: TestFixtureNN1\n            path: fixture/nn_test.go\n",
			"        evidence_kind: DOCUMENTED\n        reason: \"reason 1\"\n        sources: [\"https://example.com/doc\"]\n        assessed_date: \"2026-09-15\"\n"),
			"selected candidate's evaluation[NN1] must be a PASS backed by executed FIXTURE evidence, got PASS/DOCUMENTED")
	})

	t.Run("RED_selected_candidate_partial", func(t *testing.T) {
		expectRejected(t, mutate(t, "result: PASS\n        evidence_kind: FIXTURE\n        reason: \"reason 2\"", "result: PARTIAL\n        evidence_kind: FIXTURE\n        reason: \"reason 2\""),
			"selected candidate's evaluation[NN2] must be a PASS backed by executed FIXTURE evidence, got PARTIAL/FIXTURE")
	})

	t.Run("RED_selected_candidate_pending_evidence", func(t *testing.T) {
		expectRejected(t, mutate(t, `        path: "EVIDENCE_FILE.md"
        status: EXISTS
`, `        path: "EVIDENCE_FILE.md"
        status: EXISTS
      - description: "lease fixture not built"
        status: PENDING
        pending_todo: WF-RUN-002
`), `selected candidate's evidence[1] is still PENDING`)
	})

	t.Run("RED_no_signed_choice", func(t *testing.T) {
		expectRejected(t, mutate(t, `  signed_by: "someone (owner)"
`, ""), "selected_option.signed_by is required")
	})

	t.Run("RED_signer_is_not_an_owner", func(t *testing.T) {
		expectRejected(t, mutate(t, `signed_by: "someone (owner)"`, `signed_by: "someone-else"`), `selected_option.signed_by "someone-else" does not name a declared owner`)
	})

	t.Run("RED_selected_option_missing_choice", func(t *testing.T) {
		expectRejected(t, mutate(t, `  choice: "BUILD"
`, ""), "selected_option.choice is required")
	})

	t.Run("RED_choice_grammar", func(t *testing.T) {
		expectRejected(t, mutate(t, `choice: "BUILD"`, `choice: "BUY"`), `selected_option.choice "BUY" is not BUILD or ADOPT:<module>@<version>`)
	})

	t.Run("RED_adopt_choice_without_version", func(t *testing.T) {
		expectRejected(t, mutate(t, `choice: "BUILD"`, `choice: "ADOPT:go.temporal.io/sdk"`), `is not ADOPT:<module>@<version>`)
	})

	t.Run("RED_adopt_choice_on_build_candidate", func(t *testing.T) {
		expectRejected(t, mutate(t, `choice: "BUILD"`, `choice: "ADOPT:go.temporal.io/sdk@v1.0.0"`), `does not match the selected candidate (kind "BUILD"`)
	})

	t.Run("RED_build_choice_on_adopt_candidate", func(t *testing.T) {
		expectRejected(t, mutate(t, "    kind: BUILD\n", "    kind: ADOPT\n"), `selected_option.choice BUILD does not match the selected candidate's kind "ADOPT"`)
	})

	t.Run("RED_invalid_candidate_kind", func(t *testing.T) {
		expectRejected(t, mutate(t, "    kind: BUILD\n", "    kind: MAYBE\n"), `kind "MAYBE" is not BUILD or ADOPT`)
	})

	t.Run("RED_selected_candidate_lacks_evidence", func(t *testing.T) {
		expectRejected(t, mutate(t, `    evidence:
      - description: "evidence file"
        path: "EVIDENCE_FILE.md"
        status: EXISTS
`, ""), `selected candidate "In-house" has no evidence entries`)
	})

	t.Run("RED_evidence_path_does_not_exist", func(t *testing.T) {
		expectRejected(t, mutate(t, `path: "EVIDENCE_FILE.md"`, `path: "DOES_NOT_EXIST.md"`), `claims path "DOES_NOT_EXIST.md" exists but it does not`)
	})

	t.Run("RED_pending_evidence_without_todo", func(t *testing.T) {
		expectRejected(t, mutate(t, `      - description: "spike pending"
        status: PENDING
        pending_todo: WF-RUN-000
`, `      - description: "spike pending"
        status: PENDING
`), "status PENDING requires pending_todo")
	})

	t.Run("RED_two_candidates_selected", func(t *testing.T) {
		expectRejected(t, mutate(t, `    selected: false`, `    selected: true`), "exactly one candidate must be marked selected")
	})

	t.Run("RED_rejected_candidate_missing_reason_entry", func(t *testing.T) {
		expectRejected(t, mutate(t, `rejected_options:
  - candidate: "Temporal"
    reason: "fails NN3 by documentation"
`, ""), `candidate "Temporal" is not selected but has no matching rejected_options entry`)
	})

	t.Run("RED_no_reevaluations", func(t *testing.T) {
		start := strings.Index(minimalCompleteYAML, "reevaluations:\n")
		end := strings.Index(minimalCompleteYAML, "reevaluation_trigger:")
		if start < 0 || end <= start {
			t.Fatalf("test fixture setup: could not locate reevaluations block")
		}
		expectRejected(t, minimalCompleteYAML[:start]+minimalCompleteYAML[end:], "at least one reevaluations entry is required")
	})

	t.Run("RED_retroactive_entry_hides_that_it_followed_the_code", func(t *testing.T) {
		expectRejected(t, mutate(t, "    kind: RETROACTIVE\n    recorded_after_code: true\n", "    kind: RETROACTIVE\n    recorded_after_code: false\n"), "is RETROACTIVE but does not state recorded_after_code: true")
	})

	t.Run("RED_pre_code_entry_recorded_after_code", func(t *testing.T) {
		expectRejected(t, mutate(t, "    kind: RETROACTIVE\n", "    kind: PRE_CODE\n"), "is PRE_CODE but recorded_after_code is true")
	})

	t.Run("RED_retroactive_entry_names_no_code", func(t *testing.T) {
		expectRejected(t, mutate(t, `    code_landed: ["fixture"]
`, ""), "is RETROACTIVE but names no code_landed")
	})

	t.Run("RED_reevaluation_code_does_not_exist", func(t *testing.T) {
		expectRejected(t, mutate(t, `code_landed: ["fixture"]`, `code_landed: ["internal/workflow/scheduler"]`), `names code_landed "internal/workflow/scheduler" that does not exist`)
	})

	t.Run("RED_reevaluation_kind_and_fields", func(t *testing.T) {
		expectRejected(t, mutate(t, "    kind: INITIAL\n    recorded_after_code: false\n    statement: \"initial decision\"\n", "    kind: LATER\n    recorded_after_code: false\n    statement: \"\"\n"), `reevaluations[0].kind "LATER" is not INITIAL, PRE_CODE or RETROACTIVE`)
	})

	t.Run("RED_latest_reevaluation_disagrees_with_choice", func(t *testing.T) {
		expectRejected(t, mutate(t, "    statement: \"recorded after the gated code landed\"\n    outcome: \"BUILD\"", "    statement: \"recorded after the gated code landed\"\n    outcome: \"ADOPT:go.temporal.io/sdk@v1.0.0\""), `the latest reevaluation (2026-09-15) outcome "ADOPT:go.temporal.io/sdk@v1.0.0" does not match selected_option.choice "BUILD"`)
	})

	t.Run("RED_signed_date_is_not_the_latest_reevaluation", func(t *testing.T) {
		expectRejected(t, mutate(t, `  signed_date: "2026-09-15"`, `  signed_date: "2026-09-03"`), `selected_option.signed_date "2026-09-03" is not the latest reevaluation date "2026-09-15"`)
	})

	t.Run("RED_missing_reevaluation_trigger", func(t *testing.T) {
		expectRejected(t, mutate(t, `reevaluation_trigger:
  description: "re-run before the next gated code"
  gating_todo: WF-RUN-000
`, ""), "reevaluation_trigger.description is required")
	})

	t.Run("RED_wrong_non_negotiable_count", func(t *testing.T) {
		expectRejected(t, mutate(t, `  - id: NN4
    description: "safe-point pause and version pinning"
`, ""), "non_negotiables must list exactly the 4 non-negotiables")
	})
}
