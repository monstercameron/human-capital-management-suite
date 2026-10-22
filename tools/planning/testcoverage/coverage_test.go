package testcoverage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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

func TestParseExclusionsAcceptsFormattedThreeColumnTable(t *testing.T) {
	data := []byte(`
## Exclusions

| file   | kind                 | reason |
| :----- | -------------------: | :----: |
| sample.go | thin command wrapper | Delegates behavior to the tested package. |
`)
	got, err := parseExclusions(data)
	if err != nil {
		t.Fatalf("formatted exclusions table rejected: %v", err)
	}
	want := map[string][2]string{
		"sample.go": {"thin command wrapper", "Delegates behavior to the tested package."},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed exclusions = %#v, want %#v", got, want)
	}
}

func TestParseExclusionsRejectsMalformedTables(t *testing.T) {
	tests := []struct {
		name  string
		table string
	}{
		{
			name:  "short separator",
			table: "| file | kind | reason |\n| -- | --- | --- |\n| sample.go | wrapper | delegates behavior |\n",
		},
		{
			name:  "wrong separator column count",
			table: "| file | kind | reason |\n| --- | --- |\n| sample.go | wrapper | delegates behavior |\n",
		},
		{
			name:  "wrong header column count",
			table: "| file | kind |\n| --- | --- |\n| sample.go | wrapper |\n",
		},
		{
			name:  "wrong header names",
			table: "| file | package | reason |\n| --- | --- | --- |\n| sample.go | wrapper | delegates behavior |\n",
		},
		{
			name:  "extra edge columns",
			table: "|| file | kind | reason ||\n| --- | --- | --- |\n| sample.go | wrapper | delegates behavior |\n",
		},
		{
			name:  "repeated separator colons",
			table: "| file | kind | reason |\n| ::--- | --- | --- |\n| sample.go | wrapper | delegates behavior |\n",
		},
		{
			name:  "extra edge columns in row",
			table: "| file | kind | reason |\n| --- | --- | --- |\n|| sample.go | wrapper | delegates behavior ||\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte("## Exclusions\n\n" + tt.table)
			if _, err := parseExclusions(data); err == nil {
				t.Fatal("malformed exclusions table was accepted")
			}
		})
	}
}

