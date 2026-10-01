package application

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentcontextstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// PersonaCheckpointThreadSnapshotSource retains authenticated owner captures.
// The retained identity fields are never installed into a trust context.
type PersonaCheckpointThreadSnapshotSource struct {
	Source   PersonaThreadSnapshotSource
	Contexts *agentcontextstore.Store
}

func (s PersonaCheckpointThreadSnapshotSource) CaptureThreadSnapshot(ctx context.Context, req chat.ThreadSnapshotRequest) (chat.ThreadSnapshot, error) {
	if isNilPersonaOutputPort(s.Source) || s.Contexts == nil {
		return chat.ThreadSnapshot{}, errPersonaRunCurrentAuthority
	}
	snapshot, err := s.Source.CaptureThreadSnapshot(ctx, req)
	if err != nil {
		return snapshot, err
	}
	if err = s.Contexts.Put(ctx, snapshot); err != nil {
		return chat.ThreadSnapshot{}, err
	}
	return snapshot, nil
}

// PersonaBackgroundRuntimeConfig requires actual current owner stores and a
// deployment-held workload credential. It contains no reconstructed HUMAN identity.
type PersonaBackgroundRuntimeConfig struct {
	Foreground        agentrun.Authority
	ForegroundThreads agentinvoke.ThreadReader
	CoreDB            dbport.Beginner
	Agents            *agentstore.Store
	Personas          personaRunAuthorityFactory
	Manifests         personaRunManifestResolverFactory
	Grants            PersonaGrantTenantStoreFactory
	Skills            *AgentSkillSource
	Contexts          *agentcontextstore.Store
	Audience          PersonaBackgroundAudienceSnapshotSource
	Threads           chat.BackgroundThreadSnapshotSource
	Principal         personaRunAgentPrincipalResolver
	Budgets           *PersonaRunEffectivePolicyResolver
	Worker            PersonaPrivateChatWorkloadIdentitySource
	TenantUUID        func(values.TenantId) uuid.UUID
	Now               func() time.Time
}

// PersonaBackgroundRuntime rechecks previously accepted ON_BEHALF_OF runs.
// New admissions still require the foreground authority and verified human.
type PersonaBackgroundRuntime struct {
	cfg PersonaBackgroundRuntimeConfig
}

func NewPersonaBackgroundRuntime(cfg PersonaBackgroundRuntimeConfig) (*PersonaBackgroundRuntime, error) {
	if isNilPersonaOutputPort(cfg.Foreground) || isNilPersonaOutputPort(cfg.ForegroundThreads) || cfg.CoreDB == nil || cfg.Agents == nil || cfg.Personas == nil || cfg.Manifests == nil || cfg.Grants == nil || cfg.Skills == nil || cfg.Skills.grants == nil || cfg.Contexts == nil || cfg.Audience == nil || cfg.Threads == nil || cfg.Principal == nil || cfg.Budgets == nil || isNilPersonaOutputPort(cfg.Worker) || cfg.TenantUUID == nil || cfg.Now == nil {
		return nil, errPersonaRunCurrentAuthority
	}
	return &PersonaBackgroundRuntime{cfg: cfg}, nil
}

func (r *PersonaBackgroundRuntime) VerifyAdmission(ctx context.Context, req agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	if r == nil || ctx == nil {
		return agentrun.AuthoritySnapshot{}, errPersonaRunCurrentAuthority
	}
	if _, ok := trust.FromContext(ctx); ok {
		return r.cfg.Foreground.VerifyAdmission(ctx, req)
	}
	return (&DatabasePersonaRunAdmissionAuthority{Store: r}).VerifyAdmission(ctx, req)
}

func (r *PersonaBackgroundRuntime) ForTenant(ctx context.Context, tenant string) (PersonaRunCurrentAuthorityReader, error) {
	if r == nil || ctx == nil || values.TenantId(tenant).Validate() != nil || r.cfg.TenantUUID(values.TenantId(tenant)) == uuid.Nil {
		return nil, errPersonaRunCurrentAuthority
	}
	return personaBackgroundCurrentReader{runtime: r, tenant: tenant}, nil
}

