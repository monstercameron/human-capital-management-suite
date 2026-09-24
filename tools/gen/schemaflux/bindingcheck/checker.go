// Package bindingcheck verifies the drafted business-intent source is bound
// to the compiled intent registry and the generated SchemaFlux model.
//
// This is deliberately a consumer-side checker: it reads the existing source,
// compiled definitions, model registry, and generated descriptors. It does
// not generate or publish any artifact.
package bindingcheck

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	compiled "github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	sfx "github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux"
	generated "github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux/generated"
)

// Row is the check result for one drafted definition.
type Row struct {
	Definition      string
	SourceRef       string
	InputSchema     string
	ResultSchema    string
	AggregateRoots  int
	ReadProperties  int
	WriteProperties int
	Gaps            []string
}

// Report is the deterministic MSRC-009 coverage report. Total is intentionally
// the drafted source count; no candidate-vocabulary denominator is introduced.
type Report struct {
	Rows  []Row
	Gaps  []string
	Valid bool
}

func (r Report) Summary() string {
	if len(r.Gaps) == 0 {
		return fmt.Sprintf("%d/%d BOUND", len(r.Rows), len(r.Rows))
	}
	return fmt.Sprintf("%d/%d BOUND\n  gap: %s", len(r.Rows)-len(r.Gaps), len(r.Rows), strings.Join(r.Gaps, "\n  gap: "))
}

// Check validates the source catalog against all existing semantic owners.
func Check(catalog *sfx.Catalog) Report {
	r := Report{}
	if catalog == nil {
		r.Gaps = []string{"catalog: nil"}
		return r
	}
	if mismatches, _ := sfx.CrossCheckCompiled(catalog); len(mismatches) > 0 {
		r.Gaps = append(r.Gaps, mismatches...)
	}
	registry, err := compiled.NewRegistry()
	if err != nil {
		r.Gaps = append(r.Gaps, "compiled registry: "+err.Error())
		return r
	}
	coverage := compiled.Coverage(registry)
	for _, g := range coverage.Gaps {
		r.Gaps = append(r.Gaps, g.String())
	}
	modelRegistry, err := model.Catalog()
	if err != nil {
		r.Gaps = append(r.Gaps, "model registry: "+err.Error())
	} else if report := model.CheckModelCoverage(modelRegistry); !report.FullyVerified() {
		for _, g := range report.Gaps {
			r.Gaps = append(r.Gaps, "model: "+g.String())
		}
	}
	if err := generated.ValidateRegistry(); err != nil {
		r.Gaps = append(r.Gaps, "generated registry: "+err.Error())
	}
	// Every model property and entity used by the bindings must also exist in
	// the generated descriptor set; this catches stale generated models while
	// leaving the generated package itself untouched.
	if modelRegistry != nil {
		for _, e := range modelRegistry.Entities() {
			ge, ok := generated.Entity(e.Ref.String())
			if !ok {
				r.Gaps = append(r.Gaps, "generated model: missing entity "+e.Ref.String())
				continue
			}
			for _, p := range modelRegistry.Properties() {
				if p.Entity != e.Ref {
					continue
				}
				found := false
				for _, gp := range ge.Properties {
					if gp.Ref == string(p.Ref) {
						found = true
						break
					}
				}
				if !found {
					r.Gaps = append(r.Gaps, "generated model: missing property "+string(p.Ref))
				}
			}
		}
	}
	byID := map[string]intent.Definition{}
	for _, d := range compiled.All() {
		byID[d.Ref.TypeID] = d
	}
	for _, d := range catalog.Definitions {
		row := Row{Definition: d.Ref(), SourceRef: d.Ref(), InputSchema: d.InputSchemaRef, ResultSchema: d.ResultSchemaRef}
		if cd, ok := byID[d.IntentTypeID]; ok {
			row.SourceRef = cd.Ref.String()
		}
		for _, b := range compiled.Bindings() {
			if b.Definition.TypeID == d.IntentTypeID && b.Definition.Version == d.Version {
				row.AggregateRoots, row.ReadProperties, row.WriteProperties = len(b.AggregateRoots), len(b.ReadProperties), len(b.WriteProperties)
				break
			}
		}
		r.Rows = append(r.Rows, row)
	}
	sort.Slice(r.Rows, func(i, j int) bool { return r.Rows[i].Definition < r.Rows[j].Definition })
	sort.Strings(r.Gaps)
	r.Valid = len(r.Rows) == 14 && len(r.Gaps) == 0
	return r
}
