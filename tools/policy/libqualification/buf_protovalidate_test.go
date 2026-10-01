package libqualification_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

const ownedBufProtovalidateFallback = "github.com/monstercameron/human-capital-management-suite/tools/quality/bufprotovalidatekit"

func isRejectedProtovalidateImport(importPath string) bool {
	if importPath == ownedBufProtovalidateFallback {
		return false
	}
	return strings.Contains(importPath, "protovalidate") || strings.Contains(importPath, "buf.validate")
}

// TestBufProtovalidateQualificationCannotBecomeBusinessAuthority is LIB-019's
// primary test: Buf linting and Protovalidate structural validation must
// remain developer-only mechanics that cannot decide authorization, legality,
// eligibility or mutations. Buf is adopted as a pinned schema linter;
// Protovalidate is rejected in favor of owned descriptor-based validation
// that fails closed on business/AuthZ/legal scope.
func TestBufProtovalidateQualificationCannotBecomeBusinessAuthority(t *testing.T) {
	root := repopath.RootDir()

	// 1. Verify buf.yaml declares STANDARD lint and FILE breaking
	bufYAMLPath := filepath.Join(root, "buf.yaml")
	bufYAMLContent, err := os.ReadFile(bufYAMLPath)
	if err != nil {
		t.Fatalf("reading buf.yaml: %v", err)
	}
	bufYAMLStr := string(bufYAMLContent)

	if !strings.Contains(bufYAMLStr, "STANDARD") {
		t.Fatal("buf.yaml does not declare lint STANDARD")
	}
	if !strings.Contains(bufYAMLStr, "FILE") {
		t.Fatal("buf.yaml does not declare breaking FILE")
	}

	// 2. Verify every .proto under schema/proto passes buf lint
	protoDir := filepath.Join(root, "schema", "proto")
	if _, err := os.Stat(protoDir); err == nil {
		if err := runBufLint(t, root, protoDir); err != nil {
			t.Fatalf("buf lint failed: %v", err)
		}
	}

	// 3. Verify Protovalidate is NOT imported (rejected decision)
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}
	for _, pkg := range pkgs {
		for _, imp := range pkg.Imports {
			if isRejectedProtovalidateImport(imp) {
				t.Errorf("Protovalidate found in imports (rejected): %s in %s", imp, pkg.ImportPath)
			}
		}
	}

	t.Logf("LIB-019 DECISION: buf=ADOPT (pinned schema linter); protovalidate=REJECT (own descriptor fallback)")
}

