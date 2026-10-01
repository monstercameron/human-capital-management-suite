package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

type commonAgentTestInbox struct {
	*agentrun.MemoryAdmissionStore
	mu      sync.Mutex
	records map[string]agentrun.Record
}

func (s *commonAgentTestInbox) CreateOrGet(ctx context.Context, candidate agentrun.Record) (agentrun.Record, bool, error) {
	record, created, err := s.MemoryAdmissionStore.CreateOrGet(ctx, candidate)
	if err == nil {
		s.mu.Lock()
		s.records[record.ID] = record
		s.mu.Unlock()
	}
	return record, created, err
}

func (s *commonAgentTestInbox) GetByID(_ context.Context, id string) (agentrun.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[id]
	if !ok {
		return agentrun.Record{}, runstate.ErrNotFound
	}
	return record, nil
}

func (s *commonAgentTestInbox) ListBySource(_ context.Context, source agentrun.SourceKind, limit int) ([]agentrun.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []agentrun.Record
	for _, record := range s.records {
		if record.Request.Source.Kind == source && len(records) < limit {
			records = append(records, record)
		}
	}
	return records, nil
}

type commonAgentTestStores struct {
	inbox *commonAgentTestInbox
	state *runstate.MemoryStore
}

func (s commonAgentTestStores) ForTenant(_ context.Context, tenant string) (CommonAgentAdmissionStore, runstate.Store, error) {
	if tenant != "common-tenant" {
		return nil, nil, runstate.ErrNotFound
	}
	return s.inbox, s.state, nil
}

type commonAgentTestAuthority struct{ revoked bool }

type commonAgentTestExecutionFence struct{ decision error }

func (f commonAgentTestExecutionFence) WithExecutionFence(_ context.Context, _ agentrun.Request, claim func(error) (runstate.Run, error)) (runstate.Run, error) {
	return claim(f.decision)
}

func (a *commonAgentTestAuthority) VerifyAdmission(_ context.Context, request agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	if a.revoked {
		return agentrun.AuthoritySnapshot{}, commonAgentRefusal("SPONSOR_REVOKED")
	}
	return agentrun.AuthoritySnapshot{Agent: request.Agent, InstallationID: request.InstallationID, Principal: request.Principal,
		Audience: request.Audience, Context: request.Context, BudgetCeiling: request.Budget, GrantRef: "trust-binding", PolicyDigest: "sha256:" + strings.Repeat("b", 64)}, nil
}

func commonAgentTestRequest(now time.Time) agentrun.Request {
	digest := "sha256:" + strings.Repeat("a", 64)
	return agentrun.Request{Source: agentrun.SourceIdentity{TenantID: "common-tenant", Kind: agentrun.SourceSchedule, Key: "owner-occurrence-key", Ref: "schedule/7/firing/3"},
		LegalEntity: "entity-a", Agent: agentrun.VersionRef{AgentID: "agent-a", Version: "1", Digest: digest}, InstallationID: "install-a",
		Principal: agentrun.PrincipalChain{Mode: agentrun.ModeSponsored, AgentPrincipalID: "agent-principal", SponsorID: "sponsor"}, Purpose: "bounded analysis",
		Audience: agentrun.AudienceScope{ID: "private-a", SnapshotID: "audience-1", Digest: digest}, Context: agentrun.ContextScope{ID: "context-a", SnapshotID: "context-1", Digest: digest},
		Deadline: now.Add(time.Hour), Budget: agentrun.Budget{MaxCostMicros: 100, MaxInputTokens: 200, MaxOutputTokens: 100}, CauseID: "cause-1"}
}

func commonAgentTestRuntime(t *testing.T) (*CommonAgentRuntime, commonAgentTestStores, *commonAgentTestAuthority, *time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	stores := commonAgentTestStores{inbox: &commonAgentTestInbox{MemoryAdmissionStore: agentrun.NewMemoryAdmissionStore(), records: make(map[string]agentrun.Record)}, state: runstate.NewMemoryStore()}
	authority := &commonAgentTestAuthority{}
	runtime, err := NewCommonAgentRuntime(CommonAgentRuntimeConfig{Stores: stores, Authority: authority, Now: func() time.Time { return now },
		ExecutionFences: map[agentrun.SourceKind]CommonAgentExecutionFence{agentrun.SourceSchedule: commonAgentTestExecutionFence{}}})
	if err != nil {
		t.Fatal(err)
	}
	return runtime, stores, authority, &now
}

