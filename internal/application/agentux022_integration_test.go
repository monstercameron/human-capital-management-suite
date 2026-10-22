package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type agentUX022FailingTargets struct{ err error }

func (f agentUX022FailingTargets) ListPersonaCatalogTargets(context.Context, trust.Principal, values.TenantId) ([]productui.PersonaAdminTarget, []productui.PersonaAdminTarget, error) {
	return nil, nil, f.err
}

type agentUX022DocumentReader struct {
	previews []transportdocument.Preview
	err      error
}

func (f agentUX022DocumentReader) GetDocumentPreviews(context.Context, string, string, []string) ([]transportdocument.Preview, error) {
	return f.previews, f.err
}

// TestTodo_AGENTUX_022_Integration composes the staged snapshot builder over
// a real persona store: the catalog reads durable versions while the chat
// target stage fails, and the document reader availability reflects exactly
// the composed runtime.
func TestTodo_AGENTUX_022_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenant, tenantID := values.TenantId("tenant-a"), uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenantID)
	mapper := func(value values.TenantId) uuid.UUID {
		if value == tenant {
			return tenantID
		}
		return uuid.Nil
	}
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatal(err)
	}
	root, err := agentpersonastore.New(conn, mapper)
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := root.Scoped(tenant)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	sealed, err := agentpersona.Seal(agentpersona.PersonaProfile{Manifest: agentpersona.AgentManifestRef{ID: "agent", Version: 1, Digest: "manifest", SchemaVersion: 1}, PersonaID: "persona-a", Version: 1, Handle: "persona", DisplayName: "Persona", AvatarRef: "avatar", Purpose: "Help safely", Audience: agentpersona.Audience{Roles: []string{"member"}, Populations: []string{"staff"}, OrganizationScopes: []string{"org-a"}}, SkillPins: []agentskills.SkillPin{{ID: "skill.read", Version: 1, Digest: "skill-digest"}}, TierCeiling: agentskills.TierRead, ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate}, Instructions: "Answer only with approved records.", Owner: "owner", Steward: "steward", EvalSuiteRef: "suite", EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(sealed.Profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := scoped.CreateDraft(ctx, agentpersonastore.PersonaVersion{TenantID: tenant, PersonaID: "persona-a", Version: 1, AgentVersion: "agent@1", Handle: "persona", DisplayName: "Persona", Profile: raw, ContentDigest: sealed.Digest, CreatedAt: now}, agentpersonastore.PersonaOwner{TenantID: tenant, PersonaID: "persona-a", Role: agentpersonastore.BusinessOwner, PrincipalID: "owner"}, agentpersonastore.PersonaOwner{TenantID: tenant, PersonaID: "persona-a", Role: agentpersonastore.TechnicalSteward, PrincipalID: "steward"}, "owner", now); err != nil {
		t.Fatal(err)
	}

	auth := &catalogAuth{}
	svc := &PersonaAdminCatalogService{
		Versions:      personaAdminCatalogVersions{store: personaAdminCatalogStoreAdapter{store: root}},
		Installations: catalogInstalls{},
		Targets:       agentUX022FailingTargets{err: errors.New("chat unavailable")},
		Skills:        catalogSkills{},
		Grants:        &catalogGrants{allowed: true},
		Authorizer:    auth,
	}
	snapshot, err := svc.Snapshot(trust.WithPrincipal(ctx, agentUX022Principal(t)), productui.PersonaAdminSnapshotRequest{TenantID: "tenant-a", Principal: "user-a"})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Available || len(snapshot.Personas) != 1 || snapshot.Personas[0].Name != "Persona" {
		t.Fatalf("durable catalog did not render: %+v", snapshot)
	}
	if snapshot.CatalogState.Unavailable {
		t.Fatal("the failed targets stage took the catalog with it")
	}
	if !snapshot.TargetsState.Unavailable {
		t.Fatal("the failed targets stage is not reported")
	}
	if snapshot.DocumentServiceAvailable || !snapshot.DocumentsState.Unavailable {
		t.Fatalf("uncomposed document reader reported available: %+v", snapshot.DocumentsState)
	}

	svc.Documents = agentUX022DocumentReader{}
	withDocuments, err := svc.Snapshot(trust.WithPrincipal(ctx, agentUX022Principal(t)), productui.PersonaAdminSnapshotRequest{TenantID: "tenant-a", Principal: "user-a"})
	if err != nil {
		t.Fatal(err)
	}
	if !withDocuments.DocumentServiceAvailable || withDocuments.DocumentsState.Unavailable {
		t.Fatalf("composed document reader reported unavailable: %+v", withDocuments.DocumentsState)
	}
	if len(withDocuments.Personas) != 1 {
		t.Fatalf("composing documents changed the catalog: %+v", withDocuments.Personas)
	}
}

func agentUX022Principal(t *testing.T) *trust.Principal {
	t.Helper()
	_, p := catalogContext(t)
	return p
}
