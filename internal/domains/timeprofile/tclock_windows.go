package timeprofile

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/worktimerules"
)

// WorkingTimeInterval and WorkingTimeLedger are the time-profile view of the
// shared working-time accumulator. The aliases are deliberate: window
// reduction belongs to worktimerules, while a profile only chooses the
// aggregation key and the reference period a workflow should read.
type WorkingTimeInterval = worktimerules.LedgerEntry
type WorkingTimeLedger = worktimerules.Ledger
type WorkingTimeKind = worktimerules.IntervalKind

const (
	WorkingTimeKindWork   = worktimerules.KindWork
	WorkingTimeKindRest   = worktimerules.KindRest
	WorkingTimeKindSchool = worktimerules.KindSchool
)

// WorkingTimeWindowRequest identifies one pinned, as-of read. WorkerID is the
// profile's AggregationKey in the application adapter; keeping the field name
// generic lets the same read serve concurrent assignments for one worker.
type WorkingTimeWindowRequest struct {
	TenantID        string
	WorkerID        string
	AsOf            time.Time
	ReferencePeriod time.Duration
	HirerID         string
	BreakTolerance  time.Duration
}

func (r WorkingTimeWindowRequest) validate() error {
	if r.TenantID == "" || r.WorkerID == "" {
		return fmt.Errorf("%w: tenant and worker are required", worktimerules.ErrMissingScope)
	}
	if r.AsOf.IsZero() {
		return fmt.Errorf("%w: as_of is required", worktimerules.ErrInvalidLedger)
	}
	if r.ReferencePeriod < 0 || r.BreakTolerance < 0 {
		return fmt.Errorf("%w: durations cannot be negative", worktimerules.ErrInvalidLedger)
	}
	return nil
}

// WorkingTimeWindows is the pinned answer returned to a workflow DECISION.
// Revision must be copied into the decision evidence; a later correction
// changes the revision and makes the previous observation stale.
type WorkingTimeWindows struct {
	worktimerules.Windows
	WeeksWithHirer int
	ParityDue      bool
	HirerID        string
}

// ObserveWorkingTimeWindows reduces one scoped ledger snapshot and carries
// the exact revision used for every value. The balance engine remains the
// only reducer; this function adds the profile-facing hirer counter.
func ObserveWorkingTimeWindows(ledger WorkingTimeLedger, req WorkingTimeWindowRequest) (WorkingTimeWindows, error) {
	if err := req.validate(); err != nil {
		return WorkingTimeWindows{}, err
	}
	windows, err := worktimerules.ComputeWindows(ledger, worktimerules.WindowOptions{
		TenantID: req.TenantID, WorkerID: req.WorkerID, AsOf: req.AsOf, ReferencePeriod: req.ReferencePeriod,
	})
	if err != nil {
		return WorkingTimeWindows{}, err
	}
	out := WorkingTimeWindows{Windows: windows, HirerID: req.HirerID}
	if req.HirerID != "" {
		out.WeeksWithHirer, out.ParityDue, err = worktimerules.AWRWeeksWithHirer(
			ledger, req.TenantID, req.WorkerID, req.HirerID, req.AsOf, req.BreakTolerance,
		)
		if err != nil {
			return WorkingTimeWindows{}, err
		}
	}
	return out, nil
}

// CheckWorkingTimeRevision is the compare-and-record guard for a workflow
// DECISION. Call it immediately before recording an outcome that depends on
// the window; callers must reread after ErrRevisionMismatch.
func CheckWorkingTimeRevision(ledger WorkingTimeLedger, tenantID, workerID, expected string) error {
	return worktimerules.CheckRevision(ledger, tenantID, workerID, expected)
}
