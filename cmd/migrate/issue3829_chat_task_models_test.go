package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
)

func TestIssue3829_CommandIsRoutedAndRefusesBadInput(t *testing.T) {
	command, rest := splitCommand([]string{chatTaskModelsCommand, "-profile", "local-dev"})
	if command != chatTaskModelsCommand || len(rest) != 2 {
		t.Fatalf("split = %q %v", command, rest)
	}
	if got := spec(command, rest); got.Validate == nil || len(got.ConfigFields) == 0 {
		t.Fatal("the command has no spec")
	}
	var out bytes.Buffer
	err := runChatTaskModelsCommand(application.LocalChatTaskModelsConfig{Profile: "production", SuiteDigest: "x"}, &out)
	if err == nil || !strings.Contains(out.String(), "No chat model documents") {
		t.Fatalf("want refusal and a plain line, got %v %q", err, out.String())
	}
}

func TestIssue3829_SummaryNamesFilesAndStatedEvidence(t *testing.T) {
	text := formatChatTaskModels([]application.LocalChatTaskModelDocument{
		{Task: "chat translation", Path: "/x/a.json", Written: true},
		{Task: "ambient screen", Path: "/x/b.json"},
	}, "sha256:abc")
	for _, want := range []string{"evaluation stated by you", "/x/a.json (written)", "/x/b.json (already current)", "MODEL_API_KEY"} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary lacks %q: %s", want, text)
		}
	}
}
