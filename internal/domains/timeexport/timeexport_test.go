package timeexport

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func approvedLines() []ApprovedLine {
	return []ApprovedLine{
		{WorkerRef: "worker-1", Date: "2026-09-01", Minutes: 480, Project: "proj-a", CostCode: "CC-1", RateCode: "STD", PayCode: "REG", RevisionDigest: "rev-1"},
		{WorkerRef: "worker-1", Date: "2026-09-02", Minutes: 540, Project: "proj-a", CostCode: "CC-1", RateCode: "STD", PayCode: "OT", RevisionDigest: "rev-2"},
	}
}

func allowances() []Allowance {
	return []Allowance{{Code: "MEAL", Amount: "12.50", Currency: "USD"}}
}

func buildCard(t *testing.T) TimeCard {
	t.Helper()
	c, err := BuildTimeCard(BuildTimeCardRequest{
		WorkerRef: "worker-1", PeriodStart: "2026-09-01", PeriodEnd: "2026-09-15",
		Revision: "rev-period-1", Lines: approvedLines(), Allowances: allowances(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestTodo_TCLOCK_015 is the primary proof: an approved timecard exports as
// HR Open TimeCard JSON with worker, period, intervals (pay code and job
// allocation) and allowances, pinned to the approved revision, and imports
// back to the same content.
func TestTodo_TCLOCK_015(t *testing.T) {
	card := buildCard(t)
	raw, err := Export(card)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"worker-1", "2026-09-01", "REG", "OT", "proj-a", "MEAL", "12.50", "rev-period-1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("export missing %q: %s", want, text)
		}
	}
	imported, report, err := Import(raw)
	if err != nil {
		t.Fatal(err)
	}
	if report.HasUnmapped() {
		t.Fatalf("well-formed export flagged as having unmapped elements: %#v", report)
	}
	if imported.Revision != card.Revision || len(imported.Intervals) != 2 || len(imported.Allowances) != 1 {
		t.Fatalf("import did not round trip: %#v", imported)
	}
	if imported.Intervals[0].Project != "proj-a" || imported.Allowances[0].Amount != "12.50" {
		t.Fatal("import dropped a job-allocation or allowance field")
	}
}

// TestTodo_TCLOCK_015_Golden pins the exact exported bytes for a fixed input
// so a silent change in the wire shape is caught.
func TestTodo_TCLOCK_015_Golden(t *testing.T) {
	card := buildCard(t)
	got, err := Export(card)
	if err != nil {
		t.Fatal(err)
	}
	goldenPath := "testdata/golden_timecard.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("golden mismatch.\n got: %s\nwant: %s", got, want)
	}
}

// TestTodo_TCLOCK_015_Conformance proves the emitted export validates
// against the published subset schema authored in testdata (see
// testdata/timecard.schema.json, citing
// https://www.hropenstandards.org/standards), and that a malformed document
// (missing a required field) fails that same schema.
func TestTodo_TCLOCK_015_Conformance(t *testing.T) {
	schemaBytes, err := os.ReadFile("testdata/timecard.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	card := buildCard(t)
	raw, err := Export(card)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAgainstSchema(schemaBytes, raw); err != nil {
		t.Fatalf("valid export failed conformance: %v", err)
	}

	malformed := []byte(`{"workerId":"worker-1","period":{"start":"2026-09-01","end":"2026-09-15"},"intervals":[]}`)
	if err := ValidateAgainstSchema(schemaBytes, malformed); err == nil {
		t.Fatal("document missing required \"revision\" should fail conformance")
	}

	extraField := []byte(`{"workerId":"worker-1","period":{"start":"2026-09-01","end":"2026-09-15"},"revision":"r1","intervals":[],"unexpectedField":true}`)
	if err := ValidateAgainstSchema(schemaBytes, extraField); err == nil {
		t.Fatal("document with an unmapped top-level field should fail conformance")
	}
}

// TestTodo_TCLOCK_015_Security proves the RED cases: an export cannot
// include unapproved time (no revision digest), and a re-export after
// correction is applied idempotently rather than duplicating hours at the
// receiver.
func TestTodo_TCLOCK_015_Security(t *testing.T) {
	if _, err := BuildTimeCard(BuildTimeCardRequest{
		WorkerRef: "worker-1", PeriodStart: "2026-09-01", PeriodEnd: "2026-09-15", Revision: "rev-1",
		Lines: []ApprovedLine{{WorkerRef: "worker-1", Date: "2026-09-01", Minutes: 60, PayCode: "REG", RevisionDigest: ""}},
	}); !errors.Is(err, ErrRejected) {
		t.Fatalf("unapproved (no revision digest) line should be rejected, got %v", err)
	}

	base := buildCard(t)
	correction, err := BuildTimeCard(BuildTimeCardRequest{
		WorkerRef: base.WorkerRef, PeriodStart: base.PeriodStart, PeriodEnd: base.PeriodEnd,
		Revision: "rev-period-2", PreviousRevision: base.Revision,
		Lines: []ApprovedLine{
			{WorkerRef: "worker-1", Date: "2026-09-01", Minutes: 480, PayCode: "REG", RevisionDigest: "rev-1"},
			{WorkerRef: "worker-1", Date: "2026-09-02", Minutes: 600, PayCode: "OT", RevisionDigest: "rev-2-corrected"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	applied, err := ApplyCorrection(base, correction)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Revision != "rev-period-2" || applied.Intervals[1].Minutes != 600 {
		t.Fatalf("correction did not apply: %#v", applied)
	}
	// Idempotent replay: applying the exact same correction against the
	// state it already produced must not error and must not change anything.
	replayed, err := ApplyCorrection(applied, correction)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Revision != applied.Revision || len(replayed.Intervals) != len(applied.Intervals) {
		t.Fatal("idempotent correction replay changed the result")
	}

	// A correction naming the wrong previous revision (would duplicate hours
	// at the receiver if allowed) is rejected.
	badCorrection := correction
	badCorrection.PreviousRevision = "rev-does-not-exist"
	if _, err := ApplyCorrection(base, badCorrection); !errors.Is(err, ErrRejected) {
		t.Fatalf("correction with wrong previous revision should be rejected, got %v", err)
	}
}

// TestTodo_TCLOCK_015_UnmappedImport proves import reports (rather than
// silently drops) an allowance or job-allocation element it does not
// recognize.
func TestTodo_TCLOCK_015_UnmappedImport(t *testing.T) {
	raw := []byte(`{
		"workerId": "worker-1",
		"period": {"start": "2026-09-01", "end": "2026-09-15"},
		"revision": "rev-1",
		"extraTopLevel": "surprise",
		"intervals": [
			{"date": "2026-09-01", "minutes": 480, "payCode": "REG",
			 "jobAllocation": {"project": "p1", "costCode": "c1", "rateCode": "r1"},
			 "sourceRevision": "rev-1", "extraIntervalField": 42}
		],
		"allowances": [
			{"code": "MEAL", "amount": "12.50", "currency": "USD", "extraAllowanceField": "surprise"}
		]
	}`)
	_, report, err := Import(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !report.HasUnmapped() {
		t.Fatal("expected unmapped elements to be reported")
	}
	if len(report.TopLevel) != 1 || report.TopLevel[0] != "extraTopLevel" {
		t.Fatalf("top-level unmapped report wrong: %#v", report.TopLevel)
	}
	if fields, ok := report.Intervals[0]; !ok || len(fields) != 1 || fields[0] != "extraIntervalField" {
		t.Fatalf("interval unmapped report wrong: %#v", report.Intervals)
	}
	if fields, ok := report.Allowances[0]; !ok || len(fields) != 1 || fields[0] != "extraAllowanceField" {
		t.Fatalf("allowance unmapped report wrong: %#v", report.Allowances)
	}
}

func TestTodo_TCLOCK_015_Version(t *testing.T) {
	if Version() != 1 {
		t.Fatalf("unexpected contract version %d", Version())
	}
	card := buildCard(t)
	explain, err := card.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if explain.WorkerRef != "worker-1" || explain.IntervalCount != 2 || explain.AllowanceCount != 1 {
		t.Fatalf("explain mismatch: %#v", explain)
	}
}
