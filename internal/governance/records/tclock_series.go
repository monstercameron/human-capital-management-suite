package records

import (
	"fmt"
	"sort"
	"strings"
)

// Time record series names are deliberately separate because a punch, an
// approved timecard, and supporting device evidence do not share a lawful
// retention clock.
const (
	TimeRecordSeriesPunches      = "time.punches"
	TimeRecordSeriesTimecards    = "time.timecards"
	TimeRecordSeriesAttestations = "time.attestations"
	TimeRecordSeriesPhotos       = "time.photo_evidence"
	TimeRecordSeriesLocation     = "time.location_evidence"
	TimeRecordSeriesDeviceLogs   = "time.device_logs"
)

// TimeRecordRetentionDays supplies the tenant/jurisdiction-specific minimum
// retention periods for the time-record series. Supporting evidence is
// intentionally configurable: its legal minimum depends on purpose,
// jurisdiction, and whether it is independently used as evidence.
type TimeRecordRetentionDays struct {
	Punches      int
	Timecards    int
	Attestations int
	Photos       int
	Location     int
	DeviceLogs   int
}

// Validate rejects a schedule that would silently omit a time-record class or
// make a record disposable immediately after cutoff.
func (d TimeRecordRetentionDays) Validate() error {
	values := []struct {
		name string
		days int
	}{
		{"punches", d.Punches}, {"timecards", d.Timecards},
		{"attestations", d.Attestations}, {"photos", d.Photos},
		{"location", d.Location}, {"device_logs", d.DeviceLogs},
	}
	for _, value := range values {
		if value.days < 1 {
			return fmt.Errorf("records: time retention for %s must be positive", value.name)
		}
	}
	return nil
}

// FLSATimeRecordMinimums returns only the federal baseline from 29 CFR 516:
// wage calculation records such as time cards are kept for two years, while
// payroll records are kept for three years. Supporting evidence is left at
// zero and must be supplied by the caller because its minimum depends on its
// purpose and jurisdiction.
func FLSATimeRecordMinimums() TimeRecordRetentionDays {
	return TimeRecordRetentionDays{
		// 731 and 1096 are conservative day floors. The records engine
		// evaluates cutoff plus days, while the legal minimums are stated in
		// calendar years; the extra day covers leap-year boundaries.
		Punches: 731, Timecards: 1096,
	}
}

// TimeRecordRetentionRules converts explicit periods into the generic
// records engine's rules. The output is stable by series name and each rule
// carries the authority that explains its minimum.
func TimeRecordRetentionRules(jurisdiction string, days TimeRecordRetentionDays) ([]RetentionRule, error) {
	jurisdiction = strings.TrimSpace(jurisdiction)
	if jurisdiction == "" {
		return nil, fmt.Errorf("records: time retention jurisdiction is required")
	}
	if err := days.Validate(); err != nil {
		return nil, err
	}
	if strings.HasPrefix(strings.ToUpper(jurisdiction), "US-") {
		if days.Punches < 731 || days.Timecards < 1096 {
			return nil, fmt.Errorf("records: US time retention must preserve at least two years of punches and three years of payroll timecards")
		}
	}
	punchAuthority := jurisdiction + ":time-wage-calculation"
	cardAuthority := jurisdiction + ":time-payroll-records"
	if strings.HasPrefix(strings.ToUpper(jurisdiction), "US-") {
		punchAuthority = "29-CFR-516.6-wage-calculation"
		cardAuthority = "29-CFR-516.5-payroll"
	}
	rules := []RetentionRule{
		{RecordSeries: TimeRecordSeriesPunches, Jurisdiction: jurisdiction, MinimumDays: days.Punches, AuthorityRef: punchAuthority},
		{RecordSeries: TimeRecordSeriesTimecards, Jurisdiction: jurisdiction, MinimumDays: days.Timecards, AuthorityRef: cardAuthority},
		{RecordSeries: TimeRecordSeriesAttestations, Jurisdiction: jurisdiction, MinimumDays: days.Attestations, AuthorityRef: jurisdiction + ":time-attestation-purpose"},
		{RecordSeries: TimeRecordSeriesPhotos, Jurisdiction: jurisdiction, MinimumDays: days.Photos, AuthorityRef: jurisdiction + ":time-photo-evidence-purpose"},
		{RecordSeries: TimeRecordSeriesLocation, Jurisdiction: jurisdiction, MinimumDays: days.Location, AuthorityRef: jurisdiction + ":time-location-evidence-purpose"},
		{RecordSeries: TimeRecordSeriesDeviceLogs, Jurisdiction: jurisdiction, MinimumDays: days.DeviceLogs, AuthorityRef: jurisdiction + ":time-device-evidence-purpose"},
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].RecordSeries < rules[j].RecordSeries })
	return rules, nil
}
