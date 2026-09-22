package commit_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction"
	transactioncommit "github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/coordinator"
)

// rev00702Row is a stub single-row result. Only the commit-receipt lookup
// finds a row; every other durable lookup reports no rows.
type rev00702Row struct {
	scan func(dest ...any) error
}

func (r rev00702Row) Scan(dest ...any) error { return r.scan(dest...) }

// rev00702Rows is an empty ledger cursor.
type rev00702Rows struct{}

func (rev00702Rows) Next() bool        { return false }
func (rev00702Rows) Scan(...any) error { return errors.New("rev00702: no current row") }
func (rev00702Rows) Err() error        { return nil }
func (rev00702Rows) Close()            {}

// rev00702Store stubs the durable ledger/idempotency reader. Flags select
// which evidence exists: a commit receipt, an abort receipt, and an
// idempotency record with the given status. The ledger cursor is always
// empty.
type rev00702Store struct {
	mu          sync.Mutex
	queries     int
	commitID    uuid.UUID
	commitFound bool
	abortFound  bool
	idemStatus  string
}

func (s *rev00702Store) Query(context.Context, string, ...any) (dbport.Rows, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries++
	return rev00702Rows{}, nil
}

func (s *rev00702Store) QueryRow(_ context.Context, sql string, _ ...any) dbport.Row {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries++
	if strings.Contains(sql, "transaction_commit_receipt") && s.commitFound {
		id := s.commitID
		return rev00702Row{scan: func(dest ...any) error {
			idPtr, okID := dest[0].(*uuid.UUID)
			countPtr, okCount := dest[1].(*int)
			if len(dest) != 2 || !okID || !okCount {
				return fmt.Errorf("rev00702: unexpected commit-receipt scan targets %T", dest)
			}
			*idPtr = id
			*countPtr = 0
			return nil
		}}
	}
	if strings.Contains(sql, "transaction_abort_receipt") && s.abortFound {
		id := s.commitID
		return rev00702Row{scan: func(dest ...any) error {
			idPtr, ok := dest[0].(*uuid.UUID)
			if len(dest) != 1 || !ok {
				return fmt.Errorf("rev00702: unexpected abort-receipt scan targets %T", dest)
			}
			*idPtr = id
			return nil
		}}
	}
	if strings.Contains(sql, "idempotency_record") && s.idemStatus != "" {
		status := s.idemStatus
		return rev00702Row{scan: func(dest ...any) error {
			statusPtr, ok := dest[0].(*string)
			if len(dest) != 5 || !ok {
				return fmt.Errorf("rev00702: unexpected idempotency scan targets %T", dest)
			}
			*statusPtr = status
			return nil
		}}
	}
	return rev00702Row{scan: func(...any) error { return dbport.ErrNoRows }}
}

func (s *rev00702Store) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queries
}

// rev00702Tx severs the connection at commit: every commit acknowledgement
// is lost, so the coordinator must resolve instead of retrying.
type rev00702Tx struct{ commitErr error }

func (t *rev00702Tx) Exec(context.Context, string, ...any) (int64, error) {
	return 1, nil
}
func (t *rev00702Tx) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return rev00702Rows{}, nil
}
func (t *rev00702Tx) QueryRow(context.Context, string, ...any) dbport.Row {
	return rev00702Row{scan: func(...any) error { return dbport.ErrNoRows }}
}
func (t *rev00702Tx) Commit(context.Context) error   { return t.commitErr }
func (t *rev00702Tx) Rollback(context.Context) error { return nil }

type rev00702DB struct {
	mu        sync.Mutex
	begins    int
	commitErr error
}

func (d *rev00702DB) begin() (dbport.Tx, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.begins++
	return &rev00702Tx{commitErr: d.commitErr}, nil
}

func (d *rev00702DB) Begin(context.Context) (dbport.Tx, error) { return d.begin() }

func (d *rev00702DB) BeginSerializable(context.Context) (dbport.Tx, error) {
	return d.begin()
}

func (d *rev00702DB) attempts() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.begins
}

