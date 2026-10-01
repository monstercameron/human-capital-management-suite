package workerlifecyclestore

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workerlifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func lifecycleFixture(t *testing.T, tenant values.TenantId) Snapshot {
	t.Helper()
	date, _ := values.ParseLocalDate("2026-09-30")
	ref := func(kind string) values.EntityRef {
		return values.EntityRef{Tenant: tenant, Kind: values.Kind(kind), Id: uuid.NewString()}
	}
	evidence := ref("evidence")
	plan, err := workerlifecycle.NewPlan(workerlifecycle.WorkerLifecyclePlan{Worker: ref("worker"), Employment: ref("employment"), Proposal: ref("proposal"), Event: workerlifecycle.EventStart, EventDate: date, Completion: workerlifecycle.CompleteAllRequired, Requirements: []workerlifecycle.Requirement{{ID: "identity", Ordinal: 1, Owner: "people", Due: workerlifecycle.DueRule{Calendar: values.CalendarRef{Ref: "gregorian", Version: "1"}}, Evidence: []values.EntityRef{evidence}, VerificationPolicy: ref("evidence_policy"), Completion: workerlifecycle.CompleteAllRequired, Required: true}}, Children: []workerlifecycle.ChildTemplate{{ID: "identity-task", Ordinal: 1, IntentType: "hcm.identity.verify", IntentVersion: "1"}}})
	if err != nil {
		t.Fatal(err)
	}
	request := workerlifecycle.ResolutionRequest{Plan: plan, Requirements: []workerlifecycle.RequirementInput{{RequirementID: "identity", Kind: workerlifecycle.RequirementIdentity, Protected: true, FreshDays: 30}}, Facts: []workerlifecycle.WorkerFact{{RequirementID: "identity", Evidence: evidence, ObservedAt: date, Summary: "protected identity verification"}}, AsOf: date}
	ready, err := workerlifecycle.ResolveOnboardingReadiness(request)
	if err != nil {
		t.Fatal(err)
	}
	tracker, err := workerlifecycle.NewOnboardingTracker(plan, ready)
	if err != nil {
		t.Fatal(err)
	}
	tracker, _, err = workerlifecycle.EmitDueChildren(tracker, ready)
	if err != nil {
		t.Fatal(err)
	}
	revision, _ := values.NewSequenceRevision("people.worker."+plan.Worker.Id, 1)
	return Snapshot{Request: request, Tracker: tracker, WorkerRevision: revision, ChannelID: "onboarding-room", Revision: 1}
}