// IsBoundT0Run authorizes only an existing exact accepted invocation under
// current workload, delegation, directory, skill, and native-owner checks.
func (r *PersonaBackgroundRuntime) IsBoundT0Run(ctx context.Context, invocation agentinvoke.RunRequest) (bool, error) {
	if r == nil || ctx == nil || !validPersonaT0DynamicRequest(invocation) {
		return false, errPersonaRunCurrentAuthority
	}
	id, err := agentrun.AdmissionRequestID(agentrun.SourceIdentity{TenantID: invocation.TenantID, Kind: agentrun.SourcePersonaMention, Key: invocation.InvocationID, Ref: invocation.InvokingPostID})
	if err != nil {
		return false, err
	}
	tenant := values.TenantId(invocation.TenantID)
	store, err := agentrunstore.NewAdmissionRepository(r.cfg.Agents, r.cfg.TenantUUID(tenant), tenant)
	if err != nil {
		return false, err
	}
	record, err := store.GetByID(ctx, id)
	if err != nil || record.Request.Persona == nil {
		return false, errPersonaRunCurrentAuthority
	}
	current, err := r.current(ctx, record.Request)
	if err != nil {
		return false, err
	}
	grants, err := r.cfg.Grants.ForTenant(ctx, tenant)
	if err != nil {
		return false, err
	}
	grant, err := grants.Get(current.Request.Principal.DelegatedCredentialRef)
	if err != nil {
		return false, err
	}
	expected := personaRunInvocation(current.Request)
	bindForegroundGrant(&expected, grant)
	if !reflect.DeepEqual(invocation, expected) {
		return false, errPersonaRunCurrentAuthority
	}
	return true, nil
}

type personaBackgroundCurrentReader struct {
	runtime *PersonaBackgroundRuntime
	tenant  string
}

func (s personaBackgroundCurrentReader) ReadCurrentPersonaRunAuthority(ctx context.Context, req agentrun.Request) (PersonaRunCurrentAuthorityFacts, error) {
	r := s.runtime
	if r == nil || req.Source.TenantID != s.tenant || !validPersonaRunAdmissionRequest(req) {
		return PersonaRunCurrentAuthorityFacts{}, errPersonaRunCurrentAuthority
	}
	record, err := r.current(ctx, req)
	if err != nil {
		return PersonaRunCurrentAuthorityFacts{}, err
	}
	a := record.Authority
	return PersonaRunCurrentAuthorityFacts{TenantID: s.tenant, Persona: *req.Persona, Agent: a.Agent, InstallationID: a.InstallationID, Principal: a.Principal, Audience: a.Audience, Context: a.Context, BudgetCeiling: a.BudgetCeiling, GrantRef: a.GrantRef, PolicyDigest: a.PolicyDigest}, nil
}

