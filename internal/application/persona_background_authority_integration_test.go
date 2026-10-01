package application

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentcontextstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentdelegationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentskillgrantstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/pressly/goose/v3"
)

// This fixture represents a previously accepted invocation. Publication and
// admission history are explicitly privileged fixture seeds, not evidence of
// foreground authorization or review. All subsequent authority reads use the
// production PostgreSQL owners and an independently signed workload identity.
func TestTodo_AGENTP_008_Integration_BackgroundAuthorityRechecksDurableOwners(t *testing.T) {
	for _, mutation := range []string{"delegation epoch", "delegation revoked", "role revoked", "employment ended", "installation suspended", "skill grant revoked", "worker credential unavailable"} {
		t.Run(mutation, func(t *testing.T) {
			runtime, record, core, agents, grants := backgroundAuthorityPostgresFixture(t)
			ctx := context.Background()
			if _, found := trust.FromContext(ctx); found {
				t.Fatal("background fixture unexpectedly has a human principal")
			}
			got, err := runtime.VerifyAdmission(ctx, record.Request)
			if err != nil || got != record.Authority {
				t.Fatalf("current accepted background authority = %+v, %v; want %+v", got, err, record.Authority)
			}
			invocation := personaRunInvocation(record.Request)
			grant, err := grants.Get(record.Request.Principal.DelegatedCredentialRef)
			if err != nil {
				t.Fatal(err)
			}
			bindForegroundGrant(&invocation, grant)
			if bound, err := runtime.IsBoundT0Run(ctx, invocation); err != nil || !bound {
				t.Fatalf("real current T0 owner refused: bound=%v err=%v", bound, err)
			}
			read := agentinvoke.ThreadReadRequest{TenantID: record.Request.Source.TenantID, ConversationID: record.Request.Audience.ID, ThreadID: record.Request.Context.ID, InvokingPostID: record.Request.Source.Ref, InvokerID: record.Request.Principal.InvokerID}
			posts, err := runtime.ReadThread(WithPersonaBackgroundAdmission(ctx, record), read)
			if err != nil || len(posts) != 1 || posts[0].ID != record.Request.Source.Ref || posts[0].Body != "Explain the policy" {
				t.Fatalf("accepted background thread = %+v, %v", posts, err)
			}
			tenant := runtime.cfg.TenantUUID(values.TenantId(record.Request.Source.TenantID))
			switch mutation {
			case "delegation epoch":
				if epoch, err := grants.BumpRevocationEpoch(values.TenantId(record.Request.Source.TenantID), record.Request.Principal.InvokerID, "integration revocation"); err != nil || epoch != 2 {
					t.Fatalf("epoch=%d err=%v", epoch, err)
				}
			case "delegation revoked":
				if err := grants.Revoke(grant.GrantID, "integration revocation"); err != nil {
					t.Fatal(err)
				}
			case "role revoked":
				core.Exec(t, `UPDATE access_role SET active=false WHERE tenant_id=$1 AND role_id='background_member'`, tenant)
			case "employment ended":
				// Worker facts are append-only. Revoke current employment through
				// its real bitemporal owner instead of mutating journey_worker.
				tx, err := runtime.cfg.CoreDB.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(ctx)
				if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
					t.Fatal(err)
				}
				worker, found, err := (workforce.Store{}).Get(ctx, tx, tenant, record.Request.Principal.InvokerID)
				if err != nil || !found {
					t.Fatal("current worker unavailable")
				}
				employments, err := (aggregates.PeopleStore{}).ActiveEmploymentsForWorker(ctx, tx, tenant, worker.WorkerID, runtime.cfg.Now())
				if err != nil || len(employments) != 1 {
					t.Fatal("current employment unavailable")
				}
				previous := employments[0]
				ended, err := aggregates.NewEmployment(tenant, previous.EntityID, previous.WorkerRef, previous.LegalEntityRef, previous.EffectiveFrom, previous.EffectiveTo,
					previous.RecordedAt.Add(time.Second), previous.EmploymentType, "ENDED", previous.HireDate)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := (aggregates.PeopleStore{}).PutEmployment(ctx, tx, ended); err != nil {
					t.Fatal(err)
				}
				if err := tx.Commit(ctx); err != nil {
					t.Fatal(err)
				}
			case "installation suspended":
				agents.Exec(t, `UPDATE persona_installations SET state='SUSPENDED',suspension_reason='integration revocation',revision=revision+1,revocation_epoch=revocation_epoch+1 WHERE tenant_id=$1 AND installation_id=$2`, tenant, record.Request.InstallationID)
			case "skill grant revoked":
				core.Exec(t, `UPDATE agent_skill_grant SET revoked_at=now(),revoked_by='fixture-admin',revoked_reason='integration revocation' WHERE tenant_id=$1 AND grant_id='background-skill-grant'`, tenant)
			case "worker credential unavailable":
				runtime.cfg.Worker = &VerifiedPersonaPrivateChatWorkloadIdentitySource{}
			}
			if _, err := runtime.current(ctx, record.Request); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
				t.Fatalf("%s did not revoke current accepted owner: %v", mutation, err)
			}
			if _, err := runtime.VerifyAdmission(ctx, record.Request); err == nil {
				t.Fatalf("%s did not revoke background admission", mutation)
			}
			if bound, err := runtime.IsBoundT0Run(ctx, invocation); err == nil || bound {
				t.Fatalf("%s retained T0: %v %v", mutation, bound, err)
			}
			if _, err := runtime.ReadThread(WithPersonaBackgroundAdmission(ctx, record), read); err == nil {
				t.Fatalf("%s retained background thread access", mutation)
			}
		})
	}
}

