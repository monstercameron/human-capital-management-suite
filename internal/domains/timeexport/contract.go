// Package timeexport exports and imports approved timecards in the HR Open
// Standards TimeCard format (https://www.hropenstandards.org/standards),
// pinned to the approved revision, plus generic payroll flat exports (an
// ADP-style CSV, a Paychex-style CSV and a QuickBooks Desktop IIF timer
// activity file) built from the same read model.
//
// The package is pure: no database, no clock, no network, no package-level
// mutable state.
package timeexport

const schemaVersion = 1

// Version reports this package's contract version.
func Version() int { return schemaVersion }

// Explanation is the ARCH-GO-009 explain view of a timecard.
type Explanation struct {
	WorkerRef        string
	PeriodStart      string
	PeriodEnd        string
	Revision         string
	PreviousRevision string
	IntervalCount    int
	AllowanceCount   int
}

// Explain summarizes a timecard for audit and inspection.
func (c TimeCard) Explain() (Explanation, error) {
	if err := c.Validate(); err != nil {
		return Explanation{}, err
	}
	return Explanation{
		WorkerRef: c.WorkerRef, PeriodStart: c.PeriodStart, PeriodEnd: c.PeriodEnd,
		Revision: c.Revision, PreviousRevision: c.PreviousRevision,
		IntervalCount: len(c.Intervals), AllowanceCount: len(c.Allowances),
	}, nil
}
