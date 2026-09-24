package performancestore_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/performancestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type tenantID uuid.UUID

func (t tenantID) String() string { return uuid.UUID(t).String() }

func digest(ch byte) string {
	ch = "abcdef"[int(ch-'a')%6]
	return "sha256:" + strings.Repeat(string(ch), 64)
}

func instant() values.Instant { return values.NewInstant(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)) }

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("SET ROLE: %v", err)
	}
	return conn
}

func addTenant(t *testing.T, db *pgtest.DB) tenantID {
	t.Helper()
	id := tenantID(uuid.New())
	db.Exec(t, `INSERT INTO tenant (tenant_id) VALUES ($1)`, uuid.UUID(id))
	return id
}

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	db.Exec(t, `
		CREATE DOMAIN tenant_ref AS uuid;
		CREATE DOMAIN content_digest AS text CHECK (VALUE ~ '^[0-9a-f]{64}$');
		CREATE TABLE tenant (tenant_id tenant_ref PRIMARY KEY);
		CREATE OR REPLACE FUNCTION forbid_mutation() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			RAISE EXCEPTION '% is append-only', TG_TABLE_NAME USING ERRCODE = 'restrict_violation';
		END;
		$$;`)
	db.Exec(t, `
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hcmnext_app') THEN
				CREATE ROLE hcmnext_app NOLOGIN NOSUPERUSER NOBYPASSRLS;
			END IF;
		END
		$$;
		ALTER ROLE hcmnext_app NOSUPERUSER NOBYPASSRLS;`)
	db.Exec(t, `GRANT USAGE ON SCHEMA "`+db.Schema+`" TO hcmnext_app; GRANT SELECT ON tenant TO hcmnext_app;`)
	migration, err := migrations.FS.ReadFile("00108_performance.sql")
	if err != nil {
		t.Fatalf("read performance migration: %v", err)
	}
	up := strings.SplitN(string(migration), "-- +goose Up", 2)[1]
	up = strings.SplitN(up, "-- +goose Down", 2)[0]
	db.Exec(t, up)
	graphMigration, err := migrations.FS.ReadFile("00348_performance_frozen_participant_reviewer_graph.sql")
	if err != nil {
		t.Fatalf("read frozen graph migration: %v", err)
	}
	graphUp := strings.SplitN(string(graphMigration), "-- +goose Up", 2)[1]
	graphUp = strings.SplitN(graphUp, "-- +goose Down", 2)[0]
	db.Exec(t, graphUp)
	return db
}

type fixture struct {
	db      *pgtest.DB
	tenant  tenantID
	store   *performancestore.Store
	cycle   performance.CycleRevision
	caseRow performance.RatingCaseRecord
	event   performance.RatingEventRecord
	session performance.CalibrationSessionRecord
	review  performance.ReviewRecord
	outcome performance.OutcomeLinkRecord
	final   performance.FinalRatingRecord
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := newDB(t)
	tenant := addTenant(t, db)
	ratingRef := uuid.New()
	return fixture{
		db: db, tenant: tenant, store: performancestore.New(appConn(t, db)),
		cycle:   performance.CycleRevision{CycleID: "cycle-1", Revision: 1, State: performance.PerformanceCyclePlanned, CanonicalDigest: digest('a')},
		caseRow: performance.RatingCaseRecord{CaseID: "case-1", ParticipantID: "person-1", CanonicalDigest: digest('b')},
		event:   performance.RatingEventRecord{Kind: performance.RatingEventContestRaised, ActorID: "actor-1", At: instant(), Digest: digest('c')},
		session: performance.CalibrationSessionRecord{SessionID: "session-1", CycleID: "cycle-1", CycleRevision: 1, Graph: []byte(`{}`), Adjustments: []byte(`[]`), CanonicalDigest: digest('d')},
		review:  performance.ReviewRecord{ReviewID: "review-1", ReviewerID: "reviewer-1", ParticipantID: "person-1", CycleID: "cycle-1", CycleRevision: 1, ReviewRevision: 1, SubmittedAt: instant()},
		outcome: performance.OutcomeLinkRecord{RatingRef: ratingRef.String(), RatingDigest: digest('e'), RatingRevision: 1, Action: performance.OutcomeLinkActionLink, EffectiveAt: instant(), Digest: digest('f')},
		final:   performance.FinalRatingRecord{RatingRef: ratingRef.String(), ParticipantID: "person-1", CycleID: "cycle-1", FinalRating: values.MustDecimal("3.50", 2, values.RoundingHalfEven), FinalizedAt: instant(), CanonicalDigest: digest('g')},
	}
}

