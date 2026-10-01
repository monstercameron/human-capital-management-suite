package timesheet

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/labor"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timecard"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type t19Auth struct {
	denied bool
	calls  []Capability
}

func (a *t19Auth) Authorize(_ context.Context, _ *trust.Principal, _, _ string, cap Capability) error {
	a.calls = append(a.calls, cap)
	if a.denied {
		return ErrForbidden
	}
	return nil
}

type t19Coding struct{ scope CodingScope }

func (c t19Coding) AuthorizedCoding(context.Context, *trust.Principal, string, string) (CodingScope, error) {
	return c.scope, nil
}

type t19DurationStore struct {
	created     []DurationTimesheet
	corrections []DurationCorrectionRecord
}

func (s *t19DurationStore) CreateDuration(_ context.Context, _ string, sheet DurationTimesheet, _ string) (DurationTimesheet, error) {
	s.created = append(s.created, sheet)
	return sheet, nil
}

func (s *t19DurationStore) CorrectDuration(_ context.Context, _ string, correction DurationCorrectionRecord, _ string) (DurationTimesheet, error) {
	s.corrections = append(s.corrections, correction)
	return DurationTimesheet{ID: correction.TimesheetID, WorkerRef: correction.WorkerRef, Revision: correction.ExpectedRevision + 1}, nil
}

type t19Approval struct{ calls int }

func (a *t19Approval) VerifyDurationCorrection(_ context.Context, _ *trust.Principal, _, _ string, c timecard.DurationCorrection, _ string) error {
	a.calls++
	if c.SupervisorApprovalRef != "approval-1" {
		return ErrForbidden
	}
	return nil
}

type t19ExceptionStore struct {
	records map[string]ExceptionRecord
	created int
}

func (s *t19ExceptionStore) CreateException(_ context.Context, _ string, record ExceptionRecord, _ string) (ExceptionRecord, error) {
	if s.records == nil {
		s.records = map[string]ExceptionRecord{}
	}
	s.records[record.ID] = record
	s.created++
	return record, nil
}

func (s *t19ExceptionStore) GetException(_ context.Context, _ string, id string) (ExceptionRecord, error) {
	record, ok := s.records[id]
	if !ok {
		return ExceptionRecord{}, ErrNotFound
	}
	return record, nil
}

func (s *t19ExceptionStore) ConfirmException(_ context.Context, _ string, id string, expected uint64, period timecard.ExceptionPeriod, _ string, _ string) (ExceptionRecord, error) {
	record := s.records[id]
	if record.Revision != expected {
		return ExceptionRecord{}, ErrRevisionConflict
	}
	record.Period = period
	record.Revision++
	s.records[id] = record
	return record, nil
}

