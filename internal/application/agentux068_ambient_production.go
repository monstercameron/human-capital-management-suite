package application

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agenttriggerstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// agentUXAmbientTriggerStore is the scheduler store the announcement agents
// already use (the native trigger store in the agent database), reached for any
// tenant. A reminder somebody sets is one more announcement in it: one
// scheduler, one outbox, one delivery path (AGENTUX-068).
type agentUXAmbientTriggerStore struct {
	agents     *agentstore.Store
	tenantUUID func(values.TenantId) uuid.UUID
}

func (s agentUXAmbientTriggerStore) tenant(tenant string) (*agenttriggerstore.Store, error) {
	if s.agents == nil || s.tenantUUID == nil {
		return nil, ErrAgentUXAmbientInvalid
	}
	return agenttriggerstore.NewAnnouncementSource(s.agents, s.tenantUUID(values.TenantId(tenant)), tenant, agenttriggerstore.AgentScheduleExecutions{Database: s.agents})
}

func (s agentUXAmbientTriggerStore) Load(ctx context.Context, tenant, id string) (scheduled.Schedule, bool, error) {
	store, err := s.tenant(tenant)
	if err != nil {
		return scheduled.Schedule{}, false, err
	}
	return store.Load(ctx, tenant, id)
}

func (s agentUXAmbientTriggerStore) Save(ctx context.Context, record scheduled.Schedule, expected uint64, audit scheduled.Audit) error {
	store, err := s.tenant(record.Trigger.Definition.TenantID)
	if err != nil {
		return err
	}
	return store.Save(ctx, record, expected, audit)
}

func (s agentUXAmbientTriggerStore) AppendWindow(ctx context.Context, record scheduled.Schedule, end values.Instant, deliveries []scheduled.Delivery) error {
	store, err := s.tenant(record.Trigger.Definition.TenantID)
	if err != nil {
		return err
	}
	return store.AppendWindow(ctx, record, end, deliveries)
}

func (s agentUXAmbientTriggerStore) Pending(ctx context.Context, tenant string, limit int) ([]scheduled.Delivery, error) {
	store, err := s.tenant(tenant)
	if err != nil {
		return nil, err
	}
	return store.Pending(ctx, tenant, limit)
}

func (s agentUXAmbientTriggerStore) GetDelivery(ctx context.Context, tenant, key string) (scheduled.Delivery, bool, error) {
	store, err := s.tenant(tenant)
	if err != nil {
		return scheduled.Delivery{}, false, err
	}
	return store.GetDelivery(ctx, tenant, key)
}

func (s agentUXAmbientTriggerStore) Acknowledge(ctx context.Context, tenant, key string, receipt scheduled.Receipt) error {
	store, err := s.tenant(tenant)
	if err != nil {
		return err
	}
	return store.Acknowledge(ctx, tenant, key, receipt)
}

func (s agentUXAmbientTriggerStore) LoadReceipt(ctx context.Context, tenant, key string) (scheduled.Receipt, bool, error) {
	store, err := s.tenant(tenant)
	if err != nil {
		return scheduled.Receipt{}, false, err
	}
	return store.LoadReceipt(ctx, tenant, key)
}

var _ scheduled.OwnerStore = agentUXAmbientTriggerStore{}
var _ scheduled.Outbox = agentUXAmbientTriggerStore{}

// agentUXAmbientBindings resolves a reminder offer to the agent's installation:
// the published persona version, its service principal, the legal entity of the
// person who owns the schedule and the destination audience. Nothing here comes
// from the offer's text or the model.
type agentUXAmbientBindings struct {
	Personas    *agentpersonastore.Store
	Agents      *agentstore.Store
	Principals  AgentAnnouncementServicePrincipal
	LegalEntity func(ctx context.Context, tenant, owner string) (string, error)
	TenantUUID  func(values.TenantId) uuid.UUID
	Now         func() time.Time
}

