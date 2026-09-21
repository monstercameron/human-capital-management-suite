// Package drift provides the MSRC-010 source/generated/document drift gate.
//
// The checker is deliberately read-only: it compiles the SchemaFlux sources in
// memory, compares the resulting artifact with the checked-in generated file,
// and hashes normative documents against an explicit baseline. It never
// rewrites generated output.
package drift

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux"
	"github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux/modelgen"
	"github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux/sources"
)

type Document struct{ Path, Digest string }

type Config struct {
	Root          string
	GeneratedPath string
	Documents     []Document
}

type Finding struct{ Owner, Path, Expected, Actual string }

type Report struct{ Findings []Finding }

func (r Report) Clean() bool { return len(r.Findings) == 0 }

func (r Report) Error() error {
	if r.Clean() {
		return nil
	}
	var b strings.Builder
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "%s drift at %s: expected %s, got %s\n", f.Owner, f.Path, f.Expected, f.Actual)
	}
	return fmt.Errorf("schemaflux drift check failed:\n%s", strings.TrimSuffix(b.String(), "\n"))
}

// CheckRepository applies the checked-in MSRC-010 baseline.
func CheckRepository(root string) Report {
	return Check(Config{Root: root, GeneratedPath: "internal/generated/schemaflux/models_generated.go", Documents: []Document{
		{Path: "planning/data/models/README.md", Digest: "sha256:ea093186a267084088a4b1c2f2601d3ef442f6e3a12350402fb885596d46ac5e"},
		{Path: "planning/data/models/registry-and-coverage-contracts.md", Digest: "sha256:53873ce31851b2ae00d1ee489cb86025cff3be82d52584b8bbb894f949b39257"},
		{Path: "planning/specs/business-intent-catalog.md", Digest: "sha256:ade5585b510b2dd474d1caa463f2865f79bd092b7e90503342a524dd1d4b6999"},
	}})
}

func Check(c Config) Report {
	var r Report
	root := c.Root
	m, err := loadManifest(root)
	if err != nil {
		r.Findings = append(r.Findings, Finding{"source", "schema/schemaflux", "compilable", err.Error()})
		return r
	}
	a, err := modelgen.Generate(m, "schemaflux")
	if err != nil {
		r.Findings = append(r.Findings, Finding{"source", "schema/schemaflux", "generatable", err.Error()})
		return r
	}
	path := c.GeneratedPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		r.Findings = append(r.Findings, Finding{"generated", path, a.GeneratedDigest, err.Error()})
	} else {
		if !bytes.Equal(b, a.Go) {
			r.Findings = append(r.Findings, Finding{"generated", path, a.GeneratedDigest, schemaflux.Digest(b)})
		}
		if !strings.Contains(string(b), "Source digest: "+a.SourceDigest) {
			r.Findings = append(r.Findings, Finding{"source", path, a.SourceDigest, "missing or mismatched marker"})
		}
	}
	for _, d := range c.Documents {
		p := d.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		db, e := os.ReadFile(p)
		if e != nil {
			r.Findings = append(r.Findings, Finding{"document", p, d.Digest, e.Error()})
			continue
		}
		got := schemaflux.Digest(db)
		if d.Digest == "" || got != d.Digest {
			r.Findings = append(r.Findings, Finding{"document", p, d.Digest, got})
		}
	}
	return r
}

func loadManifest(root string) (*sources.Manifest, error) {
	mm, err := sources.LoadMetamodel(filepath.Join(root, "schema/schemaflux/metamodel/v1/metamodel.yaml"))
	if err != nil {
		return nil, err
	}
	auth, ret, err := sources.LoadRegistries(filepath.Join(root, "schema/schemaflux/registries/v1/registries.yaml"))
	if err != nil {
		return nil, err
	}
	ents, rels, err := sources.LoadEntityFamilies(filepath.Join(root, "schema/schemaflux/entities/v1"))
	if err != nil {
		return nil, err
	}
	m, errs := sources.Compile(sources.Bundle{Metamodel: mm, Authorities: auth, Retentions: ret, Entities: ents, Relationships: rels})
	if len(errs) != 0 {
		return nil, fmt.Errorf("%v", errs)
	}
	return m, nil
}
