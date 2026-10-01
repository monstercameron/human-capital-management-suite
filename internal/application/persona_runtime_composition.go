package application

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentcontextstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentdelegationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// PersonaRuntimeModelComposition contains deployment-owned provider, recovery,
// and workload authority. Credentials and signing keys never come from Chat.
type PersonaRuntimeModelComposition struct {
	Gateway          *AgentModelGateway
	Leases           *ModelLeaseSource
	Workload         string
	WorkerID         string
	LeaseTTL         time.Duration
	WorkerIdentity   PersonaPrivateChatWorkloadIdentitySource
	Recovery         *agentsecurity.FinalOutputRecoveryAuthority
	RecoveryVerifier *agentsecurity.FinalOutputRecoveryVerifier
}

// PersonaRuntimeModelOwnerDependencies are the already composed current
// authorities needed to construct provider route/source evidence. The factory
// runs after admission composition, preventing a permissive circular placeholder.
type PersonaRuntimeModelOwnerDependencies struct {
	AgentStore  *agentstore.Store
	Personas    personaRunAuthorityFactory
	Manifests   personaRunManifestResolverFactory
	Routes      PersonaModelRoutePolicyReader
	Threads     agentinvoke.ThreadReader
	Authority   agentrun.Authority
	TenantUUID  func(values.TenantId) uuid.UUID
	Now         func() time.Time
	ToolJournal PersonaRuntimeToolJournal
	ToolSources PersonaRuntimeToolSourceValidator
	ChatClasses PersonaPublicChatDisclosureClassificationSource
}

type personaRuntimeCompositionInput struct {
	AgentDatabase     composedAgentDatabase
	Pool              *pgxadapter.Pool
	Cell              *app.Cell
	Personas          *personaServeWiring
	Chat              *streamingChatService
	ChatRuntime       composedChat
	Documents         composedDocument
	Model             PersonaRuntimeModelComposition
	ModelFactory      func(PersonaRuntimeModelOwnerDependencies) (PersonaRuntimeModelComposition, error)
	AudienceSnapshots PersonaAudienceFloorSnapshotSource
	AudienceFloor     chatcore.PersonaAudienceFloor
	OutputPolicy      PersonaReplyOutputPolicy
}

