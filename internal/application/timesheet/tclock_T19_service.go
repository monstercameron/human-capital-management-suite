package timesheet

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timecard"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Service coordinates the pure timecard domain with authorization and
// persistence ports. A zero required port fails closed.
type Service struct {
	Durations  DurationStore
	Exceptions ExceptionStore
	Auth       Authorizer
	Coding     CodingScopeProvider
	Approvals  CorrectionApprovalVerifier
	Clock      func() time.Time
}

func (s Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func principalOK(p *trust.Principal) error {
	if p == nil || p.Subject() == "" || string(p.Tenant()) == "" {
		return ErrInvalidPrincipal
	}
	return nil
}

func (s Service) authorize(ctx context.Context, p *trust.Principal, worker string, cap Capability) error {
	if err := principalOK(p); err != nil {
		return err
	}
	if strings.TrimSpace(worker) == "" {
		return ErrInvalidRequest
	}
	if s.Auth == nil {
		return ErrUnavailable
	}
	return s.Auth.Authorize(ctx, p, string(p.Tenant()), worker, cap)
}

func dayStart(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

func validPeriod(start, end time.Time) bool {
	return !start.IsZero() && !end.IsZero() && end.After(start)
}

func validateDurationProfile(p timeprofile.TimeProfile) error {
	if p.Capture != timeprofile.CaptureDuration {
		return &Rejection{Field: "profile.capture", Reason: "duration evidence requires DURATION capture", Err: ErrInvalidRequest}
	}
	if p.Grant.ActivityReportRequired && p.Grant.SemiAnnualCertification {
		return &Rejection{Field: "profile.grant", Reason: "grant reporting modes are mutually exclusive", Err: ErrInvalidRequest}
	}
	if (p.GovernmentContract.DCAATotalTimeAccounting || p.Grant.ActivityReportRequired || p.Grant.SemiAnnualCertification) && strings.TrimSpace(p.Taxonomy.Primary) == "" {
		return &Rejection{Field: "profile.taxonomy", Reason: "governed duration profiles require a primary taxonomy", Err: ErrInvalidRequest}
	}
	return nil
}

func authorized(scope CodingScope, line timecard.DurationLine) error {
	if !scope.Projects[line.Project.Value] {
		return fmt.Errorf("project %q is outside the worker's authorized coding scope", line.Project.Value)
	}
	if line.Grant != "" && !scope.Grants[line.Grant] {
		return fmt.Errorf("grant %q is outside the worker's authorized coding scope", line.Grant)
	}
	if line.Taxonomy.Capitalization != "" && !scope.Capitalizations[line.Taxonomy.Capitalization] {
		return fmt.Errorf("capitalization taxonomy %q is outside the worker's authorized coding scope", line.Taxonomy.Capitalization)
	}
	if line.Taxonomy.ResearchActivity != "" && !scope.ResearchActivities[line.Taxonomy.ResearchActivity] {
		return fmt.Errorf("research taxonomy %q is outside the worker's authorized coding scope", line.Taxonomy.ResearchActivity)
	}
	return nil
}

// RecordDuration validates and persists one complete period of duration
// evidence. Lines are grouped by work date, and all groups retain entry time
// so late Friday back-fills remain observable.
func (s Service) RecordDuration(ctx context.Context, p *trust.Principal, req DurationRequest) (DurationTimesheet, error) {
	if err := principalOK(p); err != nil {
		return DurationTimesheet{}, err
	}
	if s.Durations == nil || s.Coding == nil {
		return DurationTimesheet{}, ErrUnavailable
	}
	if strings.TrimSpace(req.TimesheetID) == "" || strings.TrimSpace(req.AssignmentRef) == "" || strings.TrimSpace(req.IdempotencyKey) == "" || !validPeriod(req.PeriodStart, req.PeriodEnd) || len(req.Entries) == 0 {
		return DurationTimesheet{}, ErrInvalidRequest
	}
	if err := validateDurationProfile(req.Profile); err != nil {
		return DurationTimesheet{}, err
	}
	if req.Profile.GovernmentContract.DCAATotalTimeAccounting && req.DCAA.StandardDayMinutes <= 0 {
		return DurationTimesheet{}, &Rejection{Field: "dcaa.standard_day_minutes", Reason: "DCAA total-time accounting requires a positive rule-pack day standard", Err: ErrInvalidRequest}
	}
	if err := s.authorize(ctx, p, req.WorkerRef, CapRecordDuration); err != nil {
		return DurationTimesheet{}, err
	}
	scope, err := s.Coding.AuthorizedCoding(ctx, p, string(p.Tenant()), req.WorkerRef)
	if err != nil {
		return DurationTimesheet{}, err
	}

	byDay := map[time.Time][]timecard.EntryEvidence{}
	seen := make(map[string]bool, len(req.Entries))
	allLines := make([]timecard.DurationLine, 0, len(req.Entries))
	for i, evidence := range req.Entries {
		line := evidence.Line
		if err := line.Validate(); err != nil {
			return DurationTimesheet{}, fmt.Errorf("%w: entry %d: %v", ErrInvalidRequest, i, err)
		}
		if evidence.EnteredAt.IsZero() {
			return DurationTimesheet{}, fmt.Errorf("%w: entry %d entry time is required", ErrInvalidRequest, i)
		}
		if seen[line.ID] {
			return DurationTimesheet{}, fmt.Errorf("%w: duplicate line id %q", ErrInvalidRequest, line.ID)
		}
		seen[line.ID] = true
		if err := authorized(scope, line); err != nil {
			return DurationTimesheet{}, fmt.Errorf("%w: entry %d: %v", ErrForbidden, i, err)
		}
		if line.HourType == timecard.HourUncompensatedOT && !req.Profile.GovernmentContract.UncompensatedOvertimeTracked {
			return DurationTimesheet{}, fmt.Errorf("%w: uncompensated overtime tracking is not enabled by the profile", ErrInvalidRequest)
		}
		day := dayStart(line.WorkDate)
		if day.Before(dayStart(req.PeriodStart)) || !day.Before(dayStart(req.PeriodEnd)) {
			return DurationTimesheet{}, fmt.Errorf("%w: line %q is outside the timesheet period", ErrInvalidRequest, line.ID)
		}
		line.WorkDate = day
		evidence.Line = line
		byDay[day] = append(byDay[day], evidence)
		allLines = append(allLines, line)
	}

	days := make([]time.Time, 0, len(byDay))
	for day := range byDay {
		days = append(days, day)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	outDays := make([]timecard.DayEntry, 0, len(days))
	total := 0
	for _, day := range days {
		entries := byDay[day]
		sort.Slice(entries, func(i, j int) bool { return entries[i].Line.ID < entries[j].Line.ID })
		dayEntry, err := timecard.RecordDCAADay(timecard.DCAAProfile{
			Enabled:            req.Profile.GovernmentContract.DCAATotalTimeAccounting,
			StandardDayMinutes: req.DCAA.StandardDayMinutes,
			LateEntryThreshold: req.DCAA.LateEntryThreshold,
		}, entries)
		if err != nil {
			return DurationTimesheet{}, err
		}
		total += dayEntry.TotalMinutes
		outDays = append(outDays, dayEntry)
	}

	var grantReport timecard.GrantReportKind
	if req.Profile.Grant.ActivityReportRequired || req.Profile.Grant.SemiAnnualCertification {
		grantReport, err = timecard.RequiredGrantReport(allLines)
		if err != nil {
			return DurationTimesheet{}, err
		}
		if req.Profile.Grant.ActivityReportRequired {
			grantReport = timecard.GrantPeriodActivityReport
		}
	}
	result := DurationTimesheet{ID: req.TimesheetID, Tenant: string(p.Tenant()), WorkerRef: req.WorkerRef, AssignmentRef: req.AssignmentRef, ProfileRef: req.ProfileRef, PeriodStart: req.PeriodStart.UTC(), PeriodEnd: req.PeriodEnd.UTC(), Revision: 1, Days: outDays, TotalMinutes: total, GrantReport: grantReport, RecordedAt: s.now()}
	return s.Durations.CreateDuration(ctx, result.Tenant, result, req.IdempotencyKey)
}

// CorrectDuration routes an immutable line correction through a separately
// verified supervisor approval before the append-only store is touched.
func (s Service) CorrectDuration(ctx context.Context, p *trust.Principal, req DurationCorrectionRequest) (DurationTimesheet, error) {
	if err := principalOK(p); err != nil {
		return DurationTimesheet{}, err
	}
	if s.Durations == nil || s.Approvals == nil {
		return DurationTimesheet{}, ErrUnavailable
	}
	if strings.TrimSpace(req.TimesheetID) == "" || strings.TrimSpace(req.IdempotencyKey) == "" || req.ExpectedRevision == 0 {
		return DurationTimesheet{}, ErrInvalidRequest
	}
	if err := s.authorize(ctx, p, req.WorkerRef, CapCorrectDuration); err != nil {
		return DurationTimesheet{}, err
	}
	correction := timecard.DurationCorrection{Original: req.Original, Corrected: req.Corrected, Reason: req.Reason, SupervisorApprovalRef: req.SupervisorApprovalRef}
	if _, err := timecard.CorrectDurationLine(correction); err != nil {
		return DurationTimesheet{}, err
	}
	if err := s.Approvals.VerifyDurationCorrection(ctx, p, string(p.Tenant()), req.WorkerRef, correction, req.TimesheetID); err != nil {
		return DurationTimesheet{}, err
	}
	return s.Durations.CorrectDuration(ctx, string(p.Tenant()), DurationCorrectionRecord{TimesheetID: req.TimesheetID, WorkerRef: req.WorkerRef, ExpectedRevision: req.ExpectedRevision, Correction: correction, SupervisorApprovalRef: req.SupervisorApprovalRef, ActorRef: p.Subject(), RecordedAt: s.now()}, req.IdempotencyKey)
}

// ResolveExceptionMode applies the jurisdiction rule pack. It returns
// duration or punch when actual daily recording is required; it never
// silently coerces a non-exempt worker to exception-only.
func (s Service) ResolveExceptionMode(ctx context.Context, p *trust.Principal, worker string, classification timecard.WorkerClassification, jurisdiction timecard.RecordingDutyProfile) (timeprofile.CaptureMode, error) {
	if err := s.authorize(ctx, p, worker, CapCreateException); err != nil {
		return "", err
	}
	return timecard.SelectCaptureMode(classification, jurisdiction)
}

// CreateExceptionPeriod creates an unconfirmed exception-only period only
// when the rule pack actually permits that mode.
func (s Service) CreateExceptionPeriod(ctx context.Context, p *trust.Principal, req ExceptionRequest) (ExceptionRecord, error) {
	if err := principalOK(p); err != nil {
		return ExceptionRecord{}, err
	}
	if s.Exceptions == nil {
		return ExceptionRecord{}, ErrUnavailable
	}
	if strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.PatternRef) == "" || strings.TrimSpace(req.IdempotencyKey) == "" || !validPeriod(req.PeriodStart, req.PeriodEnd) {
		return ExceptionRecord{}, ErrInvalidRequest
	}
	if err := s.authorize(ctx, p, req.WorkerRef, CapCreateException); err != nil {
		return ExceptionRecord{}, err
	}
	mode, err := timecard.SelectCaptureMode(timecard.WorkerClassification{ExemptionStatus: req.Profile.Exemption, PayBasis: req.Profile.PayBasis}, req.Jurisdiction)
	if err != nil {
		return ExceptionRecord{}, err
	}
	if mode != timeprofile.CaptureException || req.Profile.Capture != timeprofile.CaptureException {
		return ExceptionRecord{}, fmt.Errorf("%w: lawful capture resolves to %s", ErrModeRejected, mode)
	}
	period, err := timecard.NewExceptionPeriod(req.WorkerRef, req.PatternRef, mode, req.Deviations)
	if err != nil {
		return ExceptionRecord{}, err
	}
	record := ExceptionRecord{ID: req.ID, Tenant: string(p.Tenant()), WorkerRef: req.WorkerRef, PeriodStart: req.PeriodStart.UTC(), PeriodEnd: req.PeriodEnd.UTC(), Revision: 1, Period: period, RecordedAt: s.now()}
	return s.Exceptions.CreateException(ctx, record.Tenant, record, req.IdempotencyKey)
}

// ConfirmExceptionPeriod confirms the assumed schedule as the worker and
// advances the store's revision fence.
func (s Service) ConfirmExceptionPeriod(ctx context.Context, p *trust.Principal, req ConfirmExceptionRequest) (ExceptionRecord, error) {
	if err := principalOK(p); err != nil {
		return ExceptionRecord{}, err
	}
	if s.Exceptions == nil {
		return ExceptionRecord{}, ErrUnavailable
	}
	if strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.IdempotencyKey) == "" || req.ExpectedRevision == 0 {
		return ExceptionRecord{}, ErrInvalidRequest
	}
	if err := s.authorize(ctx, p, req.WorkerRef, CapConfirmException); err != nil {
		return ExceptionRecord{}, err
	}
	record, err := s.Exceptions.GetException(ctx, string(p.Tenant()), req.ID)
	if err != nil {
		return ExceptionRecord{}, err
	}
	if record.Tenant != string(p.Tenant()) || record.WorkerRef != req.WorkerRef || record.Revision != req.ExpectedRevision {
		return ExceptionRecord{}, ErrRevisionConflict
	}
	confirmed, err := timecard.ConfirmExceptionPeriod(record.Period, p.Subject(), s.now())
	if err != nil {
		return ExceptionRecord{}, err
	}
	return s.Exceptions.ConfirmException(ctx, record.Tenant, record.ID, req.ExpectedRevision, confirmed, p.Subject(), req.IdempotencyKey)
}

// ValidateSalaryDeduction delegates partial-day salary-basis policy to the
// rule pack represented by the domain value.
func (s Service) ValidateSalaryDeduction(ctx context.Context, p *trust.Principal, req SalaryDeductionRequest) error {
	if err := s.authorize(ctx, p, req.WorkerRef, CapReviewDuration); err != nil {
		return err
	}
	return timecard.ValidateSalaryDeduction(req.Deviation, req.Worker, req.Rule)
}
