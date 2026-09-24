package sbom

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"
)

// DefaultRootVersion is used for the root/application component's version
// when the caller supplies none (this repository is not currently tagged,
// so there is no reliable version string to read without running git,
// which this generator deliberately never does).
const DefaultRootVersion = "0.0.0-devel"

// GeneratorName/GeneratorVendor identify this tool in Metadata.Tools.
const (
	GeneratorName   = "human-capital-management-suite-sbomgen"
	GeneratorVendor = "github.com/monstercameron/human-capital-management-suite"
)

// Options configures Generate. All fields are optional.
type Options struct {
	// RootVersion overrides the root/application component's version.
	// Defaults to DefaultRootVersion.
	RootVersion string
	// GeneratorVersion is recorded in Metadata.Tools; defaults to "dev".
	GeneratorVersion string
	// Now returns the generation timestamp; defaults to time.Now.
	Now func() time.Time
	// LicenseReader is the file port used to read module-owned go.mod and
	// LICENSE evidence. It defaults to OSFileReader.
	LicenseReader LicenseEvidenceReader
	// ModuleCache overrides the Go module cache used for dependency license
	// evidence. An empty value is resolved through GOMODCACHE/go env.
	ModuleCache string
	// LicenseExceptions are copied into the generated document and may
	// explain UNKNOWN dependency licenses when they are complete and current.
	LicenseExceptions []LicenseException
	// ArtifactPath binds the root application component to the exact bytes
	// shipped by a release. If empty, the generated BOM remains an inventory
	// of the source module graph and carries no artifact subject hash.
	ArtifactPath string
}

func (o Options) rootVersion() string {
	if o.RootVersion != "" {
		return o.RootVersion
	}
	return DefaultRootVersion
}

