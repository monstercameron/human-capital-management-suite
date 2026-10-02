package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// The tests below drive the real run store, invocation store and background
// dispatcher on the shared test PostgreSQL. Only the model is a fixture: it
// answers the same words every time and counts how often it is called.

const agentRunRecoveryTenant = "tenant-a"

type agentRunRecoveryModel struct {
	calls   atomic.Int32
	delay   time.Duration
	started chan struct{}
}

func (m *agentRunRecoveryModel) Execute(ctx context.Context, _ AgentModelExecutorRequest) (AgentModelExecutorResult, error) {
	m.calls.Add(1)
	if m.started != nil {
		select {
		case m.started <- struct{}{}:
		default:
		}
	}
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return AgentModelExecutorResult{}, ctx.Err()
		}
	}
	return AgentModelExecutorResult{Result: agentmodel.ModelResult{Text: "Answer", Finish: agentmodel.FinishComplete}}, nil
}

// agentRunRecoveryWork builds the model request from the run it is given, so one
// fixture serves every run of a test.
type agentRunRecoveryWork struct{}

func (agentRunRecoveryWork) BuildPersonaRunModelWork(_ context.Context, _ agentrun.Record, run runstate.Run) (PersonaRunModelWork, error) {
	return PersonaRunModelWork{Request: AgentModelExecutorRequest{Task: TrustedModelTask{TaskID: run.ID, TenantID: run.TenantID, AgentID: run.AgentDigest}}}, nil
}

type agentRunRecoveryHarness struct {
	t           *testing.T
	ctx         context.Context
	db          *pgtest.DB
	pool        *agentstore.Store
	tenant      uuid.UUID
	admissions  *agentrun.AdmissionService
	store       *agentrunstate.TenantStore
	state       *runstate.Service
	invocations *agentinvocationstore.Store
	offset      atomic.Int64

	// The served progress surface, reading the same stores, so each test can ask
	// what the person's card would be told.
	surface    *PersonaChatSurface
	surfaceCtx context.Context
	room       *personaSurfaceChatFixture
	personaID  string
	posts      int
}

func newAgentRunRecoveryHarness(t *testing.T) *agentRunRecoveryHarness {
	t.Helper()
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	h := &agentRunRecoveryHarness{t: t, ctx: ctx, db: db, tenant: uuid.New()}
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, h.tenant)
	h.pool = commonAgentOpenIntegrationStore(t, db)
	repository, err := agentrunstore.NewAdmissionRepository(h.pool, h.tenant, values.TenantId(agentRunRecoveryTenant))
	if err != nil {
		t.Fatal(err)
	}
	h.admissions, err = agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: personaChatAdmissionAuthorityFake{}, Store: repository, Now: h.now})
	if err != nil {
		t.Fatal(err)
	}
	durable, err := agentrunstate.New(h.pool, func(string) uuid.UUID { return h.tenant })
	if err != nil {
		t.Fatal(err)
	}
	if h.store, err = durable.ForTenant(agentRunRecoveryTenant); err != nil {
		t.Fatal(err)
	}
	if h.state, err = runstate.New(h.store, personaChatAdmissionRecheckerFake{}); err != nil {
		t.Fatal(err)
	}
	if h.invocations, err = agentinvocationstore.NewWithTenantUUID(h.pool, func(string) uuid.UUID { return h.tenant }); err != nil {
		t.Fatal(err)
	}
	surface, surfaceCtx, room, rows, _ := personaSurfaceFixture(t)
	surface.Invocations, surface.Failures = h.invocations, h.invocations
	surface.Executions = func(context.Context, string) (runstate.Store, error) { return h.store, nil }
	h.surface, h.surfaceCtx, h.room, h.personaID = surface, surfaceCtx, room, rows.rows[0].PersonaID
	return h
}

// now is the test's clock: real time, plus whatever the test has let pass.
func (h *agentRunRecoveryHarness) now() time.Time {
	return time.Now().UTC().Add(time.Duration(h.offset.Load())).Truncate(time.Microsecond)
}

