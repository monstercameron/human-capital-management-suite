package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/semver"
)

// A major change to a referenced document raises one notice per reference
// naming what the agent still reads and what the document changed to;
// corrections, unreadable documents and unparsable labels stay quiet
// (AGENTUX-065).
func TestAgentUX065_DocumentDriftNotices(t *testing.T) {
	refs := []productui.PersonaAdminDocumentReference{
		{DocumentID: "pto", Title: "Paid time off policy", Label: "PTO", VersionMode: "COMPATIBLE", CompatibleBase: "1.0.0", Readable: true},
		{DocumentID: "guide", Title: "Guide", Label: "Guide", VersionMode: "COMPATIBLE", CompatibleBase: "1.0.0", Readable: true},
		{DocumentID: "exact", Title: "Exact", Label: "Exact", VersionMode: "PINNED_LABEL", PinnedLabel: "1.2.0", Readable: true},
		{DocumentID: "hidden", Title: "", Label: "Hidden", VersionMode: "COMPATIBLE", CompatibleBase: "1.0.0", Readable: false},
	}
	lines := map[string]DocumentLineLabels{
		"pto":    {Reading: "1.4.2", Latest: "2.0.0"},
		"guide":  {Reading: "1.4.2", Latest: "1.5.0"},
		"exact":  {Reading: "1.2.0", Latest: "1.2.0"},
		"hidden": {Reading: "1.0.0", Latest: "3.0.0"},
	}
	notices := personaDocumentDriftNotices(refs, lines)
	if len(notices) != 1 || notices[0].DocumentID != "pto" || notices[0].Title != "Paid time off policy" || notices[0].Reading != "1.4.2" || notices[0].Latest != "2.0.0" {
		t.Fatalf("drift notices = %+v; want one for pto", notices)
	}
	if got := personaDocumentDriftNotices(refs[:1], map[string]DocumentLineLabels{"pto": {Reading: "yesterday", Latest: "2.0.0"}}); len(got) != 0 {
		t.Fatalf("unparsable reading raised %+v", got)
	}
}

// Line labels resolve under each reference's own mode and stay silent for
// documents the actor may not read (AGENTUX-065).
func TestAgentUX065_LineLabelsResolve(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	const tenant, owner, stranger = "tenant-lines", "owner", "stranger"
	doc, err := svc.store.CreateDocument(ctx, tenant, owner, "COMPANY")
	if err != nil {
		t.Fatal(err)
	}
	submit := func(markdown, base string) documenthubstore.Version {
		t.Helper()
		version, err := svc.store.SubmitCandidate(ctx, tenant, documenthubstore.Version{DocumentID: doc, CreatorID: owner, Title: "Guide", Markdown: markdown, Classification: "INTERNAL"}, base)
		if err != nil {
			t.Fatal(err)
		}
		return version
	}
	deploy := func(version documenthubstore.Version, expected string, bump semver.Bump) {
		t.Helper()
		if _, err := svc.store.RecordReview(ctx, tenant, documenthubstore.ReviewInput{DocumentID: doc, VersionID: version.ID, ScopeKind: "placement", ScopeID: "general", ReviewerID: "reviewer", Authority: "policy-owner", Decision: "approved"}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.store.Deploy(ctx, tenant, documenthubstore.DeployInput{DocumentID: doc, VersionID: version.ID, ScopeKind: "placement", ScopeID: "general", DeployerID: owner, ExpectedLive: expected, Bump: bump}); err != nil {
			t.Fatal(err)
		}
	}
	v1 := submit("First.", "")
	v2 := submit("Second, corrected.", v1.ID)
	v3 := submit("Third, rewritten.", v2.ID)
	deploy(v1, "", semver.BumpPatch)
	second, err := svc.store.ResolveDeployment(ctx, tenant, doc, "placement", "general")
	if err != nil {
		t.Fatal(err)
	}
	deploy(v2, second.VersionID, semver.BumpMinor)
	third, err := svc.store.ResolveDeployment(ctx, tenant, doc, "placement", "general")
	if err != nil {
		t.Fatal(err)
	}
	deploy(v3, third.VersionID, semver.BumpMajor)
	lines, err := svc.ResolveDocumentLineLabels(ctx, tenant, owner, []agentdocref.Reference{
		{DocumentID: doc, VersionMode: agentdocref.ModeCompatible, CompatibleBase: "1.0.0", Label: "Line"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if lines[doc] != (DocumentLineLabels{Reading: "1.1.0", Latest: "2.0.0"}) {
		t.Fatalf("caret lines = %+v; want reading 1.1.0 latest 2.0.0", lines)
	}
	hidden, err := svc.ResolveDocumentLineLabels(ctx, tenant, stranger, []agentdocref.Reference{
		{DocumentID: doc, VersionMode: agentdocref.ModeCompatible, CompatibleBase: "1.0.0", Label: "Line"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hidden) != 0 {
		t.Fatalf("unreadable lines resolved: %+v", hidden)
	}
}