func TestTodo_AGENTP_008_Integration_BackgroundAuthorityRejectsUnboundChatResources(t *testing.T) {
	runtime, record, _, _, grants := backgroundAuthorityPostgresFixture(t)
	ctx := context.Background()
	grant, err := grants.Get(record.Request.Principal.DelegatedCredentialRef)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.currentSkills(ctx, record.Request, grant); err != nil {
		t.Fatalf("exact current owner scope refused: %v", err)
	}
	for _, resource := range []string{"chat.current", personaChatAuthorityResource(record.Request.Source.TenantID, "another-room", record.Request.Context.ID, record.Request.Source.Ref),
		personaChatAuthorityResource(record.Request.Source.TenantID, record.Request.Audience.ID, record.Request.Context.ID, "another-post")} {
		changed := grant
		changed.SkillAuthorities = trust.CloneSkillAuthorities(grant.SkillAuthorities)
		for skill, authority := range changed.SkillAuthorities {
			authority.Resources = []string{resource}
			changed.SkillAuthorities[skill] = authority
		}
		if err := runtime.currentSkills(ctx, record.Request, changed); !errors.Is(err, errPersonaRunCurrentAuthority) {
			t.Fatalf("unbound resource %s retained current skill authority: %v", resource, err)
		}
	}
}

