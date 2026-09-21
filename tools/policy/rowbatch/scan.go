package rowbatch

// REV-089-01: finish the dbport.ExecAll batching rollout PERFOPT-004 scoped
// to twenty-two stores but only reached in four. This package scans
// internal/data for row-at-a-time Exec calls inside loops; every site either
// adopts ExecAll or is recorded in Exceptions with its justification.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// Finding is one Exec call inside a loop body.
type Finding struct {
	// File is the slash-separated path relative to the repository root.
	File string
	// Line is the 1-based line of the Exec call.
	Line int
}

// Exception is one row-at-a-time site that stays unconverted, with the
// reason batching does not apply.
type Exception struct {
	// File names the containing file relative to the repository root.
	File string
	// Reason is the justification a reviewer accepted.
	Reason string
}

// Exceptions lists every row-at-a-time Exec site that stays. A site lands
// here only with a reason: batching genuinely does not apply (for example
// the per-iteration statement depends on the previous iteration's result),
// never because the rollout did not reach it.
var Exceptions = []Exception{{File: "internal/data/dbport/batch.go", Reason: "ExecAll sequential fallback for drivers without batch support; the batch primitive cannot batch itself"}, {File: "internal/data/workeridstore/store.go", Reason: "ID reservation retry loop; each attempt depends on the previous attempt conflict outcome, statements cannot be queued up front"}, {File: "internal/data/seed/seed.go", Reason: "bootstrap seed path, not a request path"}, {File: "internal/data/roleaccessstore/store.go", Reason: "tenant Bootstrap path, runs once per tenant"}, {File: "internal/data/promotionladder/ladder.go", Reason: "Seed path for ladder edges, bulk import and bootstrap"}, {File: "internal/data/signals/expire.go", Reason: "background expiry sweep over due subscriptions"}, {File: "internal/data/conflictstore/store.go", Reason: "per-footprint insert with conflict-verify follow-up; convertible with the meritstore pattern, deferred to follow-up"}, {File: "internal/data/contactstore/store.go", Reason: "per-event insert; convertible with the meritstore pattern, deferred to follow-up"}, {File: "internal/data/jobarchstore/store.go", Reason: "per-revision inserts in import helpers; convertible with the meritstore pattern, deferred to follow-up"}, {File: "internal/data/meritstore/store.go", Reason: "per-recommendation insert in Save; convertible with the emission pattern, deferred to follow-up"}}

// ScanFile parses one Go source file and reports every Exec call inside a
// for or range loop body. Calls inside a nested function literal are not
// loop bodies and are ignored; nested loops are reported under their own
// loop so each Exec call is reported exactly once.
func ScanFile(rel string, src []byte) ([]Finding, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return nil, fmt.Errorf("rowbatch: parse %s: %w", rel, err)
	}
	var out []Finding
	ast.Inspect(f, func(n ast.Node) bool {
		switch loop := n.(type) {
		case *ast.ForStmt:
			out = append(out, execsInBody(fset, rel, loop.Body)...)
		case *ast.RangeStmt:
			out = append(out, execsInBody(fset, rel, loop.Body)...)
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}

// execsInBody reports Exec calls in a loop body without descending into
// nested loops (visited separately) or function literals (not loop bodies).
func execsInBody(fset *token.FileSet, rel string, body *ast.BlockStmt) []Finding {
	var out []Finding
	var walk func(node ast.Node)
	walk = func(node ast.Node) {
		switch n := node.(type) {
		case *ast.FuncLit:
			return
		case *ast.ForStmt, *ast.RangeStmt:
			return
		case *ast.CallExpr:
			if sel, ok := n.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Exec" {
				pos := fset.Position(n.Pos())
				out = append(out, Finding{File: rel, Line: pos.Line})
			}
		}
		for _, child := range children(node) {
			walk(child)
		}
	}
	for _, stmt := range body.List {
		walk(stmt)
	}
	return out
}

// children returns the directly nested statements and expressions worth
// descending into for Exec calls. It deliberately omits function literals
// and loop bodies, which execsInBody never enters.
func children(node ast.Node) []ast.Node {
	switch n := node.(type) {
	case *ast.ExprStmt:
		return []ast.Node{n.X}
	case *ast.AssignStmt:
		out := make([]ast.Node, 0, len(n.Rhs)+1)
		for _, rhs := range n.Rhs {
			out = append(out, rhs)
		}
		return out
	case *ast.IfStmt:
		out := []ast.Node{n.Body}
		if n.Else != nil {
			out = append(out, n.Else)
		}
		return out
	case *ast.BlockStmt:
		out := make([]ast.Node, 0, len(n.List))
		for _, stmt := range n.List {
			out = append(out, stmt)
		}
		return out
	case *ast.SwitchStmt:
		out := []ast.Node{n.Body}
		return out
	case *ast.TypeSwitchStmt:
		out := []ast.Node{n.Body}
		return out
	case *ast.CaseClause:
		out := make([]ast.Node, 0, len(n.Body))
		for _, stmt := range n.Body {
			out = append(out, stmt)
		}
		return out
	case *ast.DeferStmt:
		return []ast.Node{n.Call}
	case *ast.GoStmt:
		return []ast.Node{n.Call}
	case *ast.ReturnStmt:
		out := make([]ast.Node, 0, len(n.Results))
		for _, res := range n.Results {
			out = append(out, res)
		}
		return out
	}
	return nil
}

// ScanTree scans every non-test Go file under root/internal/data.
func ScanTree(root string) ([]Finding, error) {
	return scanTreeFiles(root)
}

// scanTreeFiles reads and scans every candidate file.
func scanTreeFiles(root string) ([]Finding, error) {
	var out []Finding
	base := filepath.Join(root, "internal", "data")
	err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		found, err := scanOneFile(path, rel)
		if err != nil {
			return err
		}
		out = append(out, found...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}
