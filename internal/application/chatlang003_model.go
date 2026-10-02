package application

import (
	"context"
	"crypto/rand"
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
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// The translation task is qualified like every other governed model task and
// follows the writing-style binding (chattone_model.go) step for step: a
// deployment-owned profile must name it, carry passing evaluation evidence and
// declare exactly these semantics and provider purpose; a deployment that does
// not is not eligible and translation reports itself unavailable. Nothing here
// widens a deployment's approval.
const (
	// ChatlangPurpose is the provider purpose the credential scope, destination
	// policy and clearance of a qualified deployment must carry.
	ChatlangPurpose = "chat.translation"
	// ChatlangTaskProfileID names the task a profile is qualified for.
	ChatlangTaskProfileID = "chat-translation-v1"

	chatlangSourceInstruction  = "chat-translation-instruction"
	chatlangSourceText         = "chat-translation-text"
	chatlangOutputSchemaDigest = "text:chat-translation-v1"
	chatlangToolSchemaDigest   = "none"
	chatlangRequestTimeout     = 20 * time.Second
	chatlangMaxInputTokens     = int64(4000)
	chatlangMaxOutputTokens    = int64(3000)
	chatlangActor              = "chat-translation@1"
	chatlangBudgetUser         = "chat-translation"
)

// ChatlangTaskProfile is the task a model profile is qualified for.
func ChatlangTaskProfile() agentmodel.TaskProfile {
	return agentmodel.TaskProfile{ID: ChatlangTaskProfileID, MaxLatency: 3 * time.Second, MaxCostMicros: 20000,
		DataClasses: []string{string(trustdlp.ClassPublic), string(trustdlp.ClassInternal)}, SemanticsDigest: "chat-translation-meaning-preservation-v1"}
}

// ChatlangBudgetPolicy is the platform ceiling of the dedicated translation
// ledger. It is a safety net far above any workspace's limit: the limit an
// administrator sets is enforced from the persisted usage lines, which survive a
// restart, so this in-process ledger never decides what a workspace may spend.
func ChatlangBudgetPolicy() agentbudget.Policy {
	huge := agentbudget.Limits{Steps: 1_000_000_000, Tokens: 1_000_000_000_000, WallClock: 1_000_000 * time.Hour, SpendMicros: 1_000_000_000_000}
	return agentbudget.Policy{TaskDefault: huge, UserDaily: huge, TenantMonthly: huge}
}

var errChatlangBinding = errors.New("application: chat translation model binding is invalid")

type chatlangQualified struct {
	profile agentmodel.ModelProfile
	terms   agentegress.ProviderTerms
	region  string
	limits  agentmodel.ModelLimits
}

func chatlangQualify(dep PersonaModelDeployment) (chatlangQualified, error) {
	task := ChatlangTaskProfile()
	var profile *agentmodel.ModelProfile
	for i := range dep.Profiles {
		if slices.Contains(dep.Profiles[i].TaskProfileIDs, task.ID) {
			if profile != nil {
				return chatlangQualified{}, fmt.Errorf("%w: more than one profile is qualified for %s", errChatlangBinding, task.ID)
			}
			profile = &dep.Profiles[i]
		}
	}
	if profile == nil {
		return chatlangQualified{}, fmt.Errorf("%w: no model profile is qualified for %s", errChatlangBinding, task.ID)
	}
	if !profile.Evaluation.Passed || strings.TrimSpace(profile.Evaluation.AgentVersionDigest) == "" || strings.TrimSpace(profile.Evaluation.SuiteDigest) == "" ||
		profile.SemanticsDigest != task.SemanticsDigest || profile.OutputSchemaDigest != chatlangOutputSchemaDigest || profile.ToolSchemaDigest != chatlangToolSchemaDigest {
		return chatlangQualified{}, fmt.Errorf("%w: profile evidence or semantics differ from the translation task", errChatlangBinding)
	}
	if profile.MaxLatency <= 0 || profile.MaxLatency > time.Minute || profile.MaxCostMicros <= 0 || profile.MaxCostMicros > task.MaxCostMicros {
		return chatlangQualified{}, fmt.Errorf("%w: profile latency or cost is outside the task's bounds", errChatlangBinding)
	}
	for _, class := range task.DataClasses {
		if !slices.Contains(profile.DataClasses, class) {
			return chatlangQualified{}, fmt.Errorf("%w: profile does not cover data class %s", errChatlangBinding, class)
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
		return chatlangQualified{}, fmt.Errorf("%w: no approved provider terms for the profile", errChatlangBinding)
	}
	sources := map[string]bool{}
	for _, rule := range terms.SourceRules {
		sources[rule.Class] = true
	}
	if !sources[chatlangSourceInstruction] || !sources[chatlangSourceText] {
		return chatlangQualified{}, fmt.Errorf("%w: provider terms have no source rules for the translation fields", errChatlangBinding)
	}
	purposeOK := false
	for _, d := range dep.Destinations {
		if d.Name == profile.ID && slices.Contains(d.Purposes, ChatlangPurpose) {
			purposeOK = true
		}
	}
	if !purposeOK {
		return chatlangQualified{}, fmt.Errorf("%w: no outbound destination allows purpose %s", errChatlangBinding, ChatlangPurpose)
	}
	region := ""
	for _, r := range profile.Regions {
		if slices.Contains(terms.AllowedRegions, r) {
			region = r
			break
		}
	}
	if region == "" {
		return chatlangQualified{}, fmt.Errorf("%w: profile and terms share no processing region", errChatlangBinding)
	}
	pricing, err := agentmodel.NewPricingSchedule(dep.Pricing)
	if err != nil {
		return chatlangQualified{}, fmt.Errorf("%w: pricing: %v", errChatlangBinding, err)
	}
	selection := agentmodel.ModelSelection{ProfileID: profile.ID, ProfileDigest: profile.ProfileDigest, Identity: profile.Identity}
	limits := agentmodel.ModelLimits{MaxInputTokens: chatlangMaxInputTokens, MaxOutputTokens: chatlangMaxOutputTokens, MaxCostMicros: profile.MaxCostMicros}
	for limits.MaxOutputTokens > 500 && pricing.Validate(context.Background(), selection, limits) != nil {
		limits.MaxOutputTokens -= 250
	}
	if err := pricing.Validate(context.Background(), selection, limits); err != nil {
		return chatlangQualified{}, fmt.Errorf("%w: a call cannot fit the profile's cost bound: %v", errChatlangBinding, err)
	}
	return chatlangQualified{profile: *profile, terms: *terms, region: region, limits: limits}, nil
}

// ChatlangDeploymentBinding resolves, for one translation call: the qualified
// model profile, a task-bound budget (one ledger task per workspace per month),
// a fresh route request pinned to the evaluated profile, the provider's
// processing terms as the outbound request, and a single-use credential lease.
type ChatlangDeploymentBinding struct {
	Profile  agentmodel.ModelProfile
	Terms    agentegress.ProviderTerms
	Leases   *ModelLeaseSource
	Workload string
	Ledger   *agentbudget.Ledger
	Region   string
	Limits   agentmodel.ModelLimits
	Now      func() time.Time
}

// NewChatlangDeploymentBinding selects the one profile a deployment qualified
// for translation and refuses anything that does not match the task.
func NewChatlangDeploymentBinding(dep PersonaModelDeployment, leases *ModelLeaseSource, ledger *agentbudget.Ledger, now func() time.Time) (*ChatlangDeploymentBinding, error) {
	if leases == nil || ledger == nil || now == nil {
		return nil, fmt.Errorf("%w: leases, budget ledger and clock are required", errChatlangBinding)
	}
	q, err := chatlangQualify(dep)
	if err != nil {
		return nil, err
	}
	return &ChatlangDeploymentBinding{Profile: q.profile, Terms: q.terms, Leases: leases, Workload: dep.Worker.Workload, Ledger: ledger, Region: q.region, Limits: q.limits, Now: now}, nil
}

// Ready reports that a qualified profile is bound.
func (b *ChatlangDeploymentBinding) Ready() bool {
	return b != nil && b.Leases != nil && b.Ledger != nil
}

// ChatlangBudgetTaskID names the ledger task a workspace's translations in one
// UTC month are charged to.
func ChatlangBudgetTaskID(tenant string, at time.Time) string {
	return "chatlang:" + tenant + ":" + at.UTC().Format("200601")
}

// Bind builds a fresh, single-use gateway request for one workspace.
func (b *ChatlangDeploymentBinding) Bind(ctx context.Context, tenant string) (AgentModelGatewayRequest, error) {
	if !b.Ready() || ctx == nil || strings.TrimSpace(tenant) == "" {
		return AgentModelGatewayRequest{}, chatlang.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return AgentModelGatewayRequest{}, err
	}
	task := ChatlangTaskProfile()
	now := b.Now().UTC()
	taskID := ChatlangBudgetTaskID(tenant, now)
	policy := ChatlangBudgetPolicy().TenantMonthly
	if err := b.Ledger.OpenTask(agentbudget.TaskSpec{ID: taskID, TenantID: tenant, UserID: chatlangBudgetUser, Limit: policy}); err != nil && !errors.Is(err, agentbudget.ErrTaskExists) {
		return AgentModelGatewayRequest{}, fmt.Errorf("%w: %v", chatlang.ErrUnavailable, err)
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return AgentModelGatewayRequest{}, chatlang.ErrUnavailable
	}
	trace := "chatlang-" + hex.EncodeToString(nonce[:])
	profile, terms := b.Profile, b.Terms
	version := profile.Evaluation.AgentVersionDigest
	selection := agentmodel.ModelSelection{ProfileID: profile.ID, ProfileDigest: profile.ProfileDigest, Identity: profile.Identity}
	classes := []trustdlp.DataClass{trustdlp.ClassPublic, trustdlp.ClassInternal}
	deadline := now.Add(minDuration(profile.MaxLatency, chatlangRequestTimeout))
	route := agentmodel.RouteRequest{TraceID: trace,
		Pin: agentmodel.ModelPin{AgentVersionDigest: version, TaskProfileID: task.ID, Primary: selection, SemanticsDigest: task.SemanticsDigest, OutputSchemaDigest: chatlangOutputSchemaDigest, ToolSchemaDigest: chatlangToolSchemaDigest},
		Task: agentmodel.TaskProfile{ID: task.ID, AgentVersionDigest: version, Region: b.Region, DataClasses: slices.Clone(task.DataClasses), MaxLatency: profile.MaxLatency, MaxCostMicros: profile.MaxCostMicros,
			SemanticsDigest: task.SemanticsDigest, OutputSchemaDigest: chatlangOutputSchemaDigest, ToolSchemaDigest: chatlangToolSchemaDigest},
		BudgetRemainingMicros: profile.MaxCostMicros}
	retention := fmt.Sprintf("%s:%d", terms.Retention.Mode, int64(terms.Retention.MaxAge))
	model := agentmodel.ModelRequest{ContractVersion: agentmodel.ContractVersion, TaskProfile: task.ID, ModelProfile: profile.ID,
		Output: agentmodel.OutputConstraint{Mode: agentmodel.OutputText}, Deadline: deadline, Limits: b.Limits, TraceID: trace,
		Processing: agentmodel.ProcessingPolicy{Residency: b.Region, Retention: retention, TrainingUse: terms.TrainingUse, Logging: terms.Logging}}
	trusted, err := NewTrustedModelTask(tenant, taskID, version, b.Workload)
	if err != nil {
		return AgentModelGatewayRequest{}, err
	}
	credential, err := b.Leases.Issue(ctx, ModelLeaseRequest{Task: trusted, ProviderID: profile.Identity.ProviderID, Destination: profile.ID, Purpose: ChatlangPurpose, Region: b.Region, TTL: minDuration(time.Minute, deadline.Sub(now))})
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
	return AgentModelGatewayRequest{TenantID: tenant, Route: route, Dispatch: agentegress.ProviderDispatchRequest{Model: model, Lease: credential,
		Outbound: agentegress.OutboundRequest{TaskID: taskID, Tenant: tenant, Principal: chatlangBudgetUser, Purpose: ChatlangPurpose, Profile: egress, Region: b.Region,
			Task: agentegress.TaskPolicy{AllowedRegions: []string{b.Region}, AllowedResultClasses: classes, MaxExternalRetention: terms.Retention.MaxAge, ResultRetention: receiptRetention}, Now: now}}}, nil
}

type chatlangEvidenceKey struct{}

// ChatlangModelEvidence verifies source classifications against the request the
// gateway engine built, and records the routing decision in the append-only
// audit chain. Both read the built request from the context, so neither can be
// satisfied by a request that was not built for this workspace, this call and
// these exact bytes.
type ChatlangModelEvidence struct {
	Audit interface {
		Append(context.Context, agentaudit.Entry) (agentaudit.Record, error)
	}
	Now func() time.Time
}

func chatlangBuilt(ctx context.Context) (AgentModelGatewayRequest, bool) {
	built, ok := ctx.Value(chatlangEvidenceKey{}).(AgentModelGatewayRequest)
	return built, ok && built.TenantID != "" && built.Route.TraceID != ""
}

func (e *ChatlangModelEvidence) VerifySourceClassification(ctx context.Context, source agentegress.SourceClassificationRequest) error {
	if e == nil || ctx == nil {
		return ErrAgentModelGatewayNotConfigured
	}
	built, ok := chatlangBuilt(ctx)
	if !ok || source.Tenant != built.TenantID || source.Purpose != ChatlangPurpose || built.Dispatch.Outbound.Purpose != ChatlangPurpose {
		return ErrAgentModelGatewayTenant
	}
	for _, field := range built.Dispatch.Outbound.Fields {
		value, isText := field.Value.(string)
		if field.Name != source.FieldName || !isText {
			continue
		}
		wantRole := agentmodel.RoleUser
		if built.Dispatch.FieldSources[field.Name] == chatlangSourceInstruction {
			wantRole = agentmodel.RoleSystem
		}
		if source.SourceClass == built.Dispatch.FieldSources[field.Name] && (source.SourceClass == chatlangSourceInstruction || source.SourceClass == chatlangSourceText) &&
			source.DataClass == field.Class && slices.Equal(source.Provenance, field.Provenance) && slices.Contains(source.Provenance, "chat-translation:"+built.Route.TraceID) &&
			source.ValueDigest == chattoneDigest(value) && source.MessageRole == wantRole {
			return nil
		}
	}
	return ErrAgentModelGatewayPin
}

func (e *ChatlangModelEvidence) RecordRoute(ctx context.Context, route agentmodel.RouteRecord) error {
	if e == nil || ctx == nil || e.Audit == nil {
		return ErrAgentModelGatewayNotConfigured
	}
	built, ok := chatlangBuilt(ctx)
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
	eventID := "chat-translation-route:" + route.TraceID + ":" + route.Digest
	taskID := built.Dispatch.Outbound.TaskID
	// There is no agent and no person pressing a control: the workspace's
	// translation setting is the delegation, and the audit entry says so.
	_, err = e.Audit.Append(ctx, agentaudit.Entry{EventID: eventID, TenantID: built.TenantID, Kind: agentaudit.EventModelCall,
		Actor:  agentaudit.ActorChain{UserID: chatlangBudgetUser, AgentVersion: chatlangActor, InstallationID: "workspace:" + built.TenantID, TaskID: taskID, PlanRevision: built.Route.Pin.SemanticsDigest, StepID: route.TraceID, DelegationGrantID: "workspace-translation-setting:" + built.TenantID},
		Action: "model.route", ResultDigest: route.Digest, Fields: []agentaudit.Field{{Name: "model_route", Value: string(encoded), Classification: agentaudit.ClassificationInternal}},
		Edges: []agentaudit.Edge{{Kind: agentaudit.EdgeTask, From: taskID, To: eventID}}, OccurredAt: at})
	return err
}

var (
	_ agentegress.SourceClassificationVerifier = (*ChatlangModelEvidence)(nil)
	_ agentmodel.RouteRecorder                 = (*ChatlangModelEvidence)(nil)
)

// ChatlangGatewayBinding is the binding a gateway engine asks for a request.
type ChatlangGatewayBinding interface {
	Bind(context.Context, string) (AgentModelGatewayRequest, error)
}

// ChatlangGateway is the model gateway (AgentModelGateway in production).
type ChatlangGateway interface {
	Dispatch(context.Context, AgentModelGatewayRequest) (AgentModelGatewayResult, error)
}

// ChatlangGatewayEngine is the general language model reached through the
// governed model route: the first implementation of chatlang.Engine. A
// dedicated translation service or an in-deployment model is a later one.
type ChatlangGatewayEngine struct {
	Gateway ChatlangGateway
	Binding ChatlangGatewayBinding
}

// Ready reports whether a qualified model is bound.
func (e ChatlangGatewayEngine) Ready() bool {
	if e.Gateway == nil || e.Binding == nil {
		return false
	}
	if ready, ok := e.Binding.(interface{ Ready() bool }); ok {
		return ready.Ready()
	}
	return true
}

// Translate implements chatlang.Engine.
func (e ChatlangGatewayEngine) Translate(ctx context.Context, r chatlang.Request) (chatlang.Response, error) {
	if !e.Ready() || strings.TrimSpace(r.Tenant) == "" {
		return chatlang.Response{}, chatlang.ErrUnavailable
	}
	req, err := e.Binding.Bind(ctx, r.Tenant)
	if err != nil {
		if errors.Is(err, agentbudget.ErrPaused) {
			return chatlang.Response{}, chatlang.ErrBudget
		}
		return chatlang.Response{}, err
	}
	if req.TenantID != r.Tenant || req.Dispatch.Outbound.Tenant != r.Tenant || req.Route.Task.ID != ChatlangTaskProfileID || req.Route.Pin.TaskProfileID != ChatlangTaskProfileID {
		return chatlang.Response{}, chatlang.ErrUnavailable
	}
	instruction, data := chatlang.Prompt(r)
	req.Dispatch.Model.TaskProfile = ChatlangTaskProfileID
	req.Dispatch.Model.Messages = []agentmodel.ModelMessage{{Role: agentmodel.RoleSystem, Content: instruction}, {Role: agentmodel.RoleUser, Content: data}}
	req.Dispatch.Model.Tools = nil
	provenance := []string{"chat-translation:" + req.Route.TraceID}
	req.Dispatch.Outbound.DeclaredFields = []string{"model.message.0", "model.message.1"}
	req.Dispatch.Outbound.Fields = []agentegress.Field{
		// The instruction is fixed product text; the data is what a person wrote.
		{Name: "model.message.0", Value: instruction, Class: trustdlp.ClassInternal, Taint: []string{string(agentsecurity.TaintCanonical)}, Provenance: provenance},
		{Name: "model.message.1", Value: data, Class: trustdlp.ClassInternal, Taint: []string{string(agentsecurity.TaintHuman)}, Provenance: provenance},
	}
	req.Dispatch.FieldSources = map[string]string{"model.message.0": chatlangSourceInstruction, "model.message.1": chatlangSourceText}
	result, err := e.Gateway.Dispatch(context.WithValue(ctx, chatlangEvidenceKey{}, req), req)
	if err != nil {
		if errors.Is(err, agentbudget.ErrPaused) {
			return chatlang.Response{}, chatlang.ErrBudget
		}
		return chatlang.Response{}, err
	}
	model := result.Dispatch.Model
	if model.Refusal != nil || model.Failure != nil || model.Finish != agentmodel.FinishComplete || len(model.ToolProposals) != 0 || len(model.RequestedActions) != 0 {
		return chatlang.Response{}, chatlang.ErrUnavailable
	}
	return chatlang.Response{Text: strings.TrimSpace(model.Text), Provider: model.Provider.ProviderID, Model: model.Provider.ModelID,
		InputTokens: model.Usage.InputTokens, OutputTokens: model.Usage.OutputTokens, CostMicros: model.Usage.CostMicros}, nil
}

var _ chatlang.Engine = ChatlangGatewayEngine{}
