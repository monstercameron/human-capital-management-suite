package timeexport

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// ApprovedLine is the minimal approved-time input this package needs. It is
// defined here, rather than imported from an approval or timecard-source
// package, so this domain never depends on an unfinished lane; a service
// layer maps its own approved-timecard record onto this shape. PayCode is
// required: an interval with no pay code cannot be attributed on export, and
// a missing pay code is a build-time rejection rather than a silent default.
type ApprovedLine struct {
	WorkerRef      string
	Date           string // ISO 8601 calendar date, "2006-01-02"
	Minutes        int
	Project        string
	CostCode       string
	RateCode       string
	PayCode        string
	RevisionDigest string
}

func (l ApprovedLine) validate(worker string) error {
	if strings.TrimSpace(l.WorkerRef) == "" || l.WorkerRef != worker {
		return reject("ApprovedLine.WorkerRef", l.WorkerRef, "approved line worker must match the timecard worker")
	}
	if strings.TrimSpace(l.Date) == "" {
		return reject("ApprovedLine.Date", l.Date, "approved line date is required")
	}
	if l.Minutes <= 0 {
		return reject("ApprovedLine.Minutes", strconv.Itoa(l.Minutes), "approved minutes must be positive")
	}
	if strings.TrimSpace(l.PayCode) == "" {
		return reject("ApprovedLine.PayCode", "", "approved line must carry a pay code")
	}
	if strings.TrimSpace(l.RevisionDigest) == "" {
		return reject("ApprovedLine.RevisionDigest", "", "approved line must carry its approval revision digest; unapproved time cannot export")
	}
	return nil
}

// Allowance is a per-period allowance amount (meal, travel, tool, and so
// on). Amount is plain fixed-point text; this package does no currency math,
// it only carries the approved figure through to the export.
type Allowance struct {
	Code     string
	Amount   string
	Currency string
}

func (a Allowance) validate() error {
	if strings.TrimSpace(a.Code) == "" {
		return reject("Allowance.Code", "", "allowance code is required")
	}
	if strings.TrimSpace(a.Amount) == "" {
		return reject("Allowance.Amount", "", "allowance amount is required")
	}
	if strings.TrimSpace(a.Currency) == "" {
		return reject("Allowance.Currency", "", "allowance currency is required")
	}
	return nil
}

// Interval is one worked interval on the timecard, carrying the job
// allocation (project, cost code, rate code) and pay code the HR Open
// TimeCard format expects.
type Interval struct {
	Date     string
	Minutes  int
	PayCode  string
	Project  string
	CostCode string
	RateCode string
	// SourceRevisionDigest is the individual approval this interval traces
	// back to, kept for audit even though the timecard as a whole is pinned
	// to its own Revision.
	SourceRevisionDigest string
}

// TimeCard is an approved timecard for one worker and period, pinned to the
// approved Revision it was built from. PreviousRevision is empty for a first
// export and set to the prior Revision for a correction, so a receiver can
// apply corrections idempotently: replaying the same correction twice never
// double-applies it.
type TimeCard struct {
	WorkerRef        string
	PeriodStart      string
	PeriodEnd        string
	Revision         string
	PreviousRevision string
	Intervals        []Interval
	Allowances       []Allowance
}

// BuildTimeCardRequest carries one worker's approved lines and allowances
// for one period into a timecard pinned to Revision.
type BuildTimeCardRequest struct {
	WorkerRef        string
	PeriodStart      string
	PeriodEnd        string
	Revision         string
	PreviousRevision string
	Lines            []ApprovedLine
	Allowances       []Allowance
}