func TestParseExclusionsRejectsDuplicateRows(t *testing.T) {
	data := []byte("## Exclusions\n\n| file | kind | reason |\n| --- | --- | --- |\n| sample.go | wrapper | first |\n| sample.go | wrapper | second |\n")
	if _, err := parseExclusions(data); err == nil || !strings.Contains(err.Error(), "duplicate exclusion row sample.go") {
		t.Fatalf("duplicate exclusion was not rejected: %v", err)
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

func TestCheckpointContextHashTracksDependencyAndBuildInputs(t *testing.T) {
	root := newCheckpointFingerprintFixture(t)
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOWORK", "off")

	first, err := moduleContextHash(context.Background(), root, root)
	if err != nil {
		t.Fatalf("hash initial context: %v", err)
	}
	unchanged, err := moduleContextHash(context.Background(), root, root)
	if err != nil {
		t.Fatalf("rehash unchanged context: %v", err)
	}
	if unchanged != first {
		t.Fatal("unchanged source/build context produced a different checkpoint fingerprint")
	}

	packages, err := listPackages(context.Background(), root)
	if err != nil {
		t.Fatalf("list fixture packages: %v", err)
	}
	var fixturePackage listedPackage
	for _, pkg := range packages {
		if pkg.ImportPath == "example.test/checkpoint" {
			fixturePackage = pkg
			break
		}
	}
	if fixturePackage.ImportPath == "" {
		t.Fatal("fixture root package was not listed")
	}
	rows := []sourceRow{{testHash: "unchanged-test-hash"}}
	firstPackageHash, err := packageInputHash(rows, fixturePackage, first)
	if err != nil {
		t.Fatalf("hash fixture package: %v", err)
	}

	dependencyFile := filepath.Join(root, "dependency", "value.go")
	writeCheckpointFixtureFile(t, dependencyFile, "package dependency\nfunc Value() string { return \"changed dependency\" }\n")
	dependencyChanged, err := moduleContextHash(context.Background(), root, root)
	if err != nil {
		t.Fatalf("hash changed dependency: %v", err)
	}
	if dependencyChanged == first {
		t.Fatal("dependency-only change did not invalidate module context")
	}
	dependencyPackageHash, err := packageInputHash(rows, fixturePackage, dependencyChanged)
	if err != nil {
		t.Fatalf("hash package after dependency change: %v", err)
	}
	if dependencyPackageHash == firstPackageHash {
		t.Fatal("dependency-only change reused the package checkpoint key")
	}

	writeCheckpointFixtureFile(t, filepath.Join(root, "notice.txt"), "updated embedded text\n")
	embedChanged, err := moduleContextHash(context.Background(), root, root)
	if err != nil {
		t.Fatalf("hash changed embed input: %v", err)
	}
	if embedChanged == dependencyChanged {
		t.Fatal("embed-only change did not invalidate module context")
	}

	goModPath := filepath.Join(root, "go.mod")
	goMod, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatal(err)
	}
	writeCheckpointFixtureFile(t, goModPath, string(goMod)+"// context fingerprint change\n")
	moduleChanged, err := moduleContextHash(context.Background(), root, root)
	if err != nil {
		t.Fatalf("hash changed go.mod: %v", err)
	}
	if moduleChanged == embedChanged {
		t.Fatal("go.mod-only change did not invalidate module context")
	}

	t.Setenv("GOFLAGS", "-tags=checkpointcontext")
	buildFlagsChanged, err := moduleContextHash(context.Background(), root, root)
	if err != nil {
		t.Fatalf("hash changed build flags: %v", err)
	}
	if buildFlagsChanged == moduleChanged {
		t.Fatal("GOFLAGS-only change did not invalidate module context")
	}
}

func TestCheckpointContextHashTracksOverlayReplacement(t *testing.T) {
	root := newCheckpointFingerprintFixture(t)
	t.Setenv("GOWORK", "off")
	mainPath := filepath.Join(root, "main.go")
	mainSource, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mainSource), "dependency.Value()") {
		t.Fatal("fixture source lacks expected dependency expression")
	}
	replacementPath := filepath.Join(root, ".artifacts", "main.overlay.go")
	writeCheckpointFixtureFile(t, replacementPath, strings.Replace(string(mainSource), "dependency.Value()", "dependency.Value() + \" overlay one\"", 1))
	overlayPath := filepath.Join(root, ".artifacts", "go-overlay.json")
	overlay, err := json.Marshal(struct {
		Replace map[string]string `json:"Replace"`
	}{Replace: map[string]string{mainPath: replacementPath}})
	if err != nil {
		t.Fatal(err)
	}
	writeCheckpointFixtureFile(t, overlayPath, string(overlay))
	t.Setenv("GOFLAGS", "-overlay="+overlayPath)

	first, err := moduleContextHash(context.Background(), root, root)
	if err != nil {
		t.Fatalf("hash initial overlay context: %v", err)
	}
	unchanged, err := moduleContextHash(context.Background(), root, root)
	if err != nil {
		t.Fatalf("rehash unchanged overlay context: %v", err)
	}
	if unchanged != first {
		t.Fatal("unchanged overlay inputs produced a different checkpoint fingerprint")
	}
	writeCheckpointFixtureFile(t, replacementPath, strings.Replace(string(mainSource), "dependency.Value()", "dependency.Value() + \" overlay two\"", 1))
	changed, err := moduleContextHash(context.Background(), root, root)
	if err != nil {
		t.Fatalf("hash changed overlay replacement: %v", err)
	}
	if changed == first {
		t.Fatal("overlay replacement-only change did not invalidate module context")
	}

	writeCheckpointFixtureFile(t, replacementPath, "package checkpoint\nconst overlayAdded = true\n")
	addedSourcePath := filepath.Join(root, "overlay_added.go")
	writeCheckpointFixtureFile(t, overlayPath, mustMarshalOverlay(t, map[string]string{
		addedSourcePath: replacementPath,
	}))
	addedSourceHash, err := moduleContextHash(context.Background(), root, root)
	if err != nil {
		t.Fatalf("hash overlay-added source: %v", err)
	}
	if addedSourceHash == changed {
		t.Fatal("overlay-added Go source did not invalidate the checkpoint key")
	}

	writeCheckpointFixtureFile(t, filepath.Join(root, "obsolete.go"), "package checkpoint\nconst obsolete = true\n")
	writeCheckpointFixtureFile(t, overlayPath, mustMarshalOverlay(t, map[string]string{
		filepath.Join(root, "obsolete.go"): "",
	}))
	deletedSourceHash, err := moduleContextHash(context.Background(), root, root)
	if err != nil {
		t.Fatalf("hash overlay-deleted source: %v", err)
	}
	if deletedSourceHash == addedSourceHash {
		t.Fatal("empty overlay replacement did not invalidate the checkpoint key")
	}
}

