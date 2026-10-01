package agentrunstate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_AGENT_016_Recovery(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatalf("agent migrations: %v", err)
	}
	now := time.Date(2026, 9, 29, 21, 0, 0, 0, time.UTC)
	tenantA, tenantB := uuid.New(), uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1),($2)`, tenantA, tenantB); err != nil {
		t.Fatalf("seed tenants: %v", err)
	}
	store := openAgentStore(t, db)
	tenantMapper := func(tenant string) uuid.UUID {
		if tenant == "tenant-a" {
			return tenantA
		}
		if tenant == "tenant-b" {
			return tenantB
		}
		return uuid.Nil
	}
	repository, err := New(store, tenantMapper)
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := repository.ForTenant("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	otherTenant, err := repository.ForTenant("tenant-b")
	if err != nil {
		t.Fatal(err)
	}
	admission := testAcceptedAdmission(now)
	if err := insertAcceptedAdmission(ctx, store, tenantA, admission); err != nil {
		t.Fatalf("insert accepted admission fixture: %v", err)
	}
	service, err := runstate.New(tenant, allowRecheck{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.Start(ctx, admission)
	if err != nil {
		t.Fatalf("persist admitted run: %v", err)
	}
	claimed, err := service.Claim(ctx, run.ID, "worker-1", now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := service.BeginEffect(ctx, run.ID, "worker-1", "effect-1", "tool/run-1/effect-1", testDigest("args"), claimed.Fence, claimed.Version, now.Add(100*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	store = openAgentStore(t, db)
	repository, err = New(store, tenantMapper)
	if err != nil {
		t.Fatal(err)
	}
	tenant, err = repository.ForTenant("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	otherTenant, err = repository.ForTenant("tenant-b")
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := runstate.New(tenant, allowRecheck{})
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := restarted.Recover(ctx, run.ID, pending.Version, now.Add(2*time.Second))
	if err != nil || recovered.State != runstate.StateReconciling || recovered.Effects[0].Status != runstate.EffectUnknown || recovered.Lease != nil {
		t.Fatalf("restart did not preserve ambiguity and clear stale lease: run=%+v err=%v", recovered, err)
	}
	observed, err := restarted.ReconcileEffect(ctx, run.ID, "effect-1", recovered.Version, runstate.EffectApplied, "owner-receipt", testDigest("result"), now.Add(3*time.Second))
	if err != nil || observed.State != runstate.StateReady || observed.Effects[0].ResultRef != "owner-receipt" {
		t.Fatalf("owner reconciliation = %+v, %v", observed, err)
	}
	loaded, err := tenant.Get(ctx, run.ID)
	if err != nil || loaded.Version != observed.Version || len(loaded.Checkpoints) != 3 || loaded.Effects[0].Status != runstate.EffectApplied {
		t.Fatalf("round-trip execution evidence = %+v, %v", loaded, err)
	}
	if loaded.PrincipalMode != admission.Request.Principal.Mode || loaded.ActorID != admission.Request.Principal.InvokerID {
		t.Fatalf("restart lost admitted actor and mode: %+v", loaded)
	}
	claimed, err = restarted.Claim(ctx, run.ID, "worker-2", now.Add(4*time.Second), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	declined, err := restarted.BeginEffect(ctx, run.ID, "worker-2", "effect-declined", "tool/run-1/declined", testDigest("declined-args"), claimed.Fence, claimed.Version, now.Add(4100*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err = restarted.ResolveEffect(ctx, run.ID, "worker-2", "effect-declined", declined.Fence, declined.Version, runstate.EffectNotApplied, "", "", now.Add(4200*time.Millisecond))
	if err != nil {
		t.Fatalf("persist declined effect without fabricated result: %v", err)
	}
	loaded, err = tenant.Get(ctx, run.ID)
	if err != nil || loaded.Effects[1].Status != runstate.EffectNotApplied || loaded.Effects[1].ResultRef != "" || loaded.Effects[1].ResultDigest != "" {
		t.Fatalf("declined effect round trip: %+v, %v", loaded, err)
	}
	checkpoint := loaded.Checkpoints[len(loaded.Checkpoints)-1]
	if checkpoint.Ref != "effect-declined" || checkpoint.Digest != testDigest("declined-args") {
		t.Fatalf("declined checkpoint lost admitted intent: %+v", checkpoint)
	}
	parked, err := restarted.Park(ctx, run.ID, "worker-2", claimed.Fence, claimed.Version, runstate.WaitWorkflow, "workflow:receipt-17", now.Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err = tenant.Get(ctx, run.ID)
	if err != nil || loaded.WaitKind != runstate.WaitWorkflow || loaded.WaitRef != "workflow:receipt-17" || loaded.State != runstate.StateWaiting {
		t.Fatalf("durable wait lost owner correlation: %+v, %v", loaded, err)
	}
	resumed, err := restarted.Resume(ctx, run.ID, parked.Version, now.Add(6*time.Second))
	if err != nil || resumed.WaitKind != "" || resumed.WaitRef != "" {
		t.Fatalf("resumed wait retained stale correlation: %+v, %v", resumed, err)
	}
	claimed, err = restarted.Claim(ctx, run.ID, "worker-3", now.Add(7*time.Second), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := restarted.Fail(ctx, run.ID, "worker-3", "PROVIDER_UNAVAILABLE", true, claimed.Fence, claimed.Version, now.Add(8*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err = tenant.Get(ctx, run.ID)
	if err != nil || loaded.State != runstate.StateFailed || !loaded.Retryable || loaded.TerminalCode != failed.TerminalCode {
		t.Fatalf("restart lost sanitized retry state: %+v, %v", loaded, err)
	}
	for _, mutation := range []struct {
		name   string
		change func(*runstate.Run)
	}{
		{name: "checkpoint digest", change: func(r *runstate.Run) { r.Checkpoints[0].Digest = testDigest("forged checkpoint") }},
		{name: "effect identity", change: func(r *runstate.Run) { r.Effects[0].IdempotencyKey = "forged key" }},
		{name: "final result", change: func(r *runstate.Run) { r.Effects[0].ResultRef = "forged receipt" }},
		{name: "removed effect", change: func(r *runstate.Run) { r.Effects = nil }},
	} {
		candidate, err := tenant.Get(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		expected := candidate.Version
		candidate.Version++
		mutation.change(&candidate)
		if err := tenant.Save(ctx, candidate, expected); !errors.Is(err, ErrConflict) {
			t.Fatalf("%s silently changed committed execution evidence: %v", mutation.name, err)
		}
	}
	if _, err := otherTenant.Get(ctx, run.ID); err != ErrNotFound {
		t.Fatalf("other tenant read = %v, want not found", err)
	}
	store.Close()
}

func openAgentStore(t *testing.T, db *pgtest.DB) *agentstore.Store {
	t.Helper()
	ctx := context.Background()
	login, password := "agent_exec_"+strings.ReplaceAll(uuid.NewString(), "-", ""), uuid.NewString()
	if _, err := db.SQL.ExecContext(ctx, fmt.Sprintf("CREATE ROLE %s LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS PASSWORD '%s'", login, password)); err != nil {
		t.Fatalf("create scoped login: %v", err)
	}
	if _, err := db.SQL.ExecContext(ctx, "GRANT "+agentstore.AppRole+" TO "+login); err != nil {
		t.Fatalf("grant agent role: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.SQL.ExecContext(context.Background(), "DROP ROLE "+login); err != nil {
			t.Errorf("drop scoped login: %v", err)
		}
	})
	agentDSN := integrationDSN(t, db.URL, db.Schema, login, password, "postgres")
	coreDSN := integrationDSN(t, db.URL, "", "core-login", "different-secret", "core-db")
	store, err := agentstore.New(ctx, agentstore.Config{DSN: agentDSN, CoreDSN: coreDSN, MaxConns: 1})
	if err != nil {
		t.Fatalf("open scoped agent pool: %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

func insertAcceptedAdmission(ctx context.Context, store *agentstore.Store, tenant uuid.UUID, record agentrun.Record) error {
	request, err := json.Marshal(record.Request)
	if err != nil {
		return err
	}
	authority, err := json.Marshal(record.Authority)
	if err != nil {
		return err
	}
	sourceDigest, err := agentrun.SourceKeyDigest(record.Request.Source)
	if err != nil {
		return err
	}
	return store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO agent_run_request
			(tenant_id,request_id,source_kind,source_key_digest,source_ref,request_digest,request_payload,decision,authority_snapshot,admitted_at,deadline,
			agent_id,agent_version,agent_digest,installation_id,legal_entity_id,principal_chain,purpose,audience,context_scope,budget,cause_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,'ACCEPTED',$8::jsonb,$9,$10,$11,$12,$13,$14,$15,$16::jsonb,$17,$18::jsonb,$19::jsonb,$20::jsonb,$21)`,
			tenant, record.ID, string(record.Request.Source.Kind), sourceDigest, nullable(record.Request.Source.Ref), record.RequestDigest, string(request), string(authority), record.AdmittedAt,
			record.Request.Deadline, record.Request.Agent.AgentID, record.Request.Agent.Version, record.Request.Agent.Digest, record.Request.InstallationID, record.Request.LegalEntity,
			mustJSON(record.Request.Principal), record.Request.Purpose, mustJSON(record.Request.Audience), mustJSON(record.Request.Context), mustJSON(record.Request.Budget), record.Request.CauseID)
		return err
	})
}

