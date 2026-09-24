package adapters

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation"
)

var adapterDateLayouts = map[string]bool{
	"2006-01-02": true, "2006-01-02T15:04:05Z07:00": true,
	"2006/01/02": true, "01/02/2006": true, "20060102": true,
}

// This file mirrors the DataOps import mapping form.
//
// Mirrored, field for field, from internal/domains/dataops/importing:
//
//	importing.MappingSpecInput /
//	importing.MappingProfile  -> DataOpsImportMapping (Version, Fields)
//	importing.FieldMapping    -> DataOpsFieldMapping (SourceColumn, Target,
//	                             Transform, IsIdentity)
//	importing.TransformSpec   -> DataOpsTransform (Kind, Layout, Currency,
//	                             Crosswalk, CrosswalkVersion, Constant)
//	importing.MaxRows,
//	importing.MaxCellBytes    -> DataOpsLimits
//
// Deliberately NOT mirrored:
//
//	MappingProfile.Reads, .Writes, .Diagnostics, .Digest
//	              - derived views and the profile's own content identity,
//	                recomputed by the lowering from the fields themselves.
//	FieldMapping.Target's registry resolution (model.PropertyDefinition.GoType
//	                and its TargetClass)
//	              - the canonical property registry is a domain artifact an
//	                engine may not import. The lowering therefore takes the
//	                target's name and its transform's own output class, which
//	                is what the site's compiler already proved agree.

// DataOpsTransformKind mirrors importing.TransformKind by its stable wire
// token rather than its numeric value, so a lowering names the kind the way
// the site's own diagnostics name it.
type DataOpsTransformKind string

// The DataOps import transform kinds, spelled as importing.TransformKind
// spells them.
const (
	DataOpsTransformIdentity   DataOpsTransformKind = "IDENTITY"
	DataOpsTransformTrim       DataOpsTransformKind = "TRIM"
	DataOpsTransformCaseUpper  DataOpsTransformKind = "CASE_UPPER"
	DataOpsTransformCaseLower  DataOpsTransformKind = "CASE_LOWER"
	DataOpsTransformDateParse  DataOpsTransformKind = "DATE_PARSE"
	DataOpsTransformMoneyParse DataOpsTransformKind = "MONEY_PARSE"
	DataOpsTransformLookup     DataOpsTransformKind = "LOOKUP"
	DataOpsTransformConstant   DataOpsTransformKind = "CONSTANT"
)

// DataOpsTransform mirrors importing.TransformSpec.
type DataOpsTransform struct {
	Kind             DataOpsTransformKind
	Layout           string
	Currency         string
	Crosswalk        map[string]string
	CrosswalkVersion string
	Constant         string
}

// DataOpsFieldMapping mirrors importing.FieldMapping.
type DataOpsFieldMapping struct {
	SourceColumn string
	Target       string
	Transform    DataOpsTransform
	IsIdentity   bool
}

// DataOpsLimits mirrors the bounds the importing package declares as
// package constants.
type DataOpsLimits struct {
	MaxRows      int
	MaxCellBytes int
}

// DataOpsImportMapping mirrors a compiled importing.MappingProfile.
type DataOpsImportMapping struct {
	Version string
	Fields  []DataOpsFieldMapping
	Limits  DataOpsLimits
}

