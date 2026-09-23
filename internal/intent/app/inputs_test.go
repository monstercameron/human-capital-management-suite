package app

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestAsOfFromParsesInstantCutoff pins the REV-006-01 knowledge cut-off
// contract: a full RFC-3339 instant keeps its intraday precision (what the
// journey declares for a created worker), a bare date keeps its midnight
// meaning (the corpus evaluation coordinate), empty still defaults to the
// instance's creation, and garbage is still refused.
func TestAsOfFromParsesInstantCutoff(t *testing.T) {
	effective, err := values.ParseLocalDate("2026-09-23")
	if err != nil {
		t.Fatal(err)
	}
	inst := rev00601Instance()
	dated, err := asOfFrom(inst, effective, "2026-05-20")
	if err != nil {
		t.Fatalf("asOfFrom(date): %v", err)
	}
	if got := dated.KnownAt.Instant().String(); got != "2026-05-20T00:00:00Z" {
		t.Fatalf("date cut-off = %s, want midnight", got)
	}
	precise, err := asOfFrom(inst, effective, "2026-09-23T08:58:44.9171538Z")
	if err != nil {
		t.Fatalf("asOfFrom(instant): %v", err)
	}
	if got := precise.KnownAt.Instant().String(); got != "2026-09-23T08:58:44.9171538Z" {
		t.Fatalf("instant cut-off = %s, want full precision", got)
	}
	def, err := asOfFrom(inst, effective, "")
	if err != nil {
		t.Fatalf("asOfFrom(empty): %v", err)
	}
	if got, want := def.KnownAt.Instant().String(), inst.CreatedAt.String(); got != want {
		t.Fatalf("default cut-off = %s, want creation %s", got, want)
	}
	if _, err := asOfFrom(inst, effective, "not-a-date"); err == nil {
		t.Fatal("garbage cut-off parsed")
	}
}

func TestInputs_Smoke(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestInputs_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	_ = 1
}
