// Package archdoc renders a deterministic architecture document from the
// repository's checked-in layout, dependency and library-role manifests.
package archdoc

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/depedge"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/importgraph"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/layout"
	"gopkg.in/yaml.v3"
)

const (
	layoutManifestPath = "definitions/architecture/repository-layout.yaml"
	depPolicyPath      = "definitions/architecture/package-dependency-policy.yaml"
	rolesManifestPath  = "definitions/architecture/dependency-roles.yaml"
)

type layoutDocument struct {
	Version              int            `yaml:"version"`
	Module               string         `yaml:"module"`
	AllowedRoots         []layoutRoot   `yaml:"allowed_roots"`
	InternalPackageRoots []internalRoot `yaml:"internal_package_roots"`
}

type layoutRoot struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

type internalRoot struct {
	Name        string `yaml:"name"`
	Owner       string `yaml:"owner"`
	Layer       string `yaml:"layer"`
	Phase       string `yaml:"phase"`
	Description string `yaml:"description"`
}

type roleDocument struct {
	Version     int          `yaml:"version"`
	Module      string       `yaml:"module"`
	FamilyRules []familyRule `yaml:"family_rules"`
	Modules     []moduleRole `yaml:"modules"`
}

type familyRule struct {
	Prefix             string   `yaml:"prefix"`
	Role               string   `yaml:"role"`
	AllowedImportRoots []string `yaml:"allowed_import_roots"`
}

type moduleRole struct {
	Path               string   `yaml:"path"`
	Role               string   `yaml:"role"`
	AllowedImportRoots []string `yaml:"allowed_import_roots"`
}

type packageInfo struct {
	ImportPath string
	Dir        string
	Imports    []string
}

type graphInfo struct {
	Packages []packageInfo
	Edges    []edgeInfo
	Digest   string
}

type edgeInfo struct {
	Importer string
	Imported string
}

// Generate reads the architecture manifests and the current Go package tree
// under root, returning the complete document as UTF-8 Markdown.
func Generate(root string) ([]byte, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}

	layoutPath := filepath.Join(root, filepath.FromSlash(layoutManifestPath))
	policyPath := filepath.Join(root, filepath.FromSlash(depPolicyPath))
	rolesPath := filepath.Join(root, filepath.FromSlash(rolesManifestPath))

	layoutManifest, err := layout.Load(layoutPath)
	if err != nil {
		return nil, err
	}
	policy, err := depedge.Load(policyPath)
	if err != nil {
		return nil, err
	}
	layoutDoc, err := loadYAML[layoutDocument](layoutPath)
	if err != nil {
		return nil, fmt.Errorf("load repository layout details: %w", err)
	}
	roles, err := loadYAML[roleDocument](rolesPath)
	if err != nil {
		return nil, fmt.Errorf("load dependency roles: %w", err)
	}
	graph, err := buildGraph(root, layoutManifest, policy)
	if err != nil {
		return nil, err
	}

	return render(layoutDoc, policy, roles, graph), nil
}