// LowerDataOpsImport lowers a DataOps import mapping onto the shared engine.
//
// Every declared DataOps transform is lowered to a closed map function in the
// shared transformation IR. The output is carried as canonical text so the
// shared executor preserves the site's exact strings.
func LowerDataOpsImport(m DataOpsImportMapping) (Lowered, error) {
	site := SiteDataOpsImport
	if m.Version == "" {
		return Lowered{}, refuse(site, FeatureInvalidMapping, "", "import mapping declares no version")
	}
	if len(m.Fields) == 0 {
		return Lowered{}, refuse(site, FeatureInvalidMapping, m.Version, "import mapping declares no fields")
	}
	name := "dataops.import_mapping." + m.Version
	b, err := newBuilder(site, name, name+".source", name+".target")
	if err != nil {
		return Lowered{}, err
	}

	sawIdentityField := false
	for _, f := range m.Fields {
		if f.Target == "" {
			return Lowered{}, refuse(site, FeatureInvalidMapping, f.SourceColumn, "field mapping declares no target property")
		}
		if f.SourceColumn == "" {
			return Lowered{}, refuse(site, FeatureInvalidMapping, f.Target, "field mapping declares no source column")
		}
		if f.IsIdentity {
			sawIdentityField = true
		}
		switch f.Transform.Kind {
		case DataOpsTransformIdentity:
			if err := b.project(f.Target, f.SourceColumn, transformation.TypeString); err != nil {
				return Lowered{}, err
			}
		case DataOpsTransformConstant:
			if err := b.literal(f.Target, f.Transform.Constant, transformation.TypeString); err != nil {
				return Lowered{}, err
			}
		case DataOpsTransformDateParse:
			if !adapterDateLayouts[f.Transform.Layout] {
				return Lowered{}, refuse(site, FeatureLayoutDateParse, f.Target, "date layout %q is outside the site's closed set", f.Transform.Layout)
			}
			if err := b.transform(f.Target, f.SourceColumn, "date_parse", f.Transform.Layout, nil); err != nil {
				return Lowered{}, err
			}
		case DataOpsTransformTrim:
			if err := b.transform(f.Target, f.SourceColumn, "trim", "", nil); err != nil {
				return Lowered{}, err
			}
		case DataOpsTransformCaseUpper:
			if err := b.transform(f.Target, f.SourceColumn, "upper", "", nil); err != nil {
				return Lowered{}, err
			}
		case DataOpsTransformCaseLower:
			if err := b.transform(f.Target, f.SourceColumn, "lower", "", nil); err != nil {
				return Lowered{}, err
			}
		case DataOpsTransformLookup:
			if err := b.transform(f.Target, f.SourceColumn, "lookup", "", f.Transform.Crosswalk); err != nil {
				return Lowered{}, err
			}
		case DataOpsTransformMoneyParse:
			if err := b.transform(f.Target, f.SourceColumn, "money_parse", "DATAOPS:"+f.Transform.Currency, nil); err != nil {
				return Lowered{}, err
			}
		default:
			return Lowered{}, refuse(site, FeatureInvalidMapping, f.Target,
				"transform kind %q is not a declared DataOps import transform", string(f.Transform.Kind))
		}
	}

	b.setLimits(dataOpsLimits(m.Limits, len(m.Fields)))
	b.diverge(Divergence{
		Feature: "missing_source_column",
		Vector:  "a mapping that reads a column the staged batch header does not carry",
		Site:    "returns ErrSourceColumnNotInBatch and applies nothing, for the whole batch",
		Lowered: "yields an ABSENT property for that target and completes the row",
	})
	b.diverge(Divergence{
		Feature: "failed_transform_reporting",
		Vector:  "a transform that cannot be applied to a cell",
		Site:    "returns a MappedValue with OK=false and a stable rule identity the validator turns into a ValidationError",
		Lowered: "returns a typed exec Refusal for the whole dataset, naming the breaching row and instruction",
	})
	if sawIdentityField {
		b.diverge(Divergence{
			Feature: "identity_field_grouping",
			Vector:  "a mapping marking one or more fields IsIdentity",
			Site:    "groups rows by the concatenation of every IsIdentity field's mapped value to detect duplicates",
			Lowered: "carries no row-identity notion; duplicate detection stays with the site's validator",
		})
	}
	return b.finish()
}

// dataOpsLimits is the declared limits mapping for a DataOps import mapping.
// The dataset bound is the importing package's own MaxRows; the byte bound
// is its own MaxCellBytes multiplied by the mapping's field count, which is
// the largest row the mapping can read or write.
func dataOpsLimits(l DataOpsLimits, fields int) LimitsMapping {
	notes := []string{
		fmt.Sprintf("max_operations=%d: one instruction per compiled field mapping", fields),
		"max_expansion=1: every import field mapping reads at most one source column",
	}
	rows := l.MaxRows
	if rows <= 0 {
		rows = DefaultMaxRows
		notes = append(notes, "exec max_rows: the mapping declared none; adapters default applied")
	} else {
		notes = append(notes, "exec max_rows: importing.MaxRows, verbatim")
	}
	cell := int64(l.MaxCellBytes)
	if cell <= 0 {
		cell = DefaultMaxBytes
		notes = append(notes, "max_input_bytes/max_output_bytes: the mapping declared no cell bound; adapters default applied")
	} else {
		notes = append(notes, "max_input_bytes/max_output_bytes: importing.MaxCellBytes x the mapping's field count")
		cell *= int64(fields)
	}
	return LimitsMapping{
		Definition: transformation.ResourceLimits{
			MaxOperations:  fields,
			MaxInputBytes:  cell,
			MaxOutputBytes: cell,
			MaxExpansion:   1,
		},
		Execution: execLimits{MaxRows: rows, MaxSteps: fields * rows, MaxOutputBytes: cell * int64(rows)},
		Notes:     notes,
	}
}