func (h *agentRunRecoveryHarness) advance(d time.Duration) { h.offset.Add(int64(d)) }

// request is one invocation of the persona in the room the surface fixture
// serves. postID "post-a" is the one post that room can replay for Try again.
func (h *agentRunRecoveryHarness) request(postID string) agentinvoke.RunRequest {
	h.posts++
	if postID == "" {
		postID = fmt.Sprintf("post-recovery-%d", h.posts)
	}
	inv := personaChatRunRequestFixture()
	inv.TenantID, inv.Grant.TenantID = agentRunRecoveryTenant, agentRunRecoveryTenant
	inv.InvocationID, inv.InvokingPostID, inv.ThreadID = "pinv-recovery-"+postID, postID, postID
	inv.InvokerID, inv.Grant.UserID, inv.ConversationID, inv.PersonaID = "user-a", "user-a", "channel-a", h.personaID
	inv.Actor = agentinvoke.ActorChain{UserID: "user-a", PersonaID: h.personaID, PersonaVersion: inv.PersonaVersion, InstallationID: inv.InstallationID, ConversationID: "channel-a", InvokingPostID: postID, InvocationID: inv.InvocationID}
	return inv
}

// claim records the invocation, as the mention path does first.
func (h *agentRunRecoveryHarness) claim(inv agentinvoke.RunRequest) {
	h.t.Helper()
	if _, created, err := h.invocations.Claim(h.ctx, agentinvoke.Invocation{ID: inv.InvocationID, TenantID: inv.TenantID, ConversationID: inv.ConversationID, ThreadID: inv.ThreadID, PostID: inv.InvokingPostID, InvokerID: inv.InvokerID, PersonaID: inv.PersonaID, PersonaVersion: inv.PersonaVersion, InstallationID: inv.InstallationID, Mode: inv.Mode, Actor: inv.Actor}); err != nil || !created {
		h.t.Fatalf("claim invocation: created=%t err=%v", created, err)
	}
}

// admit claims the invocation and admits its run, as far as the mention path
// gets before a process can die.
func (h *agentRunRecoveryHarness) admit(inv agentinvoke.RunRequest, deadline time.Duration) agentrun.Record {
	h.t.Helper()
	h.claim(inv)
	request := personaChatAdmissionRequestFixture()
	bindPersonaChatAdmissionRequest(&request, inv)
	request.Deadline = h.now().Add(deadline)
	record, _, err := h.admissions.Admit(h.ctx, request)
	if err != nil || record.Decision != agentrun.DecisionAccepted {
		h.t.Fatalf("admission: %+v %v", record, err)
	}
	return record
}

// orphan is an admitted run whose first worker claimed it and then died: its
// lease is already over. A run the first worker never claimed stays READY.
func (h *agentRunRecoveryHarness) orphan(inv agentinvoke.RunRequest, deadline time.Duration, claimed bool) runstate.Run {
	h.t.Helper()
	run, err := h.state.Start(h.ctx, h.admit(inv, deadline))
	if err != nil {
		h.t.Fatal(err)
	}
	if !claimed {
		return run
	}
	if run, err = h.state.Claim(h.ctx, run.ID, "worker-a", h.now().Add(-time.Second), 100*time.Millisecond); err != nil {
		h.t.Fatal(err)
	}
	return run
}

func (h *agentRunRecoveryHarness) dispatcher(workerID string, model *agentRunRecoveryModel, leaseTTL time.Duration) *PersonaBackgroundDispatcher {
	h.t.Helper()
	factory := &personaRunWorkerTenantFactoryFake{config: PersonaRunStarterConfig{
		Authority: personaChatAdmissionAuthorityFake{}, AdmissionRecheck: personaChatAdmissionRecheckerFake{},
		ExecutionStore: h.store, Model: model, Work: agentRunRecoveryWork{}, BackgroundReply: &backgroundReplyRefusal{},
		WorkerID: workerID, LeaseTTL: leaseTTL, Now: h.now,
	}}
	worker, err := NewPersonaRunModelWorker(PersonaRunModelWorkerConfig{Tenants: factory, Fence: &personaRunWorkerFenceFake{}, Leases: personaRunWorkerLeaseFake{id: "recovery-lease"}})
	if err != nil {
		h.t.Fatal(err)
	}
	dispatcher, err := NewPersonaBackgroundDispatcher(h.pool, func(values.TenantId) uuid.UUID { return h.tenant }, worker, &qualityRecoveryNoOutput{})
	if err != nil {
		h.t.Fatal(err)
	}
	return dispatcher
}