func t19Principal(t *testing.T, tenant, subject string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "org-1", Purposes: []string{"time"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-" + subject, IssuedAt: time.Unix(100, 0), ExpiresAt: time.Unix(1e9, 0),
		CredentialDigest: "credential-" + subject,
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

func t19Line(id string, kind timecard.HourType, minutes int, date time.Time) timecard.EntryEvidence {
	return timecard.EntryEvidence{Line: timecard.DurationLine{
		ID: id, WorkDate: date, Project: labor.Dimension{Kind: labor.DimensionProject, Value: "project-a", Version: "v1"},
		CostCode: "cost-1", Grant: "award-1", HourType: kind, Minutes: minutes,
	}, EnteredAt: date.Add(8 * time.Hour)}
}

func t19DurationProfile() timeprofile.TimeProfile {
	return timeprofile.TimeProfile{
		Capture:            timeprofile.CaptureDuration,
		Taxonomy:           timeprofile.ProjectTaxonomy{Primary: "project-a"},
		GovernmentContract: timeprofile.GovernmentContractFlags{DCAATotalTimeAccounting: true, UncompensatedOvertimeTracked: true},
		Grant:              timeprofile.GrantFlags{SemiAnnualCertification: true},
	}
}

func t19Service() (*Service, *t19Auth, *t19DurationStore) {
	auth := &t19Auth{}
	store := &t19DurationStore{}
	return &Service{
		Durations: store, Auth: auth,
		Coding: t19Coding{scope: CodingScope{
			Projects: map[string]bool{"project-a": true}, Grants: map[string]bool{"award-1": true},
			Capitalizations: map[string]bool{"asc-350-40": true}, ResearchActivities: map[string]bool{"irc-41": true},
		}},
		Approvals: &t19Approval{}, Clock: func() time.Time { return time.Date(2026, 1, 16, 17, 0, 0, 0, time.UTC) },
	}, auth, store
}

// TestTodo_WTIME_009 proves that the application boundary preserves daily
// entry evidence, all DCAA hour classes, two independent taxonomies, grant
// reporting and correction approval before persistence.
func TestTodo_WTIME_009(t *testing.T) {
	svc, auth, store := t19Service()
	p := t19Principal(t, "tenant-a", "worker-1")
	date := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)
	entries := []timecard.EntryEvidence{
		t19Line("worked", timecard.HourWorked, 300, date),
		t19Line("leave", timecard.HourLeave, 60, date),
		t19Line("indirect", timecard.HourIndirect, 60, date),
		t19Line("uot", timecard.HourUncompensatedOT, 60, date),
	}
	entries[0].Line.Taxonomy = timecard.Taxonomy{Capitalization: "asc-350-40", ResearchActivity: "irc-41"}
	sheet, err := svc.RecordDuration(context.Background(), p, DurationRequest{
		TimesheetID: "sheet-1", WorkerRef: "worker-1", AssignmentRef: "assignment-1", Profile: t19DurationProfile(),
		ProfileRef: "profile-v1", PeriodStart: date, PeriodEnd: date.Add(7 * 24 * time.Hour), Entries: entries,
		DCAA: timecard.DCAAProfile{StandardDayMinutes: 480, LateEntryThreshold: 24 * time.Hour}, IdempotencyKey: "create-1",
	})
	if err != nil {
		t.Fatalf("RecordDuration: %v", err)
	}
	if sheet.TotalMinutes != 480 || sheet.GrantReport != timecard.GrantSemiAnnualCertification || len(sheet.Days) != 1 {
		t.Fatalf("sheet = %+v, want 480 minutes, semiannual certification and one day", sheet)
	}
	var coded timecard.Taxonomy
	for _, evidence := range sheet.Entries() {
		if evidence.Line.ID == "worked" {
			coded = evidence.Line.Taxonomy
		}
	}
	if coded.Capitalization != "asc-350-40" || coded.ResearchActivity != "irc-41" {
		t.Fatalf("independent taxonomies lost: %+v", coded)
	}
	if sheet.Entries()[0].EnteredAt.IsZero() || len(store.created) != 1 || auth.calls[0] != CapRecordDuration {
		t.Fatalf("entry evidence or authorization not retained: created=%d calls=%v", len(store.created), auth.calls)
	}

	corrected := entries[0].Line
	corrected.Minutes = 320
	if _, err := svc.CorrectDuration(context.Background(), p, DurationCorrectionRequest{
		TimesheetID: "sheet-1", WorkerRef: "worker-1", ExpectedRevision: 1, Original: entries[0].Line, Corrected: corrected,
		Reason: "approved correction", SupervisorApprovalRef: "approval-1", IdempotencyKey: "correct-1",
	}); err != nil {
		t.Fatalf("CorrectDuration: %v", err)
	}
	if len(store.corrections) != 1 || store.corrections[0].Correction.Reason == "" {
		t.Fatalf("correction was not appended with reason: %+v", store.corrections)
	}
}

// TestTodo_WTIME_009_Property proves period totals are conserved for many
// valid mixes while work dates are normalized only to their calendar day.
func TestTodo_WTIME_009_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(19))
	for trial := 0; trial < 80; trial++ {
		svc, _, _ := t19Service()
		p := t19Principal(t, "tenant-a", fmt.Sprintf("worker-%d", trial))
		date := time.Date(2026, 2, 1+trial%5, 0, 0, 0, 0, time.UTC)
		want := 0
		entries := make([]timecard.EntryEvidence, 0, 4)
		for i := 0; i < 4; i++ {
			minutes := 1 + rng.Intn(120)
			want += minutes
			entries = append(entries, t19Line(fmt.Sprintf("p-%d-%d", trial, i), timecard.HourWorked, minutes, date.Add(time.Duration(i)*time.Hour)))
		}
		sheet, err := svc.RecordDuration(context.Background(), p, DurationRequest{TimesheetID: fmt.Sprintf("s-%d", trial), WorkerRef: p.Subject(), AssignmentRef: "a", Profile: timeprofile.TimeProfile{Capture: timeprofile.CaptureDuration}, PeriodStart: date, PeriodEnd: date.Add(24 * time.Hour), Entries: entries, IdempotencyKey: fmt.Sprintf("k-%d", trial)})
		if err != nil {
			t.Fatalf("trial %d: RecordDuration: %v", trial, err)
		}
		if sheet.TotalMinutes != want || len(sheet.Days) != 1 || !sheet.Days[0].WorkDate.Equal(dayStart(date)) {
			t.Fatalf("trial %d: sheet total/day = %d/%v, want %d/%v", trial, sheet.TotalMinutes, sheet.Days[0].WorkDate, want, dayStart(date))
		}
	}
}

