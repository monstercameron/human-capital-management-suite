// Package timesheet is the application boundary for duration and
// exception-only time. It keeps the domain's timecard rules pure while
// requiring tenant-scoped authorization and server-resolved coding scope
// before a caller can create or correct evidence.
package timesheet

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timecard"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const contractVersion = 1

// Version reports this package's application contract version.
func Version() int { return contractVersion }

var (
	ErrInvalidPrincipal = errors.New("timesheet: trusted principal required")
	ErrInvalidRequest   = errors.New("timesheet: invalid request")
	ErrUnavailable      = errors.New("timesheet: required port unavailable")
	ErrForbidden        = errors.New("timesheet: forbidden")
	ErrNotFound         = errors.New("timesheet: not found")
	ErrRevisionConflict = errors.New("timesheet: revision conflict")
	ErrModeRejected     = errors.New("timesheet: exception-only mode rejected")
)

// Capability is the closed set of application actions for this package.
type Capability string

const (
	CapRecordDuration   Capability = "TIME_DURATION_RECORD"
	CapCorrectDuration  Capability = "TIME_DURATION_CORRECT"
	CapReviewDuration   Capability = "TIME_DURATION_REVIEW"
	CapCreateException  Capability = "TIME_EXCEPTION_CREATE"
	CapConfirmException Capability = "TIME_EXCEPTION_CONFIRM"
	CapAllocateDuration Capability = "TIME_DURATION_ALLOCATE"
)

// Authorizer evaluates current authority over one worker in one tenant.
type Authorizer interface {
	Authorize(context.Context, *trust.Principal, string, string, Capability) error
}

// CodingScope is resolved by the server from the worker's assignment and
// current grants. Request fields never extend this scope.
type CodingScope struct {
	Projects           map[string]bool
	Grants             map[string]bool
	Capitalizations    map[string]bool
	ResearchActivities map[string]bool
}

type CodingScopeProvider interface {
	AuthorizedCoding(context.Context, *trust.Principal, string, string) (CodingScope, error)
}

// CorrectionApprovalVerifier proves that the referenced supervisor approval
// is a real, tenant-scoped approval for this worker and correction.
type CorrectionApprovalVerifier interface {
	VerifyDurationCorrection(context.Context, *trust.Principal, string, string, timecard.DurationCorrection, string) error
}

// DurationTimesheet is the application read/write shape for one worker's
// duration period. Days retain EntryEvidence so work date and actual entry
// time cannot be collapsed into one timestamp.
type DurationTimesheet struct {
	ID            string
	Tenant        string
	WorkerRef     string
	AssignmentRef string
	ProfileRef    string
	PeriodStart   time.Time
	PeriodEnd     time.Time
	Revision      uint64
	Days          []timecard.DayEntry
	TotalMinutes  int
	GrantReport   timecard.GrantReportKind
	RecordedAt    time.Time
}

// Entries returns a defensive flattened view of the period's evidence.
func (t DurationTimesheet) Entries() []timecard.EntryEvidence {
	var out []timecard.EntryEvidence
	for _, day := range t.Days {
		out = append(out, day.Entries...)
	}
	return append([]timecard.EntryEvidence(nil), out...)
}

// DurationCorrectionRecord is append-only evidence for a corrected line.
type DurationCorrectionRecord struct {
	TimesheetID           string
	WorkerRef             string
	ExpectedRevision      uint64
	Correction            timecard.DurationCorrection
	SupervisorApprovalRef string
	ActorRef              string
	RecordedAt            time.Time
}

type DurationStore interface {
	CreateDuration(context.Context, string, DurationTimesheet, string) (DurationTimesheet, error)
	CorrectDuration(context.Context, string, DurationCorrectionRecord, string) (DurationTimesheet, error)
}

// ExceptionRecord adds tenant and period identity to the pure exception
// period aggregate. Revision is the optimistic-concurrency fence.
type ExceptionRecord struct {
	ID          string
	Tenant      string
	WorkerRef   string
	PeriodStart time.Time
	PeriodEnd   time.Time
	Revision    uint64
	Period      timecard.ExceptionPeriod
	RecordedAt  time.Time
}

type ExceptionStore interface {
	CreateException(context.Context, string, ExceptionRecord, string) (ExceptionRecord, error)
	GetException(context.Context, string, string) (ExceptionRecord, error)
	ConfirmException(context.Context, string, string, uint64, timecard.ExceptionPeriod, string, string) (ExceptionRecord, error)
}

// DurationRequest records a complete set of duration evidence for a period.
// The profile decides whether DCAA and grant controls apply; callers cannot
// disable those controls by changing the request.
type DurationRequest struct {
	TimesheetID    string
	WorkerRef      string
	AssignmentRef  string
	Profile        timeprofile.TimeProfile
	ProfileRef     string
	PeriodStart    time.Time
	PeriodEnd      time.Time
	Entries        []timecard.EntryEvidence
	DCAA           timecard.DCAAProfile
	IdempotencyKey string
}

type DurationCorrectionRequest struct {
	TimesheetID           string
	WorkerRef             string
	ExpectedRevision      uint64
	Original              timecard.DurationLine
	Corrected             timecard.DurationLine
	Reason                string
	SupervisorApprovalRef string
	IdempotencyKey        string
}

type ExceptionRequest struct {
	ID             string
	WorkerRef      string
	Profile        timeprofile.TimeProfile
	Jurisdiction   timecard.RecordingDutyProfile
	PeriodStart    time.Time
	PeriodEnd      time.Time
	PatternRef     string
	Deviations     []timecard.Deviation
	IdempotencyKey string
}

type ConfirmExceptionRequest struct {
	ID               string
	WorkerRef        string
	ExpectedRevision uint64
	IdempotencyKey   string
}

type SalaryDeductionRequest struct {
	WorkerRef string
	Worker    timecard.WorkerClassification
	Rule      timecard.SalaryBasisRule
	Deviation timecard.Deviation
}

// Rejection carries a stable application sentinel and field context.
type Rejection struct {
	Field  string
	Reason string
	Err    error
}

func (r *Rejection) Error() string { return fmt.Sprintf("%v: field=%s: %s", r.Err, r.Field, r.Reason) }
func (r *Rejection) Unwrap() error { return r.Err }