func (r *PersonaBackgroundRuntime) current(ctx context.Context, req agentrun.Request) (agentrun.Record, error) {
	denied := func(detail string) (agentrun.Record, error) {
		return agentrun.Record{}, fmt.Errorf("%w: %s", agentrun.ErrAuthorityRefusal, detail)
	}
	now := r.cfg.Now().UTC()
	worker, err := r.cfg.Worker.ResolvePersonaChatWorker(ctx)
	if err != nil || !privatePersonaReplyWorkerMatches(worker, now) || !req.Deadline.After(now) {
		return denied("current worker or deadline")
	}
	tenant := values.TenantId(req.Source.TenantID)
	repo, err := agentrunstore.NewAdmissionRepository(r.cfg.Agents, r.cfg.TenantUUID(tenant), tenant)
	if err != nil {
		return denied("tenant admission store")
	}
	id, err := agentrun.AdmissionRequestID(req.Source)
	if err != nil {
		return denied("source identity")
	}
	record, err := repo.GetByID(ctx, id)
	if err != nil || record.Decision != agentrun.DecisionAccepted || !reflect.DeepEqual(record.Request, req) {
		return denied("durable accepted request")
	}
	grants, err := r.cfg.Grants.ForTenant(ctx, tenant)
	if err != nil || isNilPersonaOutputPort(grants) {
		return denied("current delegation store")
	}
	grant, err := grants.Get(req.Principal.DelegatedCredentialRef)
	epoch := grants.CurrentRevocationEpoch(tenant, req.Principal.InvokerID)
	if err != nil || !personaForegroundGrantEnvelope(grant, req, now, epoch) {
		return denied("current exact delegation")
	}
	invocation := personaRunInvocation(req)
	bindForegroundGrant(&invocation, grant)
	source := &DatabasePersonaRunRequestSource{Personas: r.cfg.Personas, Manifests: r.cfg.Manifests, OwnerFacts: personaBackgroundOwnerFacts{runtime: r, request: req}}
	facts, err := source.ResolvePersonaRun(ctx, invocation)
	if err != nil || facts.Agent != req.Agent || facts.PersonaDigest != req.Persona.Digest || facts.AgentPrincipal != req.Principal.AgentPrincipalID || facts.LegalEntityID != req.LegalEntity || facts.Audience != req.Audience || facts.Context != req.Context || facts.Deadline.Before(req.Deadline) || !personaRunBudgetWithin(req.Budget, facts.Budget) || !personaRunBudgetWithin(record.Authority.BudgetCeiling, facts.Budget) {
		return denied("current persona or owner scope")
	}
	if err = r.currentSkills(ctx, req, grant); err != nil {
		return denied("current role, population, organization, skill or capability grant")
	}
	return record, nil
}

type personaBackgroundOwnerFacts struct {
	runtime *PersonaBackgroundRuntime
	request agentrun.Request
}

func (s personaBackgroundOwnerFacts) ResolvePersonaRunOwnerFacts(ctx context.Context, i agentinvoke.RunRequest) (PersonaRunOwnerFacts, error) {
	r := s.runtime
	req := s.request
	legal, err := r.currentLegalEntity(ctx, req)
	if err != nil {
		return PersonaRunOwnerFacts{}, err
	}
	version, err := personaRunVersionNumber(i.PersonaVersion)
	if err != nil {
		return PersonaRunOwnerFacts{}, err
	}
	principal, err := r.cfg.Principal.Resolve(ctx, values.TenantId(i.TenantID), i.PersonaID, version)
	if err != nil {
		return PersonaRunOwnerFacts{}, err
	}
	snapshot, err := r.currentContext(ctx, req)
	if err != nil {
		return PersonaRunOwnerFacts{}, err
	}
	audience, err := r.currentAudience(ctx, req)
	if err != nil || audience != req.Audience {
		return PersonaRunOwnerFacts{}, errPersonaRunCurrentAuthority
	}
	policy, err := r.cfg.Budgets.Resolve(ctx, i.TenantID, legal)
	if err != nil {
		return PersonaRunOwnerFacts{}, err
	}
	return PersonaRunOwnerFacts{LegalEntityID: legal, AgentPrincipal: principal, Audience: audience, Context: agentrun.ContextScope{ID: snapshot.ThreadID, SnapshotID: snapshot.SnapshotID, Digest: snapshot.Digest}, Deadline: policy.Deadline, Budget: policy.Budget}, nil
}

