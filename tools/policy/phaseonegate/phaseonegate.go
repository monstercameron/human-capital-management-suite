// Package phaseonegate checks the exact production package closure admitted
// to Phase 1. The allowlist is package-level and is compiled from the real
// cmd/hcmnext production graph; repository-layout declarations alone never
// make a package part of the release.
package phaseonegate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/layout"
	"gopkg.in/yaml.v3"
)

const (
	// EntryPoint is the one composition root whose production closure Gate A
	// admits. Other commands are separate release artifacts and must not be
	// added merely because repository-layout.yaml names them.
	EntryPoint = "./cmd/hcmnext"

	// ManifestVersion changes when the JSON shape or its generation contract
	// changes, not when the package graph changes.
	ManifestVersion = 1
)

// Package is one package in the production dependency graph.
type Package struct {
	Path string
}

// Edge is one direct, in-module production import edge.
type Edge struct {
	Importer string `json:"importer"`
	Imported string `json:"imported"`
}

// Graph is the production closure returned by go list -deps -json.
type Graph struct {
	Module   string
	Entry    string
	Packages []Package
	Edges    []Edge
}

// Inclusion explains why one physically present package is admitted.
type Inclusion struct {
	Path   string `json:"path"`
	Root   string `json:"repository_root"`
	Layer  string `json:"layer"`
	Reason string `json:"reason"`
}

// Deferred explains which later gate may admit one physical package that is
// not in the cmd/hcmnext production closure.
type Deferred struct {
	Path      string `json:"path"`
	Root      string `json:"repository_root"`
	Gate      string `json:"deferred_gate"`
	OwnerTodo string `json:"owner_todo"`
	Reason    string `json:"reason"`
}

// LiveGap pins an intentionally reachable package whose layout phase is
// later than P1A. It prevents a phase-label mismatch from becoming invisible
// while preserving the exact graph refusal for any unlisted package.
type LiveGap struct {
	PathPrefix string `json:"path_prefix"`
	Gate       string `json:"gate"`
	OwnerTodo  string `json:"owner_todo"`
	Reason     string `json:"reason"`
}

// Manifest is the generated, reviewable Phase 1 package contract.
type Manifest struct {
	Version                 int         `json:"version"`
	Module                  string      `json:"module"`
	EntryPoint              string      `json:"entry_point"`
	Allowlist               []Inclusion `json:"allowlist"`
	Deferred                []Deferred  `json:"deferred"`
	ForbiddenImportPrefixes []string    `json:"forbidden_import_prefixes"`
	LiveGaps                []LiveGap   `json:"live_gaps"`
}

// Violation is one deterministic gate finding. Importer and Imported name
// the offending edge for graph findings; stale entries use Imported only.
type Violation struct {
	Kind     string
	Importer string
	Imported string
	Detail   string
}

// p1aManifest is deliberately only the part of the release manifest this
// gate consumes. The signed manifest remains owned by tools/planning.
type p1aManifest struct {
	ForbiddenImportPrefixes []string `yaml:"forbidden_import_prefixes"`
}

// LoadForbiddenImportPrefixes reads the P1A release manifest without taking
// ownership of its signature or any other planning concern.
func LoadForbiddenImportPrefixes(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("phaseonegate: reading P1A manifest: %w", err)
	}
	var m p1aManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("phaseonegate: parsing P1A manifest: %w", err)
	}
	sort.Strings(m.ForbiddenImportPrefixes)
	return m.ForbiddenImportPrefixes, nil
}

// BuildProductionGraph invokes go list on the real tree. The default Go list
// mode excludes *_test.go imports, which is the required production graph.
func BuildProductionGraph(root, entry string) (*Graph, error) {
	cmd := exec.Command("go", "list", "-deps", "-json", entry)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list -deps -json %s failed: %w\nstderr:\n%s", entry, err, stderr.String())
	}

	module := repopath.ModulePath(root)
	type listedPackage struct {
		ImportPath string
		Imports    []string
	}
	var listed []listedPackage
	decoder := json.NewDecoder(&stdout)
	for {
		var pkg listedPackage
		err := decoder.Decode(&pkg)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decoding go list output: %w", err)
		}
		listed = append(listed, pkg)
	}

	seen := make(map[string]bool)
	graph := &Graph{Module: module, Entry: entry}
	for _, pkg := range listed {
		if !isModulePath(module, pkg.ImportPath) || seen[pkg.ImportPath] {
			continue
		}
		seen[pkg.ImportPath] = true
		graph.Packages = append(graph.Packages, Package{Path: pkg.ImportPath})
		for _, imported := range pkg.Imports {
			if isModulePath(module, imported) && imported != pkg.ImportPath {
				graph.Edges = append(graph.Edges, Edge{Importer: pkg.ImportPath, Imported: imported})
			}
		}
	}
	sort.Slice(graph.Packages, func(i, j int) bool { return graph.Packages[i].Path < graph.Packages[j].Path })
	sort.Slice(graph.Edges, func(i, j int) bool {
		if graph.Edges[i].Importer != graph.Edges[j].Importer {
			return graph.Edges[i].Importer < graph.Edges[j].Importer
		}
		return graph.Edges[i].Imported < graph.Edges[j].Imported
	})
	return graph, nil
}