// composePersonaRuntimeDependencies connects existing production owner adapters
// to the durable invocation worker. Every owner is re-read by admission, model,
// output, and delivery; construction never manufactures grants or policy rows.
func composePersonaRuntimeDependencies(ctx context.Context, in personaRuntimeCompositionInput) (*PersonaInvocationProductionConfig, error) {
	if ctx == nil || in.AgentDatabase.store == nil || in.AgentDatabase.personas == nil || in.Pool == nil || in.Cell == nil || in.Personas == nil || in.Personas.aud == nil || in.Chat == nil ||
		in.ChatRuntime.extensions == nil || in.ChatRuntime.extensions.TodoStore == nil {
		return nil, errPersonaInvocationProductionComposition
	}
	skillSource, ok := in.Personas.skill.(*AgentSkillSource)
	if !ok || skillSource == nil {
		return nil, errPersonaInvocationProductionComposition
	}
	snapshots, ok := in.Chat.ConversationService.(PersonaThreadSnapshotSource)
	if !ok || isNilPersonaOutputPort(snapshots) {
		return nil, fmt.Errorf("%w: authenticated thread snapshots", errPersonaInvocationProductionComposition)
	}
	committer, ok := in.Chat.ConversationService.(chatcore.PersonaReplyCommitter)
	if !ok || isNilPersonaOutputPort(committer) {
		return nil, fmt.Errorf("%w: atomic persona replies", errPersonaInvocationProductionComposition)
	}
	backgroundThreads := chatstore.NewAdapter(in.ChatRuntime.extensions.TodoStore)
	tenantUUID := tenantKeyMapper[values.TenantId](pgstore.TenantID)
	contexts, err := agentcontextstore.NewWithTenantUUID(in.AgentDatabase.store, func(tenant string) uuid.UUID { return tenantUUID(values.TenantId(tenant)) })
	if err != nil {
		return nil, err
	}
	snapshots = PersonaCheckpointThreadSnapshotSource{Source: snapshots, Contexts: contexts}
	now := in.Personas.now
	if now == nil {
		return nil, errPersonaInvocationProductionComposition
	}
	personas := PersonaRunAuthorityStore{Store: in.AgentDatabase.personas}
	manifestReaders := TenantPersonaRunManifestResolver(func(ctx context.Context, tenant string) (personaRunManifestResolver, error) {
		key := tenantUUID(values.TenantId(tenant))
		if key == uuid.Nil {
			return nil, ErrAgentManifestUnavailable
		}
		return personaRuntimeManifestReader{AgentManifestStoreAdapter: AgentManifestStoreAdapter{Store: in.AgentDatabase.store, TenantID: key}, tenant: tenant}, nil
	})
	budgets, err := NewPersonaRunEffectivePolicyResolver(in.AgentDatabase.store, tenantUUID, now)
	if err != nil {
		return nil, err
	}
	principal := PersonaRunAgentPrincipalResolver{Bindings: AgentPersonaRunPrincipalBindingReader{Store: in.AgentDatabase.personas}, Principals: GovernancePersonaPrincipalAuthority{DB: in.Pool, TenantUUID: tenantUUID}, Now: now}
	owners, err := NewPersonaRunOwnerFactsComposer(PersonaRunLegalEntityResolver{DB: in.Pool, TenantUUID: tenantUUID, Now: now}, principal,
		&PersonaRunAudienceSnapshotSource{Chat: in.Chat.ConversationService, ChatStore: in.ChatRuntime.extensions.TodoStore, Snapshot: in.AudienceSnapshots}, snapshots, budgets)
	if err != nil {
		return nil, err
	}
	builder, err := NewDatabasePersonaRunRequestBuilder(personas, manifestReaders, owners)
	if err != nil {
		return nil, err
	}
	chatScope, err := NewPersonaChatScopeAuthorizer(in.Chat.ConversationService, in.ChatRuntime.extensions.TodoStore, snapshots)
	if err != nil {
		return nil, err
	}
	sources, err := NewPersonaMentionAuthoritySources(PersonaMentionAuthoritySourcesConfig{Audience: in.Personas.aud.Source,
		Personas: AgentPersonaAuthorityStore{Store: in.AgentDatabase.personas}, Skills: skillSource, Gate: skillSource.gate, Catalog: in.Personas.skills,
		Discovery: skillSource.current, Purpose: personaChatReplyPurpose, ChatScope: chatScope, Now: now})
	if err != nil {
		return nil, err
	}
	authority := &PersonaAuthorityAdapter{Personas: sources.Personas, Identity: sources.Identity, Invokers: sources.Invokers, Now: now}
	grants, err := agentdelegationstore.New(in.Pool, tenantUUID)
	if err != nil {
		return nil, err
	}
	issuer, err := NewRequestScopedPersonaGrantIssuer(RequestScopedPersonaGrantIssuerConfig{Personas: sources.Personas, Identity: sources.Identity, Invokers: sources.Invokers, Stores: grants, Now: now})
	if err != nil {
		return nil, err
	}
	t0, err := NewDatabasePersonaT0SkillPolicy(PersonaT0DynamicPolicyConfig{Personas: AgentPersonaAuthorityStore{Store: in.AgentDatabase.personas}, Authority: authority, InvokerAuthority: sources.Invokers,
		GrantStores: grants, Facts: builder.source, Catalog: in.Personas.skills, Now: now})
	if err != nil {
		return nil, err
	}
	admission, err := NewPersonaForegroundRunAuthority(PersonaForegroundRunAuthorityConfig{Builder: builder, Persona: authority, Grants: grants, T0Policy: t0, Now: now})
	if err != nil {
		return nil, err
	}
	modelConfig := in.Model
	threads := PersonaAtomicThreadReader{Snapshots: snapshots}
	current := &personaRuntimeCurrentOwners{authority: admission, threads: threads}
	toolSources := &personaRuntimeToolSourceOwner{}
	chatClasses, ok := in.AudienceSnapshots.(PersonaPublicChatDisclosureClassificationSource)
	if !ok || isNilPersonaOutputPort(chatClasses) {
		return nil, errPersonaInvocationProductionComposition
	}
	current.classes, current.classStore = chatClasses, in.ChatRuntime.extensions.TodoStore
	if in.ModelFactory != nil {
		modelConfig, err = in.ModelFactory(PersonaRuntimeModelOwnerDependencies{AgentStore: in.AgentDatabase.store, Personas: personas, Manifests: manifestReaders, Routes: in.AgentDatabase.store, Threads: current, Authority: current, TenantUUID: tenantUUID, Now: now, ToolJournal: DatabasePersonaRuntimeToolJournal{Store: in.AgentDatabase.personas}, ToolSources: toolSources, ChatClasses: current})
		if err != nil {
			return nil, err
		}
	}
	if modelConfig.Gateway == nil || modelConfig.Leases == nil || !required(modelConfig.Workload) || !required(modelConfig.WorkerID) || modelConfig.LeaseTTL <= 0 || isNilPersonaOutputPort(modelConfig.WorkerIdentity) || modelConfig.Recovery == nil || modelConfig.RecoveryVerifier == nil {
		return nil, errPersonaInvocationProductionComposition
	}
	background, err := NewPersonaBackgroundRuntime(PersonaBackgroundRuntimeConfig{Foreground: admission, ForegroundThreads: threads,
		CoreDB: in.Pool, Agents: in.AgentDatabase.store, Personas: personas, Manifests: manifestReaders, Grants: grants, Skills: skillSource,
		Contexts: contexts, Audience: in.ChatRuntime.extensions.TodoStore, Threads: backgroundThreads, Principal: principal, Budgets: budgets,
		Worker: modelConfig.WorkerIdentity, TenantUUID: tenantUUID, Now: now})
	if err != nil {
		return nil, err
	}
	// Bind the fully constructed current owner before the configuration can be
	// served. Provider evidence and execution retain this same owner reference.
	current.authority, current.threads = background, background
	t0.background = background
	remaining, ok := modelConfig.Gateway.budget.(PersonaRunModelRemainingBudgetSource)
	if !ok || isNilPersonaOutputPort(remaining) {
		return nil, errPersonaInvocationProductionComposition
	}
	model, err := NewPersonaRunModelPorts(modelConfig.Gateway, PersonaRunModelWorkSourceConfig{Personas: personas, Manifests: manifestReaders, Routes: in.AgentDatabase.store, Budgets: budgets,
		Threads: current, Leases: modelConfig.Leases, TenantUUID: tenantUUID, Workload: modelConfig.Workload, Now: now, Remaining: remaining})
	if err != nil {
		return nil, err
	}
	outputAuthority := &personaRuntimeOutputAuthority{authority: current, work: model.Work, grants: grants, worker: modelConfig.WorkerIdentity}
	output, err := NewPersonaRunOutputValidator(PersonaRunOutputValidatorConfig{Authority: outputAuthority, Persister: AgentPersonaRunFinalOutputPersister{Stores: AgentPersonaRunFinalOutputStoreFactory{Store: in.AgentDatabase.personas}, Recovery: modelConfig.Recovery}})
	if err != nil {
		return nil, err
	}
	recovery := &personaRuntimeOutputRecovery{store: in.AgentDatabase.store, tenantUUID: tenantUUID, authority: outputAuthority, verifier: modelConfig.RecoveryVerifier}
	committer, err = newPersonaPublicReplyCommitter(in.ChatRuntime.extensions.TodoStore, recovery, modelConfig.WorkerIdentity, now)
	if err != nil {
		return nil, err
	}
	floor := in.AudienceFloor
	if isNilPersonaOutputPort(floor) {
		floor = personaPrivateOnlyAudienceFloor{}
	}
	reply, err := NewPersonaReplyDeliveryWithOutputPolicy(in.Chat.ConversationService, committer, floor, in.OutputPolicy)
	if err != nil {
		return nil, err
	}
	switchBoard := agentsecurity.NewKillSwitch()
	fence, err := NewDatabasePersonaRunSecurityFence(DatabasePersonaRunSecurityFenceConfig{Steps: in.AgentDatabase.store, LeaseFence: agentsecurity.NewPersonaRunFence(switchBoard), TenantUUID: tenantUUID, Now: now})
	if err != nil {
		return nil, err
	}
	config := &PersonaInvocationProductionConfig{AgentStore: in.AgentDatabase.store, TenantUUID: tenantUUID, Authority: authority, Grants: issuer, T0Skills: t0,
		Run:   PersonaRunStarterConfig{Builder: builder, Authority: current, Model: model.Model, Work: model.Work, Output: output, Reply: reply, WorkerID: modelConfig.WorkerID, LeaseTTL: modelConfig.LeaseTTL, Now: now},
		Fence: fence, Leases: &personaRuntimeSecurityLeases{store: in.AgentDatabase.store, switchBoard: switchBoard, tenantUUID: tenantUUID, now: now}}
	tools, err := NewPersonaRuntimeTools(PersonaRuntimeToolsConfig{Authority: current, Policy: t0, Gateway: in.Personas.capabilityGateway,
		Journal: DatabasePersonaRuntimeToolJournal{Store: in.AgentDatabase.personas}, Documents: DatabasePersonaRuntimeDocumentVersions{Store: in.Documents.store}, Now: now})
	if err != nil {
		return nil, err
	}
	if err := BindPersonaRuntimeTools(config, tools); err != nil {
		return nil, err
	}
	backgroundReply, err := NewDatabasePersonaBackgroundReplyDelivery(PersonaBackgroundReplyDeliveryConfig{Authority: current, Worker: modelConfig.WorkerIdentity, Threads: backgroundThreads, OutputAuthority: outputAuthority, OutputPolicy: in.OutputPolicy, Now: now}, in.ChatRuntime.extensions.TodoStore)
	if err != nil {
		return nil, err
	}
	config.Run.BackgroundReply = backgroundReply
	toolSources.source = tools
	outputs, err := NewPersonaFinalOutputSource(in.AgentDatabase.personas, modelConfig.RecoveryVerifier, recovery)
	if err != nil {
		return nil, err
	}
	config.BackgroundRecovery = outputs
	config.Run.Reply = &personaRuntimeCurrentReply{next: reply, authority: recovery}
	return config, nil
}

