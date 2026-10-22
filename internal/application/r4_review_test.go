package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

// Lane R4 (agent run path review, 2026-10-02).

// r4CtxStore refuses every call made with a context that has ended, as the
// PostgreSQL store does.
type r4CtxStore struct{ runstate.Store }

func (s r4CtxStore) Get(ctx context.Context, id string) (runstate.Run, error) {
	if err := ctx.Err(); err != nil {
		return runstate.Run{}, err
	}
	return s.Store.Get(ctx, id)
}

func (s r4CtxStore) Save(ctx context.Context, run runstate.Run, expected uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.Store.Save(ctx, run, expected)
}

// A run that fails because its own context ended (the deadline, or a cancel)
// must still reach a final state, or the asker's card spins until the lease
// runs out and recovery finds it.
func TestR4_RunFailsToAFinalStateWhenItsContextHasEnded(t *testing.T) {
	inner := runstate.NewMemoryStore()
	executor, run := qualityRetryExecutor(t, &qualityRetryModel{}, r4CtxStore{Store: inner})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := executor.fail(ctx, run, "MODEL_TIMEOUT", context.DeadlineExceeded)
	var failure *PersonaRunFailure
	if !errors.As(err, &failure) || failure.Code != "MODEL_TIMEOUT" {
		t.Fatalf("fail returned %v, want the typed MODEL_TIMEOUT failure", err)
	}
	stored, getErr := inner.Get(context.Background(), run.ID)
	if getErr != nil || stored.State != runstate.StateFailed || stored.TerminalCode != "MODEL_TIMEOUT" || !stored.Retryable {
		t.Fatalf("stored run = %+v (%v), want FAILED with MODEL_TIMEOUT, retryable", stored, getErr)
	}
}

type r4FailingWork struct{ err error }

func (w r4FailingWork) BuildPersonaRunModelWork(context.Context, agentrun.Record, runstate.Run) (PersonaRunModelWork, error) {
	return PersonaRunModelWork{}, w.err
}

func r4ReadyRun(t *testing.T, work PersonaRunModelWorkSource) (*personaAdmittedRunExecutor, runstate.Run, *runstate.MemoryStore) {
	t.Helper()
	store := runstate.NewMemoryStore()
	state, err := runstate.New(store, personaChatAdmissionRecheckerFake{})
	if err != nil {
		t.Fatal(err)
	}
	run := runstate.Run{ID: "r4-run", AdmissionID: "r4-run", TenantID: "tenant", Version: 1, State: runstate.StateReady, Deadline: time.Now().Add(time.Minute)}
	if err := store.Create(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	return &personaAdmittedRunExecutor{store: store, state: state, work: work, workerID: "worker", now: time.Now, leaseTTL: time.Minute}, run, store
}

// A model route policy that is stale or invalid is a setup problem. The asker is
// told the agent is unavailable and who to ask, not that the agent "cannot answer
// this request here" and should be asked about a document it can read.
func TestR4_StaleModelRouteEndsAsRouteUnavailableNotAsPermission(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		code  string
		class string
	}{
		{"route policy stale", errors.Join(errPersonaRunModelWork, errPersonaModelRouteUnavailable), "MODEL_ROUTE_UNAVAILABLE", "unavailable"},
		{"thread unreadable", errPersonaRunModelWork, "CONTEXT_UNAVAILABLE", "permission"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			executor, run, store := r4ReadyRun(t, r4FailingWork{err: tc.cause})
			err := executor.execute(context.Background(), agentrun.Record{ID: run.ID}, run)
			var failure *PersonaRunFailure
			if !errors.As(err, &failure) || failure.Code != tc.code {
				t.Fatalf("run ended with %v, want code %s", err, tc.code)
			}
			stored, _ := store.Get(context.Background(), run.ID)
			if stored.State != runstate.StateFailed || stored.TerminalCode != tc.code {
				t.Fatalf("stored run = %+v, want FAILED %s", stored, tc.code)
			}
			if got := chatcore.AgentAnswerFailureFor("en-US", "Policy Helper", stored.TerminalCode); got.Class != tc.class {
				t.Fatalf("card class for %s = %q, want %q (%q)", stored.TerminalCode, got.Class, tc.class, got.Sentence)
			}
		})
	}
}

// The security fence reports a failed build of model work; the cause must stay
// readable through it, or a stale route is reported as a permission failure.
func TestR4_FencedModelWorkKeepsTheCauseOfAFailedBuild(t *testing.T) {
	steps := &personaRunStepIdentityState{}
	source := fencedPersonaRunModelWorkSource{inner: r4FailingWork{err: errPersonaModelRouteUnavailable}, fence: &personaRunWorkerFenceFake{}, leases: personaRunWorkerLeaseFake{id: "lease"}, steps: steps}
	run := runstate.Run{ID: "r4-run", AdmissionID: "r4-run", TenantID: "tenant"}
	admission := agentrun.Record{ID: "r4-run"}
	admission.Request.Source.TenantID = "tenant"
	_, err := source.BuildPersonaRunModelWork(context.Background(), admission, run)
	if !errors.Is(err, errPersonaModelRouteUnavailable) {
		t.Fatalf("fenced build error = %v, want the route cause preserved", err)
	}
}

