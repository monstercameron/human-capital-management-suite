package application

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/schedulingstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/availability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/schedopt"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaNativeScheduleReadFixture struct {
	id       string
	worker   values.EntityRef
	snapshot schedulingstore.PublishedScheduleSnapshot
	fence    uint64
	calls    int
	err      error
}

func (s *personaNativeScheduleReadFixture) LoadPersonaPublishedScheduleForWorker(_ context.Context, tenant values.TenantId, worker values.EntityRef) (string, schedulingstore.PublishedScheduleSnapshot, uint64, error) {
	s.calls++
	if tenant != s.worker.Tenant || worker != s.worker {
		return "", schedulingstore.PublishedScheduleSnapshot{}, 0, schedulingstore.ErrNotFound
	}
	return s.id, s.snapshot, s.fence, s.err
}

func personaNativeScheduleFixture(t *testing.T) (*PersonaScheduleNativeReader, *personaNativeScheduleReadFixture, context.Context, *trust.Principal, PersonaPublishedWorkSchedule) {
	t.Helper()
	tenant := values.TenantId("tenant-native-schedule")
	worker := values.EntityRef{Tenant: tenant, Kind: "candidate", Id: "11111111-1111-4111-8111-111111111111"}
	peer := values.EntityRef{Tenant: tenant, Kind: "candidate", Id: "22222222-2222-4222-8222-222222222222"}
	id := "33333333-3333-4333-8333-333333333333"
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: tenant, Subject: "opaque-human-subject", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: at.Add(-time.Hour), ExpiresAt: at.Add(time.Hour), CredentialDigest: "credential"})
	if err != nil {
		t.Fatal(err)
	}
	assignments := []schedopt.ReviewAssignment{{AssignmentID: "shift-owned", WorkerRef: worker.String(), DemandRef: "demand-morning", WindowRef: "window-morning"}, {AssignmentID: "shift-peer", WorkerRef: peer.String(), DemandRef: "demand-evening", WindowRef: "window-evening"}}
	ruleDigest := "sha256:" + strings.Repeat("a", 64)
	approved, err := schedopt.ApplyPrepublicationReview(schedopt.CandidateSchedule{Tenant: tenant.String(), Revision: "revision-owner-17", RuleDigest: ruleDigest, Assignments: assignments, ReviewLifecycle: schedopt.ReviewPrepublication}, nil, "scheduler:verified", at)
	if err != nil {
		t.Fatal(err)
	}
	publication, err := schedopt.PublishSchedule(approved, "publication-owner-17", map[string]string{}, at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	scheduleRef := values.EntityRef{Tenant: tenant, Kind: "schedule", Id: id}
	revision, err := values.NewOpaqueRevision(scheduleRef.String(), []byte(publication.Digest))
	if err != nil {
		t.Fatal(err)
	}
	date, _ := values.NewLocalDate(2026, time.October, 1)
	start, _ := values.NewLocalTime(9, 0, 0, 0)
	end, _ := values.NewLocalTime(17, 0, 0, 0)
	source := PersonaPublishedWorkSchedule{Schema: personaWorkScheduleSchema, Approved: approved, Publication: publication, ProblemDigest: "sha256:" + strings.Repeat("b", 64), RuleRevision: "rules-owner-3"}
	for i, ref := range []values.EntityRef{worker, peer} {
		source.Workers = append(source.Workers, PersonaWorkerWorkSchedule{Worker: ref, Schedule: availability.VersionedWorkSchedule{
			ScheduleID: scheduleRef, Assignment: values.EntityRef{Tenant: tenant, Kind: "assignment", Id: ref.Id}, Revision: revision,
			Calendar: values.CalendarRef{Ref: "owner-calendar", Version: "calendar-6"}, Timezone: values.ZoneRef{ID: "UTC", TzdbVersion: "2026b"},
			WorkingWeekdays: map[time.Weekday]bool{time.Thursday: true},
			Shifts:          []availability.WorkShift{{ID: assignments[i].AssignmentID, Date: date, Start: start, End: end, Disambiguation: values.DisambiguationRejectGap}},
			Holidays:        []availability.ScheduleHoliday{},
		}})
	}
	store := &personaNativeScheduleReadFixture{id: id, worker: worker, fence: 5}
	store.snapshot = personaNativeScheduleSnapshot(t, source)
	reader := &PersonaScheduleNativeReader{store: store, resolveWorker: func(_ context.Context, received *trust.Principal) (values.EntityRef, error) {
		if received != principal {
			t.Fatal("worker resolver received a different principal")
		}
		return worker, nil
	}}
	return reader, store, trust.WithPrincipal(context.Background(), principal), principal, source
}

