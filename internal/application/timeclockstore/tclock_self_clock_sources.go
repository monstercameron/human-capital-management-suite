package timeclockstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

// WorkforceSelfSource reads worker identity and current assignment from the
// authoritative workforce reader already used by device clock authority.
type WorkforceSelfSource struct {
	Directory AuthoritativeWorkerDirectory
	Clock     func() time.Time
}

func (s WorkforceSelfSource) ResolveSelfWorker(ctx context.Context, tenant, subject string) (clockservice.SelfWorker, error) {
	if s.Directory == nil || s.Clock == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(subject) == "" {
		return clockservice.SelfWorker{}, clockservice.ErrUnavailable
	}
	worker, active, err := s.Directory.ResolveWorker(ctx, tenant, subject)
	if err != nil {
		return clockservice.SelfWorker{}, err
	}
	if !active || worker == "" {
		return clockservice.SelfWorker{}, clockservice.ErrWorkerNotEligible
	}
	assignmentSource, ok := s.Directory.(CurrentAssignmentDirectory)
	if !ok {
		return clockservice.SelfWorker{}, clockservice.ErrUnavailable
	}
	at := s.Clock().UTC()
	if at.IsZero() {
		return clockservice.SelfWorker{}, clockservice.ErrUnavailable
	}
	assignment, _, _, found, err := assignmentSource.CurrentAssignment(ctx, tenant, worker, at)
	if err != nil {
		return clockservice.SelfWorker{}, err
	}
	if !found || assignment == "" {
		return clockservice.SelfWorker{}, clockservice.ErrWorkerNotEligible
	}
	statusSource := ProductionWorkerStatusSource{Directory: s.Directory}
	status, err := statusSource.ResolveWorkerStatus(ctx, clockservice.DeviceWorkerTokenClaims{TenantID: tenant, WorkerID: worker})
	if err != nil {
		return clockservice.SelfWorker{}, err
	}
	return clockservice.SelfWorker{WorkerRef: worker, AssignmentRef: assignment, DisplayName: status.DisplayName, Active: true}, nil
}

// PublishedProfileSource reads the effective immutable profile pin from the
// timestore's assignment and profile-version records.
type PublishedProfileSource struct{ Store *timestore.Store }

func (s PublishedProfileSource) ResolvePublishedProfile(ctx context.Context, tenant, worker, assignment string, at time.Time) (timeprofile.TimeProfile, error) {
	profile, err := TimestoreProfileResolver{Store: s.Store}.Resolve(ctx, tenant, worker, assignment, at)
	if errors.Is(err, timestore.ErrNotFound) {
		// A missing pin or an unpublished version is an answer about this
		// worker, not an outage: name it so the page can say so.
		return timeprofile.TimeProfile{}, clockservice.NotEligible(clockservice.ReasonNoTimeProfile, "no published time profile is pinned to the assignment")
	}
	return profile, err
}

// TimestoreProjectionSource reads the durable session projection and asks an
// injected localization source for user-facing labels.
type TimestoreProjectionSource struct {
	Sessions     clockservice.SessionStore
	Observations clockservice.ObservationStore
	Labels       SelfClockLabels
}

// SelfClockLabels localizes labels from authoritative session/observation
// facts. It must be backed by the product localization catalog in production.
type SelfClockLabels interface {
	Labels(context.Context, string, string, string, clockservice.SessionRecord, []clockservice.ObservationRecord) (clockservice.SelfClockStatus, error)
}

// SelfClockProjectionLabels localizes persisted projection facts directly;
// it never reconstructs a session or derives a CAS revision from observations.
type SelfClockProjectionLabels interface {
	ProjectionLabels(context.Context, string, string, string, string, time.Time, uint64) (clockservice.SelfClockStatus, error)
}

// CatalogClockLabels is the production catalog-backed labeler. Worker and
// schedule names remain upstream facts supplied by the callbacks; this type
// localizes status and last-event vocabulary through the reviewed catalog.
type CatalogClockLabels struct {
	Locale        workspace.LocaleContext
	WorkerLabel   func(string) string
	ScheduleLabel func(string) string
}

func (l CatalogClockLabels) Labels(_ context.Context, _ string, worker, _ string, session clockservice.SessionRecord, observations []clockservice.ObservationRecord) (clockservice.SelfClockStatus, error) {
	if l.WorkerLabel == nil || l.ScheduleLabel == nil {
		return clockservice.SelfClockStatus{}, clockservice.ErrUnavailable
	}
	statusCode := "CLOCKED_OUT"
	last := "No event"
	if session.ID != "" {
		statusCode = "CLOCKED_IN"
	}
	if len(observations) > 0 {
		last = observations[len(observations)-1].OccurredAt.UTC().Format(time.RFC3339)
	}
	localizedStatus := workspace.TranslateSelfClockStatus(l.Locale, statusCode)
	localizedLast := workspace.TranslateSelfClockLastEvent(l.Locale, last)
	return clockservice.SelfClockStatus{WorkerLabel: l.WorkerLabel(worker), ScheduleLabel: l.ScheduleLabel(worker), StatusLabel: localizedStatus, LastEventLabel: localizedLast, Revision: uint64(len(observations))}, nil
}

func (l CatalogClockLabels) ProjectionLabels(_ context.Context, _ string, worker, _ string, statusCode string, lastEvent time.Time, revision uint64) (clockservice.SelfClockStatus, error) {
	if l.WorkerLabel == nil || l.ScheduleLabel == nil || revision == 0 {
		return clockservice.SelfClockStatus{}, clockservice.ErrUnavailable
	}
	switch statusCode {
	case "CLOCKED_IN", "OPEN", "ON_BREAK", "CLOCKED_OUT":
	default:
		return clockservice.SelfClockStatus{}, clockservice.ErrUnavailable
	}
	last := "No event"
	if !lastEvent.IsZero() && !lastEvent.Equal(time.Unix(0, 0).UTC()) {
		last = lastEvent.UTC().Format(time.RFC3339)
	}
	localizedStatus := workspace.TranslateSelfClockStatus(l.Locale, statusCode)
	localizedLast := workspace.TranslateSelfClockLastEvent(l.Locale, last)
	return clockservice.SelfClockStatus{WorkerLabel: l.WorkerLabel(worker), ScheduleLabel: l.ScheduleLabel(worker), StatusLabel: localizedStatus, LastEventLabel: localizedLast, Revision: revision}, nil
}

func (s TimestoreProjectionSource) ReadSelfClock(ctx context.Context, tenant, worker, assignment string) (clockservice.SelfClockStatus, error) {
	if s.Sessions == nil || s.Observations == nil || s.Labels == nil {
		return clockservice.SelfClockStatus{}, clockservice.ErrUnavailable
	}
	session, err := s.Sessions.CurrentSession(ctx, tenant, worker, assignment)
	if err != nil && !errors.Is(err, timestore.ErrNotFound) {
		return clockservice.SelfClockStatus{}, err
	}
	observations, _, err := s.Observations.ListObservations(ctx, tenant, worker, time.Time{}, time.Time{}, "", 100)
	if err != nil {
		return clockservice.SelfClockStatus{}, err
	}
	return s.Labels.Labels(ctx, tenant, worker, assignment, session, observations)
}
