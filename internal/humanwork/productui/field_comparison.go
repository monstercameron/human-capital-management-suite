// Package productui presents current-versus-proposed field
// comparisons. The presenter takes already-formatted
// current and proposed values — formatting stays with the
// values' owners, so this never becomes a second authority
// — and verdicts them exactly: equal text is unchanged,
// anything else is changed with the catalog's change
// marker. Whitespace counts; two unreported values are
// unchanged rather than a change.
package productui

// FieldComparison is the presented comparison of one
// field's current value against its proposed value. Marker
// carries the catalog's change copy when Changed and stays
// empty otherwise.
type FieldComparison struct {
	Label    string
	Current  string
	Proposed string
	Changed  bool
	Marker   string
}

// FieldComparisonInput is one server-formatted current/proposed pair. The
// presenter deliberately accepts display values rather than raw domain
// values, so it cannot become a second formatting or authorization source.
type FieldComparisonInput struct {
	Label    string
	Current  string
	Proposed string
}

// FieldComparisonSet is an ordered review hierarchy. ChangedCount lets a
// compact review lead with materiality while Rows retain every field in the
// service-provided order; no row is silently dropped because it is unchanged.
type FieldComparisonSet struct {
	Rows         []FieldComparison
	ChangedCount int
}

// CompareFieldValues presents a complete current-versus-proposed review in
// input order. Inputs are copied into the output and are never mutated.
func CompareFieldValues(locale LocaleContext, fields []FieldComparisonInput) FieldComparisonSet {
	rows := make([]FieldComparison, 0, len(fields))
	changed := 0
	for _, field := range fields {
		row := CompareFieldValue(locale, field.Label, field.Current, field.Proposed)
		if row.Changed {
			changed++
		}
		rows = append(rows, row)
	}
	return FieldComparisonSet{Rows: rows, ChangedCount: changed}
}

// CompareFieldValue presents the comparison of one field's
// already-formatted current value against its proposed
// value. Comparison is exact.
func CompareFieldValue(locale LocaleContext, label, current, proposed string) FieldComparison {
	compared := FieldComparison{Label: label, Current: current, Proposed: proposed, Changed: current != proposed}
	if compared.Changed {
		compared.Marker = locale.Text("work.field_changed")
	}
	return compared
}
