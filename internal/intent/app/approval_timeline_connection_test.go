package app_test

import (
	"context"
	"errors"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	intentapp "github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

type approvalTimelineOrderStore struct {
	*pgstore.Store
	released *atomic.Bool
	calls    atomic.Int32
	err      error
}

func (s *approvalTimelineOrderStore) Timeline(ctx context.Context, tenant, intentID string) ([]intentapp.TimelineEntry, error) {
	s.calls.Add(1)
	if !s.released.Load() {
		return nil, errors.New("timeline read occurred before detail transaction release")
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.Store.Timeline(ctx, tenant, intentID)
}

type approvalTimelineOrderDB struct {
	dbport.Beginner
	released *atomic.Bool
}

type approvalTimelineOrderTx struct {
	dbport.Tx
	released *atomic.Bool
}

func (d approvalTimelineOrderDB) Begin(ctx context.Context) (dbport.Tx, error) {
	d.released.Store(false)
	tx, err := d.Beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return approvalTimelineOrderTx{Tx: tx, released: d.released}, nil
}

func (t approvalTimelineOrderTx) Commit(ctx context.Context) error {
	err := t.Tx.Commit(ctx)
	if err == nil {
		t.released.Store(true)
	}
	return err
}

func (t approvalTimelineOrderTx) Rollback(ctx context.Context) error {
	err := t.Tx.Rollback(ctx)
	if err == nil {
		t.released.Store(true)
	}
	return err
}

func approvalTimelineOrderFixture(t *testing.T) (workspace.JourneyEngine, context.Context, context.Context, *approvalTimelineOrderStore) {
	t.Helper()
	db := pgtest.New(t)
	poolURL, err := url.Parse(db.URL)
	if err != nil {
		t.Fatalf("parse pool URL: %v", err)
	}
	poolParams := poolURL.Query()
	poolParams.Set("pool_max_conns", "1")
	poolURL.RawQuery = poolParams.Encode()
	pool, err := pgxadapter.NewPool(context.Background(), poolURL.String(), map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	released := &atomic.Bool{}
	store, err := pgstore.New(pool)
	if err != nil {
		t.Fatalf("pgstore.New: %v", err)
	}
	if err := store.Bootstrap(context.Background(), string(fixtures.Tenant)); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	orderedStore := &approvalTimelineOrderStore{Store: store, released: released}
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	cell, err := intentapp.NewCell(intentapp.CellConfig{
		Store:    orderedStore,
		Verifier: trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return nil, nil }),
		Audience: "hcm-next-api", ExecutionDB: approvalTimelineOrderDB{Beginner: pool, released: released},
		Executor: promoux012NoExecutor{}, TenantUUID: func(tenant values.TenantId) uuid.UUID { return pgstore.TenantID(string(tenant)) },
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewCell: %v", err)
	}
	principal := func(tenant values.TenantId, subject string) context.Context {
		p, err := trust.NewPrincipal(trust.PrincipalSpec{
			Tenant: tenant, Subject: subject, SubjectKind: trust.SubjectKindHuman, OrganizationScopeID: "org-north-america",
			Roles: []string{"intent_author", string(authz.RoleCompAdmin)}, AuthorityRefs: []string{"authority:position:vp-people"},
			Purposes: []string{authz.PurposeCompensationReview}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
			Assurance: trust.AssuranceHigh, SessionRef: "session-" + subject, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
			CredentialDigest: "credential-digest-" + subject,
		})
		if err != nil {
			t.Fatalf("NewPrincipal: %v", err)
		}
		return trust.WithPrincipal(context.Background(), p)
	}
	author := principal(fixtures.Tenant, "principal:approval-timeline-author")
	denied := principal("tenant-not-owner", "principal:approval-timeline-denied")
	return cell.Journey, author, denied, orderedStore
}

func TestApprovalTimelineConnection_InspectReleasesBeforeTimelineAndPropagates(t *testing.T) {
	engine, author, denied, store := approvalTimelineOrderFixture(t)
	proposed, err := engine.Propose(author, workspace.ProposalInput{WorkerRef: "omar-reyes", TargetJobCode: "OPS-HRBP3", TargetGrade: "P3", ProposedBase: "98000.00", EffectiveDate: "2026-06-01", BusinessReason: "timeline connection fixture"})
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if _, err := engine.Inspect(author, proposed.IntentID); err != nil {
		t.Fatalf("Inspect author: %v", err)
	}
	if store.calls.Load() != 1 {
		t.Fatalf("Timeline calls = %d, want 1", store.calls.Load())
	}
	if !store.released.Load() {
		t.Fatal("detail transaction was not released before Timeline")
	}
	beforeDenied := store.calls.Load()
	if _, err := engine.Inspect(denied, proposed.IntentID); !errors.Is(err, workspace.ErrJourneyUnknown) {
		t.Fatalf("denied Inspect = %v, want ErrJourneyUnknown", err)
	}
	if store.calls.Load() != beforeDenied {
		t.Fatalf("denied Inspect called Timeline: before=%d after=%d", beforeDenied, store.calls.Load())
	}
	sentinel := errors.New("timeline fixture unavailable")
	store.err = sentinel
	if _, err := engine.Inspect(author, proposed.IntentID); !errors.Is(err, sentinel) {
		t.Fatalf("timeline failure = %v, want wrapped sentinel", err)
	}
}
