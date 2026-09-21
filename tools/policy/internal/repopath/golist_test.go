package repopath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListPackagesResolvesRelativeRepositoryRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/relative\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sample.go"), []byte("package sample\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(cwd, root)
	if err != nil {
		t.Fatal(err)
	}
	packages, err := ListPackages(rel)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 1 || packages[0].ImportPath != "example.com/relative" {
		t.Fatalf("relative-root package list = %+v", packages)
	}
}
