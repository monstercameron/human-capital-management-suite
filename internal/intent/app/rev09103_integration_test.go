package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/journeyinvalidation"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/productquery"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// rev09103Cell composes the production cell over real PostgreSQL exactly as
// promoux012Engine does, and returns the cell itself so the test can reach
// the invalidation hub NewCell wired to the journey engine.
func rev09103Cell(t *testing.T) (*app.Cell, func(subject string, roles ...string) context.Context) {
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
	if cell.Journey == nil || cell.JourneyInvalidations == nil {
		t.Fatal("the cell composed no journey engine or no invalidation hub")
	}
	principal := func(subject string, roles ...string) context.Context {
		p, err := trust.NewPrincipal(trust.PrincipalSpec{
			Tenant: fixtures.Tenant, Subject: subject, SubjectKind: trust.SubjectKindHuman,
			OrganizationScopeID:  "org-north-america",
			Roles:                roles,
			AuthorityRefs:        []string{"authority:position:vp-people"},
			Purposes:             []string{authz.PurposeCompensationReview},
			AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
			// Credential validity is wall-clock, the way admission judges it,
			// not the cell's pinned business clock: the hub deliberately
			// keeps wall time so a pinned or advanced business clock cannot
			// revoke a live stream. Minting from `now` made this fixture
			// expire the moment real time passed the pinned date.
			SessionRef: "session-" + subject, IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour),
			CredentialDigest: "credential-digest-" + subject,
		})
		if err != nil {
			t.Fatalf("NewPrincipal(%s): %v", subject, err)
		}
		return trust.WithPrincipal(context.Background(), p)
	}
	return cell, principal
}

func rev09103Subscribe(t *testing.T, cell *app.Cell, viewer context.Context, region promotion.Region) *journeyinvalidation.Subscription {
	t.Helper()
	p, _ := trust.FromContext(viewer)
	sub, err := cell.JourneyInvalidations.Subscribe(journeyinvalidation.SubscribeRequest{Principal: p, Region: region, Inspector: cell.Journey})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(sub.Close)
	return sub
}

func rev09103Next(t *testing.T, viewer context.Context, sub *journeyinvalidation.Subscription, wait time.Duration) (productquery.InvalidationMessage, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(viewer, wait)
	defer cancel()
	raw, err := sub.Next(ctx)
	if err != nil {
		return productquery.InvalidationMessage{}, err
	}
	var message productquery.InvalidationMessage
	if err := json.Unmarshal(raw, &message); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := message.Validate(); err != nil {
		t.Fatalf("invalid hint: %v", err)
	}
	return message, nil
}

// TestTodo_REV_091_03_Integration drives real committed writes through the
// production cell: each successful proposal allocates the next durable
// position with promotioninvalidation.NextSequence after its commit and
// reaches an authorized viewer's subscription as one contiguously numbered
// hint whose item revision is that position -- and the viewer's own engine
// read already sees the committed journey when the hint is filtered, which
// is only possible because the hook ran after the commit.
func TestTodo_REV_091_03_Integration(t *testing.T) {
	cell, principal := rev09103Cell(t)
	author := principal("rev09103-author", "intent_author", string(authz.RoleCompAdmin))
	viewer := principal("rev09103-viewer", string(authz.RoleCompAdmin))
	shell := rev09103Subscribe(t, cell, viewer, promotion.RegionShellCount)
	detail := rev09103Subscribe(t, cell, viewer, promotion.RegionDetail)

	proposed := promoux012Propose(t, cell.Journey, author, "omar-reyes", "2026-06-01")
	rev09103Withdraw(t, cell.Journey, author, proposed.IntentID)

	for i, want := range []uint64{1, 2} {
		message, err := rev09103Next(t, viewer, shell, 10*time.Second)
		if err != nil {
			t.Fatalf("shell hint %d: %v", i+1, err)
		}
		if message.SourceSequence != want || message.Watermark != want-1 {
			t.Fatalf("shell hint %d sequence = %d/%d, want %d/%d", i+1, message.SourceSequence, message.Watermark, want, want-1)
		}
		if message.Items[0].Subject != promotion.ShellCountSubject(fixtures.Tenant) || message.Items[0].Revision != want {
			t.Fatalf("shell hint %d item = %+v, want the shell subject at durable position %d", i+1, message.Items[0], want)
		}
	}
	for range 2 {
		message, err := rev09103Next(t, viewer, detail, 10*time.Second)
		if err != nil {
			t.Fatalf("detail hint: %v", err)
		}
		if got := message.Items[0].Subject; got != journeyinvalidation.JourneyRef(fixtures.Tenant, proposed.IntentID) {
			t.Fatalf("detail hint subject = %v, want journey %s", got, proposed.IntentID)
		}
	}
	if published := cell.JourneyInvalidations.Published(); published != 2 {
		t.Fatalf("hub published %d records for two committed transitions", published)
	}
}