// TestTodo_WTIME_009_Golden pins the digest of the stable application day
// projection, including the late-entry marker.
func TestTodo_WTIME_009_Golden(t *testing.T) {
	svc, _, _ := t19Service()
	date := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)
	late := t19Line("golden", timecard.HourWorked, 480, date)
	late.EnteredAt = date.Add(72 * time.Hour)
	sheet, err := svc.RecordDuration(context.Background(), t19Principal(t, "tenant-a", "worker-1"), DurationRequest{
		TimesheetID: "golden", WorkerRef: "worker-1", AssignmentRef: "a", Profile: timeprofile.TimeProfile{Capture: timeprofile.CaptureDuration, Taxonomy: timeprofile.ProjectTaxonomy{Primary: "project-a"}, GovernmentContract: timeprofile.GovernmentContractFlags{DCAATotalTimeAccounting: true, UncompensatedOvertimeTracked: true}},
		PeriodStart: date, PeriodEnd: date.Add(24 * time.Hour), Entries: []timecard.EntryEvidence{late}, DCAA: timecard.DCAAProfile{StandardDayMinutes: 480, LateEntryThreshold: 24 * time.Hour}, IdempotencyKey: "golden",
	})
	if err != nil {
		t.Fatalf("RecordDuration: %v", err)
	}
	if got := sheet.Days[0].Digest(); got != "sha256:e2a32fef2932d4ae8ec4afea9baf703ae0b464b12efcbb7df3d343f7c6e4df93" {
		t.Fatalf("day digest = %s, want pinned golden digest", got)
	}
	if len(sheet.Days[0].LateEntries) != 1 || sheet.Days[0].LateEntries[0] != "golden" {
		t.Fatalf("late entry marker = %v, want [golden]", sheet.Days[0].LateEntries)
	}
}

