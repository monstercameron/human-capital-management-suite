package libfirewall

import (
	"path/filepath"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/depmanifest"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

// Report is the deterministic result of running the library firewall over
// every direct import edge in root's module graph. An empty Violations
// means every third-party import sits inside its manifest row's
// allowed_import_roots: the REV-101-05 reconciled state.
type Report struct {
	ManifestPath string
	Packages     int
	Violations   []Violation
}

// Evaluate loads root's dependency-roles manifest and runs CheckImport over
// every direct import edge `go list -json ./...` reports for root's own
// module (REV-101-05 gate entry point; the cmd/libfirewall command and the
// TestTodo_REV_101_05 reconciliation test both call it).
func Evaluate(root string) (Report, error) {
	manifestPath := filepath.Join(root, "definitions", "architecture", "dependency-roles.yaml")
	manifest, err := depmanifest.Load(manifestPath)
	if err != nil {
		return Report{}, err
	}
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		return Report{}, err
	}
	report := CheckAll(manifest, pkgs)
	report.ManifestPath = manifestPath
	return report, nil
}

// CheckAll runs CheckPackage for every listed package and returns the
// violations in importer-then-import order.
func CheckAll(manifest *depmanifest.Manifest, pkgs []repopath.Package) Report {
	var out []Violation
	for _, pkg := range pkgs {
		out = append(out, CheckPackage(manifest, pkg.ImportPath, pkg.Imports)...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Importer != out[j].Importer {
			return out[i].Importer < out[j].Importer
		}
		return out[i].ImportedPath < out[j].ImportedPath
	})
	return Report{Packages: len(pkgs), Violations: out}
}
