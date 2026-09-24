package testcoverage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTodo_REV_103_04(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/fixture\n\ngo 1.26.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sample.go"), []byte("package fixture\nfunc Value() int { return 7 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sample_test.go"), []byte("package fixture\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value()!=7 { t.Fatal(Value()) } }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "browser_js.go"), []byte("package fixture\nfunc BrowserValue() int { return 9 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m := Module{Path: ".", Name: "fixture", Root: "example.test/fixture", Log: "inventory.md"}
	want, err := Generate(context.Background(), root, m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(want), "| sample.go | example.test/fixture | 1 | 1 | 100.0% | true |") {
		t.Fatalf("inventory lacks measured per-file coverage:\n%s", want)
	}
	if !strings.Contains(string(want), "| browser_js.go | example.test/fixture | 0 | 0 | n/a | true | pass |") {
		t.Fatalf("inventory omitted platform-specific checkout source:\n%s", want)
	}
	if err := os.WriteFile(filepath.Join(root, "inventory.md"), want, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Check(context.Background(), root, []Module{m}); err != nil {
		t.Fatalf("fresh inventory rejected: %v", err)
	}
	tampered := strings.Replace(string(want), "| true | pass |", "| false | pass |", 1)
	if err := os.WriteFile(filepath.Join(root, "inventory.md"), []byte(tampered), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Check(context.Background(), root, []Module{m}); err == nil || !strings.Contains(err.Error(), "package or test status changed") {
		t.Fatalf("tampered package test status was accepted: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "inventory.md"), want, 0644); err != nil {
		t.Fatal(err)
	}
	withExtraExclusion := string(want) + "| sample.go | undocumented exclusion | skip package validation |\n"
	if err := os.WriteFile(filepath.Join(root, "inventory.md"), []byte(withExtraExclusion), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Check(context.Background(), root, []Module{m}); err == nil || !strings.Contains(err.Error(), "exclusion rows differ") {
		t.Fatalf("unapproved exclusion was accepted: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "inventory.md"), want, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sample.go"), []byte("package fixture\nfunc Value() int { return 8 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Check(context.Background(), root, []Module{m}); err == nil {
		t.Fatal("stale inventory passed")
	}
}

func TestTodo_REV_103_04_FailedPackageIsRecorded(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/failing\n\ngo 1.26.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sample.go"), []byte("package failing\nfunc Value() int { return 7 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sample_test.go"), []byte("package failing\nimport \"testing\"\nfunc TestValue(t *testing.T) { t.Fatal(\"deliberate failure\") }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m := Module{Path: ".", Name: "fixture", Root: "example.test/failing", Log: "inventory.md"}
	want, err := Generate(context.Background(), root, m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(want), "| sample.go | example.test/failing | 1 | 0 | 0.0% | true | fail |") {
		t.Fatalf("failed package was not recorded in inventory:\n%s", want)
	}
	if err := os.WriteFile(filepath.Join(root, "inventory.md"), want, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Check(context.Background(), root, []Module{m}); err == nil || !strings.Contains(err.Error(), "coverage test run did not pass") {
		t.Fatalf("failed test package passed the coverage gate: %v", err)
	}
}

func TestTodo_REV_103_04_UntestedFileFails(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/untested\n\ngo 1.26.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file.go"), []byte("package untested\nfunc Value() int { return 1 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m := Module{Path: ".", Name: "fixture", Root: "example.test/untested", Log: "inventory.md"}
	want, err := Generate(context.Background(), root, m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "inventory.md"), want, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Check(context.Background(), root, []Module{m}); err == nil || !strings.Contains(err.Error(), "no package tests or named exclusion") {
		t.Fatalf("missing package test was not rejected: %v", err)
	}
}

func TestGoListFailureIncludesCheckoutDiagnostic(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/badlist\n\ngo nope\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := listPackages(context.Background(), root)
	if err == nil || !strings.Contains(err.Error(), "go.mod") {
		t.Fatalf("go list failure omitted its checkout diagnostic: %v", err)
	}
}

func TestTodo_REV_103_04_Golden(t *testing.T) {
	got := string(render(Module{Path: ".", Name: "fixture", Root: "example.test/fixture"}, t.TempDir(), []sourceRow{{dir: ".", file: "sample.go", importPath: "example.test/fixture", statements: 2, covered: 1, tested: true, testResult: "pass"}}))
	want := `=== fixture (example.test/fixture) ===
files(hand-written)=1 generated=0 hand-written-in-untested-pkgs=0
untested pkgs(0):

# Test-coverage file inventory - fixture (example.test/fixture) module

hand-written=1 generated=0

## Hand-written files

| dir | file | package | statements | covered | coverage | pkg-has-tests | go-test | source-sha256 | test-sha256 |
| --- | --- | --- | ---: | ---: | ---: | --- | --- | --- | --- |
| . | sample.go | example.test/fixture | 2 | 1 | 50.0% | true | pass |  |  |

## Exclusions

| file | kind | reason |
| --- | --- | --- |
| tools/planning/cmd/pilotblueprint/main.go | thin command wrapper | All business behavior is delegated to the tested tools/planning/pilotblueprint, pilotprovider, and pilotjurisdiction libraries. |
| tools/gen/librarystrategy/cmd/generatelibrarystrategy/main.go | thin generator wrapper | The command delegates its generation behavior to the tested tools/gen/librarystrategy package. |
| tools/gen/schemaflux/cmd/modelgen/main.go | thin generator wrapper | The command delegates its generation behavior to the tested tools/gen/schemaflux package. |
`
	if got != want {
		t.Fatalf("generated inventory differs from golden:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestCheckpointResumesOnlyMatchingPassingPackage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "root", "package.json")
	module := Module{Path: ".", Name: "fixture", Root: "example.test/fixture"}
	want := packageCheckpoint{
		Version:   checkpointVersion,
		Module:    module.Path,
		Package:   "example.test/fixture/sample",
		InputHash: strings.Repeat("a", 64),
		Coverage:  map[string][2]int{"sample.go": {3, 2}},
		Passed:    true,
	}
	if err := writeCheckpoint(path, want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := readCheckpoint(path, module, want.Package, want.InputHash)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.Coverage["sample.go"] != [2]int{3, 2} {
		t.Fatalf("matching checkpoint was not resumed: got=%+v ok=%t", got, ok)
	}
	if _, ok, err := readCheckpoint(path, module, want.Package, strings.Repeat("b", 64)); err != nil || ok {
		t.Fatalf("changed source/test digest reused checkpoint: ok=%t err=%v", ok, err)
	}
	want.Passed = false
	if err := writeCheckpoint(path, want); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := readCheckpoint(path, module, want.Package, want.InputHash); err != nil || ok {
		t.Fatalf("failed package checkpoint was resumed: ok=%t err=%v", ok, err)
	}
}

func TestWriteStoresPerPackageCheckpoint(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/writefixture\n\ngo 1.26.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sample.go"), []byte("package writefixture\nfunc Value() int { return 7 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sample_test.go"), []byte("package writefixture\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value()!=7 { t.Fatal(Value()) } }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m := Module{Path: ".", Name: "fixture", Root: "example.test/writefixture", Log: "inventory.md"}
	if err := Write(context.Background(), root, []Module{m}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, m.Log))
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(context.Background(), root, []Module{m}); err != nil {
		t.Fatalf("written inventory rejected: %v", err)
	}
	if err := Write(context.Background(), root, []Module{m}); err != nil {
		t.Fatalf("resumed write failed: %v", err)
	}
	if err := Check(context.Background(), root, []Module{m}); err != nil {
		t.Fatalf("resumed inventory rejected: %v", err)
	}
	if !strings.Contains(string(data), "| sample.go | example.test/writefixture | 1 | 1 | 100.0% | true | pass |") {
		t.Fatalf("written inventory lacks per-file coverage:\n%s", data)
	}
	checkpointDir := filepath.Join(root, ".artifacts", "coverage", "testcoverage", m.Name)
	entries, err := os.ReadDir(checkpointDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || filepath.Ext(entries[0].Name()) != ".json" {
		t.Fatalf("expected one durable package checkpoint, found %v", entries)
	}
}

func TestCheckpointPackageRunsOnlySelectedImportPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/partitioned\n\ngo 1.26.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha", "beta"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "value.go"), []byte("package "+name+"\nfunc Value() int { return 7 }\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "value_test.go"), []byte("package "+name+"\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value()!=7 { t.Fatal(Value()) } }\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	module := Module{Path: ".", Name: "root", Root: "example.test/partitioned"}
	if err := CheckpointPackage(context.Background(), root, module, "example.test/partitioned/alpha"); err != nil {
		t.Fatalf("selected package checkpoint failed: %v", err)
	}
	checkpointDir := filepath.Join(root, ".artifacts", "coverage", "testcoverage", module.Name)
	entries, err := os.ReadDir(checkpointDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || filepath.Ext(entries[0].Name()) != ".json" {
		t.Fatalf("single-package run should produce one checkpoint, found %v", entries)
	}
	var got packageCheckpoint
	data, err := os.ReadFile(filepath.Join(checkpointDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Package != "example.test/partitioned/alpha" || !got.Passed || len(got.Coverage) == 0 {
		t.Fatalf("checkpoint does not bind measured selected package: %+v", got)
	}
}

func TestCheckpointPackagesRunsExplicitBatch(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/batched\n\ngo 1.26.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha", "beta", "unused"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "value.go"), []byte("package "+name+"\nfunc Value() int { return 7 }\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "value_test.go"), []byte("package "+name+"\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value()!=7 { t.Fatal(Value()) } }\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	module := Module{Path: ".", Name: "root", Root: "example.test/batched"}
	selected := []string{"example.test/batched/beta", "example.test/batched/alpha"}
	if err := CheckpointPackagesWithWorkers(context.Background(), root, module, selected, 2); err != nil {
		t.Fatalf("explicit package batch failed: %v", err)
	}
	checkpointDir := filepath.Join(root, ".artifacts", "coverage", "testcoverage", module.Name)
	entries, err := os.ReadDir(checkpointDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(selected) {
		t.Fatalf("batch checkpointed %d packages, want %d", len(entries), len(selected))
	}
	got := make(map[string]bool, len(entries))
	for _, entry := range entries {
		var checkpoint packageCheckpoint
		data, err := os.ReadFile(filepath.Join(checkpointDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &checkpoint); err != nil {
			t.Fatal(err)
		}
		got[checkpoint.Package] = checkpoint.Passed && len(checkpoint.Coverage) > 0
	}
	for _, path := range selected {
		if !got[path] {
			t.Fatalf("selected package %q lacks a passing measured checkpoint: %v", path, got)
		}
	}
	if got["example.test/batched/unused"] {
		t.Fatal("package outside explicit batch was checkpointed")
	}
	for _, invalid := range [][]string{nil, {"example.test/batched/alpha", "example.test/batched/alpha"}, {"example.test/batched/missing"}} {
		if err := CheckpointPackages(context.Background(), root, module, invalid); err == nil {
			t.Fatalf("invalid package batch %q was accepted", invalid)
		}
	}
	for _, count := range []int{0, 5} {
		if err := CheckpointPackagesWithWorkers(context.Background(), root, module, selected, count); err == nil {
			t.Fatalf("invalid worker count %d was accepted", count)
		}
	}
}

func TestCheckpointPackagesKeepsPassingResultsAcrossFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/mixedbatch\n\ngo 1.26.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha", "zfail"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "value.go"), []byte("package "+name+"\nfunc Value() int { return 7 }\n"), 0644); err != nil {
			t.Fatal(err)
		}
		testBody := "package " + name + "\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value()!=7 { t.Fatal(Value()) } }\n"
		if name == "zfail" {
			testBody = "package zfail\nimport \"testing\"\nfunc TestValue(t *testing.T) { t.Fatal(\"deliberate failure\") }\n"
		}
		if err := os.WriteFile(filepath.Join(dir, "value_test.go"), []byte(testBody), 0644); err != nil {
			t.Fatal(err)
		}
	}
	module := Module{Path: ".", Name: "root", Root: "example.test/mixedbatch"}
	err := CheckpointPackagesWithWorkers(context.Background(), root, module, []string{"example.test/mixedbatch/alpha", "example.test/mixedbatch/zfail"}, 2)
	if err == nil || !strings.Contains(err.Error(), "go test did not pass for example.test/mixedbatch/zfail") {
		t.Fatalf("failing package was not reported: %v", err)
	}
	checkpointDir := filepath.Join(root, ".artifacts", "coverage", "testcoverage", module.Name)
	entries, err := filepath.Glob(filepath.Join(checkpointDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected only the passing package checkpoint, found %d entries", len(entries))
	}
	data, err := os.ReadFile(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	var got packageCheckpoint
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Package != "example.test/mixedbatch/alpha" || !got.Passed {
		t.Fatalf("passing package checkpoint was not retained: %+v", got)
	}
	failureLogs, err := filepath.Glob(filepath.Join(root, ".artifacts", "coverage", "testcoverage", module.Name, "failures", "*.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(failureLogs) != 1 {
		t.Fatalf("expected one package failure output log, found %d", len(failureLogs))
	}
	failureOutput, err := os.ReadFile(failureLogs[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(failureOutput), "deliberate failure") {
		t.Fatalf("failure output log omitted test details: %s", failureOutput)
	}
}

func TestHasPassingTestResultOnlyIgnoresWindowsCleanupNoise(t *testing.T) {
	passing := []byte("ok\texample.test/pkg\t0.012s\n")
	if !hasPassingTestResult(passing, nil) {
		t.Fatal("successful package summary was rejected")
	}
	cleanup := []byte("ok\texample.test/pkg\t0.012s\ngo: unlinkat C:\\Temp\\test.exe: Access is denied.\n")
	if got := hasPassingTestResult(cleanup, errors.New("exit status 1")); got != (runtime.GOOS == "windows") {
		t.Fatalf("cleanup noise classification on %s = %v", runtime.GOOS, got)
	}
	failed := []byte("--- FAIL: TestBad (0.00s)\nFAIL\texample.test/pkg\t0.012s\nok\texample.test/other\t0.012s\n")
	if hasPassingTestResult(failed, errors.New("exit status 1")) {
		t.Fatal("test failure was hidden by another package summary")
	}
}
