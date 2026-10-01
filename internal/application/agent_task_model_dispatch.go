package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

const AgentTaskApprovedInputSource = "agent-task-approved-input"

type AgentTaskModelRequestSourceConfig struct {
	Platform    *agentsystem.Platform
	Deployment  PersonaModelDeployment
	Credentials *ModelLeaseSource
	Workload    string
	LeaseTTL    time.Duration
	Now         func() time.Time
	Audit       agentaudit.Store
}

// AgentTaskModelRequestSource binds SchemaFlux calls to an executing durable
// task and an explicitly qualified deployment profile. It never chooses a
// model, region or source class from the supplied prompt.
type AgentTaskModelRequestSource struct {
	cfg         AgentTaskModelRequestSourceConfig
	mu          sync.RWMutex
	credentials *ModelLeaseSource
}

func NewAgentTaskModelRequestSource(cfg AgentTaskModelRequestSourceConfig) (*AgentTaskModelRequestSource, error) {
	if cfg.Platform == nil || cfg.Audit == nil || cfg.Now == nil || cfg.Workload == "" || cfg.LeaseTTL <= 0 {
		return nil, ErrAgentModelGatewayNotConfigured
	}
	encoded, err := json.Marshal(cfg.Deployment)
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(encoded, &cfg.Deployment) != nil {
		return nil, ErrAgentModelGatewayNotConfigured
	}
	return &AgentTaskModelRequestSource{cfg: cfg, credentials: cfg.Credentials}, nil
}