func personaNativeScheduleSnapshot(t *testing.T, source PersonaPublishedWorkSchedule) schedulingstore.PublishedScheduleSnapshot {
	t.Helper()
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := schedulingstore.PublishedScheduleSnapshot{Revision: source.Publication.Revision, ApprovedDigest: source.Approved.BoundDigest, PublicationDigest: source.Publication.Digest, ProblemDigest: source.ProblemDigest, RuleRevision: source.RuleRevision, RuleDigest: source.Approved.RuleDigest}
	if err := schedulingstore.BindPublishedSchedulePayload(&snapshot, data); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestTodo_AGENTP_023_NativeSchedule(t *testing.T) {
	reader, store, ctx, principal, source := personaNativeScheduleFixture(t)
	expected := source.Workers[0].Schedule
	expected.Revision, _ = values.NewSequenceRevision(expected.ScheduleID.String(), store.fence)
	got, err := reader.Schedule(ctx, store.worker)
	if err != nil || !reflect.DeepEqual(got, expected) {
		t.Fatalf("native schedule = %#v, error = %v; want owner projection %#v", got, err, expected)
	}
	before := string(store.snapshot.Payload)
	proposal, err := reader.DraftSwap(ctx, principal, "private-proposal", "shift-owned", "shift-peer", 5, "coverage exchange")
	if err != nil || proposal.Worker != store.worker || proposal.AssignmentID != "shift-owned" || proposal.RequestedAssignmentID != "shift-peer" || proposal.Fence != 5 {
		t.Fatalf("grounded proposal = %#v, error = %v", proposal, err)
	}
	if before != string(store.snapshot.Payload) || store.fence != 5 || store.calls != 2 {
		t.Fatalf("draft changed current publication or fence: fence=%d calls=%d", store.fence, store.calls)
	}
	peopleWorker := store.worker
	peopleWorker.Kind = "worker"
	if canonical, err := reader.Schedule(ctx, peopleWorker); err != nil || !reflect.DeepEqual(canonical, expected) {
		t.Fatalf("same canonical worker UUID did not resolve across People/scheduling kinds: %#v, %v", canonical, err)
	}
	got.WorkingWeekdays[time.Thursday] = false
	got.Shifts[0].ID = "changed-private-copy"
	again, err := reader.Schedule(ctx, store.worker)
	if err != nil || !again.WorkingWeekdays[time.Thursday] || again.Shifts[0].ID != "shift-owned" {
		t.Fatalf("caller mutated authoritative schedule: %#v, %v", again, err)
	}
	store.fence = 6
	if _, err := reader.DraftSwap(ctx, principal, "stale-proposal", "shift-owned", "shift-peer", 5, "coverage"); !errors.Is(err, schedopt.ErrShiftSelfServiceRejected) {
		t.Fatalf("stale current schedule fence accepted: %v", err)
	}
}

func TestTodo_AGENTP_023_NativeSchedule_Security(t *testing.T) {
	t.Run("read identity cannot select another worker", func(t *testing.T) {
		reader, store, ctx, _, source := personaNativeScheduleFixture(t)
		if _, err := reader.Schedule(ctx, source.Workers[1].Worker); !errors.Is(err, ErrShiftSelfServiceIdentity) || store.calls != 0 {
			t.Fatalf("other worker read error=%v calls=%d", err, store.calls)
		}
		if _, err := reader.Schedule(context.Background(), store.worker); !errors.Is(err, ErrShiftSelfServiceIdentity) || store.calls != 0 {
			t.Fatalf("untrusted read error=%v calls=%d", err, store.calls)
		}
		if _, err := reader.Schedule(nil, store.worker); !errors.Is(err, ErrShiftSelfServiceIdentity) {
			t.Fatalf("nil context read: %v", err)
		}
	})
	t.Run("resolver tenant and kind are enforced", func(t *testing.T) {
		reader, store, ctx, _, _ := personaNativeScheduleFixture(t)
		reader.resolveWorker = func(context.Context, *trust.Principal) (values.EntityRef, error) {
			worker := store.worker
			worker.Tenant = "another-tenant"
			return worker, nil
		}
		if _, err := reader.Schedule(ctx, store.worker); !errors.Is(err, ErrShiftSelfServiceIdentity) || store.calls != 0 {
			t.Fatalf("cross tenant worker error=%v calls=%d", err, store.calls)
		}
	})
	for _, tc := range []struct {
		name, offered, requested string
		fence                    uint64
	}{
		{"foreign owner", "shift-peer", "shift-owned", 5}, {"fabricated assignment", "invented", "shift-peer", 5},
		{"fabricated requested assignment", "shift-owned", "invented", 5}, {"same assignment", "shift-owned", "shift-owned", 5}, {"missing fence", "shift-owned", "shift-peer", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader, store, ctx, principal, _ := personaNativeScheduleFixture(t)
			before := string(store.snapshot.Payload)
			if _, err := reader.DraftSwap(ctx, principal, "proposal", tc.offered, tc.requested, tc.fence, "coverage"); err == nil {
				t.Fatal("invalid assignment pair accepted")
			}
			if store.fence != 5 || string(store.snapshot.Payload) != before {
				t.Fatal("refused draft modified persisted facts")
			}
		})
	}
	t.Run("principal must be context bound", func(t *testing.T) {
		reader, store, _, principal, _ := personaNativeScheduleFixture(t)
		if _, err := reader.DraftSwap(context.Background(), principal, "proposal", "shift-owned", "shift-peer", 5, "coverage"); !errors.Is(err, ErrShiftSelfServiceIdentity) || store.calls != 0 {
			t.Fatalf("unbound principal error=%v calls=%d", err, store.calls)
		}
	})
}

