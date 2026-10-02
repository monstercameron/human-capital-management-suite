package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type agentUXSetup3CatalogTargets struct {
	users, conversations []productui.PersonaAdminTarget
	names                map[string]string
}

func (t agentUXSetup3CatalogTargets) ListPersonaCatalogTargets(context.Context, trust.Principal, values.TenantId) ([]productui.PersonaAdminTarget, []productui.PersonaAdminTarget, error) {
	return t.users, t.conversations, nil
}

func (t agentUXSetup3CatalogTargets) ResolvePersonaCatalogTarget(_ context.Context, _ values.TenantId, id string) (productui.PersonaAdminTarget, error) {
	if name := t.names[id]; name != "" {
		return productui.PersonaAdminTarget{ID: id, Label: name}, nil
	}
	return productui.PersonaAdminTarget{}, errors.New("not found")
}

func (agentUXSetup3CatalogTargets) ResolvePersonaCatalogRoleTargets(context.Context, trust.Principal, values.TenantId, []string, []string) []productui.PersonaAdminTarget {
	return []productui.PersonaAdminTarget{{ID: "employees", Label: "Everyone in the workspace"}}
}

type agentUXSetup3CatalogDocuments struct {
	rows map[string][]PersonaAdminPlacementDocument
}

func (agentUXSetup3CatalogDocuments) GetDocumentPreviews(context.Context, string, string, []string) ([]transportdocument.Preview, error) {
	return nil, nil
}

func (d agentUXSetup3CatalogDocuments) ListPersonaAdminPlacementDocuments(_ context.Context, _, _, conversation string) ([]PersonaAdminPlacementDocument, error) {
	return append([]PersonaAdminPlacementDocument(nil), d.rows[conversation]...), nil
}

type agentUXSetup3CatalogSkills struct{}

func (agentUXSetup3CatalogSkills) ResolvePin(pin agentskills.SkillPin) (agentskills.SkillRecord, error) {
	var definition agentskills.SkillDefinition
	switch pin.ID {
	case "hcmnext.skill.knowledge_search_with_citations":
		definition = personaPolicySearchSkillDefinition()
	case "persona.chat_reply":
		definition = personaChatReplySkillDefinition()
	default:
		return agentskills.SkillRecord{}, errors.New("unknown pin")
	}
	return agentskills.SkillRecord{Definition: definition, Digest: pin.Digest, Status: agentskills.StatusActive}, nil
}

type agentUXSetup3CatalogGrants struct{ deniedSubject string }

func (g agentUXSetup3CatalogGrants) ResolvePersonaSkillGrant(_ context.Context, _ trust.Principal, _ values.TenantId, subject, _ string, pin agentskills.SkillPin) (PersonaCatalogGrant, error) {
	if subject == g.deniedSubject {
		return PersonaCatalogGrant{Reason: "This person is outside the agent audience."}, nil
	}
	dataClasses := []string(nil)
	if pin.ID == personaPolicyHelperSkillID {
		dataClasses = []string{personaPolicySearchDataClass}
	}
	return PersonaCatalogGrant{Allowed: true, Tier: agentskills.TierRead.String(), DataClasses: dataClasses}, nil
}

