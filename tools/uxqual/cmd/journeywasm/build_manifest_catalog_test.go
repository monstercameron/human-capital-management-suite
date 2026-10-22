//go:build !(js && wasm)

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

// Issue #60: scripts/build.sh stages the candidate source asset catalog into the
// embed staging directory and then runs this command with that directory as
// -out, so the manifest written by writeAssetIntegrityManifest describes
// exactly the files present there. The runtime, however, validates the
// manifest against the whole embedded catalog (the overlaid bundle plus
// every asset embedded from the source directory). Staging only a subset
// therefore produced a manifest the validator rejected as stale.
//
// The staging itself is production shell code,
// scripts/stage-workspace-assets.sh, and is shared with scripts/build.sh.
// These tests therefore never reimplement it: the Go below only arranges
// fixtures (a temporary source catalog and a pre-dirtied destination),
// invokes the real helper through bash, and then checks the destination
// with the existing manifest generation and the runtime catalog
// (workspace.BuildEmbeddedAssetIntegrityManifest). The canonical-JSON
// comparison is the same comparison
// workspace.ValidateAssetIntegrityCatalog performs at startup.

func moduleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	// go test runs with the package directory as the working directory:
	// tools/uxqual/cmd/journeywasm, four levels below the module root.
	root := filepath.Clean(filepath.Join(wd, "..", "..", "..", ".."))
	if info, err := os.Stat(filepath.Join(root, "go.mod")); err != nil || info.IsDir() {
		t.Fatalf("module root not found from %q: %v", wd, err)
	}
	return root
}

func assetCatalogSourceDir(t *testing.T) string {
	t.Helper()
	src := filepath.Join(moduleRoot(t), "internal", "humanwork", "workspace", "assets")
	if entries, err := os.ReadDir(src); err != nil || len(entries) == 0 {
		t.Fatalf("source asset catalog at %q: %v", src, err)
	}
	return src
}

// makeCatalogFixture builds a temporary source catalog for the helper to
// stage from. It enumerates the candidate catalog dynamically, so an asset
// added to the catalog joins the fixture with no test change; naming one
// file through omit builds the incompletely-staged variant. This assembles
// fixture input only -- the clear-and-copy staging under test is performed
// solely by scripts/stage-workspace-assets.sh.
func makeCatalogFixture(t *testing.T, omit string) string {
	t.Helper()
	src := assetCatalogSourceDir(t)
	out := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read source asset catalog: %v", err)
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		name := entry.Name()
		if name == omit || name == omit+".gz" {
			continue
		}
		from := filepath.Join(src, name)
		to := filepath.Join(out, name)
		if err := os.Link(from, to); err != nil {
			if err := copyFile(from, to); err != nil {
				t.Fatalf("build source fixture %s: %v", name, err)
			}
		}
	}
	return out
}

func bashExecutable(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		if override := os.Getenv("HCMNEXT_GIT_BASH"); override != "" {
			if info, err := os.Stat(override); err == nil && !info.IsDir() {
				return override
			}
			t.Fatalf("HCMNEXT_GIT_BASH is not an executable file: %q", override)
		}
		if git, err := exec.LookPath("git.exe"); err == nil {
			candidate := filepath.Join(filepath.Dir(filepath.Dir(git)), "bin", "bash.exe")
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				return candidate
			}
		}
		candidate := filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
		t.Skip("Git Bash not found; set HCMNEXT_GIT_BASH to its bash.exe path")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("no bash available to run the production staging helper: %v", err)
	}
	return bash
}

func gitBashPath(t *testing.T, bash, path string) string {
	t.Helper()
	if path == "" {
		return ""
	}
	gitRoot := filepath.Dir(filepath.Dir(bash))
	cygpath := filepath.Join(gitRoot, "usr", "bin", "cygpath.exe")
	if _, err := os.Stat(cygpath); err != nil {
		t.Fatalf("Git Bash path converter %q is unavailable: %v", cygpath, err)
	}
	converted, err := exec.Command(cygpath, "-u", "--", path).Output()
	if err != nil {
		t.Fatalf("convert Windows path %q with %q: %v", path, cygpath, err)
	}
	return strings.TrimSpace(string(converted))
}

