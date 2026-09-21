// Package prohibitedframework enforces LIB-013: infrastructure mechanics may
// not capture HCM semantics, and Phase 1 correctness may not acquire an
// unapproved framework, broker, interpreter, or provider-SDK dependency.
package prohibitedframework

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"gopkg.in/yaml.v3"
)

const (
	DispositionProhibited         = "PROHIBITED"
	DispositionPhaseOneProhibited = "PHASE1_PROHIBITED"
	DispositionAdapterOnly        = "ADAPTER_ONLY"

	CodeSemanticFramework      = "PROHIBITED_SEMANTIC_FRAMEWORK"
	CodePhaseOneInfrastructure = "PHASE1_INFRASTRUCTURE_PROHIBITED"
	CodeProviderBoundary       = "PROVIDER_SDK_OUTSIDE_ADAPTER"
	CodeProviderTypeExposure   = "PROVIDER_TYPE_EXPOSURE"
	CodeRuntimeScript          = "PRODUCTION_RULE_RUNTIME"
	CodeInvalidPolicy          = "INVALID_POLICY"
)

var requiredCategories = map[string]string{
	"ORM":                   DispositionProhibited,
	"WORKFLOW_ENGINE":       DispositionProhibited,
	"CUSTOMER_RULE_RUNTIME": DispositionProhibited,
	"PHASE1_BROKER":         DispositionPhaseOneProhibited,
	"PROVIDER_SDK":          DispositionAdapterOnly,
}

var runtimeScriptExtensions = map[string]struct{}{
	".js": {}, ".lua": {}, ".mjs": {}, ".py": {}, ".star": {}, ".ts": {},
}

// Policy is the reviewed boundary contract. Rules identify module families,
// not substring matches, so a lookalike module cannot inherit a decision.
type Policy struct {
	Version       int      `yaml:"version"`
	Module        string   `yaml:"module"`
	PolicyDate    string   `yaml:"policy_date"`
	RuntimeRoots  []string `yaml:"runtime_roots"`
	SemanticRoots []string `yaml:"semantic_roots"`
	Rules         []Rule   `yaml:"rules"`
}

// Rule describes one prohibited or adapter-confined dependency family.
type Rule struct {
	ID             string   `yaml:"id"`
	Category       string   `yaml:"category"`
	Disposition    string   `yaml:"disposition"`
	ImportPrefixes []string `yaml:"import_prefixes"`
	AllowedRoots   []string `yaml:"allowed_roots,omitempty"`
}

// Package is the source information required to evaluate one package. Tests
// construct it directly; ScanRepository fills it from go list and Go ASTs.
type Package struct {
	ImportPath          string
	Imports             []string
	ExportedTypeImports []string
}

// Violation is a stable, machine-filterable policy diagnostic.
type Violation struct {
	Code      string
	RuleID    string
	Package   string
	Subject   string
	Qualifier string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: %s %s %s (%s)", v.Package, v.Code, v.Qualifier, v.Subject, v.RuleID)
}

// Load reads and validates the committed policy.
func Load(path string) (Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, fmt.Errorf("prohibitedframework: read policy: %w", err)
	}
	var policy Policy
	if err := yaml.Unmarshal(data, &policy); err != nil {
		return Policy{}, fmt.Errorf("prohibitedframework: parse policy: %w", err)
	}
	if violations := Validate(policy); len(violations) != 0 {
		return Policy{}, fmt.Errorf("prohibitedframework: %s", violations[0].String())
	}
	return policy, nil
}

