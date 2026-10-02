package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type agentUXSetup3PolicyAuthority struct {
	agentinvoke.AuthorityResolver
	snapshot chatstore.PersonaChannelPolicySnapshot
}

func (a *agentUXSetup3PolicyAuthority) CapturePersonaChannelPolicy(context.Context, string, string, string) (chatstore.PersonaChannelPolicySnapshot, error) {
	return a.snapshot, nil
}

type agentUXSetup3PolicyAudience struct {
	authorityAudienceFake
	snapshot chatstore.PersonaChannelPolicySnapshot
}

func (a *agentUXSetup3PolicyAudience) CapturePersonaChannelPolicy(context.Context, string, string, string) (chatstore.PersonaChannelPolicySnapshot, error) {
	return a.snapshot, nil
}

type agentUXSetup3ScopeRecorder struct {
	installation agentpersonastore.ActiveInstallation
	policy       agentpersonastore.ChannelPolicy
}

func (r *agentUXSetup3ScopeRecorder) ResolvePersonaScopes(_ context.Context, _ values.TenantId, _ agentpersona.PersonaProfile, installation agentpersonastore.ActiveInstallation, policy agentpersonastore.ChannelPolicy) (agentinvoke.SkillScopes, agentinvoke.SkillScopes, agentinvoke.SkillScopes, error) {
	r.installation, r.policy = installation, policy
	scopes := agentinvoke.SkillScopes{"read": {"worker"}}
	return scopes, scopes.Clone(), scopes.Clone(), nil
}

func agentUXSetup3PolicySnapshot(search, private bool) chatstore.PersonaChannelPolicySnapshot {
	return chatstore.PersonaChannelPolicySnapshot{
		TenantID: "tenant-a", ConversationID: "room-a", PolicyRevision: 2,
		Policy: chatstore.PersonaChannelPolicy{
			PlacementClass: "ONE_TO_ONE_DM", MaxTier: "T0", AllowedDataClasses: []string{"PUBLIC"},
			AlwaysPrivate: private, ConversationSearchAllowed: search,
			AllowedChannelClasses: []string{"ONE_TO_ONE"},
		},
	}
}

func TestAgentUXSetup3_EffectiveChannelPolicyNeverWidensInstallation(t *testing.T) {
	accepted := agentpersonastore.ChannelPolicy{
		PlacementClass: "ONE_TO_ONE_DM", MaxTier: "T0", AllowedDataClasses: []string{"PUBLIC"},
		AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationOneToOne},
	}
	current := accepted
	current.MaxTier = "T3"
	current.AllowedDataClasses = []string{"PUBLIC", "INTERNAL"}
	current.ConversationSearchAllowed = true
	current.AllowExternalMembers = true
	got := effectivePersonaChannelPolicy(accepted, current)
	if !agentPersonaPolicyEqual(got, accepted) {
		t.Fatalf("wider current policy changed installation ceiling: %+v", got)
	}
}

func TestAgentUXSetup3_NarrowedSearchRefusesNextLiveSearch(t *testing.T) {
	ctx, tools, _, personas, _, _, record, run := runtimeToolFixture(t)
	ctx = WithPersonaBackgroundAdmission(ctx, record)
	identity, err := personaRunT0ToolInvocation(record, run)
	if err != nil {
		t.Fatal(err)
	}
	personas.reader.install.ChannelPolicy = agentpersonastore.ChannelPolicy{
		PlacementClass: "ONE_TO_ONE_DM", MaxTier: "T0", AllowedDataClasses: []string{"PUBLIC"},
		ConversationSearchAllowed: true, AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationOneToOne},
	}
	source := &agentUXSetup3PolicyAuthority{AuthorityResolver: tools.cfg.Policy.authority, snapshot: agentUXSetup3PolicySnapshot(true, false)}
	tools.cfg.Policy.authority = source
	if scope, err := tools.cfg.Policy.ResolvePersonaDocumentSearchScope(ctx, identity); err != nil || scope.ScopeID != identity.ConversationID {
		t.Fatalf("unchanged live policy denied scope=%+v err=%v", scope, err)
	}
	source.snapshot.Policy.ConversationSearchAllowed = false
	if _, err := tools.cfg.Policy.ResolvePersonaDocumentSearchScope(ctx, identity); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("narrowed live policy allowed document search: %v", err)
	}
}