func runStageHelper(t *testing.T, source, destination string) (string, error) {
	t.Helper()
	root := moduleRoot(t)
	script := filepath.Join(root, "scripts", "stage-workspace-assets.sh")
	bash := bashExecutable(t)
	args := []string{"-l", script, source, destination}
	if runtime.GOOS == "windows" {
		args = []string{"-l", gitBashPath(t, bash, script), gitBashPath(t, bash, source), gitBashPath(t, bash, destination)}
	}
	cmd := exec.Command(bash, args...)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start bash %q: %v", bash, err)
	}
	t.Logf("staging shell: executable=%q pid=%d helper=%q", bash, cmd.Process.Pid, script)
	waitErr := cmd.Wait()
	return stdout.String() + stderr.String(), waitErr
}

func requireRefusal(t *testing.T, source, destination, wantDiagnostic string) string {
	t.Helper()
	output, err := runStageHelper(t, source, destination)
	if err == nil {
		t.Fatalf("helper accepted unsafe arguments %q -> %q; output: %s", source, destination, output)
	}
	if !strings.Contains(output, wantDiagnostic) {
		t.Fatalf("helper refusal diagnostic %q does not contain %q", output, wantDiagnostic)
	}
	return output
}

func stageWithHelper(t *testing.T, source, destination string) {
	t.Helper()
	if output, err := runStageHelper(t, source, destination); err != nil {
		t.Fatalf("stage-workspace-assets.sh %q -> %q: %v\n%s", source, destination, err, output)
	}
}

func generatedManifest(t *testing.T, out string) workspace.AssetIntegrityManifest {
	t.Helper()
	if err := writeAssetIntegrityManifest(out); err != nil {
		t.Fatalf("writeAssetIntegrityManifest(%q): %v", out, err)
	}
	body, err := os.ReadFile(filepath.Join(out, workspace.AssetIntegrityManifestName))
	if err != nil {
		t.Fatalf("read generated manifest: %v", err)
	}
	manifest, err := workspace.ParseAssetIntegrityManifest(body)
	if err != nil {
		t.Fatalf("parse generated manifest: %v", err)
	}
	return manifest
}

func assetPaths(manifest workspace.AssetIntegrityManifest) map[string]bool {
	paths := make(map[string]bool, len(manifest.Assets))
	for _, asset := range manifest.Assets {
		paths[asset.Path] = true
	}
	return paths
}

func writeFixtureFile(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write fixture file %s: %v", path, err)
	}
}

func createFixtureSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable for this test environment: %v", err)
	}
}

func TestBuildScriptStagesCandidateCatalogBeforeManifestGeneration(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(moduleRoot(t), "scripts", "build.sh"))
	if err != nil {
		t.Fatalf("read scripts/build.sh: %v", err)
	}
	script := string(body)

	stage := strings.Index(script, `bash "$root/scripts/stage-workspace-assets.sh"`)
	stageArgs := strings.Index(script, `"$root/internal/humanwork/workspace/assets" "$embed"`)
	toolBuild := strings.Index(script, `go build -o "$asset_tool" ./tools/uxqual/cmd/journeywasm`)
	manifestBuild := strings.Index(script, `"$asset_tool" -root "$root" -out "$embed" -overlay "$embed/assets.overlay.json"`)
	targetBuild := strings.Index(script, `go build -overlay="$embed/assets.overlay.json" -o "$out/$c.exe" "./cmd/$c"`)
	if stage < 0 || stageArgs < stage || toolBuild < stageArgs || manifestBuild < toolBuild || targetBuild < manifestBuild {
		t.Fatalf("build pipeline must stage the complete source catalog before generating the manifest and embedding it through the same overlay; positions stage=%d args=%d helper-build=%d manifest=%d target=%d", stage, stageArgs, toolBuild, manifestBuild, targetBuild)
	}
}

