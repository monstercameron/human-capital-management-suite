// Package depedge implements the ARCH-GO-002 package-dependency policy:
// evaluating one importer/imported edge against
// definitions/architecture/package-dependency-policy.yaml and naming the
// exact forbidden-edge rule when a business/layering invariant is violated.
package depedge

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Rule names. These match the `rules[].name` entries in
// package-dependency-policy.yaml so CI output and the manifest share one
// vocabulary.
const (
	RuleKernelUpward            = "kernel-must-not-import-upward"
	RuleEngineImportsDomain     = "engine-must-not-import-domain-implementation"
	RuleWorkflowDomainPersist   = "workflow-must-not-import-domain-persistence"
	RuleTransportImportsStore   = "transport-must-not-import-store"
	RuleBusinessConcreteAdapter = "business-must-not-import-concrete-adapter"
	RuleLayerUpward             = "layer-must-not-import-upward"
	RuleProductImportsTools     = "product-must-not-import-tools"
)

// Layer is one ranked layer in the dependency policy.
type Layer struct {
	Name  string   `yaml:"name"`
	Rank  int      `yaml:"rank"`
	Roots []string `yaml:"roots"`
}

// PortAdapter is one port/adapter grouping (transaction, ledger, data).
type PortAdapter struct {
	Name  string   `yaml:"name"`
	Roots []string `yaml:"roots"`
}

// Exception is a reviewed, narrow waiver of one edge.
type Exception struct {
	Importer  string `yaml:"importer"`
	Imported  string `yaml:"imported"`
	Owner     string `yaml:"owner"`
	Rationale string `yaml:"rationale"`
	Expiry    string `yaml:"expiry"`
}

// Policy is the parsed form of package-dependency-policy.yaml.
type Policy struct {
	Version int    `yaml:"version"`
	Module  string `yaml:"module"`

	Layers                   []Layer       `yaml:"layers"`
	BusinessLayers           []string      `yaml:"business_layers"`
	PortsAndAdapters         []PortAdapter `yaml:"ports_and_adapters"`
	ConcreteAdapterMarkers   []string      `yaml:"concrete_adapter_markers"`
	DomainPersistenceMarkers []string      `yaml:"domain_persistence_markers"`
	Exceptions               []Exception   `yaml:"exceptions"`
}

// Violation names the exact importer, imported path and rule broken.
type Violation struct {
	Importer string
	Imported string
	Rule     string
}

// Load reads and parses the policy at path.
func Load(path string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("depedge: reading policy: %w", err)
	}

	var p Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("depedge: parsing policy: %w", err)
	}
	if p.Module == "" {
		return nil, fmt.Errorf("depedge: policy has no module")
	}
	return &p, nil
}

func (p *Policy) trimModule(importPath string) (string, bool) {
	prefix := p.Module + "/"
	if !strings.HasPrefix(importPath, prefix) {
		return "", false
	}
	return strings.TrimPrefix(importPath, prefix), true
}

// layerOf returns the layer whose root matches rel, if any. Layer roots are
// disjoint prefixes in the current manifest (internal/kernel, internal/
// engines, ...), so the first match is unambiguous.
func (p *Policy) layerOf(rel string) (Layer, bool) {
	for _, l := range p.Layers {
		for _, root := range l.Roots {
			if rel == root || strings.HasPrefix(rel, root+"/") {
				return l, true
			}
		}
	}
	return Layer{}, false
}

func (p *Policy) portAdapterOf(rel string) (string, bool) {
	for _, pa := range p.PortsAndAdapters {
		for _, root := range pa.Roots {
			if rel == root || strings.HasPrefix(rel, root+"/") {
				return pa.Name, true
			}
		}
	}
	return "", false
}

func (p *Policy) isBusinessLayer(name string) bool {
	for _, b := range p.BusinessLayers {
		if b == name {
			return true
		}
	}
	return false
}

// matchGlob matches a single-"*"-wildcard-segment glob (e.g.
// "internal/domains/*/store") against a module-relative path.
func matchGlob(glob, rel string) bool {
	globSegs := strings.Split(glob, "/")
	relSegs := strings.Split(rel, "/")
	if len(relSegs) < len(globSegs) {
		return false
	}
	for i, g := range globSegs {
		if g == "*" {
			continue
		}
		if g != relSegs[i] {
			return false
		}
	}
	return true
}

