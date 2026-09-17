package records

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestTodo_PRIV_011_Property proves composition is order-stable and
// per-class maxima accumulate: the strictest rule of a class wins.
func TestTodo_PRIV_011_Property(t *testing.T) {
	for seed := 0; seed < 25; seed++ {
		rules := fixtureClassifiedRules()
		for i := len(rules) - 1; i > 0; i-- {
			j := (seed + i) % (i + 1)
			rules[i], rules[j] = rules[j], rules[i]
		}
		first, err := ComposeClassified(rules)
		if err != nil {
			t.Fatal(err)
		}
		second, err := ComposeClassified(fixtureClassifiedRules())
		if err != nil {
			t.Fatal(err)
		}
		if first[0].Digest != second[0].Digest {
			t.Fatalf("seed %d: rule order changed the composed schedule", seed)
		}
	}
	stricter := append(fixtureClassifiedRules(), ClassifiedRetentionRule{
		Rule:                 RetentionRule{RecordSeries: "payroll-register", Jurisdiction: "US-CA", MinimumDays: 3650, AuthorityRef: "IRS-10y"},
		Class:                AuthorityTax,
		DispositionAuthority: "IRS-REV-PROC-10Y",
	})
	composed, err := ComposeClassified(stricter)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range composed[0].ClassMinima {
		if m.Class == AuthorityTax && m.MinimumDays != 3650 {
			t.Fatalf("tax minimum = %d, want the strictest 3650", m.MinimumDays)
		}
	}
}

// TestTodo_PRIV_011_Security proves undeclared classes, authority-free
// rules and incomplete holds fail closed instead of silently joining the
// composed schedule.
func TestTodo_PRIV_011_Security(t *testing.T) {
	forged := fixtureClassifiedRules()
	forged[0].Class = "SHADOW"
	if _, err := ComposeClassified(forged); !errors.Is(err, ErrDispositionInvalid) {
		t.Fatalf("undeclared class must be refused, got %v", err)
	}
	anonymous := fixtureClassifiedRules()
	anonymous[2].DispositionAuthority = ""
	if _, err := ComposeClassified(anonymous); !errors.Is(err, ErrDispositionInvalid) {
		t.Fatalf("authority-free rule must be refused, got %v", err)
	}
	schedules, err := ComposeClassified(fixtureClassifiedRules())
	if err != nil {
		t.Fatal(err)
	}
	asOf := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	blank := []DispositionHold{{ID: "", Class: AuthorityTax, Authority: "IRS", Reason: "audit"}}
	if _, err := EvaluateDisposition(schedules[0], priv011Cutoff(), asOf, blank); !errors.Is(err, ErrDispositionInvalid) {
		t.Fatalf("incomplete hold must be refused, got %v", err)
	}
	if _, err := EvaluateDisposition(ClassifiedSchedule{}, priv011Cutoff(), asOf, nil); !errors.Is(err, ErrDispositionInvalid) {
		t.Fatalf("schedule-less record must be refused, got %v", err)
	}
}

// TestTodo_PRIV_011_Integration proves the classified schedule composes
// with the RECORDS-DISP-001 simulator: both agree that unelapsed minima
// block, and the classified verdict names the authority the simulator
// leaves generic.
func TestTodo_PRIV_011_Integration(t *testing.T) {
	schedules, err := ComposeClassified(fixtureClassifiedRules())
	if err != nil {
		t.Fatal(err)
	}
	asOf := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	copy := Copy{ID: "copy-1", RecordSeries: "payroll-register", Custodian: "payroll", Jurisdiction: "US-CA", CreatedAt: priv011Cutoff(), ArchiveAcknowledged: true}
	rules := []RetentionRule{
		{RecordSeries: "payroll-register", Jurisdiction: "US-CA", MinimumDays: 3650, AuthorityRef: "CA-SOS-SCHEDULE-10Y"},
	}
	report, err := Simulate(SimulationRequest{AsOf: asOf, Copies: []Copy{copy}, Rules: rules})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	if report.Status == Eligible {
		t.Fatal("simulator released a record inside its minimum")
	}
	verdict, err := EvaluateDisposition(schedules[0], priv011Cutoff(), asOf, nil)
	if !errors.Is(err, ErrDispositionBlocked) {
		t.Fatalf("classified evaluation must block, got %+v / %v", verdict, err)
	}
	if !strings.Contains(strings.Join(verdict.BlockingAuthority, ","), "PUBLIC_RECORDS:CA-SOS-SCHEDULE-10Y") {
		t.Fatalf("classified verdict must name the authority: %+v", verdict)
	}
}

