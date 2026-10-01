package agentrunstore

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENT_015_Repository(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 123456000, time.UTC)
	request := requestForAdmission(now)
	runner := newAdmissionRunner()
	store, err := NewAdmissionRepository(runner, uuid.New(), values.TenantId("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	authority := repoAuthority{snapshot: repoSnapshot(request)}
	service, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: authority, Store: store, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}

	first, created, err := service.Admit(context.Background(), request)
	if err != nil || !created || first.Decision != agentrun.DecisionAccepted {
		t.Fatalf("first admission = (%+v,%t,%v)", first, created, err)
	}
	second, created, err := service.Admit(context.Background(), request)
	if err != nil || created || second.ID != first.ID || second.Authority.GrantRef != first.Authority.GrantRef {
		t.Fatalf("duplicate admission = (%+v,%t,%v)", second, created, err)
	}
	if got := runner.count(); got != 1 {
		t.Fatalf("durable source rows = %d, want one", got)
	}
	if strings.Contains(runner.onlyPayload(), request.Source.Key) {
		t.Fatal("raw source key leaked into the request payload")
	}

	changed := request
	changed.Purpose = "changed purpose"
	if _, _, err := service.Admit(context.Background(), changed); !errors.Is(err, agentrun.ErrSourceConflict) {
		t.Fatalf("changed source replay = %v, want conflict", err)
	}

	refused := request
	refused.Source.Key = "event-88"
	refused.Source.Ref = ""
	refusalService, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{
		Authority: repoAuthority{err: &agentrun.AdmissionRefusal{Code: "GRANT_REVOKED"}}, Store: store, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	record, created, err := refusalService.Admit(context.Background(), refused)
	if err != nil || !created || record.Decision != agentrun.DecisionRefused || record.RefusalCode != "GRANT_REVOKED" {
		t.Fatalf("refusal persistence = (%+v,%t,%v)", record, created, err)
	}
	if got := runner.onlyRefusal(); got.decision != string(agentrun.DecisionRefused) || string(got.authority) != "null" {
		t.Fatalf("stored refusal row = %+v", got)
	}
}

func TestTodo_AGENT_015_Repository_Fault(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	request := requestForAdmission(now)
	tenantID := uuid.New()
	if _, err := NewAdmissionRepository(nil, tenantID, values.TenantId("tenant-a")); !errors.Is(err, agentrun.ErrInvalidRequest) {
		t.Fatalf("nil runner constructor = %v", err)
	}
	if _, err := NewAdmissionRepository(newAdmissionRunner(), uuid.Nil, values.TenantId("tenant-a")); !errors.Is(err, agentrun.ErrInvalidRequest) {
		t.Fatalf("nil tenant UUID constructor = %v", err)
	}
	store, err := NewAdmissionRepository(newAdmissionRunner(), tenantID, values.TenantId("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	wrongTenant := request
	wrongTenant.Source.TenantID = "tenant-b"
	if _, _, err := store.CreateOrGet(ctx, repoAcceptedRecord(t, wrongTenant, now)); !errors.Is(err, agentrun.ErrInvalidRequest) {
		t.Fatalf("cross-tenant write = %v", err)
	}

	transient := errors.New("transaction failed")
	runner := newAdmissionRunner()
	runner.err = transient
	store, err = NewAdmissionRepository(runner, tenantID, values.TenantId("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateOrGet(ctx, repoAcceptedRecord(t, request, now)); !errors.Is(err, transient) {
		t.Fatalf("transaction failure = %v, want original error", err)
	}
	if runner.count() != 0 {
		t.Fatal("failed tenant transaction inserted a row")
	}
}

// TestTodo_AGENT_015_Race runs concurrent duplicate admissions through the
// real isolated agent-store migration and PostgreSQL unique constraint.
func TestTodo_AGENT_015_Race(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatalf("migrate agent database: %v", err)
	}
	tenantA, tenantB := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{tenantA, tenantB} {
		if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant (tenant_id) VALUES ($1)`, id); err != nil {
			t.Fatalf("create local tenant projection: %v", err)
		}
	}
	dsn, err := agentApplicationDSN(db.URL, db.Schema)
	if err != nil {
		t.Fatal(err)
	}
	ownerStore, err := agentstore.New(ctx, agentstore.Config{DSN: dsn, CoreDSN: "postgres://core:pw@core.invalid:5432/core", MaxConns: 8})
	if err != nil {
		t.Fatalf("open agent application pool: %v", err)
	}
	t.Cleanup(ownerStore.Close)

	now := time.Date(2026, 9, 29, 12, 0, 0, 123456000, time.UTC)
	request := requestForAdmission(now)
	request.Source.Kind = agentrun.SourceSchedule
	request.Source.Key = "schedule-7/occurrence-2026-09-29T12:00Z"
	request.Source.Ref = "schedule-7/occurrence-2026-09-29T12:00Z"
	repository, err := NewAdmissionRepository(ownerStore, tenantA, values.TenantId("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: repoAuthority{snapshot: repoSnapshot(request)}, Store: repository, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	const deliveries = 20
	start := make(chan struct{})
	var wait sync.WaitGroup
	var created atomic.Int32
	errs := make(chan error, deliveries)
	ids := make(chan string, deliveries)
	for i := 0; i < deliveries; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			record, inserted, admitErr := service.Admit(ctx, request)
			if admitErr != nil {
				errs <- admitErr
				return
			}
			if inserted {
				created.Add(1)
			}
			ids <- record.ID
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		t.Errorf("concurrent PostgreSQL admission: %v", err)
	}
	if got := created.Load(); got != 1 {
		t.Fatalf("concurrent deliveries created %d requests, want one", got)
	}
	requestID := ""
	for id := range ids {
		if requestID == "" {
			requestID = id
		} else if id != requestID {
			t.Errorf("delivery returned different run IDs %q and %q", id, requestID)
		}
	}
	var count int
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM agent_run_request WHERE tenant_id=$1`, tenantA).Scan(&count); err != nil || count != 1 {
		t.Fatalf("tenant A persisted rows = %d, %v; want one", count, err)
	}
	changed := request
	changed.Purpose = "changed purpose"
	if _, _, err := service.Admit(ctx, changed); !errors.Is(err, agentrun.ErrSourceConflict) {
		t.Fatalf("changed payload replay = %v, want conflict", err)
	}

	// The same source occurrence is independent in another tenant, while the
	// first tenant cannot see the second tenant's admission through RLS.
	requestB := request
	requestB.Source.TenantID = "tenant-b"
	repositoryB, err := NewAdmissionRepository(ownerStore, tenantB, values.TenantId("tenant-b"))
	if err != nil {
		t.Fatal(err)
	}
	serviceB, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: repoAuthority{snapshot: repoSnapshot(requestB)}, Store: repositoryB, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, inserted, err := serviceB.Admit(ctx, requestB); err != nil || !inserted {
		t.Fatalf("same source key in tenant B = inserted %t, err %v", inserted, err)
	}
	if err := ownerStore.RunTenantTx(ctx, tenantA, func(tx dbport.Tx) error {
		var visible int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM agent_run_request`).Scan(&visible); err != nil {
			return err
		}
		if visible != 1 {
			t.Fatalf("tenant A RLS exposed %d request rows, want one", visible)
		}
		return nil
	}); err != nil {
		t.Fatalf("check tenant request isolation: %v", err)
	}
}

func agentApplicationDSN(rawURL, schema string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	query.Set("options", "-c role=hcmnext_agent_app")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func repoAcceptedRecord(t *testing.T, request agentrun.Request, now time.Time) agentrun.Record {
	t.Helper()
	service, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: repoAuthority{snapshot: repoSnapshot(request)}, Store: agentrun.NewMemoryAdmissionStore(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := service.Admit(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

type repoAuthority struct {
	snapshot agentrun.AuthoritySnapshot
	err      error
}

func (a repoAuthority) VerifyAdmission(context.Context, agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	return a.snapshot, a.err
}

func requestForAdmission(now time.Time) agentrun.Request {
	return agentrun.Request{
		Source:      agentrun.SourceIdentity{TenantID: "tenant-a", Kind: agentrun.SourceEvent, Key: "event-87/persona-3", Ref: "event-87"},
		Persona:     &agentrun.PersonaRef{ID: "persona-3", Version: "4", Digest: testRequestDigest("persona-v4")},
		LegalEntity: "entity-a", Agent: agentrun.VersionRef{AgentID: "agent-3", Version: "v2", Digest: testRequestDigest("agent-version")},
		InstallationID: "install-1", Principal: agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, AgentPrincipalID: "agent-principal-3", InvokerID: "user-7", DelegatedCredentialRef: "delegation-grant-7"},
		Purpose: "read-only answer", Audience: agentrun.AudienceScope{ID: "conversation-8", SnapshotID: "audience-4", Digest: testRequestDigest("audience")},
		Context:  agentrun.ContextScope{ID: "thread-9", SnapshotID: "context-12", Digest: testRequestDigest("context")},
		Deadline: now.Add(time.Minute), Budget: agentrun.Budget{MaxCostMicros: 500, MaxInputTokens: 2000, MaxOutputTokens: 500}, CauseID: "cause-87",
	}
}

func repoSnapshot(request agentrun.Request) agentrun.AuthoritySnapshot {
	return agentrun.AuthoritySnapshot{Agent: request.Agent, InstallationID: request.InstallationID, Principal: request.Principal, Audience: request.Audience, Context: request.Context,
		BudgetCeiling: agentrun.Budget{MaxCostMicros: 1000, MaxInputTokens: 3000, MaxOutputTokens: 1000}, GrantRef: "grant:7", PolicyDigest: testRequestDigest("policy")}
}

func testRequestDigest(text string) string {
	return "sha256:" + strings.Repeat("a", 64)
}

type admissionRow struct {
	id, digest, decision, refusal string
	authority                     []byte
	admitted                      time.Time
	payload                       []byte
	keyHash                       string
}

type admissionRunner struct {
	mu   sync.Mutex
	rows map[string]admissionRow
	err  error
}

func newAdmissionRunner() *admissionRunner {
	return &admissionRunner{rows: make(map[string]admissionRow)}
}

func (r *admissionRunner) RunTenantTx(_ context.Context, _ uuid.UUID, fn func(dbport.Tx) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	return fn(admissionTx{runner: r})
}

func (r *admissionRunner) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.rows)
}

func (r *admissionRunner) onlyPayload() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, row := range r.rows {
		return string(row.payload)
	}
	return ""
}

func (r *admissionRunner) onlyRefusal() admissionRow {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, row := range r.rows {
		if row.decision == string(agentrun.DecisionRefused) {
			return row
		}
	}
	return admissionRow{}
}

type admissionTx struct{ runner *admissionRunner }

func (tx admissionTx) Exec(_ context.Context, _ string, args ...any) (int64, error) {
	key := args[0].(uuid.UUID).String() + "\x00" + args[2].(string) + "\x00" + args[3].(string)
	if _, ok := tx.runner.rows[key]; ok {
		return 0, nil
	}
	row := admissionRow{id: args[1].(string), keyHash: args[3].(string), digest: args[5].(string), payload: []byte(args[6].(string)), decision: args[7].(string), admitted: args[10].(time.Time)}
	if value, ok := args[8].(string); ok {
		row.refusal = value
	}
	if value, ok := args[9].(string); ok {
		row.authority = []byte(value)
	} else {
		row.authority = []byte("null")
	}
	tx.runner.rows[key] = row
	return 1, nil
}

func (tx admissionTx) QueryRow(_ context.Context, _ string, args ...any) dbport.Row {
	key := args[0].(uuid.UUID).String() + "\x00" + args[1].(string) + "\x00" + args[2].(string)
	row, ok := tx.runner.rows[key]
	return admissionResult{row: row, ok: ok}
}

func (admissionTx) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("unexpected query")
}
func (admissionTx) Commit(context.Context) error   { return nil }
func (admissionTx) Rollback(context.Context) error { return nil }

type admissionResult struct {
	row admissionRow
	ok  bool
}

func (r admissionResult) Scan(dest ...any) error {
	if !r.ok {
		return dbport.ErrNoRows
	}
	*dest[0].(*string) = r.row.id
	*dest[1].(*string) = r.row.digest
	*dest[2].(*string) = r.row.decision
	*dest[3].(*string) = r.row.refusal
	*dest[4].(*[]byte) = append([]byte(nil), r.row.authority...)
	*dest[5].(*time.Time) = r.row.admitted
	return nil
}