// DiscoverPhysicalPackages finds production Go package directories under
// release-shaped roots. It intentionally does not use layout declarations as
// an allowlist: a directory with no graph edge becomes a deferred row.
func DiscoverPhysicalPackages(root, module string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			if rel != "." && (excludedPhysicalDirectory(rel) || !isReleasePhysicalRoot(strings.Split(rel, "/")[0])) {
				// Only release roots can yield physical packages. Skipping the
				// rest keeps the walk out of .artifacts/, planning/ and other
				// trees that are created and removed concurrently (go build
				// temp dirs), which made discovery fail with ENOENT.
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		relFile, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		relFile = filepath.ToSlash(relFile)
		rootName := strings.Split(relFile, "/")[0]
		if !isReleasePhysicalRoot(rootName) {
			return nil
		}
		dir := filepath.ToSlash(filepath.Dir(relFile))
		if dir == "." {
			paths = append(paths, module)
		} else {
			paths = append(paths, module+"/"+dir)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discovering physical packages: %w", err)
	}
	sort.Strings(paths)
	return uniqueStrings(paths), nil
}

// Generate creates the manifest from layout roots and the actual production
// graph. physical may be nil to discover release-shaped package directories.
func Generate(layoutManifest *layout.Manifest, graph *Graph, physical []string, forbidden []string) Manifest {
	if physical == nil {
		physical, _ = DiscoverPhysicalPackagesFromGraph(graph)
	}
	allowed := make(map[string]bool, len(graph.Packages))
	for _, pkg := range graph.Packages {
		allowed[pkg.Path] = true
	}

	manifest := Manifest{
		Version:                 ManifestVersion,
		Module:                  graph.Module,
		EntryPoint:              graph.Entry,
		ForbiddenImportPrefixes: append([]string(nil), forbidden...),
	}
	sort.Strings(manifest.ForbiddenImportPrefixes)
	for _, pkg := range graph.Packages {
		manifest.Allowlist = append(manifest.Allowlist, inclusionFor(layoutManifest, pkg.Path))
	}
	for _, path := range uniqueStrings(physical) {
		if allowed[path] {
			continue
		}
		manifest.Deferred = append(manifest.Deferred, deferredFor(layoutManifest, path))
	}
	manifest.LiveGaps = append(manifest.LiveGaps, liveGaps(layoutManifest, graph.Packages)...)
	return manifest
}

// DiscoverPhysicalPackagesFromGraph is a convenience for Generate when the
// caller has only a repository graph. It locates the module root from the
// package implementation and discovers physical packages there.
func DiscoverPhysicalPackagesFromGraph(graph *Graph) ([]string, error) {
	return DiscoverPhysicalPackages(repopath.RootDir(), graph.Module)
}

// Check evaluates the exact package closure and the manifest's refusal
// conditions. Every finding is tied to a package or direct edge.
func Check(manifest Manifest, graph *Graph) []Violation {
	var violations []Violation
	if manifest.Module != graph.Module {
		violations = append(violations, Violation{Kind: "module-mismatch", Detail: fmt.Sprintf("manifest module %q differs from graph module %q", manifest.Module, graph.Module)})
	}
	allow := make(map[string]Inclusion, len(manifest.Allowlist))
	for _, entry := range manifest.Allowlist {
		if _, duplicate := allow[entry.Path]; duplicate {
			violations = append(violations, Violation{Kind: "duplicate-allowlist-entry", Imported: entry.Path, Detail: "package appears more than once"})
			continue
		}
		allow[entry.Path] = entry
	}
	graphPackages := make(map[string]bool, len(graph.Packages))
	for _, pkg := range graph.Packages {
		graphPackages[pkg.Path] = true
	}
	for _, entry := range manifest.Allowlist {
		if !graphPackages[entry.Path] {
			violations = append(violations, Violation{Kind: "stale-allowlist-entry", Imported: entry.Path, Detail: "allowlist package is not reached by the production graph"})
		}
	}
	for _, edge := range graph.Edges {
		if !graphPackages[edge.Importer] {
			continue
		}
		if _, ok := allow[edge.Imported]; !ok {
			violations = append(violations, Violation{Kind: "outside-allowlist-edge", Importer: edge.Importer, Imported: edge.Imported, Detail: "production import is outside the Phase 1 package allowlist"})
		}
		for _, prefix := range manifest.ForbiddenImportPrefixes {
			if edge.Imported == prefix || strings.HasPrefix(edge.Imported, prefix+"/") {
				violations = append(violations, Violation{Kind: "forbidden-import-prefix", Importer: edge.Importer, Imported: edge.Imported, Detail: "P1A forbidden import prefix"})
			}
		}
	}
	for _, pkg := range graph.Packages {
		if _, ok := allow[pkg.Path]; ok {
			continue
		}
		if !hasIncoming(graph.Edges, pkg.Path) && pkg.Path != graph.Module {
			violations = append(violations, Violation{Kind: "outside-allowlist-package", Importer: graph.Entry, Imported: pkg.Path, Detail: "production package is not listed and has no direct witness edge"})
		}
	}
	sort.SliceStable(violations, func(i, j int) bool {
		if violations[i].Kind != violations[j].Kind {
			return violations[i].Kind < violations[j].Kind
		}
		if violations[i].Importer != violations[j].Importer {
			return violations[i].Importer < violations[j].Importer
		}
		return violations[i].Imported < violations[j].Imported
	})
	return violations
}

// ValidateManifest returns an error suitable for a policy command when the
// exact graph and generated manifest do not agree. Check remains available
// for callers that need each offending edge separately.
func ValidateManifest(manifest Manifest, graph *Graph) error {
	violations := Check(manifest, graph)
	if len(violations) == 0 {
		return nil
	}
	first := violations[0]
	return fmt.Errorf("phaseonegate: %s: %s -> %s: %s", first.Kind, first.Importer, first.Imported, first.Detail)
}

// CanonicalJSON renders a stable generated-manifest golden.
func CanonicalJSON(manifest Manifest) ([]byte, error) {
	return json.MarshalIndent(manifest, "", "  ")
}

func isModulePath(module, path string) bool {
	return path == module || strings.HasPrefix(path, module+"/")
}

func hasIncoming(edges []Edge, imported string) bool {
	for _, edge := range edges {
		if edge.Imported == imported {
			return true
		}
	}
	return false
}

func inclusionFor(m *layout.Manifest, path string) Inclusion {
	rel := strings.TrimPrefix(path, m.Module+"/")
	root, layer := layoutRoot(m, rel)
	reason := "reachable production dependency of cmd/hcmnext; required by the Phase 1 composition closure"
	switch {
	case root == "cmd":
		reason = "the Phase 1 composition root admitted by the release gate"
	case root == "gen":
		reason = "generated wire contract consumed by the Phase 1 intent and transport surfaces"
	case root == "migrations":
		reason = "embedded PostgreSQL migration package required to serve the Phase 1 cell"
	case root == "internal" && strings.HasPrefix(rel, "internal/kernel"):
		reason = "kernel identity, value, time and digest primitives required by the reachable Phase 1 packages"
	case root == "internal" && strings.HasPrefix(rel, "internal/engines"):
		reason = "reachable transformation/rules engine contract required by the Phase 1 wedge"
	case root == "internal" && strings.HasPrefix(rel, "internal/domains"):
		reason = "reachable HCM domain/support package required by a Phase 1 intent or its evidence path"
	case root == "internal" && strings.HasPrefix(rel, "internal/data"):
		reason = "reachable data port or PostgreSQL adapter required for Phase 1 chronology and serving"
	case root == "internal" && strings.HasPrefix(rel, "internal/connectivity"):
		reason = "reachable system-integration observation adapter required by the Phase 1 cell"
	case root == "internal" && strings.HasPrefix(rel, "internal/humanwork"):
		reason = "reachable work/forms/messaging workspace adapter required by the admitted Phase 1 read-only surface"
	case root == "tools":
		reason = "reachable developer-owned rendering/qualification support required by the Phase 1 workspace"
	}
	return Inclusion{Path: path, Root: root, Layer: layer, Reason: reason}
}

func deferredFor(m *layout.Manifest, path string) Deferred {
	rel := strings.TrimPrefix(path, m.Module+"/")
	root, _ := layoutRoot(m, rel)
	gate, owner, reason := "ARCH-GO-018", "ARCH-GO-018", "not reached by cmd/hcmnext; admit only after its first approved Phase 1 consumer"
	if isDeferredSchemaDomain(rel) {
		gate, owner, reason = "DB-016", "DB-016", "deferred-domain schema/conformance only; a future domain authority todo must precede production admission"
	} else if strings.HasPrefix(rel, "internal/humanwork") {
		gate, owner, reason = "ARCH-GO-022", "ARCH-GO-022", "human-work separation and delivery authority remain deferred until the owning gate admits a consumer"
	} else if strings.HasPrefix(rel, "internal/workflow") {
		gate, owner, reason = "WF-RUN-000", "WF-RUN-000", "workflow runtime admission is governed by the selected runtime gate"
	} else if strings.HasPrefix(rel, "internal/transaction") {
		gate, owner, reason = "TX-001", "TX-001", "transaction authority is admitted only by its first approved coordinator consumer"
	} else if strings.HasPrefix(rel, "internal/engines/") {
		gate, owner, reason = "ARCH-GO-018", "ARCH-GO-018", "engine remains definitions/conformance-only until a first approved consumer reaches it"
	} else if strings.HasPrefix(rel, "internal/connectivity/") {
		gate, owner, reason = "INTG-001", "INTG-001", "integration adapter is deferred until its selected connector contract has an approved consumer"
	}
	return Deferred{Path: path, Root: root, Gate: gate, OwnerTodo: owner, Reason: reason}
}

func isDeferredSchemaDomain(rel string) bool {
	if !strings.HasPrefix(rel, "internal/domains/") {
		return false
	}
	suffix := strings.TrimPrefix(rel, "internal/domains/")
	slug := strings.Split(suffix, "/")[0]
	// This is the fixed DB-016 preview set from tools/gen/deferredschema:
	// payroll, benefits, time, leave, recruiting, talent, learning, case,
	// access and regulatory. Other domain directories have their own future
	// authority owner and must not be misrepresented as DB-016 previews.
	switch slug {
	case "payroll", "benefits", "time", "leave", "recruiting", "talent", "learning", "case", "hrcase", "access", "regulatory":
		return true
	default:
		return false
	}
}

func liveGaps(m *layout.Manifest, packages []Package) []LiveGap {
	seen := make(map[string]bool)
	var gaps []LiveGap
	for _, pkg := range packages {
		rel := strings.TrimPrefix(pkg.Path, m.Module+"/")
		if !strings.HasPrefix(rel, "internal/") {
			continue
		}
		parts := strings.Split(rel, "/")
		if len(parts) < 2 {
			continue
		}
		name := parts[1]
		phase := ""
		owner := ""
		for _, declared := range m.InternalPackageRoots {
			if declared.Name == name {
				phase, owner = declared.Phase, declared.Owner
				break
			}
		}
		if phase == "" || phase == "P1A" {
			continue
		}
		prefix := m.Module + "/internal/" + name
		if seen[prefix] {
			continue
		}
		seen[prefix] = true
		gate, todo := "ARCH-GO-018", "ARCH-GO-018"
		if name == "humanwork" {
			gate, todo = "ARCH-GO-022", "ARCH-GO-022"
		} else if name == "workflow" {
			gate, todo = "WF-RUN-000", "WF-RUN-000"
		} else if name == "transaction" {
			gate, todo = "TX-001", "TX-001"
		}
		gaps = append(gaps, LiveGap{PathPrefix: prefix, Gate: gate, OwnerTodo: todo, Reason: fmt.Sprintf("%s-owned root is labeled %s in repository-layout.yaml but is reachable from the selected Phase 1 command", owner, phase)})
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].PathPrefix < gaps[j].PathPrefix })
	return gaps
}

func layoutRoot(m *layout.Manifest, rel string) (string, string) {
	parts := strings.Split(rel, "/")
	if len(parts) == 0 {
		return "", ""
	}
	if parts[0] != "internal" {
		return parts[0], "composition"
	}
	if len(parts) < 2 {
		return "internal", ""
	}
	for _, root := range m.InternalPackageRoots {
		if root.Name == parts[1] {
			return "internal/" + root.Name, root.Layer
		}
	}
	return "internal/" + parts[1], "undeclared"
}

func isReleasePhysicalRoot(root string) bool {
	switch root {
	case "api", "cmd", "gen", "internal", "migrations":
		return true
	default:
		return false
	}
}

func excludedPhysicalDirectory(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		switch part {
		case ".git", "node_modules", "vendor", "testdata":
			return true
		}
	}
	return rel == "src" || strings.HasPrefix(rel, "src/")
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := append([]string(nil), values...)
	sort.Strings(out)
	result := out[:0]
	for _, value := range out {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}
