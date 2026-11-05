package libfirewall_test

import (
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/depmanifest"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/libfirewall"
)

const mongoImportPath = "go.mongodb.org/mongo-driver"

// mongoTypesPath is the subpackage the governed handles live in; the
// leak-type filter matches Exposure.ImportPath exactly, so the module root
// alone would match nothing.
const mongoTypesPath = "go.mongodb.org/mongo-driver/mongo"

const mongoAdapterRoot = "internal/connectivity/mongo"

// mongoOwningRoots returns dependency-roles.yaml's own allowed_import_roots
// for the mongo driver: exactly the WF-DATA-015 adapter boundary.
func mongoOwningRoots(t *testing.T, roles *depmanifest.Manifest) []string {
	t.Helper()
	for _, row := range roles.Modules {
		if row.Path == mongoImportPath {
			return row.AllowedImportRoots
		}
	}
	t.Fatalf("dependency-roles.yaml has no row for %s", mongoImportPath)
	return nil
}

const leakedMongoClientSource = `package fakeadapter

import "go.mongodb.org/mongo-driver/mongo"

// Open leaks the raw *mongo.Client to any caller; RED fixture.
func Open() *mongo.Client { return nil }
`

// TestMongoAdapterQualification pins the WF-DATA-015 admission contract:
// the driver row exists, confines imports to the adapter root, and the
// leak-type list names the driver handles that must never surface.
func TestMongoAdapterQualification(t *testing.T) {
	cfg, roles := loadFirewallConfigAndRoles(t)

	roots := mongoOwningRoots(t, roles)
	if len(roots) != 1 || roots[0] != mongoAdapterRoot {
		t.Fatalf("mongo allowed_import_roots = %v, want exactly [%s]", roots, mongoAdapterRoot)
	}

	typeNames := libfirewall.LeakTypesByImport(cfg.MongoLeakTypes)[mongoTypesPath]
	want := map[string]bool{"Client": true, "Database": true, "Collection": true, "Cursor": true, "SingleResult": true}
	if len(typeNames) != len(want) {
		t.Fatalf("mongo leak types = %v, want %v", typeNames, want)
	}
	for _, name := range typeNames {
		if !want[name] {
			t.Errorf("unexpected mongo leak type %q", name)
		}
	}

	exposures, err := libfirewall.ScanSource("fakeadapter.go", leakedMongoClientSource, []string{mongoImportPath})
	if err != nil {
		t.Fatalf("scanning RED fixture: %v", err)
	}
	leaks := libfirewall.FilterLeakTypes(exposures, mongoTypesPath, typeNames)
	if len(leaks) != 1 || leaks[0].Name != "Open" {
		t.Fatalf("RED fixture: got leaks %v, want exactly [Open]", leaks)
	}
}

// TestTodo_WF_DATA_015_FirewallIntegration scans the real repo: every
// mongo-driver import must sit inside the adapter root, the adapter root
// must exist and export no driver handle, and no other package may import
// the driver. RED until internal/connectivity/mongo exists.
func TestTodo_WF_DATA_015_FirewallIntegration(t *testing.T) {
	cfg, roles := loadFirewallConfigAndRoles(t)
	root := repopath.RootDir()
	typeNames := libfirewall.LeakTypesByImport(cfg.MongoLeakTypes)[mongoTypesPath]

	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var boundaryViolations int
	var ownedDirs []string
	for _, pkg := range pkgs {
		for _, imp := range pkg.Imports {
			if v := libfirewall.CheckImport(roles, pkg.ImportPath, imp); v != nil && v.Module == mongoImportPath {
				boundaryViolations++
				t.Errorf("WF-DATA-015 import boundary violation: %s imports %s (allowed: %v)", v.Importer, v.ImportedPath, v.AllowedRoots)
			}
		}
		for _, imp := range pkg.Imports {
			if imp == mongoImportPath || (len(imp) > len(mongoImportPath) && imp[:len(mongoImportPath)+1] == mongoImportPath+"/") {
				ownedDirs = append(ownedDirs, pkg.Dir)
				break
			}
		}
	}
	if len(ownedDirs) == 0 {
		t.Fatalf("no package imports %s yet: internal/connectivity/mongo does not exist (WF-DATA-015 RED)", mongoImportPath)
	}

	var leakViolations int
	for _, dir := range ownedDirs {
		exposures, err := libfirewall.ScanExposures(dir, []string{mongoImportPath})
		if err != nil {
			t.Fatalf("scanning %s: %v", dir, err)
		}
		for _, exp := range libfirewall.FilterLeakTypes(exposures, mongoTypesPath, typeNames) {
			leakViolations++
			t.Errorf("WF-DATA-015 leaked mongo type: %s:%d %s %s exposes %s", exp.File, exp.Line, exp.Kind, exp.Name, exp.Type)
		}
	}
	adapterDir := filepath.Join(root, filepath.FromSlash(mongoAdapterRoot))
	found := false
	for _, dir := range ownedDirs {
		if dir == adapterDir {
			found = true
		}
	}
	if !found {
		t.Errorf("mongo imports live in %v, none is the adapter root %s", ownedDirs, adapterDir)
	}
	t.Logf("mongo: %d import-boundary violations, %d owning packages scanned, %d leaked-type violations", boundaryViolations, len(ownedDirs), leakViolations)
}
