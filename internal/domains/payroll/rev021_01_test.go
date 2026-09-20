package payroll

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal/stateparams"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// rev021Thresholds mirrors the registered state-pack overtime rows
// (stateparams testdata state-parameters-wire.yaml): Alaska's daily-8/
// weekly-40 row ak-standard-overtime, Colorado's daily-12/weekly-40
// consecutive-trigger row co-comps-40, and Kentucky's weekly-40
// consecutive-trigger row ky-seventh-day with no daily threshold.
func rev021Thresholds() (ak, co, ky stateparams.OvertimeThreshold) {
	half := values.MustDecimal("1.5", 4, values.RoundingHalfUp)
	daily8 := 8
	daily12 := 12
	ak = stateparams.OvertimeThreshold{
		ID: "ak-standard-overtime", DailyThresholdHours: &daily8,
		WeeklyThresholdHours: 40, ConsecutiveDayTrigger: false, Multiplier: half,
	}
	co = stateparams.OvertimeThreshold{
		ID: "co-comps-40", DailyThresholdHours: &daily12,
		WeeklyThresholdHours: 40, ConsecutiveDayTrigger: true, Multiplier: half,
	}
	ky = stateparams.OvertimeThreshold{
		ID:                   "ky-seventh-day",
		WeeklyThresholdHours: 40, ConsecutiveDayTrigger: true, Multiplier: half,
	}
	return ak, co, ky
}

// rev021Input builds the shared golden workweek: 45 hours over six
// consecutive worked days with one long day, priced at $20/hour.
func rev021Input(t *testing.T, overtime stateparams.OvertimeThreshold) WageInput {
	t.Helper()
	monday := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	input := WageInput{
		Tenant: "acme", WorkerRef: "worker-1",
		WorkweekStart: monday, Timezone: "America/New_York",
		DailyHours: []values.Decimal{
			wageDecimal(t, "13.0"), wageDecimal(t, "6.4"), wageDecimal(t, "6.4"),
			wageDecimal(t, "6.4"), wageDecimal(t, "6.4"), wageDecimal(t, "6.4"),
			wageDecimal(t, "0.0"),
		},
		Approved: true, ApprovedBy: "manager-2",
		Exemption:        ExemptionNonexempt,
		RegularRate:      wageDecimal(t, "20"),
		MinimumWage:      wageDecimal(t, "7.25"),
		OvertimeMultiple: wageDecimal(t, "1.5"),
		Rounding:         values.RoundingHalfUp,
		RuleVersion:      "wage-rules/2026.1",
		Overtime:         &overtime,
	}
	return input
}

func rev021Split(t *testing.T, input WageInput) WageResult {
	t.Helper()
	got, err := EvaluateWages(input)
	if err != nil {
		t.Fatalf("EvaluateWages: %v", err)
	}
	if got.Status != WageOK {
		t.Fatalf("status = %v, want OK", got.Status)
	}
	return got
}

