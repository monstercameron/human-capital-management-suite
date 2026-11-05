package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// The ambient screen task is qualified like the writing-style task: a
// deployment-owned profile must name it, carry passing evaluation evidence
// for the pinned screen instructions, and declare exactly these semantics and
// provider purpose. A deployment that does not is not eligible and the offers
// planner stays absent with a stated reason; nothing here widens a
// deployment's approval (AGENTUX-079).
const (
	// AgentUX079AmbientScreenTaskProfileID is the governed task the screen
	// model is qualified for: reading one quarantined chat message and
	// proposing a task or reminder card for it.
	AgentUX079AmbientScreenTaskProfileID = "agentux-ambient-screen-v1"
	// AgentUX079AmbientScreenPurpose is the provider purpose the credential
	// scope, destination policy and clearance of a qualified deployment must
	// carry.
	AgentUX079AmbientScreenPurpose            = "ambient.screen"
	agentUX079AmbientScreenSemantics          = "agentux-ambient-screen-v1"
	agentUX079AmbientScreenOutputSchema       = "agentux-ambient-screen-proposal-v1"
	agentUX079AmbientScreenSourceInstructions = "ambient-screen-instructions"
	agentUX079AmbientScreenSourceMessage      = "ambient-screen-message"
	agentUX079AmbientScreenToolSchemaDigest   = "none"
	agentUX079AmbientScreenRequestTimeout     = 25 * time.Second
	agentUX079AmbientScreenMaxInputTokens     = int64(6000)
	agentUX079AmbientScreenMaxOutputTokens    = int64(2000)
	// agentUX079AmbientScreenDailyModelCalls is the per-tenant, per-day
	// ceiling of the budget task: one screened message is one model call.
	agentUX079AmbientScreenDailyModelCalls = 500
	agentUX079AmbientScreenActor           = "ambient-screen@1"
)

// EnvAgentUX079AmbientScreenModelConfigFile names the approval document that
// qualifies a model for the ambient screen task (the same document shape as
// the persona, private-task and writing-style deployments).
const EnvAgentUX079AmbientScreenModelConfigFile = "HCMNEXT_AMBIENT_SCREEN_MODEL_CONFIG_FILE"

// localAgentUX079AmbientScreenDeploymentPath is read, when present, by the
// local development profile only, beside the other deployment documents.
const localAgentUX079AmbientScreenDeploymentPath = ".artifacts/lanes/agent-dev/ambient-screen-deployment.json"

// AgentUX079AmbientScreenTaskProfile is what a deployment must declare for
// the screen task. It exists so the requirement is stated in one place and
// tested, not rediscovered from an eligibility refusal.
func AgentUX079AmbientScreenTaskProfile() agentmodel.TaskProfile {
	return agentmodel.TaskProfile{ID: AgentUX079AmbientScreenTaskProfileID, MaxLatency: 10 * time.Second, MaxCostMicros: 20000, DataClasses: []string{string(trustdlp.ClassPublic), string(trustdlp.ClassInternal)}, SemanticsDigest: agentUX079AmbientScreenSemantics}
}

// AgentUX079AmbientScreenAgentVersionDigest pins the screen instructions the
// model was evaluated against: Task Catcher's and Reminder's exact prompts. A
// deployment evaluated against other instructions does not qualify.
func AgentUX079AmbientScreenAgentVersionDigest() string {
	sum := sha256.Sum256([]byte(AgentUXAmbientInstructions("task-catcher") + "\x00" + AgentUXAmbientInstructions("reminder")))
	return "hcm-ambient-screen/" + hex.EncodeToString(sum[:])[:16]
}

// AgentUX079AmbientScreenEvidenceProfile is the task, output and tool contract
// a qualified profile declares.
func AgentUX079AmbientScreenEvidenceProfile() (task agentmodel.TaskProfile, outputDigest, toolDigest string) {
	return AgentUX079AmbientScreenTaskProfile(), agentUX079AmbientScreenOutputSchema, agentUX079AmbientScreenToolSchemaDigest
}

var errAgentUX079AmbientScreenBinding = errors.New("application: ambient screen model binding is invalid")

// AgentUX079AmbientScreenBinding is the production screen planner's binding.
// It resolves, for one screened message: the qualified model profile, a
// task-bound budget (one ledger task per tenant per day, so the daily call
// ceiling and the platform's tenant-month ceilings all apply), a fresh route
// request pinned to the evaluated profile, the provider's processing terms as
// the outbound request, and a single-use credential lease. The prompt itself
// is supplied by the ambient adapter's quarantined envelope, never rebuilt
// here.
type AgentUX079AmbientScreenBinding struct {
	Profile  agentmodel.ModelProfile
	Terms    agentegress.ProviderTerms
	Leases   *ModelLeaseSource
	Workload string
	Ledger   *agentbudget.Ledger
	Region   string
	Limits   agentmodel.ModelLimits
	Now      func() time.Time
}