// modelStep records that a model step was begun under the run's security lease,
// which is what a worker that died inside the model call leaves behind.
func (h *agentRunRecoveryHarness) modelStep(inv agentinvoke.RunRequest, run runstate.Run) {
	h.t.Helper()
	h.db.Exec(h.t, `INSERT INTO persona_security_lease (tenant_id,lease_id,admission_id,invocation_id,run_id,issuer_id,authority_ref,policy_digest,principal_id,persona_id,persona_version,installation_id,tenant_epoch,principal_epoch,persona_epoch,version_epoch,installation_epoch,run_epoch,issued_at,expires_at)
		VALUES ($1,$2,$3,$4,$5,'issuer','grant-ref',$6,'user-a',$7,'v3','installation-1',1,1,1,1,1,1,now(),now()+interval '1 hour')`,
		h.tenant, "lease-"+run.ID, run.AdmissionID, inv.InvocationID, run.ID, testPersonaDigest, h.personaID)
	h.db.Exec(h.t, `INSERT INTO persona_security_step (tenant_id,lease_id,step_id,state,started_at,finished_at) VALUES ($1,$2,'persona-model-abc','COMPLETED',now(),now())`, h.tenant, "lease-"+run.ID)
}

func (h *agentRunRecoveryHarness) run(run runstate.Run) runstate.Run {
	h.t.Helper()
	current, err := h.store.Get(h.ctx, run.ID)
	if err != nil {
		h.t.Fatal(err)
	}
	return current
}

// card is what the person's pending card is told about the invocation.
func (h *agentRunRecoveryHarness) card(inv agentinvoke.RunRequest) personachat.Invocation {
	h.t.Helper()
	progress, err := h.surface.Progress(h.surfaceCtx, "channel-a")
	if err != nil {
		h.t.Fatal(err)
	}
	for _, item := range progress.Invocations {
		if item.PostID == inv.InvokingPostID {
			return item
		}
	}
	h.t.Fatalf("no card for post %s in %+v", inv.InvokingPostID, progress.Invocations)
	return personachat.Invocation{}
}

// cardFinal is whether the card stops counting: the server reported a final
// state, which the client draws as an answer or as a failure, never as waiting.
func cardFinal(card personachat.Invocation) bool {
	switch strings.ToLower(card.Status) {
	case "completed", "failed", "denied", "needs_repair", "cancelled", "expired":
		return true
	}
	return false
}

func (h *agentRunRecoveryHarness) wantInterrupted(inv agentinvoke.RunRequest) personachat.Invocation {
	h.t.Helper()
	card := h.card(inv)
	if !cardFinal(card) || card.FailureCode != runstate.InterruptedCode || !card.Retryable {
		h.t.Fatalf("card is not a final, retryable interruption: %+v", card)
	}
	return card
}

func wantNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// A claimed invocation whose worker vanished before any model call is run once
// by another worker, and the card ends.
func TestTodo_AGENTRUN_001(t *testing.T) {
	h := newAgentRunRecoveryHarness(t)
	inv := h.request("")
	orphan := h.orphan(inv, 2*time.Minute, true)
	if card := h.card(inv); cardFinal(card) {
		t.Fatalf("before recovery the card is already final: %+v", card)
	}
	model := &agentRunRecoveryModel{}
	err := h.dispatcher("worker-b", model, time.Minute).DispatchTenantRecovering(h.ctx, agentRunRecoveryTenant, 8)
	// The fixture has no output stage, so the run the new worker finishes ends in
	// the output refusal; what matters is that it ran, once, to a final state.
	var failure *PersonaRunFailure
	if !errors.As(err, &failure) || failure.Code != "OUTPUT_REJECTED" {
		t.Fatalf("the new worker did not run the answer to its end: %v", err)
	}
	finished := h.run(orphan)
	if model.calls.Load() != 1 || finished.State != runstate.StateFailed || finished.Fence != orphan.Fence+1 || finished.Lease != nil {
		t.Fatalf("model calls=%d, run=%+v: want one call by the second worker", model.calls.Load(), finished)
	}
	if card := h.card(inv); !cardFinal(card) || strings.EqualFold(card.Status, "running") {
		t.Fatalf("card did not end: %+v", card)
	}
	// Nothing runs it a second time.
	wantNoErr(t, h.dispatcher("worker-c", model, time.Minute).DispatchTenantRecovering(h.ctx, agentRunRecoveryTenant, 8))
	if model.calls.Load() != 1 {
		t.Fatalf("a finished run was run again: %d calls", model.calls.Load())
	}
}

// The worker renews its lease while a call outlasts one lease term, so another
// process does not take over work that is still going on.
func TestTodo_AGENTRUN_001_Renewal(t *testing.T) {
	h := newAgentRunRecoveryHarness(t)
	inv := h.request("")
	run := h.orphan(inv, 2*time.Minute, false)
	const ttl = 300 * time.Millisecond
	model := &agentRunRecoveryModel{delay: 1500 * time.Millisecond, started: make(chan struct{}, 1)}
	done := make(chan error, 1)
	go func() { done <- h.dispatcher("worker-b", model, ttl).Wake(h.ctx, agentRunRecoveryTenant, run.ID) }()
	select {
	case <-model.started:
	case <-time.After(30 * time.Second):
		t.Fatal("the worker never reached the model call")
	}
	first := h.run(run)
	if first.State != runstate.StateRunning || first.Lease == nil {
		t.Fatalf("worker holds no lease while working: %+v", first)
	}
	time.Sleep(3 * ttl)
	second := h.run(run)
	if second.Lease == nil || !second.Lease.Until.After(first.Lease.Until) {
		t.Fatalf("lease was not renewed while the call ran: first=%v second=%+v", first.Lease, second.Lease)
	}
	if !h.now().After(first.Lease.Until) {
		t.Fatal("the test did not outlast the first lease term")
	}
	// The first term is over, yet another process finds a live lease and leaves it.
	other := &agentRunRecoveryModel{}
	if err := h.dispatcher("worker-c", other, ttl).Wake(h.ctx, agentRunRecoveryTenant, run.ID); !errors.Is(err, ErrPersonaRunExecutorBusy) || other.calls.Load() != 0 {
		t.Fatalf("a live, renewed lease was taken over: err=%v calls=%d", err, other.calls.Load())
	}
	<-done
	if model.calls.Load() != 1 {
		t.Fatalf("model calls=%d, want 1", model.calls.Load())
	}
}

