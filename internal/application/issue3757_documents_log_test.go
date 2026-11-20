package application

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
)

type issue3757FailingDocuments struct{}

func (issue3757FailingDocuments) GetDocumentPreviews(context.Context, string, string, []string) ([]transportdocument.Preview, error) {
	return nil, errors.New("document store offline")
}

func issue3757VersionWithDocument(t *testing.T) PersonaCatalogVersion {
	t.Helper()
	sealed, err := agentpersona.Seal(agentpersona.PersonaProfile{
		Manifest: agentpersona.AgentManifestRef{ID: "agent", Version: 1, Digest: "manifest", SchemaVersion: 1}, PersonaID: "policy-helper-persona", Version: 4,
		Handle: "policy-helper", DisplayName: "Policy Helper", AvatarRef: "avatar", Purpose: "Answer approved policy questions", Owner: "owner", Steward: "steward",
		Audience: agentpersona.Audience{Roles: []string{"member"}, Populations: []string{"employees"}, OrganizationScopes: []string{"org:catalog"}}, SkillPins: []agentskills.SkillPin{{ID: "skill.read", Version: 1, Digest: "skill-digest"}}, TierCeiling: agentskills.TierRead,
		ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect, agentpersona.ConversationChannel}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate, agentpersona.ChannelPublic},
		Instructions: "Use approved sources.", EvalSuiteRef: "evaluation-suite", EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1},
		DataClassesRead:    []string{"PUBLIC", "INTERNAL"},
		DocumentReferences: []agentdocref.Reference{{DocumentID: "doc-secret-handbook", VersionMode: agentdocref.ModePinned, PinnedVersion: 2, Label: "Secret handbook title"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return PersonaCatalogVersion{Profile: sealed, Lifecycle: agentpersona.StateInReview, Owner: "owner", Steward: "steward", ReviewRequired: true, ReviewApproved: true, Reviewer: "reviewer"}
}

// TestIssue3757_RealDocumentFailureIsLogged: a genuine document load failure is
// reported to the page and written to the log, without document titles or ids.
func TestIssue3757_RealDocumentFailureIsLogged(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	ctx, principal := catalogContext(t)
	targets := catalogTargets{users: []productui.PersonaAdminTarget{{ID: principal.Subject(), Label: "Admin User"}}, conversations: []productui.PersonaAdminTarget{{ID: "general", Label: "General", Kind: "CHANNEL"}}}
	service := PersonaAdminCatalogService{Versions: catalogVersions{issue3757VersionWithDocument(t)}, Installations: catalogInstalls{}, Targets: targets, Skills: catalogSkills{}, Grants: &catalogGrants{allowed: true}, Authorizer: &catalogAuth{}, Starters: &catalogStarterSource{catalog: productui.PersonaAdminStarterCatalog{Available: true}}, Documents: issue3757FailingDocuments{}}
	snapshot, err := service.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.DocumentsState.Unavailable {
		t.Fatalf("a failing document reader must show the load failure: %+v", snapshot.DocumentsState)
	}
	logged := buf.String()
	if !strings.Contains(logged, "document store offline") || !strings.Contains(logged, "persona admin catalog") {
		t.Fatalf("a real document load failure was not logged: %q", logged)
	}
	for _, leak := range []string{"doc-secret-handbook", "Secret handbook title"} {
		if strings.Contains(logged, leak) {
			t.Fatalf("log leaks document content %q: %q", leak, logged)
		}
	}

	buf.Reset()
	empty := service
	empty.Versions = catalogVersions{}
	if _, err := empty.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("an empty catalog logged a failure: %q", buf.String())
	}
}