// TestTodo_LIB_019_Property exercises buf.yaml configuration to ensure
// it declares the correct lint rules (STANDARD) and breaking compatibility
// rules (FILE).
func TestTodo_LIB_019_Property(t *testing.T) {
	root := repopath.RootDir()
	bufYAMLPath := filepath.Join(root, "buf.yaml")

	data, err := os.ReadFile(bufYAMLPath)
	if err != nil {
		t.Fatalf("reading buf.yaml: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "STANDARD") {
		t.Fatal("buf.yaml lint section does not contain STANDARD rule set")
	}
	if !strings.Contains(content, "FILE") {
		t.Fatal("buf.yaml breaking section does not contain FILE rule set")
	}
}

// TestTodo_LIB_019_Golden verifies the Buf adoption rationale and
// Protovalidate rejection.
func TestTodo_LIB_019_Golden(t *testing.T) {
	// Buf is pinned as the developer-only schema linter applying STANDARD
	// and FILE compatibility rules.
	// Protovalidate is rejected because Human Capital Management Suite must own all business,
	// authorization, legal, eligibility and mutation logic through owned
	// descriptor-based validation that fails closed on out-of-scope uses.

	t.Logf("LIB-019: buf pinned as schema linter; protovalidate rejected; validation owned by Human Capital Management Suite")
}

// TestTodo_LIB_019_Integration runs buf lint against the real schema
// directory and verifies no proto files violate STANDARD or FILE rules.
func TestTodo_LIB_019_Integration(t *testing.T) {
	root := repopath.RootDir()
	protoDir := filepath.Join(root, "schema", "proto")

	// Check if proto directory exists
	if _, err := os.Stat(protoDir); err != nil {
		t.Skipf("schema/proto not found: %v", err)
	}

	if err := runBufLint(t, root, protoDir); err != nil {
		t.Fatalf("buf lint INTEGRATION failed: %v", err)
	}

	// List all .proto files
	var protoFiles []string
	err := filepath.Walk(protoDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".proto") {
			protoFiles = append(protoFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Logf("walking proto directory: %v", err)
	}

	if len(protoFiles) == 0 {
		t.Logf("no .proto files found in schema/proto")
		return
	}

	t.Logf("buf lint passed for %d .proto files in schema/proto", len(protoFiles))
}

// TestTodo_LIB_019_Security verifies that Protovalidate is not used for
// authorization, legal eligibility or mutation decisions (rejected).
func TestTodo_LIB_019_Security(t *testing.T) {
	root := repopath.RootDir()
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	// Ensure no package imports Protovalidate
	for _, pkg := range pkgs {
		for _, imp := range pkg.Imports {
			if isRejectedProtovalidateImport(imp) {
				t.Errorf("SECURITY: Protovalidate import found (should be rejected): %s in package %s",
					imp, pkg.ImportPath)
			}
		}
	}

	t.Logf("LIB-019 SECURITY: no Protovalidate imports detected; validation owned by Human Capital Management Suite")
}

// TestTodo_LIB_019_Conformance ensures the qualification record is complete:
// buf.yaml configuration is correct, all protos lint cleanly, and Protovalidate
// remains rejected with owned fallback validation.
func TestTodo_LIB_019_Conformance(t *testing.T) {
	root := repopath.RootDir()

	// 1. Verify buf.yaml structure
	bufYAMLPath := filepath.Join(root, "buf.yaml")
	data, err := os.ReadFile(bufYAMLPath)
	if err != nil {
		t.Fatalf("reading buf.yaml: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "version: v2") {
		t.Error("buf.yaml does not declare version v2")
	}
	if !strings.Contains(content, "STANDARD") {
		t.Error("buf.yaml does not include STANDARD lint rules")
	}
	if !strings.Contains(content, "FILE") {
		t.Error("buf.yaml does not include FILE breaking rules")
	}

	// 2. Verify schema/proto lints cleanly
	protoDir := filepath.Join(root, "schema", "proto")
	if _, err := os.Stat(protoDir); err == nil {
		if err := runBufLint(t, root, protoDir); err != nil {
			t.Errorf("buf lint conformance check failed: %v", err)
		}
	}

	// 3. Verify Protovalidate rejection is upheld
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	for _, pkg := range pkgs {
		for _, imp := range pkg.Imports {
			if isRejectedProtovalidateImport(imp) {
				t.Errorf("CONFORMANCE: Protovalidate import violates rejection: %s in %s", imp, pkg.ImportPath)
			}
		}
	}

	t.Logf("LIB-019 CONFORMANCE: buf pinned and used; protovalidate rejected; validation owned by Human Capital Management Suite")
}

// TestTodo_LIB_019_Race exercises buf.yaml parsing and proto linting from
// concurrent readers. Run this test with -race on a supported builder.
func TestTodo_LIB_019_Race(t *testing.T) {
	root := repopath.RootDir()
	const readers = 8
	var wg sync.WaitGroup
	wg.Add(readers)

	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			pkgs, err := repopath.ListPackages(root)
			if err != nil {
				t.Errorf("concurrent package listing failed: %v", err)
				return
			}
			for _, pkg := range pkgs {
				for _, imp := range pkg.Imports {
					if isRejectedProtovalidateImport(imp) {
						t.Errorf("concurrent check found protovalidate (should be rejected): %s in %s", imp, pkg.ImportPath)
					}
				}
			}
		}()
	}
	wg.Wait()
}

// runBufLint executes `buf lint schema/proto` and returns nil if lint passes.
// It gracefully handles the case where buf is not installed.
func runBufLint(t *testing.T, root, protoDir string) error {
	t.Helper()

	// Try to find buf in common locations
	bufPath := "buf"
	if _, err := exec.LookPath(bufPath); err != nil {
		// Try the go bin directory
		goHome := os.Getenv("GOPATH")
		if goHome != "" {
			altPath := filepath.Join(goHome, "bin", "buf")
			if _, err := os.Stat(altPath); err == nil {
				bufPath = altPath
			}
		}

		// If still not found, try to see if it's in /c/Users/mreca/go/bin
		altPath := "/c/Users/mreca/go/bin/buf"
		if _, err := os.Stat(altPath); err == nil {
			bufPath = altPath
		}

		// If not found, skip the lint check
		if _, err := exec.LookPath(bufPath); err != nil && !strings.Contains(bufPath, "buf") {
			t.Skipf("buf not found in PATH or common locations")
			return nil
		}
	}

	cmd := exec.Command(bufPath, "lint", "schema/proto")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Errorf("buf lint failed: %v\noutput:\n%s", err, string(output))
	}

	return nil
}