// TestTodo_REV_091_03 proves the hook fires only after a commit: a proposal
// refused by the promotion-window admission (its reservation transaction is
// rolled back and no intent is created) and a proposal refused on input
// publish nothing and allocate no position, and the next committed proposal
// still receives the very next durable position.
func TestTodo_REV_091_03(t *testing.T) {
	cell, principal := rev09103Cell(t)
	author := principal("rev09103-author", "intent_author", string(authz.RoleCompAdmin))
	viewer := principal("rev09103-viewer", string(authz.RoleCompAdmin))
	shell := rev09103Subscribe(t, cell, viewer, promotion.RegionShellCount)

	proposed := promoux012Propose(t, cell.Journey, author, "omar-reyes", "2026-06-01")
	if _, err := rev09103Next(t, viewer, shell, 10*time.Second); err != nil {
		t.Fatalf("committed proposal produced no hint: %v", err)
	}

	_, err := cell.Journey.Propose(author, workspace.ProposalInput{
		WorkerRef: "omar-reyes", TargetJobCode: "OPS-HRBP3", TargetGrade: "P3",
		ProposedBase: "98000.00", EffectiveDate: "2026-06-01", BusinessReason: "rev09103 conflicting proposal",
	})
	if !errors.Is(err, workspace.ErrJourneyActiveConflict) {
		t.Fatalf("conflicting proposal = %v, want ErrJourneyActiveConflict", err)
	}
	if _, err := cell.Journey.Propose(author, workspace.ProposalInput{WorkerRef: "nobody-at-all"}); err == nil {
		t.Fatal("an invalid proposal was accepted")
	}
	if published := cell.JourneyInvalidations.Published(); published != 1 {
		t.Fatalf("hub published %d records; a refused or rolled-back proposal must publish nothing", published)
	}
	if _, err := rev09103Next(t, viewer, shell, 300*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a refused proposal reached the viewer (%v)", err)
	}

	// A withdraw refused on a stale version writes nothing either.
	if _, err := cell.Journey.RequestIntervention(author, proposed.IntentID, workspace.JourneyInterventionRequest{
		Kind: workspace.JourneyInterventionWithdraw, ExpectedInstanceVersion: proposed.GovernanceVersion + 99,
		IdempotencyKey: "idem:rev09103:stale", Reason: "stale version must be refused",
	}); err == nil {
		t.Fatal("a stale withdraw succeeded")
	}
	rev09103Withdraw(t, cell.Journey, author, proposed.IntentID)
	message, err := rev09103Next(t, viewer, shell, 10*time.Second)
	if err != nil {
		t.Fatalf("committed withdraw: %v", err)
	}
	if message.SourceSequence != 2 || message.Items[0].Revision != 2 {
		t.Fatalf("after three refusals the next hint = seq %d revision %d, want 2/2: a refusal allocated a position", message.SourceSequence, message.Items[0].Revision)
	}
}

func rev09103Withdraw(t *testing.T, engine workspace.JourneyEngine, ctx context.Context, intentID string) {
	t.Helper()
	preview, err := engine.PreviewIntervention(ctx, intentID, workspace.JourneyInterventionWithdraw)
	if err != nil || !preview.Available {
		t.Fatalf("PreviewIntervention(WITHDRAW) = %+v, %v", preview, err)
	}
	result, err := engine.RequestIntervention(ctx, intentID, workspace.JourneyInterventionRequest{
		Kind: workspace.JourneyInterventionWithdraw, ExpectedInstanceVersion: preview.CurrentGovernanceVersion,
		IdempotencyKey: "idem:rev09103:withdraw", Reason: "withdrawn before any approval",
	})
	if err != nil || result.Outcome != workspace.InterventionApplied {
		t.Fatalf("RequestIntervention(WITHDRAW) = %+v, %v", result, err)
	}
}
