package cleancheckout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func fakeToolPath(t *testing.T, name, windowsBody, posixBody string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\n" + posixBody + "\n"
	if runtime.GOOS == "windows" {
		path += ".cmd"
		script = "@echo off\r\n" + windowsBody + "\r\n"
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func fakeGitWithPaths(t *testing.T, paths ...string) {
	t.Helper()
	dir := fakeToolPath(t, "git", `type "%~dp0paths.bin"`, `cat "$(dirname "$0")/paths.bin"`)
	data := []byte(strings.Join(paths, "\x00") + "\x00")
	if err := os.WriteFile(filepath.Join(dir, "paths.bin"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExportTrackedTree_UsesTrackedPathsAndCopiesState(t *testing.T) {
	root, destination := t.TempDir(), t.TempDir()
	writeFixture(t, root, "go.mod", "module example.com/fixture\n")
	writeFixture(t, root, "main.go", "package main\n")
	writeFixture(t, root, "ignored.go", "package ignored\n")
	fakeGitWithPaths(t, "go.mod", "main.go")

	report, err := ExportTrackedTree(root, destination)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.Paths, []string{"go.mod", "main.go"}) {
		t.Fatalf("exported paths=%v, want sorted tracked paths", report.Paths)
	}
	for _, rel := range report.Paths {
		if _, err := os.Stat(filepath.Join(destination, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("exported %s missing: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(destination, "ignored.go")); !os.IsNotExist(err) {
		t.Fatalf("untracked file was exported: %v", err)
	}
}

func TestExportTrackedTree_ReturnsGitError(t *testing.T) {
	root, destination := t.TempDir(), t.TempDir()
	fakeToolPath(t, "git", "exit /b 9", "exit 9")
	if _, err := ExportTrackedTree(root, destination); err == nil || !strings.Contains(err.Error(), "git ls-files") {
		t.Fatalf("ExportTrackedTree error=%v, want git ls-files error", err)
	}
}

func TestExportPaths_DeduplicatesNormalizesAndRejectsInvalidInputs(t *testing.T) {
	root, destination := t.TempDir(), t.TempDir()
	writeFixture(t, root, "nested/file.txt", "fixture")
	report, err := ExportPaths(root, destination, []string{"nested\\file.txt", "nested/file.txt"})
	if err != nil || !reflect.DeepEqual(report.Paths, []string{"nested/file.txt"}) {
		t.Fatalf("ExportPaths report=%+v err=%v", report, err)
	}
	cases := []struct {
		name string
		path string
		want string
	}{
		{name: "absolute", path: "/nested/file.txt", want: "not repository-relative"},
		{name: "parent", path: "../nested/file.txt", want: "escapes repository root"},
		{name: "missing", path: "missing.txt", want: "read tracked path"},
		{name: "directory", path: "nested", want: "is a directory"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ExportPaths(root, t.TempDir(), []string{tc.path}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ExportPaths(%q) error=%v, want %q", tc.path, err, tc.want)
			}
		})
	}
}

func TestFindWorkingTreeGaps_ClassifiesBuildInputsAndSkipsDirectories(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "untracked.go", "package fixture\n")
	writeFixture(t, root, "untracked.proto", "syntax = \"proto3\";\n")
	writeFixture(t, root, "definitions/policy.yaml", "enabled: true\n")
	writeFixture(t, root, "gen/schema.sql", "select 1;\n")
	writeFixture(t, root, "schema/ignored.txt", "ignored\n")
	for _, dir := range []string{".git", "node_modules", ".gocache-1", ".go-build-1", "bin", "build", "dist", "out", "coverage", "tmp", "temp", "test-results", "playwright-report"} {
		writeFixture(t, root, filepath.ToSlash(filepath.Join(dir, "hidden.go")), "package hidden\n")
	}
	findings, err := FindWorkingTreeGaps(root, []string{"untracked.go", "definitions/policy.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, len(findings))
	for i, finding := range findings {
		paths[i] = finding.Path
		if finding.Allowlisted {
			t.Errorf("unexpected allowlist for %s: %+v", finding.Path, finding)
		}
	}
	want := []string{"gen/schema.sql", "untracked.proto"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("gap paths=%v, want %v", paths, want)
	}
	if got := classifyFinding("internal/humanwork/workspace/assets/uxqual.wasm"); got.Code != "expected-build-artifact" || !got.Allowlisted || got.Command == "" || got.Owner == "" {
		t.Fatalf("build artifact classification=%+v", got)
	}
	if got := classifyFinding("tools/policy/cleancheckout/new_test.go"); got.Code != "owner-allowlisted-working-file" || !got.Allowlisted {
		t.Fatalf("owner classification=%+v", got)
	}
	if got := classifyFinding("other.go"); got.Code != "missing-tracked-file" || got.Allowlisted {
		t.Fatalf("missing classification=%+v", got)
	}
}

func TestEvaluateAndCheck_ReportGapsAndRunCommands(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module example.com/fixture\n\ngo 1.26.3\n")
	writeFixture(t, root, "main.go", "package main\nfunc main() {}\n")
	fakeGitWithPaths(t, "go.mod", "main.go")
	fakeToolPath(t, "go", "exit /b 0", "exit 0")
	report, err := Evaluate(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.TrackedFiles != 2 || len(report.Checks) != 6 || len(report.NewGaps) == 0 {
		t.Fatalf("Evaluate report=%+v, want tracked files, six checks and missing command gaps", report)
	}
	for _, check := range report.Checks {
		if !check.Passed || check.ExitCode != 0 {
			t.Fatalf("fake verification check failed: %+v", check)
		}
	}
	if report.OK() {
		t.Fatal("report with missing required command roots reported OK")
	}
	if _, err := Check(root); err == nil || !strings.Contains(err.Error(), "new gap") {
		t.Fatalf("Check error=%v, want new gap error", err)
	}

	fakeToolPath(t, "go", `if "%1"=="fail" exit /b 7`, `[ "$1" = "fail" ] && exit 7; exit 0`)
	passed := runCommand(root, Command{Name: "ok", Args: []string{"ok"}})
	if !passed.Passed || passed.ExitCode != 0 || passed.Name != "ok" || passed.Args[0] != "ok" {
		t.Fatalf("successful runCommand=%+v", passed)
	}
	failed := runCommand(root, Command{Name: "bad", Args: []string{"fail"}})
	if failed.Passed || failed.ExitCode != 7 {
		t.Fatalf("failed runCommand=%+v, want exit 7", failed)
	}
}

func TestReportSerializationTextAndDefensiveCopies(t *testing.T) {
	artifacts := ExpectedBuildArtifacts()
	if len(artifacts) == 0 {
		t.Fatal("ExpectedBuildArtifacts returned no artifacts")
	}
	originalPath := BuildTimeArtifacts[0].Path
	artifacts[0].Path = "changed"
	if BuildTimeArtifacts[0].Path != originalPath {
		t.Fatal("ExpectedBuildArtifacts did not return a defensive slice copy")
	}
	commands := RequiredCommandPackages()
	if len(commands) == 0 {
		t.Fatal("RequiredCommandPackages returned no packages")
	}
	commands[0] = "changed"
	if RequiredCommandPackages()[0] == "changed" {
		t.Fatal("RequiredCommandPackages did not return a defensive slice copy")
	}
	report := Report{TrackedFiles: 1, BuildArtifacts: BuildTimeArtifacts, Checks: []CheckResult{{Name: "ok", Passed: true, ExitCode: 0}}}
	firstReportDigest, secondReportDigest := report.Digest(), report.Digest()
	if !report.OK() || firstReportDigest == "" || firstReportDigest != secondReportDigest {
		t.Fatalf("report status/digest invalid: %+v", report)
	}
	data, err := report.JSON()
	if err != nil || !json.Valid(data) || !strings.Contains(string(data), `"tracked_files": 1`) {
		t.Fatalf("JSON=%s err=%v", data, err)
	}
	text := report.Text()
	if !strings.Contains(text, "cleancheckout: PASS") || !strings.Contains(text, "checks: 1") {
		t.Fatalf("Text=%q", text)
	}
	failed := report
	failed.NewGaps = []Finding{{Code: "gap", Path: "x", Detail: "bad"}}
	if failed.OK() || !strings.Contains(failed.Text(), "cleancheckout: FAIL") || !strings.Contains(failed.Text(), "gap x") {
		t.Fatalf("failed report rendering/status invalid: %q", failed.Text())
	}
}

func TestEmbeddedInputsAndPathHelpers_RejectUnsafeData(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "embed.go", "package fixture\n\n//go:embed assets/* all:bundle\nvar data []byte\n")
	writeFixture(t, root, "assets/a.txt", "a")
	writeFixture(t, root, "bundle/b.txt", "b")
	inputs, err := embeddedInputs(root, map[string]bool{"embed.go": true})
	if err != nil || !inputs["assets/a.txt"] || !inputs["bundle/b.txt"] {
		t.Fatalf("embedded inputs=%v err=%v", inputs, err)
	}
	for _, tc := range []struct {
		pattern string
		want    string
	}{
		{pattern: "../escape", want: "unsafe embed pattern"},
		{pattern: "/absolute", want: "unsafe embed pattern"},
		{pattern: "all:assets/a.txt", want: "is not a directory"},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			if err := collectEmbedPattern(root, tc.pattern, root, map[string]bool{}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("collectEmbedPattern(%q) error=%v, want %q", tc.pattern, err, tc.want)
			}
		})
	}
	if !buildInputPath("definitions/policy.json") || !buildInputPath("schema/policy.sql") || buildInputPath("schema/policy.txt") || buildInputPath("notes.md") {
		t.Fatal("buildInputPath classification incorrect")
	}
	if !skipWorkingTreeDir(".git") || !skipWorkingTreeDir("temp") || skipWorkingTreeDir("src") {
		t.Fatal("skipWorkingTreeDir classification incorrect")
	}
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{raw: `pkg\\file.go`, want: "pkg/file.go"},
		{raw: "", want: "repository-relative"},
		{raw: "../x", want: "escapes"},
		{raw: "/x", want: "repository-relative"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := normalizeRelativePath(tc.raw)
			if tc.want == "pkg/file.go" {
				if err != nil || got != tc.want {
					t.Fatalf("normalizeRelativePath=%q err=%v", got, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("normalizeRelativePath(%q) error=%v, want %q", tc.raw, err, tc.want)
			}
		})
	}
	if got := limitOutput([]byte("short")); got != "short" {
		t.Fatalf("short limitOutput=%q", got)
	}
	if got := limitOutput([]byte(strings.Repeat("x", 32*1024+1))); !strings.HasSuffix(got, "...[output truncated]") || len(got) != 32*1024+len("\n...[output truncated]") {
		t.Fatalf("long limitOutput length/suffix incorrect: len=%d", len(got))
	}
}

func TestCleanEnvironmentAndCommandPackageExist(t *testing.T) {
	cleaned := cleanEnvironment([]string{"A=1", "gowork=bad", "GOWORK=also-bad"})
	if strings.Join(cleaned, "|") != "A=1|GOWORK=off" {
		t.Fatalf("cleanEnvironment=%v", cleaned)
	}
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "pkg"), 0o755)
	writeFixture(t, root, "pkg/file.go", "package pkg\n")
	if !commandPackageExists(root, "pkg") || commandPackageExists(root, "missing") || commandPackageExists(root, "pkg/file.go") {
		t.Fatal("commandPackageExists classification incorrect")
	}
}
