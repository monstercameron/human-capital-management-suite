package importdraft

import (
	"testing"
)

func TestValidationSeverity_WireTokens(t *testing.T) {
	cases := map[ValidationSeverity]string{
		ValidationSeverityError:   "ERROR",
		ValidationSeverityWarning: "WARNING",
		0:                         "SEVERITY_UNSPECIFIED",
		ValidationSeverity(9):     "SEVERITY_UNSPECIFIED",
	}
	for sev, want := range cases {
		if got := sev.String(); got != want {
			t.Errorf("severity %d renders %q, want %q", sev, got, want)
		}
	}
}

func TestValidationError_KeepsItsRowPropertyAndRule(t *testing.T) {
	e := ValidationError{Identity: "sha256:x", BatchID: "b", RowID: "row-1", Property: "job.title", RuleID: "presence.required_missing", Severity: ValidationSeverityError}
	if e.RowID != "row-1" || e.Property != "job.title" || e.Severity.String() != "ERROR" {
		t.Fatalf("validation error lost its identity fields: %+v", e)
	}
	batchLevel := ValidationError{RuleID: "mapping.column_unmapped", Severity: ValidationSeverityWarning}
	if batchLevel.RowID != "" || batchLevel.Severity.String() != "WARNING" {
		t.Fatalf("batch-level diagnostic must carry no row id: %+v", batchLevel)
	}
}

func TestDraftWrite_ZeroValueClaimsNoBaseline(t *testing.T) {
	var w DraftWrite
	if w.CurrentPresent {
		t.Fatal("the zero DraftWrite must not claim a present baseline value")
	}
	w.CurrentPresent = true
	if w.Current != "" {
		t.Fatal("presence and value must be independent fields")
	}
}
