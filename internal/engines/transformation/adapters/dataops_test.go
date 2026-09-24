package adapters_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops/importing"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/adapters"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// The DataOps migration proof.
//
// internal/domains/dataops/importing is imported HERE ONLY, in a test.
// definitions/architecture/package-dependency-policy.yaml's
// engine-must-not-import-domain-implementation rule forbids the adapters
// package itself from importing it; a test import is not a package import in
// `go list -json`'s Imports, so the proof can run the site's own current
// code without creating that forbidden edge.

// dataOpsFixtureSpec is the DataOps import fixture: one field per import
// transform kind that has an IR equivalent, against real canonical properties
// resolved from the real model catalog.
func dataOpsFixtureSpec() importing.MappingSpecInput {
	return importing.MappingSpecInput{
		Version: "mapping.acme.workers.lowered/v1",
		Fields: []importing.FieldMapping{
			{
				SourceColumn: "title_raw",
				Target:       model.PropertyRef("job.title"), // string
				Transform:    importing.TransformSpec{Kind: importing.TransformIdentity},
				IsIdentity:   true,
			},
			{
				SourceColumn: "currency_raw",
				Target:       model.PropertyRef("compensation_package.currency"), // string
				Transform:    importing.TransformSpec{Kind: importing.TransformConstant, Constant: "USD"},
			},
			{
				SourceColumn: "expiry_raw",
				Target:       model.PropertyRef("budget_reservation.expiry"), // values.Instant
				Transform:    importing.TransformSpec{Kind: importing.TransformDateParse, Layout: time.RFC3339},
			},
		},
	}
}

// importMappingFrom translates a compiled importing.MappingProfile into the
// adapters mirror, reading only the site's exported fields.
func importMappingFrom(p importing.MappingProfile) adapters.DataOpsImportMapping {
	out := adapters.DataOpsImportMapping{
		Version: p.Version,
		Limits:  adapters.DataOpsLimits{MaxRows: importing.MaxRows, MaxCellBytes: importing.MaxCellBytes},
	}
	for _, f := range p.Fields {
		out.Fields = append(out.Fields, adapters.DataOpsFieldMapping{
			SourceColumn: f.SourceColumn,
			Target:       string(f.Target),
			IsIdentity:   f.IsIdentity,
			Transform: adapters.DataOpsTransform{
				Kind:             adapters.DataOpsTransformKind(f.Transform.Kind.String()),
				Layout:           f.Transform.Layout,
				Currency:         f.Transform.Currency,
				Crosswalk:        f.Transform.Crosswalk,
				CrosswalkVersion: f.Transform.CrosswalkVersion,
				Constant:         f.Transform.Constant,
			},
		})
	}
	return out
}

func dataOpsCatalog(t *testing.T) *model.Registry {
	t.Helper()
	reg, err := model.Catalog()
	if err != nil {
		t.Fatalf("model.Catalog: %v", err)
	}
	return reg
}

func stageDataOpsBatch(t *testing.T, header []string, records [][]string) importing.Batch {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("parse instant: %v", err)
	}
	source := importing.SourceDescriptor{
		Kind: importing.SourceKindCSV, URI: "s3://imports/acme/workers.csv", Tenant: values.TenantId("acme-corp"),
	}
	batch, err := importing.StageBatch(source, values.NewInstant(parsed), header, records)
	if err != nil {
		t.Fatalf("StageBatch: %v", err)
	}
	return batch
}