// r4LeaseModel behaves as the egress dispatcher does: a credential lease is
// consumed by the first call that carries it.
type r4LeaseModel struct {
	used  map[string]bool
	calls []string
}

func (m *r4LeaseModel) Execute(_ context.Context, request AgentModelExecutorRequest) (AgentModelExecutorResult, error) {
	m.calls = append(m.calls, request.Lease.ID)
	if m.used[request.Lease.ID] {
		return AgentModelExecutorResult{}, errors.New("credential lease already used")
	}
	m.used[request.Lease.ID] = true
	if len(m.calls) == 1 {
		return AgentModelExecutorResult{Result: agentmodel.ModelResult{Failure: &agentmodel.ModelFailure{Code: agentmodel.FailureUnavailable, Retryable: true}}}, nil
	}
	return AgentModelExecutorResult{Result: agentmodel.ModelResult{Text: "Answer", Finish: agentmodel.FinishComplete}}, nil
}

type r4LeaseWork struct{ issued int }

func (w *r4LeaseWork) BuildPersonaRunModelWork(context.Context, agentrun.Record, runstate.Run) (PersonaRunModelWork, error) {
	return PersonaRunModelWork{}, nil
}

func (w *r4LeaseWork) RefreshPersonaRunModelLease(_ context.Context, request *AgentModelExecutorRequest) error {
	w.issued++
	request.Lease = lease.CredentialLease{ID: "lease-" + string(rune('1'+w.issued))}
	return nil
}

// The transient retry makes a second provider call. The first call consumed its
// single-use credential lease, so the retry carries a fresh one; without it the
// retry is refused and the person sees a failure for a call that would have worked.
func TestR4_TransientRetryCarriesItsOwnCredentialLease(t *testing.T) {
	model := &r4LeaseModel{used: map[string]bool{}}
	executor, run := qualityRetryExecutor(t, &qualityRetryModel{}, runstate.NewMemoryStore())
	executor.model = model
	work := &r4LeaseWork{}
	executor.work = work
	request := AgentModelExecutorRequest{StepID: "answer", Lease: lease.CredentialLease{ID: "lease-1"}}
	result, _, err := executor.executeQualityModel(context.Background(), run, request)
	if err != nil || result.Result.Text != "Answer" {
		t.Fatalf("retry result = %+v, err = %v; calls with leases %v", result, err, model.calls)
	}
	if len(model.calls) != 2 || model.calls[0] == model.calls[1] {
		t.Fatalf("leases per call = %v, want two different leases", model.calls)
	}
}

type r4FailedExecutor struct{ err error }

func (e r4FailedExecutor) Start(context.Context, agentrun.Record) (runstate.Run, error) {
	return runstate.Run{}, e.err
}

// A run's own failure (its code and whether to try again) must survive the
// admission adapter: the outcome reaction and the stored failure are chosen from it.
func TestR4_AdmissionAdapterKeepsTheRunFailure(t *testing.T) {
	failure := &PersonaRunFailure{Code: "CONTEXT_UNAVAILABLE", kind: ErrPersonaRunExecutorUnavailable}
	builder := &personaChatAdmissionBuilderFake{request: personaChatAdmissionRequestFixture()}
	adapter := personaChatAdmissionAdapterFixture(t, builder, personaChatAdmissionAuthorityFake{}, r4FailedExecutor{err: failure})
	err := adapter.Start(context.Background(), personaChatRunRequestFixture())
	var got *PersonaRunFailure
	if !errors.As(err, &got) || got.Code != "CONTEXT_UNAVAILABLE" {
		t.Fatalf("adapter error = %v, want the run's own failure inside it", err)
	}
	if emoji := agentUX075OutcomeEmoji(err); emoji != chatcore.AgentOutcomeCouldNotAnswer() {
		t.Fatalf("outcome reaction = %q, want the could-not-answer reaction for a permission-class failure", emoji)
	}
}

// A lapsed model route ends as MODEL_ROUTE_UNAVAILABLE whether the lapse is found
// when the work is built or when the router picks a model, and the run writes one
// log line that names the cause.
func TestR4_LapsedRouteIsNamedInOneLogLine(t *testing.T) {
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	defer slog.SetDefault(previous)

	cause := fmt.Errorf("%w: current tenant model policy unavailable (policy expired): %w", errPersonaRunModelWork, errPersonaModelRouteUnavailable)
	executor, run, _ := r4ReadyRun(t, r4FailingWork{err: cause})
	if err := executor.execute(context.Background(), agentrun.Record{ID: run.ID}, run); err == nil {
		t.Fatal("run with a lapsed route succeeded")
	}
	count := strings.Count(logged.String(), "hcmnext.persona_model_route_unavailable")
	if count != 1 || !strings.Contains(logged.String(), "policy expired") {
		t.Fatalf("log = %q, want one route line naming the cause", logged.String())
	}
	if got := personaQualityModelFailureCode(AgentModelExecutorResult{}, fmt.Errorf("%w: no model", agentmodel.ErrNoEligibleModel)); got != "MODEL_ROUTE_UNAVAILABLE" {
		t.Fatalf("router refusal code = %s, want MODEL_ROUTE_UNAVAILABLE", got)
	}
}