func TestTodo_AGENT_031_CommonRuntime(t *testing.T) {
	runtime, _, _, now := commonAgentTestRuntime(t)
	ctx := context.Background()
	request := commonAgentTestRequest(*now)
	record, created, err := runtime.Admit(ctx, request)
	if err != nil || !created || record.Decision != agentrun.DecisionAccepted || record.Request.Source.Key != request.Source.Key {
		t.Fatalf("admission = %+v %v %v", record, created, err)
	}
	run, err := runtime.GetRun(ctx, request.Source.TenantID, record.ID)
	if err != nil || run.State != runstate.StateReady || run.ActorID != request.Principal.SponsorID {
		t.Fatalf("durable ready = %+v %v", run, err)
	}
	second, created, err := runtime.Admit(ctx, request)
	if err != nil || created || second.ID != record.ID {
		t.Fatalf("replay = %+v %v %v", second, created, err)
	}
	request.Purpose = "changed"
	if _, _, err := runtime.Admit(ctx, request); !errors.Is(err, agentrun.ErrSourceConflict) {
		t.Fatalf("changed replay = %v", err)
	}
}

func TestTodo_AGENT_031_CommonRuntime_Recovery(t *testing.T) {
	runtime, stores, _, now := commonAgentTestRuntime(t)
	ctx := context.Background()
	request := commonAgentTestRequest(*now)
	// Persist only the inbox to model death before execution insertion.
	service, _ := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Store: stores.inbox, Authority: runtime.cfg.Authority, Now: runtime.cfg.Now})
	record, _, err := service.Admit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stores.state.Get(ctx, record.ID); !errors.Is(err, runstate.ErrNotFound) {
		t.Fatalf("execution existed before repair: %v", err)
	}
	if _, _, err := runtime.Admit(ctx, request); err != nil {
		t.Fatal(err)
	}
	pendingRuns, err := runtime.Pending(ctx, request.Source.TenantID, request.Source.Kind, 100)
	if err != nil || len(pendingRuns) != 1 || pendingRuns[0].ID != record.ID || pendingRuns[0].State != runstate.StateReady {
		t.Fatalf("restart inbox scan = %+v %v", pendingRuns, err)
	}
	run, err := runtime.Claim(ctx, request.Source.TenantID, record.ID, "worker-1", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	active, err := runtime.Recover(ctx, request.Source.TenantID, record.ID)
	if err != nil || active.State != runstate.StateRunning || active.Version != run.Version || active.Lease == nil || active.Fence != run.Fence {
		t.Fatalf("recovery replaced an active worker lease = %+v %v", active, err)
	}
	state, _ := runtime.executionService(ctx, request.Source.TenantID)
	pending, err := state.BeginEffect(ctx, record.ID, "worker-1", "tool-1", "source-effect-1", request.Agent.Digest, run.Fence, run.Version, *now)
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Second)
	recovered, err := runtime.Recover(ctx, request.Source.TenantID, record.ID)
	if err != nil || recovered.State != runstate.StateReconciling || recovered.Lease != nil || recovered.Effects[0].Status != runstate.EffectUnknown || recovered.Version <= pending.Version {
		t.Fatalf("ambiguity after death = %+v %v", recovered, err)
	}
	*now = request.Deadline
	expired, err := runtime.Recover(ctx, request.Source.TenantID, record.ID)
	if err != nil || expired.State != runstate.StateReconciling || !expired.ExpireRequested {
		t.Fatalf("expired ambiguous effect = %+v %v", expired, err)
	}
	again, err := runtime.Recover(ctx, request.Source.TenantID, record.ID)
	if err != nil || again.Version != expired.Version || again.Effects[0].Status != runstate.EffectUnknown {
		t.Fatalf("repeated recovery rewrote ambiguous effect = %+v %v", again, err)
	}
}