func saveFixture(t *testing.T, f fixture) {
	t.Helper()
	ctx := context.Background()
	if err := f.store.SaveCycle(ctx, f.tenant, f.cycle); err != nil {
		t.Fatalf("SaveCycle: %v", err)
	}
	if err := f.store.SaveRatingCase(ctx, f.tenant, f.caseRow); err != nil {
		t.Fatalf("SaveRatingCase: %v", err)
	}
	if err := f.store.AppendRatingEvent(ctx, f.tenant, f.caseRow.CaseID, 1, f.event); err != nil {
		t.Fatalf("AppendRatingEvent: %v", err)
	}
	if err := f.store.SaveCalibrationSession(ctx, f.tenant, f.session); err != nil {
		t.Fatalf("SaveCalibrationSession: %v", err)
	}
	if err := f.store.SaveReview(ctx, f.tenant, f.review); err != nil {
		t.Fatalf("SaveReview: %v", err)
	}
	if err := f.store.AppendOutcomeLink(ctx, f.tenant, f.outcome); err != nil {
		t.Fatalf("AppendOutcomeLink: %v", err)
	}
	if err := f.store.FinalizeRatingCase(ctx, f.tenant, f.caseRow.CaseID, digest('h')); err != nil {
		t.Fatalf("FinalizeRatingCase: %v", err)
	}
	if err := f.store.SaveFinalRating(ctx, f.tenant, f.caseRow.CaseID, f.final); err != nil {
		t.Fatalf("SaveFinalRating: %v", err)
	}
}

func TestTodo_REV_075_02_PerformanceStore(t *testing.T) {
	db := newDB(t)
	tenant := addTenant(t, db)
	store := performancestore.New(appConn(t, db))
	cycle, err := performance.NewPerformanceCycle("cycle-review",
		performance.PopulationBindingRef{DefinitionID: "population", RevisionVersion: "1", Digest: "sha256:population"},
		performance.CalendarBindingRef{Ref: "calendar", Version: "1", Digest: "sha256:calendar"},
		performance.RatingScaleVersionRef{ID: "scale", Version: "1", Digest: "sha256:scale"})
	if err != nil {
		t.Fatal(err)
	}
	initialRow := performance.CycleRevision{CycleID: cycle.CycleID, Revision: cycle.Revision, State: cycle.State, CanonicalDigest: cycle.CanonicalDigest}
	if err := store.SaveCycle(context.Background(), tenant, initialRow); err != nil {
		t.Fatal(err)
	}
	cycle, err = cycle.Open()
	if err != nil {
		t.Fatal(err)
	}
	cycleRow := performance.CycleRevision{CycleID: cycle.CycleID, Revision: cycle.Revision, State: cycle.State, SupersedesRevision: cycle.Revision - 1, CanonicalDigest: cycle.CanonicalDigest}
	if err := store.SaveCycle(context.Background(), tenant, cycleRow); err != nil {
		t.Fatal(err)
	}
	graph, err := performance.FreezeParticipantReviewerGraph(cycle,
		[]performance.ParticipantRef{{ID: "worker-1"}},
		[]performance.ReviewerAssignment{{ParticipantID: "worker-1", ReviewerID: "reviewer-1", Relationship: performance.ReviewerRelationshipManager}},
		performance.DefaultReviewerGraphRules(), instant())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveParticipantReviewerGraph(context.Background(), tenant, graph); err != nil {
		t.Fatal(err)
	}
	if err := db.ExecErr(`UPDATE performance_participant_reviewer_graph SET graph_digest = graph_digest`); err == nil {
		t.Fatal("frozen participant/reviewer graph accepted UPDATE")
	}
	if err := db.ExecErr(`DELETE FROM performance_participant_reviewer_graph`); err == nil {
		t.Fatal("frozen participant/reviewer graph accepted DELETE")
	}
	reader := performancestore.New(appConn(t, db))
	for _, member := range []string{"worker-1", "reviewer-1"} {
		got, err := reader.ListOpenParticipantReviewerGraphsForMember(context.Background(), tenant, member)
		if err != nil || len(got) != 1 || got[0].Digest != graph.Digest {
			t.Fatalf("member %s graph = %+v, %v", member, got, err)
		}
	}
	for _, member := range []string{"outsider", ""} {
		got, err := reader.ListOpenParticipantReviewerGraphsForMember(context.Background(), tenant, member)
		if member == "" {
			if err == nil {
				t.Fatal("empty member reference was accepted")
			}
			continue
		}
		if err != nil || len(got) != 0 {
			t.Fatalf("outsider graph = %+v, %v", got, err)
		}
	}
	otherTenant := addTenant(t, db)
	got, err := reader.ListOpenParticipantReviewerGraphsForMember(context.Background(), otherTenant, "reviewer-1")
	if err != nil || len(got) != 0 {
		t.Fatalf("cross-tenant graph = %+v, %v", got, err)
	}
	if err := reader.SaveParticipantReviewerGraph(context.Background(), tenant, graph); !errors.Is(err, performance.ErrDuplicateRevision) {
		t.Fatalf("duplicate graph snapshot = %v", err)
	}
}

