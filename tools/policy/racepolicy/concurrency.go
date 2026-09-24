// Package racepolicy declares which packages in this repository use
// concurrency primitives (import "sync" or "sync/atomic", or start a
// goroutine) and checks that every one of them carries a test suite that
// would actually run under `go test -race` on the Linux CI job where this
// host's own toolchain cannot run the race detector at all (TOOL-012;
// windows/arm64 has no race detector support). See doc.go for the full
// rationale and policy.go for the check itself.
package racepolicy

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ConcurrentPackage is one directory (Go package) whose source uses a
// concurrency primitive this policy tracks.
type ConcurrentPackage struct {
	// ImportPath is the package's full import path, e.g.
	// "github.com/monstercameron/human-capital-management-suite/internal/domains/workflow".
	ImportPath string
	// Dir is the package's directory, relative to the scan root, with
	// forward slashes (e.g. "internal/domains/workflow").
	Dir string
	// Reasons lists every distinct trigger found (e.g. "imports sync",
	// "imports sync/atomic", "starts a goroutine"), deduplicated and
	// sorted, so a report can say why a package was declared concurrent.
	Reasons []string
}

// concurrencyImports are the standard-library packages whose mere presence
// in an import list marks a package concurrent for this policy's purposes.
// This is deliberately narrow (TOOL-012 names exactly "sync/atomic, sync"):
// it does not chase every concurrency-adjacent third-party package.
var concurrencyImports = map[string]string{
	"sync":        "imports sync",
	"sync/atomic": "imports sync/atomic",
}

const goroutineReason = "starts a goroutine"

// skipDirNames are directory names never descended into: version control,
// JS tooling, and fixture/generated trees that are not hand-written Go
// packages this policy should ever declare concurrent.
var skipDirNames = map[string]bool{
	".git":         true,
	".husky":       true,
	"node_modules": true,
	"testdata":     true,
}

// isGeneratedPath reports whether relDir (forward-slash, relative to the
// scan root) is, or is inside, a generated-code tree: this repository's
// protobuf output under gen/go. tools/gen is deliberately
// NOT excluded here — despite the name, it holds hand-written generator
// *tooling* (its own subpackages, cmd/, tests), not generated output.
func isGeneratedPath(relDir string) bool {
	return relDir == "gen/go" || strings.HasPrefix(relDir, "gen/go/")
}

// FindConcurrentPackages walks root and returns every Go package (directory
// containing at least one .go file) whose source imports "sync",
// "sync/atomic", or starts a goroutine (a `go` statement) anywhere in a
// non-generated, non-testdata .go file — production or test code alike, so
// a package whose only concurrency lives in a test helper is still
// declared. The result is sorted by ImportPath for determinism.
func FindConcurrentPackages(root, modulePath string) ([]ConcurrentPackage, error) {
	found := make(map[string]*ConcurrentPackage) // dir -> package

	fset := token.NewFileSet()
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			name := info.Name()
			// Dot-prefixed directories (.git, .artifacts, lane and cache
			// scratch) are never part of the module and may be mid-write by
			// another process, so they are skipped before anything under them
			// is stat'ed.
			if name != "." && (skipDirNames[name] || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			// A directory carrying its own go.mod is a separate module.
			// `go test ./...` never crosses that boundary, so neither may a
			// filesystem scan that feeds `go test`: the root module cannot
			// build a nested module's packages, and naming them turns the
			// race step into "FAIL ... [setup failed]". src/blocks/go is
			// covered by its own CI job. Checking for go.mod rather than
			// hard-coding that path keeps any future nested module correct
			// without another edit here.
			if path != root {
				if _, statErr := os.Stat(filepath.Join(path, "go.mod")); statErr == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		relDir := filepath.ToSlash(filepath.Dir(relPath))
		if relDir == "." {
			relDir = ""
		}
		if isGeneratedPath(relDir) {
			return nil
		}

		reasons, err := fileConcurrencyReasons(fset, path)
		if err != nil {
			return err
		}
		if len(reasons) == 0 {
			return nil
		}

		pkg, ok := found[relDir]
		if !ok {
			importPath := modulePath
			if relDir != "" {
				importPath = modulePath + "/" + relDir
			}
			pkg = &ConcurrentPackage{ImportPath: importPath, Dir: relDir}
			found[relDir] = pkg
		}
		pkg.Reasons = appendUnique(pkg.Reasons, reasons...)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	result := make([]ConcurrentPackage, 0, len(found))
	for _, pkg := range found {
		sort.Strings(pkg.Reasons)
		result = append(result, *pkg)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ImportPath < result[j].ImportPath })
	return result, nil
}

// fileConcurrencyReasons parses one Go source file and reports which
// concurrency triggers it contains. Go's parser does not evaluate build
// constraints, so a file is inspected regardless of its own `//go:build`
// tags; a syntactically invalid file is reported as an error rather than
// silently skipped, since a package this policy cannot even parse cannot
// be honestly declared either concurrent or safe.
func fileConcurrencyReasons(fset *token.FileSet, path string) ([]string, error) {
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}

	var reasons []string
	for _, imp := range f.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		if reason, ok := concurrencyImports[importPath]; ok {
			reasons = appendUnique(reasons, reason)
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if _, ok := n.(*ast.GoStmt); ok {
			reasons = appendUnique(reasons, goroutineReason)
		}
		return true
	})

	return reasons, nil
}

func appendUnique(list []string, items ...string) []string {
	for _, item := range items {
		exists := false
		for _, existing := range list {
			if existing == item {
				exists = true
				break
			}
		}
		if !exists {
			list = append(list, item)
		}
	}
	return list
}