func TestTodo_AGENTP_021_Onboarding_PersistedInputsAndTenantIsolation(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenant, foreign := uuid.New(), uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,'onboarding-a','cell-local','Onboarding A','ACTIVE','2026-01-01'),($2,'onboarding-b','cell-local','Onboarding B','ACTIVE','2026-01-01')`, tenant, foreign)
	snapshot := lifecycleFixture(t, "onboarding-a")
	workerID, _ := uuid.Parse(snapshot.Request.Plan.Worker.Id)
	row := workforce.WorkerRow{TenantID: tenant, WorkerID: workerID, WorkerKey: "created-hire", LegalName: "Taylor Example", PreferredName: "Taylor", WorkerNumber: "W-1", WorkerType: "employee", LifecycleStatus: "active", EmploymentID: snapshot.Request.Plan.Employment.Id, AssignmentID: uuid.NewString(), JobCode: "OPS-HR", Grade: "P2", OrgUnit: "people", PositionID: "POS-1", Location: "Boston", PayZone: "US", FTE: "1.0000", ManagerRelationshipRef: "board:people", HireDate: "2026-09-30", EffectiveFrom: "2026-09-30", BasePay: "90000.00", Currency: "USD", PayBasis: "ANNUAL_SALARY", BonusTarget: "0.0500", RevisionStream: snapshot.WorkerRevision.Stream(), RevisionSequence: 1, KnownAt: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC), CreatedBy: "principal:hr", Source: workforce.SourceCreated}
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err = (workforce.Store{}).Create(ctx, tx, row); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	conn := db.NewConn(t)
	if _, err = conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	mapper := func(id values.TenantId) uuid.UUID {
		if id == "onboarding-a" {
			return tenant
		}
		if id == "onboarding-b" {
			return foreign
		}
		return uuid.Nil
	}
	store, err := New(conn, mapper)
	if err != nil {
		t.Fatal(err)
	}
	worker := snapshot.Request.Plan.Worker
	if _, err = store.Get(ctx, worker); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unpersisted checklist: %v", err)
	}
	if err = store.Put(ctx, snapshot, 0); err != nil {
		t.Fatal(err)
	}
	restarted, _ := New(conn, mapper)
	got, err := restarted.Get(ctx, worker)
	if err != nil || got.DisplayName != "Taylor" {
		t.Fatalf("durable current worker: %+v %v", got, err)
	}
	got.DisplayName = ""
	if !reflect.DeepEqual(got, snapshot) {
		t.Fatalf("persisted owner image changed: %+v", got)
	}
	if err = store.Put(ctx, snapshot, 0); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	next := snapshot
	next.Revision = 2
	if err = store.Put(ctx, next, 1); err != nil {
		t.Fatal(err)
	}
	if err = store.Put(ctx, next, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale CAS: %v", err)
	}
	foreignWorker := worker
	foreignWorker.Tenant = "onboarding-b"
	if _, err = store.Get(ctx, foreignWorker); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross tenant: %v", err)
	}
	foreignTx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = tenancy.WithTenant(ctx, foreignTx, foreign); err != nil {
		t.Fatal(err)
	}
	var visible int
	if err = foreignTx.QueryRow(ctx, `SELECT count(*) FROM worker_onboarding_snapshot WHERE tenant_id=$1`, tenant).Scan(&visible); err != nil || visible != 0 {
		t.Fatalf("RLS disclosed foreign snapshot: %d %v", visible, err)
	}
	_ = foreignTx.Rollback(ctx)
	stale := next
	stale.Revision = 3
	stale.WorkerRevision, _ = values.NewSequenceRevision(snapshot.WorkerRevision.Stream(), 2)
	if err = store.Put(ctx, stale, 2); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale worker revision: %v", err)
	}
	stale = next
	stale.Revision = 3
	stale.Request.Plan.Employment.Id = uuid.NewString()
	stale.Request.Plan.CanonicalDigest = ""
	stale.Request.Plan, err = workerlifecycle.NewPlan(stale.Request.Plan)
	if err != nil {
		t.Fatal(err)
	}
	ready, _ := workerlifecycle.ResolveOnboardingReadiness(stale.Request)
	stale.Tracker, _ = workerlifecycle.NewOnboardingTracker(stale.Request.Plan, ready)
	if err = store.Put(ctx, stale, 2); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong current employment: %v", err)
	}
	db.Exec(t, `UPDATE worker_onboarding_snapshot SET payload_digest='tampered' WHERE tenant_id=$1`, tenant)
	if _, err = store.Get(ctx, worker); !errors.Is(err, ErrInvalid) {
		t.Fatalf("corrupt digest: %v", err)
	}
	db.Exec(t, `UPDATE worker_onboarding_snapshot SET snapshot_payload='{}' WHERE tenant_id=$1`, tenant)
	if _, err = store.Get(ctx, worker); !errors.Is(err, ErrInvalid) {
		t.Fatalf("corrupt payload: %v", err)
	}
}

func TestTodo_AGENTP_021_Onboarding_RejectsUnboundSources(t *testing.T) {
	valid := lifecycleFixture(t, "onboarding-a")
	mutations := []func(*Snapshot){func(s *Snapshot) { s.Revision = 0 }, func(s *Snapshot) { s.ChannelID = " room " }, func(s *Snapshot) { s.Request.Plan.CanonicalDigest = "" }, func(s *Snapshot) { s.Request.Facts[0].Evidence.Id = uuid.NewString() }, func(s *Snapshot) { s.Tracker.Children = nil }, func(s *Snapshot) { s.WorkerRevision = values.UnspecifiedRevision() }}
	for i, change := range mutations {
		s := lifecycleFixture(t, "onboarding-a")
		change(&s)
		if !errors.Is(s.Validate(), ErrInvalid) {
			t.Fatalf("mutation %d accepted", i)
		}
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := New(nil, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing dependencies: %v", err)
	}
	var store *Store
	if _, err := store.Get(context.Background(), valid.Request.Plan.Worker); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil store: %v", err)
	}
	if err := store.Put(context.Background(), Snapshot{}, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid write: %v", err)
	}
}