// Validate rejects an incomplete or weakened policy before it can be used as
// evidence. In particular, semantic frameworks cannot acquire exceptions and
// provider adapters cannot be broadened to all of internal or cmd.
func Validate(policy Policy) []Violation {
	var out []Violation
	add := func(subject string) {
		out = append(out, Violation{Code: CodeInvalidPolicy, RuleID: "policy", Package: "definitions/architecture/prohibited-frameworks.yaml", Subject: subject, Qualifier: "field"})
	}
	if policy.Version != 1 {
		add("version")
	}
	if policy.Module == "" || policy.PolicyDate == "" || len(policy.RuntimeRoots) == 0 || len(policy.SemanticRoots) == 0 {
		add("required metadata")
	}

	seenID := make(map[string]bool)
	seenCategory := make(map[string]bool)
	for _, rule := range policy.Rules {
		if rule.ID == "" || seenID[rule.ID] {
			add("unique rule id")
		}
		seenID[rule.ID] = true
		requiredDisposition, required := requiredCategories[rule.Category]
		if !required || seenCategory[rule.Category] {
			add("category " + rule.Category)
		}
		seenCategory[rule.Category] = true
		if rule.Disposition != requiredDisposition || len(rule.ImportPrefixes) == 0 {
			add("rule " + rule.ID)
		}
		if rule.Disposition == DispositionProhibited && len(rule.AllowedRoots) != 0 {
			add("prohibited exception " + rule.ID)
		}
		if rule.Disposition == DispositionPhaseOneProhibited && len(rule.AllowedRoots) != 0 {
			add("Phase 1 infrastructure exception " + rule.ID)
		}
		for _, root := range rule.AllowedRoots {
			root = cleanSlash(root)
			if root == "" || root == "." || root == "internal" || root == "cmd" || withinAny(root, policy.SemanticRoots) {
				add("overbroad allowed root " + rule.ID)
			}
		}
	}
	for category := range requiredCategories {
		if !seenCategory[category] {
			add("missing category " + category)
		}
	}
	sortViolations(out)
	return out
}

// CheckPackage applies every matching dependency-family rule to one package.
func CheckPackage(policy Policy, pkg Package) []Violation {
	rel, owned := trimModule(policy.Module, pkg.ImportPath)
	if !owned {
		return nil
	}
	var out []Violation
	for _, imported := range pkg.Imports {
		for _, rule := range matchingRules(policy.Rules, imported) {
			if withinAny(rel, rule.AllowedRoots) {
				continue
			}
			code, qualifier := diagnostic(rule)
			out = append(out, Violation{Code: code, RuleID: rule.ID, Package: rel, Subject: imported, Qualifier: qualifier})
		}
	}
	if withinAny(rel, policy.SemanticRoots) {
		for _, imported := range pkg.ExportedTypeImports {
			for _, rule := range matchingRules(policy.Rules, imported) {
				if rule.Category != "PROVIDER_SDK" {
					continue
				}
				out = append(out, Violation{Code: CodeProviderTypeExposure, RuleID: rule.ID, Package: rel, Subject: imported, Qualifier: "provider type"})
			}
		}
	}
	sortViolations(out)
	return out
}

// CheckRuntimeFile rejects customer-rule scripting languages only from roots
// that ship as production Go runtime content. Developer tooling remains out of
// scope and may use the best tool for generation or verification.
func CheckRuntimeFile(policy Policy, path string) []Violation {
	rel := cleanSlash(path)
	if !withinAny(rel, policy.RuntimeRoots) {
		return nil
	}
	if _, prohibited := runtimeScriptExtensions[strings.ToLower(filepath.Ext(rel))]; !prohibited {
		return nil
	}
	return []Violation{{Code: CodeRuntimeScript, RuleID: "runtime-script", Package: rel, Subject: filepath.Ext(rel), Qualifier: "script"}}
}

// ScanRepository checks the real package graph, exported semantic API types,
// and production runtime roots. It performs no network access.
func ScanRepository(root string, policy Policy) ([]Violation, error) {
	if invalid := Validate(policy); len(invalid) != 0 {
		return invalid, nil
	}
	packages, err := repopath.ListPackages(root)
	if err != nil {
		return nil, err
	}
	var out []Violation
	for _, listed := range packages {
		exposed, err := exportedImports(listed.Dir)
		if err != nil {
			return nil, err
		}
		out = append(out, CheckPackage(policy, Package{ImportPath: listed.ImportPath, Imports: listed.Imports, ExportedTypeImports: exposed})...)
	}
	for _, runtimeRoot := range policy.RuntimeRoots {
		base := filepath.Join(root, filepath.FromSlash(runtimeRoot))
		if _, err := os.Stat(base); os.IsNotExist(err) {
			continue
		}
		if err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" || entry.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if isToolchainWasmBridge(path, rel) {
				return nil
			}
			out = append(out, CheckRuntimeFile(policy, rel)...)
			return nil
		}); err != nil {
			return nil, fmt.Errorf("prohibitedframework: scan %s: %w", runtimeRoot, err)
		}
	}
	sortViolations(out)
	return out, nil
}