func TestTodo_PERSIST_PERFORMANCE_001(t *testing.T) {
	f := newFixture(t)
	saveFixture(t, f)
	ctx := context.Background()
	if got, err := f.store.LoadCycle(ctx, f.tenant, f.cycle.CycleID, 1); err != nil || got != f.cycle {
		t.Fatalf("LoadCycle = %+v, %v", got, err)
	}
	if got, err := f.store.LoadRatingCase(ctx, f.tenant, f.caseRow.CaseID); err != nil || !got.Finalized {
		t.Fatalf("LoadRatingCase = %+v, %v", got, err)
	}
	if got, err := f.store.ListRatingEvents(ctx, f.tenant, f.caseRow.CaseID); err != nil || len(got) != 1 || got[0].Digest != f.event.Digest {
		t.Fatalf("events = %+v, %v", got, err)
	}
	if got, err := f.store.LoadCalibrationSession(ctx, f.tenant, f.session.SessionID); err != nil || string(got.Graph) != string(f.session.Graph) {
		t.Fatalf("session = %+v, %v", got, err)
	}
	if got, err := f.store.LoadReview(ctx, f.tenant, f.review.ReviewID, 1); err != nil || got != f.review {
		t.Fatalf("review = %+v, %v", got, err)
	}
	if got, err := f.store.ListOutcomeLinks(ctx, f.tenant, f.outcome.RatingRef); err != nil || len(got) != 1 || got[0].Digest != f.outcome.Digest {
		t.Fatalf("outcomes = %+v, %v", got, err)
	}
	if got, err := f.store.LoadFinalRating(ctx, f.tenant, f.final.ParticipantID, f.final.CycleID); err != nil || got.FinalRating.String() != f.final.FinalRating.String() {
		t.Fatalf("final = %+v, %v", got, err)
	}
}

