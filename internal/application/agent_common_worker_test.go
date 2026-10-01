package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

type commonWorkerSource struct {
	build                func()
	persisted, delivered int
	deliveryFails        bool
	reconciled           int
	status               runstate.EffectStatus
	result               CommonAgentOutput
}

func (s *commonWorkerSource) BuildCommonAgentModelWork(_ context.Context, record agentrun.Record, run runstate.Run) (AgentModelExecutorRequest, error) {
	if s.build != nil {
		s.build()
	}
	task, err := NewTrustedModelTask(run.TenantID, run.ID, run.AgentDigest, "common.worker")
	if err != nil {
		return AgentModelExecutorRequest{}, err
	}
	return AgentModelExecutorRequest{Task: task, StepID: run.ID, Route: agentmodel.RouteRequest{TraceID: run.ID, Pin: agentmodel.ModelPin{AgentVersionDigest: run.AgentDigest, TaskProfileID: "reply", Primary: agentmodel.ModelSelection{ProfileID: "model"}}, Task: agentmodel.TaskProfile{ID: "reply", AgentVersionDigest: run.AgentDigest, MaxCostMicros: 100}},
		Model:    agentmodel.ModelRequest{ContractVersion: 1, TraceID: run.ID, TaskProfile: "reply", ModelProfile: "model", Messages: []agentmodel.ModelMessage{{Role: agentmodel.RoleUser, Content: "Approved source content"}}, Output: agentmodel.OutputConstraint{Mode: agentmodel.OutputText}, Deadline: run.Deadline, Limits: agentmodel.ModelLimits{MaxCostMicros: 100, MaxInputTokens: 200, MaxOutputTokens: 100}, Processing: agentmodel.ProcessingPolicy{Residency: "us", Retention: "NONE:0", TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied}},
		Outbound: agentegress.OutboundRequest{TaskID: run.ID, Tenant: run.TenantID, Principal: commonAgentExecutionPrincipal(record.Request), Purpose: "inference", Region: "us", Profile: agentegress.Profile{ID: "model", Kind: agentegress.TargetModel}}, Lease: lease.CredentialLease{Tenant: run.TenantID, Purpose: "inference", Destination: "model"}}, nil
}

func (s *commonWorkerSource) ValidateAndPersistCommonAgentOutput(_ context.Context, _ agentrun.Record, _ runstate.Run, result agentmodel.ModelResult) (CommonAgentOutput, error) {
	if result.Text != "actual answer" {
		return CommonAgentOutput{}, ErrCommonAgentWorker
	}
	s.persisted++
	return CommonAgentOutput{Ref: "artifact:" + strings.Repeat("d", 64), Digest: "sha256:" + strings.Repeat("d", 64)}, nil
}
func (s *commonWorkerSource) DeliverCommonAgentOutput(_ context.Context, _ agentrun.Record, _ runstate.Run, output CommonAgentOutput) error {
	if output.Ref != "artifact:"+strings.Repeat("d", 64) {
		return ErrCommonAgentWorker
	}
	s.delivered++
	if s.deliveryFails {
		return errors.New("owner temporarily unavailable")
	}
	return nil
}
func (s *commonWorkerSource) ExecuteCommonAgentTool(context.Context, agentrun.Record, runstate.Run, agentmodel.ToolProposal, string) ([]byte, CommonAgentOutput, error) {
	return nil, CommonAgentOutput{}, errors.New("no tool applied")
}
func (s *commonWorkerSource) ReconcileCommonAgentEffect(context.Context, agentrun.Record, runstate.Run, runstate.Effect) (runstate.EffectStatus, CommonAgentOutput, error) {
	s.reconciled++
	return s.status, s.result, nil
}

type commonWorkerModel struct {
	calls     int
	principal string
	result    agentmodel.ModelResult
}

func (m *commonWorkerModel) Execute(ctx context.Context, request AgentModelExecutorRequest) (AgentModelExecutorResult, error) {
	if ctx.Err() != nil {
		return AgentModelExecutorResult{}, ctx.Err()
	}
	m.calls++
	m.principal = request.Outbound.Principal
	if lease, ok := ctx.Value(agentResourceContextKey{}).(*AgentResourceLease); !ok || lease == nil {
		return AgentModelExecutorResult{}, ErrAgentResourceIdentity
	}
	result := m.result
	if result.Finish == "" {
		result = agentmodel.ModelResult{ContractVersion: 1, Finish: agentmodel.FinishComplete, Text: "actual answer"}
	}
	return AgentModelExecutorResult{Result: result}, nil
}

