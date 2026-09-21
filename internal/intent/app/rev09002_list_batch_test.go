package app_test

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// REV-090-02: ListJourneys resolves a page in a bounded number of database
// statements and never re-simulates an unexecuted proposal.

// rev09002Recorder counts and records every statement issued through the
// execution database while recording is on.
type rev09002Recorder struct {
	mu        sync.Mutex
	recording bool
	sql       []string
}

func (r *rev09002Recorder) note(sql string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.recording {
		r.sql = append(r.sql, sql)
	}
}

func (r *rev09002Recorder) capture(fn func()) []string {
	r.mu.Lock()
	r.recording, r.sql = true, nil
	r.mu.Unlock()
	fn()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recording = false
	return append([]string(nil), r.sql...)
}

type rev09002Beginner struct {
	inner    dbport.Beginner
	recorder *rev09002Recorder
}

func (b rev09002Beginner) Begin(ctx context.Context) (dbport.Tx, error) {
	tx, err := b.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return rev09002Tx{Tx: tx, recorder: b.recorder}, nil
}

type rev09002Tx struct {
	dbport.Tx
	recorder *rev09002Recorder
}

func (t rev09002Tx) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	t.recorder.note(sql)
	return t.Tx.Exec(ctx, sql, args...)
}

func (t rev09002Tx) Query(ctx context.Context, sql string, args ...any) (dbport.Rows, error) {
	t.recorder.note(sql)
	return t.Tx.Query(ctx, sql, args...)
}

func (t rev09002Tx) QueryRow(ctx context.Context, sql string, args ...any) dbport.Row {
	t.recorder.note(sql)
	return t.Tx.QueryRow(ctx, sql, args...)
}

// rev09002Store counts LoadIntent: re-simulating a journey loads its intent,
// listing does not.
type rev09002Store struct {
	app.Store
	loads atomic.Int64
}

func (s *rev09002Store) LoadIntent(ctx context.Context, tenant, intentID string) (app.IntentRecord, error) {
	s.loads.Add(1)
	return s.Store.LoadIntent(ctx, tenant, intentID)
}

func rev09002Engine(t *testing.T) (workspace.JourneyEngine, context.Context, *rev09002Recorder, *rev09002Store) {
	t.Helper()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	inner, err := pgstore.New(pool)
	if err != nil {
		t.Fatalf("pgstore.New: %v", err)
	}
	if err := inner.Bootstrap(context.Background(), string(fixtures.Tenant)); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	store := &rev09002Store{Store: inner}
	recorder := &rev09002Recorder{}
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	cell, err := app.NewCell(app.CellConfig{
		Store:       store,
		Verifier:    trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return nil, nil }),
		Audience:    "hcm-next-api",
		ExecutionDB: rev09002Beginner{inner: pool, recorder: recorder},
		Executor:    promoux012NoExecutor{},
		TenantUUID:  func(tenant values.TenantId) uuid.UUID { return pgstore.TenantID(string(tenant)) },
		Now:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewCell: %v", err)
	}
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: fixtures.Tenant, Subject: "principal:rev09002-author", SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID:  "org-north-america",
		Roles:                []string{"intent_author", "comp_admin"},
		AuthorityRefs:        []string{"authority:position:vp-people"},
		Purposes:             []string{"compensation_review"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-rev09002", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		CredentialDigest: "credential-digest-rev09002",
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return cell.Journey, trust.WithPrincipal(context.Background(), p), recorder, store
}

func rev09002List(t *testing.T, engine workspace.JourneyEngine, ctx context.Context, recorder *rev09002Recorder, store *rev09002Store) ([]workspace.JourneySummary, []string, int64) {
	t.Helper()
	var listed []workspace.JourneySummary
	before := store.loads.Load()
	statements := recorder.capture(func() {
		var err error
		listed, err = engine.ListJourneys(ctx)
		if err != nil {
			t.Fatalf("ListJourneys: %v", err)
		}
	})
	return listed, statements, store.loads.Load() - before
}

var rev09002Dates = []string{"2026-06-01", "2026-07-01", "2026-08-01", "2026-09-01", "2026-10-01"}

