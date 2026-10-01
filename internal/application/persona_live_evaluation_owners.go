package application

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// PersonaCandidateCurrentOwners is the evaluator-only current authority. Its
// placement whitelist belongs to the reviewed synthetic deployment, while
// membership, human authority, skill pins, delegation and run facts are read
// from the same real owner stores used by ordinary execution.
type PersonaCandidateCurrentOwners struct {
	Definitions     *PersonaCandidateDefinitionSource
	OwnerFacts      PersonaRunOwnerFactsSource
	Invokers        PersonaChatInvokerAuthorityResolver
	Grants          PersonaGrantTenantStoreFactory
	Catalog         PersonaT0SkillCatalog
	Chat            chatcore.ConversationService
	Placements      map[string]string
	Documents       *documenthubstore.Store
	RequiredSources map[string][]string
	Now             func() time.Time
}

func (o *PersonaCandidateCurrentOwners) placement(ctx context.Context, target agenteval.PersonaEvaluationTarget, conversation, installation string) error {
	if o == nil || ctx == nil || o.Definitions == nil || o.OwnerFacts == nil || o.Invokers == nil || o.Grants == nil || o.Catalog == nil || o.Chat == nil || o.Now == nil || target != o.Definitions.Target || o.Placements[conversation] != installation || !required(installation) {
		return agenteval.ErrPersonaEvaluation
	}
	if _, _, _, err := o.Definitions.Resolve(ctx); err != nil {
		return err
	}
	principal, ok := trust.FromContext(ctx)
	now := o.Now().UTC()
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant().String() != target.SyntheticTenantID || principal.Subject() != target.InvokerID || !principal.AuthorizesPurpose("persona-mention") || principal.IssuedAt().After(now) || !principal.ExpiresAt().After(now) {
		return agenteval.ErrPersonaEvaluation
	}
	chatPrincipal, ok := personaRunChatPrincipal(ctx, target.SyntheticTenantID, target.InvokerID)
	if !ok {
		return agenteval.ErrPersonaEvaluation
	}
	room, err := o.Chat.GetConversation(ctx, chatcore.GetConversationRequest{Principal: chatPrincipal, TenantID: target.SyntheticTenantID, ConversationID: conversation})
	if err != nil || room.TenantID != target.SyntheticTenantID || room.ID != conversation || room.Archived {
		return agenteval.ErrPersonaEvaluation
	}
	return nil
}

func (o *PersonaCandidateCurrentOwners) ResolvePersonaEvaluationSkills(ctx context.Context, target agenteval.PersonaEvaluationTarget, conversation, installation, thread, post string) (agentinvoke.SkillScopes, error) {
	if err := o.placement(ctx, target, conversation, installation); err != nil {
		return nil, err
	}
	_, profile, _, err := o.Definitions.Resolve(ctx)
	if err != nil {
		return nil, err
	}
	current, err := o.currentInvokerAuthority(ctx, target, conversation, thread, post)
	if err != nil || !current.Active || current.UserID != target.InvokerID || current.Authority.Tenant.String() != target.SyntheticTenantID || !current.Authority.ExpiresAt.After(o.Now().UTC()) {
		return nil, agenteval.ErrPersonaEvaluation
	}
	scopes := agentinvoke.SkillScopes{}
	for _, pin := range profile.SkillPins {
		record, err := o.Catalog.ResolvePin(pin)
		if err != nil || record.Status != agentskills.StatusActive || record.Definition.SideEffectTier != agentskills.TierT0 || record.HighestCapabilityTier != agentskills.TierT0 || record.Digest != pin.Digest {
			return nil, agenteval.ErrPersonaEvaluation
		}
		capabilities, err := exactCapabilityScopes(record)
		if err != nil {
			return nil, err
		}
		authority, ok := current.SkillAuthorities[pin.ID]
		if ok && scopeSubset(capabilities, authority.Capabilities) && slices.Contains(authority.Purposes, "persona-mention") {
			scopes[pin.ID] = slices.Clone(capabilities)
		}
	}
	if len(scopes) == 0 {
		return nil, agenteval.ErrPersonaEvaluation
	}
	return scopes, nil
}

