package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReachabilityGateIsRequiredByBuildAndCI(t *testing.T) {
	root := repositoryRoot(t)

	packageBytes, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var packageManifest struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(packageBytes, &packageManifest); err != nil {
		t.Fatalf("parse package.json: %v", err)
	}
	const scriptName = "check:reachability"
	const command = "go run ./tools/planning/cmd/plancheck reachability ."
	if packageManifest.Scripts[scriptName] != command {
		t.Fatalf("%s = %q, want %q", scriptName, packageManifest.Scripts[scriptName], command)
	}
	if count := strings.Count(packageManifest.Scripts["test:all"], "npm run "+scriptName); count != 1 {
		t.Fatalf("test:all invokes %s %d times, want once", scriptName, count)
	}

	hookBytes, err := os.ReadFile(filepath.Join(root, ".husky", "pre-commit"))
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(hookBytes), command); count != 1 {
		t.Fatalf("pre-commit invokes reachability %d times, want once", count)
	}

	workflowBytes, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "tests.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(workflowBytes), "run: npm run "+scriptName); count != 1 {
		t.Fatalf("CI invokes %s %d times, want once", scriptName, count)
	}
}
