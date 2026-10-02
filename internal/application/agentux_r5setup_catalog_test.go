package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
)

type agentUXR5SetupDocuments struct {
	byActor map[string][]PersonaAdminPlacementDocument
}

func TestAgentUXR5Setup_CatalogProjectsEvaluationCaseCount(t *testing.T) {
	service, ctx := agentUXSetup3CatalogService(t)
	versions := service.Versions.(catalogVersions)
	starter, ok := agenttemplate.PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
	if !ok {
		t.Fatal("policy helper starter is missing")
	}
	profile := versions[0].Profile.Profile
	profile.EvalSuiteRef = starter.EvaluationSuite
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	versions[0].Profile = sealed
	service.Versions = versions
	snapshot, err := service.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{TenantID: "tenant-a", Principal: "user-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Personas) != 1 || snapshot.Personas[0].EvaluationCaseCount != 8 {
		t.Fatalf("evaluation case projection = %+v", snapshot.Personas)
	}
}

func (agentUXR5SetupDocuments) GetDocumentPreviews(context.Context, string, string, []string) ([]transportdocument.Preview, error) {
	return nil, nil
}

func (d agentUXR5SetupDocuments) ListPersonaAdminPlacementDocuments(_ context.Context, _, actor, _ string) ([]PersonaAdminPlacementDocument, error) {
	return append([]PersonaAdminPlacementDocument(nil), d.byActor[actor]...), nil
}

func TestAgentUXR5Setup_CatalogProjectsUnreadableDocumentCount(t *testing.T) {
	service, ctx := agentUXSetup3CatalogService(t)
	service.Targets = agentUXSetup3CatalogTargets{
		users:         []productui.PersonaAdminTarget{{ID: "user-a", Label: "Walt Brennan"}, {ID: "user-b", Label: "Robin Patel"}},
		conversations: []productui.PersonaAdminTarget{{ID: "general", Label: "general", Kind: "CHANNEL"}},
		names:         map[string]string{"ir-001-walt-brennan": "Walt Brennan", "ir-003-loretta-haynes": "Loretta Haynes", "ir-008-curtis-bell": "Curtis Bell"},
	}
	service.Documents = agentUXR5SetupDocuments{byActor: map[string][]PersonaAdminPlacementDocument{
		"user-a": {{DocumentID: "visible", Title: "Visible policy"}, {DocumentID: "restricted", Title: "Restricted policy"}},
		"user-b": {{DocumentID: "visible", Title: "Visible policy"}},
	}}
	preview, err := service.Preview(ctx, productui.PersonaAdminPreviewRequest{PersonaID: "policy-helper", SubjectID: "user-b", ConversationID: "general"})
	if err != nil {
		t.Fatal(err)
	}
	if preview.UnreadableDocumentCount != 1 || preview.OfficialDocumentCount == nil || *preview.OfficialDocumentCount != 1 || len(preview.OfficialDocumentTitles) != 1 || preview.OfficialDocumentTitles[0] != "Visible policy" {
		t.Fatalf("subject-scoped document projection = %+v", preview)
	}
}