// NewAgentUX079AmbientScreenBinding selects the one profile a deployment
// qualified for the screen task and refuses anything that does not match the
// task's declared semantics, purpose, latency and cost.
func NewAgentUX079AmbientScreenBinding(dep PersonaModelDeployment, leases *ModelLeaseSource, ledger *agentbudget.Ledger, now func() time.Time) (*AgentUX079AmbientScreenBinding, error) {
	if leases == nil || ledger == nil || now == nil {
		return nil, fmt.Errorf("%w: leases, budget ledger and clock are required", errAgentUX079AmbientScreenBinding)
	}
	q, err := agentUX079AmbientScreenQualify(dep)
	if err != nil {
		return nil, err
	}
	return &AgentUX079AmbientScreenBinding{Profile: q.profile, Terms: q.terms, Leases: leases, Workload: dep.Worker.Workload, Ledger: ledger, Region: q.region, Limits: q.limits, Now: now}, nil
}

// agentUX079AmbientScreenQualified is the outcome of checking a deployment
// document against the screen task: the one profile and terms, region and
// call limits.
type agentUX079AmbientScreenQualified struct {
	profile agentmodel.ModelProfile
	terms   agentegress.ProviderTerms
	region  string
	limits  agentmodel.ModelLimits
}

func agentUX079AmbientScreenQualify(dep PersonaModelDeployment) (agentUX079AmbientScreenQualified, error) {
	task, outputDigest, toolDigest := AgentUX079AmbientScreenEvidenceProfile()
	var profile *agentmodel.ModelProfile
	for i := range dep.Profiles {
		if slices.Contains(dep.Profiles[i].TaskProfileIDs, task.ID) {
			if profile != nil {
				return agentUX079AmbientScreenQualified{}, fmt.Errorf("%w: more than one profile is qualified for %s", errAgentUX079AmbientScreenBinding, task.ID)
			}
			profile = &dep.Profiles[i]
		}
	}
	if profile == nil {
		return agentUX079AmbientScreenQualified{}, fmt.Errorf("%w: no model profile is qualified for %s", errAgentUX079AmbientScreenBinding, task.ID)
	}
	if !profile.Evaluation.Passed || strings.TrimSpace(profile.Evaluation.AgentVersionDigest) == "" || strings.TrimSpace(profile.Evaluation.SuiteDigest) == "" ||
		profile.Evaluation.AgentVersionDigest != AgentUX079AmbientScreenAgentVersionDigest() || profile.SemanticsDigest != task.SemanticsDigest || profile.OutputSchemaDigest != outputDigest || profile.ToolSchemaDigest != toolDigest {
		return agentUX079AmbientScreenQualified{}, fmt.Errorf("%w: profile evidence or semantics differ from the screen task", errAgentUX079AmbientScreenBinding)
	}
	if profile.MaxLatency <= 0 || profile.MaxLatency > time.Minute || profile.MaxCostMicros <= 0 || profile.MaxCostMicros > task.MaxCostMicros {
		return agentUX079AmbientScreenQualified{}, fmt.Errorf("%w: profile latency or cost is outside the task's bounds", errAgentUX079AmbientScreenBinding)
	}
	for _, class := range task.DataClasses {
		if !slices.Contains(profile.DataClasses, class) {
			return agentUX079AmbientScreenQualified{}, fmt.Errorf("%w: profile does not cover data class %s", errAgentUX079AmbientScreenBinding, class)
		}
	}
	var terms *agentegress.ProviderTerms
	for i := range dep.Terms {
		t := dep.Terms[i]
		if t.ModelProfile == profile.ID && t.ProviderID == profile.Identity.ProviderID && t.ModelID == profile.Identity.ModelID && t.ModelVersion == profile.Identity.Version {
			terms = &dep.Terms[i]
		}
	}
	if terms == nil || !terms.Approved {
		return agentUX079AmbientScreenQualified{}, fmt.Errorf("%w: no approved provider terms for the profile", errAgentUX079AmbientScreenBinding)
	}
	sources := map[string]bool{}
	for _, rule := range terms.SourceRules {
		sources[rule.Class] = true
	}
	if !sources[agentUX079AmbientScreenSourceInstructions] || !sources[agentUX079AmbientScreenSourceMessage] {
		return agentUX079AmbientScreenQualified{}, fmt.Errorf("%w: provider terms have no source rules for the screen fields", errAgentUX079AmbientScreenBinding)
	}
	purposeOK := false
	for _, d := range dep.Destinations {
		if d.Name == profile.ID && slices.Contains(d.Purposes, AgentUX079AmbientScreenPurpose) {
			purposeOK = true
		}
	}
	if !purposeOK {
		return agentUX079AmbientScreenQualified{}, fmt.Errorf("%w: no outbound destination allows purpose %s", errAgentUX079AmbientScreenBinding, AgentUX079AmbientScreenPurpose)
	}
	region := ""
	for _, r := range profile.Regions {
		if slices.Contains(terms.AllowedRegions, r) {
			region = r
			break
		}
	}
	if region == "" {
		return agentUX079AmbientScreenQualified{}, fmt.Errorf("%w: profile and terms share no processing region", errAgentUX079AmbientScreenBinding)
	}
	pricing, err := agentmodel.NewPricingSchedule(dep.Pricing)
	if err != nil {
		return agentUX079AmbientScreenQualified{}, fmt.Errorf("%w: pricing: %v", errAgentUX079AmbientScreenBinding, err)
	}
	selection := agentmodel.ModelSelection{ProfileID: profile.ID, ProfileDigest: profile.ProfileDigest, Identity: profile.Identity}
	limits := agentmodel.ModelLimits{MaxInputTokens: agentUX079AmbientScreenMaxInputTokens, MaxOutputTokens: agentUX079AmbientScreenMaxOutputTokens, MaxCostMicros: profile.MaxCostMicros}
	// The worst-case bill of one call must fit the profile's cost bound; shrink
	// the output ceiling until it does rather than refuse a cheaper model.
	for limits.MaxOutputTokens > 500 && pricing.Validate(context.Background(), selection, limits) != nil {
		limits.MaxOutputTokens -= 250
	}
	if err := pricing.Validate(context.Background(), selection, limits); err != nil {
		return agentUX079AmbientScreenQualified{}, fmt.Errorf("%w: a call cannot fit the profile's cost bound: %v", errAgentUX079AmbientScreenBinding, err)
	}
	return agentUX079AmbientScreenQualified{profile: *profile, terms: *terms, region: region, limits: limits}, nil
}