func matchesAny(globs []string, rel string) bool {
	for _, g := range globs {
		if matchGlob(g, rel) {
			return true
		}
	}
	return false
}

func (p *Policy) matchException(importer, imported string) bool {
	for _, e := range p.Exceptions {
		if e.Importer == importer && e.Imported == imported {
			return true
		}
	}
	return false
}

// CheckEdge evaluates one direct import edge (importerPath importing
// importedPath, both full Go import paths) against the policy. It returns
// nil when the edge is allowed, or a Violation naming the exact rule
// broken.
func (p *Policy) CheckEdge(importerPath, importedPath string) *Violation {
	importerRel, ok := p.trimModule(importerPath)
	if !ok {
		return nil // not part of this module; not this policy's concern
	}
	importedRel, ok := p.trimModule(importedPath)
	if !ok {
		return nil // importing outside the module (stdlib/third-party) is LIB-002's concern, not ARCH-GO-002's
	}
	if importerRel == importedRel {
		return nil
	}
	// Product and command packages become part of release binaries. Developer
	// tools may inspect those packages, but the dependency direction must not
	// run from shipped code back into tools/.
	if (strings.HasPrefix(importerRel, "internal/") || strings.HasPrefix(importerRel, "cmd/")) &&
		(strings.HasPrefix(importedRel, "tools/") || importedRel == "tools") {
		return &Violation{importerPath, importedPath, RuleProductImportsTools}
	}
	if p.matchException(importerRel, importedRel) {
		return nil
	}

	importerLayer, importerHasLayer := p.layerOf(importerRel)
	importedLayer, importedHasLayer := p.layerOf(importedRel)
	importedPort, importedHasPort := p.portAdapterOf(importedRel)

	// Rule: kernel must not import upward (a higher-ranked layer or any
	// port/adapter).
	if importerHasLayer && importerLayer.Name == "kernel" {
		if importedHasLayer && importedLayer.Rank > importerLayer.Rank {
			return &Violation{importerPath, importedPath, RuleKernelUpward}
		}
		if importedHasPort {
			return &Violation{importerPath, importedPath, RuleKernelUpward}
		}
	}

	// Rule: an engine must not import a domain implementation.
	if importerHasLayer && importerLayer.Name == "engines" &&
		importedHasLayer && importedLayer.Name == "domains" {
		return &Violation{importerPath, importedPath, RuleEngineImportsDomain}
	}

	// Rule: workflow must not import a domain's persistence subpackage.
	if importerHasLayer && importerLayer.Name == "workflow" &&
		matchesAny(p.DomainPersistenceMarkers, importedRel) {
		return &Violation{importerPath, importedPath, RuleWorkflowDomainPersist}
	}

	// Rule: transport must not import a store (data/ledger port or adapter).
	if importerHasLayer && importerLayer.Name == "transport" && importedHasPort &&
		(importedPort == "data" || importedPort == "ledger") {
		return &Violation{importerPath, importedPath, RuleTransportImportsStore}
	}

	// Rule: a business-layer package must not import a concrete adapter.
	if importerHasLayer && p.isBusinessLayer(importerLayer.Name) &&
		matchesAny(p.ConcreteAdapterMarkers, importedRel) {
		return &Violation{importerPath, importedPath, RuleBusinessConcreteAdapter}
	}

	// Rule: any edge to a higher-ranked layer is forbidden. This is the
	// general form of the direction the named rules above pin for their
	// exact cases: a package may import a layer with a lower-or-equal
	// rank, never a higher one. It runs last so the named rules keep
	// their stable citations where both match. Edges touching an
	// unranked (port/adapter or unmodeled mesh) package stay out of the
	// spine policy's scope.
	if importerHasLayer && importedHasLayer &&
		importedLayer.Rank > importerLayer.Rank {
		return &Violation{importerPath, importedPath, RuleLayerUpward}
	}

	return nil
}