func (r *PersonaBackgroundRuntime) currentAudience(ctx context.Context, req agentrun.Request) (agentrun.AudienceScope, error) {
	// Public-channel admission is a distinct complete owner snapshot, including
	// eligible future readers. It is never replaced with current membership.
	reader, ok := r.cfg.Threads.(interface {
		GetConversation(context.Context, string, string) (chat.Conversation, error)
	})
	if !ok {
		return agentrun.AudienceScope{}, errPersonaRunCurrentAuthority
	}
	conversation, err := reader.GetConversation(ctx, req.Source.TenantID, req.Audience.ID)
	if err != nil || conversation.TenantID != req.Source.TenantID || conversation.ID != req.Audience.ID || conversation.Archived || conversation.Revision == 0 {
		return agentrun.AudienceScope{}, errPersonaRunCurrentAuthority
	}
	if conversation.Kind == chat.PublicChannel {
		owner, ok := r.cfg.Audience.(personaPublicAudienceStore)
		if !ok {
			return agentrun.AudienceScope{}, errPersonaRunCurrentAuthority
		}
		snapshot, err := (&PersonaPublicAudienceAuthority{Store: owner}).ReadPersonaAudienceFloorSnapshot(ctx, conversation)
		if err != nil || !validPersonaRunAudienceSnapshot(snapshot, conversation) || !personaRunAudienceContainsInvoker(snapshot.CurrentMembers, personaRunInvocation(req)) {
			return agentrun.AudienceScope{}, errPersonaRunCurrentAuthority
		}
		digest, err := personaRunAudienceDigest(conversation, snapshot)
		if err != nil {
			return agentrun.AudienceScope{}, err
		}
		return agentrun.AudienceScope{ID: conversation.ID, SnapshotID: "chat-audience-" + strings.TrimPrefix(digest, "sha256:"), Digest: digest}, nil
	}
	if !privatePersonaConversation(conversation.Kind) {
		return agentrun.AudienceScope{}, errPersonaRunCurrentAuthority
	}
	audience, err := r.cfg.Audience.CaptureAudienceSnapshot(ctx, req.Source.TenantID, req.Audience.ID)
	if err != nil || audience.TenantID != req.Source.TenantID || audience.ConversationID != req.Audience.ID || !privatePersonaConversation(chat.ConversationKind(audience.Kind)) || audience.ConversationRevision <= 0 || audience.PolicyRevision <= 0 {
		return agentrun.AudienceScope{}, errPersonaRunCurrentAuthority
	}
	found := false
	for _, member := range audience.Members {
		if member.HomeTenantID == req.Source.TenantID && member.MemberID == req.Principal.InvokerID && member.Revision > 0 && !member.External {
			found = true
		}
	}
	if !found {
		return agentrun.AudienceScope{}, errPersonaRunCurrentAuthority
	}
	return agentrun.AudienceScope{ID: audience.ConversationID, SnapshotID: audience.SnapshotID, Digest: audience.Digest}, nil
}

func (r *PersonaBackgroundRuntime) currentLegalEntity(ctx context.Context, req agentrun.Request) (string, error) {
	tenant := r.cfg.TenantUUID(values.TenantId(req.Source.TenantID))
	now := r.cfg.Now().UTC()
	tx, err := r.cfg.CoreDB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if err = tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return "", err
	}
	w, found, err := (workforce.Store{}).Get(ctx, tx, tenant, req.Principal.InvokerID)
	if err != nil || !found || w.TenantID != tenant || !strings.EqualFold(w.LifecycleStatus, "active") {
		return "", errPersonaRunCurrentAuthority
	}
	employments, err := (aggregates.PeopleStore{}).ActiveEmploymentsForWorker(ctx, tx, tenant, w.WorkerID, now)
	if err != nil {
		return "", err
	}
	legal, err := personaRunLegalEntityFromEmployment(tenant, employments)
	if err != nil {
		return "", err
	}
	entity, err := (aggregates.OrganizationStore{}).CurrentLegalEntity(ctx, tx, tenant, legal, now)
	if err != nil || entity.Tenant != tenant || entity.EntityID != legal || entity.LifecycleState != "ACTIVE" {
		return "", errPersonaRunCurrentAuthority
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return legal.String(), nil
}

