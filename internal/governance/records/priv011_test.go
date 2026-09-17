package records

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func priv011Cutoff() time.Time { return time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC) }

func fixtureClassifiedRules() []ClassifiedRetentionRule {
	return []ClassifiedRetentionRule{
		{
			Rule:                 RetentionRule{RecordSeries: "payroll-register", Jurisdiction: "US-CA", MinimumDays: 2555, AuthorityRef: "IRS-7y"},
			Class:                AuthorityTax,
			DispositionAuthority: "IRS-REV-PROC-7Y",
		},
		{
			Rule:                 RetentionRule{RecordSeries: "payroll-register", Jurisdiction: "US-CA", MinimumDays: 1825, AuthorityRef: "MSA-5y"},
			Class:                AuthorityContractual,
			DispositionAuthority: "TENANT-MSA-5Y",
		},
		{
			Rule:                 RetentionRule{RecordSeries: "payroll-register", Jurisdiction: "US-CA", MinimumDays: 3650, AuthorityRef: "CA-SOS-GOV-10y"},
			Class:                AuthorityPublicRecords,
			DispositionAuthority: "CA-SOS-SCHEDULE-10Y",
		},
	}
}

// TestTodo_PRIV_011 is the PRIMARY PRIV-011 contract test: the composed
// schedule carries a distinct public-records class, deletion stays blocked
// while any class applies, and the blocking authority is always named.
func TestTodo_PRIV_011(t *testing.T) {
	schedules, err := ComposeClassified(fixtureClassifiedRules())
	if err != nil {
		t.Fatalf("ComposeClassified: %v", err)
	}
	if len(schedules) != 1 {
		t.Fatalf("composed %d schedules, want one per series", len(schedules))
	}
	schedule := schedules[0]
	if len(schedule.ClassMinima) != 3 {
		t.Fatalf("schedule carries %d authority classes, want tax, contractual and public-records", len(schedule.ClassMinima))
	}

	t.Run("RED: one shared timer would delete early or block silently", func(t *testing.T) {
		// Cutoff 2018-01-01. Tax (7y) elapses 2025-01-01; public-records
		// (10y) holds until 2028-01-01. A single-timer evaluation at
		// 2026-06-01 sees the tax timer met and deletes a record the
		// government schedule still governs.
		asOf := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		_, err := EvaluateDisposition(schedule, priv011Cutoff(), asOf, nil)
		if !errors.Is(err, ErrDispositionBlocked) {
			t.Fatalf("public-records minimum must block at %s, got %v", asOf.Format("2006-01-02"), err)
		}
		if !strings.Contains(err.Error(), "PUBLIC_RECORDS:CA-SOS-SCHEDULE-10Y") {
			t.Fatalf("blocking authority is unnamed: %v", err)
		}
	})

	t.Run("GREEN: every class minimum gates with its named authority", func(t *testing.T) {
		// Cutoff 2018-01-01: at 2020-01-01 the tax (7y), contractual
		// (5y) and public-records (10y) minima are all unmet.
		asOf := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		verdict, err := EvaluateDisposition(schedule, priv011Cutoff(), asOf, nil)
		if !errors.Is(err, ErrDispositionBlocked) {
			t.Fatalf("unmet minima must block, got %+v / %v", verdict, err)
		}
		joined := strings.Join(verdict.BlockingAuthority, ",")
		for _, want := range []string{"TAX:", "CONTRACTUAL:", "PUBLIC_RECORDS:"} {
			if !strings.Contains(joined, want) {
				t.Fatalf("blocking authorities %q omit class %s", joined, want)
			}
		}
	})

	t.Run("GREEN: a privacy deletion blocked by a hold names its authority", func(t *testing.T) {
		asOf := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
		holds := []DispositionHold{{ID: "hold-1", Class: AuthorityPublicRecords, Authority: "CA-SOS-LITIGATION", Reason: "pending public-records litigation"}}
		verdict, err := EvaluateDisposition(schedule, priv011Cutoff(), asOf, holds)
		if !errors.Is(err, ErrDispositionBlocked) {
			t.Fatalf("held record must block, got %+v / %v", verdict, err)
		}
		if !strings.Contains(strings.Join(verdict.BlockingAuthority, ","), "CA-SOS-LITIGATION") {
			t.Fatalf("hold authority is unnamed: %+v", verdict)
		}
	})

	t.Run("GREEN: fully elapsed schedules release", func(t *testing.T) {
		asOf := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
		verdict, err := EvaluateDisposition(schedule, priv011Cutoff(), asOf, nil)
		if err != nil {
			t.Fatalf("EvaluateDisposition: %v", err)
		}
		if verdict.Status != Eligible || verdict.EligibleAt == nil {
			t.Fatalf("elapsed schedule must release: %+v", verdict)
		}
	})
}

// TestTodo_PRIV_011_Golden pins the classified schedule digest.
func TestTodo_PRIV_011_Golden(t *testing.T) {
	schedules, err := ComposeClassified(fixtureClassifiedRules())
	if err != nil {
		t.Fatal(err)
	}
	const golden = "sha256:a9a447fe32774764865178f7faf06829ff0f61599cf7ec5b12017edb3a7f05df"
	if schedules[0].Digest != golden {
		t.Fatalf("schedule digest drifted: got %s, want %s", schedules[0].Digest, golden)
	}
	again, err := ComposeClassified(fixtureClassifiedRules())
	if err != nil {
		t.Fatal(err)
	}
	if schedules[0].Digest != again[0].Digest {
		t.Fatal("identical rules compose different schedules")
	}
	t.Logf("schedule=%s", schedules[0].Digest)
}