func TestTodo_PERSIST_PERFORMANCE_001_Fault(t *testing.T) {
	f := newFixture(t)
	saveFixture(t, f)
	ctx := context.Background()
	if err := f.store.SaveCycle(ctx, f.tenant, f.cycle); !errors.Is(err, performance.ErrDuplicateRevision) {
		t.Fatalf("duplicate cycle = %v", err)
	}
	if err := f.store.SaveCycle(ctx, f.tenant, performance.CycleRevision{CycleID: f.cycle.CycleID, Revision: 3, State: performance.PerformanceCycleOpen, SupersedesRevision: 2, CanonicalDigest: digest('i')}); !errors.Is(err, performance.ErrStaleRevision) {
		t.Fatalf("stale cycle = %v", err)
	}
	if err := f.store.AppendRatingEvent(ctx, f.tenant, f.caseRow.CaseID, 1, f.event); !errors.Is(err, performance.ErrDuplicateEvent) {
		t.Fatalf("duplicate event = %v", err)
	}
	if err := f.store.SaveFinalRating(ctx, f.tenant, f.caseRow.CaseID, f.final); !errors.Is(err, performance.ErrDuplicateRevision) {
		t.Fatalf("duplicate final rating = %v", err)
	}
}

func TestTodo_PERSIST_PERFORMANCE_001_Integration(t *testing.T) {
	f := newFixture(t)
	saveFixture(t, f)
	reader := performancestore.New(appConn(t, f.db))
	if got, err := reader.LoadCycle(context.Background(), f.tenant, f.cycle.CycleID, 1); err != nil || got.CanonicalDigest != f.cycle.CanonicalDigest {
		t.Fatalf("fresh connection cycle = %+v, %v", got, err)
	}
	if got, err := reader.LoadFinalRating(context.Background(), f.tenant, f.final.ParticipantID, f.final.CycleID); err != nil || got.RatingRef != f.final.RatingRef {
		t.Fatalf("fresh connection final = %+v, %v", got, err)
	}
}

func TestTodo_PERSIST_PERFORMANCE_001_Security(t *testing.T) {
	f := newFixture(t)
	saveFixture(t, f)
	other := addTenant(t, f.db)
	reader := performancestore.New(appConn(t, f.db))
	if _, err := reader.LoadRatingCase(context.Background(), other, f.caseRow.CaseID); !errors.Is(err, performance.ErrNotFound) {
		t.Fatalf("cross-tenant case = %v", err)
	}
	if _, err := reader.LoadCycle(context.Background(), other, f.cycle.CycleID, 1); !errors.Is(err, performance.ErrNotFound) {
		t.Fatalf("cross-tenant cycle = %v", err)
	}
	if _, err := reader.ListRatingEvents(context.Background(), other, f.caseRow.CaseID); err != nil {
		t.Fatalf("cross-tenant events query: %v", err)
	}
}

func TestTodo_PERSIST_PERFORMANCE_001_Recovery(t *testing.T) {
	f := newFixture(t)
	saveFixture(t, f)
	reader := performancestore.New(appConn(t, f.db))
	got, err := reader.LoadCalibrationSession(context.Background(), f.tenant, f.session.SessionID)
	if err != nil || got.CanonicalDigest != f.session.CanonicalDigest {
		t.Fatalf("recovered session = %+v, %v", got, err)
	}
	gotReview, err := reader.LoadReview(context.Background(), f.tenant, f.review.ReviewID, 1)
	if err != nil || gotReview.ReviewID != f.review.ReviewID {
		t.Fatalf("recovered review = %+v, %v", gotReview, err)
	}
}

func TestTodo_PERSIST_PERFORMANCE_001_Mutation(t *testing.T) {
	f := newFixture(t)
	saveFixture(t, f)
	for _, table := range []string{"performance_cycle", "performance_rating_event", "performance_final_rating", "performance_calibration_session", "performance_review", "performance_outcome_link", "performance_participant_reviewer_graph"} {
		if err := f.db.ExecErr(`UPDATE ` + table + ` SET row_id = row_id`); err == nil {
			t.Errorf("%s accepted UPDATE", table)
		}
		if err := f.db.ExecErr(`DELETE FROM ` + table); err == nil {
			t.Errorf("%s accepted DELETE", table)
		}
	}
	if err := f.store.FinalizeRatingCase(context.Background(), f.tenant, f.caseRow.CaseID, digest('z')); !errors.Is(err, performance.ErrStaleRevision) {
		t.Fatalf("second finalization = %v", err)
	}
}