// A run whose model call was started is failed with a reason the person can read
// and is not run again.
func TestTodo_AGENTRUN_002(t *testing.T) {
	const woken = "the dispatcher waking it before any sweep"
	for _, evidence := range []string{"a model checkpoint", "a model step under the security lease", woken} {
		t.Run(evidence, func(t *testing.T) {
			h := newAgentRunRecoveryHarness(t)
			inv := h.request("post-a")
			var run runstate.Run
			if evidence == "a model checkpoint" {
				// The first worker held a live lease, was in the middle of its call
				// and recorded it; its lease then ran out with it.
				run = h.orphan(inv, 2*time.Minute, false)
				claimed, err := h.state.Claim(h.ctx, run.ID, "worker-a", h.now(), 200*time.Millisecond)
				wantNoErr(t, err)
				if _, err := h.state.Checkpoint(h.ctx, run.ID, "worker-a", claimed.Fence, claimed.Version, runstate.PhaseModelCall, 1, "step", testPersonaDigest, h.now()); err != nil {
					t.Fatal(err)
				}
				h.advance(time.Second)
			} else {
				run = h.orphan(inv, 2*time.Minute, true)
				h.modelStep(inv, run)
			}
			if card := h.card(inv); cardFinal(card) {
				t.Fatalf("before recovery the card is already final: %+v", card)
			}
			model := &agentRunRecoveryModel{}
			if evidence == woken {
				// The dispatcher's own list reached it before the sweep did.
				wantNoErr(t, h.dispatcher("worker-b", model, time.Minute).Wake(h.ctx, agentRunRecoveryTenant, run.ID))
			} else {
				wantNoErr(t, h.dispatcher("worker-b", model, time.Minute).DispatchTenantRecovering(h.ctx, agentRunRecoveryTenant, 8))
			}
			finished := h.run(run)
			if model.calls.Load() != 0 || finished.State != runstate.StateFailed || finished.TerminalCode != runstate.InterruptedCode || !finished.Retryable || finished.Lease != nil {
				t.Fatalf("a run that may have spent a model call was not failed once: calls=%d run=%+v", model.calls.Load(), finished)
			}
			card := h.wantInterrupted(inv)
			reason := chat.AgentAnswerFailureFor("en-US", "Policy Helper", card.FailureCode)
			if !strings.Contains(reason.Sentence, "interrupted") || !reason.Retryable || reason.NextStep != "Try again." {
				t.Fatalf("the person is not told plainly: %+v", reason)
			}
			// Nothing runs it later, and asking again is the person's own click.
			wantNoErr(t, h.dispatcher("worker-c", model, time.Minute).DispatchTenantRecovering(h.ctx, agentRunRecoveryTenant, 8))
			if model.calls.Load() != 0 {
				t.Fatalf("the failed run was run again: %d calls", model.calls.Load())
			}
			sent := len(h.room.sends)
			if _, err := h.surface.Retry(h.surfaceCtx, inv.InvocationID, "person-pressed-try-again"); err != nil || len(h.room.sends) != sent+1 {
				t.Fatalf("Try again is not accepted for an interrupted answer: %v", err)
			}
		})
	}
}

// Two workers racing for the same orphan take it once.
func TestTodo_AGENTRUN_003(t *testing.T) {
	h := newAgentRunRecoveryHarness(t)
	race := func(t *testing.T, first, second func()) {
		t.Helper()
		start := make(chan struct{})
		var wg sync.WaitGroup
		for _, work := range []func(){first, second} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				work()
			}()
		}
		close(start)
		wg.Wait()
	}
	benign := func(err error) bool {
		var failure *PersonaRunFailure
		return err == nil || errors.Is(err, ErrPersonaRunExecutorBusy) || errors.As(err, &failure)
	}
	t.Run("two workers run it", func(t *testing.T) {
		model := &agentRunRecoveryModel{}
		one, two := h.dispatcher("worker-b", model, time.Minute), h.dispatcher("worker-c", model, time.Minute)
		for round := 0; round < 12; round++ {
			inv := h.request("")
			run := h.orphan(inv, 2*time.Minute, round%2 == 0)
			before := model.calls.Load()
			var errs [2]error
			race(t, func() { errs[0] = one.Wake(h.ctx, agentRunRecoveryTenant, run.ID) }, func() { errs[1] = two.Wake(h.ctx, agentRunRecoveryTenant, run.ID) })
			for _, err := range errs {
				if !benign(err) {
					t.Fatalf("round %d: %v", round, err)
				}
			}
			finished := h.run(run)
			wantFence := run.Fence + 1
			if model.calls.Load()-before != 1 || finished.State != runstate.StateFailed || finished.Fence != wantFence {
				t.Fatalf("round %d: model calls=%d run=%+v: want taken over once (fence %d)", round, model.calls.Load()-before, finished, wantFence)
			}
		}
	})
	t.Run("two processes finish it", func(t *testing.T) {
		model := &agentRunRecoveryModel{}
		one, two := h.dispatcher("worker-b", model, time.Minute), h.dispatcher("worker-c", model, time.Minute)
		for round := 0; round < 12; round++ {
			inv := h.request("")
			run := h.orphan(inv, 2*time.Minute, true)
			h.modelStep(inv, run)
			var errs [2]error
			race(t, func() { errs[0] = one.RecoverInterrupted(h.ctx, agentRunRecoveryTenant) }, func() { errs[1] = two.RecoverInterrupted(h.ctx, agentRunRecoveryTenant) })
			for _, err := range errs {
				if err != nil {
					t.Fatalf("round %d: losing the race is not an error: %v", round, err)
				}
			}
			finished := h.run(run)
			if finished.State != runstate.StateFailed || finished.TerminalCode != runstate.InterruptedCode || finished.Fence != run.Fence+1 || model.calls.Load() != 0 {
				t.Fatalf("round %d: finished more than once or ran it: %+v calls=%d", round, finished, model.calls.Load())
			}
			// The sweeps are throttled per process; let the next round be due again.
			h.advance(time.Minute)
		}
	})
}

