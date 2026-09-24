// Package egressgateway enforces the centralized outbound HTTP boundary for
// connectivity packages.
package egressgateway

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

const modulePrefix = "github.com/monstercameron/human-capital-management-suite/"

// Violation identifies a connectivity package that constructs a standard
// HTTP client directly, bypassing the enforcing gateway adapter.
type Violation struct {
	Package        string
	File           string
	ImportsGateway bool
}

// ScanRoot checks production Go sources below internal/connectivity.
func ScanRoot(root string) ([]Violation, error) {
	base := filepath.Join(root, "internal", "connectivity")
	packages := map[string][]string{}
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		dir := filepath.Dir(rel)
		packages[dir] = append(packages[dir], path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	var out []Violation
	for dir, paths := range packages {
		if dir == filepath.Join("internal", "connectivity", "egress") {
			continue // the gateway implementation owns its underlying socket client.
		}
		usesClient := false
		importsGateway := false
		firstClientFile := ""
		for _, path := range paths {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil, err
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly|parser.ParseComments)
			if err != nil {
				return nil, fmt.Errorf("parse %s: %w", path, err)
			}
			for _, spec := range file.Imports {
				imp, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return nil, fmt.Errorf("parse import in %s: %w", path, err)
				}
				if imp == modulePrefix+"internal/connectivity/egress" {
					importsGateway = true
				}
			}
			// Parse the AST for client construction. ImportsOnly omits bodies,
			// so parse the file again only after collecting package imports.
			full, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return nil, fmt.Errorf("parse %s: %w", path, err)
			}
			ast.Inspect(full, func(node ast.Node) bool {
				if fn, ok := node.(*ast.FuncDecl); ok && filepath.ToSlash(rel) == "internal/connectivity/transport/security.go" && fn.Name.Name == "NewSafeHTTPClient" {
					return false // sole approved constructor; its resolver and redirect checks are tested.
				}
				if bypassesHTTPBoundary(node) {
					usesClient = true
					if firstClientFile == "" {
						firstClientFile = path
					}
				}
				return true
			})
		}
		if ForbiddenConstruction(usesClient) {
			out = append(out, Violation{
				Package: strings.TrimPrefix(filepath.ToSlash(dir), "internal/connectivity/"),
				File:    filepath.ToSlash(firstClientFile), ImportsGateway: importsGateway,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Package < out[j].Package })
	return out, nil
}

func bypassesHTTPBoundary(node ast.Node) bool {
	if selector, ok := node.(*ast.SelectorExpr); ok && selector.Sel.Name == "DefaultClient" {
		if ident, ok := selector.X.(*ast.Ident); ok && ident.Name == "http" {
			return true
		}
	}
	var typ ast.Expr
	switch value := node.(type) {
	case *ast.CompositeLit:
		typ = value.Type
	case *ast.CallExpr:
		if ident, ok := value.Fun.(*ast.Ident); ok && ident.Name == "new" && len(value.Args) == 1 {
			typ = value.Args[0]
		}
	}
	selector, ok := typ.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Client" {
		return false
	}
	ident, ok := selector.X.(*ast.Ident)
	return ok && ident.Name == "http"
}

// RequiresGateway reports whether a package's client construction needs the
// enforcing egress dependency.
func RequiresGateway(usesClient, importsGateway bool) bool {
	return usesClient && !importsGateway
}

// ForbiddenConstruction is the production-source rule: callers receive a
// Gateway-backed Doer and do not build a parallel http.Client or use the
// unguarded http.DefaultClient.
func ForbiddenConstruction(usesClient bool) bool { return usesClient }

// AnalyzeSource is the policy's focused source primitive, exposed for the
// synthetic bypass tests.
func AnalyzeSource(filename, source string) (bool, bool, error) {
	file, err := parser.ParseFile(token.NewFileSet(), filename, source, 0)
	if err != nil {
		return false, false, err
	}
	hasGateway, hasClient := false, false
	for _, spec := range file.Imports {
		imp, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return false, false, err
		}
		if imp == modulePrefix+"internal/connectivity/egress" {
			hasGateway = true
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		if bypassesHTTPBoundary(node) {
			hasClient = true
		}
		return true
	})
	return hasClient, hasGateway, nil
}