func TestTodo_AGENT_033_CommonRuntime_Recovery(t *testing.T) {
	runtime, stores, _, now := commonAgentTestRuntime(t)
	ctx := context.Background()
	request := commonAgentTestRequest(*now)
	request.Source.Kind, request.Source.Ref = agentrun.SourceWorkflow, "workflow:instance-a:analysis-node"
	service, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Store: stores.inbox, Authority: runtime.cfg.Authority, Now: runtime.cfg.Now})
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := service.Admit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stores.state.Get(ctx, record.ID); !errors.Is(err, runstate.ErrNotFound) {
		t.Fatalf("execution existed before workflow recovery: %v", err)
	}
	run, err := runtime.Recover(ctx, request.Source.TenantID, record.ID)
	if err != nil || run.State != runstate.StateReady || run.AdmissionID != record.ID || run.Version != 1 {
		t.Fatalf("workflow admission gap recovery = %+v %v", run, err)
	}
}

func TestTodo_AGENT_031_CommonRuntime_Security(t *testing.T) {
	runtime, _, authority, now := commonAgentTestRuntime(t)
	ctx := context.Background()
	record, _, err := runtime.Admit(ctx, commonAgentTestRequest(*now))
	if err != nil {
		t.Fatal(err)
	}
	authority.revoked = true
	if _, err := runtime.Claim(ctx, "common-tenant", record.ID, "worker", time.Minute); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
		t.Fatalf("revoked sponsor claim = %v", err)
	}
	if _, err := runtime.GetRun(ctx, "foreign-tenant", record.ID); err == nil {
		t.Fatal("foreign tenant read accepted")
	}
	run, err := runtime.GetRun(ctx, "common-tenant", record.ID)
	if err != nil || run.State != runstate.StateReady || run.Lease != nil {
		t.Fatalf("refusal mutated ready work: %+v %v", run, err)
	}
}

func TestTodo_AGENT_031_CommonRuntime_Deadline(t *testing.T) {
	runtime, _, _, now := commonAgentTestRuntime(t)
	ctx := context.Background()
	request := commonAgentTestRequest(*now)
	record, _, err := runtime.Admit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	*now = request.Deadline
	if err := runtime.Recheck(ctx, request.Source.TenantID, record.ID); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
		t.Fatalf("expired acceptance recheck = %v", err)
	}
	run, err := runtime.Recover(ctx, request.Source.TenantID, record.ID)
	if err != nil || run.State != runstate.StateExpired || !run.ExpireRequested || run.TerminalCode != "EXPIRED" {
		t.Fatalf("ready work deadline recovery = %+v %v", run, err)
	}
	if _, err := runtime.Claim(ctx, request.Source.TenantID, record.ID, "worker", time.Minute); !errors.Is(err, runstate.ErrInvalid) {
		t.Fatalf("expired work claim = %v", err)
	}
}

func TestTodo_AGENT_031_CommonRuntime_Configuration(t *testing.T) {
	if _, err := NewCommonAgentRuntime(CommonAgentRuntimeConfig{}); !errors.Is(err, agentrun.ErrAuthorityMissing) {
		t.Fatalf("unconfigured runtime = %v", err)
	}
	if _, err := NewCommonAgentAuthority(CommonAgentAuthorityConfig{}); !errors.Is(err, agentrun.ErrAuthorityMissing) {
		t.Fatalf("unconfigured authority = %v", err)
	}
	if _, _, err := (DatabaseCommonAgentStores{}).ForTenant(context.Background(), "tenant"); err == nil {
		t.Fatal("unconfigured store factory accepted")
	}
	var runtime *CommonAgentRuntime
	if _, err := runtime.GetAdmission(context.Background(), "tenant", "id"); !errors.Is(err, agentrun.ErrAuthorityMissing) {
		t.Fatalf("nil runtime read = %v", err)
	}
}

