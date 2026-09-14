package journey

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestTodo_UIPOLISH_010_ProductionCallSitesUseRegistry guards the actual
// tone/severity production adapters, not only the registry in isolation.
func TestTodo_UIPOLISH_010_ProductionCallSitesUseRegistry(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(filepath.Dir(filename), "icons.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"iconForTone", "iconForSeverity"} {
		found := false
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Name.Name != name {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.Ident)
				if ok && selector.Name == "RenderIcon" {
					found = true
				}
				return true
			})
		}
		if !found {
			t.Errorf("%s bypasses the governed RenderIcon registry", name)
		}
	}
}

// Every production renderer requests a semantic ID. Only the closed registry
// and icon definitions may construct glyphs by their private drawing helpers.
func TestTodo_UIPOLISH_010_NoDirectProductionGlyphs(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	private := map[string]bool{
		"BrandMark": true, "iconCheck": true, "iconArrowRight": true, "iconArrowLeft": true,
		"iconInfo": true, "iconSuccess": true, "iconWarning": true,
		"iconDanger": true, "iconLedger": true, "iconEmpty": true,
		"iconClock": true, "iconPerson": true, "iconSpark": true,
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "icons.go" || name == "icon_registry.go" {
			continue
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if ok && private[ident.Name] {
				t.Errorf("%s calls %s directly instead of RenderIcon", name, ident.Name)
			}
			return true
		})
	}
}
