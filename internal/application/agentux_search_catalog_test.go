package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestAgentUXSearch_SetupProjection_Integration(t *testing.T) {
	ctx, s, call, _ := agentUXSearchFixture(t)
	agentUXSearchDocument(t, ctx, s, call.TenantID.String(), "Leave guide", true)
	r := WorkspacePersonaCatalogVersions{Base: catalogVersions{{Profile: agentpersona.PersonaVersion{Profile: agentpersona.PersonaProfile{PersonaID: localAgentDemoAssistantPersonaID}}}, {Profile: agentpersona.PersonaVersion{Profile: agentpersona.PersonaProfile{PersonaID: localAgentDemoPersonaID}}, WorkspaceDocuments: 17}}, Search: s}
	rows, err := r.ListPersonaCatalogVersions(ctx, call.TenantID)
	if err != nil || rows[0].WorkspaceDocuments != 1 || rows[0].WorkspacePending != 0 || rows[1].WorkspaceDocuments != 17 {
		t.Fatalf("setup projection: %+v %v", rows, err)
	}
	s.embedder = nil
	rows, err = r.ListPersonaCatalogVersions(ctx, call.TenantID)
	if err != nil || rows[0].WorkspaceDocuments != 1 || rows[0].WorkspacePending != 1 || !rows[0].WorkspaceIndexedAt.IsZero() {
		t.Fatalf("absent index disguised: %+v %v", rows, err)
	}
	if _, err := (WorkspacePersonaCatalogVersions{}).ListPersonaCatalogVersions(context.Background(), values.TenantId("missing")); err == nil {
		t.Fatal("missing source allowed")
	}
}
