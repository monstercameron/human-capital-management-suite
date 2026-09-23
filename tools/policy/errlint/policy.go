package errlint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Exceptions names blanked calls the policy tolerates, each with the reason
// it is safe. The key is a repository-relative file path plus the enclosing
// function name and line, joined by colons; the value explains why the
// discarded result cannot lose evidence or misreport state.
var Exceptions = map[string]string{}

// Finding is one blanked call result outside a deferred rollback. File is
// relative to the scan root and Line is one-based.
type Finding struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Function string `json:"function"`
	Text     string `json:"text"`
}

// Report is the deterministic result of scanning the named files.
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

// EvaluateFiles scans exactly the named Go files (repository-relative or
// absolute) and reports every blanked call result outside a deferred
// rollback. Callers choose the scope: REV-103-06 gates the five files its
// RED names.
func EvaluateFiles(files []string) (Report, error) {
	var findings []Finding
	for _, file := range files {
		found, err := scanFile(file)
		if err != nil {
			return Report{}, err
		}
		findings = append(findings, found...)
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return Report{Findings: findings}, nil
}

// scanFile parses one Go file and reports its blanked call results.
func scanFile(path string) ([]Finding, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("errlint: parse %s: %w", path, err)
	}
	display := filepath.ToSlash(path)
	var findings []Finding
	var visit func(n ast.Node, funcName string, deferred bool)
	visit = func(n ast.Node, funcName string, deferred bool) {
		switch t := n.(type) {
		case *ast.FuncDecl:
			name := t.Name.Name
			if t.Recv != nil {
				name = "method:" + name
			}
			if t.Body != nil {
				for _, stmt := range t.Body.List {
					visit(stmt, name, false)
				}
			}
		case *ast.FuncLit:
			// A literal is deferred only when this literal itself is the
			// deferred call; nested literals and go statements stay armed.
			for _, stmt := range t.Body.List {
				visit(stmt, funcName, false)
			}
		case *ast.DeferStmt:
			if lit, ok := t.Call.Fun.(*ast.FuncLit); ok {
				for _, stmt := range lit.Body.List {
					visit(stmt, funcName, true)
				}
			}
		case *ast.GoStmt:
			// A goroutine boundary is not a rollback: keep the caller's
			// deferred state, never inherit it.
			ast.Inspect(t.Call, func(m ast.Node) bool {
				if lit, ok := m.(*ast.FuncLit); ok {
					for _, stmt := range lit.Body.List {
						visit(stmt, funcName, false)
					}
					return false
				}
				return true
			})
		case *ast.AssignStmt:
			if isBlankedCall(t) && !deferred {
				findings = append(findings, Finding{
					File:     display,
					Line:     fset.Position(t.Pos()).Line,
					Function: funcName,
					Text:     strings.TrimSpace(oneLine(t)),
				})
			}
			for _, expr := range t.Rhs {
				visit(expr, funcName, deferred)
			}
			for _, expr := range t.Lhs {
				visit(expr, funcName, deferred)
			}
		default:
			for _, child := range children(n) {
				visit(child, funcName, deferred)
			}
		}
	}
	for _, decl := range f.Decls {
		visit(decl, "<package>", false)
	}
	return findings, nil
}

// isBlankedCall reports whether the statement discards every result of a
// call: `_ = f()` or `_, _ = f()`. A fully blanked call always discards a
// value the callee returned; without type information the lint treats
// every such value as guilty until an exception proves it safe. A mixed
// capture such as `if _, err := tx.Exec(...); err != nil` is not a blanked
// call: it discards a row count while checking the error, which is exactly
// what this policy asks for. Calls to a method named Rollback are likewise
// excluded: a rollback runs only after the outcome it cleans up was
// already decided (the Commit whose error the caller did check, or a setup
// failure whose error the caller returns), so its own error can neither
// lose evidence nor misreport state.
func isBlankedCall(stmt *ast.AssignStmt) bool {
	if stmt.Tok.String() != "=" && stmt.Tok.String() != ":=" {
		return false
	}
	for _, lhs := range stmt.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok || id.Name != "_" {
			return false
		}
	}
	for _, rhs := range stmt.Rhs {
		call, ok := rhs.(*ast.CallExpr)
		if !ok {
			continue
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Rollback" {
			continue
		}
		return true
	}
	return false
}

// oneLine renders the statement without newlines for the report.
func oneLine(stmt *ast.AssignStmt) string {
	var sb strings.Builder
	for i, lhs := range stmt.Lhs {
		if i > 0 {
			sb.WriteString(", ")
		}
		if id, ok := lhs.(*ast.Ident); ok {
			sb.WriteString(id.Name)
		}
	}
	sb.WriteString(" = ...")
	return sb.String()
}

// children returns the direct child nodes for generic traversal.
func children(n ast.Node) []ast.Node {
	var out []ast.Node
	ast.Inspect(n, func(m ast.Node) bool {
		if m == nil || m == n {
			return true
		}
		out = append(out, m)
		return false
	})
	return out
}

// FilterExceptions drops findings named in Exceptions, so a documented-safe
// site does not fail the gate twice.
func (r Report) FilterExceptions() Report {
	var kept []Finding
	for _, f := range r.Findings {
		key := f.File + ":" + f.Function + ":" + strconv.Itoa(f.Line)
		if _, ok := Exceptions[key]; ok {
			continue
		}
		kept = append(kept, f)
	}
	return Report{Findings: kept}
}

// Exists reports whether path names an existing file, for the CLI's usage
// errors.
func Exists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