func mustMarshalOverlay(t *testing.T, replacements map[string]string) string {
	t.Helper()
	data, err := json.Marshal(struct {
		Replace map[string]string `json:"Replace"`
	}{Replace: replacements})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func newCheckpointFingerprintFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeCheckpointFingerprintModule(t, root)
	return root
}

func writeCheckpointFingerprintModule(t *testing.T, root string) {
	t.Helper()
	writeCheckpointFingerprintModuleWithDependency(t, root, filepath.Join(root, "dependency"))
}

func writeCheckpointFingerprintModuleWithDependency(t *testing.T, root, dependencyRoot string) {
	t.Helper()
	replacement, err := filepath.Rel(root, dependencyRoot)
	if err != nil {
		t.Fatal(err)
	}
	replacement = filepath.ToSlash(replacement)
	if replacement == "." || !strings.HasPrefix(replacement, "../") && !filepath.IsAbs(replacement) {
		replacement = "./" + strings.TrimPrefix(replacement, "./")
	}
	writeCheckpointFixtureFile(t, filepath.Join(root, "go.mod"), "module example.test/checkpoint\n\ngo 1.26.0\n\nrequire example.test/dependency v0.0.0\nreplace example.test/dependency => "+replacement+"\n")
	writeCheckpointFixtureFile(t, filepath.Join(root, "main.go"), "package checkpoint\nimport (_ \"embed\"; \"example.test/dependency\")\n//go:embed notice.txt\nvar notice string\nfunc Value() string { return dependency.Value() + notice }\n")
	writeCheckpointFixtureFile(t, filepath.Join(root, "main_test.go"), "package checkpoint\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value() == \"\" { t.Fatal(\"empty\") } }\n")
	writeCheckpointFixtureFile(t, filepath.Join(root, "notice.txt"), "fixture text\n")
	writeCheckpointFixtureFile(t, filepath.Join(dependencyRoot, "go.mod"), "module example.test/dependency\n\ngo 1.26.0\n")
	writeCheckpointFixtureFile(t, filepath.Join(dependencyRoot, "value.go"), "package dependency\nfunc Value() string { return \"initial dependency\" }\n")
}

