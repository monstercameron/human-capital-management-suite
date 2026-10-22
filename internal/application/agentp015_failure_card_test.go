package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona/limits"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// TestTodo_AGENTP_015_MentionLimitClassification: a mention refused by a
// ceiling has an outcome of its own that is not retryable, and the time to ask
// again is the refusal's retry time counted from when it was recorded.
func TestTodo_AGENTP_015_MentionLimitClassification(t *testing.T) {
	now := time.Date(2026, 10, 2, 9, 20, 0, 0, time.UTC)
	rate := &PersonaMentionLimitError{Code: limits.DenialInvokerRate, Scope: limits.ScopeInvoker, RetryAfter: 40 * time.Minute, Observed: 30}
	if code, retryable := personaPostFailureClassification(rate); code != "MENTION_LIMIT_REACHED" || retryable {
		t.Fatalf("classification = %q retryable %v", code, retryable)
	}
	wrapped := errors.Join(errors.New("admission"), rate)
	if code, _ := personaPostFailureClassification(wrapped); code != "MENTION_LIMIT_REACHED" {
		t.Fatalf("a wrapped refusal classified as %q", code)
	}
	if retryAt, asked := personaMentionLimitOutcome(rate, now); !retryAt.Equal(now.Add(40*time.Minute)) || asked != 30 {
		t.Fatalf("hourly ceiling outcome = %v, %d; want 10:00 and 30", retryAt, asked)
	}
	// Only the person's own hourly ceiling is "you asked N times".
	room := &PersonaMentionLimitError{Code: limits.DenialConversationRate, Scope: limits.ScopeConversation, RetryAfter: 10 * time.Minute, Observed: 120}
	if retryAt, asked := personaMentionLimitOutcome(room, now); !retryAt.Equal(now.Add(10*time.Minute)) || asked != 0 {
		t.Fatalf("room ceiling outcome = %v, %d; want the time and no count", retryAt, asked)
	}
	// The concurrent-run ceiling frees as soon as a run returns, so the card
	// names a near time and not the end of the hour.
	busy := &PersonaMentionLimitError{Code: limits.DenialInvokerConcurrency, Scope: limits.ScopeInvoker, RetryAfter: 40 * time.Minute, Observed: 3}
	if retryAt, asked := personaMentionLimitOutcome(busy, now); !retryAt.Equal(now.Add(personaConcurrencyRetry)) || asked != 0 {
		t.Fatalf("concurrency outcome = %v, %d", retryAt, asked)
	}
	if code, _ := personaPostFailureClassification(errors.New("something else")); code == "MENTION_LIMIT_REACHED" {
		t.Fatal("an unrelated error classified as a mention limit")
	}
}