func TestAgentUXSetup3_AuthorityUsesCurrentPolicyIntersection(t *testing.T) {
	version, _ := authorityProfile(t)
	accepted := agentpersonastore.ChannelPolicy{
		PlacementClass: "ONE_TO_ONE_DM", MaxTier: "T2", AllowedDataClasses: []string{"PUBLIC", "INTERNAL"},
		ConversationSearchAllowed: true, AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationOneToOne},
	}
	audience := &agentUXSetup3PolicyAudience{
		authorityAudienceFake: authorityAudienceFake{
			conversations: []PersonaAudienceConversation{{TenantID: "tenant-a", ConversationID: "room-a", Members: []PersonaAudienceMember{{SubjectID: "user-a", Roles: []string{"manager"}, Populations: []string{"staff"}, OrganizationScope: "org-a"}}}},
			installations: []PersonaAudienceInstallation{{Tuple: agentpersonastore.AvailableInstallation{PersonaID: "persona-a", PersonaVersion: 2, InstallationID: "install-a", ConversationID: "room-a"}, Active: true, CurrentVersion: true}},
		},
		snapshot: agentUXSetup3PolicySnapshot(false, true),
	}
	recorder := &agentUXSetup3ScopeRecorder{}
	source := &DatabasePersonaAuthoritySource{
		Installations: authorityInstallStoreFake{reader: authorityInstallReaderFake{version: version, current: agentpersonastore.ActiveInstallation{InstallationID: "install-a", PersonaID: "persona-a", PersonaVersion: 2, ConversationID: "room-a", ChannelPolicy: accepted}}},
		Audience:      audience, Scopes: recorder,
	}
	if _, err := source.ResolvePersonaAuthority(authorityContext(t), agentinvoke.AdmissionRequest{TenantID: "tenant-a", ConversationID: "room-a", InvokerID: "user-a", PersonaID: "persona-a"}, TrustedInvoker{ID: "user-a", TenantID: "tenant-a"}); err != nil {
		t.Fatal(err)
	}
	if recorder.policy.MaxTier != "T0" || recorder.policy.ConversationSearchAllowed || !recorder.policy.AlwaysPrivate || len(recorder.policy.AllowedDataClasses) != 1 || !agentPersonaPolicyEqual(recorder.installation.ChannelPolicy, recorder.policy) {
		t.Fatalf("authority did not use current intersection: policy=%+v installation=%+v", recorder.policy, recorder.installation.ChannelPolicy)
	}
	if _, err := personaRuntimePublicProfile(version, recorder.installation, agentUXSetup3OutputIdentity(version)); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("current always-private policy still allowed a public answer: %v", err)
	}
}

func agentUXSetup3OutputIdentity(version agentpersonastore.PersonaVersion) agentsecurity.FinalOutputIdentity {
	return agentsecurity.FinalOutputIdentity{TenantID: version.TenantID.String(), PersonaID: version.PersonaID, PersonaVersion: "2", InstallationID: "install-a", ConversationID: "room-a"}
}

func TestAgentUXSetup3_CurrentPolicyReaderUsesChatStore(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, db)
	store, err := chatstore.New(ctx, chatstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	adapter := chatstore.NewAdapter(store)
	if _, err := adapter.CreateConversation(ctx, chat.Conversation{ID: "room-a", TenantID: "tenant-a", Kind: chat.Direct, Name: "Helper", OwnerID: "user-a"}, []chat.Membership{
		{ConversationID: "room-a", TenantID: "tenant-a", HomeTenantID: "tenant-a", SubjectID: "user-a", Role: chat.Manager, HistoryVisibility: chat.FullHistory},
		{ConversationID: "room-a", TenantID: "tenant-a", HomeTenantID: "tenant-a", SubjectID: "policy-helper", Role: chat.Member, HistoryVisibility: chat.FullHistory},
	}, "setup3-room"); err != nil {
		t.Fatal(err)
	}
	policy := agentUXSetup3PolicySnapshot(false, true).Policy
	if _, err := store.PutPersonaChannelPolicy(ctx, "tenant-a", "room-a", 0, policy); err != nil {
		t.Fatal(err)
	}
	accepted := agentpersonastore.ChannelPolicy{PlacementClass: "ONE_TO_ONE_DM", MaxTier: "T2", AllowedDataClasses: []string{"PUBLIC", "INTERNAL"}, ConversationSearchAllowed: true, AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationOneToOne}}
	got, supported, err := currentPersonaChannelPolicy(ctx, store, "tenant-a", "room-a", accepted)
	if err != nil || !supported || got.ConversationSearchAllowed || !got.AlwaysPrivate || got.MaxTier != "T0" || len(got.AllowedDataClasses) != 1 {
		t.Fatalf("stored current policy intersection=%+v supported=%t err=%v", got, supported, err)
	}
}
