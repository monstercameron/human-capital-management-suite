package application

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// CommonAgentSourceContext is a material, classified projection returned by the
// native source owner. Reference IDs alone never become a model prompt.
type CommonAgentSourceContext struct {
	Messages   []agentmodel.ModelMessage     `json:"messages"`
	References []agentmodel.ContextReference `json:"references"`
	Tools      []agentmodel.ToolSchema       `json:"tools"`
}

type CommonAgentContextSource interface {
	ReadCommonAgentSourceContext(context.Context, agentrun.Record, runstate.Run) (CommonAgentSourceContext, error)
}

type CommonAgentManifestReader interface {
	ManifestVersion(context.Context, uuid.UUID, string, uint64) (agentmanifest.Manifest, error)
	ManifestInstructions(context.Context, uuid.UUID, string, uint64, string) (string, error)
}

type CommonAgentToolContextAuthority interface {
	CheckCommonAgentToolSchemas(context.Context, agentrun.Record, runstate.Run, []agentmodel.ToolSchema) error
}

type CommonAgentModelOutputAuthority interface {
	CommonAgentModelOutputConstraint(context.Context, agentrun.Record, runstate.Run, agentmanifest.Reference) (agentmodel.OutputConstraint, error)
}

type CommonAgentModelWorkConfig struct {
	Runtime    *CommonAgentRuntime
	Manifests  CommonAgentManifestReader
	Routes     PersonaModelRoutePolicyReader
	Contexts   map[agentrun.SourceKind]CommonAgentContextSource
	Outputs    CommonAgentModelOutputAuthority
	Budget     *CommonAgentLedgerBudget
	Leases     *ModelLeaseSource
	TenantUUID func(values.TenantId) uuid.UUID
	Workload   string
	Now        func() time.Time
}

type DatabaseCommonAgentModelWorkSource struct{ cfg CommonAgentModelWorkConfig }

func NewDatabaseCommonAgentModelWorkSource(cfg CommonAgentModelWorkConfig) (*DatabaseCommonAgentModelWorkSource, error) {
	if cfg.Runtime == nil || isNilPersonaOutputPort(cfg.Manifests) || isNilPersonaOutputPort(cfg.Routes) || len(cfg.Contexts) == 0 || cfg.Leases == nil || cfg.TenantUUID == nil || !required(cfg.Workload) || cfg.Now == nil {
		return nil, ErrCommonAgentWorker
	}
	contexts := make(map[agentrun.SourceKind]CommonAgentContextSource, len(cfg.Contexts))
	for kind, source := range cfg.Contexts {
		if isNilPersonaOutputPort(source) {
			return nil, ErrCommonAgentWorker
		}
		contexts[kind] = source
	}
	cfg.Contexts = contexts
	return &DatabaseCommonAgentModelWorkSource{cfg: cfg}, nil
}