func TestBuildManifestCatalogMatchesRuntimeCatalog(t *testing.T) {
	expected, err := workspace.BuildEmbeddedAssetIntegrityManifest()
	if err != nil {
		t.Fatalf("BuildEmbeddedAssetIntegrityManifest: %v", err)
	}
	if len(expected.Assets) == 0 {
		t.Fatal("runtime embedded catalog is empty")
	}

	source := makeCatalogFixture(t, "")
	destination := t.TempDir()

	// Dirty the destination the way an interrupted or older build leaves
	// it: a stale copy of a routable catalog asset, a stale manifest
	// claiming an entry the catalog does not have, and a leftover file no
	// source asset backs. A correct staging removes every trace of them.
	staleAssetName := filepath.Base(expected.Assets[0].Path)
	staleAssetPath := filepath.Join(destination, staleAssetName)
	writeFixtureFile(t, staleAssetPath, []byte("stale asset the staging must replace"))
	staleManifest := []byte(`{"version":"stale","assets":[{"path":"/workspace/assets/stale-entry","content_type":"text/javascript","bytes":1,"sha256":"` +
		strings.Repeat("0", 64) + `","integrity":"sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=","representations":[{"encoding":"identity","bytes":1,"sha256":"` +
		strings.Repeat("0", 64) + `"}]}]}`)
	writeFixtureFile(t, filepath.Join(destination, workspace.AssetIntegrityManifestName), staleManifest)
	staleLeftover := filepath.Join(destination, "stale-leftover-asset")
	writeFixtureFile(t, staleLeftover, []byte("stale output the staging must remove"))

	stageWithHelper(t, source, destination)

	if _, err := os.Stat(staleLeftover); !os.IsNotExist(err) {
		t.Fatalf("staging left the stale leftover %q in the destination (stat error %v)", staleLeftover, err)
	}
	stagedAsset, err := os.ReadFile(staleAssetPath)
	if err != nil {
		t.Fatalf("read staged %s: %v", staleAssetName, err)
	}
	sourceAsset, err := os.ReadFile(filepath.Join(source, staleAssetName))
	if err != nil {
		t.Fatalf("read source fixture %s: %v", staleAssetName, err)
	}
	if !bytes.Equal(stagedAsset, sourceAsset) {
		t.Fatalf("staging left stale bytes in routable asset %q", staleAssetName)
	}
	stagedManifest, err := os.ReadFile(filepath.Join(destination, workspace.AssetIntegrityManifestName))
	if err != nil {
		t.Fatalf("read staged manifest before generation: %v", err)
	}
	if bytes.Equal(stagedManifest, staleManifest) || strings.Contains(string(stagedManifest), "stale-entry") {
		t.Fatal("staging left the stale manifest entry in the destination")
	}

	generated := generatedManifest(t, destination)

	expectedPaths := assetPaths(expected)
	generatedPaths := assetPaths(generated)
	var missing, extra []string
	for path := range expectedPaths {
		if !generatedPaths[path] {
			missing = append(missing, path)
		}
	}
	for path := range generatedPaths {
		if !expectedPaths[path] {
			extra = append(extra, path)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("generated manifest covers %d assets, runtime catalog has %d; missing %v, extra %v",
			len(generated.Assets), len(expected.Assets), missing, extra)
	}

	// Canonical equality also pins every asset's byte count, SHA-256, SRI
	// and identity/gzip representations: exactly the comparison the runtime
	// validator makes before it accepts a manifest at startup.
	generatedJSON, err := generated.CanonicalJSON()
	if err != nil {
		t.Fatalf("generated manifest canonical JSON: %v", err)
	}
	expectedJSON, err := expected.CanonicalJSON()
	if err != nil {
		t.Fatalf("runtime manifest canonical JSON: %v", err)
	}
	if !bytes.Equal(generatedJSON, expectedJSON) {
		byPath := make(map[string]workspace.AssetIntegrity, len(generated.Assets))
		for _, asset := range generated.Assets {
			byPath[asset.Path] = asset
		}
		for _, want := range expected.Assets {
			got, ok := byPath[want.Path]
			if !ok {
				continue
			}
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(want)
			if !bytes.Equal(gotJSON, wantJSON) {
				t.Errorf("asset %s differs:\n generated %s\n runtime   %s", want.Path, gotJSON, wantJSON)
			}
		}
		t.Fatal("manifest generated from the helper-staged catalog does not exactly describe the runtime embedded catalog")
	}
}

func TestBuildManifestCatalogRejectsAnIncompleteStaging(t *testing.T) {
	expected, err := workspace.BuildEmbeddedAssetIntegrityManifest()
	if err != nil {
		t.Fatalf("BuildEmbeddedAssetIntegrityManifest: %v", err)
	}

	// Stage a catalog missing one asset the bundle builder itself does not
	// produce, chosen from the live catalog rather than named here: this is
	// the shape of the original subset-staging failure, so the generated
	// manifest must come back smaller than and different from the runtime
	// catalog instead of silently validating.
	var omitted string
	for _, asset := range expected.Assets {
		name := filepath.Base(asset.Path)
		if name != wasmFile && name != wasmExecFile {
			omitted = name
			break
		}
	}
	if omitted == "" {
		t.Fatal("runtime catalog contains no asset beyond the generated bundle files")
	}

	destination := t.TempDir()
	stageWithHelper(t, makeCatalogFixture(t, omitted), destination)
	if _, err := os.Stat(filepath.Join(destination, omitted)); !os.IsNotExist(err) {
		t.Fatalf("helper staged %q from a source catalog that does not contain it (stat error %v)", omitted, err)
	}

	generated := generatedManifest(t, destination)
	if path := workspace.PathAssetPrefix + omitted; assetPaths(generated)[path] {
		t.Fatalf("generated manifest still covers %q after it was left out of staging", path)
	}
	if len(generated.Assets) >= len(expected.Assets) {
		t.Fatalf("generated manifest covers %d assets after an omission, runtime catalog has %d",
			len(generated.Assets), len(expected.Assets))
	}
	generatedJSON, err := generated.CanonicalJSON()
	if err != nil {
		t.Fatalf("generated manifest canonical JSON: %v", err)
	}
	expectedJSON, err := expected.CanonicalJSON()
	if err != nil {
		t.Fatalf("runtime manifest canonical JSON: %v", err)
	}
	if bytes.Equal(generatedJSON, expectedJSON) {
		t.Fatal("manifest from an incompletely staged catalog matches the runtime catalog; the runtime validator would accept a catalog it should reject")
	}
}

func TestStageWorkspaceAssetsRejectsUnsafeArguments(t *testing.T) {
	source := makeCatalogFixture(t, "")

	t.Run("empty arguments", func(t *testing.T) {
		destination := t.TempDir()
		sentinel := filepath.Join(destination, "sentinel.txt")
		writeFixtureFile(t, sentinel, []byte("keep me"))
		requireRefusal(t, "", destination, "source and destination must both be non-empty")
		requireRefusal(t, source, "", "source and destination must both be non-empty")
		if body, err := os.ReadFile(sentinel); err != nil || string(body) != "keep me" {
			t.Fatalf("empty-argument rejection disturbed the destination: %q, %v", body, err)
		}
	})

	t.Run("source equals destination", func(t *testing.T) {
		dir := t.TempDir()
		sentinel := filepath.Join(dir, "sentinel.txt")
		writeFixtureFile(t, sentinel, []byte("keep me"))
		requireRefusal(t, dir, dir, "source and destination are the same directory")
		if body, err := os.ReadFile(sentinel); err != nil || string(body) != "keep me" {
			t.Fatalf("source==destination rejection cleared its input: %q, %v", body, err)
		}
	})

	t.Run("destination inside source is rejected before creation", func(t *testing.T) {
		destination := filepath.Join(source, "staging-must-not-exist")
		requireRefusal(t, source, destination, "destination is inside the source")
		if _, err := os.Stat(destination); !os.IsNotExist(err) {
			t.Fatalf("rejected destination was created inside source: %v", err)
		}
	})

	t.Run("destination contains the source", func(t *testing.T) {
		parent := t.TempDir()
		nested := filepath.Join(parent, "catalog")
		if err := os.Mkdir(nested, 0o755); err != nil {
			t.Fatal(err)
		}
		sentinel := filepath.Join(parent, "sentinel.txt")
		writeFixtureFile(t, sentinel, []byte("keep me"))
		writeFixtureFile(t, filepath.Join(nested, "source.txt"), []byte("source"))
		requireRefusal(t, nested, parent, "destination contains the source")
		if body, err := os.ReadFile(sentinel); err != nil || string(body) != "keep me" {
			t.Fatalf("ancestor-destination rejection cleared the destination: %q, %v", body, err)
		}
	})

	t.Run("filesystem root destination", func(t *testing.T) {
		requireRefusal(t, source, "/", "unsafe destination: /")
	})

	t.Run("top-level destination", func(t *testing.T) {
		requireRefusal(t, source, "/tmp", "unsafe destination (top-level directory)")
	})

	if runtime.GOOS == "windows" {
		t.Run("drive-root child destination", func(t *testing.T) {
			name := "codex-qa-top-level-" + filepath.Base(t.TempDir())
			destination := filepath.VolumeName(source) + `\` + name
			if _, err := os.Stat(destination); !os.IsNotExist(err) {
				t.Skipf("unique root-level destination already exists; preserving it: %s", destination)
			}
			requireRefusal(t, source, destination, "unsafe destination (top-level directory)")
			if _, err := os.Stat(destination); !os.IsNotExist(err) {
				t.Fatalf("rejected drive-root child destination was created: %v", err)
			}
		})

		t.Run("drive root destination", func(t *testing.T) {
			driveRoot := filepath.VolumeName(source) + `\`
			requireRefusal(t, source, driveRoot, "unsafe destination (drive root)")
		})
	}

	t.Run("missing destination parent", func(t *testing.T) {
		parent := filepath.Join(t.TempDir(), "missing-parent")
		destination := filepath.Join(parent, "embed")
		requireRefusal(t, source, destination, "destination parent is not an existing directory")
		if _, err := os.Stat(parent); !os.IsNotExist(err) {
			t.Fatalf("rejected destination parent was created: %v", err)
		}
	})
}

func TestStageWorkspaceAssetsRejectsSourceSymlinkBeforeMutation(t *testing.T) {
	source := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	writeFixtureFile(t, outside, []byte("outside source"))
	createFixtureSymlink(t, outside, filepath.Join(source, "linked.txt"))

	destination := t.TempDir()
	sentinel := filepath.Join(destination, "sentinel.txt")
	writeFixtureFile(t, sentinel, []byte("preserve destination"))
	output, err := runStageHelper(t, source, destination)
	if err == nil || !strings.Contains(output, "source catalog contains unsupported symlink: linked.txt") {
		t.Fatalf("helper should reject a source symlink before staging, err=%v output=%s", err, output)
	}
	if body, readErr := os.ReadFile(sentinel); readErr != nil || string(body) != "preserve destination" {
		t.Fatalf("source symlink rejection mutated destination sentinel: %q, %v", body, readErr)
	}
	if body, readErr := os.ReadFile(outside); readErr != nil || string(body) != "outside source" {
		t.Fatalf("source symlink rejection changed external target: %q, %v", body, readErr)
	}
}

func TestStageWorkspaceAssetsRefusesDestinationSymlinkToSource(t *testing.T) {
	source := t.TempDir()
	sentinel := filepath.Join(source, "sentinel.txt")
	writeFixtureFile(t, sentinel, []byte("preserve source"))
	destinationLink := filepath.Join(t.TempDir(), "destination-link")
	createFixtureSymlink(t, source, destinationLink)

	output, err := runStageHelper(t, source, destinationLink)
	if err == nil || !strings.Contains(output, "source and destination are the same directory") {
		t.Fatalf("helper should reject a destination symlink to source, err=%v output=%s", err, output)
	}
	if body, readErr := os.ReadFile(sentinel); readErr != nil || string(body) != "preserve source" {
		t.Fatalf("rejected destination symlink changed source: %q, %v", body, readErr)
	}
}

func TestStageWorkspaceAssetsUnlinksDestinationChildSymlink(t *testing.T) {
	source := t.TempDir()
	writeFixtureFile(t, filepath.Join(source, "catalog.txt"), []byte("catalog"))
	destination := t.TempDir()
	externalTarget := filepath.Join(t.TempDir(), "outside.txt")
	writeFixtureFile(t, externalTarget, []byte("preserve external target"))
	childLink := filepath.Join(destination, "outside.txt")
	createFixtureSymlink(t, externalTarget, childLink)

	stageWithHelper(t, source, destination)
	if _, err := os.Lstat(childLink); !os.IsNotExist(err) {
		t.Fatalf("staging retained destination symlink %q (lstat error %v)", childLink, err)
	}
	if body, err := os.ReadFile(externalTarget); err != nil || string(body) != "preserve external target" {
		t.Fatalf("staging followed destination symlink and changed target: %q, %v", body, err)
	}
	if body, err := os.ReadFile(filepath.Join(destination, "catalog.txt")); err != nil || string(body) != "catalog" {
		t.Fatalf("staging did not copy regular source file: %q, %v", body, err)
	}
}