func (o *PersonaCandidateCurrentOwners) currentInvokerAuthority(ctx context.Context, target agenteval.PersonaEvaluationTarget, conversation, thread, post string) (agentdelegation.UserAuthority, error) {
	if o == nil || o.Invokers == nil || o.Definitions == nil || o.Now == nil || target != o.Definitions.Target {
		return agentdelegation.UserAuthority{}, agenteval.ErrPersonaEvaluation
	}
	_, profile, _, err := o.Definitions.Resolve(ctx)
	if err != nil {
		return agentdelegation.UserAuthority{}, err
	}
	return o.Invokers.ResolvePersonaChatInvokerAuthority(ctx, PersonaChatAuthorityRequest{Tenant: values.TenantId(target.SyntheticTenantID),
		InvokerID: target.InvokerID, Purpose: "persona-mention", ConversationID: conversation, ThreadID: thread, InvokingPostID: post,
		Pins: profile.SkillPins, At: o.Now().UTC()})
}

func (o *PersonaCandidateCurrentOwners) CreateOnBehalfOfGrant(ctx context.Context, request agentinvoke.GrantRequest) (agentinvoke.DelegationGrant, error) {
	if o == nil || o.Definitions == nil {
		return agentinvoke.DelegationGrant{}, agenteval.ErrPersonaEvaluation
	}
	target := o.Definitions.Target
	if request.Mode != agentinvoke.OnBehalfOf || request.TenantID != target.SyntheticTenantID || request.UserID != target.InvokerID || request.PersonaID != target.PersonaID || !personaRunVersionMatches(target.PersonaVersion, request.PersonaVersion) || request.AgentVersion != strconv.FormatInt(target.PersonaVersion, 10) || request.Purpose != "persona-mention" || !required(request.InvocationID) || !required(request.InvokingPostID) || !required(request.ThreadID) {
		return agentinvoke.DelegationGrant{}, agenteval.ErrPersonaEvaluation
	}
	effective, err := o.ResolvePersonaEvaluationSkills(ctx, target, request.ConversationID, request.InstallationID, request.ThreadID, request.InvokingPostID)
	if err != nil || !sameSkillScopes(effective, request.Skills) {
		return agentinvoke.DelegationGrant{}, agenteval.ErrPersonaEvaluation
	}
	_, _, manifest, err := o.Definitions.Resolve(ctx)
	if err != nil || manifest.ID != request.TargetAgentID {
		return agentinvoke.DelegationGrant{}, agenteval.ErrPersonaEvaluation
	}
	current, err := o.currentInvokerAuthority(ctx, target, request.ConversationID, request.ThreadID, request.InvokingPostID)
	if err != nil {
		return agentinvoke.DelegationGrant{}, err
	}
	store, err := o.Grants.ForTenant(ctx, values.TenantId(target.SyntheticTenantID))
	if err != nil {
		return agentinvoke.DelegationGrant{}, err
	}
	service, err := agentdelegation.NewService(agentdelegation.Config{Store: store, Authority: agentdelegation.ResolverFunc(unusedPersonaGrantResolver), Now: o.Now})
	if err != nil {
		return agentinvoke.DelegationGrant{}, err
	}
	authority := current.Authority
	authority.SkillAuthorities = trust.CloneSkillAuthorities(current.SkillAuthorities)
	// The durable PostgreSQL grant stores microsecond timestamps. Canonicalize
	// before issuance so its exact reread compares the same requested expiry.
	request.ExpiresAt = request.ExpiresAt.UTC().Truncate(time.Microsecond)
	narrowed := agentdelegation.SkillAuthorities{}
	for skill, scopes := range effective {
		value := current.SkillAuthorities[skill]
		value.Capabilities = slices.Clone(scopes)
		narrowed[skill] = value
	}
	grant, err := service.CreateScopedGrant(agentdelegation.GrantRequest{GrantID: personaGrantID(request), UserID: target.InvokerID, Tenant: values.TenantId(target.SyntheticTenantID), AgentVersion: request.AgentVersion, TargetAgentID: request.TargetAgentID, InstallationID: request.InstallationID, TaskID: request.InvocationID, PlanSkillSetDigest: skillDigest(effective), Purpose: request.Purpose, OrganizationScopeID: current.Authority.OrganizationScopeID, Skills: sortedKeys(effective), SkillScopes: effective.Clone(), NotBefore: personaGrantNotBefore(o.Now()), ExpiresAt: request.ExpiresAt, UserAuthority: authority, SkillAuthorities: narrowed})
	if err != nil {
		return agentinvoke.DelegationGrant{}, err
	}
	stored, err := store.Get(grant.GrantID)
	if err != nil || !grantMatchesRequest(stored, request, grant.GrantID, skillDigest(effective), effective, narrowed, current.Authority.OrganizationScopeID, store.CurrentRevocationEpoch(grant.Tenant, grant.UserID)) {
		return agentinvoke.DelegationGrant{}, agenteval.ErrPersonaEvaluation
	}
	return agentinvoke.DelegationGrant{ID: stored.GrantID, UserID: stored.UserID, TenantID: stored.Tenant.String(), TaskID: stored.TaskID, TargetAgentID: stored.TargetAgentID, Skills: cloneSkillScopes(stored.SkillScopes), ExpiresAt: stored.ExpiresAt}, nil
}