// BuildCommonAgentModelWork resolves exact retained instructions and current
// routing policy, material source context and a fresh single-use model lease.
func (s *DatabaseCommonAgentModelWorkSource) BuildCommonAgentModelWork(ctx context.Context, record agentrun.Record, run runstate.Run, material CommonAgentSourceContext) (AgentModelExecutorRequest, error) {
	if s == nil || ctx == nil || commonAgentCheckExecutionIdentity(record, run) != nil {
		return AgentModelExecutorRequest{}, ErrCommonAgentWorker
	}
	if err := s.cfg.Runtime.Recheck(ctx, run.TenantID, run.ID); err != nil {
		return AgentModelExecutorRequest{}, err
	}
	manifest, instructions, route, err := s.current(ctx, record, run)
	if err != nil {
		return AgentModelExecutorRequest{}, err
	}
	owner := s.cfg.Contexts[record.Request.Source.Kind]
	if owner == nil {
		return AgentModelExecutorRequest{}, ErrCommonAgentWorker
	}
	current, err := owner.ReadCommonAgentSourceContext(ctx, record, run)
	if err != nil || !commonAgentSameContext(current, material) || !commonAgentSourceContextValid(current) {
		return AgentModelExecutorRequest{}, ErrCommonAgentWorker
	}
	if len(current.Tools) > 0 {
		tools, ok := owner.(CommonAgentToolContextAuthority)
		if !ok || tools.CheckCommonAgentToolSchemas(ctx, record, run, current.Tools) != nil {
			return AgentModelExecutorRequest{}, ErrCommonAgentWorker
		}
	}
	output := agentmodel.OutputConstraint{Mode: agentmodel.OutputText}
	if s.cfg.Outputs != nil {
		output, err = s.cfg.Outputs.CommonAgentModelOutputConstraint(ctx, record, run, manifest.OutputSchema)
		if err != nil {
			return AgentModelExecutorRequest{}, err
		}
	} else if !validPersonaChatReplySchemaPin(manifest.OutputSchema) {
		return AgentModelExecutorRequest{}, ErrCommonAgentWorker
	}
	model := agentmodel.ModelRequest{ContractVersion: agentmodel.ContractVersion, TaskProfile: route.Route.Task.ID, ModelProfile: route.Route.Pin.Primary.ProfileID,
		Messages:    []agentmodel.ModelMessage{{Role: agentmodel.RoleSystem, Content: manifest.Purpose}, {Role: agentmodel.RoleDeveloper, Content: instructions}},
		ContextRefs: slices.Clone(current.References), Tools: slices.Clone(current.Tools), Output: output, Deadline: run.Deadline,
		Limits:  agentmodel.ModelLimits{MaxInputTokens: int64(minUint(record.Request.Budget.MaxInputTokens, manifest.Budget.MaxInputTokens)), MaxOutputTokens: int64(minUint(record.Request.Budget.MaxOutputTokens, manifest.Budget.MaxOutputTokens)), MaxCostMicros: route.Route.Task.MaxCostMicros},
		TraceID: run.ID, Processing: route.Processing}
	model.Messages = append(model.Messages, current.Messages...)
	if s.cfg.Budget != nil {
		remaining, err := s.cfg.Budget.RemainingCommonAgentModelBudget(ctx, record, run)
		if err != nil {
			return AgentModelExecutorRequest{}, err
		}
		model.Limits.MaxInputTokens = minInt64(model.Limits.MaxInputTokens, int64(remaining.MaxInputTokens))
		model.Limits.MaxOutputTokens = minInt64(model.Limits.MaxOutputTokens, int64(remaining.MaxOutputTokens))
		model.Limits.MaxCostMicros = minInt64(model.Limits.MaxCostMicros, int64(remaining.MaxCostMicros))
		route.Route.Task.MaxCostMicros = model.Limits.MaxCostMicros
		route.Route.BudgetRemainingMicros = model.Limits.MaxCostMicros
	}
	model.Deadline = minTime(model.Deadline, s.cfg.Now().UTC().Add(route.Route.Task.MaxLatency))
	if run.Lease != nil {
		model.Deadline = minTime(model.Deadline, run.Lease.Until)
	}
	if !model.Deadline.After(s.cfg.Now().UTC()) || agentmodel.ValidateModelRequest(model) != nil {
		return AgentModelExecutorRequest{}, ErrCommonAgentWorker
	}
	task, err := NewTrustedModelTask(run.TenantID, run.ID, run.AgentDigest, s.cfg.Workload)
	if err != nil {
		return AgentModelExecutorRequest{}, err
	}
	credential, err := s.cfg.Leases.Issue(ctx, ModelLeaseRequest{Task: task, ProviderID: route.Route.Pin.Primary.Identity.ProviderID, Destination: route.Route.Pin.Primary.ProfileID, Purpose: route.Purpose, Region: route.Route.Task.Region, TTL: minDuration(time.Minute, model.Deadline.Sub(s.cfg.Now().UTC()))})
	if err != nil {
		return AgentModelExecutorRequest{}, err
	}
	fields, names, sources := commonAgentModelFields(model, route, run.ID)
	request := AgentModelExecutorRequest{Task: task, StepID: run.ID, ToolResultClass: route.ThreadClass, Route: route.Route, Model: model, Lease: credential, FieldSources: sources,
		Outbound: agentegress.OutboundRequest{TaskID: run.ID, Tenant: run.TenantID, Principal: commonAgentExecutionPrincipal(record.Request), Purpose: route.Purpose, Profile: route.Egress, Region: route.Route.Task.Region, DeclaredFields: names, Fields: fields, Task: route.TaskPolicy, Now: s.cfg.Now().UTC()}}
	if err := commonAgentWorkerRequest(record, run, request); err != nil {
		return AgentModelExecutorRequest{}, err
	}
	return request, nil
}