// Ready reports that a qualified profile is bound.
func (b *AgentUX079AmbientScreenBinding) Ready() bool {
	return b != nil && b.Leases != nil && b.Ledger != nil
}

func (b *AgentUX079AmbientScreenBinding) dailyLimits() agentbudget.Limits {
	return agentbudget.Limits{
		Steps:       agentUX079AmbientScreenDailyModelCalls,
		Tokens:      agentUX079AmbientScreenDailyModelCalls * (b.Limits.MaxInputTokens + b.Limits.MaxOutputTokens),
		SpendMicros: agentUX079AmbientScreenDailyModelCalls * b.Profile.MaxCostMicros,
		WallClock:   agentUX079AmbientScreenDailyModelCalls * agentUX079AmbientScreenRequestTimeout,
	}
}

// AgentUX079AmbientScreenBudgetTaskID names the ledger task one tenant's
// screened messages on one UTC day are charged to.
func AgentUX079AmbientScreenBudgetTaskID(tenant string, at time.Time) string {
	return "ambient-screen:" + tenant + ":" + at.UTC().Format("20060102")
}

// BindScreenMessage resolves the governed call for one screened message: the
// tenant-day budget task, a fresh route pinned to the evaluated profile, and
// a single-use lease. The two model messages stay unset: the planner attaches
// the adapter's quarantined envelope verbatim.
func (b *AgentUX079AmbientScreenBinding) BindScreenMessage(ctx context.Context, source AgentUXAmbientRunSource, agent string, task agentmodel.TaskProfile) (AgentModelGatewayRequest, error) {
	if !b.Ready() || ctx == nil || source.Tenant == "" || source.Conversation == "" || source.Post == "" || AgentUXAmbientInstructions(agent) == "" || task.ID != AgentUX079AmbientScreenTaskProfileID {
		return AgentModelGatewayRequest{}, ErrAgentUXAmbientDenied
	}
	if err := ctx.Err(); err != nil {
		return AgentModelGatewayRequest{}, err
	}
	now := b.Now().UTC()
	taskID := AgentUX079AmbientScreenBudgetTaskID(source.Tenant, now)
	if err := b.Ledger.OpenTask(agentbudget.TaskSpec{ID: taskID, TenantID: source.Tenant, UserID: agent, Limit: b.dailyLimits()}); err != nil && !errors.Is(err, agentbudget.ErrTaskExists) {
		return AgentModelGatewayRequest{}, fmt.Errorf("%w: %v", ErrAgentUXAmbientDenied, err)
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return AgentModelGatewayRequest{}, ErrAgentUXAmbientDenied
	}
	trace := "ambient-screen-" + hex.EncodeToString(nonce[:])
	profile, terms := b.Profile, b.Terms
	version := profile.Evaluation.AgentVersionDigest
	selection := agentmodel.ModelSelection{ProfileID: profile.ID, ProfileDigest: profile.ProfileDigest, Identity: profile.Identity}
	classes := []trustdlp.DataClass{trustdlp.ClassPublic, trustdlp.ClassInternal}
	deadline := now.Add(minDuration(profile.MaxLatency, agentUX079AmbientScreenRequestTimeout))
	outputDigest := agentUX079AmbientScreenOutputSchema
	route := agentmodel.RouteRequest{TraceID: trace,
		Pin: agentmodel.ModelPin{AgentVersionDigest: version, TaskProfileID: task.ID, Primary: selection, SemanticsDigest: task.SemanticsDigest, OutputSchemaDigest: outputDigest, ToolSchemaDigest: agentUX079AmbientScreenToolSchemaDigest},
		Task: agentmodel.TaskProfile{ID: task.ID, AgentVersionDigest: version, Region: b.Region, DataClasses: slices.Clone(task.DataClasses), MaxLatency: profile.MaxLatency, MaxCostMicros: profile.MaxCostMicros,
			SemanticsDigest: task.SemanticsDigest, OutputSchemaDigest: outputDigest, ToolSchemaDigest: agentUX079AmbientScreenToolSchemaDigest},
		BudgetRemainingMicros: profile.MaxCostMicros}
	retention := fmt.Sprintf("%s:%d", terms.Retention.Mode, int64(terms.Retention.MaxAge))
	model := agentmodel.ModelRequest{ContractVersion: agentmodel.ContractVersion, TaskProfile: task.ID, ModelProfile: profile.ID,
		Output: agentmodel.OutputConstraint{Mode: agentmodel.OutputJSON}, RequiredFeatures: []agentmodel.ModelFeature{agentmodel.FeatureStructuredJSON}, Deadline: deadline, Limits: b.Limits, TraceID: trace,
		Processing: agentmodel.ProcessingPolicy{Residency: b.Region, Retention: retention, TrainingUse: terms.TrainingUse, Logging: terms.Logging}}
	trusted, err := NewTrustedModelTask(source.Tenant, taskID, version, b.Workload)
	if err != nil {
		return AgentModelGatewayRequest{}, err
	}
	credential, err := b.Leases.Issue(ctx, ModelLeaseRequest{Task: trusted, ProviderID: profile.Identity.ProviderID, Destination: profile.ID, Purpose: AgentUX079AmbientScreenPurpose, Region: b.Region, TTL: minDuration(time.Minute, deadline.Sub(now))})
	if err != nil {
		return AgentModelGatewayRequest{}, err
	}
	egress := agentegress.Profile{ID: profile.ID, Kind: agentegress.TargetModel, AllowedRegions: slices.Clone(terms.AllowedRegions), AllowedClasses: slices.Clone(terms.AllowedClasses), Retention: terms.Retention}
	// The egress receipt for a call is kept as long as the provider may keep the
	// payload, and for a day when the provider keeps nothing.
	receiptRetention := terms.Retention.MaxAge
	if receiptRetention <= 0 {
		receiptRetention = 24 * time.Hour
	}
	return AgentModelGatewayRequest{TenantID: source.Tenant, Route: route, Dispatch: agentegress.ProviderDispatchRequest{Model: model, Lease: credential,
		Outbound: agentegress.OutboundRequest{TaskID: taskID, Tenant: source.Tenant, Principal: agent, Purpose: AgentUX079AmbientScreenPurpose, Profile: egress, Region: b.Region,
			Task: agentegress.TaskPolicy{AllowedRegions: []string{b.Region}, AllowedResultClasses: classes, MaxExternalRetention: terms.Retention.MaxAge, ResultRetention: receiptRetention}, Now: now}}}, nil
}