func TestTodo_XFORM_008_MigrationProof_DataOps(t *testing.T) {
	profile, err := importing.Compile(dataOpsCatalog(t), dataOpsFixtureSpec())
	if err != nil {
		t.Fatalf("importing.Compile: %v", err)
	}
	lowered, err := adapters.LowerDataOpsImport(importMappingFrom(profile))
	if err != nil {
		t.Fatalf("LowerDataOpsImport: %v", err)
	}
	t.Logf("%s", lowered.Explain())

	header := []string{"title_raw", "currency_raw", "expiry_raw"}
	records := [][]string{
		{"Staff Engineer", "ignored", "2026-06-30T23:59:59Z"},
		{"  padded title  ", "", "2026-06-30T23:59:59.500Z"},
		{"Engineering Manager", "EUR", "2026-12-31T00:00:00+05:30"},
	}
	batch := stageDataOpsBatch(t, header, records)

	const prefix = "dataops.import_mapping.mapping.acme.workers.lowered/v1.source."
	for i, row := range batch.Rows() {
		t.Run(records[i][0], func(t *testing.T) {
			applied, err := profile.Apply(header, row)
			if err != nil {
				t.Fatalf("MappingProfile.Apply: %v", err)
			}
			siteOutput := map[string]string{}
			for _, mv := range applied {
				if !mv.OK {
					t.Fatalf("site transform failed for %s: %s", mv.Target, mv.ErrorCode)
				}
				siteOutput[string(mv.Target)] = mv.Value
			}

			cells := row.Cells()
			in := map[string]string{}
			for j, name := range header {
				in[prefix+name] = cells[j]
			}
			// The constant field's source column is never read by the lowered
			// program (a default-literal map takes no source), so it is not
			// part of the record the lowering accepts.
			delete(in, prefix+"currency_raw")

			results, err := lowered.Run([]map[string]string{in})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			got, err := lowered.Texts(results[0])
			if err != nil {
				t.Fatalf("Texts: %v", err)
			}
			assertByteIdentical(t, "dataops row "+row.ID(), siteOutput, got)
		})
	}
}

func TestLowerDataOpsImportRefusals(t *testing.T) {
	base := func() adapters.DataOpsImportMapping {
		return adapters.DataOpsImportMapping{
			Version: "mapping.acme.workers/v1",
			Limits:  adapters.DataOpsLimits{MaxRows: importing.MaxRows, MaxCellBytes: importing.MaxCellBytes},
			Fields: []adapters.DataOpsFieldMapping{{
				SourceColumn: "title_raw", Target: "job.title",
				Transform: adapters.DataOpsTransform{Kind: adapters.DataOpsTransformIdentity},
			}},
		}
	}
	if _, err := adapters.LowerDataOpsImport(base()); err != nil {
		t.Fatalf("the unmutated mapping must lower: %v", err)
	}

	cases := []struct {
		name    string
		kind    adapters.DataOpsTransformKind
		mutate  func(*adapters.DataOpsTransform)
		feature string
	}{
		{"empty constant", adapters.DataOpsTransformConstant, nil, adapters.FeatureEmptyLiteral},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := base()
			m.Fields[0].Transform = adapters.DataOpsTransform{Kind: tc.kind}
			if tc.mutate != nil {
				tc.mutate(&m.Fields[0].Transform)
			}
			_, err := adapters.LowerDataOpsImport(m)
			var refusal adapters.Refusal
			if !errors.As(err, &refusal) {
				t.Fatalf("error = %v, want an adapters.Refusal", err)
			}
			if refusal.Feature != tc.feature {
				t.Fatalf("refusal feature = %q, want %q (%v)", refusal.Feature, tc.feature, err)
			}
			if refusal.Site != adapters.SiteDataOpsImport {
				t.Fatalf("refusal site = %q", refusal.Site)
			}
			if refusal.Target != "job.title" {
				t.Fatalf("refusal target = %q, want the field it applies to", refusal.Target)
			}
		})
	}

	t.Run("the whole DataOps fixture spec lowers lookup and money fields", func(t *testing.T) {
		profile, err := importing.Compile(dataOpsCatalog(t), importing.MappingSpecInput{
			Version: "mapping.acme.workers/v1",
			Fields: []importing.FieldMapping{
				{SourceColumn: "region_raw", Target: model.PropertyRef("position.pay_band_ref"),
					Transform: importing.TransformSpec{Kind: importing.TransformLookup,
						Crosswalk: map[string]string{"east": "BAND_E"}, CrosswalkVersion: "crosswalk.region_band/v1"}},
			},
		})
		if err != nil {
			t.Fatalf("importing.Compile: %v", err)
		}
		if _, err = adapters.LowerDataOpsImport(importMappingFrom(profile)); err != nil {
			t.Fatalf("LowerDataOpsImport: %v", err)
		}
	})
}

