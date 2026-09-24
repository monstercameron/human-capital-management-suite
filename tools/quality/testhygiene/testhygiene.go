// Package testhygiene rejects test-hygiene violations: alias tests (a test
// whose body only calls another test), _Race tests without concurrency (no
// goroutine and no concurrency helper), and Golden tests that skip (a
// missing golden file must fail, never skip). It is REV-103-03's lint.
package testhygiene

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

// Rule identifiers reported by this package. They are part of the
// REV-103-03 contract and are pinned by TestTodo_REV_103_03_Golden.
const (
	RuleAliasTest              = "alias-test"
	RuleRaceWithoutConcurrency = "race-without-concurrency"
	RuleGoldenSkip             = "golden-skip"
)

// Violation is one rejected test function.
type Violation struct {
	// File is the offending file path: slash-separated, relative to the
	// scan root for tree scans, or the given name for CheckSource.
	File string
	// Line is the 1-based line of the test func (alias, race) or of the
	// skip call (golden-skip).
	Line int
	// Test is the offending test function name.
	Test string
	// Rule is one of the Rule* identifiers above.
	Rule string
	// Detail is a human-readable reason naming the evidence.
	Detail string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s:%d: %s: %s: %s", v.File, v.Line, v.Test, v.Rule, v.Detail)
}

// skipSelectors are the testing skip calls a Golden test must never use: a
// missing golden oracle is a failure, never a skip.
var skipSelectors = map[string]bool{
	"Skip":    true,
	"Skipf":   true,
	"SkipNow": true,
}

// skipDirNames mirrors the repository's scanner ignore lists (racepolicy,
// quality checks): fixture, generated, vendored and tooling trees are never
// linted as hand-written tests.
var skipDirNames = map[string]bool{
	".git":         true,
	".husky":       true,
	"node_modules": true,
	"dist":         true,
	"tmp":          true,
	"vendor":       true,
	"testdata":     true,
	"src":          true,
}

// isGeneratedPath reports whether relDir is generated protobuf/wire output,
// which carries no hand-written tests.
func isGeneratedPath(relDir string) bool {
	return relDir == "gen/go" || strings.HasPrefix(relDir, "gen/go/")
}

// CheckSource parses one Go test source and returns its hygiene violations
// in deterministic order. filename names the file in reports; sources that
// are not Go test files yield no violations. A syntactically invalid file
// is an error, never a silent pass.
func CheckSource(filename string, src []byte) ([]Violation, error) {
	if !strings.HasSuffix(filename, "_test.go") {
		return nil, nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("testhygiene: parse %s: %w", filename, err)
	}
	var out []Violation
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil {
			continue
		}
		name := fn.Name.Name
		if !strings.HasPrefix(name, "Test") || name == "TestMain" {
			continue
		}
		line := fset.Position(fn.Pos()).Line
		if callees, alias := aliasCallees(fn.Body); alias {
			out = append(out, Violation{File: filename, Line: line, Test: name, Rule: RuleAliasTest,
				Detail: fmt.Sprintf("test body only calls another test (%s)", strings.Join(callees, ", "))})
		}
		if strings.HasSuffix(name, "_Race") && !hasConcurrency(fn.Body, fn, f) {
			out = append(out, Violation{File: filename, Line: line, Test: name, Rule: RuleRaceWithoutConcurrency,
				Detail: "Race test starts no goroutine and uses no concurrent runner (go statement, t.Parallel, errgroup.Group.Go)"})
		}
		if strings.Contains(name, "Golden") {
			if call, line := goldenSkip(fset, fn.Body, f); call != "" {
				out = append(out, Violation{File: filename, Line: line, Test: name, Rule: RuleGoldenSkip,
					Detail: fmt.Sprintf("golden test must fail on a missing file, not skip (%s)", call)})
			}
		}
	}
	sortViolations(out)
	return out, nil
}

// aliasCallees reports whether every statement in body is a call to another
// test function (an Ident callee starting with "Test"). Method calls such
// as t.Run, assertions and assignments all break the alias shape.
func aliasCallees(body *ast.BlockStmt) ([]string, bool) {
	if len(body.List) == 0 {
		return nil, false
	}
	var callees []string
	for _, stmt := range body.List {
		found, ok := aliasStatement(stmt)
		if !ok || len(found) == 0 {
			return nil, false
		}
		callees = append(callees, found...)
	}
	return callees, true
}

