package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type agentUXSetupFailingVersions struct{ err error }

func (f agentUXSetupFailingVersions) ListPersonaCatalogVersions(context.Context, values.TenantId) ([]PersonaCatalogVersion, error) {
	return nil, f.err
}

type agentUXSetupFailingTargets struct{ err error }

func (f agentUXSetupFailingTargets) ListPersonaCatalogTargets(context.Context, trust.Principal, values.TenantId) ([]productui.PersonaAdminTarget, []productui.PersonaAdminTarget, error) {
	return nil, nil, f.err
}

type agentUXSetupDocuments struct{ err error }

func (f agentUXSetupDocuments) GetDocumentPreviews(context.Context, string, string, []string) ([]transportdocument.Preview, error) {
	return nil, f.err
}

type agentUXSetupDirectory struct{ missing map[string]bool }

func (f agentUXSetupDirectory) ResolvePersonaCatalogTarget(_ context.Context, _ values.TenantId, id string) (productui.PersonaAdminTarget, error) {
	if f.missing[id] {
		return productui.PersonaAdminTarget{}, errors.New("directory subject unavailable")
	}
	return productui.PersonaAdminTarget{ID: id, Label: "Member " + id}, nil
}

func TestTodo_AGENTUX_003_ServedSnapshotSurvivesAgentIdentityMember(t *testing.T) {
	ctx, principal := catalogContext(t)
	version := agentUXSetupVersion(t, agentpersona.StatePublished)
	public := chat.Conversation{ID: "general", TenantID: string(principal.Tenant()), Name: "General", Kind: chat.PublicChannel}
	direct := chat.Conversation{ID: "direct-policy-helper", TenantID: string(principal.Tenant()), Name: "Policy Helper", Kind: chat.Direct}
	targets := &ChatDirectoryPersonaCatalogTargets{
		Chat: personaCatalogChatFake{rooms: []chat.Conversation{public, direct}, members: []chat.Membership{
			{ConversationID: public.ID, TenantID: public.TenantID, HomeTenantID: public.TenantID, SubjectID: principal.Subject()},
			{ConversationID: direct.ID, TenantID: direct.TenantID, HomeTenantID: direct.TenantID, SubjectID: principal.Subject()},
			{ConversationID: direct.ID, TenantID: direct.TenantID, HomeTenantID: direct.TenantID, SubjectID: "policy-helper"},
		}},
		Directory: agentUXSetupDirectory{missing: map[string]bool{"policy-helper": true}},
	}
	grants := &catalogGrants{allowed: true}
	service := &PersonaAdminCatalogService{
		Versions: catalogVersions{version}, Installations: catalogInstalls{
			{ID: "placement-general", PersonaID: version.Profile.Profile.PersonaID, PersonaVersion: version.Profile.Profile.Version, ConversationID: public.ID, Active: true, ChannelClass: string(agentpersona.ChannelPublic), ConversationKind: string(agentpersona.ConversationChannel), MaxTier: agentskills.TierRead, AllowedDataClasses: []string{"PUBLIC", "INTERNAL"}},
			{ID: "placement-direct", PersonaID: version.Profile.Profile.PersonaID, PersonaVersion: version.Profile.Profile.Version, ConversationID: direct.ID, Active: true, ChannelClass: string(agentpersona.ChannelPrivate), ConversationKind: string(agentpersona.ConversationDirect), MaxTier: agentskills.TierRead, AllowedDataClasses: []string{"PUBLIC", "INTERNAL"}},
		}, Targets: targets, Skills: catalogSkills{}, Grants: grants, Authorizer: &catalogAuth{}, Starters: &catalogStarterSource{catalog: productui.PersonaAdminStarterCatalog{Available: true}}, Documents: agentUXSetupDocuments{},
	}
	snapshot, err := service.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{TenantID: string(principal.Tenant()), Principal: principal.Subject()})
	if err != nil || !snapshot.Available || len(snapshot.Personas) != 1 {
		t.Fatalf("served snapshot=%+v err=%v", snapshot, err)
	}
	if grants.calls != 1 || snapshot.Preview.Subject != principal.Subject() || snapshot.Preview.Conversation != public.ID || len(snapshot.Preview.EffectiveSkills) != 1 {
		t.Fatalf("snapshot lost the authorized current-member preview: grants=%d preview=%+v", grants.calls, snapshot.Preview)
	}
	if len(snapshot.SubjectOptions) != 1 || snapshot.SubjectOptions[0].ID != principal.Subject() || len(snapshot.Conversations) != 2 || len(snapshot.Personas[0].Installations) != 2 {
		t.Fatalf("agent identity was guessed or valid placements were lost: %+v", snapshot)
	}
	if snapshot.TargetsState.Omitted != 1 {
		t.Fatalf("unresolvable agent identity omission count=%d, want 1", snapshot.TargetsState.Omitted)
	}
	for _, placement := range snapshot.Personas[0].Installations {
		if placement.Conversation == "" || placement.Kind == "" {
			t.Fatalf("placement is missing conversation name or kind: %+v", placement)
		}
	}
	if !snapshot.DocumentServiceAvailable {
		t.Fatal("bound document reader was not projected as available")
	}
	preview, err := service.Preview(ctx, productui.PersonaAdminPreviewRequest{PersonaID: version.Profile.Profile.PersonaID, SubjectID: principal.Subject(), ConversationID: direct.ID})
	if err != nil || len(preview.EffectiveSkills) != 1 || preview.EffectiveSkills[0].DataClasses[0] != "WORKFORCE" {
		t.Fatalf("classification ceiling was compared with the skill domain: preview=%+v err=%v", preview, err)
	}
}