// TestTodo_REV_090_02 is the PRIMARY case: the listed stage, revision and
// digest of every unexecuted proposal are exactly what Propose answered, and
// producing them loads no intent for re-simulation.
func TestTodo_REV_090_02(t *testing.T) {
	engine, ctx, recorder, store := rev09002Engine(t)
	proposed := map[string]workspace.JourneySummary{}
	for _, date := range rev09002Dates[:3] {
		summary := promoux012Propose(t, engine, ctx, "omar-reyes", date)
		proposed[summary.IntentID] = summary
	}
	listed, _, loads := rev09002List(t, engine, ctx, recorder, store)
	if loads != 0 {
		t.Fatalf("ListJourneys loaded %d intents for re-simulation, want 0", loads)
	}
	seen := 0
	for _, summary := range listed {
		want, ok := proposed[summary.IntentID]
		if !ok {
			continue
		}
		seen++
		if want.Stage != workspace.JourneyStageProposed || want.ProposalRevisionID == "" {
			t.Fatalf("fixture assumption broken: Propose answered %s with revision %q", want.Stage, want.ProposalRevisionID)
		}
		if summary.Stage != want.Stage || summary.ProposalRevisionID != want.ProposalRevisionID || summary.MaterialDigest != want.MaterialDigest {
			t.Fatalf("listed %s = (%s, %q, %q), Propose answered (%s, %q, %q)", summary.IntentID,
				summary.Stage, summary.ProposalRevisionID, summary.MaterialDigest, want.Stage, want.ProposalRevisionID, want.MaterialDigest)
		}
	}
	if seen != len(proposed) {
		t.Fatalf("ListJourneys returned %d of the %d proposals", seen, len(proposed))
	}
}

// TestTodo_REV_090_02_Benchmark is the PERFORMANCE case: the number of
// execution-database statements ListJourneys issues does not grow with the
// number of journeys listed.
func TestTodo_REV_090_02_Benchmark(t *testing.T) {
	engine, ctx, recorder, store := rev09002Engine(t)
	promoux012Propose(t, engine, ctx, "omar-reyes", rev09002Dates[0])
	one, small, _ := rev09002List(t, engine, ctx, recorder, store)
	for _, date := range rev09002Dates[1:] {
		promoux012Propose(t, engine, ctx, "omar-reyes", date)
	}
	many, large, loads := rev09002List(t, engine, ctx, recorder, store)
	if len(many) != len(one)+len(rev09002Dates)-1 {
		t.Fatalf("listed %d journeys after %d more proposals, started from %d", len(many), len(rev09002Dates)-1, len(one))
	}
	if len(large) != len(small) {
		t.Fatalf("listing %d journeys issued %d statements, listing %d issued %d: the read is O(N)",
			len(many), len(large), len(one), len(small))
	}
	// One tenant-scoping statement plus the bounded list reads.
	if limit := 1 + 8; len(large) > limit {
		t.Fatalf("ListJourneys issued %d statements, want at most %d:\n%s", len(large), limit, strings.Join(large, "\n---\n"))
	}
	if loads != 0 {
		t.Fatalf("ListJourneys loaded %d intents for re-simulation, want 0", loads)
	}
}

// TestTodo_REV_090_02_Golden pins the exact statement plan of an unexecuted
// page: the tenant scope, then one read each of the stored proposals, their
// simulation answers and the work items that would name an instance. Nothing
// else, in this order, for any page size.
func TestTodo_REV_090_02_Golden(t *testing.T) {
	engine, ctx, recorder, store := rev09002Engine(t)
	for _, date := range rev09002Dates[:2] {
		promoux012Propose(t, engine, ctx, "omar-reyes", date)
	}
	_, statements, _ := rev09002List(t, engine, ctx, recorder, store)
	table := regexp.MustCompile(`(?i)\bFROM\s+([a-z_]+)`)
	var plan []string
	for _, sql := range statements {
		match := table.FindStringSubmatch(sql)
		switch {
		case match != nil:
			plan = append(plan, "read "+match[1])
		case strings.Contains(strings.ToLower(sql), "set_config") || strings.Contains(strings.ToLower(sql), "set local"):
			plan = append(plan, "scope tenant")
		default:
			plan = append(plan, "other "+strings.Fields(sql)[0])
		}
	}
	const golden = "scope tenant\nread proposal_revision\nread intent_simulation_result\nread work_item"
	if got := strings.Join(plan, "\n"); got != golden {
		t.Fatalf("ListJourneys statement plan:\n%s\nwant:\n%s", got, golden)
	}
}