// TestTodo_REV_021_01 is the PRIMARY test for REV-021-01: the same 45-hour,
// six-consecutive-day workweek resolves jurisdiction overtime parameters
// through the stateparams threshold instead of opaque caller multipliers,
// and each jurisdiction produces a different overtime split.
func TestTodo_REV_021_01(t *testing.T) {
	ak, co, ky := rev021Thresholds()

	gotAK := rev021Split(t, rev021Input(t, ak))
	// Daily 8 prices the 13-hour day's five excess hours at overtime; the
	// weekly true-up adds nothing further. No double-time bucket exists on
	// the resolved path: the packs record no double threshold.
	if gotAK.RegularHours.String() != "40.00" || gotAK.OvertimeHours.String() != "5.00" || gotAK.DoubleTimeHours.String() != "0.00" {
		t.Fatalf("Alaska split = %s/%s/%s, want 40.00/5.00/0.00",
			gotAK.RegularHours, gotAK.OvertimeHours, gotAK.DoubleTimeHours)
	}
	if gotAK.GrossPay.String() != "950.00" {
		t.Fatalf("Alaska gross = %s, want 950.00", gotAK.GrossPay)
	}

	gotCO := rev021Split(t, rev021Input(t, co))
	// Daily 12 prices one excess hour; the six-day streak moves the sixth
	// day's 6.4 hours to overtime; the weekly true-up adds nothing further.
	if gotCO.RegularHours.String() != "37.60" || gotCO.OvertimeHours.String() != "7.40" || gotCO.DoubleTimeHours.String() != "0.00" {
		t.Fatalf("Colorado split = %s/%s/%s, want 37.60/7.40/0.00",
			gotCO.RegularHours, gotCO.OvertimeHours, gotCO.DoubleTimeHours)
	}
	if gotCO.GrossPay.String() != "974.00" {
		t.Fatalf("Colorado gross = %s, want 974.00", gotCO.GrossPay)
	}

	gotKY := rev021Split(t, rev021Input(t, ky))
	// No daily threshold: only the weekly-40 true-up and the six-day
	// streak tail price overtime.
	if gotKY.RegularHours.String() != "38.60" || gotKY.OvertimeHours.String() != "6.40" || gotKY.DoubleTimeHours.String() != "0.00" {
		t.Fatalf("Kentucky split = %s/%s/%s, want 38.60/6.40/0.00",
			gotKY.RegularHours, gotKY.OvertimeHours, gotKY.DoubleTimeHours)
	}
	if gotKY.GrossPay.String() != "964.00" {
		t.Fatalf("Kentucky gross = %s, want 964.00", gotKY.GrossPay)
	}

	if gotAK.OvertimeHours.String() == gotCO.OvertimeHours.String() ||
		gotCO.OvertimeHours.String() == gotKY.OvertimeHours.String() ||
		gotAK.OvertimeHours.String() == gotKY.OvertimeHours.String() {
		t.Fatal("jurisdictions did not produce distinct overtime splits")
	}

	t.Run("threshold multiplier sources the rate when the caller passes none", func(t *testing.T) {
		_, _, kyRow := rev021Thresholds()
		input := rev021Input(t, kyRow)
		input.OvertimeMultiple = values.Decimal{}
		got, err := EvaluateWages(input)
		if err != nil {
			t.Fatalf("EvaluateWages: %v", err)
		}
		if got.OvertimeHours.String() != "6.40" || got.GrossPay.String() != "964.00" {
			t.Fatalf("threshold-multiplier split = %s/%s, want 6.40/964.00", got.OvertimeHours, got.GrossPay)
		}
	})

	t.Run("contradictory double-time multiple with a resolved threshold refuses", func(t *testing.T) {
		akRow, _, _ := rev021Thresholds()
		input := rev021Input(t, akRow)
		input.DoubleTimeMultiple = wageDecimal(t, "2")
		if _, err := EvaluateWages(input); err == nil {
			t.Fatal("EvaluateWages accepted a double-time multiple the threshold path cannot price")
		}
	})

	t.Run("legacy path without a threshold is unchanged", func(t *testing.T) {
		input := rev021Input(t, ak)
		input.Overtime = nil
		input.DoubleTimeMultiple = wageDecimal(t, "2")
		got, err := EvaluateWages(input)
		if err != nil {
			t.Fatalf("EvaluateWages: %v", err)
		}
		// Legacy prices hours past twelve at double time, unlike the
		// resolved path: 40.00 regular, 4.00 overtime, 1.00 double.
		if got.RegularHours.String() != "40.00" || got.OvertimeHours.String() != "4.00" || got.DoubleTimeHours.String() != "1.00" {
			t.Fatalf("legacy split = %s/%s/%s, want 40.00/4.00/1.00",
				got.RegularHours, got.OvertimeHours, got.DoubleTimeHours)
		}
	})
}

// TestTodo_REV_021_01_Golden pins the exact overtime splits and digests for
// the shared workweek so a pack-parameter or pricing regression is caught.
func TestTodo_REV_021_01_Golden(t *testing.T) {
	ak, co, ky := rev021Thresholds()
	want := map[string]struct {
		row                      stateparams.OvertimeThreshold
		regular, overtime, gross string
	}{
		"alaska":   {ak, "40.00", "5.00", "950.00"},
		"colorado": {co, "37.60", "7.40", "974.00"},
		"kentucky": {ky, "38.60", "6.40", "964.00"},
	}
	digests := map[string]string{}
	for name, tc := range want {
		got := rev021Split(t, rev021Input(t, tc.row))
		if got.RegularHours.String() != tc.regular || got.OvertimeHours.String() != tc.overtime || got.GrossPay.String() != tc.gross {
			t.Fatalf("%s = %s/%s/%s, want %s/%s/%s", name,
				got.RegularHours, got.OvertimeHours, got.GrossPay, tc.regular, tc.overtime, tc.gross)
		}
		if got.Digest == "" {
			t.Fatalf("%s carries no digest", name)
		}
		digests[name] = got.Digest
	}
	if digests["alaska"] == digests["colorado"] || digests["colorado"] == digests["kentucky"] {
		t.Fatal("jurisdiction digests collide: the digest does not capture the resolved rule")
	}
}