func (s *DatabaseCommonAgentModelWorkSource) current(ctx context.Context, record agentrun.Record, run runstate.Run) (agentmanifest.Manifest, string, PersonaRunModelRoute, error) {
	tenant := s.cfg.TenantUUID(values.TenantId(run.TenantID))
	version, err := strconv.ParseUint(record.Authority.Agent.Version, 10, 64)
	if err != nil || tenant == uuid.Nil || version == 0 {
		return agentmanifest.Manifest{}, "", PersonaRunModelRoute{}, ErrCommonAgentWorker
	}
	manifest, err := s.cfg.Manifests.ManifestVersion(ctx, tenant, record.Authority.Agent.AgentID, version)
	if err != nil || manifest.ID != run.AgentID || manifest.Version != version || manifest.Purpose != record.Request.Purpose || personaRunManifestDigest(manifest) != run.AgentDigest {
		return agentmanifest.Manifest{}, "", PersonaRunModelRoute{}, ErrCommonAgentWorker
	}
	instructions, err := s.cfg.Manifests.ManifestInstructions(ctx, tenant, manifest.ID, manifest.Version, manifest.InstructionsDigest)
	if err != nil || !required(instructions) || personaRunBytesDigest([]byte(instructions)) != manifest.InstructionsDigest {
		return agentmanifest.Manifest{}, "", PersonaRunModelRoute{}, ErrCommonAgentWorker
	}
	ref := manifest.ModelPolicy
	stored, err := currentPersonaAgentModelRoute(ctx, s.cfg.Routes, tenant, record.Request.LegalEntity, ref, record.Request.Agent.Digest, s.cfg.Now().UTC())
	if err != nil || stored.TenantID != tenant || stored.LegalEntityID != record.Request.LegalEntity || stored.PolicyID != ref.ID || stored.PolicyVersion != int64(ref.Version) || stored.PolicySchemaVersion != int64(ref.SchemaVersion) || stored.PolicyDigest != ref.Digest || stored.Revision <= 0 {
		return agentmanifest.Manifest{}, "", PersonaRunModelRoute{}, ErrCommonAgentWorker
	}
	var route PersonaRunModelRoute
	if json.Unmarshal(stored.RoutePayload, &route) != nil || route.Purpose == "" || route.Route.Pin.Primary.Identity.ProviderID != "openai" || route.Route.Pin.AgentVersionDigest != run.AgentDigest || route.Route.Task.AgentVersionDigest != run.AgentDigest ||
		route.Route.Pin.OutputSchemaDigest != manifest.OutputSchema.Digest || route.Route.Task.OutputSchemaDigest != manifest.OutputSchema.Digest || route.Route.Pin.ToolSchemaDigest == "" || route.Route.Pin.SemanticsDigest == "" ||
		route.Route.Pin.TaskProfileID != route.Route.Task.ID || route.Route.Task.ID == "" || len(route.Route.Pin.Fallbacks) != 0 || route.Route.Task.MaxLatency <= 0 || route.Route.Task.MaxCostMicros <= 0 ||
		route.Egress.ID != route.Route.Pin.Primary.ProfileID || route.Egress.Kind != agentegress.TargetModel || !slices.Contains(route.Egress.AllowedRegions, route.Route.Task.Region) || route.Processing.Residency != route.Route.Task.Region ||
		!slices.Contains(route.Route.Task.DataClasses, string(route.ProfileClass)) || !slices.Contains(route.Route.Task.DataClasses, string(route.ThreadClass)) {
		return agentmanifest.Manifest{}, "", PersonaRunModelRoute{}, ErrCommonAgentWorker
	}
	for _, bound := range []uint64{record.Request.Budget.MaxCostMicros, record.Request.Budget.MaxInputTokens, record.Request.Budget.MaxOutputTokens, manifest.Budget.MaxCostMicros, manifest.Budget.MaxInputTokens, manifest.Budget.MaxOutputTokens} {
		if bound == 0 || bound > math.MaxInt64 {
			return agentmanifest.Manifest{}, "", PersonaRunModelRoute{}, ErrCommonAgentWorker
		}
	}
	route.Route.TraceID = run.ID
	route.Route.Task.MaxCostMicros = minInt64(route.Route.Task.MaxCostMicros, int64(minUint(record.Request.Budget.MaxCostMicros, manifest.Budget.MaxCostMicros)))
	route.Route.BudgetRemainingMicros = route.Route.Task.MaxCostMicros
	route.Route.Task.MaxLatency = minDuration(route.Route.Task.MaxLatency, run.Deadline.Sub(s.cfg.Now().UTC()))
	return manifest, instructions, route, nil
}

