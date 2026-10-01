package timeexport

import (
	"encoding/json"
	"fmt"
)

// The wire structs below are this package's subset of the HR Open Standards
// TimeCard object (https://www.hropenstandards.org/standards): worker,
// period, intervals with pay code and job allocation, and allowances,
// pinned to an approved revision. testdata/timecard.schema.json describes
// exactly the same fields for the CONFORMANCE test.

type wireJobAllocation struct {
	Project  string `json:"project"`
	CostCode string `json:"costCode"`
	RateCode string `json:"rateCode"`
}

type wireInterval struct {
	Date           string            `json:"date"`
	Minutes        int               `json:"minutes"`
	PayCode        string            `json:"payCode"`
	JobAllocation  wireJobAllocation `json:"jobAllocation"`
	SourceRevision string            `json:"sourceRevision"`
}

type wireAllowance struct {
	Code     string `json:"code"`
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

type wirePeriod struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type wireTimeCard struct {
	WorkerId         string          `json:"workerId"`
	Period           wirePeriod      `json:"period"`
	Revision         string          `json:"revision"`
	PreviousRevision string          `json:"previousRevision,omitempty"`
	Intervals        []wireInterval  `json:"intervals"`
	Allowances       []wireAllowance `json:"allowances,omitempty"`
}

func toWire(c TimeCard) wireTimeCard {
	w := wireTimeCard{
		WorkerId: c.WorkerRef, Period: wirePeriod{Start: c.PeriodStart, End: c.PeriodEnd},
		Revision: c.Revision, PreviousRevision: c.PreviousRevision,
	}
	for _, iv := range c.Intervals {
		w.Intervals = append(w.Intervals, wireInterval{
			Date: iv.Date, Minutes: iv.Minutes, PayCode: iv.PayCode,
			JobAllocation:  wireJobAllocation{Project: iv.Project, CostCode: iv.CostCode, RateCode: iv.RateCode},
			SourceRevision: iv.SourceRevisionDigest,
		})
	}
	for _, a := range c.Allowances {
		w.Allowances = append(w.Allowances, wireAllowance{Code: a.Code, Amount: a.Amount, Currency: a.Currency})
	}
	return w
}

func fromWire(w wireTimeCard) TimeCard {
	c := TimeCard{
		WorkerRef: w.WorkerId, PeriodStart: w.Period.Start, PeriodEnd: w.Period.End,
		Revision: w.Revision, PreviousRevision: w.PreviousRevision,
	}
	for _, iv := range w.Intervals {
		c.Intervals = append(c.Intervals, Interval{
			Date: iv.Date, Minutes: iv.Minutes, PayCode: iv.PayCode,
			Project: iv.JobAllocation.Project, CostCode: iv.JobAllocation.CostCode, RateCode: iv.JobAllocation.RateCode,
			SourceRevisionDigest: iv.SourceRevision,
		})
	}
	for _, a := range w.Allowances {
		c.Allowances = append(c.Allowances, Allowance{Code: a.Code, Amount: a.Amount, Currency: a.Currency})
	}
	return c
}

// Export renders an approved timecard as HR Open TimeCard JSON, pinned to
// its approved revision. It never exports an invalid timecard.
func Export(c TimeCard) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(toWire(c), "", "  ")
}

// knownTopLevelKeys, knownIntervalKeys and knownAllowanceKeys name every
// field this package maps, so Import can report elements it saw but did not
// map rather than silently dropping them.
var (
	knownTopLevelKeys = map[string]bool{
		"workerId": true, "period": true, "revision": true, "previousRevision": true,
		"intervals": true, "allowances": true,
	}
	knownIntervalKeys = map[string]bool{
		"date": true, "minutes": true, "payCode": true, "jobAllocation": true, "sourceRevision": true,
	}
	knownAllowanceKeys = map[string]bool{"code": true, "amount": true, "currency": true}
)

// UnmappedReport lists every JSON element Import saw but could not map onto
// a TimeCard field, indexed by where it was found.
type UnmappedReport struct {
	TopLevel   []string
	Intervals  map[int][]string
	Allowances map[int][]string
}

// HasUnmapped reports whether the import found anything it could not map.
func (r UnmappedReport) HasUnmapped() bool {
	return len(r.TopLevel) > 0 || len(r.Intervals) > 0 || len(r.Allowances) > 0
}

// Import parses HR Open TimeCard JSON into a TimeCard and validates its
// shape. It never silently drops an allowance or job-allocation element that
// it does not recognize: an unrecognized element is reported in the
// returned UnmappedReport instead.
func Import(raw []byte) (TimeCard, UnmappedReport, error) {
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		return TimeCard{}, UnmappedReport{}, fmt.Errorf("timeexport: import: not valid JSON object: %w", err)
	}
	report := UnmappedReport{Intervals: map[int][]string{}, Allowances: map[int][]string{}}
	for key := range generic {
		if !knownTopLevelKeys[key] {
			report.TopLevel = append(report.TopLevel, key)
		}
	}

	var rawIntervals []map[string]json.RawMessage
	if raw, ok := generic["intervals"]; ok {
		if err := json.Unmarshal(raw, &rawIntervals); err != nil {
			return TimeCard{}, UnmappedReport{}, fmt.Errorf("timeexport: import: intervals malformed: %w", err)
		}
	}
	for i, iv := range rawIntervals {
		for key := range iv {
			if !knownIntervalKeys[key] {
				report.Intervals[i] = append(report.Intervals[i], key)
			}
		}
	}

	var rawAllowances []map[string]json.RawMessage
	if raw, ok := generic["allowances"]; ok {
		if err := json.Unmarshal(raw, &rawAllowances); err != nil {
			return TimeCard{}, UnmappedReport{}, fmt.Errorf("timeexport: import: allowances malformed: %w", err)
		}
	}
	for i, a := range rawAllowances {
		for key := range a {
			if !knownAllowanceKeys[key] {
				report.Allowances[i] = append(report.Allowances[i], key)
			}
		}
	}

	var w wireTimeCard
	if err := json.Unmarshal(raw, &w); err != nil {
		return TimeCard{}, UnmappedReport{}, fmt.Errorf("timeexport: import: %w", err)
	}
	c := fromWire(w)
	if err := c.Validate(); err != nil {
		return TimeCard{}, UnmappedReport{}, err
	}
	return c, report, nil
}