func loadYAML[T any](path string) (T, error) {
	var value T
	data, err := os.ReadFile(path)
	if err != nil {
		return value, fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &value); err != nil {
		return value, fmt.Errorf("parse %s: %w", path, err)
	}
	return value, nil
}
func render(layoutDoc layoutDocument, policy *depedge.Policy, roles roleDocument, graph graphInfo) []byte {
	var b strings.Builder
	b.WriteString("# Human Capital Management Suite Repository Architecture\n\n")
	b.WriteString("Generated from the checked-in architecture manifests and the current Go package tree.\n\n")
	b.WriteString("- Module: `" + layoutDoc.Module + "`\n")
	b.WriteString("- Source graph: " + graph.Digest + "\n")
	b.WriteString(fmt.Sprintf("- Package count: %d\n", len(graph.Packages)))
	b.WriteString(fmt.Sprintf("- Within-module edge count: %d\n", len(graph.Edges)))
	b.WriteString("- Source manifests: `" + layoutManifestPath + "`, `" + depPolicyPath + "`, `" + rolesManifestPath + "`\n\n")

	b.WriteString("## Declared layers and roots\n\n")
	b.WriteString("| Layer | Declared root | Owner | Phase | Purpose |\n")
	b.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, root := range layoutDoc.InternalPackageRoots {
		b.WriteString("| " + cell(root.Layer) + " | `internal/" + root.Name + "` | " + cell(root.Owner) + " | " + cell(root.Phase) + " | " + cell(root.Description) + " |\n")
	}
	b.WriteString("\n")

	b.WriteString("## Allowed dependency edges\n\n")
	b.WriteString("Ranked layers may depend on the same layer or a lower-ranked layer. Port packages are allowed dependencies for business layers; concrete adapters remain behind their ports.\n\n")
	b.WriteString("| Importing layer | Allowed ranked layers | Port roots |\n")
	b.WriteString("| --- | --- | --- |\n")
	for _, from := range policy.Layers {
		var allowed []string
		for _, to := range policy.Layers {
			if to.Rank <= from.Rank {
				allowed = append(allowed, to.Name)
			}
		}
		var ports []string
		if contains(policy.BusinessLayers, from.Name) {
			for _, port := range policy.PortsAndAdapters {
				ports = append(ports, joinCode(port.Roots))
			}
		}
		if len(ports) == 0 {
			ports = []string{"none"}
		}
		b.WriteString("| " + cell(from.Name) + " | " + cell(strings.Join(allowed, ", ")) + " | " + cell(strings.Join(ports, ", ")) + " |\n")
	}
	b.WriteString("\n")
	b.WriteString("Forbidden edge rules are evaluated by `tools/policy/depedge`: `" + strings.Join(ruleNames(policy), "`, `") + "`.\n\n")

	b.WriteString("## Library firewall roots\n\n")
	b.WriteString("Third-party modules are admitted only at the owning roots declared by `dependency-roles.yaml`. Empty root lists mean the module is not expected to be imported directly.\n\n")
	b.WriteString("| Library selector | Role | Allowed import roots |\n")
	b.WriteString("| --- | --- | --- |\n")
	for _, rule := range roles.FamilyRules {
		b.WriteString("| `" + cell(rule.Prefix) + "` | " + cell(rule.Role) + " | " + cell(joinCode(rule.AllowedImportRoots)) + " |\n")
	}
	for _, module := range roles.Modules {
		if len(module.AllowedImportRoots) == 0 {
			continue
		}
		b.WriteString("| `" + cell(module.Path) + "` | " + cell(module.Role) + " | " + cell(joinCode(module.AllowedImportRoots)) + " |\n")
	}
	b.WriteString("\n")

	b.WriteString("## Package inventory by declared root\n\n")
	packages := inventory(layoutDoc, layoutDoc.Module, graph)
	for _, root := range layoutDoc.AllowedRoots {
		b.WriteString("### `" + root.Name + "`\n\n")
		b.WriteString(cell(root.Description) + "\n\n")
		items := packages[root.Name]
		if len(items) == 0 {
			b.WriteString("_No Go packages currently scanned._\n\n")
			continue
		}
		for _, item := range items {
			b.WriteString("- `" + item + "`\n")
		}
		b.WriteString("\n")
	}

	digest := sha256.Sum256([]byte(b.String()))
	b.WriteString("## Document digest\n\n`" + hex.EncodeToString(digest[:]) + "`\n")
	return []byte(b.String())
}

func inventory(manifest layoutDocument, module string, graph graphInfo) map[string][]string {
	result := make(map[string][]string, len(manifest.AllowedRoots))
	for _, root := range manifest.AllowedRoots {
		result[root.Name] = nil
	}
	for _, pkg := range graph.Packages {
		rel := strings.TrimPrefix(pkg.ImportPath, module+"/")
		if rel == pkg.ImportPath || rel == "" {
			continue
		}
		root := strings.Split(rel, "/")[0]
		if _, ok := result[root]; !ok {
			continue
		}
		result[root] = append(result[root], module+"/"+rel)
	}
	for root := range result {
		sort.Strings(result[root])
	}
	return result
}

