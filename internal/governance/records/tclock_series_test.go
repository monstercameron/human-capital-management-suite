package records

import (
	"errors"
	"testing"
	"time"
)

func TestTodo_TCLOCK_017_TimeRecordRules(t *testing.T) {
	days := FLSATimeRecordMinimums()
	days.Attestations, days.Photos, days.Location, days.DeviceLogs = 730, 730, 730, 730
	rules, err := TimeRecordRetentionRules("US-CA", days)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 6 {
		t.Fatalf("rules = %d, want six separately retained series", len(rules))
	}
	if rules[0].RecordSeries != TimeRecordSeriesAttestations || rules[len(rules)-1].RecordSeries != TimeRecordSeriesTimecards {
		t.Fatalf("rules are not deterministically sorted: %+v", rules)
	}
	bySeries := make(map[string]RetentionRule, len(rules))
	for _, rule := range rules {
		bySeries[rule.RecordSeries] = rule
	}
	if bySeries[TimeRecordSeriesPunches].MinimumDays != 731 || bySeries[TimeRecordSeriesTimecards].MinimumDays != 1096 {
		t.Fatalf("FLSA baseline = %+v", bySeries)
	}
	if bySeries[TimeRecordSeriesPhotos].MinimumDays != 730 {
		t.Fatal("test fixture must explicitly configure photo evidence")
	}
	if bySeries[TimeRecordSeriesPunches].AuthorityRef != "29-CFR-516.6-wage-calculation" || bySeries[TimeRecordSeriesTimecards].AuthorityRef != "29-CFR-516.5-payroll" {
		t.Fatal("US rules must carry the applicable federal authority references")
	}

	schedules, err := ComposeClassified([]ClassifiedRetentionRule{
		{Rule: bySeries[TimeRecordSeriesPunches], Class: AuthorityContractual, DispositionAuthority: "time-punch-policy"},
		{Rule: bySeries[TimeRecordSeriesTimecards], Class: AuthorityTax, DispositionAuthority: "payroll-records"},
	})
	if err != nil {
		t.Fatal(err)
	}
	verdict, err := EvaluateDisposition(schedules[1], time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2022, 1, 2, 0, 0, 0, 0, time.UTC), nil)
	if !errors.Is(err, ErrDispositionBlocked) || verdict.Status != BlockedWithReasons {
		t.Fatalf("unmet retention must block with the existing engine: %+v, %v", verdict, err)
	}
}

func TestTodo_TCLOCK_017_TimeRecordRulesRejectIncompleteConfig(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*TimeRecordRetentionDays)
	}{
		{name: "missing jurisdiction", mutate: func(*TimeRecordRetentionDays) {}},
		{name: "missing evidence period", mutate: func(d *TimeRecordRetentionDays) { d.Photos = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			days := FLSATimeRecordMinimums()
			tc.mutate(&days)
			jurisdiction := "US-CA"
			if tc.name == "missing jurisdiction" {
				jurisdiction = ""
			}
			if _, err := TimeRecordRetentionRules(jurisdiction, days); err == nil {
				t.Fatal("incomplete declaration unexpectedly accepted")
			}
		})
	}
}

func TestTodo_TCLOCK_017_USMinimumAndLeapBoundary(t *testing.T) {
	days := FLSATimeRecordMinimums()
	days.Attestations, days.Photos, days.Location, days.DeviceLogs = 30, 7, 14, 21
	if _, err := TimeRecordRetentionRules("US-CA", days); err != nil {
		t.Fatalf("explicit supporting evidence periods should be accepted: %v", err)
	}
	days.Punches = 730
	if _, err := TimeRecordRetentionRules("US-CA", days); err == nil {
		t.Fatal("730 days must not stand in for two calendar years in a US schedule")
	}
	days = FLSATimeRecordMinimums()
	days.Attestations, days.Photos, days.Location, days.DeviceLogs = 30, 7, 14, 21
	rules, err := TimeRecordRetentionRules("US-CA", days)
	if err != nil {
		t.Fatal(err)
	}
	var punchRule RetentionRule
	for _, rule := range rules {
		if rule.RecordSeries == TimeRecordSeriesPunches {
			punchRule = rule
			break
		}
	}
	schedules, err := ComposeClassified([]ClassifiedRetentionRule{{Rule: punchRule, Class: AuthorityContractual, DispositionAuthority: punchRule.AuthorityRef}})
	if err != nil {
		t.Fatal(err)
	}
	cutoff := time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)
	verdict, err := EvaluateDisposition(schedules[0], cutoff, time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC), nil)
	if !errors.Is(err, ErrDispositionBlocked) || verdict.Status != BlockedWithReasons {
		t.Fatalf("leap-year boundary must remain blocked before two full calendar years: %+v, %v", verdict, err)
	}
}