func (o Options) generatorVersion() string {
	if o.GeneratorVersion != "" {
		return o.GeneratorVersion
	}
	return "dev"
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// purl builds a Go module purl per the CycloneDX/package-url Go type
// (https://github.com/package-url/purl-spec): "pkg:golang/<path>@<version>".
func purl(path, version string) string {
	return fmt.Sprintf("pkg:golang/%s@%s", path, version)
}

// Generate builds the CycloneDX 1.5 BOM for the Go module rooted at root.
//
// It parses go.mod's require block (ParseRequires), go.sum's content
// hashes (ParseGoSum) and `go mod graph`'s require edges (ModGraph, filtered
// to selected versions) — see doc.go for why, in preference to
// `go list -m -json all`.
func Generate(root string, opts Options) (*Document, error) {
	modulePath, err := ModulePath(root)
	if err != nil {
		return nil, err
	}
	requires, err := ParseRequires(root)
	if err != nil {
		return nil, err
	}
	hashes, err := ParseGoSum(root)
	if err != nil {
		return nil, err
	}
	edges, err := ModGraph(root)
	if err != nil {
		return nil, err
	}

	doc := buildDocument(modulePath, requires, hashes, edges, opts)
	if opts.ArtifactPath != "" {
		digest, err := artifactDigest(opts.ArtifactPath)
		if err != nil {
			return nil, err
		}
		root := doc.Metadata.Component
		root.Hashes = []Hash{{Alg: HashAlgSHA256, Content: digest}}
		doc.Metadata.Component = root
	}
	licenses, err := resolveComponentLicenses(root, modulePath, requires, opts)
	if err != nil {
		return nil, err
	}
	applyLicenseEvidence(doc, licenses, opts.LicenseExceptions)
	return doc, nil
}

func buildDocumentWithLicenses(modulePath string, requires []Require, hashes map[string]SumHash, edges []GraphEdge, opts Options, licenses map[string]LicenseEvidence) *Document {
	doc := buildDocument(modulePath, requires, hashes, edges, opts)
	applyLicenseEvidence(doc, licenses, opts.LicenseExceptions)
	return doc
}

func resolveComponentLicenses(root, modulePath string, requires []Require, opts Options) (map[string]LicenseEvidence, error) {
	cache := opts.ModuleCache
	if cache == "" {
		var err error
		cache, err = defaultModuleCache()
		if err != nil {
			return nil, err
		}
	}
	resolver := NewLicenseResolver(opts.LicenseReader, cache)
	licenses := make(map[string]LicenseEvidence, len(requires)+1)
	rootEvidence, err := resolveLicenseInDir(resolver.Reader, root)
	if err != nil {
		return nil, err
	}
	licenses[modulePath+"@"+opts.rootVersion()] = rootEvidence
	for _, require := range requires {
		evidence, err := resolver.Resolve(require.Path, require.Version, "")
		if err != nil {
			return nil, err
		}
		licenses[require.Path+"@"+require.Version] = evidence
	}
	return licenses, nil
}

func applyLicenseEvidence(doc *Document, licenses map[string]LicenseEvidence, exceptions []LicenseException) {
	if doc == nil {
		return
	}
	root := doc.Metadata.Component
	if evidence, ok := licenses[root.Name+"@"+root.Version]; ok {
		root.License = evidence.Expression
		doc.Metadata.Component = root
	}
	for i := range doc.Components {
		component := &doc.Components[i]
		if evidence, ok := licenses[component.Name+"@"+component.Version]; ok {
			component.License = evidence.Expression
		}
	}
	doc.LicenseExceptions = append([]LicenseException(nil), exceptions...)
	sort.Slice(doc.LicenseExceptions, func(i, j int) bool {
		left, right := doc.LicenseExceptions[i], doc.LicenseExceptions[j]
		if left.Component != right.Component {
			return left.Component < right.Component
		}
		return left.Version < right.Version
	})
}

// buildDocument assembles the Document from already-parsed inputs, with no
// filesystem or subprocess access of its own — the pure core Generate
// delegates to, and what golden/unit tests exercise directly for
// deterministic, exec-free coverage of the document shape.
func buildDocument(modulePath string, requires []Require, hashes map[string]SumHash, edges []GraphEdge, opts Options) *Document {
	rootVersion := opts.rootVersion()
	rootRef := purl(modulePath, rootVersion)

	// selected holds, for every module this BOM knows a version for
	// (including the root), the version considered "as built" — used to
	// filter go mod graph's pre-MVS-selection edges down to real ones.
	selected := make(map[string]string, len(requires)+1)
	selected[modulePath] = rootVersion
	for _, r := range requires {
		selected[r.Path] = r.Version
	}

	components := make([]Component, 0, len(requires))
	for _, r := range requires {
		ref := purl(r.Path, r.Version)
		scope := ScopeRequired
		if r.Indirect {
			scope = ScopeOptional
		}
		c := Component{
			BOMRef:  ref,
			Type:    ComponentTypeLibrary,
			Name:    r.Path,
			Version: r.Version,
			PURL:    ref,
			Scope:   scope,
		}
		if h, ok := hashes[r.Path+"@"+r.Version]; ok {
			c.Hashes = []Hash{{Alg: h.Alg, Content: h.Content}}
		}
		components = append(components, c)
	}
	sort.Slice(components, func(i, j int) bool { return components[i].Name < components[j].Name })

	rootComponent := Component{
		BOMRef:  rootRef,
		Type:    ComponentTypeApplication,
		Name:    modulePath,
		Version: rootVersion,
		PURL:    rootRef,
	}

	dependencies := buildDependencies(modulePath, rootRef, selected, edges)

	doc := &Document{
		BOMFormat:    BOMFormatCycloneDX,
		SpecVersion:  SpecVersion15,
		SerialNumber: deterministicSerial(modulePath, rootVersion, components),
		Version:      1,
		Metadata: Metadata{
			Timestamp: opts.now().UTC().Format(time.RFC3339),
			Tools: []Tool{
				{Vendor: GeneratorVendor, Name: GeneratorName, Version: opts.generatorVersion()},
			},
			Component: rootComponent,
		},
		Components:   components,
		Dependencies: dependencies,
	}
	return doc
}

// buildDependencies turns go mod graph's edges into a CycloneDX dependency
// list, keeping only edges whose endpoints match the selected (as-built)
// version for both the from- and to-module — see ModGraph's doc comment.
func buildDependencies(modulePath, rootRef string, selected map[string]string, edges []GraphEdge) []Dependency {
	dependsOn := make(map[string]map[string]bool) // fromRef -> set of toRef

	addEdge := func(fromRef, toRef string) {
		if dependsOn[fromRef] == nil {
			dependsOn[fromRef] = make(map[string]bool)
		}
		dependsOn[fromRef][toRef] = true
	}

	for _, e := range edges {
		var fromRef string
		if e.FromPath == modulePath {
			// `go mod graph` never prints a version for the main module.
			fromRef = rootRef
		} else {
			wantVersion, ok := selected[e.FromPath]
			if !ok || wantVersion != e.FromVersion {
				continue // superseded/never-selected version of the source module
			}
			fromRef = purl(e.FromPath, e.FromVersion)
		}

		wantVersion, ok := selected[e.ToPath]
		if !ok || wantVersion != e.ToVersion {
			continue // superseded/never-selected version of the target module
		}
		toRef := purl(e.ToPath, e.ToVersion)
		if fromRef == toRef {
			continue
		}
		addEdge(fromRef, toRef)
	}

	// Every component gets a Dependency entry, even with no outbound edges,
	// so the graph is queryable by ref for any known component.
	refs := make([]string, 0, len(dependsOn)+1)
	seen := map[string]bool{rootRef: true}
	refs = append(refs, rootRef)
	for path, version := range selected {
		if path == modulePath {
			continue
		}
		ref := purl(path, version)
		if !seen[ref] {
			seen[ref] = true
			refs = append(refs, ref)
		}
	}
	sort.Strings(refs)

	deps := make([]Dependency, 0, len(refs))
	for _, ref := range refs {
		d := Dependency{Ref: ref}
		if set := dependsOn[ref]; len(set) > 0 {
			list := make([]string, 0, len(set))
			for to := range set {
				list = append(list, to)
			}
			sort.Strings(list)
			d.DependsOn = list
		}
		deps = append(deps, d)
	}
	return deps
}

// deterministicSerial derives a stable "urn:uuid:"-form serial number from
// the document's own content identity (root module/version plus the sorted
// component list), so re-generating the BOM against an unchanged dependency
// graph reproduces the same serial number instead of a fresh random one
// every run.
func deterministicSerial(modulePath, rootVersion string, components []Component) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s@%s\n", modulePath, rootVersion)
	for _, c := range components {
		fmt.Fprintf(h, "%s@%s\n", c.Name, c.Version)
	}
	sum := h.Sum(nil)

	// Lay the first 16 bytes of the digest out as a UUID string, forcing
	// the version/variant nibbles per RFC 4122 so the result is a
	// syntactically valid UUID; this is an identifier, not a
	// cryptographic commitment, so a truncated hash is sufficient.
	b := make([]byte, 16)
	copy(b, sum[:16])
	b[6] = (b[6] & 0x0f) | 0x50 // version 5 (name-based, by convention here)
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	hexStr := hex.EncodeToString(b)
	return "urn:uuid:" + hexStr[0:8] + "-" + hexStr[8:12] + "-" + hexStr[12:16] + "-" + hexStr[16:20] + "-" + hexStr[20:32]
}
