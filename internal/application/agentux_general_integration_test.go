package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/roleaccessstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/pressly/goose/v3"
)

func TestAgentUXGeneral_PublishRunnable_Integration(t *testing.T) {
	agentUXGeneralPublishRunnable(t, false, false)
}

func TestAgentUXGeneral_PolicyUpgrade_Integration(t *testing.T) {
	agentUXGeneralPublishRunnable(t, true, false)
}

func agentUXGeneralPublishRunnable(t *testing.T, upgrade, workspaceSearch bool) {
	ctx := context.Background()
	coreDB, agentDB := pgtest.New(t), pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, agentDB.SQL); err != nil {
		t.Fatal(err)
	}
	mapper := tenantKeyMapper[values.TenantId](pgstore.TenantID)
	core, err := pgxadapter.NewPool(ctx, personaChatSchemaDSN(t, coreDB.URL, coreDB.Schema), map[string]string{"role": "hcmnext_app"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(core.Close)
	coreOwner, err := pgxadapter.NewPool(ctx, personaChatSchemaDSN(t, coreDB.URL, coreDB.Schema), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(coreOwner.Close)
	coreStore, err := pgstore.New(coreOwner, pgstore.WithCellID("general-test"))
	if err != nil {
		t.Fatal(err)
	}
	agents := commonAgentOpenIntegrationStore(t, agentDB)
	now := time.Now().UTC().Add(-time.Second).Truncate(time.Microsecond)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	evaluations, err := localAgentDemoEvaluationAuthority(key, localAgentDemoTenant, mapper, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	reviews, err := agentpersonastore.NewDurableReviewAuthority(mapper)
	if err != nil {
		t.Fatal(err)
	}
	personas, err := agentpersonastore.NewWithPublicationAuthorities(agents, mapper, reviews, evaluations)
	if err != nil {
		t.Fatal(err)
	}
	reviewConn := agentDB.NewConn(t)
	if _, err := reviewConn.Exec(ctx, "SET ROLE hcmnext_persona_review_authority"); err != nil {
		t.Fatal(err)
	}
	writer, err := agentpersonastore.NewDurablePersonaReviewIssuer(reviewConn, mapper)
	if err != nil {
		t.Fatal(err)
	}
	reviewAuth, err := NewCurrentPersonaReviewGrantAuthority(agents, mapper)
	if err != nil {
		t.Fatal(err)
	}
	reviewService, err := NewPersonaReviewIssuanceService(reviewAuth, writer)
	if err != nil {
		t.Fatal(err)
	}
	agentDSN := personaChatSchemaDSN(t, agentDB.URL, agentDB.Schema)
	signing := localOpenAIMaterialFixture()
	_, policyKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signing.PolicySeed = base64.StdEncoding.EncodeToString(policyKey.Seed())
	runtime := &LocalPersonaRuntimeProvisioner{Config: LocalAgentDemoConfig{Tenant: localAgentDemoTenant, AgentDatabaseURL: agentDSN, RouteAuthorityDatabaseURL: agentDSN}, Tenants: []string{"ironridge-demo", "harborcare-demo"}, Core: core, Agents: agents, Personas: personas, Evaluations: evaluations, Signing: signing, DeploymentPath: filepath.Join(t.TempDir(), "deployment.json"), Now: func() time.Time { return now }}
	runner := &localPersonaAdminEvaluationRunner{store: personas, agents: agents, tenant: localAgentDemoTenant, tenants: runtime.Tenants, tenantUUID: mapper, privateKey: key, now: func() time.Time { return now }, newRunID: uuid.NewString}
	for _, tenant := range runtime.Tenants {
		t.Run(tenant, func(t *testing.T) {
			if err := coreStore.Bootstrap(ctx, tenant); err != nil {
				t.Fatal(err)
			}
			if _, _, err := bootstrapLocalDevWorkforce(ctx, coreOwner, tenant); err != nil {
				t.Fatal(err)
			}
			agentDB.Exec(t, "INSERT INTO tenant(tenant_id) VALUES($1)", mapper(values.TenantId(tenant)))
			pack, _ := demoworkforce.PackFor(tenant)
			owner, steward, err := localDevPolicyHelperOwners(pack, mapper(values.TenantId(tenant)))
			if err != nil {
				t.Fatal(err)
			}
			reviewer, err := localDevPolicyHelperReviewer(pack, mapper(values.TenantId(tenant)))
			if err != nil {
				t.Fatal(err)
			}
			principal, err := localAgentDemoPrincipal(now, tenant, owner, pack.OrgScope())
			if err != nil {
				t.Fatal(err)
			}
			adminCtx := trust.WithPrincipal(ctx, principal)
			if upgrade {
				agentUXGeneralUpgradePolicyContracts(t, ctx, core, agents, agentDB, mapper, principal.Tenant(), runtime.Signing, now, true)
			}
			actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: owner}
			reviewerPrincipal, err := localAgentDemoPrincipal(now, tenant, reviewer, pack.OrgScope())
			if err != nil {
				t.Fatal(err)
			}
			reviewerCtx := trust.WithPrincipal(ctx, reviewerPrincipal)
			reviewerActor := PersonaAdminCommandActor{Principal: reviewerPrincipal, Tenant: principal.Tenant(), Subject: reviewer}
			agentDB.Exec(t, `INSERT INTO persona_review_grant(tenant_id,grant_id,principal_id,permission,granted_at,expires_at) VALUES($1,$2,$3,'persona:review',now()-interval '1 hour',now()+interval '1 day')`, mapper(principal.Tenant()), uuid.NewString(), reviewer)
			for _, starterID := range []string{"hcmnext.persona_template.policy_helper", localAgentDemoAssistantStarterID} {
				// Both starters get their first image here, so the preparation loop below
				// meets a cell that already holds earlier versions of each, as the
				// review cell does: Assistant's first image is upgraded, not created.
				starter, _ := agenttemplate.PersonaStarterFor(starterID, 1)
				manifest, err := ensureLocalAgentDemoAssistantManifest(ctx, agents, mapper(principal.Tenant()), starter)
				if err != nil {
					t.Fatal(err)
				}
				personaID := localAgentDemoPersonaID
				if starter.Handle == localAgentDemoAssistantAgentID {
					personaID = localAgentDemoAssistantPersonaID
				}
				profile := personaStarterProfile(starter, PersonaStarterDraftRequest{PersonaID: personaID, AvatarRef: "avatar", OrganizationScopes: []string{pack.OrgScope()}, BusinessOwnerID: owner, TechnicalStewardID: steward}, manifest, personaStarterInstructions(starter))
				profile.DataClassesRead = []string{"PUBLIC", "INTERNAL"}
				sealed, err := agentpersona.Seal(profile)
				if err != nil {
					t.Fatal(err)
				}
				drafts := &PersonaAdminDraftService{Store: personaAdminDraftStoreAdapter{store: personas}, Authorizer: &personaCreateAuthorizerFake{allowedTenant: principal.Tenant()}, Profiles: agentDemoProfileBuilder{manifest: manifest}, Clock: personaAdminDraftClock{now: func() time.Time { return now }}}
				executor := NewPersonaAdminLifecycleExecutor(personaAdminLifecycleStoreAdapter{store: personas}, personaAdminLifecycleAuthorizerFake{}, drafts, nil, reviewService, personaAdminStoredEvidence{store: personas}, nil, nil, func() time.Time { return now }, uuid.NewString, runtime)
				executor.Evaluations = runner
				if err := executor.ExecutePersonaAdminCommand(adminCtx, actor, PersonaAdminCommand{Action: PersonaAdminCreateDraft, PersonaID: profile.PersonaID, Version: sealed, BusinessOwnerID: owner, TechnicalStewardID: steward}); err != nil {
					t.Fatal(err)
				}
				for version := int64(1); version <= 2; version++ {
					if version == 2 {
						profile.Version = 2
						sealed, err = agentpersona.Seal(profile)
						if err != nil {
							t.Fatal(err)
						}
						if err := executor.ExecutePersonaAdminCommand(adminCtx, actor, PersonaAdminCommand{Action: PersonaAdminCreateVersion, PersonaID: profile.PersonaID, Version: sealed, BusinessOwnerID: owner, TechnicalStewardID: steward}); err != nil {
							t.Fatal(err)
						}
					}
					if err := executor.ExecutePersonaAdminCommand(adminCtx, actor, PersonaAdminCommand{Action: PersonaAdminRequestReview, PersonaID: profile.PersonaID}); err != nil {
						t.Fatal(err)
					}
					if err := executor.ExecutePersonaAdminCommand(adminCtx, actor, PersonaAdminCommand{Action: PersonaAdminReview, PersonaID: profile.PersonaID, Decision: "APPROVE"}); err == nil {
						t.Fatal("owner approved their own version")
					}
					if err := executor.ExecutePersonaAdminCommand(reviewerCtx, reviewerActor, PersonaAdminCommand{Action: PersonaAdminReview, PersonaID: profile.PersonaID, Decision: "APPROVE"}); err != nil {
						t.Fatal(err)
					}
					if err := executor.ExecutePersonaAdminCommand(adminCtx, actor, PersonaAdminCommand{Action: PersonaAdminRunEvaluation, PersonaID: profile.PersonaID}); err != nil {
						t.Fatal(err)
					}
					if err := executor.ExecutePersonaAdminCommand(adminCtx, actor, PersonaAdminCommand{Action: PersonaAdminPublish, PersonaID: profile.PersonaID}); err != nil {
						t.Fatal(err)
					}
					scoped, _ := personas.Scoped(principal.Tenant())
					binding, err := scoped.ResolvePersonaAgentPrincipal(ctx, profile.PersonaID, version)
					if err != nil {
						t.Fatal(err)
					}
					resolver := PersonaRunAgentPrincipalResolver{Bindings: AgentPersonaRunPrincipalBindingReader{Store: personas}, Principals: GovernancePersonaPrincipalAuthority{DB: core, TenantUUID: mapper}, Now: func() time.Time { return now }}
					if !(PersonaCatalogRuntimeStatus{Store: personas, Principal: resolver}).PersonaRuntimeReady(ctx, principal.Tenant(), profile.PersonaID, version) {
						t.Fatal("runnable version was not repairable")
					}
					resolved, err := resolver.Resolve(ctx, principal.Tenant(), profile.PersonaID, version)
					if err != nil || resolved != binding.PrincipalID.String() {
						t.Fatalf("resolved principal=%q err=%v", resolved, err)
					}
					if version == 2 {
						previous, err := scoped.ResolvePersonaAgentPrincipal(ctx, profile.PersonaID, 1)
						if err != nil || previous.PrincipalID == binding.PrincipalID {
							t.Fatalf("versions share an identity: %+v %+v %v", previous, binding, err)
						}
					}
					legal, err := (PersonaRunLegalEntityResolver{DB: core, TenantUUID: mapper, Now: func() time.Time { return now }}).Resolve(adminCtx, agentinvoke.RunRequest{TenantID: tenant, InvokerID: owner, Mode: agentinvoke.OnBehalfOf})
					if err != nil {
						t.Fatal(err)
					}
					policy, _ := LocalPersonaOpenAIModelPolicyReference()
					if _, err := agents.CurrentPersonaModelRoutePolicyForAgent(ctx, mapper(principal.Tenant()), legal, policy.ID, int64(policy.Version), int64(policy.SchemaVersion), policy.Digest, profile.Manifest.Digest, now); err != nil {
						t.Fatal(err)
					}
					installation := agentpersonastore.PersonaInstallation{TenantID: principal.Tenant(), InstallationID: uuid.NewString(), PersonaID: profile.PersonaID, PersonaVersion: version, ConversationID: starter.Handle, ConversationClass: agentpersonastore.ConversationPublic, InstallerID: owner, ChannelPolicy: agentpersonastore.ChannelPolicy{MaxTier: "T0", AllowedDataClasses: []string{"PUBLIC", "INTERNAL"}, AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationPublic}}, State: agentpersonastore.InstallationActive, Revision: 1, RevocationEpoch: 1}
					if _, _, err := scoped.ReplaceActiveInstallation(ctx, installation); err != nil {
						t.Fatal(err)
					}
					if changed, err := ReconcileRestoredAgentInstallations(ctx, agents, personas, core, principal.Tenant(), mapper(principal.Tenant()), now); err != nil || changed != 0 {
						t.Fatalf("reconcile changed=%d err=%v", changed, err)
					}
					rows, err := scoped.ListActiveInstallations(ctx, starter.Handle)
					if err != nil || len(rows) != 1 || rows[0].PersonaVersion != version {
						t.Fatalf("active placements=%+v err=%v", rows, err)
					}
					otherTenant := values.TenantId("harborcare-demo")
					if tenant == otherTenant.String() {
						otherTenant = localAgentDemoTenant
					}
					if _, err := resolver.Resolve(ctx, otherTenant, profile.PersonaID, version); err == nil { // Other tenant may already own this persona; identities must still differ.
						other, _ := personas.Scoped(otherTenant)
						otherBinding, _ := other.ResolvePersonaAgentPrincipal(ctx, profile.PersonaID, version)
						if otherBinding.PrincipalID == binding.PrincipalID {
							t.Fatal("identity crossed tenants")
						}
					}
					row, err := scoped.GetVersion(ctx, profile.PersonaID, version)
					if err != nil {
						t.Fatal(err)
					}
					evidence, err := scoped.ResolvePublicationEvidence(ctx, profile.PersonaID, version)
					if err != nil {
						t.Fatal(err)
					}
					forged := actor
					forged.Tenant = otherTenant
					if err := runtime.ProvisionPersonaRuntime(adminCtx, forged, row, profile, evidence); !errors.Is(err, ErrPersonaAdminRuntimeUnavailable) {
						t.Fatalf("forged tenant err=%v", err)
					}
					evidence.EvaluationRunID = "forged-evaluation"
					if err := runtime.ProvisionPersonaRuntime(adminCtx, actor, row, profile, evidence); !errors.Is(err, ErrPersonaAdminRuntimeUnavailable) {
						t.Fatalf("forged evidence err=%v", err)
					}
					var decoded agentpersona.PersonaProfile
					if json.Unmarshal(row.Profile, &decoded) != nil {
						t.Fatal("stored profile not JSON")
					}
					if verified, err := agentpersona.Seal(decoded); err != nil || verified.Digest != row.ContentDigest {
						t.Fatalf("published bytes changed %v", err)
					}
				}
				if upgrade && starter.Handle == localAgentDemoAgentID {
					agentUXGeneralUpgradePolicyContracts(t, ctx, core, agents, agentDB, mapper, principal.Tenant(), runtime.Signing, now, false)
				}
			}
		})
	}
	if t.Failed() {
		return
	}
	// Replay the actual preparation loop over published versions in a live chat
	// composition. No paid model is called by these deterministic fixtures.
	tenant := values.TenantId(localAgentDemoTenant)
	scoped, _ := personas.Scoped(tenant)
	policyRow, err := scoped.GetVersion(ctx, localAgentDemoPersonaID, 2)
	if err != nil {
		t.Fatal(err)
	}
	var policyProfile agentpersona.PersonaProfile
	if json.Unmarshal(policyRow.Profile, &policyProfile) != nil {
		t.Fatal("policy profile unavailable")
	}
	roles := roleaccessstore.New(core, mapper, productFeatureCatalog()...)
	if err := roles.Bootstrap(ctx, tenant, "system:bootstrap"); err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrapLocalDevRoleAssignments(ctx, core, localAgentDemoTenant); err != nil {
		t.Fatal(err)
	}
	// A day later: a review lasts as long as its reviewer's grant, so every
	// decision and grant the cell holds is now stale. A rerun of the preparation
	// on such a cell failed with "review decision revoked or stale"; it must now
	// leave the published versions alone and give the new Assistant version a
	// fresh reviewer grant, review, evaluation and publication.
	agentDB.Exec(t, `UPDATE persona_review_decision SET revoked_at=now(), revoked_by='test: a day later'`)
	agentDB.Exec(t, `UPDATE persona_review_grant SET revoked_at=now(), revoked_by='test: a day later'`)
	if _, err := scoped.ResolvePublicationEvidence(ctx, localAgentDemoPersonaID, 2); !errors.Is(err, agentpersonastore.ErrPublicationEvidenceRequired) {
		t.Fatalf("published Policy Helper still has current evidence: %v", err)
	}
	now = time.Now().UTC().Truncate(time.Microsecond)
	chatDB := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, chatDB)
	chatDSN := personaChatSchemaDSN(t, chatDB.URL, chatDB.Schema)
	chat, err := chatstore.New(ctx, chatstore.Config{DSN: chatDSN})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chat.Close)
	lazyReferences := &lazyPersonaReferenceSource{}
	chatRuntime, err := composeChat(ctx, ServeConfig{Profile: ServeProfileLocalDev, ChatEnabled: true, ChatDatabaseURL: chatDSN, ChatCursorKey: "general-integration"}, time.Now, newCurrentWorkerChatFacts(roles, core, mapper, nil), coreOwner, nil, ChatComposition{PersonaReferences: lazyReferences})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chatRuntime.close)
	principal, err := localAgentDemoPrincipal(now, localAgentDemoTenant, localAgentDemoAdmin, policyProfile.Audience.OrganizationScopes[0])
	if err != nil {
		t.Fatal(err)
	}
	adminCtx := trust.WithPrincipal(ctx, principal)
	publicID := localDevPersonaDemoConversationID(localAgentDemoTenant, "general")
	chatPrincipal := chatcore.Principal{TenantID: localAgentDemoTenant, SubjectID: localAgentDemoAdmin}
	if _, err := chatRuntime.service.CreateConversation(adminCtx, chatcore.CreateConversationRequest{Principal: chatPrincipal, TenantID: localAgentDemoTenant, ConversationID: publicID, Kind: chatcore.PublicChannel, Name: "General", IdempotencyKey: "general-create"}); err != nil {
		t.Fatal(err)
	}
	if _, err := chatRuntime.service.AddMembership(adminCtx, chatcore.AddMembershipRequest{Principal: chatPrincipal, Membership: chatcore.Membership{ConversationID: publicID, TenantID: localAgentDemoTenant, HomeTenantID: localAgentDemoTenant, SubjectID: "ir-008-curtis-bell", Role: chatcore.Member, HistoryVisibility: chatcore.FullHistory}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ProvisionLocalDevPersonaChatPolicy(ctx, chat, ServeProfileLocalDev, localAgentDemoTenant, publicID, "general"); err != nil {
		t.Fatal(err)
	}
	preparation := localAgentDemoPreparation{config: runtime.Config, core: core, agents: agents, personas: personas, evaluations: evaluations, mapper: mapper, key: key, material: runtime.Signing, admin: principal, chat: chat, repairStore: chat, chatService: chatRuntime.service, roleAccess: roles, publicConversation: publicID, deploymentPath: runtime.DeploymentPath, now: now}
	preparation.config.Profile = ServeProfileLocalDev
	reviewLogin, password := "general_review_"+strings.ReplaceAll(uuid.NewString(), "-", ""), uuid.NewString()
	agentDB.Exec(t, fmt.Sprintf("CREATE ROLE %s LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS PASSWORD '%s'", reviewLogin, password))
	agentDB.Exec(t, "GRANT "+agentstore.PersonaReviewAuthorityRole+" TO "+reviewLogin)
	t.Cleanup(func() { agentDB.Exec(t, "DROP ROLE "+reviewLogin) })
	reviewURL, err := url.Parse(agentDSN)
	if err != nil {
		t.Fatal(err)
	}
	reviewURL.User = url.UserPassword(reviewLogin, password)
	preparation.config.ReviewAuthorityDatabaseURL = reviewURL.String()
	// The review pool validates isolation before connecting. These unused URLs
	// identify the separate data planes without opening additional databases.
	preparation.config.DatabaseURL = "postgres://general-core@127.0.0.1:18540/general_core"
	for _, seed := range []localAgentDemoSeed{{localAgentDemoPersonaID, "hcmnext.persona_template.policy_helper"}, {localAgentDemoAssistantPersonaID, localAgentDemoAssistantStarterID}} {
		first, direct, err := preparation.prepare(adminCtx, seed, policyProfile)
		if err != nil {
			t.Fatalf("prepare %s: %v", seed.personaID, err)
		}
		created := seed.personaID == localAgentDemoAssistantPersonaID
		if first.installationsCreated != 2 || first.published != created || first.evaluationRecorded != created || first.state != agentpersonastore.StatePublished {
			t.Fatalf("prepare receipt=%+v", first)
		}
		second, again, err := preparation.prepare(adminCtx, seed, policyProfile)
		if err != nil || again != direct || second.installationsCreated != 0 || second.installationsRefreshed != 0 || second.modelRouteCreated || second.providerDeploymentCreated || second.chatConversationRepaired || second.duplicateInstallationsGone != 0 || second.evaluationRecorded || second.published {
			t.Fatalf("prepare replay=%+v direct=%q err=%v", second, again, err)
		}
		rows, err := scoped.ListActiveInstallations(ctx, direct)
		if err != nil || len(rows) != 1 || rows[0].PersonaID != seed.personaID {
			t.Fatalf("direct installations=%+v %v", rows, err)
		}
	}
	rows, err := scoped.ListActiveInstallations(ctx, publicID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("general installations=%+v %v", rows, err)
	}
	if changed, err := ReconcileRestoredAgentInstallations(ctx, agents, personas, core, tenant, mapper(tenant), now); err != nil || changed != 0 {
		t.Fatalf("prepared reconciliation=%d %v", changed, err)
	}
	documentDB := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(documenthubstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, documentDB.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	documents, err := documenthubstore.New(ctx, documenthubstore.Config{DSN: personaChatSchemaDSN(t, documentDB.URL, documentDB.Schema), ChatDSN: "postgres://chat@127.0.0.1:1/chat", CoreDSN: "postgres://core@127.0.0.1:1/core"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(documents.Close)
	placed, err := ensureLocalAgentDemoHolidayDocument(ctx, documents, chat, localAgentDemoTenant, localAgentDemoAdmin, publicID, now)
	if err != nil || placed != 1 {
		t.Fatalf("holiday placed=%d err=%v", placed, err)
	}
	if placed, err := ensureLocalAgentDemoHolidayDocument(ctx, documents, chat, localAgentDemoTenant, localAgentDemoAdmin, publicID, now); err != nil || placed != 0 {
		t.Fatalf("holiday replay=%d %v", placed, err)
	}
	for _, reader := range []string{localAgentDemoAdmin, "ir-008-curtis-bell"} {
		hits, err := documents.SearchOfficialPlacementLexical(ctx, localAgentDemoTenant, publicID, "Juneteenth", "person", reader)
		if err != nil || len(hits) != 1 || hits[0].Title != localAgentDemoHolidayTitle {
			t.Fatalf("holiday reader %s hits=%+v %v", reader, hits, err)
		}
	}
	if hits, err := documents.SearchOfficialPlacementLexical(ctx, "harborcare-demo", publicID, "Juneteenth", "person", localAgentDemoAdmin); err != nil || len(hits) != 0 {
		t.Fatalf("foreign holiday hits=%+v err=%v", hits, err)
	}
	if placed, err := ensureLocalAgentDemoPolicyDocument(ctx, documents, chat, localAgentDemoTenant, localAgentDemoAdmin, publicID, []string{publicID}, now); err != nil || placed != 1 {
		t.Fatalf("policy placed=%d err=%v", placed, err)
	}
	agentUXGeneralChatMention(t, adminCtx, chatRuntime.service, lazyReferences, personas, core, documents, publicID, policyProfile, now)
	if workspaceSearch {
		agentUXSearchWorkspaceReaders(t, ctx, core, mapper, documents, publicID)
		agentUXSearchHolidayMention(t, adminCtx, chatRuntime.service, personas, core, documents, publicID, now, mapper)
	}

	deployment, err := LoadPersonaModelDeployment(runtime.DeploymentPath)
	if err != nil || len(deployment.Profiles) != 3 {
		t.Fatalf("deployment profiles=%d err=%v", len(deployment.Profiles), err)
	}
	if len(agenteval.AssistantSuite(personaPolicyHelperSkillID, personaChatReplySkillID).Cases) != 8 {
		t.Fatal("Assistant suite changed")
	}
}
