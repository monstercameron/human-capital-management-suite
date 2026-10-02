package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

var errPersonaRunModelWork = errors.New("application: persona model work authority unavailable")

// PersonaModelRoutePolicyReader resolves the current exact model policy named
// by the immutable agent manifest under tenant and legal-entity scope.
type PersonaModelRoutePolicyReader interface {
	CurrentPersonaModelRoutePolicy(context.Context, uuid.UUID, string, string, int64, int64, string, time.Time) (agentstore.PersonaModelRoutePolicy, error)
}

// PersonaRunModelRoute is the administrator-provisioned T0 route payload.
// It contains one exact primary model selection; fallback is intentionally
// unsupported until run-attempt state and destination-scoped leases are bound.
type PersonaRunModelRoute struct {
	Route        agentmodel.RouteRequest     `json:"route"`
	Purpose      string                      `json:"purpose"`
	Processing   agentmodel.ProcessingPolicy `json:"processing"`
	Egress       agentegress.Profile         `json:"egress"`
	TaskPolicy   agentegress.TaskPolicy      `json:"task_policy"`
	ProfileClass trustdlp.DataClass          `json:"profile_class"`
	InvokerClass trustdlp.DataClass          `json:"invoker_class"`
	ThreadClass  trustdlp.DataClass          `json:"thread_class"`
}

// PersonaRunModelWorkSourceConfig contains production authorities. Every
// field is required; this source has no development or caller-supplied route.
type PersonaRunModelWorkSourceConfig struct {
	Personas   personaRunAuthorityFactory
	Manifests  personaRunManifestResolverFactory
	Routes     PersonaModelRoutePolicyReader
	Budgets    *PersonaRunEffectivePolicyResolver
	Threads    agentinvoke.ThreadReader
	Leases     *ModelLeaseSource
	TenantUUID func(values.TenantId) uuid.UUID
	Workload   string
	Now        func() time.Time
	Remaining  PersonaRunModelRemainingBudgetSource
	Documents  agentdocref.Resolver
}

// PersonaRunModelRemainingBudgetSource narrows a model step to capacity left in
// the same durable task ledger. It cannot reopen or extend the admission cap.
type PersonaRunModelRemainingBudgetSource interface {
	RemainingPersonaRunModelBudget(context.Context, agentrun.Record, runstate.Run) (agentrun.Budget, error)
}

// DatabasePersonaRunModelWorkSource rebuilds one T0 persona model request from
// current tenant authority, an exact manifest pin, and the invoker-readable
// thread. It never accepts caller model, budget, prompt, or routing defaults.
type DatabasePersonaRunModelWorkSource struct {
	personas   personaRunAuthorityFactory
	manifests  personaRunManifestResolverFactory
	routes     PersonaModelRoutePolicyReader
	budgets    *PersonaRunEffectivePolicyResolver
	threads    agentinvoke.ThreadReader
	leases     *ModelLeaseSource
	tenantUUID func(values.TenantId) uuid.UUID
	workload   string
	now        func() time.Time
	remaining  PersonaRunModelRemainingBudgetSource
	documents  agentdocref.Resolver
}

// NewDatabasePersonaRunModelWorkSource requires all authority, thread and
// credential dependencies before a production persona run can be executed.
func NewDatabasePersonaRunModelWorkSource(cfg PersonaRunModelWorkSourceConfig) (*DatabasePersonaRunModelWorkSource, error) {
	if cfg.Personas == nil || cfg.Manifests == nil || cfg.Routes == nil || cfg.Budgets == nil || cfg.Threads == nil || cfg.Leases == nil || cfg.TenantUUID == nil || !required(cfg.Workload) || cfg.Now == nil {
		return nil, errPersonaRunModelWork
	}
	return &DatabasePersonaRunModelWorkSource{personas: cfg.Personas, manifests: cfg.Manifests, routes: cfg.Routes, budgets: cfg.Budgets, threads: cfg.Threads, leases: cfg.Leases, tenantUUID: cfg.TenantUUID, workload: cfg.Workload, now: cfg.Now, remaining: cfg.Remaining, documents: cfg.Documents}, nil
}

