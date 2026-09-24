package archrules

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

// CeremonyRules is the reviewed ARCH-GO-027 policy. Naming a package with a
// listed suffix is not itself a violation; parallel copies of one semantic
// layer are.
type CeremonyRules struct {
	PackageSuffixes []string            `yaml:"package_suffixes"`
	Exceptions      []CeremonyException `yaml:"exceptions"`
}

// CeremonyException is a narrow, reviewed exception for a real boundary that
// currently resembles ceremony. All evidence fields are required before it
// can suppress a finding.
type CeremonyException struct {
	Kind      string `yaml:"kind"`
	Package   string `yaml:"package"`
	Owner     string `yaml:"owner"`
	Rationale string `yaml:"rationale"`
	Evidence  string `yaml:"evidence"`
	Expiry    string `yaml:"expiry"`
}

// SourcePackage is the syntax-level package input used by the checker. Tests
// can construct one without writing temporary source files.
type SourcePackage struct {
	ImportPath string
	Dir        string
	Name       string
	Source     string
	Files      []string
	Generated  bool
}

// CeremonyViolation is one stable ARCH-GO-027 finding.
type CeremonyViolation struct {
	Kind    string
	Package string
	Symbol  string
	Detail  string
	Waived  bool
}

func (v CeremonyViolation) String() string {
	if v.Symbol != "" {
		return fmt.Sprintf("%s: %s (%s: %s)", v.Package, v.Detail, v.Kind, v.Symbol)
	}
	return fmt.Sprintf("%s: %s (%s)", v.Package, v.Detail, v.Kind)
}

// CheckCeremonySources evaluates raw ARCH-GO-027 rules. Reviewed exceptions
// are deliberately not applied here so the primary test exercises detection.
func CheckCeremonySources(packages []SourcePackage, rules CeremonyRules) []CeremonyViolation {
	var out []CeremonyViolation
	for _, pkg := range packages {
		out = append(out, checkSourcePackage(pkg)...)
	}
	out = append(out, parallelSuffixViolations(packages, rules.PackageSuffixes)...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Package != out[j].Package {
			return out[i].Package < out[j].Package
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Symbol < out[j].Symbol
	})
	return out
}

// CheckCeremonyWithPolicy applies only complete, unexpired reviewed
// exceptions to raw findings.
func CheckCeremonyWithPolicy(packages []SourcePackage, rules CeremonyRules, asOf time.Time) []CeremonyViolation {
	return ApplyCeremonyPolicy(CheckCeremonySources(packages, rules), rules, asOf)
}

// ApplyCeremonyPolicy applies reviewed exceptions to an already scanned
// finding set. This keeps filesystem scanning and policy application separate
// for callers that cache an import/AST scan.
func ApplyCeremonyPolicy(raw []CeremonyViolation, rules CeremonyRules, asOf time.Time) []CeremonyViolation {
	var out []CeremonyViolation
	for _, v := range raw {
		waived := false
		for _, e := range rules.Exceptions {
			if exceptionCovers(e, v, asOf) {
				waived = true
				break
			}
		}
		if !waived {
			out = append(out, v)
		}
	}
	return out
}

// ScanCeremony scans all root-module Go packages, excluding test files, and
// applies no exception.
func ScanCeremony(root string, rules CeremonyRules) ([]CeremonyViolation, error) {
	packages, err := repopath.ListPackages(root)
	if err != nil {
		return nil, err
	}
	module := repopath.ModulePath(root)
	var sources []SourcePackage
	for _, pkg := range packages {
		if !strings.HasPrefix(pkg.ImportPath, module+"/") {
			continue
		}
		entries, err := os.ReadDir(pkg.Dir)
		if err != nil {
			return nil, fmt.Errorf("archrules: reading %s: %w", pkg.Dir, err)
		}
		var files []string
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(pkg.Dir, entry.Name()))
			if err != nil {
				return nil, fmt.Errorf("archrules: reading %s: %w", entry.Name(), err)
			}
			files = append(files, string(data))
		}
		if len(files) > 0 {
			rel, _ := TrimModule(module, pkg.ImportPath)
			sources = append(sources, SourcePackage{
				ImportPath: pkg.ImportPath, Dir: pkg.Dir, Files: files,
				Generated: UnderRoot(rel, "gen"),
			})
		}
	}
	return CheckCeremonySources(sources, rules), nil
}

// ScanCeremonyWithPolicy scans the real module and applies only complete,
// unexpired reviewed exceptions.
func ScanCeremonyWithPolicy(root string, rules CeremonyRules, asOf time.Time) ([]CeremonyViolation, error) {
	raw, err := ScanCeremony(root, rules)
	if err != nil {
		return nil, err
	}
	return ApplyCeremonyPolicy(raw, rules, asOf), nil
}

func checkSourcePackage(pkg SourcePackage) []CeremonyViolation {
	if pkg.Generated {
		return nil
	}
	var files []*ast.File
	if len(pkg.Files) > 0 {
		for i, source := range pkg.Files {
			file, err := parser.ParseFile(token.NewFileSet(), fmt.Sprintf("file%d.go", i), source, 0)
			if err != nil {
				return []CeremonyViolation{{Kind: "syntax-error", Package: pkg.ImportPath, Detail: err.Error()}}
			}
			files = append(files, file)
		}
	} else {
		file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", pkg.Source, 0)
		if err != nil {
			return []CeremonyViolation{{Kind: "syntax-error", Package: pkg.ImportPath, Detail: err.Error()}}
		}
		files = []*ast.File{file}
	}
	return checkParsedPackage(pkg, files)
}