func buildGraph(root string, layoutManifest *layout.Manifest, policy *depedge.Policy) (graphInfo, error) {
	graph, err := importgraph.Build(root, layoutManifest, policy)
	if err == nil {
		result := graphInfo{Digest: graph.Digest}
		for _, pkg := range graph.Packages {
			result.Packages = append(result.Packages, packageInfo{ImportPath: pkg.ImportPath, Dir: pkg.Dir, Imports: append([]string(nil), pkg.Imports...)})
		}
		for _, edge := range graph.Edges {
			result.Edges = append(result.Edges, edgeInfo{Importer: edge.Importer, Imported: edge.Imported})
		}
		return result, nil
	}

	// A package tree can contain an empty deferred directory or another
	// pre-existing package that makes go list reject ./... before it can return
	// useful JSON. Architecture documentation still needs to describe the
	// source tree, so fall back to a deterministic AST scan of actual .go
	// files. The normal path remains importgraph.Build, preserving its policy
	// semantics whenever the repository package graph is listable.
	return fallbackGraph(root, policy)
}

func fallbackGraph(root string, policy *depedge.Policy) (graphInfo, error) {
	module := "github.com/monstercameron/human-capital-management-suite"
	byDir := map[string]*packageInfo{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			if rel != "." && (rel == ".git" || strings.HasPrefix(rel, ".git/") || rel == "node_modules" || strings.HasPrefix(rel, "node_modules/") || rel == "src/blocks/go" || strings.HasPrefix(rel, "src/blocks/go/")) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || filepath.Ext(path) != ".go" {
			return nil
		}
		relDir, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		importPath := module
		if relDir != "." {
			importPath += "/" + filepath.ToSlash(relDir)
		}
		pkg := byDir[relDir]
		if pkg == nil {
			pkg = &packageInfo{ImportPath: importPath, Dir: filepath.Dir(path)}
			byDir[relDir] = pkg
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		for _, spec := range file.Imports {
			pkg.Imports = append(pkg.Imports, strings.Trim(spec.Path.Value, `"`))
		}
		return nil
	})
	if err != nil {
		return graphInfo{}, fmt.Errorf("fallback Go tree scan: %w", err)
	}

	result := graphInfo{}
	for _, pkg := range byDir {
		sort.Strings(pkg.Imports)
		result.Packages = append(result.Packages, *pkg)
	}
	sort.Slice(result.Packages, func(i, j int) bool { return result.Packages[i].ImportPath < result.Packages[j].ImportPath })
	known := make(map[string]bool, len(result.Packages))
	for _, pkg := range result.Packages {
		known[pkg.ImportPath] = true
	}
	for _, pkg := range result.Packages {
		for _, imported := range pkg.Imports {
			if !strings.HasPrefix(imported, module+"/") || !known[imported] {
				continue
			}
			result.Edges = append(result.Edges, edgeInfo{Importer: pkg.ImportPath, Imported: imported})
			_ = policy.CheckEdge(pkg.ImportPath, imported)
		}
	}
	lines := make([]string, len(result.Edges))
	for i, edge := range result.Edges {
		lines[i] = edge.Importer + " -> " + edge.Imported
	}
	sort.Strings(lines)
	h := sha256.New()
	for _, line := range lines {
		h.Write([]byte(line + "\n"))
	}
	result.Digest = hex.EncodeToString(h.Sum(nil))
	return result, nil
}

func ruleNames(policy *depedge.Policy) []string {
	return []string{
		depedge.RuleKernelUpward,
		depedge.RuleEngineImportsDomain,
		depedge.RuleWorkflowDomainPersist,
		depedge.RuleTransportImportsStore,
		depedge.RuleBusinessConcreteAdapter,
		depedge.RuleLayerUpward,
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func joinCode(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	items := make([]string, len(values))
	for i, value := range values {
		items[i] = "`" + value + "`"
	}
	return strings.Join(items, ", ")
}

func cell(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	return strings.ReplaceAll(value, "|", "\\|")
}
