package app_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// promoux012NoExecutor satisfies ProposalExecutor so NewCell composes its
// journey engine. Nothing in this file executes a journey; every method
// refuses, so a test that reached one would fail loudly.
type promoux012NoExecutor struct{}

var errPromoux012NoExecution = errors.New("promoux012: execution is not part of this fixture")

func (promoux012NoExecutor) Execute(context.Context, runtime.StartRequest) (app.ExecutionResult, error) {
	return app.ExecutionResult{}, errPromoux012NoExecution
}

func (promoux012NoExecutor) Resume(context.Context, app.ExecutionResumeRequest) (app.ExecutionResult, error) {
	return app.ExecutionResult{}, errPromoux012NoExecution
}

func (promoux012NoExecutor) ResumeTimer(context.Context, app.ExecutionTimerResumeRequest) (app.ExecutionResult, error) {
	return app.ExecutionResult{}, errPromoux012NoExecution
}

// promoux012Engine composes a real cell over real PostgreSQL (pgtest): the
// production pgstore intent store and the production NewCell path, its
// intent service, its journey engine and that engine's tenant-scoped reads,
// all on one connection pool exactly as internal/application composes them.
// It returns the journey engine plus a principal factory.
func promoux012Engine(t *testing.T) (workspace.JourneyEngine, func(subject string) context.Context) {
	t.Helper()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	store, err := pgstore.New(pool)
	if err != nil {
		t.Fatalf("pgstore.New: %v", err)
	}
	if err := store.Bootstrap(context.Background(), string(fixtures.Tenant)); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	cell, err := app.NewCell(app.CellConfig{
		Store:       store,
		Verifier:    trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return nil, nil }),
		Audience:    "hcm-next-api",
		ExecutionDB: pool,
		Executor:    promoux012NoExecutor{},
		TenantUUID:  func(tenant values.TenantId) uuid.UUID { return pgstore.TenantID(string(tenant)) },
		Now:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewCell: %v", err)
	}
	if cell.Journey == nil {
		t.Fatal("the cell composed no journey engine")
	}
	principal := func(subject string) context.Context {
		p, err := trust.NewPrincipal(trust.PrincipalSpec{
			Tenant: fixtures.Tenant, Subject: subject, SubjectKind: trust.SubjectKindHuman,
			OrganizationScopeID:  "org-north-america",
			Roles:                []string{"intent_author", string(authz.RoleCompAdmin)},
			AuthorityRefs:        []string{"authority:position:vp-people"},
			Purposes:             []string{authz.PurposeCompensationReview},
			AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
			SessionRef: "session-" + subject, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
			CredentialDigest: "credential-digest-" + subject,
		})
		if err != nil {
			t.Fatalf("NewPrincipal(%s): %v", subject, err)
		}
		return trust.WithPrincipal(context.Background(), p)
	}
	return cell.Journey, principal
}

func promoux012Propose(t *testing.T, engine workspace.JourneyEngine, ctx context.Context, worker, effective string) workspace.JourneySummary {
	t.Helper()
	proposed, err := engine.Propose(ctx, workspace.ProposalInput{
		WorkerRef: worker, TargetJobCode: "OPS-HRBP3", TargetGrade: "P3",
		ProposedBase: "98000.00", EffectiveDate: effective, BusinessReason: "promoux012 fixture",
	})
	if err != nil {
		t.Fatalf("Propose(%s, %s): %v", worker, effective, err)
	}
	return proposed
}

func promoux012Listed(t *testing.T, engine workspace.JourneyEngine, ctx context.Context, intentID string) workspace.JourneySummary {
	t.Helper()
	listed, err := engine.ListJourneys(ctx)
	if err != nil {
		t.Fatalf("ListJourneys: %v", err)
	}
	for _, summary := range listed {
		if summary.IntentID == intentID {
			return summary
		}
	}
	t.Fatalf("ListJourneys did not return %s", intentID)
	return workspace.JourneySummary{}
}

