package productui

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// jsValueOfCompatible mirrors the dynamic types syscall/js.ValueOf accepts.
// Anything else -- []string, map[string]string, a struct -- panics at the JS
// boundary, and a panic inside a GWC effect aborts the hydration commit.
func jsValueOfCompatible(value any) error {
	switch v := value.(type) {
	case nil, bool, string,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, uintptr,
		float32, float64:
		return nil
	case []any:
		for index, item := range v {
			if err := jsValueOfCompatible(item); err != nil {
				return fmt.Errorf("[%d]: %w", index, err)
			}
		}
		return nil
	case map[string]any:
		for key, item := range v {
			if err := jsValueOfCompatible(item); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
		return nil
	default:
		return fmt.Errorf("js.ValueOf panics on %T", value)
	}
}

// TestTodo_UXBLIND_083 pins the MutationObserver init dictionary the shell's
// popover controller hands to JavaScript on its first effect. Built as a
// []string it made js.ValueOf panic, the hydration commit never finished,
// the product router never mounted, and every page stayed on its skeleton.
func TestTodo_UXBLIND_083(t *testing.T) {
	options := uxblindQObserverOptions()
	if err := jsValueOfCompatible(options); err != nil {
		t.Fatalf("observer options cannot cross into JavaScript: %v", err)
	}
	filter, ok := options["attributeFilter"].([]any)
	if !ok {
		t.Fatalf("attributeFilter = %T, want []any", options["attributeFilter"])
	}
	var names []string
	for _, item := range filter {
		names = append(names, item.(string))
	}
	if got := strings.Join(names, ","); got != "aria-hidden,class,hidden,open,role" {
		t.Fatalf("attributeFilter = %q", got)
	}
	for _, key := range []string{"subtree", "childList", "attributes"} {
		if options[key] != true {
			t.Errorf("observer option %s = %v, want true", key, options[key])
		}
	}
}

// TestTodo_UXBLIND_083_Regression scans every browser-only source that feeds
// the workspace client for a literal js.ValueOf cannot convert: a typed slice
// or a map whose values are not `any`, passed to js.ValueOf or straight into a
// js.Value Call/New/Invoke/Set. Native tests never execute those files, so the
// class of bug that blanked every page is caught here at the source.
func TestTodo_UXBLIND_083_Regression(t *testing.T) {
	roots := []string{".", "../../../tools/uxqual/render/journey", "../../../tools/uxqual/cmd/journeywasm"}
	scanned := 0
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(root, name)
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(source), `"syscall/js"`) {
				continue
			}
			scanned++
			for _, finding := range jsBoundaryLiteralFindings(t, path, source) {
				t.Errorf("%s", finding)
			}
		}
	}
	if scanned < 10 {
		t.Fatalf("scanned %d browser sources; the scan roots are wrong", scanned)
	}
	bad := []byte("package p\nimport \"syscall/js\"\nfunc f(o js.Value) { o.Call(\"observe\", js.ValueOf(map[string]any{\"attributeFilter\": []string{\"open\"}})) }\n")
	if findings := jsBoundaryLiteralFindings(t, "fixture.go", bad); len(findings) != 1 {
		t.Fatalf("scanner missed the UXBLIND-083 shape: %v", findings)
	}
}

func jsBoundaryLiteralFindings(t *testing.T, path string, source []byte) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, source, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var findings []string
	var inspectArgument func(ast.Expr)
	inspectArgument = func(expr ast.Expr) {
		literal, ok := expr.(*ast.CompositeLit)
		if !ok {
			return
		}
		switch typ := literal.Type.(type) {
		case *ast.ArrayType:
			if !isAnyType(typ.Elt) {
				findings = append(findings, fmt.Sprintf("%s: typed slice literal crosses the JS boundary", fset.Position(literal.Pos())))
				return
			}
		case *ast.MapType:
			if !isAnyType(typ.Value) {
				findings = append(findings, fmt.Sprintf("%s: map literal with non-any values crosses the JS boundary", fset.Position(literal.Pos())))
				return
			}
		}
		for _, element := range literal.Elts {
			if pair, ok := element.(*ast.KeyValueExpr); ok {
				inspectArgument(pair.Value)
				continue
			}
			inspectArgument(element)
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch selector.Sel.Name {
		case "ValueOf":
			if pkg, ok := selector.X.(*ast.Ident); !ok || pkg.Name != "js" {
				return true
			}
		case "Call", "New", "Invoke", "Set":
		default:
			return true
		}
		for _, argument := range call.Args {
			inspectArgument(argument)
		}
		return true
	})
	return findings
}

func isAnyType(expr ast.Expr) bool {
	switch typ := expr.(type) {
	case *ast.Ident:
		return typ.Name == "any"
	case *ast.InterfaceType:
		return typ.Methods == nil || len(typ.Methods.List) == 0
	}
	return false
}

// TestTodo_UXBLIND_087 pins the browser-tab format: the page's own name and
// the company, never the product suite and never the Home greeting.
func TestTodo_UXBLIND_087(t *testing.T) {
	theme := DefaultCustomerTheme()
	home := testView(PageHome)
	home.Viewer.Name = "Walt Brennan"
	if heading := ResolvePageIdentity(home).Title; !strings.HasPrefix(heading, "Good ") {
		t.Fatalf("home heading lost its greeting: %q", heading)
	}
	for _, tc := range []struct {
		name  string
		title string
		theme CustomerTheme
		want  string
	}{
		{"home", ResolveDocumentPageTitle(home), theme, "Home · Ironridge Builders"},
		{"people", "People", theme, "People · Ironridge Builders"},
		{"branded", "Journeys", CustomerTheme{BrandName: "Ironridge", BrandMark: "IR"}, "Journeys · Ironridge"},
		{"no page title", "", theme, "Ironridge Builders"},
	} {
		if got := DocumentTitle(tc.title, tc.theme, "Ironridge Builders"); got != tc.want {
			t.Errorf("%s: DocumentTitle = %q, want %q", tc.name, got, tc.want)
		}
	}
	german := testView(PageHome)
	german.Locale = ResolveProductLocale("de-DE")
	german.Viewer.Name = "Walt Brennan"
	if got := DocumentTitle(ResolveDocumentPageTitle(german), theme, "Ironridge Builders"); got != "Start · Ironridge Builders" {
		t.Fatalf("localized home title = %q", got)
	}
	doc, err := Render(func() View {
		view := testView(PagePeople)
		view.Tenant = "Ironridge Builders"
		return view
	}())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "<title>People · Ironridge Builders</title>") {
		t.Fatalf("SSR title is not '<page> · <company>': %s", doc[strings.Index(doc, "<title>"):strings.Index(doc, "</title>")+8])
	}
}
