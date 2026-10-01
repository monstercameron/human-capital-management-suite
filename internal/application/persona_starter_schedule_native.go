package application

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/schedulingstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/availability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/schedopt"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const personaWorkScheduleSchema = "persona-work-schedule/v1"

// PersonaWorkerWorkSchedule is an owner-authored civil-time projection of the
// worker's assignments in the approved publication. No calendar, timezone,
// weekday rule or holiday is inferred from chat input.
type PersonaWorkerWorkSchedule struct {
	Worker   values.EntityRef                   `json:"worker"`
	Schedule availability.VersionedWorkSchedule `json:"schedule"`
}

// PersonaPublishedWorkSchedule carries the native schedule owner's approved
// publication and complete availability projection in one sealed revision.
// ProblemDigest and RuleRevision identify the owner's reviewed input snapshot.
type PersonaPublishedWorkSchedule struct {
	Schema        string                      `json:"schema"`
	Approved      schedopt.ApprovedSchedule   `json:"approved"`
	Publication   schedopt.Publication        `json:"publication"`
	ProblemDigest string                      `json:"problem_digest"`
	RuleRevision  string                      `json:"rule_revision"`
	Workers       []PersonaWorkerWorkSchedule `json:"workers"`
}

type personaNativeScheduleStore interface {
	LoadPersonaPublishedScheduleForWorker(context.Context, values.TenantId, values.EntityRef) (string, schedulingstore.PublishedScheduleSnapshot, uint64, error)
}

// PersonaScheduleNativeReader reloads the current persisted publication on
// every call. Its only storage capability is a read; drafting cannot persist
// an offer or advance a fence.
type PersonaScheduleNativeReader struct {
	store         personaNativeScheduleStore
	resolveWorker ShiftSelfServiceWorkerResolver
}

func NewPersonaScheduleNativeReader(store *schedulingstore.Store, resolveWorker ShiftSelfServiceWorkerResolver) (*PersonaScheduleNativeReader, error) {
	if store == nil || resolveWorker == nil {
		return nil, ErrShiftSelfServiceFacts
	}
	return &PersonaScheduleNativeReader{store: store, resolveWorker: resolveWorker}, nil
}

// NewPersonaScheduleWorkforceResolver binds the admitted subject's durable
// worker key to the scheduling candidate identity. The candidate uses the
// canonical worker UUID; neither a display name nor a request field supplies
// its identity. Current active workforce ownership is checked on every call.
func NewPersonaScheduleWorkforceResolver(facts workforce.Facts, now func() time.Time) (ShiftSelfServiceWorkerResolver, error) {
	if facts.DB == nil || facts.TenantUUID == nil || now == nil {
		return nil, ErrShiftSelfServiceIdentity
	}
	return func(ctx context.Context, principal *trust.Principal) (values.EntityRef, error) {
		if ctx == nil || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Subject() == "" {
			return values.EntityRef{}, ErrShiftSelfServiceIdentity
		}
		current, ok := trust.FromContext(ctx)
		if !ok || current != principal {
			return values.EntityRef{}, ErrShiftSelfServiceIdentity
		}
		at := now().UTC()
		if at.Before(principal.IssuedAt()) || !at.Before(principal.ExpiresAt()) {
			return values.EntityRef{}, ErrShiftSelfServiceIdentity
		}
		row, found, err := facts.Lookup(ctx, principal.Tenant(), principal.Subject())
		if err != nil || !found || row.WorkerID == uuid.Nil || row.TenantID != facts.TenantUUID(principal.Tenant()) || strings.ToUpper(strings.TrimSpace(row.LifecycleStatus)) != "ACTIVE" ||
			row.RevisionStream == "" || row.RevisionSequence == 0 || row.KnownAt.IsZero() || row.RecordedAt.IsZero() || row.KnownAt.After(at) || row.RecordedAt.After(at) {
			return values.EntityRef{}, ErrShiftSelfServiceIdentity
		}
		effective, err := time.Parse("2006-01-02", row.EffectiveFrom)
		if err != nil || effective.After(at) {
			return values.EntityRef{}, ErrShiftSelfServiceIdentity
		}
		return values.EntityRef{Tenant: principal.Tenant(), Kind: "candidate", Id: row.WorkerID.String()}, nil
	}, nil
}

