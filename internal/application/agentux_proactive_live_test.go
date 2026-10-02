package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

// Provider credentials and model text are deterministic fixtures. Admission,
// installation, document grants, schedule outbox, execution and public delivery
// use the real PostgreSQL owners; the run has no human credential or thread.
type proactiveLiveModel struct {
	t        *testing.T
	calls    int
	runtime  *AgentAnnouncementRuntime
	fail     bool
	texts    []string
	requests []AgentModelExecutorRequest
	after    func()
}

type proactiveLiveRun struct {
	runtime *AgentAnnouncementRuntime
	t       *testing.T
}

func (r proactiveLiveRun) RunAnnouncement(ctx context.Context, request AgentAnnouncementRunRequest) (AgentAnnouncementRunResult, error) {
	result, err := r.runtime.RunAnnouncement(ctx, request)
	if err != nil {
		r.t.Logf("announcement run: %v", err)
	}
	return result, err
}

type proactiveLiveCustody struct{ now func() time.Time }

func (p proactiveLiveCustody) IssueLease(ctx custody.Context, h custody.Handle, op custody.Operation, ttl time.Duration) (custody.Lease, error) {
	return custody.Lease{ID: "custody-1", Handle: h, Operation: op, ExpiresAt: p.now().Add(ttl), ContextDigest: custody.ContextDigest(ctx.RequestContext)}, nil
}