func rev00702Resolution(t *testing.T, planID string) transaction.Resolution {
	t.Helper()
	boundary := transaction.ConsistencyBoundary{
		BoundaryID: "rev00702", Tenant: values.TenantId("tenant"), CellID: "c", CoordinatorID: "k",
		Admitted:  []transaction.AdmissionSelector{{StorageClass: "LOCAL", StreamPrefix: "s."}},
		Isolation: transaction.IsolationSerializable, Protocol: transaction.CommitProtocolSingleDatabaseACID,
		CoordinatorEpoch: 1, CrossBoundaryDisposition: transaction.CrossBoundaryDispositionSplitIntoEffects,
	}
	planned := intent.TransactionPlan{
		PlanID: planID, Tenant: "tenant",
		Participants: []intent.PlanParticipant{
			{ParticipantID: "a", StreamID: "s.a", StorageClass: "LOCAL", Local: true},
			{ParticipantID: "b", StreamID: "s.b", StorageClass: "LOCAL", Local: true},
		},
	}
	resolved, err := transaction.ResolveConsistencyBoundary(boundary, planned, 1)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func rev00702Writes(applied *int) []coordinator.Write {
	return []coordinator.Write{
		{ParticipantID: "a", Apply: func(context.Context, dbport.Tx) error { *applied++; return nil }},
		{ParticipantID: "b", Apply: func(context.Context, dbport.Tx) error { *applied++; return nil }},
	}
}

// TestTodo_REV_007_02 proves the production construction wires the
// coordinator's ResolveAmbiguous seam to the durable store: the hook is set,
// it consults the store, a committed receipt resolves to the receipt, a
// foreign plan is refused before any store read, and incomplete bindings fail
// closed at construction.
func TestTodo_REV_007_02(t *testing.T) {
	ctx := context.Background()
	tenant := uuid.New()
	planID, key := "rev00702-plan", "rev00702-key"
	for name, mutate := range map[string]func(*dbport.Querier, *uuid.UUID, *string, *string){
		"nil store":  func(store *dbport.Querier, _ *uuid.UUID, _ *string, _ *string) { *store = nil },
		"nil tenant": func(_ *dbport.Querier, tenant *uuid.UUID, _ *string, _ *string) { *tenant = uuid.Nil },
		"empty plan": func(_ *dbport.Querier, _ *uuid.UUID, plan *string, _ *string) { *plan = "" },
		"empty key":  func(_ *dbport.Querier, _ *uuid.UUID, _ *string, key *string) { *key = "" },
	} {
		var store dbport.Querier = &rev00702Store{}
		boundTenant, boundPlan, boundKey := tenant, planID, key
		mutate(&store, &boundTenant, &boundPlan, &boundKey)
		if _, err := transactioncommit.AmbiguousResolver(store, boundTenant, boundPlan, boundKey); err == nil {
			t.Fatalf("%s: expected a fail-closed construction error", name)
		}
	}

	store := &rev00702Store{commitID: uuid.New(), commitFound: true}
	admitted := false
	base := coordinator.RetryOptions{
		MaxAttempts: 7,
		Admit:       func(context.Context) error { admitted = true; return nil },
		Prepare: func(context.Context, dbport.Tx) (coordinator.CommitRequest, error) {
			return coordinator.CommitRequest{}, nil
		},
	}
	opts, err := transactioncommit.WithAmbiguousRecovery(base, store, tenant, planID, key)
	if err != nil {
		t.Fatalf("WithAmbiguousRecovery: %v", err)
	}
	if opts.ResolveAmbiguous == nil {
		t.Fatal("WithAmbiguousRecovery left ResolveAmbiguous unset")
	}
	if opts.MaxAttempts != base.MaxAttempts || opts.Admit == nil || opts.Prepare == nil {
		t.Fatal("WithAmbiguousRecovery did not carry the caller's retry policy through")
	}

	want := coordinator.Receipt{PlanID: planID, ResolutionDigest: "digest", Participants: []string{"a"}}
	got, err := opts.ResolveAmbiguous(ctx, want)
	if err != nil {
		t.Fatalf("resolve committed: %v", err)
	}
	if got.PlanID != want.PlanID || got.ResolutionDigest != want.ResolutionDigest || len(got.Participants) != 1 || got.Participants[0] != "a" {
		t.Fatalf("resolved receipt = %+v, want %+v", got, want)
	}
	if store.count() == 0 {
		t.Fatal("resolver answered without consulting the durable store")
	}

	before := store.count()
	if _, err := opts.ResolveAmbiguous(ctx, coordinator.Receipt{PlanID: "foreign-plan"}); !errors.Is(err, coordinator.ErrCommitAmbiguous) {
		t.Fatalf("foreign-plan error = %v, want ErrCommitAmbiguous", err)
	}
	if store.count() != before {
		t.Fatal("foreign-plan resolution touched the durable store")
	}
	if admitted {
		t.Fatal("resolution ran the admission closure")
	}
	for _, tc := range []struct {
		name  string
		store *rev00702Store
	}{
		{"contradictory receipts stay ambiguous", &rev00702Store{commitID: uuid.New(), commitFound: true, abortFound: true}},
		{"reserved idempotency requires recovery", &rev00702Store{idemStatus: "RESERVED"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hook, err := transactioncommit.AmbiguousResolver(tc.store, tenant, planID, key)
			if err != nil {
				t.Fatalf("AmbiguousResolver: %v", err)
			}
			_, err = hook(ctx, want)
			if !errors.Is(err, coordinator.ErrCommitAmbiguous) {
				t.Fatalf("unresolved evidence error = %v, want ErrCommitAmbiguous", err)
			}
			if errors.Is(err, transactioncommit.ErrCommitAbsent) {
				t.Fatalf("unresolved evidence error = %v, must not read as absent", err)
			}
		})
	}
}

// TestTodo_REV_007_02_Fault severs the connection at commit after the plan is
// already durable and proves the coordinator returns the recovered COMMITTED
// receipt instead of a bare ambiguous error, with the writes applied exactly
// once. The unwired run first reproduces the RED gap: bare
// ErrCommitAmbiguous with no verdict.
func TestTodo_REV_007_02_Fault(t *testing.T) {
	ctx := context.Background()
	db, prepared, tenant := commitFixture(t)
	if _, err := committer(t, db).Commit(ctx, prepared); err != nil {
		t.Fatalf("durable pre-commit: %v", err)
	}
	resolved := rev00702Resolution(t, prepared.PlanID)
	severed := errors.New("rev00702: connection reset by peer")

	bare := &rev00702DB{commitErr: severed}
	applied := 0
	bareOpts := coordinator.RetryOptions{
		MaxAttempts: 3,
		Admit:       func(context.Context) error { return nil },
		Sleep:       func(context.Context, time.Duration) error { return nil },
		Prepare: func(context.Context, dbport.Tx) (coordinator.CommitRequest, error) {
			return coordinator.CommitRequest{Plan: resolved, Writes: rev00702Writes(&applied)}, nil
		},
	}
	if _, err := coordinator.New(bare).CommitWithRetry(ctx, coordinator.CommitRequest{}, bareOpts); !errors.Is(err, coordinator.ErrCommitAmbiguous) {
		t.Fatalf("unwired commit error = %v, want bare ErrCommitAmbiguous", err)
	}

	wired := &rev00702DB{commitErr: severed}
	wiredApplied := 0
	wiredOpts, err := transactioncommit.WithAmbiguousRecovery(coordinator.RetryOptions{
		MaxAttempts: 3,
		Admit:       func(context.Context) error { return nil },
		Sleep:       func(context.Context, time.Duration) error { return nil },
		Prepare: func(context.Context, dbport.Tx) (coordinator.CommitRequest, error) {
			return coordinator.CommitRequest{Plan: resolved, Writes: rev00702Writes(&wiredApplied)}, nil
		},
	}, db.Conn, tenant, prepared.PlanID, prepared.IdempotencyKey)
	if err != nil {
		t.Fatalf("WithAmbiguousRecovery: %v", err)
	}
	receipt, err := coordinator.New(wired).CommitWithRetry(ctx, coordinator.CommitRequest{}, wiredOpts)
	if err != nil {
		t.Fatalf("wired ambiguous commit: %v, want the recovered COMMITTED receipt", err)
	}
	if receipt.PlanID != prepared.PlanID {
		t.Fatalf("recovered receipt plan = %q, want %q", receipt.PlanID, prepared.PlanID)
	}
	if wired.attempts() != 1 || wiredApplied != 2 {
		t.Fatalf("attempts=%d writes=%d, want exactly one attempt with no replay", wired.attempts(), wiredApplied)
	}
}

// TestTodo_REV_007_02_Recovery severs the connection at commit with nothing
// durable and proves the coordinator returns the recovered ABSENT verdict:
// the typed ErrCommitAbsent that stays distinguishable from an unresolved
// ambiguity, with the writes still applied exactly once.
func TestTodo_REV_007_02_Recovery(t *testing.T) {
	ctx := context.Background()
	db, prepared, tenant := commitFixture(t)
	resolved := rev00702Resolution(t, prepared.PlanID)
	severed := errors.New("rev00702: connection reset by peer")

	hook, err := transactioncommit.AmbiguousResolver(db.Conn, tenant, prepared.PlanID, prepared.IdempotencyKey)
	if err != nil {
		t.Fatalf("AmbiguousResolver: %v", err)
	}
	if _, err := hook(ctx, coordinator.Receipt{PlanID: prepared.PlanID}); !errors.Is(err, transactioncommit.ErrCommitAbsent) {
		t.Fatalf("direct absent resolution = %v, want ErrCommitAbsent", err)
	} else if errors.Is(err, coordinator.ErrCommitAmbiguous) {
		t.Fatalf("direct absent resolution = %v, must not read as unresolved ambiguity", err)
	}

	wired := &rev00702DB{commitErr: severed}
	applied := 0
	opts, err := transactioncommit.WithAmbiguousRecovery(coordinator.RetryOptions{
		MaxAttempts: 3,
		Admit:       func(context.Context) error { return nil },
		Sleep:       func(context.Context, time.Duration) error { return nil },
		Prepare: func(context.Context, dbport.Tx) (coordinator.CommitRequest, error) {
			return coordinator.CommitRequest{Plan: resolved, Writes: rev00702Writes(&applied)}, nil
		},
	}, db.Conn, tenant, prepared.PlanID, prepared.IdempotencyKey)
	if err != nil {
		t.Fatalf("WithAmbiguousRecovery: %v", err)
	}
	_, err = coordinator.New(wired).CommitWithRetry(ctx, coordinator.CommitRequest{}, opts)
	if !errors.Is(err, transactioncommit.ErrCommitAbsent) {
		t.Fatalf("absent commit error = %v, want the recovered ABSENT verdict", err)
	}
	if !errors.Is(err, coordinator.ErrCommitAmbiguous) {
		t.Fatalf("absent commit error = %v, want the coordinator ambiguity classification preserved", err)
	}
	if wired.attempts() != 1 || applied != 2 {
		t.Fatalf("attempts=%d writes=%d, want exactly one attempt with no replay", wired.attempts(), applied)
	}
}
