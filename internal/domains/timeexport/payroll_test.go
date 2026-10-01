package timeexport

import (
	"errors"
	"strings"
	"testing"
)

// TestTodo_TCLOCK_015_Payroll proves the generic payroll flat exports (ADP-
// style CSV, Paychex-style CSV and QuickBooks Desktop IIF timer activities)
// build from the same TimeCard read model as the HR Open export, with the
// column mapping expressed as data rather than one hand-written exporter
// per provider.
func TestTodo_TCLOCK_015_Payroll(t *testing.T) {
	card := buildCard(t)

	adp, err := ExportFlatCSV(card, ADPColumns)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(adp, "EmployeeID") || !strings.Contains(adp, "worker-1") || !strings.Contains(adp, "REG") {
		t.Fatalf("ADP export missing expected columns: %s", adp)
	}
	// 480 minutes = 8.0000 hours.
	if !strings.Contains(adp, "8.0000") {
		t.Fatalf("ADP export did not render minutes as decimal hours: %s", adp)
	}

	paychex, err := ExportFlatCSV(card, PaychexColumns)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(paychex, "EmployeeNumber") || !strings.Contains(paychex, "HoursWorked") {
		t.Fatalf("Paychex export missing expected columns: %s", paychex)
	}

	iif, err := ExportQuickBooksIIF(card, "customer-ironridge")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(iif, "!TIMERHDR") || !strings.Contains(iif, "!TIMERENTRY") || !strings.Contains(iif, "customer-ironridge") {
		t.Fatalf("IIF export missing header or customer: %s", iif)
	}
	if !strings.Contains(iif, "09/01/2026") || !strings.Contains(iif, "8:00") {
		t.Fatalf("IIF export did not render date/duration in IIF format: %s", iif)
	}
}

// TestTodo_TCLOCK_015_Payroll_Rejections proves a generic mapping engine
// still fails closed: an unknown column field and a missing customer name
// are rejected rather than silently producing a malformed file.
func TestTodo_TCLOCK_015_Payroll_Rejections(t *testing.T) {
	card := buildCard(t)
	if _, err := ExportFlatCSV(card, []ColumnMapping{{Header: "X", Field: ColumnField("NOT_A_FIELD")}}); !errors.Is(err, ErrRejected) {
		t.Fatalf("unknown column field should be rejected, got %v", err)
	}
	if _, err := ExportFlatCSV(card, nil); !errors.Is(err, ErrRejected) {
		t.Fatalf("empty mapping should be rejected, got %v", err)
	}
	if _, err := ExportQuickBooksIIF(card, ""); !errors.Is(err, ErrRejected) {
		t.Fatalf("missing customer should be rejected, got %v", err)
	}
}

// TestTodo_TCLOCK_015_Schema proves an authored schema that uses an
// unimplemented keyword is rejected loudly by the validator rather than
// silently under-validating.
func TestTodo_TCLOCK_015_Schema(t *testing.T) {
	badSchema := []byte(`{"type":"object","patternProperties":{}}`)
	if err := ValidateAgainstSchema(badSchema, []byte(`{}`)); err == nil {
		t.Fatal("schema using an unimplemented keyword should fail loudly")
	}
	if err := ValidateAgainstSchema([]byte(`not json`), []byte(`{}`)); err == nil {
		t.Fatal("malformed schema should fail")
	}
	if err := ValidateAgainstSchema([]byte(`{"type":"object"}`), []byte(`not json`)); err == nil {
		t.Fatal("malformed data should fail")
	}
	if err := ValidateAgainstSchema([]byte(`{"type":"integer"}`), []byte(`"a string"`)); err == nil {
		t.Fatal("wrong type should fail")
	}
	if err := ValidateAgainstSchema([]byte(`{"type":"array","items":{"type":"string"}}`), []byte(`[1,2]`)); err == nil {
		t.Fatal("array item type mismatch should fail")
	}
}
