package application

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

type qualityRetryModel struct {
	failures, calls int
	kind            string
	steps           []string
	withError       bool
	before          func(int)
}

func (m *qualityRetryModel) Execute(_ context.Context, request AgentModelExecutorRequest) (AgentModelExecutorResult, error) {
	m.calls++
	if m.before != nil {
		m.before(m.calls)
	}
	m.steps = append(m.steps, request.StepID)
	if m.calls <= m.failures {
		switch m.kind {
		case "reset":
			return AgentModelExecutorResult{}, syscall.ECONNRESET
		case "lease":
			return AgentModelExecutorResult{}, runstate.ErrLease
		case "timeout":
			return m.failure(agentmodel.FailureTimeout)
		case "limit":
			return m.failure(agentmodel.FailureLimit)
		default:
			return m.failure(agentmodel.FailureUnavailable)
		}
	}
	return AgentModelExecutorResult{Result: agentmodel.ModelResult{Text: "Answer", Finish: agentmodel.FinishComplete}}, nil
}

type qualityLeaseWork struct {
	request AgentModelExecutorRequest
	calls   int
}

func (w *qualityLeaseWork) BuildPersonaRunModelWork(context.Context, agentrun.Record, runstate.Run) (PersonaRunModelWork, error) {
	w.calls++
	return PersonaRunModelWork{Request: w.request}, nil
}

func TestAgentUXQuality_ExpiredLease_Fault(t *testing.T) {
	for _, failures := range []int{1, 2} {
		store := runstate.NewMemoryStore()
		model := &qualityRetryModel{failures: failures, kind: "lease"}
		executor, run := qualityRetryExecutor(t, model, store)
		run.Version++
		run.AgentDigest = "quality-agent-digest"
		run.Lease.Until = time.Now().Add(-time.Second)
		if err := store.Save(context.Background(), run, run.Version-1); err != nil {
			t.Fatal(err)
		}
		request := AgentModelExecutorRequest{Task: TrustedModelTask{TaskID: run.ID, TenantID: run.TenantID, AgentID: run.AgentDigest}, StepID: "answer"}
		work := &qualityLeaseWork{request: request}
		executor.work = work
		model.before = func(call int) {
			if call == 2 && work.calls != 1 {
				t.Fatal("lease retry ran before trusted work was rebuilt")
			}
		}
		result, updated, err := executor.executeQualityModel(context.Background(), run, request, agentrun.Record{ID: run.AdmissionID})
		if updated.Fence <= run.Fence || work.calls != 1 || model.calls != 2 {
			t.Fatalf("expired lease not recovered: run=%+v work=%d model=%d", updated, work.calls, model.calls)
		}
		if failures == 1 && (err != nil || result.Result.Text != "Answer") || failures == 2 && personaQualityModelFailureCode(result, err) != "ANSWER_INTERRUPTED" {
			t.Fatalf("lease outcome: %+v %v", result, err)
		}
	}
}

func (m *qualityRetryModel) failure(code agentmodel.FailureCode) (AgentModelExecutorResult, error) {
	result := AgentModelExecutorResult{Result: agentmodel.ModelResult{Failure: &agentmodel.ModelFailure{Code: code, Retryable: true}}}
	if m.withError {
		return result, errors.New("typed generation failed")
	}
	return result, nil
}

func TestAgentUXQuality_FencedTransient_Fault(t *testing.T) {
	for _, kind := range []string{"provider", "timeout", "reset"} {
		for _, failures := range []int{1, 2} {
			model := &qualityRetryModel{failures: failures, kind: kind, withError: true}
			executor, run := qualityRetryExecutor(t, model, runstate.NewMemoryStore())
			fence := &personaRunWorkerFenceFake{bound: map[agentsecurity.PersonaRunID]agentsecurity.KillSwitchLeaseID{agentsecurity.PersonaRunID(run.ID): "quality-security-lease"}}
			steps := &personaRunStepIdentityState{}
			steps.set(run)
			executor.model = fencedPersonaRunModelExecutor{inner: model, fence: fence, steps: steps}
			request := AgentModelExecutorRequest{Task: TrustedModelTask{TaskID: run.ID}, StepID: "answer"}
			result, _, err := executor.executeQualityModel(context.Background(), run, request)
			if model.calls != 2 || len(fence.steps) != 2 || fence.stepIDs[0] == fence.stepIDs[1] {
				t.Fatalf("%s/%d bypassed fence or lost retry: calls=%d steps=%v", kind, failures, model.calls, fence.stepIDs)
			}
			if failures == 1 && (err != nil || result.Result.Text != "Answer") {
				t.Fatalf("%s transient surfaced: %+v %v", kind, result, err)
			}
			if failures == 2 && (err == nil || kind == "timeout" && personaQualityModelFailureCode(result, err) != "MODEL_TIMEOUT") {
				t.Fatalf("%s repeated failure lost cause: %+v %v", kind, result, err)
			}
		}
	}
}

