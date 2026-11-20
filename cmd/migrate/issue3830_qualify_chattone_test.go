package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
)

func TestIssue3830_CommandIsRoutedAndRefusesBadInput(t *testing.T) {
	command, rest := splitCommand([]string{qualifyChattoneCommand, "-profile", "local-dev"})
	if command != qualifyChattoneCommand || len(rest) != 2 {
		t.Fatalf("split = %q %v", command, rest)
	}
	got := spec(command, rest)
	if got.Validate == nil || len(got.ConfigFields) == 0 {
		t.Fatal("the command has no spec")
	}
	secret := false
	for _, f := range got.ConfigFields {
		if f.Env == "MODEL_API_KEY" {
			secret = f.Secret
		}
	}
	if !secret {
		t.Fatal("the provider key must be read from MODEL_API_KEY and marked secret")
	}
	var out bytes.Buffer
	err := runQualifyChattoneCommand(t.Context(), application.LocalChattoneQualifyConfig{Profile: "production", APIKey: "do-not-print"}, &out)
	if err == nil || strings.Contains(out.String(), "do-not-print") || !strings.Contains(out.String(), "did not run") {
		t.Fatalf("want refusal and a plain line, got %v %q", err, out.String())
	}
}

func TestIssue3830_SummaryStatesMeasuredEvidenceOrTheFailure(t *testing.T) {
	q := application.ChattoneQualification{SuiteDigest: "sha256:abc", Passed: true, Cases: []application.ChattoneCaseResult{{ID: "a", Passed: true}, {ID: "b", Passed: true}}}
	text := formatQualifyChattone(application.LocalChattoneQualifyResult{Qualification: q, Path: "/x/d.json", RecordPath: "/x/r.json", Written: true}, nil)
	for _, want := range []string{"2 of 2 cases passed", "sha256:abc", "/x/d.json (written)", "/x/r.json", "Measured evidence"} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary lacks %q: %s", want, text)
		}
	}
	q.Passed, q.Cases[1].Passed = false, false
	text = formatQualifyChattone(application.LocalChattoneQualifyResult{Qualification: q, RecordPath: "/x/r.json"}, application.ErrChattoneNotQualified)
	if !strings.Contains(text, "1 of 2") || !strings.Contains(text, "no deployment was written") || strings.Contains(text, "Measured evidence") {
		t.Fatalf("failure summary: %s", text)
	}
	if !strings.Contains(formatQualifyChattone(application.LocalChattoneQualifyResult{}, errors.New("no price")), "did not run: no price") {
		t.Fatal("pre-run failure not stated")
	}
}
