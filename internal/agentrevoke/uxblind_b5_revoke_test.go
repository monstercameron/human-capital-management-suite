package agentrevoke

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

type testEpochs struct {
	mu     sync.Mutex
	epochs map[string]uint64
}

func newTestEpochs() *testEpochs { return &testEpochs{epochs: make(map[string]uint64)} }

func (s *testEpochs) Current(_ context.Context, scope Scope) (uint64, error) {
	if !scope.valid() {
		return 0, fmt.Errorf("invalid scope")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.epochs[scope.key()] == 0 {
		s.epochs[scope.key()] = 1
	}
	return s.epochs[scope.key()], nil
}

func (s *testEpochs) Bump(_ context.Context, scope Scope, reason string) (uint64, error) {
	if reason == "" {
		return 0, fmt.Errorf("missing reason")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.epochs[scope.key()] == 0 {
		s.epochs[scope.key()] = 1
	}
	s.epochs[scope.key()]++
	return s.epochs[scope.key()], nil
}

type testGrants struct {
	mu     sync.Mutex
	grants map[string]Grant
}

func newTestGrants() *testGrants { return &testGrants{grants: make(map[string]Grant)} }

func (s *testGrants) add(g Grant) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grants[g.ID] = cloneGrant(g)
}

func (s *testGrants) Get(_ context.Context, id string) (Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.grants[id]
	if !ok {
		return Grant{}, ErrNotFound
	}
	return cloneGrant(g), nil
}

func (s *testGrants) List(_ context.Context) ([]Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Grant, 0, len(s.grants))
	for _, grant := range s.grants {
		out = append(out, cloneGrant(grant))
	}
	return out, nil
}

func (s *testGrants) Revoke(_ context.Context, id, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.grants[id]
	if !ok {
		return ErrNotFound
	}
	g.Revoked = true
	s.grants[id] = cloneGrant(g)
	if reason == "" {
		return fmt.Errorf("missing reason")
	}
	return nil
}

type testTasks struct {
	mu    sync.Mutex
	tasks map[string]agentrun.AgentTask
}

func newTestTasks() *testTasks { return &testTasks{tasks: make(map[string]agentrun.AgentTask)} }

func (s *testTasks) add(task agentrun.AgentTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.ID] = cloneTask(task)
}

func (s *testTasks) List(_ context.Context) ([]agentrun.AgentTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]agentrun.AgentTask, 0, len(s.tasks))
	for _, task := range s.tasks {
		out = append(out, cloneTask(task))
	}
	return out, nil
}

func (s *testTasks) Save(_ context.Context, task agentrun.AgentTask, expected uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.tasks[task.ID]
	if !ok || current.Version != expected {
		return agentrun.ErrConflict
	}
	s.tasks[task.ID] = cloneTask(task)
	return nil
}

func (s *testTasks) get(id string) agentrun.AgentTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneTask(s.tasks[id])
}

func cloneGrant(in Grant) Grant {
	in.Epochs = cloneEpochs(in.Epochs)
	return in
}

func cloneTask(in agentrun.AgentTask) agentrun.AgentTask {
	in.Plan.Steps = append([]agentrun.PlanStep(nil), in.Plan.Steps...)
	in.Constraints = append([]string(nil), in.Constraints...)
	return in
}

func fixture(t *testing.T) (*Coordinator, *testEpochs, *testGrants, *testTasks) {
	t.Helper()
	epochs, grants, tasks := newTestEpochs(), newTestGrants(), newTestTasks()
	grant := Grant{ID: "grant-1", TaskID: "task-1", TenantID: "tenant-a", UserID: "user-1", SessionFamilyID: "family-1", AgentVersion: "agent-v1", InstallationID: "install-1"}
	grant.Epochs = map[string]uint64{}
	for _, scope := range grantScopes(grant) {
		grant.Epochs[scope.key()] = 1
	}
	grants.add(grant)
	plan, err := agentrun.NewPlan([]agentrun.PlanStep{{
		ID: "draft", Type: agentrun.StepDraft, SkillID: "skill", SkillVersion: 1,
		ExpectedOutput: "draft", Tier: agentrun.TierPrivateDraft,
	}})
	if err != nil {
		t.Fatal(err)
	}
	plan.Steps[0].Approved = true
	plan.Steps[0].ApprovalDigest = "sha256:approved"
	plan.Steps[0].State = agentrun.StepAwaitingApproval
	tasks.add(agentrun.AgentTask{ID: "task-1", TenantID: "tenant-a", UserID: "user-1", Plan: plan,
		State: agentrun.StateWaiting, Version: 3, Wake: &agentrun.WakeCondition{Kind: agentrun.WakeTimer, Key: "wake-1"},
		WorkerLease: "worker", ModelSession: "model"})
	coordinator, err := New(Config{Epochs: epochs, Grants: grants, Tasks: tasks, Clock: func() time.Time { return time.Unix(100, 0).UTC() }})
	if err != nil {
		t.Fatal(err)
	}
	return coordinator, epochs, grants, tasks
}