func TestTodo_AGENT_030_CommonRuntime_Overlap(t *testing.T) {
	for _, code := range []string{"SCHEDULE_OVERLAP_QUEUED", "SCHEDULE_OVERLAP_SKIPPED", "SCHEDULE_OVERLAP_REFUSED"} {
		t.Run(code, func(t *testing.T) {
			runtime, _, _, now := commonAgentTestRuntime(t)
			var decision error = &CommonAgentExecutionRefused{Code: code}
			if code == "SCHEDULE_OVERLAP_QUEUED" {
				decision = &CommonAgentExecutionDeferred{Code: code}
			}
			runtime.cfg.ExecutionFences[agentrun.SourceSchedule] = commonAgentTestExecutionFence{decision: decision}
			ctx := context.Background()
			record, _, err := runtime.Admit(ctx, commonAgentTestRequest(*now))
			if err != nil {
				t.Fatal(err)
			}
			_, err = runtime.Claim(ctx, "common-tenant", record.ID, "worker", time.Minute)
			if !errors.Is(err, decision) {
				t.Fatalf("source overlap = %v, want %v", err, decision)
			}
			run, err := runtime.GetRun(ctx, "common-tenant", record.ID)
			if err != nil || run.Lease != nil {
				t.Fatalf("overlap acquired model lease = %+v %v", run, err)
			}
			switch code {
			case "SCHEDULE_OVERLAP_QUEUED":
				if run.State != runstate.StateReady || run.Version != 1 || run.TerminalCode != "" {
					t.Fatalf("queued firing consumed = %+v", run)
				}
			case "SCHEDULE_OVERLAP_SKIPPED":
				if run.State != runstate.StateCancelled || !run.CancelRequested || run.TerminalCode != code {
					t.Fatalf("skip was not terminal = %+v", run)
				}
			case "SCHEDULE_OVERLAP_REFUSED":
				if run.State != runstate.StateFailed || !run.FailureRequested || run.TerminalCode != code {
					t.Fatalf("refusal was not terminal = %+v", run)
				}
			}
		})
	}
}

func TestTodo_AGENT_031_CommonRuntime_Race(t *testing.T) {
	runtime, _, _, now := commonAgentTestRuntime(t)
	request := commonAgentTestRequest(*now)
	var wait sync.WaitGroup
	results := make(chan agentrun.Record, 12)
	errorsFound := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			record, _, err := runtime.Admit(context.Background(), request)
			if err != nil {
				errorsFound <- err
			} else {
				results <- record
			}
		}()
	}
	wait.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("concurrent replay: %v", err)
	}
	id := ""
	for record := range results {
		if id != "" && id != record.ID {
			t.Fatalf("multiple run ids: %s %s", id, record.ID)
		}
		id = record.ID
	}
	run, err := runtime.GetRun(context.Background(), "common-tenant", id)
	if err != nil || run.Version != 1 || len(run.Checkpoints) != 1 {
		t.Fatalf("concurrent execution = %+v %v", run, err)
	}
}

func TestTodo_AGENT_011_CommonBinding_Security(t *testing.T) {
	now := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	request := commonAgentTestRequest(now)
	scope := CommonAgentBindingScope{SchemaVersion: 1, TenantID: request.Source.TenantID, Agent: request.Agent, InstallationID: request.InstallationID,
		AgentPrincipal: request.Principal.AgentPrincipalID, SponsorID: request.Principal.SponsorID, LegalEntity: request.LegalEntity, Purpose: request.Purpose,
		Audience: request.Audience, Context: request.Context, Sources: []agentrun.SourceKind{request.Source.Kind}, BudgetCeiling: request.Budget, Deadline: request.Deadline}
	if !commonAgentBindingMatches(scope, request, now) {
		t.Fatal("exact scope denied")
	}
	mutations := []func(*CommonAgentBindingScope){func(s *CommonAgentBindingScope) { s.Agent.Version = "2" }, func(s *CommonAgentBindingScope) { s.SponsorID = "other" }, func(s *CommonAgentBindingScope) { s.Sources = nil }, func(s *CommonAgentBindingScope) { s.BudgetCeiling.MaxCostMicros-- }, func(s *CommonAgentBindingScope) { s.Deadline = now }, func(s *CommonAgentBindingScope) { s.Context.Digest = "changed" }, func(s *CommonAgentBindingScope) { s.Audience.ID = "public" }, func(s *CommonAgentBindingScope) { s.LegalEntity = "other" }, func(s *CommonAgentBindingScope) { s.TenantID = "foreign" }}
	for i, mutate := range mutations {
		changed := scope
		mutate(&changed)
		if commonAgentBindingMatches(changed, request, now) {
			t.Fatalf("scope mutation %d accepted", i)
		}
	}
}
