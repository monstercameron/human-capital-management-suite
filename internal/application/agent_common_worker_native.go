package application

import (
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// NativeCommonAgentWorkerConfig contains deployment-owned provider and policy
// ports. Context artifacts must already be issued by the native source owner.
type NativeCommonAgentWorkerConfig struct {
	Core              dbport.Beginner
	CoreSchema        string
	TenantUUID        func(values.TenantId) uuid.UUID
	Manifests         CommonAgentManifestReader
	Routes            PersonaModelRoutePolicyReader
	OutputPolicy      CommonAgentProtectedOutputPolicy
	OutputConstraint  CommonAgentModelOutputAuthority
	InputClasses      []model.ClassificationLabel
	OutputClasses     []model.ClassificationLabel
	EvidenceRetention string
	Budget            *agentbudget.Ledger
	Resources         *AgentResourceRuntime
	Model             OpenAIAgentModelGatewayConfig
	// BuildGateway binds an owned Go type when OutputConstraint is structured.
	// For the registered plain reply contract use NewOpenAIAgentModelGateway.
	BuildGateway func(OpenAIAgentModelGatewayConfig) (*AgentModelGateway, error)
	Workload     string
	WorkerID     string
	LeaseTTL     time.Duration
	Now          func() time.Time
}

// NewNativeCommonAgentWorker binds actual native sources, protected artifacts,
// current egress evidence, task budget, resource pools and provider leases.
// It is intended to be called by AgentServedAssemblyInput.WorkerFactory.
func NewNativeCommonAgentWorker(runtime *CommonAgentRuntime, schedules *AgentScheduleService, workflows AgentWorkflowSourceAuthority, cfg NativeCommonAgentWorkerConfig) (*CommonAgentWorker, error) {
	if runtime == nil || schedules == nil || workflows.Core == nil || workflows.ResolveTenant == nil || workflows.Versions == nil || cfg.Core == nil || !required(cfg.CoreSchema) || cfg.TenantUUID == nil || cfg.OutputPolicy == nil || cfg.Budget == nil || cfg.Resources == nil || cfg.Model.LeaseBindings == nil || cfg.Model.Leases != cfg.Model.LeaseBindings.leases || cfg.BuildGateway == nil || cfg.Now == nil || !required(cfg.EvidenceRetention) || len(cfg.InputClasses) == 0 || len(cfg.OutputClasses) == 0 {
		return nil, ErrCommonAgentWorker
	}
	for _, class := range append(append([]model.ClassificationLabel{}, cfg.InputClasses...), cfg.OutputClasses...) {
		if !class.Valid() {
			return nil, ErrCommonAgentWorker
		}
	}
	resolveTenant := func(key string) uuid.UUID { return cfg.TenantUUID(values.TenantId(key)) }
	contextOwner := NativeCommonAgentSource{Core: cfg.Core, CoreSchema: cfg.CoreSchema, ResolveTenant: resolveTenant, Schedule: schedules, Workflow: workflows, Now: cfg.Now, InputClasses: append([]model.ClassificationLabel{}, cfg.InputClasses...)}
	budget, err := NewCommonAgentLedgerBudget(runtime, cfg.Budget)
	if err != nil {
		return nil, err
	}
	work, err := NewDatabaseCommonAgentModelWorkSource(CommonAgentModelWorkConfig{Runtime: runtime, Manifests: cfg.Manifests, Routes: cfg.Routes,
		Contexts: map[agentrun.SourceKind]CommonAgentContextSource{agentrun.SourceSchedule: contextOwner, agentrun.SourceWorkflow: contextOwner}, Outputs: cfg.OutputConstraint,
		Budget: budget, Leases: cfg.Model.LeaseBindings, TenantUUID: cfg.TenantUUID, Workload: cfg.Workload, Now: cfg.Now})
	if err != nil {
		return nil, err
	}
	evidence, err := NewCommonAgentModelEvidence(CommonAgentModelEvidence{Runtime: runtime, Work: work, Core: cfg.Core, CoreSchema: cfg.CoreSchema, RetentionClass: cfg.EvidenceRetention, Now: cfg.Now})
	if err != nil {
		return nil, err
	}
	modelConfig := cfg.Model
	modelConfig.Budget, modelConfig.Sources, modelConfig.Routes, modelConfig.Resources = budget, evidence, evidence, cfg.Resources
	gateway, err := cfg.BuildGateway(modelConfig)
	if err != nil {
		return nil, err
	}
	executor, err := NewAgentModelExecutorAdapter(gateway)
	if err != nil {
		return nil, err
	}
	output := CommonAgentProtectedOutput{Core: cfg.Core, CoreSchema: cfg.CoreSchema, ResolveTenant: resolveTenant, Policy: cfg.OutputPolicy, Current: runtime, Now: cfg.Now,
		OutputClasses: append([]model.ClassificationLabel{}, cfg.OutputClasses...), SourceClasses: append([]model.ClassificationLabel{}, cfg.InputClasses...)}
	source := NativeCommonAgentExecutionSource{Context: contextOwner, Work: work, Output: output}
	return NewCommonAgentWorker(CommonAgentWorkerConfig{Runtime: runtime, Model: executor, Resources: cfg.Resources, WorkerID: cfg.WorkerID, LeaseTTL: cfg.LeaseTTL, Now: cfg.Now,
		Sources: map[agentrun.SourceKind]CommonAgentExecutionSource{agentrun.SourceSchedule: source, agentrun.SourceWorkflow: source}})
}