// TestTodo_PROMOUX_012_Integration reaches a real PostgreSQL server through
// the production cell composition and proves the viewer projection is
// resolved from durable state by the engine, not asserted by a page: the
// principal CreateIntent recorded as initiator sees the proposal as their
// own actionable step (start approval, held by the proposer) on the list,
// on Propose's own answer and on Inspect, while a second principal in the
// same tenant sees the same journey with no relationship at all.
func TestTodo_PROMOUX_012_Integration(t *testing.T) {
	engine, principal := promoux012Engine(t)
	author, colleague := principal("principal:promoux012-author"), principal("principal:promoux012-colleague")

	proposed := promoux012Propose(t, engine, author, "omar-reyes", "2026-06-01")
	if proposed.Stage != workspace.JourneyStageProposed {
		t.Fatalf("fixture assumption broken: the proposal is %s, not PROPOSED", proposed.Stage)
	}
	wantAuthor := workspace.JourneyViewerProjection{
		Relationships:  []workspace.JourneyViewerRelationship{workspace.JourneyViewerInitiator},
		Responsibility: workspace.JourneyResponsibilityActionRequired,
		NextStep:       workspace.JourneyNextStepStartApproval, NextStepOwner: workspace.JourneyStepOwnerProposer, AwaitsPerson: true,
	}
	if !reflect.DeepEqual(proposed.Viewer, wantAuthor) {
		t.Fatalf("Propose's own viewer projection = %+v, want %+v", proposed.Viewer, wantAuthor)
	}
	if got := promoux012Listed(t, engine, author, proposed.IntentID).Viewer; !reflect.DeepEqual(got, wantAuthor) {
		t.Fatalf("the initiator's listed projection = %+v, want %+v", got, wantAuthor)
	}
	detail, err := engine.Inspect(author, proposed.IntentID)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !reflect.DeepEqual(detail.Summary.Viewer, wantAuthor) {
		t.Fatalf("Inspect's projection = %+v, want the list's %+v", detail.Summary.Viewer, wantAuthor)
	}

	wantColleague := workspace.JourneyViewerProjection{
		Responsibility: workspace.JourneyResponsibilityObserving,
		NextStep:       workspace.JourneyNextStepStartApproval, NextStepOwner: workspace.JourneyStepOwnerProposer, AwaitsPerson: true,
	}
	if got := promoux012Listed(t, engine, colleague, proposed.IntentID).Viewer; !reflect.DeepEqual(got, wantColleague) {
		t.Fatalf("a non-initiator's listed projection = %+v, want %+v", got, wantColleague)
	}
}

// TestTodo_PROMOUX_012_Security proves, against real PostgreSQL, that the
// relationship projection discloses nothing new to a viewer without a
// relationship. Two different principals initiate two journeys; an outsider
// listing both receives byte-for-byte the same projection for each, so it
// cannot tell who initiated either, or whether they were initiated by the
// same person. No field of any summary the outsider receives carries either
// initiator's principal id (the list names no initiator at all), and the
// initiators themselves learn only their own standing: each sees INITIATOR on
// exactly their own journey and nothing on the other's.
func TestTodo_PROMOUX_012_Security(t *testing.T) {
	engine, principal := promoux012Engine(t)
	const alice, bob = "principal:promoux012-alice", "principal:promoux012-bob"
	aliceCtx, bobCtx, outsider := principal(alice), principal(bob), principal("principal:promoux012-outsider")

	aliceJourney := promoux012Propose(t, engine, aliceCtx, "omar-reyes", "2026-06-01")
	bobJourney := promoux012Propose(t, engine, bobCtx, "omar-reyes", "2026-07-01")

	outsiderList, err := engine.ListJourneys(outsider)
	if err != nil {
		t.Fatalf("ListJourneys(outsider): %v", err)
	}
	seen := map[string]workspace.JourneyViewerProjection{}
	for _, summary := range outsiderList {
		seen[summary.IntentID] = summary.Viewer
		dump := fmt.Sprintf("%+v", summary)
		if strings.Contains(dump, alice) || strings.Contains(dump, bob) {
			t.Fatalf("an outsider's summary carries an initiator identity: %s", dump)
		}
	}
	aliceSeen, okA := seen[aliceJourney.IntentID]
	bobSeen, okB := seen[bobJourney.IntentID]
	if !okA || !okB {
		t.Fatalf("fixture assumption broken: the outsider must see both journeys, saw %v", seen)
	}
	if !reflect.DeepEqual(aliceSeen, bobSeen) {
		t.Fatalf("the outsider can distinguish initiators: %+v versus %+v", aliceSeen, bobSeen)
	}
	if len(aliceSeen.Relationships) != 0 || aliceSeen.Responsibility != workspace.JourneyResponsibilityObserving {
		t.Fatalf("an outsider was given a relationship or a responsibility: %+v", aliceSeen)
	}

	for _, c := range []struct {
		name     string
		ctx      context.Context
		own      string
		others   string
		initiate string
	}{
		{"alice", aliceCtx, aliceJourney.IntentID, bobJourney.IntentID, alice},
		{"bob", bobCtx, bobJourney.IntentID, aliceJourney.IntentID, bob},
	} {
		own := promoux012Listed(t, engine, c.ctx, c.own).Viewer
		if !reflect.DeepEqual(own.Relationships, []workspace.JourneyViewerRelationship{workspace.JourneyViewerInitiator}) {
			t.Fatalf("%s's own journey relationships = %v, want INITIATOR", c.name, own.Relationships)
		}
		other := promoux012Listed(t, engine, c.ctx, c.others).Viewer
		if len(other.Relationships) != 0 || other.Responsibility != workspace.JourneyResponsibilityObserving {
			t.Fatalf("%s gained standing on the other principal's journey: %+v", c.name, other)
		}
		if !reflect.DeepEqual(other, aliceSeen) {
			t.Fatalf("%s's view of the other journey differs from an outsider's: %+v versus %+v", c.name, other, aliceSeen)
		}
	}
}
