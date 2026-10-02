package scheduled

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Delivery is retained before admission. Request must be replayed byte-for-byte
// after a crash, including its original context snapshot and deadline.
type Delivery struct {
	TenantID, Key, ScheduleID string
	ControlRevision           uint64
	Firing                    Firing
	Request                   agentrun.Request
	EnqueuedAt                time.Time
}

// Outbox atomically advances the schedule cursor with all frozen deliveries.
// A changed control revision or cursor aborts the entire window transaction.
type Outbox interface {
	AppendWindow(context.Context, Schedule, values.Instant, []Delivery) error
	Pending(context.Context, string, int) ([]Delivery, error)
	GetDelivery(context.Context, string, string) (Delivery, bool, error)
	Acknowledge(context.Context, string, string, Receipt) error
	LoadReceipt(context.Context, string, string) (Receipt, bool, error)
}
type ContextBuilder interface {
	BuildScheduleContext(context.Context, Schedule, schedule.Occurrence) (agentrun.ContextScope, error)
}

// RequestBuilder refreshes source-specific authority before the occurrence is
// frozen. Replay always uses the persisted request without rebuilding it.
type RequestBuilder interface {
	BuildScheduleRequest(context.Context, Schedule, agentrun.Request) (agentrun.Request, error)
}
type OutboxWorker struct {
	owner    *Owner
	outbox   Outbox
	contexts ContextBuilder
	inbox    Inbox
}

func NewOutboxWorker(owner *Owner, outbox Outbox, contexts ContextBuilder, inbox Inbox) (*OutboxWorker, error) {
	if owner == nil || outbox == nil || contexts == nil || inbox == nil {
		return nil, ErrInvalidSchedule
	}
	return &OutboxWorker{owner, outbox, contexts, inbox}, nil
}

// Plan freezes the current eligible occurrence window in the durable source
// outbox. Only Scheduling calculates calendars, DST folds and misfires.
func (w *OutboxWorker) Plan(ctx context.Context, tenant, id string, at time.Time) error {
	s, found, err := w.owner.store.Load(ctx, tenant, id)
	if err != nil {
		return err
	}
	if !found || s.State != StateActive {
		return ErrInactive
	}
	if err := ValidateSchedule(s); err != nil {
		return err
	}
	if err := w.owner.authority.CheckCurrent(ctx, s); err != nil {
		return err
	}
	end := values.NewInstant(at)
	if s.Cursor.Validate() != nil || end.Compare(s.Cursor) <= 0 {
		return ErrInvalidSchedule
	}
	result, err := schedule.CalculateOccurrences(s.Trigger, schedule.OccurrenceRequest{Window: schedule.OccurrenceWindow{Start: s.Cursor, End: end}, Zone: s.Zone, Calendar: s.Calendar, Misfire: s.Misfire, ResumeAt: end})
	if err != nil {
		return err
	}
	result, err = ApplyDST(s, result)
	if err != nil {
		return err
	}
	deliveries := make([]Delivery, 0, len(result.Occurrences))
	for _, occ := range result.Occurrences {
		if occ.Misfire == schedule.MisfireSkipped || occ.Misfire == schedule.MisfireNeedsReview || slices.Contains(s.SkippedKeys, occ.Key) {
			continue
		}
		firing := Firing{Occurrence: occ, Target: *s.Trigger.Definition.AgentRun}
		source, err := firing.SourceIdentity()
		if err != nil {
			return err
		}
		// The stable source key is also the source-owned lookup reference, allowing
		// a durable reader to restore identity without trusting a caller's key.
		source.Ref = source.Key
		if s.SourceKind == agentrun.SourceAnnouncement {
			source.Kind = agentrun.SourceAnnouncement
		}
		contextScope, err := w.contexts.BuildScheduleContext(ctx, s, occ)
		if err != nil {
			return err
		}
		request := agentrun.Request{Source: source, Agent: targetAgentRef(firing.Target), InstallationID: s.InstallationID, LegalEntity: s.LegalEntity, Principal: agentrun.PrincipalChain{Mode: agentrun.ModeSponsored, AgentPrincipalID: s.AgentPrincipalID, SponsorID: firing.Target.SponsorID}, Purpose: firing.Target.Purpose, Audience: targetAudience(firing.Target.Destination), Context: contextScope, Deadline: at.UTC().Add(s.RunTimeout), Budget: targetBudget(firing.Target.Budget), CauseID: source.Key}
		if s.SourceKind == agentrun.SourceAnnouncement {
			request.Persona = s.Persona
			request.Principal.RequesterID = s.RequesterID
		}
		if builder, ok := w.contexts.(RequestBuilder); ok {
			request, err = builder.BuildScheduleRequest(ctx, s, request)
			if err != nil {
				return err
			}
		}
		deliveries = append(deliveries, Delivery{tenant, source.Key, id, s.Revision, firing, request, at.UTC()})
	}
	return w.outbox.AppendWindow(ctx, s, end, deliveries)
}