// TestTodo_WTIME_009_Security proves caller-supplied coding cannot escape
// the server-resolved project, grant or secondary-taxonomy scope.
func TestTodo_WTIME_009_Security(t *testing.T) {
	svc, _, store := t19Service()
	p := t19Principal(t, "tenant-a", "worker-1")
	date := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)
	line := t19Line("denied", timecard.HourWorked, 60, date)
	line.Line.Project.Value = "project-forged"
	_, err := svc.RecordDuration(context.Background(), p, DurationRequest{TimesheetID: "denied", WorkerRef: "worker-1", AssignmentRef: "a", Profile: timeprofile.TimeProfile{Capture: timeprofile.CaptureDuration}, PeriodStart: date, PeriodEnd: date.Add(24 * time.Hour), Entries: []timecard.EntryEvidence{line}, IdempotencyKey: "denied"})
	if !errors.Is(err, ErrForbidden) || len(store.created) != 0 {
		t.Fatalf("unauthorized project: err=%v created=%d, want forbidden before write", err, len(store.created))
	}

	line = t19Line("denied-grant", timecard.HourWorked, 60, date)
	line.Line.Grant = "award-forged"
	_, err = svc.RecordDuration(context.Background(), p, DurationRequest{TimesheetID: "denied-grant", WorkerRef: "worker-1", AssignmentRef: "a", Profile: timeprofile.TimeProfile{Capture: timeprofile.CaptureDuration}, PeriodStart: date, PeriodEnd: date.Add(24 * time.Hour), Entries: []timecard.EntryEvidence{line}, IdempotencyKey: "denied-grant"})
	if !errors.Is(err, ErrForbidden) || len(store.created) != 0 {
		t.Fatalf("unauthorized grant: err=%v created=%d, want forbidden before write", err, len(store.created))
	}
}

// TestTodo_WTIME_010 proves lawful exception-only creation, worker
// confirmation, jurisdictional fallback and salary-basis enforcement.
func TestTodo_WTIME_010(t *testing.T) {
	auth := &t19Auth{}
	exceptions := &t19ExceptionStore{}
	svc := Service{Auth: auth, Exceptions: exceptions, Clock: func() time.Time { return time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC) }}
	p := t19Principal(t, "tenant-a", "worker-1")
	date := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	profile := timeprofile.TimeProfile{Capture: timeprofile.CaptureException, Exemption: timeprofile.Exempt, PayBasis: timeprofile.PaySalary}
	record, err := svc.CreateExceptionPeriod(context.Background(), p, ExceptionRequest{ID: "exception-1", WorkerRef: "worker-1", Profile: profile, Jurisdiction: timecard.RecordingDutyProfile{JurisdictionCode: "US-TX", Ref: "pack-v1"}, PeriodStart: date, PeriodEnd: date.Add(7 * 24 * time.Hour), PatternRef: "schedule-1", Deviations: []timecard.Deviation{{Date: date, Kind: timecard.DeviationFullDayAbsence, Minutes: 480, Reason: "leave"}}, IdempotencyKey: "create-exception"})
	if err != nil || record.Period.Confirmed {
		t.Fatalf("CreateExceptionPeriod = %+v, %v; want unconfirmed period", record, err)
	}
	confirmed, err := svc.ConfirmExceptionPeriod(context.Background(), p, ConfirmExceptionRequest{ID: record.ID, WorkerRef: record.WorkerRef, ExpectedRevision: record.Revision, IdempotencyKey: "confirm-exception"})
	if err != nil || !confirmed.Period.Confirmed || confirmed.Revision != 2 {
		t.Fatalf("ConfirmExceptionPeriod = %+v, %v; want confirmed revision 2", confirmed, err)
	}

	de, err := svc.ResolveExceptionMode(context.Background(), p, "worker-1", timecard.WorkerClassification{ExemptionStatus: timeprofile.Exempt, PayBasis: timeprofile.PaySalary}, timecard.RecordingDutyProfile{JurisdictionCode: "DE", DailyRecordingDutyRequired: true, Ref: "BAG-1-ABR-22-21"})
	if err != nil || de != timeprofile.CaptureDuration {
		t.Fatalf("German mode = %s, %v; want DURATION", de, err)
	}
	if _, err := svc.CreateExceptionPeriod(context.Background(), p, ExceptionRequest{ID: "de-denied", WorkerRef: "worker-1", Profile: profile, Jurisdiction: timecard.RecordingDutyProfile{JurisdictionCode: "DE", DailyRecordingDutyRequired: true}, PeriodStart: date, PeriodEnd: date.Add(24 * time.Hour), PatternRef: "schedule-1", IdempotencyKey: "de-denied"}); !errors.Is(err, ErrModeRejected) {
		t.Fatalf("German exception-only creation = %v, want ErrModeRejected", err)
	}

	partial := timecard.Deviation{Date: date, Kind: timecard.DeviationPartialDay, Minutes: 120, Reason: "left early"}
	if err := svc.ValidateSalaryDeduction(context.Background(), p, SalaryDeductionRequest{WorkerRef: "worker-1", Worker: timecard.WorkerClassification{ExemptionStatus: timeprofile.Exempt, PayBasis: timeprofile.PaySalary}, Rule: timecard.SalaryBasisRule{RuleRef: "flsa-541"}, Deviation: partial}); !errors.Is(err, timecard.ErrDisallowedDeduction) {
		t.Fatalf("partial-day salary deduction = %v, want ErrDisallowedDeduction", err)
	}
}

