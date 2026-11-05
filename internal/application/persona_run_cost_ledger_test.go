package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// The AGENTCOST-001 executor half: each model turn describes itself for the
// recording adapter, and newly recorded lines seal into the run record under
// the worker lease.

func TestPersonaRunTriggerKind(t *testing.T) {
	for kind, want := range map[agentrun.SourceKind]string{
		agentrun.SourcePersonaMention: "mention",
		agentrun.SourceSupportEmail:   "email",
		agentrun.SourceSchedule:       "schedule",
		agentrun.SourceChat:           "chat",
		agentrun.SourceAnnouncement:   "announcement",
		agentrun.SourceWorkflow:       "workflow",
	} {
		if got := personaRunTriggerKind(kind); got != want {
			t.Fatalf("trigger(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestPersonaRunCallContext(t *testing.T) {
	admission := agentrun.Record{Request: agentrun.Request{
		Source:  agentrun.SourceIdentity{TenantID: "tenant-a", Kind: agentrun.SourcePersonaMention},
		CauseID: "chat-post-42",
	}}
	run := runstate.Run{ID: "run-1", TenantID: "tenant-a", AgentID: "agent-a"}
	call := personaRunCallContext(admission, run, personaRunAnswerCallSite, agentmodel.CallStageAnswer)
	if call.TenantID != "tenant-a" || call.AgentID != "agent-a" || call.TriggerKind != "mention" ||
		call.RunID != "run-1" || call.QuestionID != "chat-post-42" ||
		call.CallSite != "persona_run_executor:answer" || call.Stage != agentmodel.CallStageAnswer || call.Kind != "" {
		t.Fatalf("call context = %+v", call)
	}
	continuation := personaRunCallContext(admission, run, personaRunContinuationCallSite, agentmodel.CallStageToolContinuation)
	if continuation.CallSite != "persona_run_executor:tool_continuation" || continuation.Stage != agentmodel.CallStageToolContinuation {
		t.Fatalf("continuation context = %+v", continuation)
	}
}

func TestPersonaRunSealModelCalls(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	store := runstate.NewMemoryStore()
	run := runstate.Run{ID: "cost-run", AdmissionID: "cost-run", TenantID: "tenant-a", AgentID: "agent-a",
		Version: 1, Fence: 1, State: runstate.StateRunning, Deadline: now.Add(time.Hour),
		Lease: &runstate.Lease{Owner: "worker", Fence: 1, Until: now.Add(time.Hour)}}
	if err := store.Create(ctx, run); err != nil {
		t.Fatal(err)
	}
	state, err := runstate.New(store, personaChatAdmissionRecheckerFake{})
	if err != nil {
		t.Fatal(err)
	}
	executor := &personaAdmittedRunExecutor{state: state, store: store, workerID: "worker", now: func() time.Time { return now }}
	line := agentmodel.CallLine{
		TenantID: "tenant-a", AgentID: "agent-a", TriggerKind: "mention", RunID: "cost-run",
		CallSite: personaRunAnswerCallSite, Stage: agentmodel.CallStageAnswer, Kind: agentmodel.CallKindGeneration,
		ProviderID: "schemaflux", ModelID: "gpt-6-luna", ModelVersion: "2026-09-01",
		InputTokens: 100, OutputTokens: 20, CostMicros: 60, Basis: agentmodel.CostMeasured,
		LatencyMillis: 410, StartedAt: now,
	}
	var ledger agentmodel.CallLedger
	if err := ledger.Append(line); err != nil {
		t.Fatal(err)
	}
	sealed, err := executor.sealModelCalls(ctx, run, &ledger)
	if err != nil {
		t.Fatalf("sealing one line: %v", err)
	}
	if len(sealed.Calls) != 1 || sealed.Calls[0] != line {
		t.Fatalf("sealed calls = %+v", sealed.Calls)
	}
	// Sealing again seals nothing new: the version does not move.
	again, err := executor.sealModelCalls(ctx, sealed, &ledger)
	if err != nil || again.Version != sealed.Version {
		t.Fatalf("repeat seal moved the run: %+v, %v", again, err)
	}
	if _, err := executor.sealModelCalls(ctx, sealed, nil); err != nil {
		t.Fatalf("nil ledger seal: %v", err)
	}
	// A second line seals exactly itself.
	second := line
	second.CallSite = personaRunContinuationCallSite
	second.Stage = agentmodel.CallStageToolContinuation
	if err := ledger.Append(second); err != nil {
		t.Fatal(err)
	}
	sealed, err = executor.sealModelCalls(ctx, sealed, &ledger)
	if err != nil {
		t.Fatalf("sealing a second line: %v", err)
	}
	if len(sealed.Calls) != 2 || sealed.Calls[1] != second {
		t.Fatalf("resealed calls = %+v", sealed.Calls)
	}
	// A foreign line seals nothing.
	foreign := line
	foreign.RunID = "another-run"
	combined := agentmodel.CallLedger{}
	for _, sealedLine := range sealed.Calls {
		if err := combined.Append(sealedLine); err != nil {
			t.Fatal(err)
		}
	}
	if err := combined.Append(foreign); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.sealModelCalls(ctx, sealed, &combined); err == nil {
		t.Fatal("a foreign line was sealed")
	}
	stored, err := store.Get(ctx, run.ID)
	if err != nil || len(stored.Calls) != 2 {
		t.Fatalf("the refused seal changed the record: %+v, %v", stored.Calls, err)
	}
}

// agentcost001RecordingModel simulates the recording adapter: every Execute
// appends one line to the ledger carried on the call context, capturing Usage
// + provider identity from its scripted result. Turn identity (call site and
// stage) follows the call order, as the executor's call contexts do; run
// identity is the test's, as the recorder reads it from its call context.
type agentcost001RecordingModel struct {
	replies  []agentmodel.ModelResult
	agentID  string
	trigger  string
	question string
	now      time.Time
	calls    int
}

func (m *agentcost001RecordingModel) Execute(ctx context.Context, request AgentModelExecutorRequest) (AgentModelExecutorResult, error) {
	m.calls++
	reply := m.replies[m.calls-1]
	if ledger := agentmodel.CallLedgerFromContext(ctx); ledger != nil {
		site, stage := personaRunAnswerCallSite, agentmodel.CallStageAnswer
		if m.calls > 1 {
			site, stage = personaRunContinuationCallSite, agentmodel.CallStageToolContinuation
		}
		basis := agentmodel.CostEstimated
		if reply.Usage.CostMicros > 0 {
			basis = agentmodel.CostMeasured
		}
		_ = ledger.Append(agentmodel.CallLine{
			TenantID: request.Task.TenantID, AgentID: m.agentID,
			TriggerKind: m.trigger, RunID: request.Task.TaskID,
			QuestionID: m.question, CallSite: site, Stage: stage,
			Kind:       agentmodel.CallKindGeneration,
			ProviderID: reply.Provider.ProviderID, ModelID: reply.Provider.ModelID,
			ModelVersion: reply.Provider.Version,
			InputTokens:  reply.Usage.InputTokens, OutputTokens: reply.Usage.OutputTokens,
			CachedInputTokens: reply.Usage.CachedInputTokens,
			CostMicros:        reply.Usage.CostMicros, Basis: basis,
			LatencyMillis: 7, StartedAt: m.now,
		})
	}
	return AgentModelExecutorResult{Result: reply}, nil
}

// agentcost001Run admits one mention on the memory admission and run stores
// and executes it end to end with the recording model, reusing the served
// fixtures (agentUXSpeedWork, agentUXSpeedTools, personaRunOutputFixture,
// agentUXSpeedReply). It returns the finished run and the model.
func agentcost001Run(t *testing.T, replies ...agentmodel.ModelResult) (runstate.Run, *agentcost001RecordingModel, error) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	request := foregroundAuthorityRequest(now)
	request.Source.Key, request.Source.Ref, request.CauseID = "invocation-a", "post-a", "invocation-a"
	request.Persona = &agentrun.PersonaRef{ID: "persona-a", Version: "7", Digest: "sha256:" + strings.Repeat("a", 64)}
	request.Agent = agentrun.VersionRef{AgentID: "agent-a", Version: "4", Digest: "sha256:" + strings.Repeat("d", 64)}
	request.Principal.InvokerID, request.Purpose = "alice", "persona-chat"
	request.Audience.ID, request.Context.ID = "room-a", "thread-a"
	admissionService, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: personaChatAdmissionAuthorityFake{}, Store: agentrun.NewMemoryAdmissionStore(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	admission, _, err := admissionService.Admit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	runStore := runstate.NewMemoryStore()
	state, err := runstate.New(runStore, personaChatAdmissionRecheckerFake{})
	if err != nil {
		t.Fatal(err)
	}
	validator, _, _, _, _ := personaRunOutputFixture(t)
	model := &agentcost001RecordingModel{replies: replies, agentID: "agent-a", trigger: "mention", question: "invocation-a", now: now}
	executor := &personaAdmittedRunExecutor{state: state, store: runStore, model: model, work: agentUXSpeedWork{t: t}, tools: agentUXSpeedTools{}, output: validator, reply: agentUXSpeedReply{}, workerID: "agentcost001-worker", leaseTTL: time.Minute, now: func() time.Time { return now }}
	principal := foregroundPrincipal(t, now.Add(-time.Minute), now.Add(time.Hour), request.Purpose, "alice", "tenant-a")
	run, runErr := executor.Start(trust.WithPrincipal(ctx, principal), admission)
	return run, model, runErr
}

func agentcost001Answer() agentmodel.ModelResult {
	return agentmodel.ModelResult{
		ContractVersion: agentmodel.ContractVersion,
		Text:            "The policy allows annual leave carryover.",
		Finish:          agentmodel.FinishComplete,
		Provider:        agentmodel.ModelIdentity{ProviderID: "schemaflux", ModelID: "gpt-6-luna", Version: "2026-09-01"},
		Usage:           agentmodel.ModelUsage{InputTokens: 100, OutputTokens: 20, TotalTokens: 120, CostMicros: 60},
	}
}

func agentcost001Search() agentmodel.ModelResult {
	return agentmodel.ModelResult{
		ContractVersion: agentmodel.ContractVersion,
		Text:            "I will search.",
		Finish:          agentmodel.FinishToolCalls,
		ToolProposals:   []agentmodel.ToolProposal{{ID: "search-1", Name: personaDocumentSearchTool, Arguments: []byte(`{"query":"leave"}`)}},
		Provider:        agentmodel.ModelIdentity{ProviderID: "schemaflux", ModelID: "gpt-6-luna", Version: "2026-09-01"},
		Usage:           agentmodel.ModelUsage{InputTokens: 90, OutputTokens: 10, TotalTokens: 100, CostMicros: 45},
	}
}

// TestTodo_AGENTCOST_001_ExecutorSealsRecordedCalls is the AGENTCOST-001
// executor e2e: a finished run seals exactly the lines its model turns
// recorded, with the answer call site, and a tool-proposal run seals answer +
// continuation lines.
//
// Known gap (follow-up, not fixed here): lines from FAILED model turns stay
// in the memory ledger and are not sealed (fail() transitions terminal under
// the same save). The budget ledger still settles billed usage via the
// gateway, so money is not lost, only the per-call split of failed turns.
func TestTodo_AGENTCOST_001_ExecutorSealsRecordedCalls(t *testing.T) {
	t.Run("answer seals its line", func(t *testing.T) {
		answer := agentcost001Answer()
		run, model, err := agentcost001Run(t, answer)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if run.State != runstate.StateCompleted || model.calls != 1 {
			t.Fatalf("run state=%s model calls=%d", run.State, model.calls)
		}
		if len(run.Calls) != 1 {
			t.Fatalf("sealed calls = %+v, want the one recorded line", run.Calls)
		}
		line := run.Calls[0]
		if line.CallSite != personaRunAnswerCallSite || line.Stage != agentmodel.CallStageAnswer {
			t.Fatalf("sealed line = %+v, want the answer call site", line)
		}
		if line.TenantID != "tenant-a" || line.AgentID != "agent-a" || line.TriggerKind != "mention" ||
			line.RunID != run.ID || line.QuestionID != "invocation-a" || line.Kind != agentmodel.CallKindGeneration {
			t.Fatalf("sealed line lost its run identity: %+v", line)
		}
		if line.ProviderID != "schemaflux" || line.ModelID != "gpt-6-luna" || line.ModelVersion != "2026-09-01" ||
			line.InputTokens != 100 || line.OutputTokens != 20 || line.CostMicros != 60 || line.Basis != agentmodel.CostMeasured {
			t.Fatalf("sealed line lost its usage: %+v", line)
		}
	})

	t.Run("tool proposal seals answer and continuation", func(t *testing.T) {
		search, answer := agentcost001Search(), agentcost001Answer()
		run, model, err := agentcost001Run(t, search, answer)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if run.State != runstate.StateCompleted || model.calls != 2 {
			t.Fatalf("run state=%s model calls=%d", run.State, model.calls)
		}
		if len(run.Calls) != 2 {
			t.Fatalf("sealed calls = %+v, want answer + continuation lines", run.Calls)
		}
		first, second := run.Calls[0], run.Calls[1]
		if first.CallSite != personaRunAnswerCallSite || first.Stage != agentmodel.CallStageAnswer {
			t.Fatalf("first sealed line = %+v, want the answer call site", first)
		}
		if second.CallSite != personaRunContinuationCallSite || second.Stage != agentmodel.CallStageToolContinuation {
			t.Fatalf("second sealed line = %+v, want the continuation call site", second)
		}
		if first.InputTokens != 90 || first.OutputTokens != 10 || first.CostMicros != 45 ||
			second.InputTokens != 100 || second.OutputTokens != 20 || second.CostMicros != 60 {
			t.Fatalf("sealed lines lost their per-turn usage: %+v %+v", first, second)
		}
		if first.RunID != run.ID || second.RunID != run.ID {
			t.Fatalf("sealed lines are not this run's: %+v %+v", first, second)
		}
	})
}