// PublishPersonaWorkScheduleSnapshot is the composition seam for the native
// schedule owner, after review/publication. It validates the exact publication
// and its worker projection before the store atomically advances its pointer.
// This function is never registered as a persona skill.
func PublishPersonaWorkScheduleSnapshot(ctx context.Context, store *schedulingstore.Store, tenant values.TenantId, scheduleID, expectedDigest string, expectedFence uint64, source PersonaPublishedWorkSchedule) (uint64, error) {
	if store == nil || source.Schema != personaWorkScheduleSchema {
		return 0, ErrShiftSelfServiceFacts
	}
	if err := validatePersonaPublishedWorkSchedule(tenant, scheduleID, source); err != nil {
		return 0, err
	}
	data, err := json.Marshal(source)
	if err != nil {
		return 0, fmt.Errorf("%w: encode work schedule: %v", ErrShiftSelfServiceFacts, err)
	}
	// Refuse a value the owner envelope cannot rehydrate before advancing the
	// durable pointer. Some domain values intentionally have no JSON decoder.
	var decoded PersonaPublishedWorkSchedule
	if json.Unmarshal(data, &decoded) != nil || validatePersonaPublishedWorkSchedule(tenant, scheduleID, decoded) != nil {
		return 0, fmt.Errorf("%w: work schedule does not round trip through its persisted envelope", ErrShiftSelfServiceFacts)
	}
	snapshot := schedulingstore.PublishedScheduleSnapshot{
		Revision: source.Publication.Revision, ApprovedDigest: source.Approved.BoundDigest,
		PublicationDigest: source.Publication.Digest, ProblemDigest: source.ProblemDigest,
		RuleRevision: source.RuleRevision, RuleDigest: source.Approved.RuleDigest,
	}
	if err := schedulingstore.BindPublishedSchedulePayload(&snapshot, data); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrShiftSelfServiceFacts, err)
	}
	return store.PublishScheduleSnapshot(ctx, tenant, scheduleID, expectedDigest, expectedFence, snapshot)
}

func (p *PersonaScheduleNativeReader) Schedule(ctx context.Context, worker values.EntityRef) (availability.VersionedWorkSchedule, error) {
	if ctx == nil {
		return availability.VersionedWorkSchedule{}, ErrShiftSelfServiceIdentity
	}
	principal, ok := trust.FromContext(ctx)
	if !ok {
		return availability.VersionedWorkSchedule{}, ErrShiftSelfServiceIdentity
	}
	resolved, err := p.worker(ctx, principal)
	if err != nil {
		return availability.VersionedWorkSchedule{}, err
	}
	// People and scheduling name the same persisted worker UUID under their
	// own domain kinds. Accept only these two explicit kinds for the caller's
	// own UUID; the storage lookup always uses the resolver-issued candidate.
	if worker.Validate() != nil || worker.Tenant != resolved.Tenant || worker.Id != resolved.Id || worker.Kind != "candidate" && worker.Kind != "worker" {
		return availability.VersionedWorkSchedule{}, ErrShiftSelfServiceIdentity
	}
	source, fence, err := p.current(ctx, resolved)
	if err != nil {
		return availability.VersionedWorkSchedule{}, err
	}
	for _, item := range source.Workers {
		if item.Worker == resolved {
			schedule := clonePersonaWorkSchedule(item.Schedule)
			// The returned revision is the store-issued current CAS fence. The
			// persisted projection above remains bound to the publication digest.
			// A caller can use this exact sequence when drafting a swap.
			schedule.Revision, err = values.NewSequenceRevision(schedule.ScheduleID.String(), fence)
			if err != nil {
				return availability.VersionedWorkSchedule{}, ErrShiftSelfServiceFacts
			}
			return schedule, nil
		}
	}
	return availability.VersionedWorkSchedule{}, ErrShiftSelfServiceFacts
}