func testAcceptedAdmission(now time.Time) agentrun.Record {
	request := agentrun.Request{
		Source: agentrun.SourceIdentity{TenantID: "tenant-a", Kind: agentrun.SourceChat, Key: "message-17"}, LegalEntity: "entity-a",
		Agent: agentrun.VersionRef{AgentID: "agent-a", Version: "v1", Digest: testDigest("agent")}, InstallationID: "install-a",
		Principal: agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, AgentPrincipalID: "agent-principal", InvokerID: "user-a", DelegatedCredentialRef: "credential-a"},
		Purpose:   "chat-assist", Audience: agentrun.AudienceScope{ID: "conversation-a", SnapshotID: "audience-v1", Digest: testDigest("audience")},
		Context: agentrun.ContextScope{ID: "thread-a", SnapshotID: "context-v1", Digest: testDigest("context")}, Deadline: now.Add(time.Hour),
		Budget: agentrun.Budget{MaxCostMicros: 1000, MaxInputTokens: 1000, MaxOutputTokens: 500}, CauseID: "post-17",
	}
	digest, _ := agentrun.AdmissionRequestDigest(request)
	id, _ := agentrun.AdmissionRequestID(request.Source)
	snapshot := agentrun.AuthoritySnapshot{Agent: request.Agent, InstallationID: request.InstallationID, Principal: request.Principal,
		Audience: request.Audience, Context: request.Context, BudgetCeiling: request.Budget, GrantRef: "grant-a", PolicyDigest: testDigest("policy")}
	return agentrun.Record{ID: id, Request: request, RequestDigest: digest, Decision: agentrun.DecisionAccepted, Authority: snapshot, AdmittedAt: now}
}

func integrationDSN(t *testing.T, base, schema, user, password, database string) string {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.User, u.Path = url.UserPassword(user, password), "/"+database
	query := u.Query()
	if schema != "" {
		query.Set("search_path", schema)
	}
	u.RawQuery = query.Encode()
	return u.String()
}

func testDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
func mustJSON(value any) string { encoded, _ := json.Marshal(value); return string(encoded) }

type allowRecheck struct{}

func (allowRecheck) Recheck(context.Context, string, string) error { return nil }