func (o *PersonaCandidateCurrentOwners) VerifyAdmission(ctx context.Context, request agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	if o == nil || o.Definitions == nil || !validPersonaRunAdmissionRequest(request) {
		return agentrun.AuthoritySnapshot{}, agenteval.ErrPersonaEvaluation
	}
	target := o.Definitions.Target
	if request.Source.TenantID != target.SyntheticTenantID || request.Principal.InvokerID != target.InvokerID || request.Persona.ID != target.PersonaID || request.Persona.Digest != target.ProfileDigest || !personaRunVersionMatches(target.PersonaVersion, request.Persona.Version) {
		return agentrun.AuthoritySnapshot{}, agenteval.ErrPersonaEvaluation
	}
	if err := o.placement(ctx, target, request.Audience.ID, request.InstallationID); err != nil {
		return agentrun.AuthoritySnapshot{}, err
	}
	// Each fixture placement pins real official sources. Their current document
	// read grants are checked independently before inference or tool execution.
	sources := o.RequiredSources[request.Audience.ID]
	if o.Documents == nil || len(sources) == 0 {
		return agentrun.AuthoritySnapshot{}, agenteval.ErrPersonaEvaluation
	}
	for _, documentID := range sources {
		placement, err := o.Documents.CurrentPlacement(ctx, target.SyntheticTenantID, documentID, "placement", request.Audience.ID)
		if err != nil || !placement.IsOfficialPlacement() {
			return agentrun.AuthoritySnapshot{}, agenteval.ErrPersonaEvaluation
		}
		if err := o.Documents.Authorize(ctx, target.SyntheticTenantID, documentID, "person", target.InvokerID, documenthubstore.ActionRead); err != nil {
			if errors.Is(err, documenthubstore.ErrDenied) {
				return agentrun.AuthoritySnapshot{}, &agentrun.AdmissionRefusal{Code: "AUTHORITY_DENIED"}
			}
			return agentrun.AuthoritySnapshot{}, err
		}
	}
	store, err := o.Grants.ForTenant(ctx, values.TenantId(target.SyntheticTenantID))
	if err != nil {
		return agentrun.AuthoritySnapshot{}, err
	}
	grant, err := store.Get(request.Principal.DelegatedCredentialRef)
	now := o.Now().UTC()
	if err != nil || !personaForegroundGrantEnvelope(grant, request, now, store.CurrentRevocationEpoch(grant.Tenant, grant.UserID)) {
		return agentrun.AuthoritySnapshot{}, &agentrun.AdmissionRefusal{Code: "AUTHORITY_DENIED"}
	}
	invocation := personaRunInvocation(request)
	bindForegroundGrant(&invocation, grant)
	skills, err := o.ResolvePersonaEvaluationSkills(ctx, target, invocation.ConversationID, invocation.InstallationID, invocation.ThreadID, invocation.InvokingPostID)
	if err != nil || !sameSkillScopes(skills, invocation.Skills) {
		return agentrun.AuthoritySnapshot{}, &agentrun.AdmissionRefusal{Code: "AUTHORITY_DENIED"}
	}
	current, err := o.currentInvokerAuthority(ctx, target, invocation.ConversationID, invocation.ThreadID, invocation.InvokingPostID)
	// The current projection starts at its own read time, after the grant was
	// checked. Compare it with the clock after that read, not the earlier one.
	now = o.Now().UTC()
	if err != nil || !currentGrantAuthorityCovers(current, invocation, now) || !personaT0StoredAuthorityCurrent(current, grant, invocation) {
		return agentrun.AuthoritySnapshot{}, &agentrun.AdmissionRefusal{Code: "AUTHORITY_DENIED"}
	}
	builder, err := NewPersonaRunRequestBuilder(&PersonaCandidateRunRequestSource{Definitions: o.Definitions, OwnerFacts: o.OwnerFacts})
	if err != nil {
		return agentrun.AuthoritySnapshot{}, err
	}
	fresh, err := builder.BuildPersonaChatAdmission(ctx, invocation)
	if err != nil || !personaForegroundRequestMatches(request, fresh, now) {
		return agentrun.AuthoritySnapshot{}, agenteval.ErrPersonaEvaluation
	}
	principal, _ := trust.FromContext(ctx)
	digest, err := personaForegroundPolicyDigest(request, grant, store.CurrentRevocationEpoch(grant.Tenant, grant.UserID), principal.Fingerprint())
	if err != nil {
		return agentrun.AuthoritySnapshot{}, err
	}
	return agentrun.AuthoritySnapshot{Agent: fresh.Agent, InstallationID: fresh.InstallationID, Principal: fresh.Principal, Audience: fresh.Audience, Context: fresh.Context, BudgetCeiling: fresh.Budget, GrantRef: grant.GrantID, PolicyDigest: digest}, nil
}