type agentUX079AmbientScreenEvidenceKey struct{}

// AgentUX079AmbientScreenEvidence verifies source classifications against the
// request the screen planner built and records the routing decision in the
// append-only audit chain. Both read the built request from the context, so
// neither can be satisfied by a request that was not built by the planner for
// this message, this agent and these exact bytes.
type AgentUX079AmbientScreenEvidence struct {
	Audit interface {
		Append(context.Context, agentaudit.Entry) (agentaudit.Record, error)
	}
	Now func() time.Time
}

func agentUX079AmbientScreenDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func agentUX079AmbientScreenBuilt(ctx context.Context) (AgentModelGatewayRequest, bool) {
	built, ok := ctx.Value(agentUX079AmbientScreenEvidenceKey{}).(AgentModelGatewayRequest)
	return built, ok && built.TenantID != "" && built.Route.TraceID != ""
}

func (e *AgentUX079AmbientScreenEvidence) VerifySourceClassification(ctx context.Context, source agentegress.SourceClassificationRequest) error {
	if e == nil || ctx == nil {
		return ErrAgentModelGatewayNotConfigured
	}
	built, ok := agentUX079AmbientScreenBuilt(ctx)
	if !ok || source.Tenant != built.TenantID || source.Purpose != AgentUX079AmbientScreenPurpose || built.Dispatch.Outbound.Purpose != AgentUX079AmbientScreenPurpose {
		return ErrAgentModelGatewayTenant
	}
	for _, field := range built.Dispatch.Outbound.Fields {
		value, isText := field.Value.(string)
		if field.Name != source.FieldName || !isText {
			continue
		}
		wantRole := agentmodel.RoleUser
		if built.Dispatch.FieldSources[field.Name] == agentUX079AmbientScreenSourceInstructions {
			wantRole = agentmodel.RoleDeveloper
		}
		if source.SourceClass == built.Dispatch.FieldSources[field.Name] && (source.SourceClass == agentUX079AmbientScreenSourceInstructions || source.SourceClass == agentUX079AmbientScreenSourceMessage) &&
			source.DataClass == field.Class && slices.Equal(source.Provenance, field.Provenance) && slices.Contains(source.Provenance, "ambient-screen:"+built.Route.TraceID) &&
			source.ValueDigest == agentUX079AmbientScreenDigest(value) && source.MessageRole == wantRole {
			return nil
		}
	}
	return ErrAgentModelGatewayPin
}