// BuildTimeCard prices no money and computes no premiums; it only shapes
// already-approved lines and allowances into the HR Open TimeCard export
// shape, pinned to the approved revision. Every line must carry a non-empty
// approval revision digest: unapproved time cannot export.
func BuildTimeCard(req BuildTimeCardRequest) (TimeCard, error) {
	if strings.TrimSpace(req.WorkerRef) == "" {
		return TimeCard{}, reject("BuildTimeCardRequest.WorkerRef", "", "timecard worker is required")
	}
	if strings.TrimSpace(req.PeriodStart) == "" || strings.TrimSpace(req.PeriodEnd) == "" {
		return TimeCard{}, reject("BuildTimeCardRequest.Period", "", "timecard period start and end are required")
	}
	if strings.TrimSpace(req.Revision) == "" {
		return TimeCard{}, reject("BuildTimeCardRequest.Revision", "", "timecard must be pinned to an approved revision")
	}
	if len(req.Lines) == 0 {
		return TimeCard{}, reject("BuildTimeCardRequest.Lines", "", "timecard needs at least one approved line")
	}
	seen := map[string]bool{}
	intervals := make([]Interval, 0, len(req.Lines))
	for _, l := range req.Lines {
		if err := l.validate(req.WorkerRef); err != nil {
			return TimeCard{}, err
		}
		key := l.Date + "|" + l.PayCode + "|" + l.RevisionDigest
		if seen[key] {
			return TimeCard{}, reject("ApprovedLine", key, "duplicate approved line")
		}
		seen[key] = true
		intervals = append(intervals, Interval{
			Date: l.Date, Minutes: l.Minutes, PayCode: l.PayCode, Project: l.Project,
			CostCode: l.CostCode, RateCode: l.RateCode, SourceRevisionDigest: l.RevisionDigest,
		})
	}
	allowances := make([]Allowance, 0, len(req.Allowances))
	allowanceSeen := map[string]bool{}
	for _, a := range req.Allowances {
		if err := a.validate(); err != nil {
			return TimeCard{}, err
		}
		if allowanceSeen[a.Code] {
			return TimeCard{}, reject("Allowance.Code", a.Code, "duplicate allowance code")
		}
		allowanceSeen[a.Code] = true
		allowances = append(allowances, a)
	}
	return TimeCard{
		WorkerRef: req.WorkerRef, PeriodStart: req.PeriodStart, PeriodEnd: req.PeriodEnd,
		Revision: req.Revision, PreviousRevision: req.PreviousRevision,
		Intervals: intervals, Allowances: allowances,
	}, nil
}

// Validate reports whether the timecard is internally consistent.
func (c TimeCard) Validate() error {
	if strings.TrimSpace(c.WorkerRef) == "" {
		return reject("TimeCard.WorkerRef", "", "timecard worker is required")
	}
	if strings.TrimSpace(c.PeriodStart) == "" || strings.TrimSpace(c.PeriodEnd) == "" {
		return reject("TimeCard.Period", "", "timecard period start and end are required")
	}
	if strings.TrimSpace(c.Revision) == "" {
		return reject("TimeCard.Revision", "", "timecard revision is required")
	}
	if len(c.Intervals) == 0 {
		return reject("TimeCard.Intervals", "", "timecard has no intervals")
	}
	for i, iv := range c.Intervals {
		if strings.TrimSpace(iv.Date) == "" || iv.Minutes <= 0 || strings.TrimSpace(iv.PayCode) == "" {
			return reject("TimeCard.Intervals", fmt.Sprintf("%d", i), "interval missing date, minutes or pay code")
		}
	}
	for i, a := range c.Allowances {
		if err := a.validate(); err != nil {
			return fmt.Errorf("allowance %d: %w", i, err)
		}
	}
	return nil
}

// digest is a stable content hash of the timecard, used to detect a no-op
// correction replay.
func (c TimeCard) digest() string {
	fields := []string{c.WorkerRef, c.PeriodStart, c.PeriodEnd, c.Revision, c.PreviousRevision}
	for _, iv := range c.Intervals {
		fields = append(fields, iv.Date, strconv.Itoa(iv.Minutes), iv.PayCode, iv.Project, iv.CostCode, iv.RateCode, iv.SourceRevisionDigest)
	}
	for _, a := range c.Allowances {
		fields = append(fields, a.Code, a.Amount, a.Currency)
	}
	b := strings.Builder{}
	for _, f := range fields {
		fmt.Fprintf(&b, "%d:%s", len(f), f)
	}
	h := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(h[:])
}

// ApplyCorrection applies a correction timecard on top of a base timecard.
// The correction must name the base's revision as its PreviousRevision.
// Applying the exact same correction a second time against the state it
// already produced is a no-op, not a duplicate: idempotent replay is
// verified by content digest, not merely by revision label.
func ApplyCorrection(base, correction TimeCard) (TimeCard, error) {
	if err := base.Validate(); err != nil {
		return TimeCard{}, fmt.Errorf("base timecard: %w", err)
	}
	if err := correction.Validate(); err != nil {
		return TimeCard{}, fmt.Errorf("correction timecard: %w", err)
	}
	if correction.WorkerRef != base.WorkerRef || correction.PeriodStart != base.PeriodStart || correction.PeriodEnd != base.PeriodEnd {
		return TimeCard{}, reject("Correction", "identity", "correction must retain the base timecard's worker and period")
	}
	if base.digest() == correction.digest() {
		// Replaying the exact same correction against the state it already
		// produced: idempotent no-op.
		return base, nil
	}
	if correction.PreviousRevision != base.Revision {
		return TimeCard{}, reject("Correction.PreviousRevision", correction.PreviousRevision,
			"correction must name the base timecard's revision as its previous revision")
	}
	return correction, nil
}