var _ PersonaRunModelWorkSource = (*DatabasePersonaRunModelWorkSource)(nil)

// BuildPersonaRunModelWork creates a single bounded model request for one
// admitted T0 persona chat invocation.
func (s *DatabasePersonaRunModelWorkSource) BuildPersonaRunModelWork(ctx context.Context, admission agentrun.Record, run runstate.Run) (PersonaRunModelWork, error) {
	if admission.Request.Source.Kind == agentrun.SourceAnnouncement {
		if runtime, ok := ctx.Value(announcementRuntimeKey{}).(*AgentAnnouncementRuntime); ok && runtime.Work == s {
			return runtime.buildModelWork(ctx, admission, run)
		}
		return PersonaRunModelWork{}, errPersonaRunModelWork
	}
	if ctx != nil {
		ctx = WithPersonaBackgroundAdmission(ctx, admission)
	}
	if s == nil || ctx == nil || s.personas == nil || s.manifests == nil || s.routes == nil || s.budgets == nil || s.threads == nil || s.leases == nil || s.tenantUUID == nil || s.now == nil {
		return PersonaRunModelWork{}, errPersonaRunModelWork
	}
	if err := validatePersonaModelWorkBinding(admission, run); err != nil {
		return PersonaRunModelWork{}, err
	}
	immutable, cached := agentUXSpeedImmutableModelWork(ctx, run.ID)
	if !cached {
		type profileResult struct {
			profile  agentpersona.PersonaProfile
			manifest agentmanifest.Manifest
			err      error
		}
		type policyResult struct {
			policy PersonaRunEffectivePolicy
			err    error
		}
		profiles, policies := make(chan profileResult, 1), make(chan policyResult, 1)
		go func() {
			done := agentUXSpeedEvent(ctx, "store.profile_manifest")
			defer done()
			profile, manifest, err := s.resolveProfileAndManifest(ctx, admission)
			profiles <- profileResult{profile: profile, manifest: manifest, err: err}
		}()
		go func() {
			done := agentUXSpeedEvent(ctx, "store.budget_policy")
			defer done()
			policy, err := s.budgets.Resolve(ctx, admission.Request.Source.TenantID, admission.Request.LegalEntity)
			policies <- policyResult{policy: policy, err: err}
		}()
		profileRead, policyRead := <-profiles, <-policies
		if profileRead.err != nil {
			return PersonaRunModelWork{}, profileRead.err
		}
		if policyRead.err != nil {
			return PersonaRunModelWork{}, fmt.Errorf("%w: current budget authority: %v", errPersonaRunModelWork, policyRead.err)
		}
		profile, manifest, policy := profileRead.profile, profileRead.manifest, policyRead.policy
		done := agentUXSpeedEvent(ctx, "store.model_route")
		route, err := s.resolveRoute(ctx, admission, run, manifest, profile, policy)
		done()
		if err != nil {
			return PersonaRunModelWork{}, err
		}
		immutable = agentUXImmutableModelWork{profile: profile, manifest: manifest, route: route, policy: policy}
		agentUXSpeedCacheImmutableModelWork(ctx, run.ID, immutable)
	}
	profile, manifest, route, policy := immutable.profile, immutable.manifest, immutable.route, immutable.policy
	done := agentUXSpeedEvent(ctx, "store.reference_documents")
	documents, _, err := s.resolveAgentDocuments(ctx, admission, profile)
	done()
	if err != nil {
		return PersonaRunModelWork{}, fmt.Errorf("%w: resolve invoker-readable reference documents: %v", errPersonaRunModelWork, err)
	}
	documentClass, err := personaReferenceDocumentDataClass(ctx, s.documents, admission, documents, route)
	if err != nil {
		return PersonaRunModelWork{}, fmt.Errorf("%w: reference document classification: %v", errPersonaRunModelWork, err)
	}
	profile.Guidance = renderPersonaGuidanceDocumentTokens(profile, documents)
	if !isNilPersonaOutputPort(s.remaining) {
		remaining, err := s.remaining.RemainingPersonaRunModelBudget(ctx, admission, run)
		if err != nil {
			return PersonaRunModelWork{}, err
		}
		policy.Budget.MaxInputTokens = minUint(policy.Budget.MaxInputTokens, remaining.MaxInputTokens)
		policy.Budget.MaxOutputTokens = minUint(policy.Budget.MaxOutputTokens, remaining.MaxOutputTokens)
		policy.Budget.MaxCostMicros = minUint(policy.Budget.MaxCostMicros, remaining.MaxCostMicros)
		if policy.Budget.MaxInputTokens == 0 || policy.Budget.MaxOutputTokens == 0 || policy.Budget.MaxCostMicros == 0 {
			return PersonaRunModelWork{}, errPersonaRunModelWork
		}
		// The remaining budget bounds the request limits built from
		// policy.Budget and the ledger reservation made before the call. It
		// does not shrink the route's contractual cost limit: the pinned
		// model's declared ceiling equals the whole run budget, so a shrunk
		// limit would make every continuation ineligible.
	}
	done = agentUXSpeedEvent(ctx, "store.thread_context")
	goal, history, refs, err := s.readThreadContext(ctx, admission, profile)
	done()
	if err != nil {
		return PersonaRunModelWork{}, err
	}
	request, err := s.buildExecutorRequest(ctx, admission, run, profile, manifest, route, policy, goal, history, refs)
	if err != nil {
		return PersonaRunModelWork{}, err
	}
	if err := addAgentDocumentsToModelRequest(&request, documents, route); err != nil {
		return PersonaRunModelWork{}, fmt.Errorf("%w: quarantine reference documents: %v", errPersonaRunModelWork, err)
	}
	classifyPersonaReferenceDocumentFields(&request, documentClass)
	// AGENTUX-076: the agent's own facts and the documents it can read here.
	facts, withFacts := personaAgentFactsFor(ctx, s.documents, admission, profile, true)
	if withFacts {
		if err := insertPersonaAgentFacts(&request, facts, route); err != nil {
			return PersonaRunModelWork{}, fmt.Errorf("%w: agent facts: %v", errPersonaRunModelWork, err)
		}
	}
	if err := validateExecutorRequest(request); err != nil {
		return PersonaRunModelWork{}, fmt.Errorf("%w: generated executor binding: %v", errPersonaRunModelWork, err)
	}
	return PersonaRunModelWork{Request: request, Facts: facts, HasFacts: withFacts, General: personaAnswersGenerally(profile)}, nil
}

