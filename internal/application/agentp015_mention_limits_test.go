package application

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona/limits"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// agentp015Posts commits each human message as a new post, so every send is a
// new mention rather than a replay of the first.
type agentp015Posts struct {
	mu   sync.Mutex
	next int
}

func (w *agentp015Posts) SendPost(_ context.Context, request chatcore.SendPostRequest) (chatcore.Post, error) {
	w.mu.Lock()
	w.next++
	id := fmt.Sprintf("post-%d", w.next)
	w.mu.Unlock()
	return chatcore.Post{
		ID: id, TenantID: request.TenantID, ConversationID: request.ConversationID, AuthorID: request.Principal.SubjectID,
		Body: request.Body, Revision: 1, References: append([]chatcore.Reference(nil), request.References...),
	}, nil
}

type agentp015References struct{}

func (agentp015References) ResolvePersonaMentions(context.Context, string, string, []chatcore.Reference) ([]agentinvoke.Mention, error) {
	return []agentinvoke.Mention{{Kind: agentinvoke.PersonaMention, PersonaID: "persona-comp", Canonical: true}}, nil
}

type agentp015Grants struct{}

func (agentp015Grants) CreateOnBehalfOfGrant(_ context.Context, request agentinvoke.GrantRequest) (agentinvoke.DelegationGrant, error) {
	return agentinvoke.DelegationGrant{ID: "grant-" + request.InvocationID, UserID: request.UserID, TenantID: request.TenantID, TaskID: request.InvocationID, TargetAgentID: request.TargetAgentID, Skills: request.Skills.Clone(), ExpiresAt: request.ExpiresAt}, nil
}

// agentp015Worker stands where the model worker stands. It counts the runs it
// is asked to start; a run that reaches it is a run that would call the model.
// When hold is set each run waits there until it is let go.
type agentp015Worker struct {
	mu      sync.Mutex
	started []agentinvoke.RunRequest
	err     error
	hold    chan struct{}
	entered chan struct{}
}

func (w *agentp015Worker) Start(_ context.Context, request agentinvoke.RunRequest) error {
	w.mu.Lock()
	w.started = append(w.started, request)
	err, hold, entered := w.err, w.hold, w.entered
	w.mu.Unlock()
	if hold != nil {
		entered <- struct{}{}
		<-hold
	}
	return err
}

func (w *agentp015Worker) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.started)
}

func (w *agentp015Worker) set(err error, hold, entered chan struct{}) {
	w.mu.Lock()
	w.err, w.hold, w.entered = err, hold, entered
	w.mu.Unlock()
}

type agentp015Failures struct {
	mu   sync.Mutex
	errs []error
}

func (f *agentp015Failures) RecordPersonaInvocationFailure(_ context.Context, _ string, err error) {
	f.mu.Lock()
	f.errs = append(f.errs, err)
	f.mu.Unlock()
}

func (f *agentp015Failures) take() []error {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.errs
	f.errs = nil
	return out
}

func agentp015Context(t *testing.T, subject string, now time.Time) context.Context {
	t.Helper()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: subject, SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow,
		SessionRef: "agentp-015-" + subject, CredentialDigest: "test-credential", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), principal)
}

func agentp015Mention(subject string) chatcore.SendPostRequest {
	request := personaSendRequest()
	request.Principal.SubjectID = subject
	return request
}