func (b agentUXAmbientBindings) ResolveAmbientAnnouncement(ctx context.Context, o AgentUXAmbientOffer) (scheduled.Schedule, error) {
	if b.Personas == nil || b.Agents == nil || b.LegalEntity == nil || b.TenantUUID == nil || b.Now == nil || ctx == nil {
		return scheduled.Schedule{}, ErrAgentUXAmbientInvalid
	}
	owner := o.Person
	if setter, ok := trust.FromContext(ctx); ok && setter != nil && setter.SubjectKind() == trust.SubjectKindHuman && setter.Tenant().String() == o.Tenant {
		owner = setter.Subject()
	}
	if owner == "" {
		return scheduled.Schedule{}, ErrAgentUXAmbientDenied
	}
	scoped, err := b.Personas.Scoped(values.TenantId(o.Tenant))
	if err != nil {
		return scheduled.Schedule{}, err
	}
	identity, err := scoped.LookupPersonaChatIdentity(ctx, o.Agent)
	if err != nil || !identity.Active {
		return scheduled.Schedule{}, ErrAgentUXAmbientDenied
	}
	version, install, err := scoped.ReadCurrentPersonaAuthority(ctx, o.Conversation, identity.PersonaID)
	if err != nil {
		return scheduled.Schedule{}, ErrAgentUXAmbientDenied
	}
	var profile agentpersona.PersonaProfile
	if json.Unmarshal(version.Profile, &profile) != nil {
		return scheduled.Schedule{}, ErrAgentUXAmbientInvalid
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil || sealed.Digest != version.ContentDigest {
		return scheduled.Schedule{}, ErrAgentUXAmbientDenied
	}
	manifest, err := (AgentManifestStoreAdapter{Store: b.Agents, TenantID: b.TenantUUID(values.TenantId(o.Tenant))}).ResolveAgentManifestContext(ctx, profile.Manifest)
	if err != nil {
		return scheduled.Schedule{}, err
	}
	digest, err := manifest.Digest()
	if err != nil {
		return scheduled.Schedule{}, err
	}
	principal, err := b.Principals.CurrentAnnouncementServicePrincipal(ctx, o.Tenant, install.InstallationID)
	if err != nil {
		return scheduled.Schedule{}, err
	}
	legal, err := b.LegalEntity(ctx, o.Tenant, owner)
	if err != nil {
		return scheduled.Schedule{}, err
	}
	audience := o.Conversation
	if o.Scope == "PRIVATE" {
		audience, err = chatcore.DirectPairConversationID(o.Tenant, []chatcore.MemberRef{{TenantID: o.Tenant, SubjectID: o.Person}, {TenantID: o.Tenant, SubjectID: o.Agent}})
		if err != nil {
			return scheduled.Schedule{}, err
		}
	}
	offerDigest := agentUXAmbientOfferDigest(o)
	budget := schedule.AgentRunBudget{MaxCostMicros: manifest.Budget.MaxCostMicros, MaxInputTokens: manifest.Budget.MaxInputTokens, MaxOutputTokens: manifest.Budget.MaxOutputTokens}
	target := &schedule.AgentRunTarget{Agent: schedule.AgentVersionRef{ID: manifest.ID, Version: fmt.Sprint(manifest.Version), Digest: digest}, SponsorID: principal.SubjectID, Purpose: "ambient-reminder", Budget: budget, Destination: schedule.AgentRunDestination{AudienceID: audience, AudienceSnapshotID: "ambient-v1", AudienceDigest: personaRunBytesDigest([]byte(o.Tenant + "\x00" + audience + "\x00" + offerDigest))}}
	return scheduled.Schedule{Persona: &agentrun.PersonaRef{ID: profile.PersonaID, Version: fmt.Sprint(profile.Version), Digest: sealed.Digest}, OwnerID: owner, InstallationID: install.InstallationID, AgentPrincipalID: principal.PrincipalID, LegalEntity: legal, RunTimeout: time.Minute, Trigger: schedule.PublishedTrigger{Definition: schedule.TriggerDefinition{TenantID: o.Tenant, TargetKind: schedule.TargetAgentRun, AgentRun: target, Purpose: "ambient-reminder", Storm: schedule.StormPolicy{MaxFiringsPerWindow: 50, Window: 366 * 24 * time.Hour}, ExecutionMode: intent.ModeExecute, ExecutionEnvironment: intent.EnvironmentProduction}}}, nil
}

// agentUXAmbientAdmissions admits the occurrence of a reminder that has come
// due. The scheduler's inbox (agentUXAmbientAnnouncementInbox) has already
// checked the offer, its source message and the channel installation; this
// authority checks the agent's own installation and principal once more, and
// the admission is stored in the same run-admission store as every other run.
type agentUXAmbientAdmissions struct {
	Agents     *agentstore.Store
	Store      agentUXAmbientTriggerStore
	Principals AgentAnnouncementServicePrincipal
	TenantUUID func(values.TenantId) uuid.UUID
	Now        func() time.Time
}

func (a agentUXAmbientAdmissions) Admit(ctx context.Context, r agentrun.Request) (agentrun.Record, bool, error) {
	if a.Agents == nil || a.TenantUUID == nil || a.Now == nil {
		return agentrun.Record{}, false, ErrAgentUXAmbientInvalid
	}
	tenant := r.Source.TenantID
	repository, err := agentrunstore.NewAdmissionRepositoryWithSourceResolver(a.Agents, a.TenantUUID(values.TenantId(tenant)), values.TenantId(tenant), agentUXAmbientSourceKeys{store: a.Store})
	if err != nil {
		return agentrun.Record{}, false, err
	}
	service, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: a, Store: repository, Now: a.Now})
	if err != nil {
		return agentrun.Record{}, false, err
	}
	return service.Admit(ctx, r)
}

