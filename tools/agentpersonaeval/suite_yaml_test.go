package agentpersonaeval

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTodo_AGENTP_021_SuiteYAML(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "definitions", "agents", "personas", "uxblind_P23_*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 4 {
		t.Fatalf("starter suite count=%d, want 4", len(paths))
	}
	seen := map[string]bool{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		suite, err := ParsePersonaEvaluationSuite(data)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		if suite.Version != 1 || len(suite.Cases) < 5 || seen[suite.ID] {
			t.Fatalf("invalid or duplicate suite %s: %+v", path, suite)
		}
		seen[suite.ID] = true
		for _, testCase := range suite.Cases {
			if testCase.InputText == "" || testCase.Audience == "" {
				t.Errorf("%s fixture %s lacks request or audience", suite.ID, testCase.ID)
			}
			if testCase.Kind == CaseOutOfScope && (testCase.ExpectedRefusalCode == "" || testCase.ExpectedPointer == "" || testCase.ExpectedSkill != "") {
				t.Errorf("%s fixture %s has incomplete refusal contract: %+v", suite.ID, testCase.ID, testCase)
			}
			if testCase.Kind == CasePeerInjection && testCase.PeerText == "" {
				t.Errorf("%s fixture %s lacks the untrusted peer input", suite.ID, testCase.ID)
			}
		}
		if suite.ID == "AGENTP-021.policy_helper" {
			var publicCase *TaskCase
			for i := range suite.Cases {
				if suite.Cases[i].Kind == CaseMixedAudience {
					publicCase = &suite.Cases[i]
				}
			}
			if publicCase == nil || publicCase.ExpectedOutcome != OutcomeComplete || publicCase.ExpectedVisibility != "PUBLIC" || len(publicCase.ExpectedCitations) == 0 {
				t.Fatalf("Policy Helper does not contract for its tenant-wide cited policy answer: %+v", publicCase)
			}
		}
	}
}

func TestTodo_AGENTP_021_SuiteYAMLRejectsMalformed(t *testing.T) {
	for _, test := range []struct {
		name string
		yaml string
	}{
		{name: "unknown field", yaml: "evaluation_suite:\n  id: suite\n  version: 1\n  unexpected: true\n  fixtures: []\n"},
		{name: "incomplete public result", yaml: "evaluation_suite:\n  id: suite\n  version: 1\n  fixtures:\n    - id: mixed\n      kind: AUDIENCE\n      input_text: policy?\n      expected_skill: knowledge\n      expected_outcome: complete\n      audience: mixed\n      expected_visibility: PUBLIC\n"},
		{name: "multiple documents", yaml: "evaluation_suite:\n  id: suite\n  version: 1\n  fixtures: []\n---\nevaluation_suite: {}\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParsePersonaEvaluationSuite([]byte(test.yaml)); err == nil {
				t.Fatal("malformed suite parsed successfully")
			}
		})
	}
}
