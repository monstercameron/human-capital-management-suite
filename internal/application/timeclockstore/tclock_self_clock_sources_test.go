package timeclockstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

type selfClockDirectoryFake struct {
	active, assigned   bool
	worker, assignment string
	status             clockservice.WorkerStatusResult
	err                error
}

func (d selfClockDirectoryFake) ResolveWorker(context.Context, string, string) (string, bool, error) {
	return d.worker, d.active, d.err
}
func (d selfClockDirectoryFake) ResolveAssignment(context.Context, string, string, string) (string, string, bool, error) {
	return "project-1", "site-1", d.assigned, d.err
}
func (d selfClockDirectoryFake) CurrentAssignment(context.Context, string, string, time.Time) (string, string, string, bool, error) {
	return d.assignment, "project-1", "site-1", d.assigned, d.err
}
func (d selfClockDirectoryFake) ResolveWorkerStatus(context.Context, string, string, string) (clockservice.WorkerStatusResult, error) {
	return d.status, d.err
}

type selfClockTokenFake struct {
	claims clockservice.DeviceWorkerTokenClaims
	err    error
}

func (f selfClockTokenFake) VerifyDeviceWorkerToken(context.Context, string) (clockservice.DeviceWorkerTokenClaims, error) {
	return f.claims, f.err
}

type selfClockProfileFake struct{ err error }

func (f selfClockProfileFake) Resolve(context.Context, string, string, string, time.Time) (timeprofile.TimeProfile, error) {
	return timeprofile.TimeProfile{}, f.err
}

type selfClockContextProfileFake struct{ err error }

func (f selfClockContextProfileFake) Resolve(context.Context, string, string, string, time.Time) (timeprofile.TimeProfile, error) {
	return timeprofile.TimeProfile{}, f.err
}

type selfClockSessionFake struct {
	record clockservice.SessionRecord
	err    error
}

func (f selfClockSessionFake) OpenSession(context.Context, string, clockservice.SessionRecord, clockservice.SessionEvent) (clockservice.SessionRecord, error) {
	return clockservice.SessionRecord{}, errors.New("not used")
}
func (f selfClockSessionFake) ApplyTransition(context.Context, string, string, uint64, clockservice.SessionRecord, []clockservice.SessionEvent) (clockservice.SessionRecord, error) {
	return clockservice.SessionRecord{}, errors.New("not used")
}
func (f selfClockSessionFake) CurrentSession(context.Context, string, string, string) (clockservice.SessionRecord, error) {
	return f.record, f.err
}

type selfClockObservationFake struct {
	rows []clockservice.ObservationRecord
	err  error
}

func (f selfClockObservationFake) AppendObservation(context.Context, string, clockservice.ObservationRecord) (clockservice.ObservationRecord, bool, error) {
	return clockservice.ObservationRecord{}, false, errors.New("not used")
}
func (f selfClockObservationFake) ListObservations(_ context.Context, _, _ string, _, _ time.Time, cursor string, limit int) ([]clockservice.ObservationRecord, string, error) {
	if cursor != "" || limit != 100 {
		return nil, "", errors.New("unexpected observation bounds")
	}
	return f.rows, "", f.err
}

type selfClockLabelsFake struct {
	status       clockservice.SelfClockStatus
	err          error
	session      clockservice.SessionRecord
	observations []clockservice.ObservationRecord
}

func (f *selfClockLabelsFake) Labels(_ context.Context, _, _, _ string, session clockservice.SessionRecord, observations []clockservice.ObservationRecord) (clockservice.SelfClockStatus, error) {
	f.session, f.observations = session, observations
	return f.status, f.err
}