func (a agentUXAmbientAdmissions) VerifyAdmission(ctx context.Context, r agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	if r.Source.Kind != agentrun.SourceAnnouncement || r.Persona == nil || r.Principal.Mode != agentrun.ModeSponsored || !r.Deadline.After(a.Now()) {
		return agentrun.AuthoritySnapshot{}, ErrAgentUXAmbientDenied
	}
	identity, err := a.Principals.CurrentAnnouncementServicePrincipal(ctx, r.Source.TenantID, r.InstallationID)
	if err != nil || identity.PrincipalID != r.Principal.AgentPrincipalID || identity.SubjectID != r.Principal.SponsorID || identity.PersonaID != r.Persona.ID {
		return agentrun.AuthoritySnapshot{}, errorsJoinAmbient(err)
	}
	return agentrun.AuthoritySnapshot{Agent: r.Agent, InstallationID: r.InstallationID, Principal: r.Principal, Audience: r.Audience, Context: r.Context, BudgetCeiling: r.Budget, GrantRef: "ambient-installation:" + r.InstallationID, PolicyDigest: personaRunBytesDigest([]byte("ambient-reminder\x00" + r.Agent.Digest))}, nil
}

func errorsJoinAmbient(err error) error {
	if err == nil {
		return &agentrun.AdmissionRefusal{Code: "AUTHORITY_REFUSED"}
	}
	return fmt.Errorf("%w: %v", ErrAgentUXAmbientDenied, err)
}

// agentUXAmbientSourceKeys recovers the occurrence key an admission is stored
// under from the scheduler's own delivery, the way the announcement agents do.
type agentUXAmbientSourceKeys struct{ store agentUXAmbientTriggerStore }

func (k agentUXAmbientSourceKeys) ResolveSourceKey(ctx context.Context, r agentrun.Request) (string, error) {
	if r.Source.Kind != agentrun.SourceAnnouncement || r.Source.Ref == "" {
		return "", ErrAgentUXAmbientDenied
	}
	delivery, found, err := k.store.GetDelivery(ctx, r.Source.TenantID, r.Source.Ref)
	if err != nil || !found || delivery.Request.Source.Kind != agentrun.SourceAnnouncement || delivery.Request.Context != r.Context || delivery.Request.InstallationID != r.InstallationID {
		return "", ErrAgentUXAmbientDenied
	}
	if r.Source.Key == "" {
		r.Source.Key = delivery.Key
	}
	want, wantErr := agentrun.AdmissionRequestDigest(delivery.Request)
	got, gotErr := agentrun.AdmissionRequestDigest(r)
	if wantErr != nil || gotErr != nil || want != got {
		return "", ErrAgentUXAmbientDenied
	}
	return delivery.Key, nil
}