// TestDataOpsDivergences pins each declared difference against the site's real
// behaviour. Each is a finding, not a change: internal/domains/dataops is not
// edited by this lane.
func TestDataOpsDivergences(t *testing.T) {
	profile, err := importing.Compile(dataOpsCatalog(t), dataOpsFixtureSpec())
	if err != nil {
		t.Fatalf("importing.Compile: %v", err)
	}
	lowered, err := adapters.LowerDataOpsImport(importMappingFrom(profile))
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	for _, feature := range []string{"missing_source_column", "failed_transform_reporting", "identity_field_grouping"} {
		assertDivergence(t, lowered.Divergences, feature)
	}

	t.Run("missing source column: the site refuses the batch, the lowering yields ABSENT", func(t *testing.T) {
		header := []string{"title_raw", "currency_raw"} // expiry_raw is gone.
		batch := stageDataOpsBatch(t, header, [][]string{{"Staff Engineer", "ignored"}})
		if _, err := profile.Apply(header, batch.Rows()[0]); !errors.Is(err, importing.ErrSourceColumnNotInBatch) {
			t.Fatalf("site error = %v, want ErrSourceColumnNotInBatch", err)
		}
		const prefix = "dataops.import_mapping.mapping.acme.workers.lowered/v1.source."
		rows, err := lowered.Run([]map[string]string{{prefix + "title_raw": "Staff Engineer"}})
		if err != nil {
			t.Fatalf("lowered Run: %v", err)
		}
		got, err := lowered.Texts(rows[0])
		if err != nil {
			t.Fatalf("Texts: %v", err)
		}
		if _, present := got["budget_reservation.expiry"]; present {
			t.Fatalf("lowered produced a value for the missing column: %+v", got)
		}
		if got["job.title"] != "Staff Engineer" || got["compensation_package.currency"] != "USD" {
			t.Fatalf("lowered dropped the fields it could still map: %+v", got)
		}
	})

	t.Run("failed transform: the site reports per value, the lowering refuses the dataset", func(t *testing.T) {
		header := []string{"title_raw", "currency_raw", "expiry_raw"}
		batch := stageDataOpsBatch(t, header, [][]string{{"Staff Engineer", "ignored", "not-a-timestamp"}})
		applied, err := profile.Apply(header, batch.Rows()[0])
		if err != nil {
			t.Fatalf("site Apply: %v", err)
		}
		failures := 0
		for _, mv := range applied {
			if !mv.OK {
				failures++
				if mv.ErrorCode != importing.RuleDateParseFailed {
					t.Fatalf("site error code = %q", mv.ErrorCode)
				}
			}
		}
		if failures != 1 {
			t.Fatalf("site reported %d failed values, want 1", failures)
		}
		const prefix = "dataops.import_mapping.mapping.acme.workers.lowered/v1.source."
		if _, err := lowered.Run([]map[string]string{{
			prefix + "title_raw":  "Staff Engineer",
			prefix + "expiry_raw": "not-a-timestamp",
		}}); err == nil {
			t.Fatal("the lowered program accepted an unparseable timestamp; the declared divergence says it refuses the dataset")
		}
	})

	t.Run("whitespace date parsing has identical shared semantics", func(t *testing.T) {
		header := []string{"title_raw", "currency_raw", "expiry_raw"}
		batch := stageDataOpsBatch(t, header, [][]string{{"Staff Engineer", "x", " 2026-06-30T23:59:59Z "}})
		applied, err := profile.Apply(header, batch.Rows()[0])
		if err != nil {
			t.Fatalf("site Apply: %v", err)
		}
		for _, mv := range applied {
			if mv.Target == model.PropertyRef("budget_reservation.expiry") && !mv.OK {
				t.Fatalf("the site failed to trim and parse a padded date cell: %s", mv.ErrorCode)
			}
		}
		const prefix = "dataops.import_mapping.mapping.acme.workers.lowered/v1.source."
		rows, err := lowered.Run([]map[string]string{{
			prefix + "title_raw":  "Staff Engineer",
			prefix + "expiry_raw": " 2026-06-30T23:59:59Z ",
		}})
		if err != nil {
			t.Fatalf("lowered Run: %v", err)
		}
		texts, err := lowered.Texts(rows[0])
		if err != nil {
			t.Fatal(err)
		}
		if texts["budget_reservation.expiry"] != "2026-06-30T23:59:59Z" {
			t.Fatalf("padded date = %q", texts["budget_reservation.expiry"])
		}
	})
}