// BindCredentials closes the gateway construction cycle exactly once.
// A missing binding refuses work before any provider admission or lease.
func (s *AgentTaskModelRequestSource) BindCredentials(credentials *ModelLeaseSource) error {
	if s == nil || credentials == nil {
		return ErrAgentModelGatewayNotConfigured
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.credentials != nil {
		return ErrAgentModelGatewayNotConfigured
	}
	s.credentials = credentials
	return nil
}

func taskModelDigest(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// AgentTaskModelSkillProfileID binds evaluation to the exact immutable skill
// revision as well as its SchemaFlux output schema.
func AgentTaskModelSkillProfileID(pin agentskills.SkillPin, schema json.RawMessage) string {
	encoded, _ := json.Marshal(struct {
		Skill        agentskills.SkillPin
		SchemaDigest string
	}{pin, taskModelDigest(schema)})
	return "agent-task:" + taskModelDigest(encoded)
}

func (s *AgentTaskModelRequestSource) invocation(ctx context.Context, tenant string) (agentsystem.ModelInvocation, error) {
	if s == nil || ctx == nil {
		return agentsystem.ModelInvocation{}, ErrAgentModelGatewayNotConfigured
	}
	runner, err := s.cfg.Platform.ForTenant(ctx, values.TenantId(tenant))
	if err != nil {
		return agentsystem.ModelInvocation{}, err
	}
	return runner.CurrentModelInvocation(ctx)
}

func (s *AgentTaskModelRequestSource) BuildTypedAgentModelRequest(ctx context.Context, req agentmodel.Request, input agentmodel.TypedModelInput) (AgentModelGatewayRequest, error) {
	if s == nil || ctx == nil || input.WebSearch || input.Prompt == "" || !json.Valid(input.Schema) {
		return AgentModelGatewayRequest{}, ErrAgentModelExecutorBinding
	}
	s.mu.RLock()
	credentials := s.credentials
	s.mu.RUnlock()
	if credentials == nil {
		return AgentModelGatewayRequest{}, ErrAgentModelGatewayNotConfigured
	}
	runner, err := s.cfg.Platform.ForTenant(ctx, values.TenantId(req.TenantID))
	if err != nil {
		return AgentModelGatewayRequest{}, err
	}
	authority, err := runner.ResolveModelInvocation(ctx, req)
	if err != nil {
		return AgentModelGatewayRequest{}, err
	}
	// Composite payloads require one explicitly declared class; adding a new
	// projection needs an owner-reviewed classifier rather than a guessed rank.
	if len(req.DataClasses) != 1 || !trustdlp.DataClass(req.DataClasses[0]).Valid() {
		return AgentModelGatewayRequest{}, ErrAgentModelGatewayPin
	}
	class := trustdlp.DataClass(req.DataClasses[0])
	profileID := AgentTaskModelSkillProfileID(req.Skill, input.Schema)
	schemaDigest := taskModelDigest(input.Schema)
	var selected agentmodel.ModelProfile
	for _, p := range s.cfg.Deployment.Profiles {
		if p.Evaluation.Passed && p.Evaluation.AgentVersionDigest == authority.Grant.AgentVersion && p.OutputSchemaDigest == schemaDigest && slices.Contains(p.TaskProfileIDs, profileID) && slices.Contains(p.DataClasses, string(class)) {
			if selected.ID != "" {
				return AgentModelGatewayRequest{}, ErrAgentModelGatewayPin
			}
			selected = p
		}
	}
	if selected.ID == "" {
		return AgentModelGatewayRequest{}, ErrAgentModelGatewayPin
	}
	var terms agentegress.ProviderTerms
	for _, t := range s.cfg.Deployment.Terms {
		if t.ModelProfile == selected.ID {
			terms = t
		}
	}
	region := ""
	for _, scope := range s.cfg.Deployment.Credential.Scopes {
		if scope.TenantID == req.TenantID && scope.Purpose == req.Purpose && scope.Destination == selected.ID && slices.Contains(selected.Regions, scope.Region) && slices.Contains(terms.AllowedRegions, scope.Region) {
			if region != "" && region != scope.Region {
				return AgentModelGatewayRequest{}, ErrAgentModelGatewayPin
			}
			region = scope.Region
		}
	}
	if region == "" || !terms.Approved || !slices.Contains(terms.AllowedClasses, class) {
		return AgentModelGatewayRequest{}, ErrAgentModelGatewayPin
	}
	deadline := minTime(authority.Task.ExpiresAt, authority.Grant.ExpiresAt)
	latency := minDuration(selected.MaxLatency, deadline.Sub(s.cfg.Now()))
	ceiling := minInt64(selected.MaxCostMicros, req.Estimate.SpendMicros)
	if latency <= 0 || ceiling <= 0 || req.Estimate.Tokens < 2 {
		return AgentModelGatewayRequest{}, ErrAgentModelGatewayPin
	}
	deadline = minTime(deadline, s.cfg.Now().Add(latency))
	outputTokens := max(int64(1), req.Estimate.Tokens/4)
	selection := agentmodel.ModelSelection{ProfileID: selected.ID, ProfileDigest: selected.ProfileDigest, Identity: selected.Identity}
	trace := fmt.Sprintf("%s:%s:attempt-%d", authority.Task.ID, req.Actor.StepID, authority.Step.Attempt)
	route := agentmodel.RouteRequest{TraceID: trace, Pin: agentmodel.ModelPin{AgentVersionDigest: authority.Grant.AgentVersion, TaskProfileID: profileID, Primary: selection, SemanticsDigest: selected.SemanticsDigest, OutputSchemaDigest: schemaDigest, ToolSchemaDigest: selected.ToolSchemaDigest}, Task: agentmodel.TaskProfile{ID: profileID, AgentVersionDigest: authority.Grant.AgentVersion, Region: region, DataClasses: []string{string(class)}, MaxLatency: latency, MaxCostMicros: ceiling, SemanticsDigest: selected.SemanticsDigest, OutputSchemaDigest: schemaDigest, ToolSchemaDigest: selected.ToolSchemaDigest}, BudgetRemainingMicros: ceiling}
	model := agentmodel.ModelRequest{ContractVersion: agentmodel.ContractVersion, TaskProfile: profileID, ModelProfile: selected.ID, TraceID: trace, Messages: []agentmodel.ModelMessage{{Role: agentmodel.RoleUser, Content: input.Prompt}}, Output: agentmodel.OutputConstraint{Mode: agentmodel.OutputSchema, Schema: append(json.RawMessage(nil), input.Schema...)}, Deadline: deadline, Limits: agentmodel.ModelLimits{MaxInputTokens: req.Estimate.Tokens - outputTokens, MaxOutputTokens: outputTokens, MaxCostMicros: ceiling}, Processing: agentmodel.ProcessingPolicy{Residency: region, Retention: fmt.Sprintf("%s:%d", terms.Retention.Mode, int64(terms.Retention.MaxAge)), TrainingUse: terms.TrainingUse, Logging: terms.Logging}}
	task, err := NewTrustedModelTask(req.TenantID, authority.Task.ID, authority.Grant.AgentVersion, s.cfg.Workload)
	if err != nil {
		return AgentModelGatewayRequest{}, err
	}
	credential, err := credentials.Issue(ctx, ModelLeaseRequest{Task: task, ProviderID: selected.Identity.ProviderID, Destination: selected.ID, Purpose: req.Purpose, Region: region, TTL: minDuration(s.cfg.LeaseTTL, deadline.Sub(s.cfg.Now()))})
	if err != nil {
		return AgentModelGatewayRequest{}, err
	}
	// Public input uses the operator-configured provider retention ceiling. Other classes require zero provider retention.
	var retentionCeiling time.Duration
	if class == trustdlp.ClassPublic && s.cfg.Deployment.PublicTaskRetentionSeconds > 0 {
		retentionCeiling = time.Duration(s.cfg.Deployment.PublicTaskRetentionSeconds) * time.Second
	}
	field := agentegress.Field{Name: "model.message.0", Value: input.Prompt, Class: class, Taint: []string{"AGENT_DERIVED"}, Provenance: []string{"agent-task:" + authority.Task.ID, "plan:" + authority.Task.Plan.Digest, "skill:" + req.Skill.Digest}}
	return AgentModelGatewayRequest{TenantID: req.TenantID, Route: route, Dispatch: agentegress.ProviderDispatchRequest{Model: model, Lease: credential, FieldSources: map[string]string{field.Name: AgentTaskApprovedInputSource}, Outbound: agentegress.OutboundRequest{TaskID: authority.Task.ID, Tenant: req.TenantID, Principal: authority.Task.UserID, Purpose: req.Purpose, Profile: agentegress.Profile{ID: selected.ID, Kind: agentegress.TargetModel, AllowedRegions: terms.AllowedRegions, AllowedClasses: terms.AllowedClasses, Retention: terms.Retention}, Region: region, DeclaredFields: []string{field.Name}, Fields: []agentegress.Field{field}, Task: agentegress.TaskPolicy{AllowedRegions: terms.AllowedRegions, AllowedResultClasses: terms.AllowedClasses, MaxExternalRetention: retentionCeiling, ResultRetention: time.Hour}, Now: s.cfg.Now().UTC()}}}, nil
}

func (s *AgentTaskModelRequestSource) VerifySourceClassification(ctx context.Context, source agentegress.SourceClassificationRequest) error {
	if s == nil || ctx == nil {
		return ErrAgentModelGatewayNotConfigured
	}
	built, ok := ctx.Value(agentTypedModelEvidenceContextKey{}).(AgentModelGatewayRequest)
	if !ok || source.Tenant != built.TenantID || source.Purpose != built.Dispatch.Outbound.Purpose {
		return ErrAgentModelGatewayTenant
	}
	authority, err := s.invocation(ctx, source.Tenant)
	if err != nil {
		return err
	}
	if built.Dispatch.Outbound.TaskID != authority.Task.ID || built.Dispatch.Outbound.Principal != authority.Task.UserID || built.Route.Pin.AgentVersionDigest != authority.Grant.AgentVersion {
		return ErrAgentModelGatewayPin
	}
	for _, field := range built.Dispatch.Outbound.Fields {
		value, ok := field.Value.(string)
		if field.Name == source.FieldName && ok && source.SourceClass == built.Dispatch.FieldSources[field.Name] && source.DataClass == field.Class && slices.Equal(source.Provenance, field.Provenance) && source.ValueDigest == taskModelDigest([]byte(value)) {
			return nil
		}
	}
	return ErrAgentModelGatewayPin
}

func (s *AgentTaskModelRequestSource) RecordRoute(ctx context.Context, route agentmodel.RouteRecord) error {
	if s == nil || ctx == nil {
		return ErrAgentModelGatewayNotConfigured
	}
	built, ok := ctx.Value(agentTypedModelEvidenceContextKey{}).(AgentModelGatewayRequest)
	if !ok || route.TraceID != built.Route.TraceID || route.AgentVersionDigest != built.Route.Pin.AgentVersionDigest || route.TaskProfileID != built.Route.Task.ID || route.Region != built.Route.Task.Region || route.Fallback || (route.Selected != (agentmodel.ModelSelection{}) && route.Selected != built.Route.Pin.Primary) || route.Digest == "" {
		return ErrAgentModelGatewayPin
	}
	authority, err := s.invocation(ctx, built.TenantID)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(route)
	if err != nil {
		return err
	}
	eventID := "agent-task-model-route:" + route.TraceID + ":" + route.Digest
	_, err = s.cfg.Audit.Append(ctx, agentaudit.Entry{EventID: eventID, TenantID: built.TenantID, Kind: agentaudit.EventModelCall, Actor: authority.Actor, Action: "model.route", ResultDigest: route.Digest, Fields: []agentaudit.Field{{Name: "model_route", Value: string(encoded), Classification: agentaudit.ClassificationInternal}}, Edges: []agentaudit.Edge{{Kind: agentaudit.EdgeTask, From: authority.Task.ID, To: eventID}}, OccurredAt: s.cfg.Now().UTC()})
	return err
}