func TestAgentUXQuality_FencedRetry_Security(t *testing.T) {
	model := &qualityRetryModel{}
	executor, run := qualityRetryExecutor(t, model, runstate.NewMemoryStore())
	steps := &personaRunStepIdentityState{}
	steps.set(run)
	fence := &personaRunWorkerFenceFake{deny: errors.New("revoked security lease")}
	executor.model = fencedPersonaRunModelExecutor{inner: model, fence: fence, steps: steps}
	_, _, err := executor.executeQualityModel(context.Background(), run, AgentModelExecutorRequest{Task: TrustedModelTask{TaskID: run.ID}, StepID: "answer"})
	if err == nil || model.calls != 0 || len(fence.steps) != 1 {
		t.Fatalf("security refusal retried or bypassed: err=%v model=%d fence=%d", err, model.calls, len(fence.steps))
	}
}

func qualityRetryExecutor(t *testing.T, model *qualityRetryModel, store runstate.Store) (*personaAdmittedRunExecutor, runstate.Run) {
	t.Helper()
	now := time.Now().UTC()
	run := runstate.Run{ID: "quality-run", AdmissionID: "quality-run", TenantID: "tenant", Version: 1, Fence: 1, State: runstate.StateRunning, Deadline: now.Add(time.Minute), Lease: &runstate.Lease{Owner: "worker", Fence: 1, Until: now.Add(time.Minute)}}
	if err := store.Create(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	state, err := runstate.New(store, personaChatAdmissionRecheckerFake{})
	if err != nil {
		t.Fatal(err)
	}
	return &personaAdmittedRunExecutor{store: store, state: state, model: model, workerID: "worker", now: time.Now, leaseTTL: time.Minute}, run
}

func TestAgentUXQuality_Transient_Fault(t *testing.T) {
	for _, kind := range []string{"provider", "timeout", "reset", "lease"} {
		for _, failures := range []int{1, 2} {
			t.Run(kind+string(rune('0'+failures)), func(t *testing.T) {
				model := &qualityRetryModel{failures: failures, kind: kind}
				executor, run := qualityRetryExecutor(t, model, runstate.NewMemoryStore())
				result, updated, err := executor.executeQualityModel(context.Background(), run, AgentModelExecutorRequest{StepID: "answer"})
				if model.calls != 2 || len(updated.Checkpoints) != 1 || model.steps[0] == model.steps[1] {
					t.Fatalf("retry not recorded/bounded: calls=%d run=%+v steps=%v", model.calls, updated, model.steps)
				}
				if failures == 1 && (err != nil || result.Result.Text != "Answer") {
					t.Fatalf("transient visible instead of succeeding: %+v %v", result, err)
				}
				if failures == 2 {
					want := "MODEL_UNAVAILABLE"
					if kind == "timeout" {
						want = "MODEL_TIMEOUT"
					}
					if kind == "lease" {
						want = "ANSWER_INTERRUPTED"
					}
					if got := personaQualityModelFailureCode(result, err); got != want {
						t.Fatalf("failure code=%s want %s", got, want)
					}
				}
			})
		}
	}
}

type qualityConflictStore struct {
	*runstate.MemoryStore
	failures, calls int
	cause           error
}

func (s *qualityConflictStore) Save(ctx context.Context, run runstate.Run, expected uint64) error {
	s.calls++
	if s.calls <= s.failures {
		if s.cause != nil {
			return s.cause
		}
		return runstate.ErrConflict
	}
	return s.MemoryStore.Save(ctx, run, expected)
}

type qualitySerializationError struct{}

func (qualitySerializationError) Error() string    { return "serialization aborted" }
func (qualitySerializationError) SQLState() string { return "40001" }

func TestAgentUXQuality_Serialization_Fault(t *testing.T) {
	for _, cause := range []error{runstate.ErrConflict, qualitySerializationError{}} {
		for _, failures := range []int{1, 2} {
			store := &qualityConflictStore{MemoryStore: runstate.NewMemoryStore(), failures: failures, cause: cause}
			executor, run := qualityRetryExecutor(t, &qualityRetryModel{}, store)
			updated, err := executor.qualityCheckpoint(context.Background(), run.ID, "worker", run.Fence, run.Version, runstate.PhaseModelCall, 1, "answer", personaRunBytesDigest([]byte("answer")), time.Now())
			if failures == 1 && (err != nil || len(updated.Checkpoints) != 2 || updated.Checkpoints[1].Ref != "state-save-retry") {
				t.Fatalf("serialization retry: %+v %v", updated, err)
			}
			if failures == 2 && (!errors.Is(err, cause) || store.calls != 2) {
				t.Fatalf("repeated serialization: calls=%d err=%v", store.calls, err)
			}
		}
	}
}

func TestAgentUXQuality_EffectSerialization_Fault(t *testing.T) {
	for _, failures := range []int{1, 2} {
		store := &qualityConflictStore{MemoryStore: runstate.NewMemoryStore(), failures: failures, cause: qualitySerializationError{}}
		executor, run := qualityRetryExecutor(t, &qualityRetryModel{}, store)
		updated, err := executor.qualityStateChange(context.Background(), run, func() (runstate.Run, error) {
			return executor.state.BeginEffect(context.Background(), run.ID, "worker", "search", "search-once", personaRunBytesDigest([]byte("search")), run.Fence, run.Version, time.Now())
		})
		if failures == 1 && (err != nil || len(updated.Effects) != 1 || len(updated.Checkpoints) != 2 || updated.Checkpoints[1].Ref != "state-save-retry") {
			t.Fatalf("effect retry duplicated or missing evidence: %+v %v", updated, err)
		}
		if failures == 2 && (err == nil || store.calls != 2) {
			t.Fatalf("effect retry not bounded: calls=%d err=%v", store.calls, err)
		}
	}
}

func TestAgentUXQuality_Deadline_Fault(t *testing.T) {
	model := &qualityRetryModel{failures: 1, kind: "timeout"}
	executor, run := qualityRetryExecutor(t, model, runstate.NewMemoryStore())
	run.Deadline = time.Now().Add(10 * time.Millisecond)
	_, _, err := executor.executeQualityModel(context.Background(), run, AgentModelExecutorRequest{StepID: "answer"})
	if !errors.Is(err, context.DeadlineExceeded) || model.calls != 1 {
		t.Fatalf("deadline retry: calls=%d err=%v", model.calls, err)
	}
	model.calls = 0
	run.Deadline = time.Now().Add(-time.Second)
	_, _, err = executor.executeQualityModel(context.Background(), run, AgentModelExecutorRequest{StepID: "answer"})
	if !errors.Is(err, context.DeadlineExceeded) || model.calls != 0 {
		t.Fatalf("expired deadline reached provider: calls=%d err=%v", model.calls, err)
	}
}

func TestAgentUXQuality_RetryLimits_Security(t *testing.T) {
	model := &qualityRetryModel{failures: 1, kind: "limit"}
	executor, run := qualityRetryExecutor(t, model, runstate.NewMemoryStore())
	result, _, err := executor.executeQualityModel(context.Background(), run, AgentModelExecutorRequest{StepID: "answer"})
	if err != nil || model.calls != 1 || personaQualityModelFailureCode(result, err) != "MODEL_LIMIT" {
		t.Fatalf("limit attempted a provider retry: calls=%d result=%+v err=%v", model.calls, result, err)
	}
}
