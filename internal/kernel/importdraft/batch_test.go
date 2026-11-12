package importdraft

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func instant(t *testing.T) values.Instant {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("parse instant: %v", err)
	}
	return values.NewInstant(parsed)
}

func source(t *testing.T) SourceDescriptor {
	t.Helper()
	s := SourceDescriptor{Kind: SourceKindCSV, URI: "upload://people.csv", Tenant: values.TenantId("acme-corp")}
	if err := s.Validate(); err != nil {
		t.Fatalf("source descriptor: %v", err)
	}
	return s
}

func TestStageCSV_StagesRowsInOrderWithStableDigest(t *testing.T) {
	csv := "id,name\n1,ada\n2,grace\n"
	b, err := StageCSV(source(t), instant(t), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("StageCSV: %v", err)
	}
	if got := b.RowCount(); got != 2 {
		t.Fatalf("RowCount = %d, want 2", got)
	}
	if h := b.Header(); len(h) != 2 || h[0] != "id" || h[1] != "name" {
		t.Fatalf("Header = %v", h)
	}
	r0, ok := b.Row(0)
	if !ok {
		t.Fatal("Row(0) missing")
	}
	if cells := r0.Cells(); cells[1] != "ada" {
		t.Fatalf("row 0 cells = %v", cells)
	}
	if _, ok := b.Row(2); ok {
		t.Fatal("Row(2) must be out of range for a two-row batch")
	}
	if idx, ok := b.ColumnIndex("name"); !ok || idx != 1 {
		t.Fatalf("ColumnIndex(name) = %d, %v", idx, ok)
	}
	if _, ok := b.ColumnIndex("missing"); ok {
		t.Fatal("ColumnIndex must refuse an unknown column")
	}

	again, err := StageCSV(source(t), instant(t), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("second StageCSV: %v", err)
	}
	if again.Digest() != b.Digest() {
		t.Fatalf("identical bytes staged to different digests: %s vs %s", again.Digest(), b.Digest())
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("staged batch failed its own validation: %v", err)
	}
}

func TestStageBatch_RowIdentityIgnoresPositionButDigestDoesNot(t *testing.T) {
	header := []string{"id", "name"}
	forward, err := StageBatch(source(t), instant(t), header, [][]string{{"1", "ada"}, {"2", "grace"}})
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	reversed, err := StageBatch(source(t), instant(t), header, [][]string{{"2", "grace"}, {"1", "ada"}})
	if err != nil {
		t.Fatalf("reversed: %v", err)
	}
	fwd, _ := forward.Row(0)
	rev, _ := reversed.Row(1)
	if fwd.ID() != rev.ID() {
		t.Fatalf("the same row content gained a different identity after reordering: %s vs %s", fwd.ID(), rev.ID())
	}
	if forward.Digest() == reversed.Digest() {
		t.Fatal("reordering rows must change the batch digest, which is order-sensitive")
	}
}

func TestStageBatch_RejectsMalformedInput(t *testing.T) {
	good := source(t)
	cases := []struct {
		name string
		run  func() error
		want error
	}{
		{"unset retrieved-at", func() error {
			_, err := StageBatch(good, values.Instant{}, []string{"a"}, nil)
			return err
		}, ErrRetrievedAtUnset},
		{"duplicate column", func() error {
			_, err := StageBatch(good, instant(t), []string{"a", "a"}, nil)
			return err
		}, ErrDuplicateColumn},
		{"ragged row", func() error {
			_, err := StageBatch(good, instant(t), []string{"a", "b"}, [][]string{{"1"}})
			return err
		}, ErrRowWidthMismatch},
		{"invalid source", func() error {
			_, err := StageBatch(SourceDescriptor{}, instant(t), []string{"a"}, nil)
			return err
		}, ErrSourceInvalid},
		{"invalid utf-8 cell", func() error {
			_, err := StageBatch(good, instant(t), []string{"a"}, [][]string{{"\xff"}})
			return err
		}, ErrCellEncoding},
	}
	for _, c := range cases {
		if err := c.run(); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want errors.Is %v", c.name, err, c.want)
		}
	}
}

// endlessCSV never ends; a staging path that buffers before bounding would
// never return.
type endlessCSV struct{ served int }

func (r *endlessCSV) Read(p []byte) (int, error) {
	line := []byte(strings.Repeat("a", 200) + "\n")
	n := copy(p, line)
	r.served += n
	return n, nil
}

func TestStageCSV_RefusesUnboundedInputWithoutBuffering(t *testing.T) {
	r := &endlessCSV{}
	_, err := StageCSV(source(t), instant(t), r)
	if !errors.Is(err, ErrSourceTooLarge) {
		t.Fatalf("err = %v, want ErrSourceTooLarge", err)
	}
	if r.served > MaxSourceCSVBytes+4096 {
		t.Fatalf("read %d bytes before refusing; the budget is %d", r.served, MaxSourceCSVBytes)
	}
}

func TestSourceKind_WireAndValidity(t *testing.T) {
	if !SourceKindCSV.Valid() || !SourceKindRecords.Valid() {
		t.Fatal("CSV and records must be legal source kinds")
	}
	if SourceKindUnspecified.Valid() {
		t.Fatal("the zero source kind must never be legal")
	}
	if SourceKindCSV.String() == SourceKindRecords.String() {
		t.Fatal("distinct source kinds must have distinct wire tokens")
	}
}