func (m *proactiveLiveModel) Execute(ctx context.Context, request AgentModelExecutorRequest) (AgentModelExecutorResult, error) {
	m.calls++
	m.requests = append(m.requests, request)
	if m.after != nil {
		defer m.after()
	}
	if m.fail {
		return AgentModelExecutorResult{}, fmt.Errorf("fixture provider unavailable")
	}
	if len(request.Model.Tools) != 0 || request.Model.ActionPolicy != nil || len(request.Model.ContextRefs) != 1 || !strings.HasPrefix(request.Model.ContextRefs[0].ID, "document:") || request.Outbound.Principal != "workload:policy" {
		m.t.Fatalf("private context or tools entered announcement: %+v", request)
	}
	admission, ok := ctx.Value(personaBackgroundAdmissionKey{}).(agentrun.Request)
	if !ok || admission.Principal.InvokerID != "" || admission.Principal.RequesterID != "owner" {
		m.t.Fatal("announcement borrowed a human actor")
	}
	profile, manifest, _, _, _, err := m.runtime.facts(ctx, mustProactiveLiveRecord(m.t, ctx, m.runtime, admission.Context.ID))
	if err != nil {
		return AgentModelExecutorResult{}, err
	}
	route, err := m.runtime.Work.routes.CurrentPersonaModelRoutePolicy(ctx, m.runtime.Work.tenantUUID("tenant-a"), "entity-a", manifest.ModelPolicy.ID, int64(manifest.ModelPolicy.Version), int64(manifest.ModelPolicy.SchemaVersion), manifest.ModelPolicy.Digest, m.runtime.Now())
	if err != nil {
		return AgentModelExecutorResult{}, err
	}
	var modelRoute PersonaRunModelRoute
	if err = json.Unmarshal(route.RoutePayload, &modelRoute); err != nil {
		return AgentModelExecutorResult{}, err
	}
	fields, err := announcementAuthoritativeModelFields(ctx, agentrun.Record{Request: admission}, profile, manifest, modelRoute)
	if err != nil {
		return AgentModelExecutorResult{}, err
	}
	for _, field := range request.Outbound.Fields {
		expected, ok := fields[field.Name]
		if !ok || fmt.Sprint(field.Value) != expected.value || field.Class != expected.class || request.FieldSources[field.Name] != expected.source {
			m.t.Fatalf("outbound verifier disagrees on %s", field.Name)
		}
	}
	text := "The next company holidays are Thanksgiving on November 26 and the day after Thanksgiving on November 27."
	if len(m.texts) >= m.calls {
		text = m.texts[m.calls-1]
	}
	return AgentModelExecutorResult{Result: agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Finish: agentmodel.FinishComplete, Text: text, Usage: agentmodel.ModelUsage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2, CostMicros: 1}}}, nil
}
func mustProactiveLiveRecord(t *testing.T, ctx context.Context, r *AgentAnnouncementRuntime, id string) agentstore.Announcement {
	t.Helper()
	if d, ok := ctx.Value(announcementDraftKey{}).(agentstore.Announcement); ok {
		return d
	}
	record, err := r.Store.Get(ctx, r.Work.tenantUUID("tenant-a"), id)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func proactiveLiveFixture(t *testing.T) (*AgentAnnouncementControlSurface, *AgentAnnouncementWorker, *chatstore.Adapter, *proactiveLiveModel, *time.Time, agentcontrols.AnnouncementDraft) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	db := pgtest.NewEmpty(t)
	for attempt := range 5 {
		err := agentstore.Migrate(ctx, db.SQL)
		if err == nil {
			break
		}
		if attempt == 4 || !strings.Contains(err.Error(), "tuple concurrently updated") {
			t.Fatal(err)
		}
		t.Logf("shared PostgreSQL role changed during migration; retrying after sixty seconds: %v", err)
		time.Sleep(time.Minute)
	}
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenant)
	agents, err := agentstore.New(ctx, agentstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema), CoreDSN: "postgres://unused@127.0.0.1:1/unused", MaxConns: 8})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(agents.Close)
	mapper := func(id values.TenantId) uuid.UUID {
		if id == "tenant-a" {
			return tenant
		}
		return uuid.Nil
	}
	manifest := personaRunTestManifest()
	manifest.InstructionsDigest = personaRunBytesDigest([]byte("Use permitted records."))
	manifest.OutputSchema = agentmanifest.Reference{ID: PersonaChatReplySchema, Version: 1, SchemaVersion: 1, Digest: PersonaChatReplySchemaDigest}
	digest, _ := manifest.Digest()
	profile := personaRunTestProfile(manifest, digest)
	profile.PersonaID = "policy-helper"
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(sealed.Profile)
	db.Exec(t, `INSERT INTO persona_versions(tenant_id,persona_id,version,agent_version,handle,display_name,profile,content_digest) VALUES($1,'policy-helper',2,'agent-a@4','policy-helper','Policy Helper',$2::jsonb,$3)`, tenant, string(raw), sealed.Digest)
	db.Exec(t, `INSERT INTO persona_lifecycle_events(tenant_id,event_id,persona_id,persona_version,from_state,to_state,reason,actor_id,occurred_at,profile_digest,review_digest,reviewer_id,evaluation_digest,evaluation_profile_digest,evaluation_suite_digest) VALUES($1,'publication','policy-helper',2,'IN_REVIEW','PUBLISHED','privileged fixture','reviewer',$2,$3,$4,'reviewer',$4,$3,$4)`, tenant, now.Add(-time.Minute), sealed.Digest, foregroundDigest("fixture-review"))
	db.Exec(t, `INSERT INTO persona_installations(tenant_id,installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,revision,revocation_epoch,created_at,updated_at) VALUES($1,'install','policy-helper',2,'general','PUBLIC','owner','{"placement_class":"ANY_INTERNAL","max_tier":"T0","allowed_data_classes":["PUBLIC","INTERNAL"],"allowed_channel_classes":["PUBLIC"],"always_private":false,"conversation_search_allowed":true,"allow_external_members":false,"allow_cross_company_members":false}','ACTIVE',1,1,now(),now())`, tenant)
	db.Exec(t, `INSERT INTO persona_run_policy(tenant_id,legal_entity_id,revision,effective_from,max_cost_micros,max_input_tokens,max_output_tokens,max_run_duration_ms) VALUES($1,'entity-a',1,'2020-01-01',100,1000,100,90000)`, tenant)
	personas, err := agentpersonastore.New(agents, mapper)
	if err != nil {
		t.Fatal(err)
	}
	chatDB := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, chatDB)
	chatRaw, err := chatstore.New(ctx, chatstore.Config{DSN: personaChatSchemaDSN(t, chatDB.URL, chatDB.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	adapter := chatstore.NewAdapter(chatRaw)
	t.Cleanup(adapter.Close)
	service := chat.NewService(adapter, clock)
	service.SetAuthority(servedPersonaChatAuthority{})
	owner := chat.Principal{TenantID: "tenant-a", SubjectID: "owner"}
	if _, err = service.CreateConversation(ctx, chat.CreateConversationRequest{Principal: owner, TenantID: "tenant-a", ConversationID: "general", Name: "general", Kind: chat.PublicChannel}); err != nil {
		t.Fatal(err)
	}
	if _, err = chatRaw.PutPublicAudiencePolicy(ctx, "tenant-a", "general", 0, chatstore.PublicAudiencePolicy{Classification: "INTERNAL", Principals: []chatstore.PublicAudiencePrincipal{{HomeTenantID: "tenant-a", SubjectID: "owner"}, {HomeTenantID: "tenant-a", SubjectID: "employee"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.AddMembership(ctx, chat.AddMembershipRequest{Principal: owner, Membership: chat.Membership{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "general", SubjectID: "employee", HistoryVisibility: chat.FullHistory}}); err != nil {
		t.Fatal(err)
	}
	documents := documentServiceFixture(t).store
	id, err := documents.CreateDocument(ctx, "tenant-a", "owner", "COMPANY")
	if err != nil {
		t.Fatal(err)
	}
	version, err := documents.SubmitCandidate(ctx, "tenant-a", documenthubstore.Version{DocumentID: id, CreatorID: "owner", Title: "2026 holiday guide", Markdown: localAgentDemoHolidayMarkdown, Classification: "INTERNAL"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = documents.RecordReview(ctx, "tenant-a", documenthubstore.ReviewInput{DocumentID: id, VersionID: version.ID, ScopeKind: "placement", ScopeID: "general", ReviewerID: "employee", Authority: "policy-owner", Decision: documenthubstore.ReviewApproved}); err != nil {
		t.Fatal(err)
	}
	if _, err = documents.PlaceDocument(ctx, "tenant-a", documenthubstore.PlaceInput{DocumentID: id, VersionID: version.ID, ScopeKind: "placement", ScopeID: "general", ActorID: "owner", CustodianID: "owner", ReviewDueAt: now.AddDate(1, 0, 0)}); err != nil {
		t.Fatal(err)
	}
	if _, err = documents.ShareDocument(ctx, "tenant-a", id, "owner", documenthubstore.GrantInput{SubjectKind: "person", SubjectID: "employee", Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	refs := []agentdocref.Reference{{DocumentID: id, VersionMode: agentdocref.ModeLatestPublished, Label: "2026 holiday guide"}}
	principals := &proactiveServicePrincipal{}
	resolver := AgentAnnouncementReferenceResolver{Hub: documents, Principals: principals}
	hub := AgentAnnouncementHubAuthority{Store: documents, Now: clock}
	classes := []dlp.DataClass{dlp.ClassPublic, dlp.ClassInternal}
	route := PersonaRunModelRoute{Purpose: personaChatReplyPurpose, ProfileClass: dlp.ClassInternal, InvokerClass: dlp.ClassInternal, ThreadClass: dlp.ClassInternal, Processing: agentmodel.ProcessingPolicy{Residency: "us-east", Retention: "NONE:0", TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied}, Egress: agentegress.Profile{ID: "test-model", Kind: agentegress.TargetModel, AllowedRegions: []string{"us-east"}, AllowedClasses: classes, Retention: agentegress.RetentionPolicy{Mode: agentegress.RetentionNone}}, TaskPolicy: agentegress.TaskPolicy{AllowedRegions: []string{"us-east"}, AllowedResultClasses: classes, ResultRetention: time.Hour}, Route: agentmodel.RouteRequest{TraceID: "route", BudgetRemainingMicros: 100, Pin: agentmodel.ModelPin{AgentVersionDigest: digest, TaskProfileID: "persona.reply", SemanticsDigest: manifest.ModelPolicy.Digest, OutputSchemaDigest: PersonaChatReplySchemaDigest, ToolSchemaDigest: foregroundDigest("tool"), Primary: agentmodel.ModelSelection{ProfileID: "test-model", ProfileDigest: strings.Repeat("a", 64), Identity: agentmodel.ModelIdentity{ProviderID: "test-provider", ModelID: "fixture", Version: "1"}}}, Task: agentmodel.TaskProfile{ID: "persona.reply", AgentVersionDigest: digest, OutputSchemaDigest: PersonaChatReplySchemaDigest, Region: "us-east", DataClasses: []string{"PUBLIC", "INTERNAL"}, MaxCostMicros: 100, MaxLatency: time.Minute}}}
	routeRaw, _ := json.Marshal(route)
	routes := agentDocEgressRoute{record: agentstore.PersonaModelRoutePolicy{TenantID: tenant, LegalEntityID: "entity-a", PolicyID: manifest.ModelPolicy.ID, PolicyVersion: int64(manifest.ModelPolicy.Version), PolicySchemaVersion: int64(manifest.ModelPolicy.SchemaVersion), PolicyDigest: manifest.ModelPolicy.Digest, Revision: 1, RoutePayload: routeRaw}}
	manager, err := lease.NewManager(proactiveLiveCustody{clock}, clock)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := NewModelLeaseSource(ModelLeaseSourceConfig{Authorities: map[string]ModelCredentialAuthority{"test-provider": &modelLeaseAuthority{handle: custody.Handle{ID: "fixture", Kind: custody.Secret, Version: "1", Tenant: "tenant-a", Region: "us-east"}}}, Leases: manager, MaxTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	policies, err := NewPersonaRunEffectivePolicyResolver(agents, mapper, clock)
	if err != nil {
		t.Fatal(err)
	}
	work := &DatabasePersonaRunModelWorkSource{personas: PersonaRunAuthorityStore{Store: personas}, manifests: personaRunManifestFactoryFake{resolver: personaRunManifestResolverFake{manifest: manifest, tenant: "tenant-a"}}, routes: routes, budgets: policies, leases: credentials, tenantUUID: mapper, workload: "fixture-worker", now: clock}
	repo, err := agentstore.NewAnnouncementStore(agents)
	if err != nil {
		t.Fatal(err)
	}
	validator, _, _, _, _ := personaRunOutputFixture(t)
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := agentsecurity.NewFinalOutputRecoveryAuthority("announcement-fixture", private)
	if err != nil {
		t.Fatal(err)
	}
	persister := AgentPersonaRunFinalOutputPersister{Stores: AgentPersonaRunFinalOutputStoreFactory{Store: personas}, Recovery: signer}
	model := &proactiveLiveModel{t: t}
	if _, err = chatRaw.PutPersonaChannelPolicy(ctx, "tenant-a", "general", 0, chatstore.PersonaChannelPolicy{MaxTier: "T0", AllowedDataClasses: []string{"PUBLIC", "INTERNAL"}, AllowedChannelClasses: []string{"PUBLIC"}, PlacementClass: "ANY_INTERNAL", ConversationSearchAllowed: true}); err != nil {
		t.Fatal(err)
	}
	audience := NewPersonaPublicAudienceAuthority(chatRaw)
	runtime := &AgentAnnouncementRuntime{Store: repo, Agents: agents, Work: work, Principals: principals, Documents: resolver, Worker: privateChatGatewayVerifiedWorker(t, now), Persister: persister, Chat: chatRaw, Audience: NewPersonaAudienceFloorAdapter(audience, audience, audience), Now: clock, DocumentAuthority: hub, Names: func(context.Context, string, string) (string, error) { return "Alex Example", nil }, LegalEntity: func(context.Context, string, string) (string, error) { return "entity-a", nil }}
	runtime.Base = PersonaRunStarterConfig{Builder: mustPersonaRunBuilder(t), Authority: runtime, Model: model, Work: work, Output: validator, Reply: agentUXSpeedReply{}, WorkerID: "fixture-worker", LeaseTTL: time.Minute, Now: clock}
	runtime.BodyClasses = &runtimeReplyClassFake{class: dlp.ClassInternal}
	model.runtime = runtime
	if _, _, _, _, _, err := runtime.facts(ctx, agentstore.Announcement{TenantKey: "tenant-a", InstallationID: "install", PersonaID: "policy-helper", ConversationID: "general", OwnerID: "owner"}); err != nil {
		t.Fatalf("runtime facts: %v", err)
	}
	if _, err := runtime.currentAudience(ctx, "tenant-a", "general"); err != nil {
		t.Fatalf("runtime audience: %v", err)
	}
	authority := &proactiveOwner{actor: AgentAnnouncementActor{TenantID: "tenant-a", TenantUUID: tenant, SubjectID: "owner"}}
	schedules := AgentAnnouncementNativeSchedules{Store: announcementScheduleStore{runtime}, Bindings: runtime, Now: clock}
	announcementService := &AgentAnnouncementService{Store: repo, Authority: authority, Schedules: schedules, Now: clock, Shares: AgentAnnouncementHubShares{Hub: documents, Principals: principals}}
	runner := &AgentAnnouncementRunner{Store: repo, Documents: resolver, Model: proactiveLiveRun{runtime, t}, Public: announcementRuntimeGate{runtime}, Delivery: runtime, Now: clock, ConversationName: func(context.Context, string, string) (string, error) { return "#general", nil }}
	surface := &AgentAnnouncementControlSurface{Service: announcementService, Runner: runner, Actors: authority, Names: proactiveNames{}, Preview: AgentAnnouncementDraftPreview{Authority: authority, Runner: runner, Reader: hub}, Now: clock}
	return surface, &AgentAnnouncementWorker{Runtime: runtime, Runner: runner}, adapter, model, &now, agentcontrols.AnnouncementDraft{InstallationID: "install", PersonaID: "policy-helper", ConversationID: "general", Instruction: "Tell employees which company holidays are coming up, using the 2026 holiday guide", Documents: refs, Cadence: "NOW", Time: "09:00", Zone: "America/New_York", IdempotencyKey: "live-now-123"}
}

func TestAgentUXProactive_PostNow_Integration(t *testing.T) {
	surface, worker, store, model, _, draft := proactiveLiveFixture(t)
	ctx := context.Background()
	for range 2 {
		reply, err := surface.CreateAnnouncement(ctx, draft)
		if err != nil || len(reply.Snapshot.Rows) != 1 || reply.Snapshot.Rows[0].ResultCode != agentstore.AnnouncementPosted {
			t.Fatalf("post now: %+v %v", reply, err)
		}
	}
	proactiveLiveAssertPosts(t, ctx, store, 1, false)
	if model.calls != 1 {
		t.Fatalf("occurrence replay called model %d times", model.calls)
	}
	record, err := surface.Service.Store.Get(ctx, worker.Runtime.Work.tenantUUID("tenant-a"), announcementID(surface.Actors.(*proactiveOwner).actor, draft.IdempotencyKey))
	if err != nil {
		t.Fatal(err)
	}
	output, err := surface.Runner.RunOccurrence(ctx, record.TenantID, record.ID, record.LastOccurrence, false)
	if err != nil || output.MessageID != record.LastMessageID {
		t.Fatalf("replay %+v %v", output, err)
	}
}

type proactiveUnavailableSchedule struct{ AgentAnnouncementScheduleOwner }

func (proactiveUnavailableSchedule) CreateAnnouncementSchedule(context.Context, AgentAnnouncementActor, agentstore.Announcement, string) (string, *time.Time, error) {
	return "", nil, ErrAgentAnnouncementUnavailable
}

func TestAgentUXProactive_FailedSaveRevokesDocumentGrant_Security_Integration(t *testing.T) {
	surface, worker, _, model, _, draft := proactiveLiveFixture(t)
	ctx := context.Background()
	surface.Service.Schedules = proactiveUnavailableSchedule{surface.Service.Schedules}
	if _, err := surface.CreateAnnouncement(ctx, draft); !errors.Is(err, agentcontrols.ErrUnavailable) {
		t.Fatalf("failed save=%v", err)
	}
	actor, err := surface.actor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := announcementID(actor, draft.IdempotencyKey)
	hub := worker.Runtime.DocumentAuthority.Store
	if err = hub.AuthorizeAnnouncementDocument(ctx, actor.TenantID, draft.Documents[0].DocumentID, "workload:policy", id); !errors.Is(err, documenthubstore.ErrDenied) {
		t.Fatalf("failed save left a service document grant: %v", err)
	}
	if _, err = surface.Service.Store.Get(ctx, actor.TenantUUID, id); !errors.Is(err, agentstore.ErrAnnouncementNotFound) || model.calls != 0 {
		t.Fatalf("failed save persisted an announcement or ran the model: calls=%d %v", model.calls, err)
	}
}

func TestAgentUXProactive_ExpiredRunCannotPost_Security_Integration(t *testing.T) {
	surface, worker, store, model, now, draft := proactiveLiveFixture(t)
	ctx := context.Background()
	model.after = func() { *now = now.Add(2 * time.Minute) }
	reply, err := surface.CreateAnnouncement(ctx, draft)
	if err != nil || reply.Snapshot.Rows[0].ResultCode != agentstore.AnnouncementFailed {
		t.Fatalf("expired run result=%+v %v", reply, err)
	}
	posts, err := store.ListPosts(ctx, chat.Principal{TenantID: "tenant-a", SubjectID: "employee"}, "tenant-a", "general", 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(posts.Posts) != 0 || model.calls != 1 {
		t.Fatalf("expired run published: %+v calls=%d %v", posts, model.calls, err)
	}
	occurrence := announcementManualOccurrence(reply.Snapshot.Rows[0].ID, draft.IdempotencyKey)
	repo, err := worker.Runtime.admissionRepository("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	admissionID, err := agentrun.AdmissionRequestID(agentrun.SourceIdentity{TenantID: "tenant-a", Kind: agentrun.SourceAnnouncement, Key: occurrence, Ref: occurrence})
	if err != nil {
		t.Fatal(err)
	}
	admission, err := repo.GetByID(ctx, admissionID)
	if err != nil {
		t.Fatal(err)
	}
	factory := &DatabasePersonaRunTenantRuntimeFactory{db: worker.Runtime.Agents, tenantUUID: worker.Runtime.Work.tenantUUID, base: worker.Runtime.Base}
	cfg, err := factory.ForPersonaRunTenant(ctx, admission.Request.Source.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := cfg.ExecutionStore.Get(ctx, admission.ID)
	if err != nil || run.State != runstate.StateExpired {
		t.Fatalf("expired occurrence retained overlap: %+v %v", run, err)
	}
}

type proactiveLateDelivery struct {
	AgentAnnouncementPublicGate
	now *time.Time
}

func (g proactiveLateDelivery) AuthorizeAnnouncementDocuments(ctx context.Context, tenant, conversation string, documents []AgentAnnouncementResolvedDocument, citations []string) (bool, int, error) {
	*g.now = g.now.Add(2 * time.Minute)
	return g.AgentAnnouncementPublicGate.AuthorizeAnnouncementDocuments(ctx, tenant, conversation, documents, citations)
}

func TestAgentUXProactive_ExpiredDeliveryCannotPost_Security_Integration(t *testing.T) {
	surface, worker, store, model, now, draft := proactiveLiveFixture(t)
	ctx := context.Background()
	surface.Runner.Public = proactiveLateDelivery{surface.Runner.Public, now}
	reply, err := surface.CreateAnnouncement(ctx, draft)
	if err != nil || reply.Snapshot.Rows[0].ResultCode != agentstore.AnnouncementFailed {
		t.Fatalf("late delivery=%+v %v", reply, err)
	}
	occurrence := announcementManualOccurrence(reply.Snapshot.Rows[0].ID, draft.IdempotencyKey)
	admissionID, err := agentrun.AdmissionRequestID(agentrun.SourceIdentity{TenantID: "tenant-a", Kind: agentrun.SourceAnnouncement, Key: occurrence, Ref: occurrence})
	if err != nil {
		t.Fatal(err)
	}
	factory := &DatabasePersonaRunTenantRuntimeFactory{db: worker.Runtime.Agents, tenantUUID: worker.Runtime.Work.tenantUUID, base: worker.Runtime.Base}
	cfg, err := factory.ForPersonaRunTenant(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	run, err := cfg.ExecutionStore.Get(ctx, admissionID)
	if err != nil || run.State != runstate.StateExpired {
		t.Fatalf("late delivery retained overlap: %+v %v", run, err)
	}
	posts, err := store.ListPosts(ctx, chat.Principal{TenantID: "tenant-a", SubjectID: "employee"}, "tenant-a", "general", 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(posts.Posts) != 0 || model.calls != 1 {
		t.Fatalf("late delivery posted: %+v calls=%d %v", posts, model.calls, err)
	}
}
func TestAgentUXProactive_WeeklyTick_Integration(t *testing.T) {
	surface, worker, store, model, now, draft := proactiveLiveFixture(t)
	ctx := context.Background()
	draft.Cadence = "WEEKLY"
	draft.Weekdays = []int{1}
	reply, err := surface.CreateAnnouncement(ctx, draft)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Snapshot.Rows[0].NextRunAt != "2026-10-05T13:00:00Z" {
		t.Fatalf("next Monday: %+v", reply)
	}
	*now = time.Date(2026, 10, 5, 13, 0, 1, 0, time.UTC)
	// Renew the deterministic workload credential for the advanced clock.
	worker.Runtime.Worker = privateChatGatewayVerifiedWorker(t, *now)
	for range 2 {
		if err = worker.TickTenant(ctx, "tenant-a"); err != nil {
			t.Fatal(err)
		}
	}
	proactiveLiveAssertPosts(t, ctx, store, 1, true)
	if model.calls != 1 {
		t.Fatalf("weekly replay model calls %d", model.calls)
	}
}
func proactiveLiveAssertPosts(t *testing.T, ctx context.Context, store *chatstore.Adapter, count int, scheduled bool) {
	t.Helper()
	posts, err := store.ListPosts(ctx, chat.Principal{TenantID: "tenant-a", SubjectID: "employee"}, "tenant-a", "general", 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(posts.Posts) != count {
		t.Fatalf("second member reads public posts %+v %v", posts, err)
	}
	post := posts.Posts[0]
	message, decoded := chatui.DecodeAnnouncementMessageBody(post.Body)
	if !decoded || post.AuthorID != "policy-helper" || post.ParentID != "" || len(message.Sources) != 1 || message.Sources[0].Title != "2026 holiday guide" || message.OwnerName != "Alex Example" || !strings.Contains(message.Text, "November 26") || message.Scheduled != scheduled || message.PostedAt.IsZero() || message.AgentName == "Agent" {
		t.Fatalf("public root lost author, sources or attribution: %+v", post)
	}
}

func TestAgentUXProactive_DocumentRefusal_Security_Integration(t *testing.T) {
	for _, withdraw := range []bool{false, true} {
		surface, worker, store, model, now, draft := proactiveLiveFixture(t)
		draft.Cadence, draft.Weekdays = "WEEKLY", []int{1}
		reply, err := surface.CreateAnnouncement(context.Background(), draft)
		if err != nil {
			t.Fatal(err)
		}
		hub := worker.Runtime.DocumentAuthority.Store
		doc := draft.Documents[0].DocumentID
		if withdraw {
			placement, err := hub.CurrentPlacement(context.Background(), "tenant-a", doc, "placement", "general")
			if err != nil {
				t.Fatal(err)
			}
			_, err = hub.Withdraw(context.Background(), "tenant-a", documenthubstore.WithdrawInput{DocumentID: doc, ScopeKind: "placement", ScopeID: "general", ActorID: "owner", ExpectedLive: placement.VersionID, Reason: "Withdrawn"})
			if err != nil {
				t.Fatal(err)
			}
		} else {
			_, err = hub.ShareDocument(context.Background(), "tenant-a", doc, "owner", documenthubstore.GrantInput{SubjectKind: "person", SubjectID: "employee", Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectDeny})
			if err != nil {
				t.Fatal(err)
			}
		}
		*now = time.Date(2026, 10, 5, 13, 0, 1, 0, time.UTC)
		worker.Runtime.Worker = privateChatGatewayVerifiedWorker(t, *now)
		for range 2 {
			if err = worker.TickTenant(context.Background(), "tenant-a"); err != nil {
				t.Fatal(err)
			}
		}
		reply, err = surface.ListAnnouncements(context.Background())
		if err != nil || reply.Snapshot.Rows[0].ResultCode != agentstore.AnnouncementRefused || reply.Snapshot.Rows[0].Reason != "1 of the documents cannot be read by everyone in #general" {
			t.Fatalf("refusal=%+v %v", reply, err)
		}
		posts, err := store.ListPosts(context.Background(), chat.Principal{TenantID: "tenant-a", SubjectID: "employee"}, "tenant-a", "general", 0, chat.Page{PageSize: 10}, chat.PostWindow{})
		if err != nil || len(posts.Posts) != 0 || withdraw && model.calls != 0 {
			t.Fatalf("refused document leaked: %+v calls=%d %v", posts, model.calls, err)
		}
	}
}

func TestAgentUXProactive_PreviewPost_Integration(t *testing.T) {
	surface, _, store, _, _, draft := proactiveLiveFixture(t)
	preview, err := surface.PreviewAnnouncement(context.Background(), draft)
	if err != nil || preview.Preview == nil || !preview.Preview.Public {
		t.Fatalf("preview=%+v %v", preview, err)
	}
	posts, err := store.ListPosts(context.Background(), chat.Principal{TenantID: "tenant-a", SubjectID: "employee"}, "tenant-a", "general", 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(posts.Posts) != 0 {
		t.Fatalf("preview posted: %+v %v", posts, err)
	}
	draft.PreviewDigest = preview.Preview.Digest
	reply, err := surface.CreateAnnouncement(context.Background(), draft)
	if err != nil || reply.Snapshot.Rows[0].ResultCode != agentstore.AnnouncementPosted {
		t.Fatalf("preview to post=%+v %v", reply, err)
	}
	proactiveLiveAssertPosts(t, context.Background(), store, 1, false)
}

func TestAgentUXProactive_DocumentChangedDuringModel_Security_Integration(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		t.Run(fmt.Sprintf("pinned=%t", pinned), func(t *testing.T) {
			proactiveReplacedDocument(t, pinned)
		})
	}
}

func proactiveReplacedDocument(t *testing.T, pinned bool) {
	t.Helper()
	surface, worker, store, model, now, draft := proactiveLiveFixture(t)
	ctx := context.Background()
	hub := worker.Runtime.DocumentAuthority.Store
	document := draft.Documents[0].DocumentID
	expected := agentstore.AnnouncementFailed
	if pinned {
		draft.Documents[0].VersionMode, draft.Documents[0].PinnedVersion = agentdocref.ModePinned, 1
		expected = agentstore.AnnouncementPosted
	}
	model.after = func() {
		placement, err := hub.CurrentPlacement(ctx, "tenant-a", document, "placement", "general")
		if err != nil {
			t.Fatal(err)
		}
		version, err := hub.SubmitCandidate(ctx, "tenant-a", documenthubstore.Version{DocumentID: document, ParentID: placement.VersionID, CreatorID: "owner", Title: "2026 holiday guide", Markdown: "The guide was corrected: Christmas is December 25, 2026.", Classification: "INTERNAL"}, placement.VersionID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = hub.RecordReview(ctx, "tenant-a", documenthubstore.ReviewInput{DocumentID: document, VersionID: version.ID, ScopeKind: "placement", ScopeID: "general", ReviewerID: "employee", Authority: "policy-owner", Decision: documenthubstore.ReviewApproved}); err != nil {
			t.Fatal(err)
		}
		if _, err = hub.PlaceDocument(ctx, "tenant-a", documenthubstore.PlaceInput{DocumentID: document, VersionID: version.ID, ScopeKind: "placement", ScopeID: "general", ActorID: "owner", ExpectedLive: placement.VersionID, CustodianID: "owner", ReviewDueAt: now.AddDate(1, 0, 0)}); err != nil {
			t.Fatal(err)
		}
	}
	reply, err := surface.CreateAnnouncement(ctx, draft)
	if err != nil || reply.Snapshot.Rows[0].ResultCode != expected || model.calls != 1 {
		t.Fatalf("changed guide sealed as the model's source: %+v calls=%d %v", reply, model.calls, err)
	}
	if pinned {
		proactiveLiveAssertPosts(t, ctx, store, 1, false)
		return
	}
	posts, err := store.ListPosts(ctx, chat.Principal{TenantID: "tenant-a", SubjectID: "employee"}, "tenant-a", "general", 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(posts.Posts) != 0 {
		t.Fatalf("old answer attributed to replacement guide: %+v %v", posts, err)
	}
}

func TestAgentUXProactive_FailureProjection_Integration(t *testing.T) {
	surface, worker, store, model, now, draft := proactiveLiveFixture(t)
	draft.Cadence, draft.Weekdays = "WEEKLY", []int{1}
	if _, err := surface.CreateAnnouncement(context.Background(), draft); err != nil {
		t.Fatal(err)
	}
	model.fail = true
	*now = time.Date(2026, 10, 5, 13, 0, 1, 0, time.UTC)
	worker.Runtime.Worker = privateChatGatewayVerifiedWorker(t, *now)
	for range 2 {
		if err := worker.TickTenant(context.Background(), "tenant-a"); err != nil {
			t.Fatal(err)
		}
	}
	reply, err := surface.ListAnnouncements(context.Background())
	if err != nil || reply.Snapshot.Rows[0].ResultCode != agentstore.AnnouncementFailed || reply.Snapshot.Rows[0].Reason != "The service is busy or unavailable. Try again in a few minutes." || model.calls != 1 {
		t.Fatalf("failure=%+v calls=%d %v", reply, model.calls, err)
	}
	posts, err := store.ListPosts(context.Background(), chat.Principal{TenantID: "tenant-a", SubjectID: "employee"}, "tenant-a", "general", 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(posts.Posts) != 0 {
		t.Fatalf("failed run posted: %+v %v", posts, err)
	}
	// A terminal failed run must not hold the next occurrence's overlap lease.
	model.fail = false
	*now = time.Date(2026, 10, 12, 13, 0, 1, 0, time.UTC)
	worker.Runtime.Worker = privateChatGatewayVerifiedWorker(t, *now)
	if err := worker.TickTenant(context.Background(), "tenant-a"); err != nil {
		t.Fatal(err)
	}
	proactiveLiveAssertPosts(t, context.Background(), store, 1, true)
	if model.calls != 2 {
		t.Fatalf("next week calls=%d", model.calls)
	}
}

func TestAgentUXProactive_PauseBeforeTick_Integration(t *testing.T) {
	surface, worker, store, model, now, draft := proactiveLiveFixture(t)
	draft.Cadence, draft.Weekdays = "WEEKLY", []int{1}
	reply, err := surface.CreateAnnouncement(context.Background(), draft)
	if err != nil {
		t.Fatal(err)
	}
	row := reply.Snapshot.Rows[0]
	_, err = surface.ControlAnnouncement(context.Background(), agentcontrols.AnnouncementCommand{ID: row.ID, Action: "PAUSE", ExpectedRevision: row.Revision, IdempotencyKey: "pause-123456"})
	if err != nil {
		t.Fatal(err)
	}
	*now = time.Date(2026, 10, 5, 13, 0, 1, 0, time.UTC)
	if err = worker.TickTenant(context.Background(), "tenant-a"); err != nil {
		t.Fatal(err)
	}
	posts, err := store.ListPosts(context.Background(), chat.Principal{TenantID: "tenant-a", SubjectID: "employee"}, "tenant-a", "general", 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(posts.Posts) != 0 || model.calls != 0 {
		t.Fatalf("paused occurrence fired %+v calls=%d %v", posts, model.calls, err)
	}
}

func TestAgentUXProactive_PauseQueuedOccurrence_Integration(t *testing.T) {
	surface, worker, store, model, now, draft := proactiveLiveFixture(t)
	ctx := context.Background()
	draft.Cadence, draft.Weekdays = "WEEKLY", []int{1}
	reply, err := surface.CreateAnnouncement(ctx, draft)
	if err != nil {
		t.Fatal(err)
	}
	row := reply.Snapshot.Rows[0]
	definition, err := surface.Service.Store.Get(ctx, worker.Runtime.Work.tenantUUID("tenant-a"), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	native, err := (announcementScheduleStore{worker.Runtime}).tenant("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := scheduled.NewOwner(native, announcementScheduleAuthority{worker.Runtime})
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := scheduled.NewOutboxWorker(owner, announcementOutbox{native}, announcementScheduleContext{worker.Runtime}, announcementScheduleInbox{worker.Runtime, worker.Runner})
	if err != nil {
		t.Fatal(err)
	}
	*now = time.Date(2026, 10, 5, 13, 0, 1, 0, time.UTC)
	if err = outbox.Plan(ctx, "tenant-a", definition.SchedulerID, *now); err != nil {
		t.Fatal(err)
	}
	pending, err := native.Pending(ctx, "tenant-a", 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("queued occurrence=%+v %v", pending, err)
	}
	repo, err := worker.Runtime.admissionRepository("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	admitter, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: worker.Runtime, Store: repo, Now: worker.Runtime.Now})
	if err != nil {
		t.Fatal(err)
	}
	admission, _, err := admitter.Admit(ctx, pending[0].Request)
	if err != nil || admission.Decision != agentrun.DecisionAccepted {
		t.Fatalf("accepted queued occurrence=%+v %v", admission, err)
	}
	_, err = surface.ControlAnnouncement(ctx, agentcontrols.AnnouncementCommand{ID: row.ID, Action: "PAUSE", ExpectedRevision: row.Revision, IdempotencyKey: "pause-queued-123"})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = worker.TickTenant(ctx, "tenant-a"); err != nil {
			t.Fatal(err)
		}
	}
	pending, err = native.Pending(ctx, "tenant-a", 10)
	if err != nil || len(pending) != 0 || model.calls != 0 {
		t.Fatalf("paused queue retained: %+v calls=%d %v", pending, model.calls, err)
	}
	reply, err = surface.ListAnnouncements(ctx)
	if err != nil {
		t.Fatal(err)
	}
	row = reply.Snapshot.Rows[0]
	if row.ResultCode != agentstore.AnnouncementFailed || row.Reason != announcementScheduleChangedReason {
		t.Fatalf("paused result=%+v", row)
	}
	_, err = surface.ControlAnnouncement(ctx, agentcontrols.AnnouncementCommand{ID: row.ID, Action: "RESUME", ExpectedRevision: row.Revision, IdempotencyKey: "resume-queued-123"})
	if err != nil {
		t.Fatal(err)
	}
	*now = time.Date(2026, 10, 12, 13, 0, 1, 0, time.UTC)
	worker.Runtime.Worker = privateChatGatewayVerifiedWorker(t, *now)
	if err = worker.TickTenant(ctx, "tenant-a"); err != nil {
		t.Fatal(err)
	}
	proactiveLiveAssertPosts(t, ctx, store, 1, true)
	if model.calls != 1 {
		t.Fatalf("resumed occurrence calls=%d", model.calls)
	}
}
