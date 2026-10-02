package application

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func composeAgentAnnouncements(in personaRuntimeCompositionInput, cfg *PersonaInvocationProductionConfig) (*AgentAnnouncementControlSurface, *AgentAnnouncementWorker, error) {
	if cfg == nil || in.Documents.store == nil || in.ChatRuntime.extensions == nil || in.ChatRuntime.extensions.TodoStore == nil {
		return nil, nil, nil
	}
	work, ok := cfg.Run.Work.(*DatabasePersonaRunModelWorkSource)
	if !ok {
		return nil, nil, nil
	}
	validator, ok := cfg.Run.Output.(*SealedPersonaRunOutputValidator)
	if !ok {
		return nil, nil, ErrAgentAnnouncementUnavailable
	}
	output, ok := validator.authority.(*personaRuntimeOutputAuthority)
	if !ok {
		return nil, nil, ErrAgentAnnouncementUnavailable
	}
	current, ok := cfg.Run.Authority.(*personaRuntimeCurrentOwners)
	if !ok {
		return nil, nil, ErrAgentAnnouncementUnavailable
	}
	store, err := agentstore.NewAnnouncementStore(in.AgentDatabase.store)
	if err != nil {
		return nil, nil, err
	}
	owner := AgentAnnouncementOwnerAuthority{Personas: in.AgentDatabase.personas, Authorizer: PersonaAdminCommandRoleAuthorizer{Roles: in.Cell.RoleAccess}, TenantUUID: work.tenantUUID, Now: work.now}
	principals := AgentAnnouncementServicePrincipal{Agents: in.AgentDatabase.store, Personas: in.AgentDatabase.personas, Principals: GovernancePersonaPrincipalAuthority{DB: in.Pool, TenantUUID: work.tenantUUID}, TenantUUID: work.tenantUUID, Now: work.now}
	docs := AgentAnnouncementReferenceResolver{Hub: in.Documents.store, Principals: principals}
	authority := AgentAnnouncementHubAuthority{Store: in.Documents.store, Now: work.now}
	floor := NewPersonaAudienceFloorAdapter(in.AudienceSnapshots, in.AudienceSnapshots.(PersonaAudienceFloorDisclosureAuthorizer), in.AudienceSnapshots.(PersonaAudienceFloorPolicy))
	runtime := &AgentAnnouncementRuntime{Store: store, Agents: in.AgentDatabase.store, Work: work, Base: cfg.Run, Principals: principals, Documents: docs, Worker: output.worker, Persister: validator.persister, Chat: in.ChatRuntime.extensions.TodoStore, Audience: floor, Now: work.now, DocumentAuthority: authority}
	preferences, err := agentstore.NewSupportInboxStore(in.AgentDatabase.store)
	if err != nil {
		return nil, nil, err
	}
	runtime.Sources = AgentUXDemoAnnouncementSources{Documents: AgentUXDemoDocumentSource{Documents: docs}, Birthdays: AgentUXDemoBirthdaySource{Audience: runtime, Directory: AgentUXDemoCoreBirthdayDirectory{Core: in.Pool, TenantUUID: work.tenantUUID}, Preferences: preferences, TenantUUID: work.tenantUUID}}
	if in.ChatRuntime.routes != nil {
		runtime.Routes, runtime.RouteCache = in.ChatRuntime.routes, chatrouting.NewRouteCache(5*time.Second)
	}
	if currentReply, ok := cfg.Run.Reply.(*personaRuntimeCurrentReply); ok {
		if delivery, ok := currentReply.next.(*PersonaReplyDelivery); ok {
			if publicFloor, ok := delivery.floor.(*PersonaRuntimeAudienceFloor); ok {
				runtime.BodyClasses = publicFloor.BodyClasses
			}
		}
	}
	if isNilPersonaOutputPort(runtime.BodyClasses) {
		return nil, nil, ErrAgentAnnouncementUnavailable
	}
	names := documentOwnerNames(in.Pool)
	runtime.Names = func(ctx context.Context, tenant, owner string) (string, error) {
		out, err := names(ctx, tenant, []string{owner})
		if err != nil || out[owner] == "" || out[owner] == owner {
			return "", ErrAgentAnnouncementUnavailable
		}
		return out[owner], nil
	}
	runtime.LegalEntity = func(ctx context.Context, tenant, owner string) (string, error) {
		key := work.tenantUUID(values.TenantId(tenant))
		tx, err := in.Pool.Begin(ctx)
		if err != nil {
			return "", err
		}
		defer tx.Rollback(ctx)
		if err := tenancy.WithTenant(ctx, tx, key); err != nil {
			return "", err
		}
		worker, found, err := (workforce.Store{}).Get(ctx, tx, key, owner)
		if err != nil || !found || worker.LifecycleStatus != "ACTIVE" && worker.LifecycleStatus != "active" {
			return "", ErrAgentAnnouncementDenied
		}
		employments, err := (aggregates.PeopleStore{}).ActiveEmploymentsForWorker(ctx, tx, key, worker.WorkerID, work.now())
		if err != nil {
			return "", err
		}
		legal, err := personaRunLegalEntityFromEmployment(key, employments)
		if err != nil {
			return "", err
		}
		entity, err := (aggregates.OrganizationStore{}).CurrentLegalEntity(ctx, tx, key, legal, work.now())
		if err != nil || entity.LifecycleState != "ACTIVE" {
			return "", ErrAgentAnnouncementDenied
		}
		return legal.String(), tx.Commit(ctx)
	}
	if err := bindAnnouncementModelBudget(runtime); err != nil {
		return nil, nil, err
	}
	// The provider evidence retained this owner pointer during composition.
	// Adding the announcement delegate therefore preserves one verifier.
	current.authority = announcementAdmissionAuthority{mention: current.authority, announcement: runtime}
	schedules := AgentAnnouncementNativeSchedules{Store: announcementScheduleStore{runtime: runtime}, Bindings: runtime, Now: work.now}
	service := &AgentAnnouncementService{Store: store, Authority: owner, Schedules: schedules, Now: work.now, Shares: AgentAnnouncementHubShares{Hub: in.Documents.store, Principals: principals}}
	gate := announcementRuntimeGate{runtime: runtime}
	runner := &AgentAnnouncementRunner{Sources: runtime.Sources, Store: store, Documents: docs, Model: runtime, Public: gate, Delivery: runtime, Now: work.now, ConversationName: func(ctx context.Context, tenant, id string) (string, error) {
		var name string
		err := runtime.Chat.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, `SELECT name FROM chat_conversation WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&name)
		})
		return name, err
	}}
	catalogStore := personaAdminCatalogStoreAdapter{store: in.AgentDatabase.personas}
	catalogNames := AgentAnnouncementCatalogNames{Versions: personaAdminCatalogVersions{store: catalogStore}, Installations: announcementCatalogInstallations{delegate: personaAdminCatalogInstallations{store: catalogStore}, chat: runtime.Chat}, Authority: owner, OwnerNames: names}
	surface := &AgentAnnouncementControlSurface{Service: service, Runner: runner, Actors: owner, Names: catalogNames, Preview: AgentAnnouncementDraftPreview{Authority: owner, Runner: runner, Reader: authority}, Now: work.now}
	return surface, &AgentAnnouncementWorker{Runtime: runtime, Runner: runner}, nil
}

type announcementCatalogInstallations struct {
	delegate PersonaCatalogInstallationReader
	chat     *chatstore.Store
}

func (s announcementCatalogInstallations) ListPersonaCatalogInstallations(ctx context.Context, tenant values.TenantId) ([]PersonaCatalogInstallation, error) {
	items, err := s.delegate.ListPersonaCatalogInstallations(ctx, tenant)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if err := s.chat.RunTenantTx(ctx, tenant.String(), func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, `SELECT name,kind FROM chat_conversation WHERE tenant_id=$1 AND id=$2`, tenant.String(), items[i].ConversationID).Scan(&items[i].Conversation, &items[i].ConversationKind)
		}); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func bindAnnouncementModelBudget(runtime *AgentAnnouncementRuntime) error {
	if model, ok := runtime.Base.Model.(*AgentModelExecutorAdapter); ok {
		budget, ok := model.gateway.budget.(*PersonaLedgerBudget)
		if !ok {
			return ErrAgentAnnouncementUnavailable
		}
		announcements := announcementLedgerBudget{mention: budget, runtime: runtime}
		model.gateway.budget = announcements
		runtime.Work.remaining = announcements
	}
	return nil
}
