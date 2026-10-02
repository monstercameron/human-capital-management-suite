package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// The writing-style model task is qualified like every other governed model
// task: a deployment-owned profile must name it, carry passing evaluation
// evidence, and declare exactly these semantics and provider purpose. A
// deployment that does not is not eligible and the service reports itself
// unavailable; nothing here widens a deployment's approval.
const (
	// ChattonePurpose is the provider purpose the credential scope, destination
	// policy and clearance of a qualified deployment must carry.
	ChattonePurpose            = "chat.writing_style"
	chattoneSourceInstruction  = "chat-writing-instruction"
	chattoneSourceDraft        = "chat-writing-draft"
	chattoneOutputSchemaDigest = "text:chat-writing-style-v1"
	chattoneToolSchemaDigest   = "none"
	chattoneRequestTimeout     = 25 * time.Second
	chattoneMaxInputTokens     = int64(6000)
	chattoneMaxOutputTokens    = int64(4000)
	// chattoneDailyModelCalls is the per-person, per-day ceiling of the budget
	// task: one rewrite is one model call (the meaning guard is not a model).
	chattoneDailyModelCalls = 30
	chattoneActor           = "chat-writing-style@1"
)

// ChattoneModelEvidenceProfile is what a deployment must declare for the
// writing-style task. It exists so the requirement is stated in one place and
// tested, not rediscovered from an eligibility refusal.
func ChattoneModelEvidenceProfile() (task agentmodel.TaskProfile, outputDigest, toolDigest string) {
	return ChattoneTaskProfile(), chattoneOutputSchemaDigest, chattoneToolSchemaDigest
}

// ChattoneDeploymentBinding is the production ChattoneGatewayBinding. It
// resolves, for one writer and one call: the qualified model profile, a
// task-bound budget (one ledger task per person per day, so the daily call
// ceiling and the platform's user-day and tenant-month ceilings all apply), a
// fresh route request pinned to the evaluated profile, the provider's
// processing terms as the outbound request, and a single-use credential lease.
// The prompt itself is supplied later by ChattoneGatewayModel.
type ChattoneDeploymentBinding struct {
	Profile  agentmodel.ModelProfile
	Terms    agentegress.ProviderTerms
	Leases   *ModelLeaseSource
	Workload string
	Ledger   *agentbudget.Ledger
	Region   string
	Limits   agentmodel.ModelLimits
	Now      func() time.Time
}

var errChattoneBinding = errors.New("application: chat writing style model binding is invalid")

// NewChattoneDeploymentBinding selects the one profile a deployment qualified
// for the writing-style task and refuses anything that does not match the
// task's declared semantics, purpose, latency and cost.
func NewChattoneDeploymentBinding(dep PersonaModelDeployment, leases *ModelLeaseSource, ledger *agentbudget.Ledger, now func() time.Time) (*ChattoneDeploymentBinding, error) {
	if leases == nil || ledger == nil || now == nil {
		return nil, fmt.Errorf("%w: leases, budget ledger and clock are required", errChattoneBinding)
	}
	q, err := chattoneQualify(dep)
	if err != nil {
		return nil, err
	}
	return &ChattoneDeploymentBinding{Profile: q.profile, Terms: q.terms, Leases: leases, Workload: dep.Worker.Workload, Ledger: ledger, Region: q.region, Limits: q.limits, Now: now}, nil
}

// chattoneQualified is the outcome of checking a deployment document against
// the writing-style task: the one profile and terms, region and call limits.
type chattoneQualified struct {
	profile agentmodel.ModelProfile
	terms   agentegress.ProviderTerms
	region  string
	limits  agentmodel.ModelLimits
}