// Replay leaves the outbox pending after any failure. Admission source
// uniqueness reconciles the crash between inbox persistence and acknowledgement.
func (w *OutboxWorker) Replay(ctx context.Context, tenant string, limit int) (int, error) {
	pending, err := w.outbox.Pending(ctx, tenant, limit)
	if err != nil {
		return 0, err
	}
	done := 0
	var failures []error
	for _, delivery := range pending {
		if err := w.CheckRequest(ctx, delivery.Request); err != nil {
			failures = append(failures, err)
			continue
		}
		record, _, err := w.inbox.Admit(ctx, delivery.Request)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if err := agentrun.ValidateAdmissionRecord(record); err != nil || record.Request.Source != delivery.Request.Source {
			failures = append(failures, ErrReceiptConflict)
			continue
		}
		receipt := Receipt{delivery.Key, record.ID, record.RequestDigest, record.Decision, record.RefusalCode}
		if err := w.outbox.Acknowledge(ctx, tenant, delivery.Key, receipt); err != nil {
			failures = append(failures, err)
			continue
		}
		done++
	}
	return done, errors.Join(failures...)
}

// CheckRequest is the source-owner admission and worker fence. It compares
// the complete frozen request before rechecking the current schedule state.
func (w *OutboxWorker) CheckRequest(ctx context.Context, request agentrun.Request) error {
	delivery, found, err := w.outbox.GetDelivery(ctx, request.Source.TenantID, request.Source.Ref)
	if err != nil {
		return err
	}
	if !found || (request.Source.Kind != agentrun.SourceSchedule && request.Source.Kind != agentrun.SourceAnnouncement) || delivery.Request.Source.Kind != request.Source.Kind || delivery.Key != request.Source.Key {
		return ErrFiringRefused
	}
	want, wantErr := agentrun.AdmissionRequestDigest(delivery.Request)
	got, gotErr := agentrun.AdmissionRequestDigest(request)
	if wantErr != nil || gotErr != nil || want != got {
		return ErrFiringRefused
	}
	if request.Source.Kind == agentrun.SourceAnnouncement {
		current, found, err := w.owner.store.Load(ctx, request.Source.TenantID, delivery.ScheduleID)
		if err != nil {
			return err
		}
		if !found || current.Revision != delivery.ControlRevision {
			return errors.Join(ErrFiringRefused, ErrRevision)
		}
	}
	if err := w.owner.CheckCurrent(ctx, delivery.Firing); err != nil {
		return errors.Join(ErrFiringRefused, err)
	}
	return nil
}
func (w *OutboxWorker) ResolveSourceKey(ctx context.Context, request agentrun.Request) (string, error) {
	delivery, found, err := w.outbox.GetDelivery(ctx, request.Source.TenantID, request.Source.Ref)
	if err != nil {
		return "", err
	}
	if !found || (request.Source.Kind != agentrun.SourceSchedule && request.Source.Kind != agentrun.SourceAnnouncement) || delivery.Request.Source.Kind != request.Source.Kind {
		return "", ErrInvalidFiring
	}
	return delivery.Key, nil
}
