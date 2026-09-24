package layout_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

func TestTodo_ARCH_GO_001_Golden(t *testing.T) {
	path := filepath.Join(repopath.RootDir(), "definitions", "architecture", "repository-layout.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprintf("%X", sha256.Sum256(content))
	const want = "F801BD09728EA18357442B7563E36D2A836E15591A6EE4A71F99A74F7B0DE45E"
	if got != want {
		t.Fatalf("repository-layout.yaml SHA256 = %s, want %s", got, want)
	}
}

func TestTodo_ARCH_GO_001_Conformance(t *testing.T) {
	m, _ := loadManifest(t)
	mod := m.Module
	vectors := []struct {
		path    string
		allowed bool
	}{
		{mod + "/cmd/hcmnext", true}, {mod + "/cmd/frontenddev", true},
		{mod + "/api", true}, {mod + "/definitions/architecture", true},
		{mod + "/internal/kernel/values", true}, {mod + "/migrations", true},
		{mod + "/test/conformance", true}, {mod + "/tools/policy/layout", true},
		{mod + "/gen/go/example", true}, {mod + "/cmd/retired", false},
		{mod + "/internal/unowned/pkg", false}, {mod + "/vendor/example", false},
		{"example.org/other", false},
	}
	for _, vector := range vectors {
		if got := m.ClassifyImportPath(vector.path).Allowed; got != vector.allowed {
			t.Errorf("ClassifyImportPath(%q).Allowed = %v, want %v", vector.path, got, vector.allowed)
		}
	}
}

func TestTodo_ARCH_GO_001_Integration(t *testing.T) {
	m, root := loadManifest(t)
	entries, err := os.ReadDir(filepath.Join(root, "cmd"))
	if err != nil {
		t.Fatal(err)
	}
	var actual []string
	for _, entry := range entries {
		if entry.IsDir() {
			actual = append(actual, entry.Name())
		}
	}
	var approved []string
	for _, name := range m.ApprovedCommands.Initial {
		approved = append(approved, name)
	}
	sort.Strings(actual)
	sort.Strings(approved)
	if !reflect.DeepEqual(actual, approved) {
		t.Fatalf("cmd directories = %v, approved_commands.initial = %v", actual, approved)
	}
}

func TestTodo_ARCH_GO_001_Mutation(t *testing.T) {
	m, _ := loadManifest(t)
	path := m.Module + "/cmd/hcmnext"
	if !m.ClassifyImportPath(path).Allowed {
		t.Fatal("baseline approved command was rejected")
	}
	m.ApprovedCommands.Initial = nil
	if verdict := m.ClassifyImportPath(path); verdict.Allowed || verdict.Reason == "" {
		t.Fatalf("removing the command approval left it allowed: %+v", verdict)
	}
}