type personaRuntimeToolSourceOwner struct {
	source PersonaRuntimeToolSourceValidator
}

func (s *personaRuntimeToolSourceOwner) RecheckPersonaRuntimeToolResult(ctx context.Context, record agentpersonastore.ToolResultRecord) error {
	if s == nil || isNilPersonaOutputPort(s.source) {
		return errPersonaRuntimeTools
	}
	return s.source.RecheckPersonaRuntimeToolResult(ctx, record)
}

// personaRuntimeCurrentOwners is bound once during composition, before any run
// is served. It resolves the dependency between provider evidence and the
// verified process identity produced by provider composition.
type personaRuntimeCurrentOwners struct {
	authority  agentrun.Authority
	threads    agentinvoke.ThreadReader
	classes    PersonaPublicChatDisclosureClassificationSource
	classStore interface {
		PublicChatDisclosureClass(context.Context, string, string, string, string) (dlp.DataClass, error)
	}
}

func (o *personaRuntimeCurrentOwners) PersonaPublicChatDisclosureClass(ctx context.Context, tenant, conversation, postID, digest string) (dlp.DataClass, error) {
	if o == nil || ctx == nil || isNilPersonaOutputPort(o.classes) || isNilPersonaOutputPort(o.classStore) {
		return "", ErrPersonaAudienceFloorUnavailable
	}
	if _, ok := trust.FromContext(ctx); ok {
		return o.classes.PersonaPublicChatDisclosureClass(ctx, tenant, conversation, postID, digest)
	}
	req, ok := ctx.Value(personaBackgroundAdmissionKey{}).(agentrun.Request)
	if !ok || req.Source.TenantID != tenant || req.Audience.ID != conversation {
		return "", ErrPersonaAudienceFloorUnavailable
	}
	if _, err := o.VerifyAdmission(ctx, req); err != nil {
		return "", err
	}
	posts, err := o.ReadThread(ctx, agentinvoke.ThreadReadRequest{TenantID: tenant, ConversationID: conversation, ThreadID: req.Context.ID, InvokerID: req.Principal.InvokerID, InvokingPostID: req.Source.Ref, Limit: agentinvoke.MaxThreadPosts})
	if err != nil {
		return "", err
	}
	for _, post := range posts {
		if post.ID == postID && post.TenantID == tenant && post.ConversationID == conversation && personaRunBytesDigest([]byte(post.Body)) == digest {
			return o.classStore.PublicChatDisclosureClass(ctx, tenant, conversation, postID, digest)
		}
	}
	return "", ErrPersonaAudienceFloorUnavailable
}

func (o *personaRuntimeCurrentOwners) VerifyAdmission(ctx context.Context, req agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	if o == nil || isNilPersonaOutputPort(o.authority) {
		return agentrun.AuthoritySnapshot{}, errPersonaInvocationProductionComposition
	}
	return o.authority.VerifyAdmission(ctx, req)
}

func (o *personaRuntimeCurrentOwners) ReadThread(ctx context.Context, req agentinvoke.ThreadReadRequest) ([]agentinvoke.ThreadPost, error) {
	if o == nil || isNilPersonaOutputPort(o.threads) {
		return nil, errPersonaInvocationProductionComposition
	}
	return o.threads.ReadThread(ctx, req)
}

type personaRuntimeManifestReader struct {
	AgentManifestStoreAdapter
	tenant string
}

func (r personaRuntimeManifestReader) TenantID() string { return r.tenant }