func (r *PersonaBackgroundRuntime) currentSkills(ctx context.Context, req agentrun.Request, grant agentdelegation.Grant) error {
	tenant := values.TenantId(req.Source.TenantID)
	d := NewAgentDirectoryDB(r.cfg.CoreDB, r.cfg.TenantUUID)
	roles, err := d.CurrentRoles(ctx, tenant, req.Principal.InvokerID)
	if err != nil {
		return err
	}
	populations, err := d.CurrentPopulations(ctx, tenant, req.Principal.InvokerID)
	if err != nil {
		return err
	}
	orgs, err := d.CurrentOrganizationScopes(ctx, tenant, req.Principal.InvokerID, roles)
	if err != nil || !slices.Contains(orgs, grant.OrganizationScopeID) {
		return errPersonaRunCurrentAuthority
	}
	reader, err := r.cfg.Personas.ForTenant(ctx, req.Source.TenantID)
	if err != nil {
		return err
	}
	version, _, err := reader.ReadCurrentPersonaAuthority(ctx, req.Audience.ID, req.Persona.ID)
	if err != nil {
		return err
	}
	var profile agentpersona.PersonaProfile
	if json.Unmarshal(version.Profile, &profile) != nil {
		return errPersonaRunCurrentAuthority
	}
	if !backgroundAudienceMatches(profile.Audience, roles, populations, grant.OrganizationScopeID) || !pinnedT0ScopesMatch(r.cfg.Skills, profile.SkillPins, grant.SkillScopes) {
		return errPersonaRunCurrentAuthority
	}
	for skill, scopes := range grant.SkillScopes {
		// Native owners of these two skills apply current chat/document visibility.
		// Field-bearing workforce skills require a delegated PDP and remain refused.
		if skill != personaChatReplySkillID && skill != personaPolicyHelperSkillID {
			return errPersonaRunCurrentAuthority
		}
		authority, ok := grant.SkillAuthorities[skill]
		resource := personaChatAuthorityResource(req.Source.TenantID, req.Audience.ID, req.Context.ID, req.Source.Ref)
		if !ok || len(authority.Fields) != 0 || !sameScopes(authority.Capabilities, scopes) || !sameScopes(authority.Purposes, []string{req.Purpose}) || !sameScopes(authority.Resources, []string{resource}) {
			return errPersonaRunCurrentAuthority
		}
		var pinFound bool
		for _, pin := range profile.SkillPins {
			if pin.ID != skill {
				continue
			}
			pinFound = true
			rows, e := r.cfg.Skills.grants.Grants(ctx, tenant, pin.Key())
			if e != nil {
				return e
			}
			allowed := false
			for _, row := range rows {
				if validPersonaSkillGrant(row, tenant, pin.Key()) && !row.ConsentRequired && backgroundGrantTupleMatches(row, roles, populations, grant.OrganizationScopeID, req.Purpose) {
					allowed = true
					break
				}
			}
			if !allowed {
				return errPersonaRunCurrentAuthority
			}
		}
		if !pinFound {
			return errPersonaRunCurrentAuthority
		}
	}
	return nil
}

func backgroundAudienceMatches(a agentpersona.Audience, roles, populations []string, organization string) bool {
	return backgroundDimensionOverlap(a.Roles, roles) && backgroundDimensionOverlap(a.Populations, populations) && grantDimensionCovers(a.OrganizationScopes, organization)
}
func backgroundDimensionOverlap(granted, current []string) bool {
	for _, value := range current {
		if grantDimensionCovers(granted, value) {
			return true
		}
	}
	return false
}
func backgroundGrantTupleMatches(row agentgate.SkillGrant, roles, populations []string, organization, purpose string) bool {
	return backgroundDimensionOverlap(row.Roles, roles) && (row.Population == agentgate.AnyScope || slices.Contains(populations, row.Population)) && grantDimensionCovers(row.OrganizationScopes, organization) && grantDimensionCovers(row.Purposes, purpose)
}