func commonWorkerFixture(t *testing.T, mode agentrun.RunMode) (*CommonAgentWorker, agentrun.Record, *commonWorkerSource, *commonWorkerModel, *commonAgentTestAuthority, *time.Time) {
	t.Helper()
	runtime, _, authority, now := commonAgentTestRuntime(t)
	*now = time.Now().UTC()
	request := commonAgentTestRequest(*now)
	request.Source.Kind = agentrun.SourceWorkflow
	if mode == agentrun.ModeOnBehalfOf {
		request.Principal.Mode = mode
		request.Principal.SponsorID = ""
		request.Principal.InvokerID = "real-user"
		request.Principal.DelegatedCredentialRef = "actual-grant"
	}
	record, _, err := runtime.Admit(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	resources, err := NewAgentResourceRuntime(AgentResourceRuntimeConfig{Policy: DefaultAgentResourcePolicy("test-cell")})
	if err != nil {
		t.Fatal(err)
	}
	source, model := &commonWorkerSource{}, &commonWorkerModel{}
	worker, err := NewCommonAgentWorker(CommonAgentWorkerConfig{Runtime: runtime, Model: model, Resources: resources, Sources: map[agentrun.SourceKind]CommonAgentExecutionSource{agentrun.SourceWorkflow: source}, WorkerID: "worker", LeaseTTL: time.Minute, Now: func() time.Time { return *now }})
	if err != nil {
		t.Fatal(err)
	}
	return worker, record, source, model, authority, now
}

func TestTodo_AGENT_033_CommonWorker(t *testing.T) {
	for _, mode := range []agentrun.RunMode{agentrun.ModeSponsored, agentrun.ModeOnBehalfOf} {
		t.Run(string(mode), func(t *testing.T) {
			worker, record, source, model, _, _ := commonWorkerFixture(t, mode)
			run, err := worker.Execute(context.Background(), record.Request.Source.TenantID, record.ID)
			if err != nil || run.State != runstate.StateCompleted || model.calls != 1 || source.persisted != 1 || source.delivered != 1 || model.principal != commonAgentExecutionPrincipal(record.Request) {
				t.Fatalf("execution=%+v err=%v calls=%d persisted=%d delivered=%d principal=%s", run, err, model.calls, source.persisted, source.delivered, model.principal)
			}
			validation, ok := commonAgentValidatedOutput(run)
			last := run.Checkpoints[len(run.Checkpoints)-1]
			if !ok || last.Phase != runstate.PhaseDelivery || last.Ref != validation.Ref || last.Digest != validation.Digest {
				t.Fatalf("output checkpoints=%+v", run.Checkpoints)
			}
			if _, err := worker.Execute(context.Background(), run.TenantID, run.ID); err != nil || model.calls != 1 || source.delivered != 1 {
				t.Fatalf("replay inference/delivery calls=%d/%d err=%v", model.calls, source.delivered, err)
			}
		})
	}
}

func TestTodo_AGENT_033_CommonWorker_Recovery(t *testing.T) {
	worker, record, source, model, _, now := commonWorkerFixture(t, agentrun.ModeSponsored)
	source.deliveryFails = true
	run, err := worker.Execute(context.Background(), record.Request.Source.TenantID, record.ID)
	if err == nil || run.State != runstate.StateRunning || source.persisted != 1 {
		t.Fatalf("initial=%+v err=%v", run, err)
	}
	*now = now.Add(2 * time.Minute)
	source.deliveryFails = false
	restarted, err := NewCommonAgentWorker(worker.cfg)
	if err != nil {
		t.Fatal(err)
	}
	run, err = restarted.Execute(context.Background(), run.TenantID, run.ID)
	if err != nil || run.State != runstate.StateCompleted || model.calls != 1 || source.persisted != 1 || source.delivered != 2 {
		t.Fatalf("restart=%+v err=%v model=%d persisted=%d delivered=%d", run, err, model.calls, source.persisted, source.delivered)
	}
}

func TestTodo_AGENT_033_CommonWorker_Security(t *testing.T) {
	worker, record, source, model, authority, _ := commonWorkerFixture(t, agentrun.ModeSponsored)
	source.build = func() { authority.revoked = true }
	_, err := worker.Execute(context.Background(), record.Request.Source.TenantID, record.ID)
	if err == nil || model.calls != 0 || source.persisted != 0 || source.delivered != 0 {
		t.Fatalf("revoked sponsor dispatched: err=%v calls=%d", err, model.calls)
	}
	if _, err := worker.Execute(context.Background(), "foreign", record.ID); err == nil {
		t.Fatal("cross tenant execution accepted")
	}
	if _, err := NewCommonAgentWorker(CommonAgentWorkerConfig{}); !errors.Is(err, ErrCommonAgentWorker) {
		t.Fatalf("composition=%v", err)
	}
}

func TestTodo_AGENT_046_CommonWorkerRestoredEffect_Security(t *testing.T) {
	worker, record, source, model, _, now := commonWorkerFixture(t, agentrun.ModeSponsored)
	run, err := worker.cfg.Runtime.Claim(context.Background(), record.Request.Source.TenantID, record.ID, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	state, err := worker.cfg.Runtime.ExecutionService(context.Background(), run.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	run, err = state.BeginEffect(context.Background(), run.ID, "worker", "effect", "owner-idempotency-key", "sha256:"+strings.Repeat("e", 64), run.Fence, run.Version, *now)
	if err != nil {
		t.Fatal(err)
	}
	effect := run.Effects[0]
	source.status = runstate.EffectApplied
	source.result = CommonAgentOutput{Ref: "owner-receipt", Digest: "sha256:" + strings.Repeat("f", 64)}
	status, ref, digest, err := worker.LookupRestoredEffect(context.Background(), run.TenantID, run.ID, effect)
	if err != nil || status != runstate.EffectApplied || ref != source.result.Ref || digest != source.result.Digest || model.calls != 0 || source.reconciled != 1 {
		t.Fatalf("lookup=%s %s %s %v", status, ref, digest, err)
	}
	effect.IdempotencyKey = "forged"
	if _, _, _, err := worker.LookupRestoredEffect(context.Background(), run.TenantID, run.ID, effect); err == nil || source.reconciled != 1 {
		t.Fatalf("forged effect reached owner: %v", err)
	}
	retained, err := worker.cfg.Runtime.GetRun(context.Background(), run.TenantID, run.ID)
	if err != nil || retained.Effects[0].Status != runstate.EffectUnknown {
		t.Fatalf("lookup mutated effect: %+v %v", retained, err)
	}
}
