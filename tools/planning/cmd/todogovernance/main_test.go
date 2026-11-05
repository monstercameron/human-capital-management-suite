package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realRepoRoot resolves the four levels from this package
// (tools/planning/cmd/todogovernance) up to the repository root.
const realRepoRoot = "../../../.."

func TestRunRealCorpusReportsKnownViolationsAndExitsNonZero(t *testing.T) {
	var buf bytes.Buffer
	code := run([]string{
		"-markdown", filepath.Join(realRepoRoot, "planning", "archive", "todos-2026-10-22.md"),
		"-registry", filepath.Join(realRepoRoot, "definitions", "planning", "todo-registry.json"),
		"-catalog", filepath.Join(realRepoRoot, "planning", "specs", "business-intent-catalog.md"),
	}, &buf)

	// The real backlog has reviewed, allowlisted GOV-016/017/025 defects
	// (see tools/planning/todogovernance/allowlist.go), so the CLI - which
	// reports every finding regardless of any test allowlist - must exit
	// non-zero and name at least one of them.
	if code != 1 {
		t.Fatalf("expected exit code 1 for the real corpus, got %d; output:\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "violation(s) found") {
		t.Errorf("expected a violation count summary line, got:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "GOV-016") && !strings.Contains(buf.String(), "GOV-017") && !strings.Contains(buf.String(), "GOV-025") {
		t.Errorf("expected at least one finding line naming its rule, got:\n%s", buf.String())
	}
}

func TestRunCleanFixtureExitsZero(t *testing.T) {
	dir := t.TempDir()

	markdown := "- [ ] `FX-001` **[P0][LUNA] Clean fixture.**\n" +
		"  - **Depends:** none.\n" +
		"  - **INTENT CONTEXT:** `ROLE=GOVERNANCE; SETS=BI.ALL; DIRECT=none; WHY=fixture`.\n" +
		"  - **TEST:** `TestFX001`.\n" +
		"  - **TEST MATRIX:** `PRIMARY=TestFX001`; `GOLDEN=TestFX001_Golden`.\n" +
		"  - **RED:** r.\n  - **GREEN:** g.\n  - **REFACTOR:** rf.\n" +
		"  - **Refs:** [f](f.md).\n"
	markdownPath := filepath.Join(dir, "todos.md")
	if err := os.WriteFile(markdownPath, []byte(markdown), 0o644); err != nil {
		t.Fatal(err)
	}

	registry := `[{"id":"FX-001","phase":"P0","model":"LUNA","title":"Clean fixture.","section":"","depends":[],"test":"TestFX001","test_matrix":{"PRIMARY":"TestFX001","GOLDEN":"TestFX001_Golden"},"red":"r","green":"g","refactor":"rf","refs":"x"}]`
	registryPath := filepath.Join(dir, "registry.json")
	if err := os.WriteFile(registryPath, []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}

	catalog := "no definitions here"
	catalogPath := filepath.Join(dir, "catalog.md")
	if err := os.WriteFile(catalogPath, []byte(catalog), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	code := run([]string{
		"-markdown", markdownPath,
		"-registry", registryPath,
		"-catalog", catalogPath,
	}, &buf)

	if code != 0 {
		t.Fatalf("expected exit code 0 for a clean fixture, got %d; output:\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "no GOV-016/GOV-017/GOV-018/GOV-025 violations found") {
		t.Errorf("expected the clean-run message, got:\n%s", buf.String())
	}
}

func TestRunMissingRegistryExitsTwo(t *testing.T) {
	var buf bytes.Buffer
	code := run([]string{"-registry", filepath.Join(t.TempDir(), "does-not-exist.json")}, &buf)
	if code != 2 {
		t.Fatalf("expected exit code 2 for an unreadable registry, got %d", code)
	}
}