func (e *AgentUX079AmbientScreenEvidence) RecordRoute(ctx context.Context, route agentmodel.RouteRecord) error {
	if e == nil || ctx == nil || e.Audit == nil {
		return ErrAgentModelGatewayNotConfigured
	}
	built, ok := agentUX079AmbientScreenBuilt(ctx)
	if !ok || route.TraceID != built.Route.TraceID || route.AgentVersionDigest != built.Route.Pin.AgentVersionDigest || route.TaskProfileID != built.Route.Task.ID ||
		route.Region != built.Route.Task.Region || route.Fallback || (route.Selected != (agentmodel.ModelSelection{}) && route.Selected != built.Route.Pin.Primary) || route.Digest == "" {
		return ErrAgentModelGatewayPin
	}
	encoded, err := json.Marshal(route)
	if err != nil {
		return err
	}
	at := time.Now().UTC()
	if e.Now != nil {
		at = e.Now().UTC()
	}
	eventID := "ambient-screen-route:" + route.TraceID + ":" + route.Digest
	taskID, agent := built.Dispatch.Outbound.TaskID, built.Dispatch.Outbound.Principal
	// No person pressed a control for a screened message: the ambient agent is
	// the actor and the tenant-day screen task is the grant.
	_, err = e.Audit.Append(ctx, agentaudit.Entry{EventID: eventID, TenantID: built.TenantID, Kind: agentaudit.EventModelCall,
		Actor:  agentaudit.ActorChain{UserID: agent, AgentVersion: agentUX079AmbientScreenActor, InstallationID: "workspace:" + built.TenantID, TaskID: taskID, PlanRevision: built.Route.Pin.SemanticsDigest, StepID: route.TraceID, DelegationGrantID: "screen-request:" + agent},
		Action: "model.route", ResultDigest: route.Digest, Fields: []agentaudit.Field{{Name: "model_route", Value: string(encoded), Classification: agentaudit.ClassificationInternal}},
		Edges: []agentaudit.Edge{{Kind: agentaudit.EdgeTask, From: taskID, To: eventID}}, OccurredAt: at})
	return err
}

var (
	_ agentegress.SourceClassificationVerifier = (*AgentUX079AmbientScreenEvidence)(nil)
	_ agentmodel.RouteRecorder                 = (*AgentUX079AmbientScreenEvidence)(nil)
)

// AgentUX079AmbientScreenGateway dispatches one governed screen call. It is
// satisfied by the composed model gateway; tests substitute a fixture.
type AgentUX079AmbientScreenGateway interface {
	Dispatch(context.Context, AgentModelGatewayRequest) (AgentModelGatewayResult, error)
}