// TestTodo_AGENTP_015_FailureCardServed sends the thirty-first mention of an
// hour through the served invocation path with the durable failure store of the
// runtime database role, then reads back what the page is sent: the outcome is
// MENTION_LIMIT_REACHED, it is not retryable, and it carries when the person can
// ask again and how many times they asked.
func TestTodo_AGENTP_015_FailureCardServed(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenantUUID := pgstore.TenantID("tenant-a")
	db.Exec(t, `INSERT INTO tenant (tenant_id) VALUES ($1)`, tenantUUID)
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatal(err)
	}
	personas, err := agentpersonastore.New(conn, func(tenant values.TenantId) uuid.UUID {
		if tenant == "tenant-a" {
			return tenantUUID
		}
		return uuid.Nil
	})
	if err != nil {
		t.Fatal(err)
	}
	agents := commonAgentOpenIntegrationStore(t, db)
	logged := &agentp015Failures{}
	sink, err := newPersonaDurableInvocationFailureSink(agents, logged)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 2, 9, 20, 0, 0, time.UTC)
	streaming := &streamingChatService{ConversationService: servedPortChatWriter{writer: &personaChatWriterFake{}}}
	refs := &lazyPersonaReferenceSource{}
	refs.bind(servedPortReferenceSource{lookup: &personaReferenceLookupFake{}})
	config := PersonaInvocationProductionConfig{
		Authority: personaAuthorityFake{admission: personaAdmission()}, Grants: &personaGrantFake{}, T0Skills: personaT0PolicyFake{allowed: true},
		Fence: &personaRunWorkerFenceFake{}, Leases: personaRunWorkerLeaseFake{id: "lease"},
		Run: PersonaRunStarterConfig{
			Builder: mustPersonaRunBuilder(t), Authority: personaChatAdmissionAuthorityFake{},
			Model: personaRunWorkerModelFake{}, Work: &personaRunWorkerWorkFake{},
			Output: &personaRunWorkerOutputFake{}, Reply: personaRunWorkerReplyFake{},
			WorkerID: "served-worker", LeaseTTL: time.Minute, Now: func() time.Time { return now },
		},
	}
	runtime, err := composeServedPersonaInvocation(streaming, &personaServeWiring{refs: refs, now: func() time.Time { return now }},
		composedAgentDatabase{store: &agentstore.Store{}, personas: personas}, &config, &recordingLogger{})
	if err != nil {
		t.Fatal(err)
	}
	served := runtime.Wiring.Chat.limits
	worker := &agentp015Worker{}
	invocation, err := newPersonaChatInvocation(personaChatInvocationConfig{
		Chat: &agentp015Posts{}, References: agentp015References{},
		Authority: personaAuthorityFake{admission: personaAdmission(), requireTuple: true}, Grants: agentp015Grants{},
		Runs: worker, T0Skills: personaT0PolicyFake{allowed: true}, Repository: agentinvoke.NewMemoryRepository(),
		Failures: sink, Limits: served,
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 30 {
		if _, err := invocation.SendPost(agentp015Context(t, "alice", now), agentp015Mention("alice")); err != nil {
			t.Fatal(err)
		}
	}
	store, err := agentinvocationstore.NewWithTenantUUID(agents, pgstore.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := store.ListPostFailures(ctx, "tenant-a", "alice", "channel-a"); err != nil || len(rows) != 0 {
		t.Fatalf("thirty admitted mentions left failures %+v, %v", rows, err)
	}
	before := time.Now().UTC()
	if _, err := invocation.SendPost(agentp015Context(t, "alice", now), agentp015Mention("alice")); err != nil {
		t.Fatal(err)
	}
	after := time.Now().UTC()
	if worker.count() != 30 {
		t.Fatalf("the refused mention reached the worker: %d runs", worker.count())
	}
	rows, err := store.ListPostFailures(ctx, "tenant-a", "alice", "channel-a")
	if err != nil || len(rows) != 1 {
		t.Fatalf("stored failures = %+v, %v", rows, err)
	}
	failure := rows[0]
	if failure.Code != "MENTION_LIMIT_REACHED" || failure.Retryable || failure.MentionsAsked != 30 {
		t.Fatalf("stored outcome = %+v", failure)
	}
	if failure.RetryAt.Before(before.Add(40*time.Minute-time.Second)) || failure.RetryAt.After(after.Add(40*time.Minute+time.Second)) {
		t.Fatalf("retry time = %v, want forty minutes after %v", failure.RetryAt, before)
	}
	looked, err := store.LookupPostFailure(ctx, "tenant-a", "alice", failure.PostID)
	if err != nil || !looked.RetryAt.Equal(failure.RetryAt) || looked.MentionsAsked != 30 {
		t.Fatalf("lookup = %+v, %v", looked, err)
	}
	// What the page is sent.
	var sent personachat.Invocation
	projectPersonaPostFailure(&sent, failure)
	retryAt, parseErr := time.Parse(time.RFC3339, sent.RetryAt)
	if parseErr != nil || !retryAt.Equal(failure.RetryAt.Truncate(time.Second)) || sent.MentionsAsked != 30 || sent.FailureCode != "MENTION_LIMIT_REACHED" || sent.Retryable {
		t.Fatalf("projection = %+v (%v)", sent, parseErr)
	}
	// Another person is not shown it, and another failure carries no time.
	if rows, err := store.ListPostFailures(ctx, "tenant-a", "bob", "channel-a"); err != nil || len(rows) != 0 {
		t.Fatalf("another person's failures = %+v, %v", rows, err)
	}
	var plain personachat.Invocation
	projectPersonaPostFailure(&plain, agentinvocationstore.PostFailure{Code: "MODEL_UNAVAILABLE", Retryable: true})
	if plain.RetryAt != "" || plain.MentionsAsked != 0 {
		t.Fatalf("an unrelated failure carries a retry time: %+v", plain)
	}
	// The table refuses a mention-limit outcome with no time to ask again.
	err = store.RecordPostFailure(ctx, agentinvocationstore.PostFailure{TenantID: "tenant-a", InvokerID: "alice", ConversationID: "channel-a", ThreadID: "t", PostID: "post-x", Code: "MENTION_LIMIT_REACHED"})
	if !errors.Is(err, agentinvocationstore.ErrInvalid) {
		t.Fatalf("a mention limit with no retry time was stored: %v", err)
	}
}