func TestTodo_FTIME_003_WorkforceSelfSourceResolvesCurrentAssignmentAndStatus(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	directory := selfClockDirectoryFake{active: true, assigned: true, worker: "worker-1", assignment: "assignment-1", status: clockservice.WorkerStatusResult{DisplayName: "Ana Flores"}}
	source := WorkforceSelfSource{Directory: directory, Clock: func() time.Time { return now }}
	got, err := source.ResolveSelfWorker(context.Background(), "tenant-1", "subject-1")
	if err != nil || got.WorkerRef != "worker-1" || got.AssignmentRef != "assignment-1" || got.DisplayName != "Ana Flores" || !got.Active {
		t.Fatalf("worker=%+v err=%v", got, err)
	}
	for _, tc := range []struct {
		name   string
		source WorkforceSelfSource
		want   error
	}{
		{"missing directory", WorkforceSelfSource{Clock: source.Clock}, clockservice.ErrUnavailable},
		{"missing clock", WorkforceSelfSource{Directory: directory}, clockservice.ErrUnavailable},
		{"inactive worker", WorkforceSelfSource{Directory: selfClockDirectoryFake{worker: "worker-1"}, Clock: source.Clock}, clockservice.ErrWorkerNotEligible},
		{"no assignment", WorkforceSelfSource{Directory: selfClockDirectoryFake{active: true, worker: "worker-1"}, Clock: source.Clock}, clockservice.ErrWorkerNotEligible},
		{"zero clock", WorkforceSelfSource{Directory: directory, Clock: func() time.Time { return time.Time{} }}, clockservice.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.source.ResolveSelfWorker(context.Background(), "tenant-1", "subject-1"); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
		})
	}
}

func TestTodo_FTIME_003_PunchContextRequiresCurrentTenantDeviceWorkerAndProfile(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	claims := clockservice.DeviceWorkerTokenClaims{TenantID: "tenant-1", DeviceID: "device-1", WorkerID: "worker-1", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}
	directory := selfClockDirectoryFake{active: true, assigned: true, worker: "worker-1", assignment: "assignment-1"}
	source := ProductionPunchContextSource{Tokens: selfClockTokenFake{claims: claims}, Workers: ProductionWorkerDirectory{Source: directory}, Profiles: selfClockContextProfileFake{}}
	worker, assignment, err := source.ResolvePunchContext(context.Background(), "tenant-1", "device-1", "opaque-token", now)
	if err != nil || worker != "worker-1" || assignment != "assignment-1" {
		t.Fatalf("worker=%q assignment=%q err=%v", worker, assignment, err)
	}
	for _, tc := range []struct {
		name                  string
		source                ProductionPunchContextSource
		tenant, device, token string
		at                    time.Time
		want                  error
	}{
		{"missing ports", ProductionPunchContextSource{}, "tenant-1", "device-1", "token", now, clockservice.ErrUnavailable},
		{"tenant mismatch", source, "other", "device-1", "opaque-token", now, clockservice.ErrInvalidDeviceWorkerToken},
		{"device mismatch", source, "tenant-1", "other", "opaque-token", now, clockservice.ErrInvalidDeviceWorkerToken},
		{"token expired", source, "tenant-1", "device-1", "opaque-token", claims.ExpiresAt, clockservice.ErrInvalidDeviceWorkerToken},
		{"inactive worker", ProductionPunchContextSource{Tokens: selfClockTokenFake{claims: claims}, Workers: ProductionWorkerDirectory{Source: selfClockDirectoryFake{}}, Profiles: selfClockContextProfileFake{}}, "tenant-1", "device-1", "opaque-token", now, clockservice.ErrWorkerNotEligible},
		{"missing assignment", ProductionPunchContextSource{Tokens: selfClockTokenFake{claims: claims}, Workers: ProductionWorkerDirectory{Source: selfClockDirectoryFake{active: true, worker: "worker-1"}}, Profiles: selfClockContextProfileFake{}}, "tenant-1", "device-1", "opaque-token", now, clockservice.ErrAssignmentNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := tc.source.ResolvePunchContext(context.Background(), tc.tenant, tc.device, tc.token, tc.at); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
		})
	}
}