func validatePersonaModelWorkBinding(admission agentrun.Record, run runstate.Run) error {
	if agentrun.ValidateAdmissionRecord(admission) != nil || admission.Decision != agentrun.DecisionAccepted || admission.Request.Source.Kind != agentrun.SourcePersonaMention ||
		admission.Request.Principal.Mode != agentrun.ModeOnBehalfOf || admission.Request.Persona == nil || admission.Authority.Agent != admission.Request.Agent ||
		run.ID != admission.ID || run.AdmissionID != admission.ID || run.TenantID != admission.Request.Source.TenantID || run.AgentID != admission.Authority.Agent.AgentID ||
		run.AgentVersion != admission.Authority.Agent.Version || run.AgentDigest != admission.Authority.Agent.Digest || run.ContextDigest != admission.Authority.Context.Digest ||
		run.Deadline.IsZero() || !run.Deadline.Equal(admission.Request.Deadline) || run.PrincipalMode != agentrun.ModeOnBehalfOf || run.ActorID != admission.Request.Principal.InvokerID {
		return fmt.Errorf("%w: admitted persona run identity mismatch", errPersonaRunModelWork)
	}
	return nil
}

func (s *DatabasePersonaRunModelWorkSource) resolveProfileAndManifest(ctx context.Context, admission agentrun.Record) (agentpersona.PersonaProfile, agentmanifest.Manifest, error) {
	tenant := admission.Request.Source.TenantID
	store, err := s.personas.ForTenant(ctx, tenant)
	if err != nil || store == nil {
		return agentpersona.PersonaProfile{}, agentmanifest.Manifest{}, fmt.Errorf("%w: tenant persona store unavailable", errPersonaRunModelWork)
	}
	version, _, err := store.ReadCurrentPersonaAuthority(ctx, admission.Request.Audience.ID, admission.Request.Persona.ID)
	if err != nil || version.TenantID.String() != tenant || version.PersonaID != admission.Request.Persona.ID || version.Version <= 0 || version.AgentVersion != admission.Authority.Agent.AgentID+"@"+admission.Authority.Agent.Version || version.ContentDigest != admission.Request.Persona.Digest {
		return agentpersona.PersonaProfile{}, agentmanifest.Manifest{}, fmt.Errorf("%w: current persona version changed", errPersonaRunModelWork)
	}
	var profile agentpersona.PersonaProfile
	if json.Unmarshal(version.Profile, &profile) != nil || profile.PersonaID != admission.Request.Persona.ID || fmt.Sprint(profile.Version) != strings.TrimPrefix(admission.Request.Persona.Version, "v") || profile.Manifest.ID != admission.Authority.Agent.AgentID || profile.Manifest.Digest != admission.Authority.Agent.Digest || profile.Manifest.Version == 0 {
		return agentpersona.PersonaProfile{}, agentmanifest.Manifest{}, fmt.Errorf("%w: persona profile does not match admitted manifest", errPersonaRunModelWork)
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil || sealed.Digest != admission.Request.Persona.Digest {
		return agentpersona.PersonaProfile{}, agentmanifest.Manifest{}, fmt.Errorf("%w: persona profile digest mismatch", errPersonaRunModelWork)
	}
	resolver, err := s.manifests.ForTenant(ctx, tenant)
	if err != nil || resolver == nil || resolver.TenantID() != tenant {
		return agentpersona.PersonaProfile{}, agentmanifest.Manifest{}, fmt.Errorf("%w: tenant manifest resolver unavailable", errPersonaRunModelWork)
	}
	manifest, err := resolver.ResolveAgentManifestContext(ctx, profile.Manifest)
	if err != nil || manifest.ID != admission.Authority.Agent.AgentID || fmt.Sprint(manifest.Version) != admission.Authority.Agent.Version {
		return agentpersona.PersonaProfile{}, agentmanifest.Manifest{}, fmt.Errorf("%w: exact pinned manifest unavailable", errPersonaRunModelWork)
	}
	digest, err := manifest.Digest()
	// The sealed profile carries a domain-separated digest of its own
	// instruction text, which can never equal the manifest's plain digest.
	// What must hold is that the profile's instruction text is exactly the
	// text the pinned manifest digests.
	if err != nil || digest != admission.Authority.Agent.Digest || !personaInstructionsMatchDigest(profile.Instructions, manifest.InstructionsDigest) {
		return agentpersona.PersonaProfile{}, agentmanifest.Manifest{}, fmt.Errorf("%w: manifest or persona instruction digest mismatch", errPersonaRunModelWork)
	}
	if !validPersonaChatReplySchemaPin(manifest.OutputSchema) {
		return agentpersona.PersonaProfile{}, agentmanifest.Manifest{}, fmt.Errorf("%w: manifest output schema is not the registered T0 chat reply", errPersonaRunModelWork)
	}
	return profile, manifest, nil
}

func validPersonaChatReplySchemaPin(ref agentmanifest.Reference) bool {
	return ref.ID == PersonaChatReplySchema && ref.Version == 1 && ref.SchemaVersion == 1 && ref.Digest == PersonaChatReplySchemaDigest
}

func (s *DatabasePersonaRunModelWorkSource) resolveRoute(ctx context.Context, admission agentrun.Record, run runstate.Run, manifest agentmanifest.Manifest, persona agentpersona.PersonaProfile, budget PersonaRunEffectivePolicy) (PersonaRunModelRoute, error) {
	tenantID := admission.Request.Source.TenantID
	tenant := values.TenantId(tenantID)
	tenantKey := s.tenantUUID(tenant)
	if tenantKey == uuid.Nil {
		return PersonaRunModelRoute{}, fmt.Errorf("%w: canonical tenant unavailable", errPersonaRunModelWork)
	}
	ref := manifest.ModelPolicy
	stored, err := currentPersonaAgentModelRoute(ctx, s.routes, tenantKey, admission.Request.LegalEntity, ref, admission.Request.Agent.Digest, s.now().UTC())
	if err != nil || stored.TenantID != tenantKey || stored.LegalEntityID != admission.Request.LegalEntity || stored.PolicyID != ref.ID || stored.PolicyVersion != int64(ref.Version) || stored.PolicySchemaVersion != int64(ref.SchemaVersion) || stored.PolicyDigest != ref.Digest || stored.Revision <= 0 {
		return PersonaRunModelRoute{}, fmt.Errorf("%w: current tenant model policy unavailable", errPersonaRunModelWork)
	}
	var policy PersonaRunModelRoute
	if json.Unmarshal(stored.RoutePayload, &policy) != nil || strings.TrimSpace(policy.Purpose) == "" || policy.Route.TraceID == "" || policy.Route.Task.ID == "" || policy.Route.Pin.Primary == (agentmodel.ModelSelection{}) || len(policy.Route.Pin.Fallbacks) != 0 || policy.Route.BudgetRemainingMicros == 0 {
		return PersonaRunModelRoute{}, fmt.Errorf("%w: tenant model policy payload is invalid or permits fallback", errPersonaRunModelWork)
	}
	if policy.Route.Pin.AgentVersionDigest != personaRunManifestDigest(manifest) || policy.Route.Task.AgentVersionDigest != personaRunManifestDigest(manifest) || policy.Route.Pin.SemanticsDigest == "" || policy.Route.Pin.OutputSchemaDigest != PersonaChatReplySchemaDigest || policy.Route.Task.OutputSchemaDigest != PersonaChatReplySchemaDigest || policy.Route.Pin.ToolSchemaDigest == "" ||
		policy.Route.Task.ID != policy.Route.Pin.TaskProfileID || policy.Route.Task.Region == "" || policy.Route.Task.MaxLatency <= 0 || len(policy.Route.Task.DataClasses) == 0 ||
		policy.Egress.ID != policy.Route.Pin.Primary.ProfileID || policy.Egress.Kind != agentegress.TargetModel || policy.Egress.Retention.Mode == "" || !slices.Contains(policy.Egress.AllowedRegions, policy.Route.Task.Region) || policy.Processing.Residency != policy.Route.Task.Region ||
		policy.Processing.Retention == "" || policy.Processing.TrainingUse == agentmodel.UseUnspecified || policy.Processing.Logging == agentmodel.UseUnspecified ||
		!containsString(policy.Route.Task.DataClasses, string(policy.ProfileClass)) || !containsString(policy.Route.Task.DataClasses, string(policy.InvokerClass)) || !containsString(policy.Route.Task.DataClasses, string(policy.ThreadClass)) {
		return PersonaRunModelRoute{}, fmt.Errorf("%w: route policy does not satisfy model contract", errPersonaRunModelWork)
	}
	for _, class := range persona.DataClassesRead {
		routeClass, known := personaDataClassRouteClass(class)
		if !known || !personaRunContains(policy.Route.Task.DataClasses, routeClass) {
			return PersonaRunModelRoute{}, fmt.Errorf("%w: route excludes persona data class %s", errPersonaRunModelWork, class)
		}
	}
	ceiling := minUint(admission.Request.Budget.MaxCostMicros, budget.Budget.MaxCostMicros, uint64(manifest.Budget.MaxCostMicros))
	input := minUint(admission.Request.Budget.MaxInputTokens, budget.Budget.MaxInputTokens, uint64(manifest.Budget.MaxInputTokens))
	output := minUint(admission.Request.Budget.MaxOutputTokens, budget.Budget.MaxOutputTokens, uint64(manifest.Budget.MaxOutputTokens))
	if ceiling == 0 || input == 0 || output == 0 || ceiling > math.MaxInt64 || input > math.MaxInt64 || output > math.MaxInt64 {
		return PersonaRunModelRoute{}, fmt.Errorf("%w: effective model budget is empty", errPersonaRunModelWork)
	}
	policy.Route.TraceID = run.ID
	policy.Route.Task.AgentVersionDigest = manifestDigest(manifest)
	policy.Route.Pin.AgentVersionDigest = manifestDigest(manifest)
	// The router decides eligibility from the route's contractual cost limit.
	// What this run may still spend is the model request's own limit, which is
	// built from the remaining budget below and reserved in the ledger before
	// the call, so a continuation is not refused merely because the first
	// turn spent part of the run's budget.
	policy.Route.BudgetRemainingMicros = policy.Route.Task.MaxCostMicros
	// The task's latency limit stays the route's contractual value: the router
	// uses it to decide whether the pinned model is fast enough for this task
	// profile. The time actually left is enforced by the model request's
	// deadline (the earlier of the run and budget deadlines), so shrinking the
	// limit here would make a model whose worst case equals the run's
	// wall-clock ceiling ineligible the moment any time has passed.
	if !budget.Deadline.After(s.now().UTC()) || policy.Route.Task.MaxLatency <= 0 || !run.Deadline.After(s.now().UTC()) {
		return PersonaRunModelRoute{}, fmt.Errorf("%w: run deadline elapsed", errPersonaRunModelWork)
	}
	if policy.Route.Task.MaxCostMicros <= 0 {
		return PersonaRunModelRoute{}, fmt.Errorf("%w: no eligible model budget", errPersonaRunModelWork)
	}
	return policy, nil
}

func (s *DatabasePersonaRunModelWorkSource) readThreadContext(ctx context.Context, admission agentrun.Record, profile agentpersona.PersonaProfile) (string, []string, []agentmodel.ContextReference, error) {
	request := admission.Request
	posts, err := s.threads.ReadThread(ctx, agentinvoke.ThreadReadRequest{TenantID: request.Source.TenantID, ConversationID: request.Audience.ID, ThreadID: request.Context.ID, InvokerID: request.Principal.InvokerID, InvokingPostID: request.Source.Ref, Limit: agentinvoke.MaxThreadPosts})
	if err != nil || len(posts) == 0 || len(posts) > agentinvoke.MaxThreadPosts {
		return "", nil, nil, fmt.Errorf("%w: authorized thread context unavailable", errPersonaRunModelWork)
	}
	goal := ""
	history := make([]string, 0, len(posts))
	refs := make([]agentmodel.ContextReference, 0, len(posts))
	invokingPostSeen := false
	for _, post := range posts {
		if post.TenantID != request.Source.TenantID || post.ConversationID != request.Audience.ID || post.ThreadID != request.Context.ID || strings.TrimSpace(post.ID) == "" {
			return "", nil, nil, fmt.Errorf("%w: thread snapshot escaped admitted scope", errPersonaRunModelWork)
		}
		if !invokingPostSeen && post.ID == request.Source.Ref && post.AuthorID == request.Principal.InvokerID && !post.Bot {
			// Where the answer goes is the asker's request, not part of the
			// question: the model is asked the question (AGENTUX-070).
			goal = WithoutPrivacyRequest(post.Body)
			invokingPostSeen = true
		} else if !invokingPostSeen && post.AuthorID == request.Principal.InvokerID && !post.Bot && strings.TrimSpace(post.Body) != "" {
			history = append(history, WithoutPrivacyRequest(post.Body))
		}
		refs = append(refs, agentmodel.ContextReference{ID: post.ID, Version: "thread", Digest: digestPersonaThreadPost(post)})
	}
	if strings.TrimSpace(goal) == "" {
		return "", nil, nil, fmt.Errorf("%w: invoking user message missing from thread snapshot", errPersonaRunModelWork)
	}
	if strings.TrimSpace(profile.Instructions) == "" || profile.InstructionsDigest == "" {
		return "", nil, nil, fmt.Errorf("%w: persona instructions are unavailable", errPersonaRunModelWork)
	}
	return goal, history, refs, nil
}

func (s *DatabasePersonaRunModelWorkSource) buildExecutorRequest(ctx context.Context, admission agentrun.Record, run runstate.Run, profile agentpersona.PersonaProfile, manifest agentmanifest.Manifest, route PersonaRunModelRoute, budget PersonaRunEffectivePolicy, goal string, history []string, refs []agentmodel.ContextReference) (AgentModelExecutorRequest, error) {
	selection := route.Route.Pin.Primary
	model := buildPersonaRunModelRequest(admission, run, profile, manifest, route, budget, goal, history, refs)
	task, err := NewTrustedModelTask(run.TenantID, run.ID, run.AgentDigest, s.workload)
	if err != nil {
		return AgentModelExecutorRequest{}, err
	}
	done := agentUXSpeedEvent(ctx, "store.model_lease")
	leaseValue, err := s.leases.Issue(ctx, ModelLeaseRequest{Task: task, ProviderID: selection.Identity.ProviderID, Destination: selection.ProfileID, Purpose: route.Purpose, Region: route.Route.Task.Region, TTL: time.Minute})
	done()
	if err != nil {
		return AgentModelExecutorRequest{}, fmt.Errorf("%w: model credential lease unavailable", errPersonaRunModelWork)
	}
	route.Route.TraceID = run.ID
	fields, sources := personaRunModelFields(model)
	return AgentModelExecutorRequest{Task: task, StepID: run.ID, ToolResultClass: route.ThreadClass, Route: route.Route, Model: model, FieldSources: sources,
		Outbound: agentegress.OutboundRequest{TaskID: run.ID, Tenant: run.TenantID, Principal: admission.Request.Principal.InvokerID, Purpose: route.Purpose,
			Profile: route.Egress, Region: route.Route.Task.Region, DeclaredFields: fields,
			Fields: personaRunModelOutboundFields(model, route), Task: route.TaskPolicy, Now: s.now().UTC()}, Lease: leaseValue}, nil
}

func buildPersonaRunModelRequest(admission agentrun.Record, run runstate.Run, profile agentpersona.PersonaProfile, manifest agentmanifest.Manifest, route PersonaRunModelRoute, budget PersonaRunEffectivePolicy, goal string, history []string, refs []agentmodel.ContextReference) agentmodel.ModelRequest {
	selection := route.Route.Pin.Primary
	model := agentmodel.ModelRequest{ContractVersion: agentmodel.ContractVersion, TaskProfile: route.Route.Task.ID, ModelProfile: selection.ProfileID,
		Messages:    []agentmodel.ModelMessage{{Role: agentmodel.RoleSystem, Content: manifest.Purpose}, {Role: agentmodel.RoleDeveloper, Content: personaDeveloperMessage(profile)}},
		ContextRefs: refs, Output: agentmodel.OutputConstraint{Mode: agentmodel.OutputText}, Deadline: minTime(run.Deadline, budget.Deadline),
		Limits:  agentmodel.ModelLimits{MaxInputTokens: int64(minUint(admission.Request.Budget.MaxInputTokens, budget.Budget.MaxInputTokens, uint64(manifest.Budget.MaxInputTokens))), MaxOutputTokens: int64(minUint(admission.Request.Budget.MaxOutputTokens, budget.Budget.MaxOutputTokens, uint64(manifest.Budget.MaxOutputTokens))), MaxCostMicros: int64(minUint(admission.Request.Budget.MaxCostMicros, budget.Budget.MaxCostMicros, uint64(manifest.Budget.MaxCostMicros)))},
		TraceID: run.ID, Processing: route.Processing}
	if profile.EvalSuiteRef == "AGENTP-021.policy_helper" {
		for _, pin := range profile.SkillPins {
			if pin.ID == personaPolicyHelperSkillID {
				model.ActionPolicy = &agentmodel.RequestedActionPolicy{ProfileDigest: admission.Request.Persona.Digest, Allowed: []agentmodel.RequestedAction{agentmodel.ActionReadPolicy}}
			}
		}
	}
	for _, item := range history {
		model.Messages = append(model.Messages, agentmodel.ModelMessage{Role: agentmodel.RoleUser, Content: item})
	}
	model.Messages = append(model.Messages, agentmodel.ModelMessage{Role: agentmodel.RoleUser, Content: goal})
	return model
}

func personaRunManifestDigest(manifest agentmanifest.Manifest) string {
	digest, _ := manifest.Digest()
	return digest
}
func minUint(values ...uint64) uint64 {
	var result uint64
	for i, value := range values {
		if i == 0 || value < result {
			result = value
		}
	}
	return result
}
func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// personaDataClassRouteClass maps a persona's domain data label to the
// sensitivity class a model route must admit before content of that kind may
// be sent to the model. Route task profiles list sensitivity classes
// (trustdlp), while skills declare domain labels; a label that is itself a
// sensitivity class maps to itself. A label with no mapping is refused, so a
// new domain label cannot reach a model until its sensitivity is decided here.
func personaDataClassRouteClass(class string) (string, bool) {
	if class == personaPolicySearchDataClass {
		// Policy documents are company-internal reference content.
		return string(trustdlp.ClassInternal), true
	}
	if trustdlp.DataClass(class).Valid() {
		return class, true
	}
	return "", false
}

func personaRunContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func digestPersonaThreadPost(post agentinvoke.ThreadPost) string {
	sum := sha256.Sum256([]byte(post.Body))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func personaRunModelFields(model agentmodel.ModelRequest) ([]string, map[string]string) {
	fields := make([]string, 0, len(model.Messages)+len(model.ContextRefs))
	sources := make(map[string]string, cap(fields))
	for i := range model.Messages {
		name := fmt.Sprintf("model.message.%d", i)
		fields = append(fields, name)
		switch model.Messages[i].Role {
		case agentmodel.RoleUser:
			sources[name] = "persona-invoking-post"
		case agentmodel.RoleTool:
			sources[name] = "persona-untrusted-tool-result"
		case agentmodel.RoleAssistant:
			if model.Messages[i].ToolCallID != "" {
				sources[name] = "persona-model-tool-proposal"
			} else {
				sources[name] = "persona-profile"
			}
		default:
			sources[name] = "persona-profile"
		}
	}
	for i := range model.ContextRefs {
		name := fmt.Sprintf("model.context.%d", i)
		fields = append(fields, name)
		sources[name] = "persona-thread-context"
	}
	return fields, sources
}

func personaRunModelOutboundFields(model agentmodel.ModelRequest, route PersonaRunModelRoute) []agentegress.Field {
	fields := make([]agentegress.Field, 0, len(model.Messages)+len(model.ContextRefs))
	for i, message := range model.Messages {
		name := fmt.Sprintf("model.message.%d", i)
		class := route.ProfileClass
		referenceDocument := strings.HasPrefix(message.Content, agentDocumentReferenceDataBegin+"\n")
		if referenceDocument {
			// Reference documents follow the same class as the already-reviewed
			// policy-search tool result: they are untrusted document evidence,
			// never persona profile instructions.
			class = route.ThreadClass
		} else if message.Role == agentmodel.RoleUser || (message.Role == agentmodel.RoleAssistant && message.ToolCallID != "") {
			class = route.InvokerClass
		}
		value := any(message.Content)
		taint := []string{"PERSONA_PROFILE"}
		if referenceDocument {
			taint = []string{"UNTRUSTED_REFERENCE_DOCUMENT"}
		} else if message.Role == agentmodel.RoleUser {
			taint = []string{"PERSONA_INVOKING_POST"}
		}
		if message.Role == agentmodel.RoleAssistant && message.ToolCallID != "" {
			value = string(message.ToolArguments)
			taint = []string{"MODEL_TOOL_PROPOSAL"}
		} else if message.Role == agentmodel.RoleTool {
			class = route.ThreadClass
			taint = []string{"UNTRUSTED_TOOL_RESULT"}
		}
		fields = append(fields, agentegress.Field{Name: name, Value: value, Class: class, Taint: taint, Provenance: []string{"persona-run:" + model.TraceID}})
	}
	for i, ref := range model.ContextRefs {
		name := fmt.Sprintf("model.context.%d", i)
		value := fmt.Sprintf("%s\x00%s\x00%s", ref.ID, ref.Version, ref.Digest)
		class := route.ThreadClass
		taint := []string{"UNTRUSTED_THREAD_REFERENCE"}
		if strings.HasPrefix(ref.ID, "document:") {
			class = route.ThreadClass
			taint = []string{"UNTRUSTED_REFERENCE_DOCUMENT"}
		}
		fields = append(fields, agentegress.Field{Name: name, Value: value, Class: class, Taint: taint, Provenance: []string{"persona-run:" + model.TraceID}})
	}
	return fields
}