// AgentUX079AmbientScreenPlanner runs the ambient adapter's quarantined
// envelope through the governed gateway. It never rebuilds the prompt: the
// two model messages are forwarded verbatim with no tools, and the model's
// proposal bytes are returned for the adapter's strict decoder.
type AgentUX079AmbientScreenPlanner struct {
	Gateway AgentUX079AmbientScreenGateway
	Binding *AgentUX079AmbientScreenBinding
}

// PlanAmbientMessage screens one message. The step key is the adapter's
// immutable identity for this message, agent and revision; it is required but
// the call itself carries a fresh trace, so a replanned message never
// collides with an earlier routing record.
func (p AgentUX079AmbientScreenPlanner) PlanAmbientMessage(ctx context.Context, source AgentUXAmbientRunSource, agent, key string, messages []agentmodel.ModelMessage) ([]byte, error) {
	if p.Gateway == nil || p.Binding == nil || !p.Binding.Ready() || ctx == nil || key == "" || len(messages) != 2 {
		return nil, ErrAgentUXAmbientDenied
	}
	req, err := p.Binding.BindScreenMessage(ctx, source, agent, AgentUX079AmbientScreenTaskProfile())
	if err != nil {
		return nil, err
	}
	if req.TenantID != source.Tenant || req.Dispatch.Outbound.Tenant != source.Tenant || req.Dispatch.Outbound.Principal != agent || req.Route.Task.ID != AgentUX079AmbientScreenTaskProfileID || req.Route.Pin.TaskProfileID != AgentUX079AmbientScreenTaskProfileID {
		return nil, ErrAgentUXAmbientDenied
	}
	// The typed result is the call's contract whatever the binding built.
	req.Dispatch.Model.Output = agentmodel.OutputConstraint{Mode: agentmodel.OutputJSON}
	req.Dispatch.Model.RequiredFeatures = []agentmodel.ModelFeature{agentmodel.FeatureStructuredJSON}
	req.Dispatch.Model.TaskProfile = AgentUX079AmbientScreenTaskProfileID
	req.Dispatch.Model.Messages = messages
	req.Dispatch.Model.Tools = nil
	// Every message must be present verbatim in the classified outbound field
	// set, with a source class the provider terms approve, or egress refuses it.
	provenance := []string{"ambient-screen:" + req.Route.TraceID}
	// Both fields are human-written: the instructions carry the developers'
	// screen prompts and the message is the author's quarantined text.
	taint := []string{string(agentsecurity.TaintHuman)}
	req.Dispatch.Outbound.DeclaredFields = []string{"model.message.0", "model.message.1"}
	req.Dispatch.Outbound.Fields = []agentegress.Field{
		{Name: "model.message.0", Value: messages[0].Content, Class: trustdlp.ClassInternal, Taint: taint, Provenance: provenance},
		{Name: "model.message.1", Value: messages[1].Content, Class: trustdlp.ClassInternal, Taint: taint, Provenance: provenance},
	}
	req.Dispatch.FieldSources = map[string]string{"model.message.0": agentUX079AmbientScreenSourceInstructions, "model.message.1": agentUX079AmbientScreenSourceMessage}
	result, err := p.Gateway.Dispatch(context.WithValue(ctx, agentUX079AmbientScreenEvidenceKey{}, req), req)
	if err != nil {
		return nil, err
	}
	model := result.Dispatch.Model
	if model.Failure != nil || model.Refusal != nil || model.Finish != agentmodel.FinishComplete || len(model.ToolProposals) != 0 || len(model.RequestedActions) != 0 || len(model.Structured) == 0 {
		return nil, ErrAgentUXAmbientInvalid
	}
	return []byte(model.Structured), nil
}

var _ AgentUXAmbientRunPlanner = AgentUX079AmbientScreenPlanner{}