func TestTodo_AGENT2_020(t *testing.T) {
	coordinator, epochs, grants, tasks := fixture(t)
	result, err := coordinator.Apply(context.Background(), Event{TenantID: "tenant-a", UserID: "user-1", Cause: CauseRoleChanged, OccurredAt: time.Unix(101, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if result.RevokedGrants != 1 || result.PausedTasks != 1 || result.VoidedApprovals != 1 {
		t.Fatalf("result = %+v, want one grant, task and approval affected", result)
	}
	if result.Reason != ReasonRoleChanged || len(result.Epochs) != 1 || result.Epochs[0].Before != 1 || result.Epochs[0].After != 2 {
		t.Fatalf("epoch result = %+v", result)
	}
	if result.Digest == "" || result.Digest[:7] != "sha256:" {
		t.Fatalf("result digest = %q", result.Digest)
	}
	if got, _ := epochs.Current(context.Background(), Scope{Kind: ScopeUser, TenantID: "tenant-a", Subject: "user-1"}); got != 2 {
		t.Fatalf("user epoch = %d, want 2", got)
	}
	grant, _ := grants.Get(context.Background(), "grant-1")
	if !grant.Revoked {
		t.Fatal("grant was not revoked")
	}
	task := tasks.get("task-1")
	if task.State != agentrun.StatePaused || task.PausedState != agentrun.StateWaiting || task.Wake != nil || task.WorkerLease != "" || task.ModelSession != "" {
		t.Fatalf("task was not fenced: %+v", task)
	}
	if task.FailureCode != ReasonRoleChanged || task.Plan.Steps[0].Approved || task.Plan.Steps[0].ApprovalDigest != "" || task.Plan.Steps[0].State != agentrun.StepPending {
		t.Fatalf("approval was not voided: %+v", task.Plan.Steps[0])
	}
	if _, err := coordinator.StartStep(context.Background(), StepRequest{GrantID: "grant-1"}); !errors.Is(err, ErrRevoked) {
		t.Fatalf("StartStep after revocation = %v, want ErrRevoked", err)
	}
}

func TestTodo_AGENT2_020_Golden(t *testing.T) {
	coordinator, _, _, _ := fixture(t)
	result, err := coordinator.Apply(context.Background(), Event{TenantID: "tenant-a", UserID: "user-1", Cause: CauseRoleChanged, OccurredAt: time.Unix(101, 0)})
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:85f03d72ff3486b048358000b0d86d7b1a014ee468ec8292f6b91ef7fcfc1aa2"
	if result.Digest != want {
		t.Fatalf("digest = %q, want %q", result.Digest, want)
	}
}

func TestTodo_AGENT2_020_Security(t *testing.T) {
	coordinator, epochs, grants, tasks := fixture(t)
	other := Grant{ID: "grant-2", TenantID: "tenant-b", UserID: "user-1", AgentVersion: "agent-v1", InstallationID: "install-1", Epochs: map[string]uint64{}}
	for _, scope := range grantScopes(other) {
		other.Epochs[scope.key()] = 1
	}
	grants.add(other)
	tasks.add(agentrun.AgentTask{ID: "task-2", TenantID: "tenant-b", UserID: "user-1", State: agentrun.StateWaiting, Version: 1})
	if _, err := coordinator.Apply(context.Background(), Event{TenantID: "tenant-a", UserID: "user-1", Cause: CauseUserDeactivated}); err != nil {
		t.Fatal(err)
	}
	untouched, _ := grants.Get(context.Background(), "grant-2")
	if untouched.Revoked {
		t.Fatal("cross-tenant grant was revoked")
	}
	if tasks.get("task-2").State != agentrun.StateWaiting {
		t.Fatal("cross-tenant task was paused")
	}
	if _, err := coordinator.Apply(context.Background(), Event{TenantID: "tenant-a", ConnectionID: "conn-1", Cause: CauseConnectionAdminRevoke}); err != nil {
		t.Fatal(err)
	}
	if got, _ := epochs.Current(context.Background(), Scope{Kind: ScopeConnection, TenantID: "tenant-a", Subject: "conn-1"}); got != 2 {
		t.Fatal("connection epoch did not bump")
	}
	if _, err := coordinator.Apply(context.Background(), Event{TenantID: "tenant-a", AgentVersion: "agent-v1", Cause: CauseAgentQuarantined}); err != nil {
		t.Fatal(err)
	}
	if got, _ := epochs.Current(context.Background(), Scope{Kind: ScopeAgent, TenantID: "tenant-a", Subject: "agent-v1"}); got != 2 {
		t.Fatal("agent quarantine epoch did not bump")
	}
	if _, err := coordinator.Apply(context.Background(), Event{TenantID: "tenant-a", Cause: CauseTenantKillSwitch}); err != nil {
		t.Fatal(err)
	}
	if got, _ := epochs.Current(context.Background(), Scope{Kind: ScopeTenant, TenantID: "tenant-a", Subject: "tenant-a"}); got != 2 {
		t.Fatal("tenant kill-switch epoch did not bump")
	}
	if _, err := coordinator.Apply(context.Background(), Event{TenantID: "tenant-a", Cause: CauseRoleChanged}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("malformed role event = %v, want ErrInvalid", err)
	}
	permit := StepPermit{ExpiresAt: time.Unix(100, 0).Add(MaxTokenLifetime)}
	if permit.Valid(time.Unix(100, 0).Add(MaxTokenLifetime)) || permit.Valid(time.Unix(100, 0).Add(MaxTokenLifetime+time.Nanosecond)) {
		t.Fatal("permit lifetime was not bounded to five minutes")
	}
}

func TestTodo_AGENT2_020_Security_ScopedTasks(t *testing.T) {
	coordinator, epochs, grants, tasks := fixture(t)
	otherFamily := Grant{ID: "grant-other-family", TaskID: "task-other-family", TenantID: "tenant-a", UserID: "user-1", SessionFamilyID: "family-2", AgentVersion: "agent-v2", InstallationID: "install-2", Epochs: map[string]uint64{}}
	for _, scope := range grantScopes(otherFamily) {
		otherFamily.Epochs[scope.key()] = 1
	}
	grants.add(otherFamily)
	tasks.add(agentrun.AgentTask{ID: "task-other-family", TenantID: "tenant-a", UserID: "user-1", State: agentrun.StateWaiting, Version: 1})
	if _, err := coordinator.Apply(context.Background(), Event{TenantID: "tenant-a", UserID: "user-1", SessionFamilyID: "family-1", Cause: CauseSessionFamilyRevoked}); err != nil {
		t.Fatal(err)
	}
	other, _ := grants.Get(context.Background(), "grant-other-family")
	if other.Revoked || tasks.get("task-other-family").State != agentrun.StateWaiting || tasks.get("task-1").State != agentrun.StatePaused {
		t.Fatal("revoking one session family affected a sibling family")
	}
	userEpoch, _ := epochs.Current(context.Background(), Scope{Kind: ScopeUser, TenantID: "tenant-a", Subject: "user-1"})
	familyEpoch, _ := epochs.Current(context.Background(), Scope{Kind: ScopeSession, TenantID: "tenant-a", Subject: "family-1"})
	otherFamilyEpoch, _ := epochs.Current(context.Background(), Scope{Kind: ScopeSession, TenantID: "tenant-a", Subject: "family-2"})
	if userEpoch != 1 || familyEpoch != 2 || otherFamilyEpoch != 1 {
		t.Fatalf("session revocation epochs user=%d family=%d sibling=%d, want 1/2/1", userEpoch, familyEpoch, otherFamilyEpoch)
	}

	coordinator, _, _, tasks = fixture(t)
	tasks.add(agentrun.AgentTask{ID: "unrelated-task", TenantID: "tenant-a", UserID: "user-2", State: agentrun.StateWaiting, Version: 1})
	if _, err := coordinator.Apply(context.Background(), Event{TenantID: "tenant-a", AgentVersion: "agent-v1", Cause: CauseAgentQuarantined}); err != nil {
		t.Fatal(err)
	}
	if tasks.get("task-1").State != agentrun.StatePaused || tasks.get("unrelated-task").State != agentrun.StateWaiting {
		t.Fatal("agent quarantine did not pause exactly the task attached to its grant")
	}
}

func TestTodo_AGENT2_020_Race(t *testing.T) {
	coordinator, _, _, _ := fixture(t)
	var revoked atomic.Bool
	var applyErr error
	var applyWG sync.WaitGroup
	applyWG.Add(1)
	go func() {
		defer applyWG.Done()
		_, applyErr = coordinator.Apply(context.Background(), Event{TenantID: "tenant-a", UserID: "user-1", Cause: CauseSessionFamilyRevoked, SessionFamilyID: "family-1"})
		revoked.Store(true)
	}()
	var wg sync.WaitGroup
	var postCommitSuccess atomic.Int64
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 32; j++ {
				_, err := coordinator.StartStep(context.Background(), StepRequest{GrantID: "grant-1"})
				if err == nil && revoked.Load() {
					postCommitSuccess.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	applyWG.Wait()
	if applyErr != nil {
		t.Fatal(applyErr)
	}
	if postCommitSuccess.Load() != 0 {
		t.Fatalf("%d steps started after revocation commit", postCommitSuccess.Load())
	}
	if _, err := coordinator.StartStep(context.Background(), StepRequest{GrantID: "grant-1"}); !errors.Is(err, ErrRevoked) {
		t.Fatalf("post-race StartStep = %v, want ErrRevoked", err)
	}
}