func TestCheckpointContextHashTracksExternalDependencyTestdata(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	moduleRoot := filepath.Join(repo, "module")
	dependencyRoot := filepath.Join(base, "local-replacement")
	if err := os.MkdirAll(moduleRoot, 0755); err != nil {
		t.Fatal(err)
	}
	writeCheckpointFingerprintModuleWithDependency(t, moduleRoot, dependencyRoot)
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOWORK", "off")

	first, err := moduleContextHash(context.Background(), moduleRoot, repo)
	if err != nil {
		t.Fatalf("hash initial external dependency: %v", err)
	}
	writeCheckpointFixtureFile(t, filepath.Join(dependencyRoot, "testdata", "runtime.json"), "{\"mode\":\"first\"}\n")
	changed, err := moduleContextHash(context.Background(), moduleRoot, repo)
	if err != nil {
		t.Fatalf("hash external dependency testdata change: %v", err)
	}
	if changed == first {
		t.Fatal("external local-replacement testdata change did not invalidate the checkpoint context")
	}
}

func TestCheckpointContextHashTracksRepositoryLevelInputs(t *testing.T) {
	repo := t.TempDir()
	moduleRoot := filepath.Join(repo, "tools", "fixture")
	if err := os.MkdirAll(moduleRoot, 0755); err != nil {
		t.Fatal(err)
	}
	writeCheckpointFingerprintModule(t, moduleRoot)
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOWORK", "off")

	first, err := moduleContextHash(context.Background(), moduleRoot, repo)
	if err != nil {
		t.Fatalf("hash initial repository context: %v", err)
	}
	writeCheckpointFixtureFile(t, filepath.Join(repo, "definitions", "policy.yaml"), "policy: initial\n")
	changed, err := moduleContextHash(context.Background(), moduleRoot, repo)
	if err != nil {
		t.Fatalf("hash changed repository input: %v", err)
	}
	if changed == first {
		t.Fatal("repository-level non-Go input did not invalidate the checkpoint context")
	}
}

func writeCheckpointFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
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
	checkpointDir := filepath.Join(root, ".artifacts", "coverage", "testcoverage", m.Name)
	firstEntries, err := os.ReadDir(checkpointDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstEntries) != 2 {
		t.Fatalf("first inventory write should invalidate the pre-inventory checkpoint once; found %d checkpoints", len(firstEntries))
	}
	firstHashes := make(map[string]bool, len(firstEntries))
	for _, entry := range firstEntries {
		checkpointData, err := os.ReadFile(filepath.Join(checkpointDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var checkpoint packageCheckpoint
		if err := json.Unmarshal(checkpointData, &checkpoint); err != nil {
			t.Fatal(err)
		}
		if checkpoint.Package != m.Root || !checkpoint.Passed || len(checkpoint.Coverage) == 0 {
			t.Fatalf("checkpoint did not retain the expected passing measurement: %+v", checkpoint)
		}
		firstHashes[checkpoint.InputHash] = true
	}
	if len(firstHashes) != 2 {
		t.Fatalf("expected distinct pre- and post-inventory input hashes, got %v", firstHashes)
	}
	stableNames := make([]string, len(firstEntries))
	for i, entry := range firstEntries {
		stableNames[i] = entry.Name()
	}
	if err := Write(context.Background(), root, []Module{m}); err != nil {
		t.Fatalf("stable-view write failed: %v", err)
	}
	stableEntries, err := os.ReadDir(checkpointDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(stableEntries) != len(stableNames) {
		t.Fatalf("unchanged repository view added a checkpoint: before %v after %v", stableNames, stableEntries)
	}
	for i, entry := range stableEntries {
		if entry.Name() != stableNames[i] {
			t.Fatalf("unchanged repository view changed checkpoint set: before %v after %v", stableNames, stableEntries)
		}
	}
	if !strings.Contains(string(data), "| sample.go | example.test/writefixture | 1 | 1 | 100.0% | true | pass |") {
		t.Fatalf("written inventory lacks per-file coverage:\n%s", data)
	}
	for _, entry := range stableEntries {
		if filepath.Ext(entry.Name()) != ".json" {
			t.Fatalf("unexpected checkpoint artifact %q", entry.Name())
		}
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