// agentUXAmbientLegalEntity resolves the legal entity a person works under, the
// same way the preparation and the announcement agents do.
func agentUXAmbientLegalEntity(core dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID, now func() time.Time) func(context.Context, string, string) (string, error) {
	resolver := PersonaRunLegalEntityResolver{DB: core, TenantUUID: tenantUUID, Now: now}
	return func(ctx context.Context, tenant, owner string) (string, error) {
		return resolver.Resolve(ctx, agentinvoke.RunRequest{TenantID: tenant, Mode: agentinvoke.OnBehalfOf, InvokerID: owner})
	}
}

// agentUXAmbientChatDelivery is where a due reminder is written. A reminder for
// the channel is posted by the agent through the chat store's public-agent write
// (the agent is installed in the channel); a private one is posted by the agent
// in the person's conversation with it, which is also the person's notification:
// the product's unread count is the one place a new message in a conversation is
// announced, and there is no second notification table to fall out of step.
func agentUXAmbientChatDelivery(store *chatstore.Store, service *AgentUXAmbientService, sourceLink func(context.Context, string, string, string) (string, error)) AgentUXAmbientDeliveryAdapter {
	adapter := chatstore.NewAdapter(store)
	return AgentUXAmbientDeliveryAdapter{
		Service: service, SourceLink: sourceLink,
		PublicMessage: func(ctx context.Context, o AgentUXAmbientOffer, text, key string) (string, error) {
			post, err := store.PostAmbientAgentMessage(ctx, o.Tenant, o.Conversation, o.Agent, text, key)
			return post.ID, err
		},
		PrivateAgentDM: func(ctx context.Context, o AgentUXAmbientOffer, text, key string) (string, error) {
			dm, err := chatcore.DirectPairConversationID(o.Tenant, []chatcore.MemberRef{{TenantID: o.Tenant, SubjectID: o.Person}, {TenantID: o.Tenant, SubjectID: o.Agent}})
			if err != nil {
				return "", err
			}
			post, err := adapter.SendPost(ctx, chatcore.SendPostRequest{Principal: chatcore.Principal{TenantID: o.Tenant, SubjectID: o.Agent}, TenantID: o.Tenant, ConversationID: dm, IdempotencyKey: key}, chatcore.Post{AuthorID: o.Agent, AuthorHomeTenantID: o.Tenant, Body: text})
			return post.ID, err
		},
		NotifyPerson: func(_ context.Context, _, _, id, _ string) error {
			if id == "" {
				return ErrAgentUXAmbientDenied
			}
			return nil
		},
	}
}

// agentUXAmbientDMEnsurer makes a person's conversation with an agent exist,
// under the person's own authority, through the same provisioner the persona
// chat uses.
func agentUXAmbientDMEnsurer(store *chatstore.Store, service chatcore.ConversationService) func(context.Context, string, string, string) error {
	if store == nil || service == nil {
		return nil
	}
	return func(ctx context.Context, tenant, person, agent string) error {
		member := chatcore.MemberRef{TenantID: tenant, SubjectID: agent}
		resolver, err := NewPersonaDMResolver(chatstore.NewAdapter(store), member)
		if err != nil {
			return err
		}
		provisioner, err := NewPersonaDMProvisioner(service, resolver, member)
		if err != nil {
			return err
		}
		provisioner.Policies = store
		_, err = provisioner.EnsurePersonaDM(ctx, chatcore.Principal{TenantID: tenant, SubjectID: person}, tenant)
		return err
	}
}