func (o *PersonaCandidateCurrentOwners) ResolvePersonaRuntimeToolPins(ctx context.Context, record agentrun.Record, run runstate.Run) ([]agentskills.SkillPin, error) {
	if validatePersonaModelWorkBinding(record, run) != nil {
		return nil, agenteval.ErrPersonaEvaluation
	}
	current, err := o.VerifyAdmission(ctx, record.Request)
	if err != nil || !reflect.DeepEqual(current, record.Authority) {
		return nil, agenteval.ErrPersonaEvaluation
	}
	_, profile, _, err := o.Definitions.Resolve(ctx)
	if err != nil {
		return nil, err
	}
	store, err := o.Grants.ForTenant(ctx, values.TenantId(run.TenantID))
	if err != nil {
		return nil, err
	}
	grant, err := store.Get(record.Request.Principal.DelegatedCredentialRef)
	if err != nil {
		return nil, err
	}
	if !pinnedT0ScopesMatch(o.Catalog, profile.SkillPins, grant.SkillScopes) {
		return nil, agenteval.ErrPersonaEvaluation
	}
	pins := []agentskills.SkillPin{}
	for _, pin := range profile.SkillPins {
		if len(grant.SkillScopes[pin.ID]) > 0 {
			pins = append(pins, pin)
		}
	}
	return pins, nil
}

func (o *PersonaCandidateCurrentOwners) ResolvePersonaDocumentSearchScope(ctx context.Context, id PersonaRunT0ToolInvocation) (PersonaDocumentSearchScope, error) {
	if o == nil || o.Definitions == nil || ctx == nil {
		return PersonaDocumentSearchScope{}, agenteval.ErrPersonaEvaluation
	}
	request, ok := ctx.Value(personaBackgroundAdmissionKey{}).(agentrun.Request)
	if !ok || request.Persona == nil || request.Source.TenantID != id.TenantID || request.Source.Key != id.InvocationID || request.Principal.InvokerID != id.InvokerID || request.Audience.ID != id.ConversationID || request.Context.ID != id.ThreadID || request.InstallationID != id.InstallationID || request.Persona.ID != id.PersonaID || request.Persona.Version != id.PersonaVersion || request.Agent.AgentID != id.AgentID {
		return PersonaDocumentSearchScope{}, agenteval.ErrPersonaEvaluation
	}
	if _, err := o.VerifyAdmission(ctx, request); err != nil {
		return PersonaDocumentSearchScope{}, err
	}
	return PersonaDocumentSearchScope{ScopeID: id.ConversationID}, nil
}

var _ agentrun.Authority = (*PersonaCandidateCurrentOwners)(nil)
var _ agentinvoke.GrantIssuer = (*PersonaCandidateCurrentOwners)(nil)
var _ PersonaLiveCaseSkillSource = (*PersonaCandidateCurrentOwners)(nil)
var _ PersonaRuntimeToolPinAuthority = (*PersonaCandidateCurrentOwners)(nil)
var _ PersonaDocumentSearchScopeSource = (*PersonaCandidateCurrentOwners)(nil)
