package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/semver"
)

// Announcement documents resolve exact pins and caret lines against
// sealed labels and report the resolved label with the content
// (AGENTUX-065).
func TestAgentUX065_AnnouncementResolvesLabelModes(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	const tenant = "announcement-labels"
	doc, err := svc.store.CreateDocument(ctx, tenant, "owner", "COMPANY")
	if err != nil {
		t.Fatal(err)
	}
	submit := func(title, markdown, base string) documenthubstore.Version {
		t.Helper()
		version, err := svc.store.SubmitCandidate(ctx, tenant, documenthubstore.Version{DocumentID: doc, CreatorID: "owner", Title: title, Markdown: markdown, Classification: "INTERNAL"}, base)
		if err != nil {
			t.Fatal(err)
		}
		return version
	}
	deploy := func(version documenthubstore.Version, scope, expected string, bump semver.Bump) documenthubstore.Deployment {
		t.Helper()
		if _, err := svc.store.RecordReview(ctx, tenant, documenthubstore.ReviewInput{DocumentID: doc, VersionID: version.ID, ScopeKind: "placement", ScopeID: scope, ReviewerID: "reviewer", Authority: "policy-owner", Decision: "approved"}); err != nil {
			t.Fatal(err)
		}
		deployment, err := svc.store.Deploy(ctx, tenant, documenthubstore.DeployInput{DocumentID: doc, VersionID: version.ID, ScopeKind: "placement", ScopeID: scope, DeployerID: "owner", ExpectedLive: expected, Bump: bump})
		if err != nil {
			t.Fatal(err)
		}
		return deployment
	}
	v1 := submit("Guide", "First edition.", "")
	v2 := submit("Guide", "Second edition, corrected.", v1.ID)
	first := deploy(v1, "general", "", semver.BumpPatch)
	deploy(v2, "general", first.VersionID, semver.BumpMinor)
	if err := svc.store.SetAnnouncementDocuments(ctx, tenant, "owner", "workload:policy", "ann", []string{doc}, "general"); err != nil {
		t.Fatal(err)
	}
	principal := &proactiveServicePrincipal{}
	resolver := AgentAnnouncementReferenceResolver{Hub: svc.store, Principals: principal}
	resolve := func(ref agentdocref.Reference) string {
		t.Helper()
		ctx := context.WithValue(ctx, announcementDraftKey{}, agentstore.Announcement{ID: "ann", TenantKey: tenant, InstallationID: "install", Documents: []agentdocref.Reference{ref}})
		resolved, err := resolver.ResolveAnnouncementDocuments(ctx, tenant, "install", []agentdocref.Reference{ref})
		if err != nil || len(resolved) != 1 {
			t.Fatalf("label ref %+v: %+v %v", ref, resolved, err)
		}
		return resolved[0].VersionLabel + "\x00" + resolved[0].Content
	}
	if got := resolve(agentdocref.Reference{DocumentID: doc, VersionMode: agentdocref.ModePinnedLabel, PinnedLabel: "1.0.0", Label: "Exact"}); got != "1.0.0\x00First edition." {
		t.Fatalf("exact pin resolved %q; want 1.0.0 with first edition", got)
	}
	if got := resolve(agentdocref.Reference{DocumentID: doc, VersionMode: agentdocref.ModeCompatible, CompatibleBase: "1.0.0", Label: "Line"}); got != "1.1.0\x00Second edition, corrected." {
		t.Fatalf("caret line resolved %q; want 1.1.0 with second edition", got)
	}
	future := agentdocref.Reference{DocumentID: doc, VersionMode: agentdocref.ModeCompatible, CompatibleBase: "2.0.0", Label: "Future line"}
	futureCtx := context.WithValue(ctx, announcementDraftKey{}, agentstore.Announcement{ID: "ann", TenantKey: tenant, InstallationID: "install", Documents: []agentdocref.Reference{future}})
	if _, err := resolver.ResolveAnnouncementDocuments(futureCtx, tenant, "install", []agentdocref.Reference{future}); !errors.Is(err, ErrAgentAnnouncementDenied) {
		t.Fatalf("caret past every publication err=%v; want refusal", err)
	}
}