// currentContext preserves the original image while owner reads prove that
// every original post remains visible and unchanged. New posts are not replayed.
func (r *PersonaBackgroundRuntime) currentContext(ctx context.Context, req agentrun.Request) (chat.ThreadSnapshot, error) {
	original, err := r.cfg.Contexts.Get(ctx, req.Source.TenantID, req.Context.SnapshotID)
	if err != nil || original.Digest != req.Context.Digest || original.ThreadID != req.Context.ID || original.ConversationID != req.Audience.ID || original.InvokingPostID != req.Source.Ref || original.PrincipalID != req.Principal.InvokerID {
		return chat.ThreadSnapshot{}, errPersonaRunCurrentAuthority
	}
	current, err := r.cfg.Threads.CaptureBackgroundThreadSnapshot(ctx, chat.BackgroundThreadSnapshotRequest{TenantID: req.Source.TenantID, ReaderID: req.Principal.InvokerID, ConversationID: req.Audience.ID, ThreadID: req.Context.ID, InvokingPostID: req.Source.Ref, Limit: chat.MaxThreadSnapshotPosts})
	if err != nil || current.TenantID != original.TenantID || current.ConversationID != original.ConversationID || current.ThreadID != original.ThreadID || current.InvokingPostID != original.InvokingPostID || current.ReaderTenantID != original.TenantID || current.ReaderID != original.PrincipalID || current.AuthorityRevision != original.AuthorityRevision || current.Revision < original.Revision || len(current.Posts) == 0 || len(current.Posts) > chat.MaxThreadSnapshotPosts {
		return chat.ThreadSnapshot{}, errPersonaRunCurrentAuthority
	}
	digest, err := chat.BackgroundThreadSnapshotDigest(current)
	if err != nil || digest != current.Digest || current.SnapshotID != "chat-background-thread-"+digest {
		return chat.ThreadSnapshot{}, errPersonaRunCurrentAuthority
	}
	byID := map[string]chat.Post{}
	for _, post := range current.Posts {
		if _, found := byID[post.ID]; found || post.Deleted || post.TenantID != original.TenantID || post.ConversationID != original.ConversationID || (post.ID != original.ThreadID && post.ParentID != original.ThreadID) {
			return chat.ThreadSnapshot{}, errPersonaRunCurrentAuthority
		}
		byID[post.ID] = post
	}
	for _, post := range original.Posts {
		current, ok := byID[post.ID]
		if !ok || !reflect.DeepEqual(current, post) {
			return chat.ThreadSnapshot{}, errPersonaRunCurrentAuthority
		}
	}
	return original, nil
}

type personaBackgroundAdmissionKey struct{}

// WithPersonaBackgroundAdmission binds background thread reads to the exact
// admission being dispatched. The runtime still reloads it from its owner store.
func WithPersonaBackgroundAdmission(ctx context.Context, record agentrun.Record) context.Context {
	return context.WithValue(ctx, personaBackgroundAdmissionKey{}, record.Request)
}

func (r *PersonaBackgroundRuntime) ReadThread(ctx context.Context, req agentinvoke.ThreadReadRequest) ([]agentinvoke.ThreadPost, error) {
	if r == nil || ctx == nil {
		return nil, errPersonaRunCurrentAuthority
	}
	if _, ok := trust.FromContext(ctx); ok {
		return r.cfg.ForegroundThreads.ReadThread(ctx, req)
	}
	admission, ok := ctx.Value(personaBackgroundAdmissionKey{}).(agentrun.Request)
	if !ok || admission.Source.TenantID != req.TenantID || admission.Source.Ref != req.InvokingPostID || admission.Context.ID != req.ThreadID || admission.Audience.ID != req.ConversationID || admission.Principal.InvokerID != req.InvokerID {
		return nil, errPersonaRunCurrentAuthority
	}
	if _, err := r.current(ctx, admission); err != nil {
		return nil, err
	}
	snapshot, err := r.currentContext(ctx, admission)
	if err != nil {
		return nil, err
	}
	posts := make([]agentinvoke.ThreadPost, 0, len(snapshot.Posts))
	for _, p := range snapshot.Posts {
		posts = append(posts, agentinvoke.ThreadPost{TenantID: p.TenantID, ConversationID: p.ConversationID, ThreadID: req.ThreadID, ID: p.ID, AuthorID: p.AuthorID, Body: p.Body})
	}
	return posts, nil
}

var _ agentrun.Authority = (*PersonaBackgroundRuntime)(nil)
var _ agentinvoke.ThreadReader = (*PersonaBackgroundRuntime)(nil)