func TestTodo_AGENTUX_003_StagesDegradeIndependently(t *testing.T) {
	ctx, principal := catalogContext(t)
	version := agentUXSetupVersion(t, agentpersona.StateInReview)
	targets := catalogTargets{users: []productui.PersonaAdminTarget{{ID: principal.Subject(), Label: "Admin User"}}, conversations: []productui.PersonaAdminTarget{{ID: "general", Label: "General", Kind: "CHANNEL"}}}
	base := PersonaAdminCatalogService{Versions: catalogVersions{version}, Installations: catalogInstalls{}, Targets: targets, Skills: catalogSkills{}, Grants: &catalogGrants{allowed: true}, Authorizer: &catalogAuth{}, Starters: &catalogStarterSource{catalog: productui.PersonaAdminStarterCatalog{Available: true}}, Documents: agentUXSetupDocuments{}}

	catalogFailure := base
	catalogFailure.Versions = agentUXSetupFailingVersions{err: errors.New("catalog offline")}
	snapshot, err := catalogFailure.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{})
	if err != nil || !snapshot.Available || !snapshot.CatalogState.Unavailable || len(snapshot.SubjectOptions) != 1 {
		t.Fatalf("catalog failure took down other regions: %+v err=%v", snapshot, err)
	}

	targetFailure := base
	targetFailure.Targets = agentUXSetupFailingTargets{err: errors.New("directory offline")}
	snapshot, err = targetFailure.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{})
	if err != nil || !snapshot.Available || !snapshot.TargetsState.Unavailable || len(snapshot.Personas) != 1 {
		t.Fatalf("target failure took down catalog: %+v err=%v", snapshot, err)
	}

	documentFailure := base
	documentFailure.Documents = agentUXSetupDocuments{err: errors.New("documents offline")}
	documentVersion := version
	documentVersion.Profile.Profile.DocumentReferences = []agentdocref.Reference{{DocumentID: "doc-123e4567-e89b-42d3-a456-426614174000", VersionMode: agentdocref.ModePinned, PinnedVersion: 4, Label: "Leave policy"}}
	documentVersion.Profile, err = agentpersona.Seal(documentVersion.Profile.Profile)
	if err != nil {
		t.Fatal(err)
	}
	documentFailure.Versions = catalogVersions{documentVersion}
	snapshot, err = documentFailure.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{})
	if err != nil || !snapshot.Available || !snapshot.DocumentServiceAvailable || !snapshot.DocumentsState.Unavailable || len(snapshot.Personas) != 1 {
		t.Fatalf("bound document service availability was lost: %+v err=%v", snapshot, err)
	}
}

func agentUXSetupVersion(t *testing.T, lifecycle agentpersona.LifecycleState) PersonaCatalogVersion {
	t.Helper()
	sealed, err := agentpersona.Seal(agentpersona.PersonaProfile{
		Manifest: agentpersona.AgentManifestRef{ID: "agent", Version: 1, Digest: "manifest", SchemaVersion: 1}, PersonaID: "policy-helper-persona", Version: 4,
		Handle: "policy-helper", DisplayName: "Policy Helper", AvatarRef: "avatar", Purpose: "Answer approved policy questions", Owner: "owner", Steward: "steward",
		Audience: agentpersona.Audience{Roles: []string{"member"}, Populations: []string{"employees"}, OrganizationScopes: []string{"org:catalog"}}, SkillPins: []agentskills.SkillPin{{ID: "skill.read", Version: 1, Digest: "skill-digest"}}, TierCeiling: agentskills.TierRead,
		ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect, agentpersona.ConversationChannel}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate, agentpersona.ChannelPublic},
		Instructions: "Use approved sources.", EvalSuiteRef: "evaluation-suite", EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1},
		DataClassesRead: []string{"PUBLIC", "INTERNAL"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return PersonaCatalogVersion{Profile: sealed, Lifecycle: lifecycle, Owner: "owner", Steward: "steward", ReviewRequired: true, ReviewApproved: true, Reviewer: "reviewer"}
}