// aliasStatement accepts only control-flow statements whose every executable
// leaf is a direct call to a Test function. It intentionally does not follow
// arbitrary function values or helpers: that would need type/control-flow analysis.
func aliasStatement(stmt ast.Stmt) ([]string, bool) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		_, ok := s.X.(*ast.CallExpr)
		if !ok {
			return nil, false
		}
		fun := s.X.(*ast.CallExpr).Fun
		for {
			p, ok := fun.(*ast.ParenExpr)
			if !ok {
				break
			}
			fun = p.X
		}
		switch c := fun.(type) {
		case *ast.Ident:
			if strings.HasPrefix(c.Name, "Test") {
				return []string{c.Name}, true
			}
		case *ast.SelectorExpr:
			if strings.HasPrefix(c.Sel.Name, "Test") {
				return []string{c.Sel.Name}, true
			}
		}
		return nil, false
	case *ast.DeferStmt:
		return aliasStatement(&ast.ExprStmt{X: s.Call})
	case *ast.BlockStmt:
		return aliasCallees(s)
	case *ast.IfStmt:
		if s.Init != nil {
			return nil, false
		}
		a, ok := aliasCallees(s.Body)
		if !ok {
			return nil, false
		}
		if s.Else == nil {
			return a, true
		}
		var b []string
		switch e := s.Else.(type) {
		case *ast.BlockStmt:
			b, ok = aliasCallees(e)
		case *ast.IfStmt:
			b, ok = aliasStatement(e)
		}
		if !ok {
			return nil, false
		}
		return append(a, b...), true
	}
	return nil, false
}

// hasConcurrency reports whether body starts concurrent work: a go statement,
// t.Parallel(), or an errgroup.Group.Go call. Merely using a mutex, atomic,
// or WaitGroup does not make a test concurrent.
func hasConcurrency(body *ast.BlockStmt, fn *ast.FuncDecl, file *ast.File) bool {
	found := false
	imports := map[string]bool{}
	for _, im := range file.Imports {
		path := strings.Trim(im.Path.Value, "\"")
		base := filepath.Base(path)
		name := base
		if im.Name != nil {
			name = im.Name.Name
		}
		if path == "golang.org/x/sync/errgroup" {
			imports[name] = true
		}
	}
	groups := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		sel, ok := vs.Type.(*ast.SelectorExpr)
		if ok && sel.Sel.Name == "Group" {
			if id, ok := sel.X.(*ast.Ident); ok && imports[id.Name] {
				for _, name := range vs.Names {
					groups[name.Name] = true
				}
			}
		}
		return true
	})
	// Only executable statements count. A function literal contributes when it
	// is the operand of go or an errgroup.Group.Go call.
	var inspectExecutable func(ast.Node)
	inspectExecutable = func(root ast.Node) {
		ast.Inspect(root, func(n ast.Node) bool {
			if found {
				return false
			}
			switch node := n.(type) {
			case *ast.IfStmt:
				if id, ok := node.Cond.(*ast.Ident); ok && id.Name == "false" {
					if node.Else != nil {
						inspectExecutable(node.Else)
					}
					return false
				}
			case *ast.FuncLit:
				return false // only an enclosing go/Group.Go invocation makes it concurrent
			case *ast.GoStmt:
				found = true
				return false
			case *ast.CallExpr:
				if sel, ok := node.Fun.(*ast.SelectorExpr); ok {
					if sel.Sel.Name == "Parallel" {
						if id, ok := sel.X.(*ast.Ident); ok && isTestParam(fn, id.Name) {
							found = true
							return false
						}
					}
					if sel.Sel.Name == "Go" {
						if id, ok := sel.X.(*ast.Ident); ok && groups[id.Name] {
							found = true
							return false
						}
					}
				}
			}
			return true
		})
	}
	inspectExecutable(body)
	return found
}