func TestTodo_FTIME_003_TimestoreProjectionSourceReadsFactsAndFailsClosed(t *testing.T) {
	observations := selfClockObservationFake{rows: []clockservice.ObservationRecord{{ID: "break", EventType: "BREAK_START"}}}
	labels := &selfClockLabelsFake{status: clockservice.SelfClockStatus{StatusCode: "ON_BREAK", Revision: 5}}
	source := TimestoreProjectionSource{Sessions: selfClockSessionFake{record: clockservice.SessionRecord{ID: "session"}}, Observations: observations, Labels: labels}
	got, err := source.ReadSelfClock(context.Background(), "tenant-1", "worker-1", "assignment-1")
	if err != nil || got.StatusCode != "ON_BREAK" || got.Revision != 5 || labels.session.ID != "session" || len(labels.observations) != 1 || labels.observations[0].ID != "break" {
		t.Fatalf("status=%+v session=%+v observations=%+v err=%v", got, labels.session, labels.observations, err)
	}
	noSession := TimestoreProjectionSource{Sessions: selfClockSessionFake{err: timestore.ErrNotFound}, Observations: observations, Labels: labels}
	if _, err := noSession.ReadSelfClock(context.Background(), "tenant-1", "worker-1", "assignment-1"); err != nil || labels.session.ID != "" {
		t.Fatalf("missing session err=%v labels session=%+v", err, labels.session)
	}
	for _, tc := range []struct {
		name   string
		source TimestoreProjectionSource
	}{
		{"missing dependencies", TimestoreProjectionSource{}},
		{"session read failure", TimestoreProjectionSource{Sessions: selfClockSessionFake{err: errors.New("session read")}, Observations: observations, Labels: labels}},
		{"observation read failure", TimestoreProjectionSource{Sessions: selfClockSessionFake{}, Observations: selfClockObservationFake{err: errors.New("observation read")}, Labels: labels}},
		{"label failure", TimestoreProjectionSource{Sessions: selfClockSessionFake{}, Observations: observations, Labels: &selfClockLabelsFake{err: errors.New("labels")}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.source.ReadSelfClock(context.Background(), "tenant-1", "worker-1", "assignment-1"); err == nil {
				t.Fatal("projection read unexpectedly succeeded")
			}
		})
	}
}

func TestTodo_FTIME_003_CatalogClockLabelsLocalizeCurrentBreakAndLastEvent(t *testing.T) {
	labels := CatalogClockLabels{Locale: workspace.ResolveLocale("de-DE"), WorkerLabel: func(worker string) string { return worker }, ScheduleLabel: func(worker string) string { return "Shift " + worker }}
	last := time.Date(2026, 9, 29, 11, 0, 0, 0, time.UTC)
	got, err := labels.ProjectionLabels(context.Background(), "tenant-1", "worker-1", "assignment-1", "ON_BREAK", last, 4)
	if err != nil || got.WorkerLabel != "worker-1" || got.ScheduleLabel != "Shift worker-1" || got.StatusLabel != workspace.TranslateSelfClockStatus(labels.Locale, "ON_BREAK") || got.LastEventLabel != last.Format(time.RFC3339) || got.Revision != 4 {
		t.Fatalf("labels=%+v err=%v", got, err)
	}
	legacy, err := labels.Labels(context.Background(), "tenant-1", "worker-1", "assignment-1", clockservice.SessionRecord{}, []clockservice.ObservationRecord{{OccurredAt: last}})
	if err != nil || legacy.LastEventLabel != last.Format(time.RFC3339) || legacy.StatusCode != "" || legacy.Revision != 1 || legacy.StatusLabel != workspace.TranslateSelfClockStatus(labels.Locale, "CLOCKED_OUT") {
		t.Fatalf("legacy labels=%+v err=%v", legacy, err)
	}
	if _, err := (CatalogClockLabels{}).Labels(context.Background(), "t", "w", "a", clockservice.SessionRecord{}, nil); !errors.Is(err, clockservice.ErrUnavailable) {
		t.Fatalf("missing localizers error=%v", err)
	}
	for _, tc := range []struct {
		name     string
		status   string
		revision uint64
		want     error
	}{
		{"missing localizer", "ON_BREAK", 4, clockservice.ErrUnavailable},
		{"zero revision", "ON_BREAK", 0, clockservice.ErrUnavailable},
		{"unknown status", "UNKNOWN", 4, clockservice.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := labels
			if tc.name == "missing localizer" {
				candidate.WorkerLabel = nil
			}
			_, err := candidate.ProjectionLabels(context.Background(), "t", "w", "a", tc.status, time.Time{}, tc.revision)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
		})
	}
}

func TestTodo_FTIME_003_PublishedProfileSourceNamesMissingPin(t *testing.T) {
	store, tenant := adapterFixture(t)
	_, err := (PublishedProfileSource{Store: store}).ResolvePublishedProfile(context.Background(), tenant, "worker-1", "assignment-1", time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC))
	if reason, ok := clockservice.ReasonOf(err); !ok || reason != clockservice.ReasonNoTimeProfile {
		t.Fatalf("missing profile reason=%v ok=%v err=%v", reason, ok, err)
	}
}