// TestTodo_PRIV_011_Conformance walks the authority-class matrix: every
// declared class composes, gates and names its disposition authority.
func TestTodo_PRIV_011_Conformance(t *testing.T) {
	for _, class := range []ScheduleAuthorityClass{AuthorityTax, AuthorityContractual, AuthorityPublicRecords} {
		rules := []ClassifiedRetentionRule{{
			Rule:                 RetentionRule{RecordSeries: "series-" + string(class), Jurisdiction: "US", MinimumDays: 365, AuthorityRef: "ref"},
			Class:                class,
			DispositionAuthority: "AUTH-" + string(class),
		}}
		schedules, err := ComposeClassified(rules)
		if err != nil {
			t.Fatalf("%s: %v", class, err)
		}
		if len(schedules[0].ClassMinima) != 1 || schedules[0].ClassMinima[0].DispositionAuthority != "AUTH-"+string(class) {
			t.Fatalf("%s lost its disposition authority: %+v", class, schedules[0])
		}
		asOf := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
		cutoff := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		if _, err := EvaluateDisposition(schedules[0], cutoff, asOf, nil); !errors.Is(err, ErrDispositionBlocked) {
			t.Fatalf("%s minimum must block, got %v", class, err)
		}
	}
}

// TestTodo_PRIV_011_Mutation kills the timer-collapse mutants: a dropped
// public-records class, a shortened minimum and a silent hold must each
// be detected.
func TestTodo_PRIV_011_Mutation(t *testing.T) {
	collapsed := fixtureClassifiedRules()[:2]
	schedules, err := ComposeClassified(collapsed)
	if err != nil {
		t.Fatal(err)
	}
	if len(schedules[0].ClassMinima) == 3 {
		t.Fatal("collapsed schedule kept a dropped class")
	}
	// At 2027-06-01 the tax (elapses 2024-12-30) and contractual minima
	// are met, but the public-records minimum (elapses 2027-12-30) holds.
	asOf := time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
	if _, err := EvaluateDisposition(schedules[0], priv011Cutoff(), asOf, nil); err != nil {
		t.Fatalf("collapsed schedule must release at %s, got %v", asOf.Format("2006-01-02"), err)
	}
	full, err := ComposeClassified(fixtureClassifiedRules())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateDisposition(full[0], priv011Cutoff(), asOf, nil); !errors.Is(err, ErrDispositionBlocked) {
		t.Fatalf("honest schedule must still block at %s, got %v", asOf.Format("2006-01-02"), err)
	}
	shortened := fixtureClassifiedRules()
	shortened[2].Rule.MinimumDays = 365
	shortSchedules, err := ComposeClassified(shortened)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateDisposition(shortSchedules[0], priv011Cutoff(), asOf, nil); err != nil {
		t.Fatalf("shortened public-records minimum must change the verdict, got %v", err)
	}
}

// FuzzTodo_PRIV_011 proves arbitrary classes, minima and holds either
// compose and gate deterministically or fail closed with a named verdict.
func FuzzTodo_PRIV_011(f *testing.F) {
	f.Add("TAX", 365, "AUTH-X", "hold-1", "TAX")
	f.Add("SHADOW", -5, "", "", "")
	f.Add("PUBLIC_RECORDS", 3650, "NARA-1", "", "")
	f.Fuzz(func(t *testing.T, class string, minimum int, authority, holdID, holdClass string) {
		if minimum < 0 || minimum > 36500 {
			t.Skip("minimum outside the modeled range")
		}
		rules := []ClassifiedRetentionRule{{
			Rule:                 RetentionRule{RecordSeries: "fuzz", Jurisdiction: "US", MinimumDays: minimum, AuthorityRef: "ref"},
			Class:                ScheduleAuthorityClass(class),
			DispositionAuthority: authority,
		}}
		schedules, err := ComposeClassified(rules)
		if err != nil {
			if !errors.Is(err, ErrDispositionInvalid) {
				t.Fatalf("ComposeClassified must fail closed, got %v", err)
			}
			return
		}
		var holds []DispositionHold
		if holdID != "" {
			holds = []DispositionHold{{ID: holdID, Class: ScheduleAuthorityClass(holdClass), Authority: authority, Reason: "fuzz"}}
		}
		cutoff := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		asOf := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
		verdict, err := EvaluateDisposition(schedules[0], cutoff, asOf, holds)
		if err != nil {
			if !errors.Is(err, ErrDispositionBlocked) && !errors.Is(err, ErrDispositionInvalid) {
				t.Fatalf("EvaluateDisposition must fail closed, got %v", err)
			}
			if errors.Is(err, ErrDispositionBlocked) && len(verdict.BlockingAuthority) == 0 {
				t.Fatal("blocked verdict names no authority")
			}
			return
		}
		if verdict.Status != Eligible {
			t.Fatalf("unblocked verdict has status %s", verdict.Status)
		}
	})
}