func isTestParam(fn *ast.FuncDecl, name string) bool {
	if fn.Type.Params == nil {
		return false
	}
	for _, f := range fn.Type.Params.List {
		for _, n := range f.Names {
			if n.Name == name {
				return true
			}
		}
	}
	return false
}

func goldenSkip(fset *token.FileSet, body *ast.BlockStmt, file *ast.File) (string, int) {
	if call, line := firstSkipCall(fset, body); call != "" {
		return call, line
	}
	helpers := map[string]*ast.FuncDecl{}
	for _, d := range file.Decls {
		if f, ok := d.(*ast.FuncDecl); ok && f.Recv == nil && f.Body != nil {
			helpers[f.Name.Name] = f
		}
	}
	seen := map[string]bool{}
	var visit func(*ast.BlockStmt) (string, int)
	visit = func(b *ast.BlockStmt) (string, int) {
		if call, line := firstSkipCall(fset, b); call != "" {
			return call, line
		}
		var result string
		var ln int
		ast.Inspect(b, func(n ast.Node) bool {
			if result != "" {
				return false
			}
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := c.Fun.(*ast.Ident); ok {
				if skipSelectors[id.Name] {
					result = id.Name
					ln = fset.Position(c.Pos()).Line
					return false
				}
				if h := helpers[id.Name]; h != nil && !seen[id.Name] {
					seen[id.Name] = true
					result, ln = visit(h.Body)
					return false
				}
			}
			return true
		})
		return result, ln
	}
	return visit(body)
}

// firstSkipCall returns the first testing skip call in body ("t.Skipf" and
// the line it sits on), or "" when the body never skips.
func firstSkipCall(fset *token.FileSet, body *ast.BlockStmt) (string, int) {
	var call string
	var line int
	ast.Inspect(body, func(n ast.Node) bool {
		if call != "" {
			return false
		}
		expr, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := expr.Fun.(type) {
		case *ast.SelectorExpr:
			if !skipSelectors[fun.Sel.Name] {
				return true
			}
			if id, ok := fun.X.(*ast.Ident); ok {
				call = id.Name + "." + fun.Sel.Name
			} else {
				call = fun.Sel.Name
			}
		case *ast.Ident:
			if !skipSelectors[fun.Name] {
				return true
			}
			call = fun.Name
		default:
			return true
		}
		line = fset.Position(expr.Pos()).Line
		return false
	})
	return call, line
}

func sortViolations(out []Violation) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Rule < out[j].Rule
	})
}

// CheckDir parses every *_test.go file directly inside dir and returns the
// combined violations, sorted for determinism.
func CheckDir(dir string) ([]Violation, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("testhygiene: read dir %s: %w", dir, err)
	}
	var out []Violation
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("testhygiene: read %s: %w", path, err)
		}
		found, err := CheckSource(entry.Name(), src)
		if err != nil {
			return nil, err
		}
		out = append(out, found...)
	}
	sortViolations(out)
	return out, nil
}

// CheckTree walks root, skipping dot directories, fixture/generated/
// vendored trees and nested modules (the same boundaries the repository's
// other filesystem scanners honor), and returns every hygiene violation in
// the hand-written *_test.go files it finds. File paths are
// slash-separated and relative to root so reports are reproducible.
func CheckTree(root string) ([]Violation, error) {
	var out []Violation
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			name := info.Name()
			if path != root && (strings.HasPrefix(name, ".") || skipDirNames[name]) {
				return filepath.SkipDir
			}
			if path != root {
				if _, statErr := os.Stat(filepath.Join(path, "go.mod")); statErr == nil {
					return filepath.SkipDir
				}
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			if rel != "." && isGeneratedPath(filepath.ToSlash(rel)) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found, err := CheckSource(filepath.ToSlash(rel), src)
		if err != nil {
			return err
		}
		out = append(out, found...)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sortViolations(out)
	return out, nil
}

// FormatReport renders violations one per line, ending with a newline when
// non-empty. An empty report renders as "".
func FormatReport(violations []Violation) string {
	if len(violations) == 0 {
		return ""
	}
	var b strings.Builder
	for _, v := range violations {
		b.WriteString(v.String())
		b.WriteByte('\n')
	}
	return b.String()
}