// The Go WASM browser shim is generated by our frontend build, not a
// customer rule runtime. Admit only the exact toolchain bytes at its exact
// generated path; a different script at that path remains prohibited.
func isToolchainWasmBridge(path, rel string) bool {
	if filepath.ToSlash(rel) != "internal/humanwork/workspace/assets/wasm_exec.js" {
		return false
	}
	root, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return false
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, location := range []string{"lib", "misc"} {
		toolchain, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(root)), location, "wasm", "wasm_exec.js"))
		if err == nil && bytes.Equal(actual, toolchain) {
			return true
		}
	}
	return false
}

func exportedImports(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("prohibitedframework: read package %s: %w", dir, err)
	}
	used := make(map[string]struct{})
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, entry.Name()), nil, 0)
		if err != nil {
			return nil, fmt.Errorf("prohibitedframework: parse %s: %w", entry.Name(), err)
		}
		aliases := make(map[string]string)
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			name := filepath.Base(path)
			if spec.Name != nil {
				name = spec.Name.Name
			}
			if name != "_" && name != "." {
				aliases[name] = path
			}
		}
		for _, declaration := range file.Decls {
			if !exportedDeclaration(declaration) {
				continue
			}
			ast.Inspect(declaration, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				identifier, ok := selector.X.(*ast.Ident)
				if ok {
					if path, found := aliases[identifier.Name]; found {
						used[path] = struct{}{}
					}
				}
				return true
			})
		}
	}
	out := make([]string, 0, len(used))
	for path := range used {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}

func exportedDeclaration(declaration ast.Decl) bool {
	switch node := declaration.(type) {
	case *ast.FuncDecl:
		return node.Name.IsExported()
	case *ast.GenDecl:
		for _, spec := range node.Specs {
			switch value := spec.(type) {
			case *ast.TypeSpec:
				if value.Name.IsExported() {
					return true
				}
			case *ast.ValueSpec:
				for _, name := range value.Names {
					if name.IsExported() {
						return true
					}
				}
			}
		}
	}
	return false
}

func matchingRules(rules []Rule, importPath string) []Rule {
	var matches []Rule
	for _, rule := range rules {
		for _, prefix := range rule.ImportPrefixes {
			if moduleMatch(importPath, prefix) {
				matches = append(matches, rule)
				break
			}
		}
	}
	return matches
}

func diagnostic(rule Rule) (string, string) {
	switch rule.Category {
	case "ORM":
		return CodeSemanticFramework, "ORM"
	case "WORKFLOW_ENGINE":
		return CodeSemanticFramework, "workflow engine"
	case "CUSTOMER_RULE_RUNTIME":
		return CodeSemanticFramework, "customer rule runtime"
	case "PHASE1_BROKER":
		return CodePhaseOneInfrastructure, "broker"
	case "PROVIDER_SDK":
		return CodeProviderBoundary, "provider SDK"
	default:
		return CodeInvalidPolicy, "unknown category"
	}
}

func moduleMatch(importPath, prefix string) bool {
	return importPath == prefix || strings.HasPrefix(importPath, strings.TrimSuffix(prefix, "/")+"/")
}

func trimModule(module, importPath string) (string, bool) {
	if importPath == module {
		return ".", true
	}
	prefix := strings.TrimSuffix(module, "/") + "/"
	if !strings.HasPrefix(importPath, prefix) {
		return "", false
	}
	return strings.TrimPrefix(importPath, prefix), true
}

func withinAny(path string, roots []string) bool {
	path = cleanSlash(path)
	for _, root := range roots {
		root = cleanSlash(root)
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

func cleanSlash(path string) string {
	return strings.Trim(strings.ReplaceAll(filepath.Clean(path), "\\", "/"), "/")
}

func sortViolations(violations []Violation) {
	sort.Slice(violations, func(i, j int) bool {
		return violations[i].String() < violations[j].String()
	})
}