func chattoneQualify(dep PersonaModelDeployment) (chattoneQualified, error) {
	task, outputDigest, toolDigest := ChattoneModelEvidenceProfile()
	var profile *agentmodel.ModelProfile
	for i := range dep.Profiles {
		if slices.Contains(dep.Profiles[i].TaskProfileIDs, task.ID) {
			if profile != nil {
				return chattoneQualified{}, fmt.Errorf("%w: more than one profile is qualified for %s", errChattoneBinding, task.ID)
			}
			profile = &dep.Profiles[i]
		}
	}
	if profile == nil {
		return chattoneQualified{}, fmt.Errorf("%w: no model profile is qualified for %s", errChattoneBinding, task.ID)
	}
	if !profile.Evaluation.Passed || strings.TrimSpace(profile.Evaluation.AgentVersionDigest) == "" || strings.TrimSpace(profile.Evaluation.SuiteDigest) == "" ||
		profile.Evaluation.AgentVersionDigest != ChattoneAgentVersionDigest() || profile.SemanticsDigest != task.SemanticsDigest || profile.OutputSchemaDigest != outputDigest || profile.ToolSchemaDigest != toolDigest {
		return chattoneQualified{}, fmt.Errorf("%w: profile evidence or semantics differ from the writing-style task", errChattoneBinding)
	}
	if profile.MaxLatency <= 0 || profile.MaxLatency > time.Minute || profile.MaxCostMicros <= 0 || profile.MaxCostMicros > task.MaxCostMicros {
		return chattoneQualified{}, fmt.Errorf("%w: profile latency or cost is outside the task's bounds", errChattoneBinding)
	}
	for _, class := range task.DataClasses {
		if !slices.Contains(profile.DataClasses, class) {
			return chattoneQualified{}, fmt.Errorf("%w: profile does not cover data class %s", errChattoneBinding, class)
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
		return chattoneQualified{}, fmt.Errorf("%w: no approved provider terms for the profile", errChattoneBinding)
	}
	sources := map[string]bool{}
	for _, rule := range terms.SourceRules {
		sources[rule.Class] = true
	}
	if !sources[chattoneSourceInstruction] || !sources[chattoneSourceDraft] {
		return chattoneQualified{}, fmt.Errorf("%w: provider terms have no source rules for the writing-style fields", errChattoneBinding)
	}
	purposeOK := false
	for _, d := range dep.Destinations {
		if d.Name == profile.ID && slices.Contains(d.Purposes, ChattonePurpose) {
			purposeOK = true
		}
	}
	if !purposeOK {
		return chattoneQualified{}, fmt.Errorf("%w: no outbound destination allows purpose %s", errChattoneBinding, ChattonePurpose)
	}
	region := ""
	for _, r := range profile.Regions {
		if slices.Contains(terms.AllowedRegions, r) {
			region = r
			break
		}
	}
	if region == "" {
		return chattoneQualified{}, fmt.Errorf("%w: profile and terms share no processing region", errChattoneBinding)
	}
	pricing, err := agentmodel.NewPricingSchedule(dep.Pricing)
	if err != nil {
		return chattoneQualified{}, fmt.Errorf("%w: pricing: %v", errChattoneBinding, err)
	}
	selection := agentmodel.ModelSelection{ProfileID: profile.ID, ProfileDigest: profile.ProfileDigest, Identity: profile.Identity}
	limits := agentmodel.ModelLimits{MaxInputTokens: chattoneMaxInputTokens, MaxOutputTokens: chattoneMaxOutputTokens, MaxCostMicros: profile.MaxCostMicros}
	// The worst-case bill of one call must fit the profile's cost bound; shrink
	// the output ceiling until it does rather than refuse a cheaper model.
	for limits.MaxOutputTokens > 500 && pricing.Validate(context.Background(), selection, limits) != nil {
		limits.MaxOutputTokens -= 250
	}
	if err := pricing.Validate(context.Background(), selection, limits); err != nil {
		return chattoneQualified{}, fmt.Errorf("%w: a call cannot fit the profile's cost bound: %v", errChattoneBinding, err)
	}
	return chattoneQualified{profile: *profile, terms: *terms, region: region, limits: limits}, nil
}

// Ready reports that a qualified profile is bound.
func (b *ChattoneDeploymentBinding) Ready() bool {
	return b != nil && b.Leases != nil && b.Ledger != nil
}

func (b *ChattoneDeploymentBinding) dailyLimits() agentbudget.Limits {
	return agentbudget.Limits{
		Steps:       chattoneDailyModelCalls,
		Tokens:      chattoneDailyModelCalls * (b.Limits.MaxInputTokens + b.Limits.MaxOutputTokens),
		SpendMicros: chattoneDailyModelCalls * b.Profile.MaxCostMicros,
		WallClock:   chattoneDailyModelCalls * chattoneRequestTimeout,
	}
}

// ChattoneBudgetTaskID names the ledger task one writer's calls on one UTC day
// are charged to.
func ChattoneBudgetTaskID(id chatrewrite.Identity, at time.Time) string {
	return "chattone:" + id.Tenant + ":" + id.Person + ":" + at.UTC().Format("20060102")
}

func (b *ChattoneDeploymentBinding) BindWritingStyle(ctx context.Context, id chatrewrite.Identity, task agentmodel.TaskProfile) (AgentModelGatewayRequest, error) {
	if !b.Ready() || ctx == nil || !id.Valid() || task.ID != chatrewrite.TaskProfileID {
		return AgentModelGatewayRequest{}, chatrewrite.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return AgentModelGatewayRequest{}, err
	}
	now := b.Now().UTC()
	taskID := ChattoneBudgetTaskID(id, now)
	if err := b.Ledger.OpenTask(agentbudget.TaskSpec{ID: taskID, TenantID: id.Tenant, UserID: id.Person, Limit: b.dailyLimits()}); err != nil && !errors.Is(err, agentbudget.ErrTaskExists) {
		return AgentModelGatewayRequest{}, fmt.Errorf("%w: %v", chatrewrite.ErrUnavailable, err)
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return AgentModelGatewayRequest{}, chatrewrite.ErrUnavailable
	}
	trace := "chattone-" + hex.EncodeToString(nonce[:])
	profile, terms := b.Profile, b.Terms
	version := profile.Evaluation.AgentVersionDigest
	selection := agentmodel.ModelSelection{ProfileID: profile.ID, ProfileDigest: profile.ProfileDigest, Identity: profile.Identity}
	classes := []trustdlp.DataClass{trustdlp.ClassPublic, trustdlp.ClassInternal}
	deadline := now.Add(minDuration(profile.MaxLatency, chattoneRequestTimeout))
	route := agentmodel.RouteRequest{TraceID: trace,
		Pin: agentmodel.ModelPin{AgentVersionDigest: version, TaskProfileID: task.ID, Primary: selection, SemanticsDigest: task.SemanticsDigest, OutputSchemaDigest: chattoneOutputSchemaDigest, ToolSchemaDigest: chattoneToolSchemaDigest},
		Task: agentmodel.TaskProfile{ID: task.ID, AgentVersionDigest: version, Region: b.Region, DataClasses: slices.Clone(task.DataClasses), MaxLatency: profile.MaxLatency, MaxCostMicros: profile.MaxCostMicros,
			SemanticsDigest: task.SemanticsDigest, OutputSchemaDigest: chattoneOutputSchemaDigest, ToolSchemaDigest: chattoneToolSchemaDigest},
		BudgetRemainingMicros: profile.MaxCostMicros}
	retention := fmt.Sprintf("%s:%d", terms.Retention.Mode, int64(terms.Retention.MaxAge))
	model := agentmodel.ModelRequest{ContractVersion: agentmodel.ContractVersion, TaskProfile: task.ID, ModelProfile: profile.ID,
		Output: agentmodel.OutputConstraint{Mode: agentmodel.OutputText}, Deadline: deadline, Limits: b.Limits, TraceID: trace,
		Processing: agentmodel.ProcessingPolicy{Residency: b.Region, Retention: retention, TrainingUse: terms.TrainingUse, Logging: terms.Logging}}
	trusted, err := NewTrustedModelTask(id.Tenant, taskID, version, b.Workload)
	if err != nil {
		return AgentModelGatewayRequest{}, err
	}
	credential, err := b.Leases.Issue(ctx, ModelLeaseRequest{Task: trusted, ProviderID: profile.Identity.ProviderID, Destination: profile.ID, Purpose: ChattonePurpose, Region: b.Region, TTL: minDuration(time.Minute, deadline.Sub(now))})
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
	return AgentModelGatewayRequest{TenantID: id.Tenant, Route: route, Dispatch: agentegress.ProviderDispatchRequest{Model: model, Lease: credential,
		Outbound: agentegress.OutboundRequest{TaskID: taskID, Tenant: id.Tenant, Principal: id.Person, Purpose: ChattonePurpose, Profile: egress, Region: b.Region,
			Task: agentegress.TaskPolicy{AllowedRegions: []string{b.Region}, AllowedResultClasses: classes, MaxExternalRetention: terms.Retention.MaxAge, ResultRetention: receiptRetention}, Now: now}}}, nil
}

var _ ChattoneGatewayBinding = (*ChattoneDeploymentBinding)(nil)

type chattoneEvidenceKey struct{}

// ChattoneModelEvidence verifies source classifications against the request
// the gateway model built and records the routing decision in the append-only
// audit chain. Both read the built request from the context, so neither can be
// satisfied by a request that was not built by ChattoneGatewayModel for this
// writer, this call and these exact bytes.
type ChattoneModelEvidence struct {
	Audit interface {
		Append(context.Context, agentaudit.Entry) (agentaudit.Record, error)
	}
	Now func() time.Time
}

func chattoneDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func chattoneBuilt(ctx context.Context) (AgentModelGatewayRequest, bool) {
	built, ok := ctx.Value(chattoneEvidenceKey{}).(AgentModelGatewayRequest)
	return built, ok && built.TenantID != "" && built.Route.TraceID != ""
}

func (e *ChattoneModelEvidence) VerifySourceClassification(ctx context.Context, source agentegress.SourceClassificationRequest) error {
	if e == nil || ctx == nil {
		return ErrAgentModelGatewayNotConfigured
	}
	built, ok := chattoneBuilt(ctx)
	if !ok || source.Tenant != built.TenantID || source.Purpose != ChattonePurpose || built.Dispatch.Outbound.Purpose != ChattonePurpose {
		return ErrAgentModelGatewayTenant
	}
	for _, field := range built.Dispatch.Outbound.Fields {
		value, isText := field.Value.(string)
		if field.Name != source.FieldName || !isText {
			continue
		}
		wantRole := agentmodel.RoleUser
		if built.Dispatch.FieldSources[field.Name] == chattoneSourceInstruction {
			wantRole = agentmodel.RoleSystem
		}
		if source.SourceClass == built.Dispatch.FieldSources[field.Name] && (source.SourceClass == chattoneSourceInstruction || source.SourceClass == chattoneSourceDraft) &&
			source.DataClass == field.Class && slices.Equal(source.Provenance, field.Provenance) && slices.Contains(source.Provenance, "chat-writing-style:"+built.Route.TraceID) &&
			source.ValueDigest == chattoneDigest(value) && source.MessageRole == wantRole {
			return nil
		}
	}
	return ErrAgentModelGatewayPin
}

func (e *ChattoneModelEvidence) RecordRoute(ctx context.Context, route agentmodel.RouteRecord) error {
	if e == nil || ctx == nil || e.Audit == nil {
		return ErrAgentModelGatewayNotConfigured
	}
	built, ok := chattoneBuilt(ctx)
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
	eventID := "chat-writing-style-route:" + route.TraceID + ":" + route.Digest
	taskID, person := built.Dispatch.Outbound.TaskID, built.Dispatch.Outbound.Principal
	// There is no agent in a writing-style call: the person pressed a control, so
	// the person is the actor and the "grant" is that request.
	_, err = e.Audit.Append(ctx, agentaudit.Entry{EventID: eventID, TenantID: built.TenantID, Kind: agentaudit.EventModelCall,
		Actor:  agentaudit.ActorChain{UserID: person, AgentVersion: chattoneActor, InstallationID: "workspace:" + built.TenantID, TaskID: taskID, PlanRevision: built.Route.Pin.SemanticsDigest, StepID: route.TraceID, DelegationGrantID: "writer-request:" + person},
		Action: "model.route", ResultDigest: route.Digest, Fields: []agentaudit.Field{{Name: "model_route", Value: string(encoded), Classification: agentaudit.ClassificationInternal}},
		Edges: []agentaudit.Edge{{Kind: agentaudit.EdgeTask, From: taskID, To: eventID}}, OccurredAt: at})
	return err
}

var (
	_ agentegress.SourceClassificationVerifier = (*ChattoneModelEvidence)(nil)
	_ agentmodel.RouteRecorder                 = (*ChattoneModelEvidence)(nil)
)