func (p *PersonaScheduleNativeReader) DraftSwap(ctx context.Context, principal *trust.Principal, offerID, assignmentID, requestedID string, fence uint64, reason string) (PersonaShiftSwapProposal, error) {
	if strings.TrimSpace(offerID) == "" || strings.TrimSpace(assignmentID) == "" || strings.TrimSpace(requestedID) == "" || assignmentID == requestedID || fence == 0 || strings.TrimSpace(reason) == "" {
		return PersonaShiftSwapProposal{}, errPersonaStarterBinding
	}
	worker, err := p.worker(ctx, principal)
	if err != nil {
		return PersonaShiftSwapProposal{}, err
	}
	source, currentFence, err := p.current(ctx, worker)
	if err != nil {
		return PersonaShiftSwapProposal{}, err
	}
	if currentFence != fence {
		return PersonaShiftSwapProposal{}, &schedopt.ShiftSelfServiceRejection{Field: "fencing_token", State: "STALE", Version: schedopt.ShiftSelfServiceVersion, Reason: "published schedule changed"}
	}
	var offered, requested *schedopt.ReviewAssignment
	for i := range source.Publication.Assignments {
		assignment := &source.Publication.Assignments[i]
		if assignment.AssignmentID == assignmentID {
			offered = assignment
		}
		if assignment.AssignmentID == requestedID {
			requested = assignment
		}
	}
	if offered == nil || requested == nil || offered.WorkerRef != worker.String() || offered.WorkerRef == requested.WorkerRef {
		return PersonaShiftSwapProposal{}, &schedopt.ShiftSelfServiceRejection{Field: "assignment", State: "NOT_OWNED_OR_CURRENT", Version: schedopt.ShiftSelfServiceVersion, Reason: "draft requires two current assignments owned by different workers and the offered assignment must belong to the invoker"}
	}
	return PersonaShiftSwapProposal{OfferID: offerID, AssignmentID: assignmentID, RequestedAssignmentID: requestedID, Fence: fence, Reason: reason, Worker: worker}, nil
}

func (p *PersonaScheduleNativeReader) worker(ctx context.Context, principal *trust.Principal) (values.EntityRef, error) {
	if p == nil || p.store == nil || p.resolveWorker == nil || ctx == nil || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman {
		return values.EntityRef{}, ErrShiftSelfServiceIdentity
	}
	current, ok := trust.FromContext(ctx)
	if !ok || current != principal {
		return values.EntityRef{}, ErrShiftSelfServiceIdentity
	}
	worker, err := p.resolveWorker(ctx, principal)
	if err != nil || worker.Validate() != nil || worker.Tenant != principal.Tenant() || worker.Kind != "candidate" {
		return values.EntityRef{}, ErrShiftSelfServiceIdentity
	}
	return worker, nil
}

func (p *PersonaScheduleNativeReader) current(ctx context.Context, worker values.EntityRef) (PersonaPublishedWorkSchedule, uint64, error) {
	id, snapshot, fence, err := p.store.LoadPersonaPublishedScheduleForWorker(ctx, worker.Tenant, worker)
	if err != nil {
		return PersonaPublishedWorkSchedule{}, 0, fmt.Errorf("%w: %v", ErrShiftSelfServiceFacts, err)
	}
	var envelope schedulingstore.PublishedSchedulePayload
	var source PersonaPublishedWorkSchedule
	if json.Unmarshal(snapshot.Payload, &envelope) != nil || json.Unmarshal(envelope.Data, &source) != nil ||
		fence == 0 || envelope.Schema != "schedopt-published-schedule/v1" || schedulingstore.PayloadDigest(snapshot.Payload) != snapshot.PayloadDigest ||
		envelope.Revision != snapshot.Revision || envelope.ApprovedDigest != snapshot.ApprovedDigest || envelope.PublicationDigest != snapshot.PublicationDigest ||
		envelope.ProblemDigest != snapshot.ProblemDigest || envelope.RuleRevision != snapshot.RuleRevision || envelope.RuleDigest != snapshot.RuleDigest ||
		source.Publication.Revision != snapshot.Revision || source.Approved.BoundDigest != snapshot.ApprovedDigest || source.Publication.Digest != snapshot.PublicationDigest ||
		source.ProblemDigest != snapshot.ProblemDigest || source.RuleRevision != snapshot.RuleRevision || source.Approved.RuleDigest != snapshot.RuleDigest {
		return PersonaPublishedWorkSchedule{}, 0, ErrShiftSelfServiceFacts
	}
	if err := validatePersonaPublishedWorkSchedule(worker.Tenant, id, source); err != nil {
		return PersonaPublishedWorkSchedule{}, 0, err
	}
	for _, item := range source.Workers {
		if item.Worker == worker {
			return source, fence, nil
		}
	}
	return PersonaPublishedWorkSchedule{}, 0, ErrShiftSelfServiceFacts
}