func agentUXSetup3CatalogService(t *testing.T) (*PersonaAdminCatalogService, context.Context) {
	t.Helper()
	ctx, _ := catalogContext(t)
	profile, err := agentpersona.Seal(agentpersona.PersonaProfile{
		Manifest:  agentpersona.AgentManifestRef{ID: "agent.policy-helper", Version: 1, Digest: "manifest", SchemaVersion: 1},
		PersonaID: "policy-helper", Version: 4, Handle: "policy-helper", DisplayName: "Policy Helper", AvatarRef: "avatar", Purpose: "Answer policy questions.", Instructions: "Cite official policy documents.", Owner: "ir-001-walt-brennan", Steward: "ir-003-loretta-haynes", EvalSuiteRef: "policy-eval",
		Audience:    agentpersona.Audience{Roles: []string{"employees"}, Populations: []string{"staff"}, OrganizationScopes: []string{"org-a"}},
		SkillPins:   []agentskills.SkillPin{{ID: "hcmnext.skill.knowledge_search_with_citations", Version: 1, Digest: "search-digest"}, {ID: "persona.chat_reply", Version: 1, Digest: "reply-digest"}},
		TierCeiling: agentskills.TierRead, ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationChannel, agentpersona.ConversationDirect}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPublic, agentpersona.ChannelPrivate},
		DataClassesRead: []string{"POLICY_DOCUMENT"}, EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 2, MaxLatencyMS: 1000},
	})
	if err != nil {
		t.Fatal(err)
	}
	targets := agentUXSetup3CatalogTargets{
		users:         []productui.PersonaAdminTarget{{ID: "user-a", Label: "Walt Brennan"}, {ID: "outside", Label: "Riley Outside"}},
		conversations: []productui.PersonaAdminTarget{{ID: "general", Label: "general", Kind: "CHANNEL"}, {ID: "direct-policy", Label: "Policy Helper", PlacementLabel: "Walt Brennan", Kind: "DIRECT_MESSAGE", ViewerDirect: true}},
		names:         map[string]string{"ir-001-walt-brennan": "Walt Brennan", "ir-003-loretta-haynes": "Loretta Haynes", "ir-008-curtis-bell": "Curtis Bell"},
	}
	installations := catalogInstalls{
		{ID: "install-general", PersonaID: "policy-helper", PersonaVersion: 4, ConversationID: "general", Active: true, ChannelClass: string(agentpersona.ChannelPublic), ConversationKind: string(agentpersona.ConversationChannel), MaxTier: agentskills.TierRead, AllowedDataClasses: []string{"POLICY_DOCUMENT"}, Audience: "employees", ReplyPlacement: "public channel"},
		{ID: "install-direct", PersonaID: "policy-helper", PersonaVersion: 4, ConversationID: "direct-policy", Active: true, ChannelClass: string(agentpersona.ChannelPrivate), ConversationKind: string(agentpersona.ConversationDirect), MaxTier: agentskills.TierRead, AllowedDataClasses: []string{"POLICY_DOCUMENT"}, Audience: "employees", ReplyPlacement: "direct conversation"},
	}
	documents := agentUXSetup3CatalogDocuments{rows: map[string][]PersonaAdminPlacementDocument{
		"general":       {{DocumentID: "pto", VersionID: "v1", Title: "Paid time off policy"}},
		"direct-policy": {{DocumentID: "pto", VersionID: "v1", Title: "Paid time off policy"}},
	}}
	return &PersonaAdminCatalogService{
		Versions:      catalogVersions{{Profile: profile, Lifecycle: agentpersona.StatePublished, Owner: "ir-001-walt-brennan", Steward: "ir-003-loretta-haynes", ReviewApproved: true, Reviewer: "ir-008-curtis-bell", EvaluationRef: "eval-4", ReviewApprovedAt: "2026-09-29", EvaluationPassedAt: "2026-09-30"}},
		Installations: installations, Targets: targets, Skills: agentUXSetup3CatalogSkills{}, Grants: agentUXSetup3CatalogGrants{deniedSubject: "outside"}, Authorizer: &catalogAuth{}, Documents: documents,
	}, ctx
}

func TestAgentUXSetup3_F2CatalogProjectsPlacementDocumentsAndEvidence(t *testing.T) {
	service, ctx := agentUXSetup3CatalogService(t)
	snapshot, err := service.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{TenantID: "tenant-a", Principal: "user-a"})
	if err != nil || len(snapshot.Personas) != 1 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	persona := snapshot.Personas[0]
	if persona.Reviewer != "ir-008-curtis-bell" || persona.ReviewApprovedAt != "2026-09-29" || persona.EvaluationPassedAt != "2026-09-30" || len(persona.Installations) != 2 {
		t.Fatalf("publication/placement projection=%+v", persona)
	}
	for _, installation := range persona.Installations {
		if installation.InstallationID == "" || installation.OfficialDocumentCount == nil || *installation.OfficialDocumentCount != 1 || len(installation.OfficialDocumentTitles) != 1 || installation.OfficialDocumentTitles[0] != "Paid time off policy" {
			t.Fatalf("official placement projection=%+v", installation)
		}
	}
}

func TestAgentUXSetup3_F4CatalogUsesDirectoryNamesAndInitials(t *testing.T) {
	service, ctx := agentUXSetup3CatalogService(t)
	snapshot, err := service.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{})
	if err != nil {
		t.Fatal(err)
	}
	persona := snapshot.Personas[0]
	if persona.OwnerName != "Walt Brennan" || persona.OwnerInitials != "WB" || persona.StewardName != "Loretta Haynes" || persona.StewardInitials != "LH" || persona.ReviewerName != "Curtis Bell" || persona.ReviewerInitials != "CB" {
		t.Fatalf("directory identities=%+v", persona)
	}
}

func TestAgentUXSetup3_F7PreviewReturnsTruthfulAllowedAndRefusedResults(t *testing.T) {
	service, ctx := agentUXSetup3CatalogService(t)
	allowed, err := service.Preview(ctx, productui.PersonaAdminPreviewRequest{PersonaID: "policy-helper", SubjectID: "user-a", ConversationID: "general"})
	if err != nil || len(allowed.EffectiveSkills) != 2 || allowed.ReplyPlacement != "public channel" || allowed.OfficialDocumentCount == nil || *allowed.OfficialDocumentCount != 1 || len(allowed.OfficialDocumentTitles) != 1 {
		t.Fatalf("allowed preview=%+v err=%v", allowed, err)
	}
	denied, err := service.Preview(ctx, productui.PersonaAdminPreviewRequest{PersonaID: "policy-helper", SubjectID: "outside", ConversationID: "general"})
	if err != nil || len(denied.EffectiveSkills) != 0 || denied.ReplyPlacement != "" || len(denied.Warnings) != 2 {
		t.Fatalf("denied preview=%+v err=%v", denied, err)
	}
}