func TestTodo_AGENTP_023_NativeSchedule_Fault(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*personaNativeScheduleReadFixture, *PersonaPublishedWorkSchedule)
	}{
		{"missing persisted publication", func(store *personaNativeScheduleReadFixture, _ *PersonaPublishedWorkSchedule) {
			store.err = schedulingstore.ErrNotFound
		}},
		{"corrupt bytes", func(store *personaNativeScheduleReadFixture, _ *PersonaPublishedWorkSchedule) {
			store.snapshot.Payload[0] = '!'
		}},
		{"row digest differs", func(store *personaNativeScheduleReadFixture, _ *PersonaPublishedWorkSchedule) {
			store.snapshot.PublicationDigest = "sha256:" + strings.Repeat("f", 64)
		}},
		{"unsealed publication", func(_ *personaNativeScheduleReadFixture, source *PersonaPublishedWorkSchedule) {
			source.Publication.Assignments[0].WorkerRef = source.Workers[1].Worker.String()
		}},
		{"foreign projected shift", func(_ *personaNativeScheduleReadFixture, source *PersonaPublishedWorkSchedule) {
			source.Workers[0].Schedule.Shifts[0].ID = "shift-peer"
		}},
		{"missing peer projection", func(_ *personaNativeScheduleReadFixture, source *PersonaPublishedWorkSchedule) {
			source.Workers = source.Workers[:1]
		}},
		{"unbound revision", func(_ *personaNativeScheduleReadFixture, source *PersonaPublishedWorkSchedule) {
			source.Workers[0].Schedule.Revision = values.UnspecifiedRevision()
		}},
		{"different schedule id", func(store *personaNativeScheduleReadFixture, _ *PersonaPublishedWorkSchedule) {
			store.id = "44444444-4444-4444-8444-444444444444"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader, store, ctx, _, source := personaNativeScheduleFixture(t)
			tc.mutate(store, &source)
			if tc.name != "missing persisted publication" && tc.name != "corrupt bytes" && tc.name != "row digest differs" && tc.name != "different schedule id" {
				store.snapshot = personaNativeScheduleSnapshot(t, source)
			}
			if _, err := reader.Schedule(ctx, store.worker); !errors.Is(err, ErrShiftSelfServiceFacts) {
				t.Fatalf("invalid persisted source served: %v", err)
			}
		})
	}
	if _, err := NewPersonaScheduleNativeReader(nil, nil); !errors.Is(err, ErrShiftSelfServiceFacts) {
		t.Fatalf("constructor without real source: %v", err)
	}
	if _, err := PublishPersonaWorkScheduleSnapshot(context.Background(), nil, "tenant-a", "id", "", 0, PersonaPublishedWorkSchedule{}); !errors.Is(err, ErrShiftSelfServiceFacts) {
		t.Fatalf("provision without real source: %v", err)
	}
}

