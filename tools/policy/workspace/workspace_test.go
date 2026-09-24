package workspace_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/layout"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/workspace"
)

// TestGoWorkspacePolicy is the TOOL-001 primary test.
func TestGoWorkspacePolicy(t *testing.T) {
	root := repopath.RootDir()

	t.Run("pinned Go version", func(t *testing.T) {
		info, err := workspace.ParseGoMod(root)
		if err != nil {
			t.Fatalf("parsing go.mod: %v", err)
		}
		if info.Go == "" {
			t.Fatalf("go.mod has no \"go\" directive")
		}
		if !workspace.IsPinnedGoVersion(info.Go) {
			t.Fatalf("go.mod \"go\" directive %q is not a pinned x.y.z version", info.Go)
		}
		if !workspace.IsAcceptableToolchain(info.Toolchain) {
			t.Fatalf("go.mod \"toolchain\" directive %q is not well-formed", info.Toolchain)
		}
	})

	t.Run("exactly one root module, no go.work", func(t *testing.T) {
		if workspace.HasGoWork(root) {
			t.Fatalf("found go.work at repository root; TOOL-001 requires one root module until a measured technical constraint justifies another")
		}
		layoutManifest, err := layout.Load(filepath.Join(root, "definitions", "architecture", "repository-layout.yaml"))
		if err != nil {
			t.Fatalf("loading repository-layout manifest: %v", err)
		}
		modules, err := workspace.FindGoModules(root, map[string]bool{
			".git": true, ".artifacts": true, "node_modules": true,
			"vendor": true, "testdata": true,
		})
		if err != nil {
			t.Fatalf("scanning Go modules: %v", err)
		}
		slices.Sort(modules)
		want := []string{"."}
		for _, exemption := range layoutManifest.LegacyModuleExemptions {
			want = append(want, filepath.ToSlash(exemption.Path))
		}
		slices.Sort(want)
		if !slices.Equal(modules, want) {
			t.Fatalf("Go modules = %v, want root plus only the manifest's legacy exemption %v", modules, want)
		}
	})

	t.Run("production packages only under allowed roots", func(t *testing.T) {
		layoutManifest, err := layout.Load(filepath.Join(root, "definitions", "architecture", "repository-layout.yaml"))
		if err != nil {
			t.Fatalf("loading repository-layout manifest: %v", err)
		}

		packages, err := repopath.ListPackages(root)
		if err != nil {
			t.Fatalf("listing packages: %v", err)
		}
		if len(packages) == 0 {
			t.Fatalf("go list ./... returned no packages")
		}

		for _, pkg := range packages {
			if v := layoutManifest.ClassifyImportPath(pkg.ImportPath); !v.Allowed {
				t.Errorf("package %s is outside the declared repository layout: %s", pkg.ImportPath, v.Reason)
			}
		}
	})

	t.Run("no Node or npm requirement on the Go build path", func(t *testing.T) {
		ignore := map[string]bool{
			".git": true, ".artifacts": true, "node_modules": true,
			"src/blocks/go": true, // the exact legacy module declared in repository-layout.yaml
			"testdata":      true, "dist": true, "tmp": true, "vendor": true,
		}

		hits, err := workspace.FindNodeExecCalls(root, ignore)
		if err != nil {
			t.Fatalf("scanning for Node/npm exec calls: %v", err)
		}
		for _, h := range hits {
			t.Errorf("found a literal os/exec invocation of npm/node in %s; the Go build path must not require Node/npm (planning/specs/go-only-technology-constitution.md)", h)
		}
	})

	t.Run("legacy module exemption is documented and exact", func(t *testing.T) {
		layoutManifest, err := layout.Load(filepath.Join(root, "definitions", "architecture", "repository-layout.yaml"))
		if err != nil {
			t.Fatalf("loading repository-layout manifest: %v", err)
		}
		if len(layoutManifest.LegacyModuleExemptions) == 0 {
			t.Fatalf("repository-layout manifest declares no legacy_module_exemptions")
		}
		found := false
		for _, e := range layoutManifest.LegacyModuleExemptions {
			if e.Path == "src/blocks/go" {
				found = true
				if e.Module != "human-capital-management-suite-executor" {
					t.Errorf("src/blocks/go exemption module = %q", e.Module)
				}
				if e.Expiry == "" {
					t.Errorf("src/blocks/go legacy exemption has no expiry")
				}
				if e.Owner == "" {
					t.Errorf("src/blocks/go legacy exemption has no owner")
				}
			}
		}
		if !found {
			t.Errorf("repository-layout manifest does not document the src/blocks/go legacy module exemption")
		}
	})
}