func validatePersonaPublishedWorkSchedule(tenant values.TenantId, id string, source PersonaPublishedWorkSchedule) error {
	publication, err := schedopt.PublishSchedule(source.Approved, source.Publication.IdempotencyKey, map[string]string{}, source.Publication.PublishedAt)
	if err != nil || tenant.Validate() != nil || source.Schema != personaWorkScheduleSchema || source.Publication.Tenant != tenant.String() ||
		!reflect.DeepEqual(publication, source.Publication) || strings.TrimSpace(source.ProblemDigest) == "" || strings.TrimSpace(source.RuleRevision) == "" {
		return ErrShiftSelfServiceFacts
	}
	assignments := make(map[string]string, len(source.Publication.Assignments))
	for _, assignment := range source.Publication.Assignments {
		var worker values.EntityRef
		if worker.UnmarshalText([]byte(assignment.WorkerRef)) != nil || worker.Tenant != tenant || worker.Kind != "candidate" || assignment.AssignmentID == "" || assignment.WindowRef == "" || assignment.DemandRef == "" {
			return ErrShiftSelfServiceFacts
		}
		if _, duplicate := assignments[assignment.AssignmentID]; duplicate {
			return ErrShiftSelfServiceFacts
		}
		assignments[assignment.AssignmentID] = assignment.WorkerRef
	}
	seenWorkers := make(map[values.EntityRef]bool, len(source.Workers))
	seenAssignments := make(map[string]bool, len(assignments))
	for _, item := range source.Workers {
		if item.Worker.Validate() != nil || item.Worker.Tenant != tenant || item.Worker.Kind != "candidate" || seenWorkers[item.Worker] || item.Schedule.Validate() != nil ||
			item.Schedule.ScheduleID.Tenant != tenant || item.Schedule.ScheduleID.Id != id || item.Schedule.Assignment.Tenant != tenant || len(item.Schedule.Shifts) == 0 {
			return ErrShiftSelfServiceFacts
		}
		revision, err := values.NewOpaqueRevision(item.Schedule.ScheduleID.String(), []byte(source.Publication.Digest))
		if err != nil || !bytes.Equal(item.Schedule.Revision.Canonical(), revision.Canonical()) {
			return ErrShiftSelfServiceFacts
		}
		seenWorkers[item.Worker] = true
		for _, shift := range item.Schedule.Shifts {
			if assignments[shift.ID] != item.Worker.String() || seenAssignments[shift.ID] || !item.Schedule.WorkingWeekdays[time.Date(int(shift.Date.Year()), shift.Date.Month(), int(shift.Date.Day()), 0, 0, 0, 0, time.UTC).Weekday()] {
				return ErrShiftSelfServiceFacts
			}
			seenAssignments[shift.ID] = true
		}
	}
	if len(seenAssignments) != len(assignments) {
		return ErrShiftSelfServiceFacts
	}
	return nil
}

func clonePersonaWorkSchedule(schedule availability.VersionedWorkSchedule) availability.VersionedWorkSchedule {
	weekdays := make(map[time.Weekday]bool, len(schedule.WorkingWeekdays))
	for day, working := range schedule.WorkingWeekdays {
		weekdays[day] = working
	}
	schedule.WorkingWeekdays = weekdays
	schedule.Shifts = append([]availability.WorkShift(nil), schedule.Shifts...)
	if schedule.Holidays != nil {
		holidays := make([]availability.ScheduleHoliday, len(schedule.Holidays))
		copy(holidays, schedule.Holidays)
		schedule.Holidays = holidays
	}
	return schedule
}

var _ PersonaScheduleReader = (*PersonaScheduleNativeReader)(nil)
var _ PersonaScheduleSwapDrafter = (*PersonaScheduleNativeReader)(nil)