func TestTodo_AGENTP_023_NativeSchedule_Integration(t *testing.T) {
	_, fixtureStore, ctx, principal, source := personaNativeScheduleFixture(t)
	db := pgtest.New(t)
	tenant := fixtureStore.worker.Tenant
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenantID, tenant.String(), tenant.String())
	at := principal.IssuedAt().Add(time.Hour)
	db.Exec(t, `INSERT INTO journey_worker (tenant_id,worker_id,worker_key,legal_name,preferred_name,worker_number,worker_type,lifecycle_status,
		employment_id,assignment_id,job_code,grade,org_unit,position_id,location,pay_zone,fte,manager_relationship_ref,hire_date,effective_from,
		base_pay,currency,pay_basis,bonus_target,revision_stream,revision_sequence,known_at,recorded_at,created_by)
		VALUES ($1,$2,$3,'Native Worker','Native Worker','NATIVE-1','EMPLOYEE','active',
		'emp-native','assignment-native','SUPPORT','G1','crew-native','position-native','site-native','ZONE-1',1,'manager-native','2026-01-01','2026-01-01',
		25,'USD','HOURLY_RATE',0,'worker-native',1,$4,$4,'owner:verified')`, tenantID, fixtureStore.worker.Id, principal.Subject(), at.Add(-time.Hour))
	facts := workforce.NewFacts(db.Conn, func(key values.TenantId) uuid.UUID {
		if key == tenant {
			return tenantID
		}
		return uuid.Nil
	})
	resolveWorker, err := NewPersonaScheduleWorkforceResolver(facts, func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveWorker(ctx, principal)
	if err != nil || resolved != fixtureStore.worker {
		t.Fatalf("persisted workforce subject resolution=%#v error=%v", resolved, err)
	}
	writer := schedulingstore.New(db.Conn)
	fence, err := PublishPersonaWorkScheduleSnapshot(ctx, writer, tenant, fixtureStore.id, "", 0, source)
	if err != nil || fence != 1 {
		t.Fatalf("owner publication fence=%d error=%v", fence, err)
	}
	restarted := schedulingstore.New(db.NewConn(t))
	reader, err := NewPersonaScheduleNativeReader(restarted, resolveWorker)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reader.Schedule(ctx, fixtureStore.worker)
	expected := source.Workers[0].Schedule
	expected.Revision, _ = values.NewSequenceRevision(expected.ScheduleID.String(), fence)
	if err != nil || !reflect.DeepEqual(got, expected) {
		t.Fatalf("restarted reader schedule=%#v error=%v", got, err)
	}
	proposal, err := reader.DraftSwap(ctx, principal, "private-only", "shift-owned", "shift-peer", fence, "coverage")
	if err != nil || proposal.Worker != fixtureStore.worker {
		t.Fatalf("persisted grounded private draft=%#v error=%v", proposal, err)
	}
	if _, err := restarted.LoadShiftOffer(ctx, tenant, fixtureStore.id, proposal.OfferID); !errors.Is(err, schedulingstore.ErrNotFound) {
		t.Fatalf("T1 draft persisted an offer: %v", err)
	}
	before, unchangedFence, err := writer.LoadPublishedSchedule(ctx, tenant, fixtureStore.id)
	if err != nil || unchangedFence != fence || before.PublicationDigest != source.Publication.Digest {
		t.Fatalf("T1 draft changed schedule snapshot or fence: fence=%d snapshot=%+v error=%v", unchangedFence, before, err)
	}
	nextApproved, err := schedopt.ApplyPrepublicationReview(schedopt.CandidateSchedule{Tenant: tenant.String(), Revision: "revision-owner-18", RuleDigest: source.Approved.RuleDigest, Assignments: source.Approved.Assignments, ReviewLifecycle: schedopt.ReviewPrepublication}, nil, "scheduler:verified", source.Approved.ApprovedAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	nextPublication, err := schedopt.PublishSchedule(nextApproved, "publication-owner-18", map[string]string{}, source.Publication.PublishedAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	source.Approved, source.Publication = nextApproved, nextPublication
	for i := range source.Workers {
		source.Workers[i].Schedule.Revision, err = values.NewOpaqueRevision(source.Workers[i].Schedule.ScheduleID.String(), []byte(nextPublication.Digest))
		if err != nil {
			t.Fatal(err)
		}
	}
	nextFence, err := PublishPersonaWorkScheduleSnapshot(ctx, writer, tenant, fixtureStore.id, before.PublicationDigest, fence, source)
	if err != nil || nextFence != fence+1 {
		t.Fatalf("advance owner schedule fence=%d error=%v", nextFence, err)
	}
	if _, err := reader.DraftSwap(ctx, principal, "private-stale", "shift-owned", "shift-peer", fence, "coverage"); !errors.Is(err, schedopt.ErrShiftSelfServiceRejected) {
		t.Fatalf("old draft fence accepted after native owner publication: %v", err)
	}
	latest, err := reader.Schedule(ctx, fixtureStore.worker)
	latestFence, specified := latest.Revision.Sequence()
	if err != nil || latest.Revision == got.Revision || !specified || latestFence != nextFence {
		t.Fatalf("reader served old revision after owner publication: %#v, %v", latest, err)
	}
	if _, err := resolveWorker(context.Background(), principal); !errors.Is(err, ErrShiftSelfServiceIdentity) {
		t.Fatalf("native workforce resolver accepted absent trusted context: %v", err)
	}
	expired, err := NewPersonaScheduleWorkforceResolver(facts, func() time.Time { return principal.ExpiresAt() })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := expired(ctx, principal); !errors.Is(err, ErrShiftSelfServiceIdentity) {
		t.Fatalf("native workforce resolver accepted expired principal: %v", err)
	}
}