func checkParsedPackage(pkg SourcePackage, files []*ast.File) []CeremonyViolation {
	name := pkg.ImportPath
	if name == "" {
		name = pkg.Name
	}
	var out []CeremonyViolation
	for _, file := range files {
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				iface, ok := ts.Type.(*ast.InterfaceType)
				if !ok || len(iface.Methods.List) != 1 {
					continue
				}
				methodName := interfaceMethodName(iface)
				if methodName == "" || interfaceReferences(files, ts.Name, iface) != 0 {
					continue
				}
				implementers := methodImplementers(files, methodName, iface.Methods.List[0].Type.(*ast.FuncType))
				if len(implementers) != 1 {
					continue
				}
				out = append(out, CeremonyViolation{
					Kind: "one-method-interface-no-consumer", Package: name, Symbol: ts.Name.Name,
					Detail: fmt.Sprintf("one-method interface has one in-package implementation (%s) and no consumer reference", implementers[0]),
				})
			}
		}
	}
	if forwardingOnly(files) {
		out = append(out, CeremonyViolation{Kind: "forwarding-only-layer", Package: name, Detail: "package contains only forwarding declarations and adds no semantic layer"})
	}
	return out
}

func interfaceMethodName(iface *ast.InterfaceType) string {
	field := iface.Methods.List[0]
	if len(field.Names) != 1 {
		return ""
	}
	return field.Names[0].Name
}

func interfaceReferences(files []*ast.File, declared *ast.Ident, iface *ast.InterfaceType) int {
	refs := 0
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if !ok || ident.Name != declared.Name || ident == declared {
				return true
			}
			refs++
			return true
		})
	}
	return refs
}

func methodImplementers(files []*ast.File, method string, signature *ast.FuncType) []string {
	seen := map[string]bool{}
	wantSignature := signatureKey(signature)
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != method || len(fn.Recv.List) == 0 || signatureKey(fn.Type) != wantSignature {
				continue
			}
			if name := receiverName(fn.Recv.List[0].Type); name != "" {
				seen[name] = true
			}
		}
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func formatNode(node ast.Node) string {
	var buf bytes.Buffer
	if err := format.Node(&buf, token.NewFileSet(), node); err != nil {
		return ""
	}
	return buf.String()
}

func signatureKey(signature *ast.FuncType) string {
	fieldKeys := func(fields *ast.FieldList) []string {
		if fields == nil {
			return nil
		}
		var keys []string
		for _, field := range fields.List {
			count := len(field.Names)
			if count == 0 {
				count = 1
			}
			for i := 0; i < count; i++ {
				keys = append(keys, formatNode(field.Type))
			}
		}
		return keys
	}
	return strings.Join(fieldKeys(signature.Params), ",") + "->" + strings.Join(fieldKeys(signature.Results), ",")
}

func receiverName(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

func forwardingOnly(files []*ast.File) bool {
	hasDeclaration := false
	for _, file := range files {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Body == nil {
					continue
				}
				hasDeclaration = true
				if len(d.Body.List) != 1 {
					return false
				}
				switch stmt := d.Body.List[0].(type) {
				case *ast.ReturnStmt:
					if len(stmt.Results) != 1 || !isForwardCall(stmt.Results[0]) {
						return false
					}
				case *ast.ExprStmt:
					if !isForwardCall(stmt.X) {
						return false
					}
				default:
					return false
				}
			case *ast.GenDecl:
				if d.Tok == token.IMPORT {
					continue
				}
				if d.Tok == token.VAR || d.Tok == token.CONST {
					return false
				}
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok || !ts.Assign.IsValid() {
						return false
					}
					hasDeclaration = true
				}
			}
		}
	}
	return hasDeclaration
}

func isForwardCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	_, ok = call.Fun.(*ast.SelectorExpr)
	return ok
}

func parallelSuffixViolations(packages []SourcePackage, suffixes []string) []CeremonyViolation {
	groups := map[string][]string{}
	for _, pkg := range packages {
		base := filepath.Base(pkg.ImportPath)
		for _, suffix := range suffixes {
			suffix = strings.TrimPrefix(suffix, "*")
			if suffix == "" || !strings.HasSuffix(base, suffix) || base == suffix {
				continue
			}
			stem := strings.TrimSuffix(base, suffix)
			key := filepath.Join(filepath.Dir(pkg.ImportPath), stem)
			groups[key] = append(groups[key], pkg.ImportPath)
		}
	}
	var out []CeremonyViolation
	for group, members := range groups {
		unique := map[string]bool{}
		for _, member := range members {
			unique[member] = true
		}
		if len(unique) < 2 {
			continue
		}
		paths := make([]string, 0, len(unique))
		for member := range unique {
			paths = append(paths, member)
		}
		sort.Strings(paths)
		out = append(out, CeremonyViolation{Kind: "parallel-ceremonial-packages", Package: group, Detail: "parallel package suffix family: " + strings.Join(paths, ", ")})
	}
	return out
}

func exceptionCovers(e CeremonyException, v CeremonyViolation, asOf time.Time) bool {
	if e.Kind == "" || e.Package == "" || e.Owner == "" || e.Rationale == "" || e.Evidence == "" {
		return false
	}
	if e.Kind != v.Kind || (v.Package != e.Package && !strings.HasPrefix(v.Package, strings.TrimSuffix(e.Package, "/")+"/")) {
		return false
	}
	expires, err := time.Parse("2006-01-02", e.Expiry)
	return err == nil && !expires.Before(asOf.UTC().Truncate(24*time.Hour))
}