// TestTodo_AGENTP_015_ServedIntegration proves the mention ceilings are
// enforced where mentions are served. The served composition is built over a
// real agent database and must bind the durable limit store; mentions are then
// sent through the invocation coordinator the served chat calls, with that
// same limiter, and the worker stands for the model.
func TestTodo_AGENTP_015_ServedIntegration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenantUUID := uuid.New()
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
	// 09:20: the hourly windows end in forty minutes.
	now := time.Date(2026, 10, 2, 9, 20, 0, 0, time.UTC)

	// The served assembly, as ComposeServe builds it.
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
	if served == nil {
		t.Fatal("the served invocation path has no mention limits")
	}
	if served.Policy != servedPersonaMentionLimitPolicy() || served.Policy.InvokerPerPersona.Max != 30 || served.Policy.InvokerConcurrency != 3 || served.Policy.Conversation.Max != 120 {
		t.Fatalf("served mention policy = %+v", served.Policy)
	}
	if stores, ok := served.Stores.(AgentPersonaMentionLimitStores); !ok || stores.Store != personas {
		t.Fatalf("served mention limits are not bound to the agent database: %#v", served.Stores)
	}
	if config.Limits != nil {
		t.Fatal("serve composition mutated the caller's options")
	}
	// A production runtime without limits is refused outright.
	unmetered := config
	unmetered.AgentStore, unmetered.TenantUUID = &agentstore.Store{}, func(values.TenantId) uuid.UUID { return tenantUUID }
	unmetered.Chat, unmetered.References = &agentp015Posts{}, agentp015References{}
	if _, err := NewDatabasePersonaInvocationProductionRuntime(unmetered); !errors.Is(err, errPersonaInvocationProductionComposition) || !errors.Is(err, errPersonaMentionLimits) {
		t.Fatalf("a runtime with no mention limits was composed: %v", err)
	}

	worker, failures := &agentp015Worker{}, &agentp015Failures{}
	invocation, err := newPersonaChatInvocation(personaChatInvocationConfig{
		Chat: &agentp015Posts{}, References: agentp015References{},
		Authority: personaAuthorityFake{admission: personaAdmission(), requireTuple: true}, Grants: agentp015Grants{},
		Runs: worker, T0Skills: personaT0PolicyFake{allowed: true}, Repository: agentinvoke.NewMemoryRepository(),
		Failures: failures, Limits: served,
	})
	if err != nil {
		t.Fatal(err)
	}
	mention := func(t *testing.T, subject string) {
		t.Helper()
		// The human post is durable whatever admission decides.
		if post, err := invocation.SendPost(agentp015Context(t, subject, now), agentp015Mention(subject)); err != nil || post.ID == "" {
			t.Errorf("%s's post = %+v, %v", subject, post, err)
		}
	}
	bucket := func(t *testing.T, kind, key string) (admissions, active, used, reserved int64) {
		t.Helper()
		if err := db.QueryRow(ctx, `SELECT admission_count,active_count,used_spend,reserved_spend FROM persona_limit_buckets WHERE tenant_id=$1 AND bucket_kind=$2 AND bucket_key=$3`,
			tenantUUID, kind, key).Scan(&admissions, &active, &used, &reserved); err != nil {
			t.Fatalf("read %s bucket %q: %v", kind, key, err)
		}
		return
	}
	estimate := served.SpendEstimateMicros

	t.Run("the thirty-first mention in an hour is refused before the model", func(t *testing.T) {
		for range 30 {
			mention(t, "alice")
		}
		if got := worker.count(); got != 30 || len(failures.take()) != 0 {
			t.Fatalf("thirty mentions started %d runs", got)
		}
		mention(t, "alice")
		if got := worker.count(); got != 30 {
			t.Fatalf("the thirty-first mention reached the worker: %d runs", got)
		}
		refused := failures.take()
		var limit *PersonaMentionLimitError
		if len(refused) != 1 || !errors.As(refused[0], &limit) || !errors.Is(refused[0], limits.ErrDenied) {
			t.Fatalf("refusal = %v", refused)
		}
		if limit.Code != limits.DenialInvokerRate || limit.Scope != limits.ScopeInvoker || limit.RetryAfter != 40*time.Minute {
			t.Fatalf("typed refusal = %+v, want the per-person hourly ceiling and forty minutes", limit)
		}
		admissions, active, _, _ := bucket(t, "INVOKER", "tenant-a|alice|persona-comp|1")
		if admissions != 30 || active != 0 {
			t.Fatalf("alice's durable count = %d admitted, %d running", admissions, active)
		}
		// The ceiling is per person: someone else in the same conversation is
		// still answered, and the conversation counts both.
		mention(t, "bob")
		if got := worker.count(); got != 31 || len(failures.take()) != 0 {
			t.Fatalf("bob's mention started %d runs in total", got)
		}
		if admissions, _, _, _ := bucket(t, "CONVERSATION", "tenant-a|channel-a"); admissions != 31 {
			t.Fatalf("conversation count = %d, want 31", admissions)
		}
		if _, _, used, reserved := bucket(t, "PERSONA", "tenant-a|persona-comp"); used != 31*estimate || reserved != 0 {
			t.Fatalf("agent spend = %d used, %d reserved; want %d and 0", used, reserved, 31*estimate)
		}
	})

	t.Run("a run that never started is refunded and a run that failed is counted", func(t *testing.T) {
		_, _, before, _ := bucket(t, "PERSONA", "tenant-a|persona-comp")
		worker.set(agentinvoke.ErrDenied, nil, nil)
		mention(t, "carol")
		if _, _, used, reserved := bucket(t, "PERSONA", "tenant-a|persona-comp"); used != before || reserved != 0 {
			t.Fatalf("a refused run changed spend: %d used (was %d), %d reserved", used, before, reserved)
		}
		worker.set(&PersonaRunFailure{Code: "MODEL_TIMEOUT", Retryable: true, kind: ErrPersonaRunModelFailure}, nil, nil)
		mention(t, "carol")
		if _, _, used, reserved := bucket(t, "PERSONA", "tenant-a|persona-comp"); used != before+estimate || reserved != 0 {
			t.Fatalf("a run that failed at the model was not counted: %d used (was %d), %d reserved", used, before, reserved)
		}
		if admissions, active, _, _ := bucket(t, "INVOKER", "tenant-a|carol|persona-comp|1"); admissions != 2 || active != 0 {
			t.Fatalf("carol's durable count = %d admitted, %d running", admissions, active)
		}
		if got := len(failures.take()); got != 2 {
			t.Fatalf("recorded failures = %d, want the two run errors", got)
		}
		worker.set(nil, nil, nil)
	})

	t.Run("a fourth mention while three are running is refused", func(t *testing.T) {
		before := worker.count()
		hold, entered := make(chan struct{}), make(chan struct{})
		worker.set(nil, hold, entered)
		daveCtx, returned := agentp015Context(t, "dave", now), make(chan error)
		for range 3 {
			go func() {
				_, err := invocation.SendPost(daveCtx, agentp015Mention("dave"))
				returned <- err
			}()
			// Each run is inside the worker, its reservation open, before the next
			// is sent.
			<-entered
		}
		// The fourth is refused at admission, so it never reaches the held worker.
		mention(t, "dave")
		refused := failures.take()
		var limit *PersonaMentionLimitError
		if len(refused) != 1 || !errors.As(refused[0], &limit) || limit.Code != limits.DenialInvokerConcurrency {
			t.Fatalf("fourth concurrent mention: %v", refused)
		}
		if got := worker.count(); got != before+3 {
			t.Fatalf("runs while three were held = %d, want %d", got, before+3)
		}
		if _, active, _, _ := bucket(t, "INVOKER", "tenant-a|dave|persona-comp|1"); active != 3 {
			t.Fatalf("dave's running count = %d, want 3", active)
		}
		// Let one finish at a time; when all have, a new mention is admitted.
		for range 3 {
			hold <- struct{}{}
			if err := <-returned; err != nil {
				t.Fatalf("a held mention's post failed: %v", err)
			}
		}
		worker.set(nil, nil, nil)
		if _, active, _, _ := bucket(t, "INVOKER", "tenant-a|dave|persona-comp|1"); active != 0 {
			t.Fatalf("dave's running count after the runs returned = %d", active)
		}
		mention(t, "dave")
		if got := worker.count(); got != before+4 || len(failures.take()) != 0 {
			t.Fatalf("a mention after the runs returned was not admitted: %d runs", got)
		}
	})
}