func commonAgentSameContext(a, b CommonAgentSourceContext) bool {
	x, err := json.Marshal(a)
	y, otherErr := json.Marshal(b)
	return err == nil && otherErr == nil && string(x) == string(y)
}

func commonAgentSourceContextValid(material CommonAgentSourceContext) bool {
	if len(material.Messages) == 0 || len(material.Messages) > 100 || len(material.References) == 0 || len(material.References) > 100 {
		return false
	}
	size := 0
	for _, message := range material.Messages {
		if message.Role != agentmodel.RoleUser || strings.TrimSpace(message.Content) == "" || message.ToolCallID != "" || message.ToolName != "" || len(message.ToolArguments) != 0 {
			return false
		}
		size += len(message.Content)
	}
	for _, ref := range material.References {
		if !required(ref.ID) || !required(ref.Version) || !personaRunAuthorityDigest(ref.Digest) {
			return false
		}
	}
	return size <= 1<<20
}

func commonAgentModelFields(model agentmodel.ModelRequest, route PersonaRunModelRoute, runID string) ([]agentegress.Field, []string, map[string]string) {
	fields := make([]agentegress.Field, 0, len(model.Messages)+len(model.ContextRefs))
	names := make([]string, 0, cap(fields))
	sources := make(map[string]string, cap(fields))
	for i, message := range model.Messages {
		name := fmt.Sprintf("model.message.%d", i)
		class, source := route.ProfileClass, "common-agent-profile"
		if message.Role == agentmodel.RoleUser {
			class, source = route.ThreadClass, "common-agent-source-context"
		}
		fields = append(fields, agentegress.Field{Name: name, Value: message.Content, Class: class, Provenance: []string{"common-agent-run:" + runID}})
		names, sources[name] = append(names, name), source
	}
	for i, ref := range model.ContextRefs {
		name := fmt.Sprintf("model.context.%d", i)
		value := fmt.Sprintf("%s\x00%s\x00%s", ref.ID, ref.Version, ref.Digest)
		fields = append(fields, agentegress.Field{Name: name, Value: value, Class: route.ThreadClass, Provenance: []string{"common-agent-run:" + runID}})
		names, sources[name] = append(names, name), "common-agent-context-ref"
	}
	return fields, names, sources
}
