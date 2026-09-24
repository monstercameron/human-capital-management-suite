package racepolicy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/racepolicy"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

func TestFindConcurrentPackages_ImportsSync(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkga/a.go", `package pkga

import "sync"

var mu sync.Mutex
`)
	writeFile(t, root, "pkgb/b.go", `package pkgb

func Do() {}
`)

	got, err := racepolicy.FindConcurrentPackages(root, "example.com/mod")
	if err != nil {
		t.Fatalf("FindConcurrentPackages: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("FindConcurrentPackages returned %d packages, want 1: %+v", len(got), got)
	}
	if got[0].ImportPath != "example.com/mod/pkga" {
		t.Errorf("ImportPath = %q, want example.com/mod/pkga", got[0].ImportPath)
	}
	if got[0].Dir != "pkga" {
		t.Errorf("Dir = %q, want pkga", got[0].Dir)
	}
	if len(got[0].Reasons) != 1 || got[0].Reasons[0] != "imports sync" {
		t.Errorf("Reasons = %v, want [imports sync]", got[0].Reasons)
	}
}

func TestFindConcurrentPackages_ImportsSyncAtomic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkga/a.go", `package pkga

import "sync/atomic"

var counter atomic.Int64
`)
	got, err := racepolicy.FindConcurrentPackages(root, "example.com/mod")
	if err != nil {
		t.Fatalf("FindConcurrentPackages: %v", err)
	}
	if len(got) != 1 || got[0].Reasons[0] != "imports sync/atomic" {
		t.Fatalf("FindConcurrentPackages = %+v, want one package flagged for sync/atomic", got)
	}
}

func TestFindConcurrentPackages_StartsGoroutine(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkga/a.go", `package pkga

func Do() {
	go func() {}()
}
`)
	got, err := racepolicy.FindConcurrentPackages(root, "example.com/mod")
	if err != nil {
		t.Fatalf("FindConcurrentPackages: %v", err)
	}
	if len(got) != 1 || got[0].Reasons[0] != "starts a goroutine" {
		t.Fatalf("FindConcurrentPackages = %+v, want one package flagged for starting a goroutine", got)
	}
}

func TestFindConcurrentPackages_TestFileCounts(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkga/a.go", `package pkga

func Do() {}
`)
	writeFile(t, root, "pkga/a_test.go", `package pkga

import (
	"sync"
	"testing"
)

func TestDo(t *testing.T) {
	var mu sync.Mutex
	_ = mu
}
`)
	got, err := racepolicy.FindConcurrentPackages(root, "example.com/mod")
	if err != nil {
		t.Fatalf("FindConcurrentPackages: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("FindConcurrentPackages = %+v, want the package declared concurrent because its test file uses sync", got)
	}
}

func TestFindConcurrentPackages_SkipsGeneratedAndTestdata(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "gen/go/hcmnext/thing.go", `package hcmnext

import "sync"

var mu sync.Mutex
`)
	writeFile(t, root, "somepkg/testdata/fixture.go", `package testdata

import "sync"

var mu sync.Mutex
`)
	writeFile(t, root, "somepkg/real.go", `package somepkg

func Do() {}
`)

	got, err := racepolicy.FindConcurrentPackages(root, "example.com/mod")
	if err != nil {
		t.Fatalf("FindConcurrentPackages: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("FindConcurrentPackages = %+v, want none (generated/testdata trees must be excluded)", got)
	}
}

func TestFindConcurrentPackages_ToolsGenIsNotExcluded(t *testing.T) {
	// Unlike gen/go, tools/gen holds hand-written generator
	// tooling, not generated output, and must still be scanned.
	root := t.TempDir()
	writeFile(t, root, "tools/gen/somegen/gen.go", `package somegen

import "sync"

var mu sync.Mutex
`)
	got, err := racepolicy.FindConcurrentPackages(root, "example.com/mod")
	if err != nil {
		t.Fatalf("FindConcurrentPackages: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("FindConcurrentPackages = %+v, want tools/gen/somegen to still be scanned", got)
	}
}

func TestFindConcurrentPackages_UnrelatedImportNotFlagged(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkga/a.go", `package pkga

import "golang.org/x/sync/errgroup"

var _ = errgroup.Group{}
`)
	got, err := racepolicy.FindConcurrentPackages(root, "example.com/mod")
	if err != nil {
		t.Fatalf("FindConcurrentPackages: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("FindConcurrentPackages = %+v, want none (golang.org/x/sync is not the stdlib \"sync\" package TOOL-012 names)", got)
	}
}

func TestFindConcurrentPackages_MalformedFileErrors(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkga/a.go", "not valid go {{{")
	if _, err := racepolicy.FindConcurrentPackages(root, "example.com/mod"); err == nil {
		t.Fatal("FindConcurrentPackages: expected a parse error, got nil")
	}
}

func TestFindConcurrentPackages_RootPackageImportPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

import "sync"

var mu sync.Mutex

func main() {}
`)
	got, err := racepolicy.FindConcurrentPackages(root, "example.com/mod")
	if err != nil {
		t.Fatalf("FindConcurrentPackages: %v", err)
	}
	if len(got) != 1 || got[0].ImportPath != "example.com/mod" || got[0].Dir != "" {
		t.Fatalf("FindConcurrentPackages = %+v, want the root package itself flagged as example.com/mod with empty Dir", got)
	}
}

func TestFindConcurrentPackagesSkipsDotDirectories(t *testing.T) {
	root := t.TempDir()
	src := "package p\n\nfunc Go() { go func() {}() }\n"
	for _, dir := range []string{"internal/live", ".artifacts/tmp/hcmnext-pg-1/leak", ".codex-tmp/leak", ".git/leak"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(dir), "p.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pkgs, err := racepolicy.FindConcurrentPackages(root, "example.com/m")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(pkgs) != 1 || filepath.ToSlash(pkgs[0].Dir) != "internal/live" {
		t.Fatalf("only the live package must be found, got %+v", pkgs)
	}
}

// TestFindConcurrentPackages_SkipsNestedModules pins the boundary that the
// race step depends on. The workflow feeds this scan straight into
// `go test -race`, and the root module cannot build a nested module's
// packages: naming them produced "FAIL ... [setup failed]" for all three
// src/blocks/go packages and took the whole TOOL-012 step down with them.
// `go test ./...` never crosses a go.mod, so neither may this walk.
func TestFindConcurrentPackages_SkipsNestedModules(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/outer\n\ngo 1.26\n")
	writeFile(t, root, "outer/outer.go", "package outer\n\nimport \"sync\"\n\nvar mu sync.Mutex\n")
	// A nested module that is itself unmistakably concurrent.
	writeFile(t, root, "nested/go.mod", "module example.com/nested\n\ngo 1.26\n")
	writeFile(t, root, "nested/inner.go", "package inner\n\nimport \"sync\"\n\nvar mu sync.Mutex\n")
	writeFile(t, root, "nested/deep/deep.go", "package deep\n\nimport \"sync\"\n\nvar mu sync.Mutex\n")

	packages, err := racepolicy.FindConcurrentPackages(root, "example.com/outer")
	if err != nil {
		t.Fatalf("FindConcurrentPackages: %v", err)
	}
	if len(packages) == 0 {
		t.Fatal("scan returned nothing; the outer concurrent package should still be found")
	}
	for _, pkg := range packages {
		if strings.Contains(pkg.ImportPath, "nested") {
			t.Errorf("nested module package %q was listed; the root module cannot build it", pkg.ImportPath)
		}
	}
}
