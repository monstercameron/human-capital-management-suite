package observetest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Uninstrumented walks the production Go files under root (skipping testdata
// and any directory skip reports) and returns "rel/path.go Recv.Func" for every
// exported function taking a context.Context that opens no observe.Begin or
// observe.Start operation, sorted.
func Uninstrumented(root string, skip func(rel string) bool) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if d.Name() == "testdata" || (rel != "." && skip != nil && skip(rel)) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !fn.Name.IsExported() || !takesContext(fn) {
				continue
			}
			body := string(src[fset.Position(fn.Body.Pos()).Offset:fset.Position(fn.Body.End()).Offset])
			if strings.Contains(body, "observe.Begin(") || strings.Contains(body, "observe.Start(") {
				continue
			}
			name := fn.Name.Name
			if fn.Recv != nil && len(fn.Recv.List) > 0 {
				recv := string(src[fset.Position(fn.Recv.List[0].Type.Pos()).Offset:fset.Position(fn.Recv.List[0].Type.End()).Offset])
				name = strings.TrimPrefix(recv, "*") + "." + name
			}
			out = append(out, rel+" "+name)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func takesContext(fn *ast.FuncDecl) bool {
	for _, p := range fn.Type.Params.List {
		if sel, ok := p.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "Context" {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "context" {
				return true
			}
		}
	}
	return false
}

// CheckExemptions compares found against exempt: it returns the functions
// missing instrumentation without an exemption, and exemptions that no longer
// match an uninstrumented function.
func CheckExemptions(found []string, exempt map[string]string) (missing, stale []string) {
	seen := map[string]bool{}
	for _, key := range found {
		seen[key] = true
		if _, ok := exempt[key]; !ok {
			missing = append(missing, key)
		}
	}
	for key := range exempt {
		if !seen[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	return missing, stale
}