func backgroundAuthorityPostgresFixture(t *testing.T) (*PersonaBackgroundRuntime, agentrun.Record, *pgtest.DB, *pgtest.DB, agentdelegation.GrantStore) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC)
	const tenant = "ironridge-demo"
	tenantID := pgstore.TenantID(tenant)
	mapper := func(ref values.TenantId) uuid.UUID {
		if ref == tenant {
			return tenantID
		}
		return uuid.Nil
	}
	core := pgtest.New(t)
	pool, err := pgxadapter.NewPool(ctx, core.URL, map[string]string{"search_path": core.Schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	workflow, err := pgstore.New(pool, pgstore.WithCellID("cell-background-authority-test"))
	if err != nil {
		t.Fatal(err)
	}
	if err = workflow.Bootstrap(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	if _, _, err = bootstrapLocalDevWorkforce(ctx, pool, tenant); err != nil {
		t.Fatal(err)
	}
	pack, ok := demoworkforce.PackFor(tenant)
	if !ok {
		t.Fatal("missing actual demo workforce pack")
	}
	workers, err := pack.Plan(tenantID)
	if err != nil || len(workers) == 0 {
		t.Fatalf("workforce plan: %v", err)
	}
	invoker := workers[0].Row.WorkerKey
	legal, err := (&PersonaBackgroundRuntime{cfg: PersonaBackgroundRuntimeConfig{CoreDB: pool, TenantUUID: mapper, Now: func() time.Time { return now }}}).currentLegalEntity(ctx, agentrun.Request{Source: agentrun.SourceIdentity{TenantID: tenant}, Principal: agentrun.PrincipalChain{InvokerID: invoker}})
	if err != nil {
		t.Fatalf("actual workforce legal authority: %v", err)
	}
	core.Exec(t, `INSERT INTO access_role(tenant_id,role_id,version,name,updated_by) VALUES($1,'background_member',1,'Background member','fixture-admin')`, tenantID)
	core.Exec(t, `INSERT INTO worker_access_role_set(tenant_id,worker_ref,version,updated_by) VALUES($1,$2,1,'fixture-admin') ON CONFLICT DO NOTHING`, tenantID, invoker)
	core.Exec(t, `INSERT INTO worker_access_role_assignment(tenant_id,worker_ref,role_id) VALUES($1,$2,'background_member')`, tenantID, invoker)
	core.Exec(t, `INSERT INTO role_organization_visibility(tenant_id,organization_scope_id,role_id,version,mode,updated_by) VALUES($1,'background-org','background_member',1,'ALL','fixture-admin')`, tenantID)
	core.Exec(t, `INSERT INTO agent_current_population(tenant_id,membership_id,subject_ref,population_id,source_identity,revision,effective_from) VALUES($1,$2,$3,'background-staff','integration-fixture',1,'2020-01-01')`, tenantID, uuid.New(), invoker)
	core.Exec(t, `INSERT INTO agent_skill_grant(tenant_id,grant_id,skill_id,skill_version,roles,population,organization_scopes,purposes,not_before,granted_by,granted_at,admin_evidence_ref) VALUES($1,'background-skill-grant','persona.chat_reply',1,ARRAY['background_member'],'background-staff',ARRAY['background-org'],ARRAY['persona-mention'],'2020-01-01','fixture-admin',now(),'integration-fixture')`, tenantID)
	principalID := uuid.New()
	core.Exec(t, `INSERT INTO principal(tenant_id,principal_id,kind,subject,assurance,authn_method) VALUES($1,$2,'SERVICE','background-persona','AAL2','workload')`, tenantID, principalID)

	agents := pgtest.NewEmpty(t)
	if err = agentstore.Migrate(ctx, agents.SQL); err != nil {
		t.Fatal(err)
	}
	agents.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenantID)
	u, err := url.Parse(agents.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", agents.Schema)
	u.RawQuery = q.Encode()
	store, err := agentstore.New(ctx, agentstore.Config{DSN: u.String(), CoreDSN: "postgres://core:unused@core.invalid:5432/core", MaxConns: 4})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	personas, err := agentpersonastore.New(store, mapper)
	if err != nil {
		t.Fatal(err)
	}
	caps := capability.NewRegistry()
	skills := agentskills.NewRegistry(caps)
	pin, err := bindPersonaChatReplySkill(caps, skills)
	if err != nil {
		t.Fatal(err)
	}
	manifest := personaRunTestManifest()
	manifest.InstructionsDigest, err = store.SaveInstructionContent(ctx, tenantID, "Use permitted records.")
	if err != nil {
		t.Fatal(err)
	}
	manifestDigest, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SaveManifest(ctx, tenantID, manifest, 0); err != nil {
		t.Fatal(err)
	}
	profile := personaRunTestProfile(manifest, manifestDigest)
	profile.Audience = agentpersona.Audience{Roles: []string{"background_member"}, Populations: []string{"background-staff"}, OrganizationScopes: []string{"background-org"}}
	profile.SkillPins = []agentskills.SkillPin{pin}
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(sealed.Profile)
	if err != nil {
		t.Fatal(err)
	}
	agents.Exec(t, `INSERT INTO persona_versions(tenant_id,persona_id,version,agent_version,handle,display_name,profile,content_digest) VALUES($1,'persona-a',2,'agent-a@4','persona-a','Persona A',$2::jsonb,$3)`, tenantID, string(body), sealed.Digest)
	agents.Exec(t, `INSERT INTO persona_lifecycle_events(tenant_id,event_id,persona_id,persona_version,from_state,to_state,reason,actor_id,occurred_at,profile_digest,review_digest,reviewer_id,evaluation_digest,evaluation_profile_digest,evaluation_suite_digest) VALUES($1,'fixture-publication','persona-a',2,'IN_REVIEW','PUBLISHED','privileged integration seed','independent-reviewer',$2,$3,$4,'independent-reviewer',$5,$3,$6)`, tenantID, now.Add(-time.Minute), sealed.Digest, foregroundDigest("fixture-review"), foregroundDigest("fixture-evaluation"), foregroundDigest("fixture-suite"))
	agents.Exec(t, `INSERT INTO persona_agent_principal_binding(tenant_id,persona_id,persona_version,principal_id,provisioned_at) VALUES($1,'persona-a',2,$2,$3)`, tenantID, principalID, now.Add(-time.Minute))
	agents.Exec(t, `INSERT INTO persona_run_policy(tenant_id,legal_entity_id,revision,effective_from,max_cost_micros,max_input_tokens,max_output_tokens,max_run_duration_ms) VALUES($1,$2,1,'2020-01-01',100,1000,100,90000)`, tenantID, legal)

	chatDB := pgtest.NewEmpty(t)
	migrations, err := fs.Sub(chatstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, chatDB.SQL, migrations, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	u, err = url.Parse(chatDB.URL)
	if err != nil {
		t.Fatal(err)
	}
	q = u.Query()
	q.Set("search_path", chatDB.Schema)
	u.RawQuery = q.Encode()
	chatOwner, err := chatstore.New(ctx, chatstore.Config{DSN: u.String()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chatOwner.Close)
	adapter := chatstore.NewAdapter(chatOwner)
	conversation := chat.Conversation{TenantID: tenant, ID: "background-room", Kind: chat.PrivateChannel, Name: "Background room", OwnerID: invoker, Revision: 1}
	if _, err = adapter.CreateConversationWithAudiencePolicy(ctx, conversation, []chat.Membership{{TenantID: tenant, HomeTenantID: tenant, ConversationID: conversation.ID, SubjectID: invoker, Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, "", chatstore.AudiencePolicy{RoleMode: 1, Classification: "INTERNAL", AllowedTenants: []string{tenant}}); err != nil {
		t.Fatal(err)
	}
	post, err := adapter.SendPost(ctx, chat.SendPostRequest{Principal: chat.Principal{TenantID: tenant, SubjectID: invoker}, TenantID: tenant, ConversationID: conversation.ID, IdempotencyKey: "background-invocation"}, chat.Post{AuthorID: invoker, Body: "Explain the policy"})
	if err != nil {
		t.Fatal(err)
	}
	contexts, err := agentcontextstore.NewWithTenantUUID(store, func(ref string) uuid.UUID { return mapper(values.TenantId(ref)) })
	if err != nil {
		t.Fatal(err)
	}
	snapshots := PersonaCheckpointThreadSnapshotSource{Source: adapter, Contexts: contexts}
	snapshot, err := snapshots.CaptureThreadSnapshot(ctx, chat.ThreadSnapshotRequest{Principal: chat.Principal{TenantID: tenant, SubjectID: invoker}, TenantID: tenant, ConversationID: conversation.ID, ThreadID: post.ID, InvokingPostID: post.ID, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	audience, err := chatOwner.CaptureAudienceSnapshot(ctx, tenant, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := json.Marshal(agentpersonastore.ChannelPolicy{MaxTier: "T0", AllowedDataClasses: []string{"INTERNAL"}, AlwaysPrivate: true, AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationPrivate}})
	if err != nil {
		t.Fatal(err)
	}
	agents.Exec(t, `INSERT INTO persona_installations(tenant_id,installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,revision,revocation_epoch,created_at,updated_at) VALUES($1,'background-install','persona-a',2,$2,'PRIVATE',$3,$4::jsonb,'ACTIVE',1,1,$5,$5)`, tenantID, conversation.ID, invoker, string(policy), now)
	request := agentrun.Request{Source: agentrun.SourceIdentity{TenantID: tenant, Kind: agentrun.SourcePersonaMention, Key: "background-invocation", Ref: post.ID}, Persona: &agentrun.PersonaRef{ID: "persona-a", Version: "2", Digest: sealed.Digest}, LegalEntity: legal, Agent: agentrun.VersionRef{AgentID: manifest.ID, Version: "4", Digest: manifestDigest}, InstallationID: "background-install", Principal: agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, AgentPrincipalID: principalID.String(), InvokerID: invoker, DelegatedCredentialRef: "background-grant"}, Purpose: personaChatReplyPurpose, Audience: agentrun.AudienceScope{ID: conversation.ID, SnapshotID: audience.SnapshotID, Digest: audience.Digest}, Context: agentrun.ContextScope{ID: post.ID, SnapshotID: snapshot.SnapshotID, Digest: snapshot.Digest}, Deadline: now.Add(time.Minute), Budget: agentrun.Budget{MaxCostMicros: 100, MaxInputTokens: 1000, MaxOutputTokens: 100}, CauseID: "background-invocation"}
	grant := privateChatGatewayGrant(agentrun.Record{Request: request}, runstate.Run{}, now)
	grant.OrganizationScopeID = "background-org"
	grant.SkillScopes = map[string][]string{pin.ID: {agentgate.PrivateChatReplyScope}}
	grant.PlanSkillSetDigest = skillDigest(grant.SkillScopes)
	grant.SkillAuthorities = trust.SkillAuthorities{pin.ID: {Capabilities: []string{agentgate.PrivateChatReplyScope}, Resources: []string{personaChatAuthorityResource(request.Source.TenantID, request.Audience.ID, request.Context.ID, request.Source.Ref)}, Purposes: []string{request.Purpose}}}
	grant.Authority.OrganizationScopeID = grant.OrganizationScopeID
	grant.Authority.Capabilities = []string{agentgate.PrivateChatReplyScope}
	grant.Authority.Resources = []string{"chat.current"}
	grant.Authority.SkillAuthorities = trust.CloneSkillAuthorities(grant.SkillAuthorities)
	grantFactory, err := agentdelegationstore.New(pool, mapper)
	if err != nil {
		t.Fatal(err)
	}
	grantStore, err := grantFactory.ForTenant(ctx, values.TenantId(tenant))
	if err != nil {
		t.Fatal(err)
	}
	if err = grantStore.Save(grant); err != nil {
		t.Fatal(err)
	}
	agents.Exec(t, `INSERT INTO persona_invocations(tenant_id,invocation_id,conversation_id,thread_id,post_id,invoker_id,persona_id,persona_version,installation_id,mode,skills,actor,owner_id,state) VALUES($1,$2,$3,$4,$4,$5,'persona-a','2','background-install','ON_BEHALF_OF','{}','{}',$5,'CLAIMED')`, tenantID, request.Source.Key, conversation.ID, post.ID, invoker)
	id, err := agentrun.AdmissionRequestID(request.Source)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := agentrun.AdmissionRequestDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	budgets, err := NewPersonaRunEffectivePolicyResolver(store, mapper, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	currentPolicy, err := budgets.Resolve(ctx, tenant, legal)
	if err != nil {
		t.Fatal(err)
	}
	record := agentrun.Record{ID: id, RequestDigest: digest, Request: request, Decision: agentrun.DecisionAccepted, AdmittedAt: now, Authority: agentrun.AuthoritySnapshot{Agent: request.Agent, InstallationID: request.InstallationID, Principal: request.Principal, Audience: request.Audience, Context: request.Context, BudgetCeiling: currentPolicy.Budget, GrantRef: grant.GrantID, PolicyDigest: currentPolicy.PolicyDigest}}
	repository, err := agentrunstore.NewAdmissionRepository(store, tenantID, values.TenantId(tenant))
	if err != nil {
		t.Fatal(err)
	}
	if _, created, err := repository.CreateOrGet(ctx, record); err != nil || !created {
		t.Fatalf("seed valid prior admission: %v %v", created, err)
	}
	skillGrants, err := agentskillgrantstore.New(pool, mapper)
	if err != nil {
		t.Fatal(err)
	}
	principal := PersonaRunAgentPrincipalResolver{Bindings: AgentPersonaRunPrincipalBindingReader{Store: personas}, Principals: GovernancePersonaPrincipalAuthority{DB: pool, TenantUUID: mapper}, Now: func() time.Time { return now }}
	manifestFactory := TenantPersonaRunManifestResolver(func(_ context.Context, ref string) (personaRunManifestResolver, error) {
		return personaRuntimeManifestReader{AgentManifestStoreAdapter: AgentManifestStoreAdapter{Store: store, TenantID: mapper(values.TenantId(ref))}, tenant: ref}, nil
	})
	runtime, err := NewPersonaBackgroundRuntime(PersonaBackgroundRuntimeConfig{Foreground: &DatabasePersonaRunAdmissionAuthority{}, ForegroundThreads: PersonaAtomicThreadReader{Snapshots: snapshots}, CoreDB: pool, Agents: store, Personas: PersonaRunAuthorityStore{Store: personas}, Manifests: manifestFactory, Grants: grantFactory, Skills: &AgentSkillSource{skills: skills, grants: tenantGrantProvider{store: skillGrants}}, Contexts: contexts, Audience: chatOwner, Threads: adapter, Principal: principal, Budgets: budgets, Worker: privateChatGatewayVerifiedWorker(t, now), TenantUUID: mapper, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return runtime, record, core, agents, grantStore
}