// NewAgentUX079AmbientScreenDeployment qualifies the screen task on a base
// deployment's model: one profile, one terms, one destination carrying only
// the screen purpose, and credential scopes narrowed to it. The base
// deployment's pricing must already carry an approved price for the identity;
// nothing here invents one.
func NewAgentUX079AmbientScreenDeployment(base PersonaModelDeployment, evaluation agentmodel.ModelEvaluation, maxLatency time.Duration, maxCostMicros int64) (PersonaModelDeployment, error) {
	if len(base.Profiles) == 0 || len(base.Destinations) == 0 || len(base.Credential.Scopes) == 0 {
		return PersonaModelDeployment{}, fmt.Errorf("%w: the base deployment has no profile, destination or credential scope", errAgentUX079AmbientScreenBinding)
	}
	if !evaluation.Passed || evaluation.AgentVersionDigest != AgentUX079AmbientScreenAgentVersionDigest() || strings.TrimSpace(evaluation.SuiteDigest) == "" {
		return PersonaModelDeployment{}, fmt.Errorf("%w: a passing evaluation with agent-version and suite digests is required", errAgentUX079AmbientScreenBinding)
	}
	task, outputDigest, toolDigest := AgentUX079AmbientScreenEvidenceProfile()
	if maxLatency <= 0 || maxLatency > time.Minute || maxCostMicros <= 0 || maxCostMicros > task.MaxCostMicros {
		return PersonaModelDeployment{}, fmt.Errorf("%w: latency or cost is outside the task's bounds", errAgentUX079AmbientScreenBinding)
	}
	template := base.Profiles[0]
	var baseTerms *agentegress.ProviderTerms
	for i := range base.Terms {
		t := base.Terms[i]
		if t.ModelProfile == template.ID {
			baseTerms = &base.Terms[i]
		}
	}
	if baseTerms == nil {
		return PersonaModelDeployment{}, fmt.Errorf("%w: the template profile has no provider terms", errAgentUX079AmbientScreenBinding)
	}
	identity := template.Identity
	sum := sha256.Sum256([]byte(evaluation.AgentVersionDigest + "\x00" + evaluation.SuiteDigest + "\x00" + identity.ModelID + "\x00" + identity.Version))
	profile := agentmodel.ModelProfile{
		ID: "ambient-screen-" + hex.EncodeToString(sum[:])[:12], Identity: identity, Regions: slices.Clone(template.Regions),
		DataClasses: slices.Clone(task.DataClasses), TaskProfileIDs: []string{task.ID}, MaxLatency: maxLatency, MaxCostMicros: maxCostMicros, ExpectedCostMicros: maxCostMicros / 2,
		SemanticsDigest: task.SemanticsDigest, OutputSchemaDigest: outputDigest, ToolSchemaDigest: toolDigest, Evaluation: evaluation,
	}
	profile.ProfileDigest = agentmodel.ModelProfileDigest(profile)
	classes := []trustdlp.DataClass{trustdlp.ClassPublic, trustdlp.ClassInternal}
	terms := *baseTerms
	terms.ModelProfile = profile.ID
	terms.ModelID, terms.ModelVersion = identity.ModelID, identity.Version
	terms.AllowedClasses = slices.Clone(classes)
	terms.AllowedRegions = slices.Clone(baseTerms.AllowedRegions)
	terms.SourceRules = []agentegress.ProviderSourceRule{
		{Class: agentUX079AmbientScreenSourceInstructions, Classes: slices.Clone(classes)},
		{Class: agentUX079AmbientScreenSourceMessage, Classes: slices.Clone(classes)},
	}
	region := ""
	for _, r := range profile.Regions {
		if slices.Contains(terms.AllowedRegions, r) {
			region = r
			break
		}
	}
	if region == "" {
		return PersonaModelDeployment{}, fmt.Errorf("%w: profile and terms share no processing region", errAgentUX079AmbientScreenBinding)
	}
	out := base
	out.Profiles = []agentmodel.ModelProfile{profile}
	out.Terms = []agentegress.ProviderTerms{terms}
	destination := base.Destinations[0]
	destination.Name = profile.ID
	destination.Purposes = []string{AgentUX079AmbientScreenPurpose}
	destination.DataClasses = []string{string(trustdlp.ClassPublic), string(trustdlp.ClassInternal)}
	out.Destinations = append(base.Destinations[:0:0], destination)
	out.Clearances = []trustdlp.Clearance{{Destination: profile.ID, Classes: slices.Clone(classes), Decision: trustdlp.Allow}}
	out.Credential.Scopes = nil
	seen := map[string]bool{}
	for _, scope := range base.Credential.Scopes {
		if seen[scope.TenantID] {
			continue
		}
		seen[scope.TenantID] = true
		out.Credential.Scopes = append(out.Credential.Scopes, OpenAIModelCredentialScope{TenantID: scope.TenantID, Region: region, Purpose: AgentUX079AmbientScreenPurpose, Destination: profile.ID})
	}
	// Policy sources and registrations belong to persona agents, not this task.
	out.PolicySources, out.PolicyRegistrations = nil, nil
	if _, _, err := out.validate(); err != nil {
		return PersonaModelDeployment{}, err
	}
	if _, err := agentUX079AmbientScreenQualify(out); err != nil {
		return PersonaModelDeployment{}, err
	}
	return out, nil
}

