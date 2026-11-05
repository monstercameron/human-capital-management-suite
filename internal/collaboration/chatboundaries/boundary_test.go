package chatboundaries

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// forbiddenParents are the layers a chat domain library must never import.
// Test files are exempt: a test may reach anywhere to arrange its fixtures.
var forbiddenParents = []string{
	"/humanwork/",   // UI and workspace adapters
	"/application/", // composition roots
	"/transport/",   // protocol adapters
	"/tools/",       // developer tooling, including the journeywasm harness
	"/cmd/",         // binaries
}

// TestChatBoundaries scans every non-test Go file in internal/collaboration/chat*
// and fails on any import from a forbidden layer.
func TestChatBoundaries(t *testing.T) {
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test")
	}
	root := filepath.Join(filepath.Dir(self), "..", "..", "..")
	collab := filepath.Join(root, "internal", "collaboration")
	entries, err := os.ReadDir(collab)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	scanned := 0
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "chat") {
			continue
		}
		dir := filepath.Join(collab, entry.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			name := file.Name()
			if file.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			scanned++
			for _, spec := range f.Imports {
				imp := strings.Trim(spec.Path.Value, `"`)
				for _, parent := range forbiddenParents {
					if strings.Contains(imp, parent) {
						t.Errorf("%s imports %s: chat domain libraries must not depend on %s", path, imp, parent)
					}
				}
			}
		}
	}
	// The chat domain is many packages; a scan that finds almost nothing is
	// looking in the wrong place, not passing.
	if scanned < 100 {
		t.Fatalf("scanned %d files, want at least 100", scanned)
	}
	t.Logf("scanned %d chat domain files", scanned)
}

// TestChatBoundariesASTSanity pins the scanner against a fixture: a file that
// imports the UI layer is reported, whatever comments surround it.
func TestChatBoundariesASTSanity(t *testing.T) {
	src := "package p\nimport (\n\"fmt\"\n// a comment\nchatui \"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui\"\n)\n"
	f, err := parser.ParseFile(token.NewFileSet(), "fixture.go", src, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, spec := range f.Imports {
		if strings.Contains(strings.Trim(spec.Path.Value, `"`), "/humanwork/") {
			found = true
		}
	}
	if !found {
		t.Fatal("the scanner missed a humanwork import")
	}
}