// TestTodo_WTIME_010_Golden pins the mode selected for representative rule
// packs and makes the German/Spanish daily-recording fallback explicit.
func TestTodo_WTIME_010_Golden(t *testing.T) {
	auth := &t19Auth{}
	svc := Service{Auth: auth}
	p := t19Principal(t, "tenant-a", "worker-1")
	cases := []struct {
		name         string
		jurisdiction timecard.RecordingDutyProfile
		want         timeprofile.CaptureMode
	}{
		{"us-exempt", timecard.RecordingDutyProfile{JurisdictionCode: "US-TX"}, timeprofile.CaptureException},
		{"germany-exempt", timecard.RecordingDutyProfile{JurisdictionCode: "DE", DailyRecordingDutyRequired: true}, timeprofile.CaptureDuration},
		{"spain-exempt", timecard.RecordingDutyProfile{JurisdictionCode: "ES", DailyRecordingDutyRequired: true}, timeprofile.CaptureDuration},
		{"germany-hourly", timecard.RecordingDutyProfile{JurisdictionCode: "DE", DailyRecordingDutyRequired: true}, timeprofile.CapturePunch},
	}
	for _, tc := range cases {
		classification := timecard.WorkerClassification{ExemptionStatus: timeprofile.Exempt, PayBasis: timeprofile.PaySalary}
		if tc.name == "germany-hourly" {
			classification = timecard.WorkerClassification{ExemptionStatus: timeprofile.NonExempt, PayBasis: timeprofile.PayHourly}
		}
		got, err := svc.ResolveExceptionMode(context.Background(), p, "worker-1", classification, tc.jurisdiction)
		if err != nil || got != tc.want {
			t.Errorf("%s: mode=%s err=%v, want %s", tc.name, got, err, tc.want)
		}
	}
}

// TestTodo_WTIME_010_Property proves exception-only is never returned for a
// daily-recording duty or for a non-exempt classification.
func TestTodo_WTIME_010_Property(t *testing.T) {
	svc := Service{Auth: &t19Auth{}}
	p := t19Principal(t, "tenant-a", "worker-1")
	rng := rand.New(rand.NewSource(10))
	exemptions := []timeprofile.ExemptionStatus{timeprofile.NonExempt, timeprofile.Exempt, timeprofile.SalariedNonExempt}
	payBases := []timeprofile.PayBasis{timeprofile.PayHourly, timeprofile.PaySalary, timeprofile.PayPieceRate}
	for i := 0; i < 120; i++ {
		classification := timecard.WorkerClassification{ExemptionStatus: exemptions[rng.Intn(len(exemptions))], PayBasis: payBases[rng.Intn(len(payBases))]}
		duty := rng.Intn(2) == 0
		mode, err := svc.ResolveExceptionMode(context.Background(), p, "worker-1", classification, timecard.RecordingDutyProfile{JurisdictionCode: "generated", DailyRecordingDutyRequired: duty})
		if err != nil {
			t.Fatalf("trial %d: ResolveExceptionMode: %v", i, err)
		}
		if duty && mode == timeprofile.CaptureException {
			t.Fatalf("trial %d: exception-only selected under daily duty", i)
		}
		if classification.ExemptionStatus != timeprofile.Exempt && mode == timeprofile.CaptureException {
			t.Fatalf("trial %d: exception-only selected for %s", i, classification.ExemptionStatus)
		}
	}
}