type agentUX079AmbientScreenServeInput struct {
	Config  ServeConfig
	Runtime *agentRuntime
	Env     func(string) string
	Now     func() time.Time
}

// composeAgentUX079AmbientScreenPlanner puts the offers planner together over
// the governed model gateway. It returns a nil planner and a plain reason
// when a qualified screen model is not provisioned, so the consumer reads no
// messages rather than failing them. A document that exists but is invalid is
// an error to log, never a silent default.
func composeAgentUX079AmbientScreenPlanner(ctx context.Context, in agentUX079AmbientScreenServeInput) (AgentUXAmbientRunPlanner, string, error) {
	if in.Env == nil {
		in.Env = os.Getenv
	}
	cfg := in.Config
	path := in.Env(EnvAgentUX079AmbientScreenModelConfigFile)
	local := cfg.Profile == ServeProfileLocalDev
	if path == "" && local {
		candidate := filepath.FromSlash(localAgentUX079AmbientScreenDeploymentPath)
		if _, err := os.Stat(candidate); err == nil {
			path = candidate
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, "", fmt.Errorf("ambient screen deployment cannot be read: %w", err)
		}
	}
	if path == "" {
		return nil, "no model is qualified to screen messages for offers (set " + EnvAgentUX079AmbientScreenModelConfigFile + ")", nil
	}
	if in.Runtime == nil || in.Runtime.Budget == nil || isNilPersonaOutputPort(in.Runtime.Audit) {
		return nil, "the agent platform's budget ledger and audit chain are not composed", nil
	}
	if in.Env("MODEL_API_KEY") == "" {
		return nil, "MODEL_API_KEY is not set", nil
	}
	cfg.AgentModelConfigFile = path
	if local {
		material, err := LoadOrCreateLocalPersonaModelSigningMaterial(filepath.FromSlash(localPersonaModelSigningPath))
		if err != nil {
			return nil, "", err
		}
		cfg.PersonaOutputSigningSeed, cfg.PersonaWorkloadSigningSeed = material.OutputSeed, material.WorkloadSeed
	}
	cfg, deployment, apiKey, configured, err := loadServedAgentModelConfiguration(ctx, cfg, in.Env)
	if err != nil {
		return nil, "", err
	}
	if !configured {
		return nil, "the ambient screen deployment is not configured", nil
	}
	if local {
		// As for the persona deployment, the local profile serves its own cell.
		deployment.Worker.Cell = cfg.CellID
	}
	if cfg.CellID == "" || deployment.Worker.Cell != cfg.CellID {
		return nil, "", fmt.Errorf("%w: ambient screen worker cell does not match the serving cell", ErrPersonaModelConfiguration)
	}
	now := personaServeClock(in.Now)
	evidence := &AgentUX079AmbientScreenEvidence{Audit: in.Runtime.Audit, Now: now}
	var resourcesPort AgentModelResourceAdmission
	if in.Runtime.Resources != nil {
		resourcesPort = personaRunModelResources{runtime: in.Runtime.Resources}
	}
	model, _, err := ComposePersonaRuntimeTypedModel(deployment, apiKey, cfg.PersonaOutputSigningSeed, cfg.PersonaWorkloadSigningSeed,
		PersonaModelDeploymentDependencies{Budget: agentmodel.BudgetAdapter{Ledger: in.Runtime.Budget}, Routes: evidence, Sources: evidence, Resources: resourcesPort, Now: now})
	if err != nil {
		return nil, "", err
	}
	binding, err := NewAgentUX079AmbientScreenBinding(deployment, model.Leases, in.Runtime.Budget, now)
	if err != nil {
		// A deployment that does not qualify a model for this task is the
		// ordinary "not provisioned" state, stated plainly.
		return nil, err.Error(), nil
	}
	return AgentUX079AmbientScreenPlanner{Gateway: model.Gateway, Binding: binding}, "", nil
}

// logAgentUX079AmbientScreenComposition states, once at start-up, whether the
// offers planner is served and, if not, why.
func logAgentUX079AmbientScreenComposition(logger interface {
	Info(string, ...any)
	Error(string, ...any)
}, planner AgentUXAmbientRunPlanner, reason string, err error) {
	if isNilPersonaOutputPort(logger) {
		return
	}
	switch {
	case err != nil:
		logger.Error("hcmnext.ambient_screen_unavailable", "error", err.Error())
	case planner == nil:
		logger.Info("hcmnext.ambient_screen_unavailable", "reason", reason)
	default:
		logger.Info("hcmnext.ambient_screen_ready")
	}
}