// Every invocation reaches a final state in bounded time, with no worker
// involved, and the card ends with it.
func TestTodo_AGENTRUN_004(t *testing.T) {
	t.Run("a claim that never reached admission", func(t *testing.T) {
		h := newAgentRunRecoveryHarness(t)
		inv := h.request("post-a")
		h.claim(inv)
		sweeper := h.dispatcher("worker-b", &agentRunRecoveryModel{}, time.Minute)
		wantNoErr(t, sweeper.RecoverInterrupted(h.ctx, agentRunRecoveryTenant))
		if card := h.card(inv); cardFinal(card) {
			t.Fatalf("a fresh claim, still being admitted, was finished: %+v", card)
		}
		h.advance(4 * time.Minute)
		wantNoErr(t, sweeper.RecoverInterrupted(h.ctx, agentRunRecoveryTenant))
		h.wantInterrupted(inv)
		// Finishing it again, here or in another process, changes nothing.
		h.advance(time.Minute)
		wantNoErr(t, h.dispatcher("worker-c", &agentRunRecoveryModel{}, time.Minute).RecoverInterrupted(h.ctx, agentRunRecoveryTenant))
		failures, err := h.invocations.ListPostFailures(h.ctx, agentRunRecoveryTenant, "user-a", "channel-a")
		if err != nil || len(failures) != 1 || failures[0].Code != runstate.InterruptedCode || !failures[0].Retryable {
			t.Fatalf("failures=%+v err=%v", failures, err)
		}
		if _, err := h.surface.Retry(h.surfaceCtx, inv.InvocationID, "person-pressed-try-again"); err != nil {
			t.Fatalf("Try again after a claim that never ran: %v", err)
		}
	})
	t.Run("an admission whose run was never created", func(t *testing.T) {
		h := newAgentRunRecoveryHarness(t)
		inv := h.request("")
		h.admit(inv, 2*time.Minute)
		sweeper := h.dispatcher("worker-b", &agentRunRecoveryModel{}, time.Minute)
		wantNoErr(t, sweeper.RecoverInterrupted(h.ctx, agentRunRecoveryTenant))
		if card := h.card(inv); cardFinal(card) {
			t.Fatalf("an admission with time left was finished: %+v", card)
		}
		h.advance(3 * time.Minute)
		wantNoErr(t, sweeper.RecoverInterrupted(h.ctx, agentRunRecoveryTenant))
		h.wantInterrupted(inv)
	})
	t.Run("the restart outlasted the deadline", func(t *testing.T) {
		// What the review server showed: runs admitted, claimed or not, whose
		// deadline passed while the server was down. The dispatcher no longer
		// lists them, so nothing but the sweep can end them.
		h := newAgentRunRecoveryHarness(t)
		ready, running := h.request(""), h.request("")
		readyRun, runningRun := h.orphan(ready, 2*time.Minute, false), h.orphan(running, 2*time.Minute, true)
		h.advance(3 * time.Minute)
		model := &agentRunRecoveryModel{}
		wantNoErr(t, h.dispatcher("worker-b", model, time.Minute).DispatchTenantRecovering(h.ctx, agentRunRecoveryTenant, 8))
		for _, pair := range []struct {
			inv agentinvoke.RunRequest
			run runstate.Run
		}{{ready, readyRun}, {running, runningRun}} {
			finished := h.run(pair.run)
			if finished.State != runstate.StateFailed || finished.TerminalCode != runstate.InterruptedCode {
				t.Fatalf("a run past its deadline was left: %+v", finished)
			}
			h.wantInterrupted(pair.inv)
		}
		if model.calls.Load() != 0 {
			t.Fatalf("a run past its deadline was run: %d calls", model.calls.Load())
		}
	})
	t.Run("a failure was recorded but its run was left running", func(t *testing.T) {
		// What the review server held for two of its questions: the post's failure
		// row said INVOCATION_FAILED while the run stayed RUNNING, and the card
		// follows the run, so it counted on.
		h := newAgentRunRecoveryHarness(t)
		inv := h.request("")
		run := h.orphan(inv, 2*time.Minute, true)
		failure := agentinvocationstore.PostFailure{TenantID: agentRunRecoveryTenant, InvokerID: "user-a", ConversationID: "channel-a", ThreadID: inv.ThreadID, PostID: inv.InvokingPostID, Code: "INVOCATION_FAILED"}
		wantNoErr(t, h.invocations.RecordPostFailure(h.ctx, failure))
		if card := h.card(inv); cardFinal(card) {
			t.Fatalf("the card follows the run, which is still running: %+v", card)
		}
		h.advance(3 * time.Minute)
		wantNoErr(t, h.dispatcher("worker-b", &agentRunRecoveryModel{}, time.Minute).RecoverInterrupted(h.ctx, agentRunRecoveryTenant))
		if finished := h.run(run); finished.State != runstate.StateFailed || finished.TerminalCode != runstate.InterruptedCode {
			t.Fatalf("the orphaned run was left: %+v", finished)
		}
		h.wantInterrupted(inv)
	})
	t.Run("the hard ceiling with no worker at all", func(t *testing.T) {
		h := newAgentRunRecoveryHarness(t)
		inv := h.request("")
		// An hour of deadline, so only the ceiling can end it; nobody claims it.
		run := h.orphan(inv, time.Hour, false)
		sweeper := h.dispatcher("worker-b", &agentRunRecoveryModel{}, time.Minute)
		sweeper.recovery.policy = personaRecoveryPolicy{Ceiling: 10 * time.Minute}
		h.advance(5 * time.Minute)
		wantNoErr(t, sweeper.RecoverInterrupted(h.ctx, agentRunRecoveryTenant))
		if card := h.card(inv); cardFinal(card) || h.run(run).State != runstate.StateReady {
			t.Fatalf("a run inside its ceiling was finished: %+v", card)
		}
		h.advance(6 * time.Minute)
		wantNoErr(t, sweeper.RecoverInterrupted(h.ctx, agentRunRecoveryTenant))
		if finished := h.run(run); finished.State != runstate.StateFailed || finished.TerminalCode != runstate.InterruptedCode {
			t.Fatalf("the run outlived its ceiling: %+v", finished)
		}
		h.wantInterrupted(inv)
	})
	t.Run("a run a worker still holds is left alone", func(t *testing.T) {
		h := newAgentRunRecoveryHarness(t)
		inv := h.request("")
		run := h.orphan(inv, 2*time.Minute, false)
		if _, err := h.state.Claim(h.ctx, run.ID, "worker-a", h.now(), time.Minute); err != nil {
			t.Fatal(err)
		}
		h.modelStep(inv, run)
		wantNoErr(t, h.dispatcher("worker-b", &agentRunRecoveryModel{}, time.Minute).RecoverInterrupted(h.ctx, agentRunRecoveryTenant))
		if live := h.run(run); live.State != runstate.StateRunning || live.Lease == nil {
			t.Fatalf("a run under a live lease was taken: %+v", live)
		}
		if card := h.card(inv); cardFinal(card) {
			t.Fatalf("the card ended while the worker is working: %+v", card)
		}
	})
}
