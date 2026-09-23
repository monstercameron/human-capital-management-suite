package clockinject

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ScanRoots are the repository-relative trees whose non-test Go files must
// not read the wall clock directly.
var ScanRoots = []string{"internal/engines", "internal/domains"}

// DeclaredAdapters names the only direct time.Now reads the policy
// tolerates, each with the reason it is a fallback rather than a clock.
// The key is a repository-relative file path plus the enclosing function
// name, joined by a colon.
var DeclaredAdapters = map[string]string{
	"internal/engines/abuse/investigation.go:now":  "zero-value fallback; OpenInvestigation always injects a clock",
	"internal/domains/pseudonym/revelation.go:now": "nil-Clock fallback; callers inject through RevelationPolicy.Clock",
}

// Finding is one direct wall-clock read outside the declared adapters.
// File is relative to the scan root and Line is one-based.
type Finding struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Function string `json:"function"`
	Reason   string `json:"reason"`
}

// Report is the deterministic result of scanning the engine and domain
// trees. An empty Violations means every timestamp flows from an injected
// clock.
type Report struct {
	Findings []Finding `json:"findings"`
}

// Violations returns the findings in file-then-line order.
func (r Report) Violations() []Finding {
	out := append([]Finding(nil), r.Findings...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// Evaluate scans root's engine and domain trees and reports every direct
// time.Now read outside the declared adapters.
func Evaluate(root string) (Report, error) {
	var findings []Finding
	for _, rel := range ScanRoots {
		dir := filepath.Join(root, filepath.FromSlash(rel))
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			// A fixture or partial tree may hold only one scan root;
			// the live-tree contract test always has both.
			continue
		}
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if info.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(info.Name(), ".go") || strings.HasSuffix(info.Name(), "_test.go") {
				return nil
			}
			relFile, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			found, err := scanFile(path, filepath.ToSlash(relFile))
			if err != nil {
				return fmt.Errorf("clockinject: scan %s: %w", relFile, err)
			}
			findings = append(findings, found...)
			return nil
		})
		if err != nil {
			return Report{}, err
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return Report{Findings: findings}, nil
}

// scanFile parses one Go file and reports its direct time.Now reads.
func scanFile(path, relFile string) ([]Finding, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	if !importsTime(f) {
		return nil, nil
	}
	var findings []Finding
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}
		inspectFunc(fset, relFile, fn.Name.Name, fn.Body, &findings)
		return false
	})
	// Package-level variable initializers live outside any FuncDecl; a
	// direct read there has no enclosing function to declare as an adapter.
	// Adapter defaults (func() time.Time literals) are skipped the same way
	// as inside function bodies. Only top-level declarations are visited
	// here so findings inside function bodies are not reported twice.
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		ast.Inspect(gen, func(m ast.Node) bool {
			if lit, ok := m.(*ast.FuncLit); ok && isClockLiteral(lit) {
				return false
			}
			call, ok := m.(*ast.CallExpr)
			if !ok {
				return true
			}
			if isWallClockRead(call) {
				findings = append(findings, Finding{
					File:     relFile,
					Line:     fset.Position(call.Pos()).Line,
					Function: "<package>",
					Reason:   "direct time.Now read outside any function",
				})
			}
			return true
		})
	}
	return findings, nil
}

// importsTime reports whether the file imports the time package under its
// default name, so a local identifier named time cannot be mistaken for
// the wall clock.
func importsTime(f *ast.File) bool {
	for _, imp := range f.Imports {
		if imp.Name != nil {
			continue
		}
		if strings.Trim(imp.Path.Value, `"`) == "time" {
			return true
		}
	}
	return false
}

// inspectFunc reports direct time.Now reads in one function body, skipping
// the wall-default adapter idiom and the declared adapters.
func inspectFunc(fset *token.FileSet, relFile, funcName string, body *ast.BlockStmt, findings *[]Finding) {
	if body == nil {
		return
	}
	key := relFile + ":" + funcName
	if _, ok := DeclaredAdapters[key]; ok {
		return
	}
	ast.Inspect(body, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if ok && isClockLiteral(lit) {
			// The wall-default adapter idiom: the injected clock's
			// fallback, not a direct read. Do not descend.
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !isWallClockRead(call) {
			return true
		}
		*findings = append(*findings, Finding{
			File:     relFile,
			Line:     fset.Position(call.Pos()).Line,
			Function: funcName,
			Reason:   "direct time.Now read; take an injected clock instead",
		})
		return true
	})
}

// isClockLiteral reports whether the literal has the adapter shape
// func() time.Time, the declared wall-default form.
func isClockLiteral(lit *ast.FuncLit) bool {
	t := lit.Type
	if t == nil || len(t.Params.List) != 0 || t.Results == nil || len(t.Results.List) != 1 {
		return false
	}
	sel, ok := t.Results.List[0].Type.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "time" && sel.Sel.Name == "Time"
}

// isWallClockRead reports whether the call expression is time.Now().
func isWallClockRead(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Now" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "time" && len(call.Args) == 0
}
