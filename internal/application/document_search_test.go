package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

// deployedSearchFixture creates, reviews and deploys one document version
// for tenant/owner and returns its version ID, mirroring the store-level
// search fixtures but through the application-level documentService so
// the wrapper is exercised against the real store.
func deployedSearchFixture(t *testing.T, svc documentService, ctx context.Context, tenant, owner, title, markdown string) (string, string) {
	t.Helper()
	docID, versionID, err := svc.CreateDocument(ctx, tenant, owner, title, markdown)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.RecordReview(ctx, tenant, documenthubstore.ReviewInput{
		DocumentID: docID, VersionID: versionID, ScopeKind: "default", ScopeID: "",
		ReviewerID: "u-reviewer", Authority: "team:leads", Decision: documenthubstore.ReviewApproved,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.GrantAction(ctx, tenant, documenthubstore.GrantInput{
		DocumentID: docID, SubjectKind: "person", SubjectID: "u-deployer",
		Action: documenthubstore.ActionDeploy, Effect: documenthubstore.EffectAllow, Issuer: owner,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.Deploy(ctx, tenant, documenthubstore.DeployInput{
		DocumentID: docID, VersionID: versionID, ScopeKind: "default", ScopeID: "", DeployerID: "u-deployer",
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.IndexDeployedVersion(ctx, tenant, docID, versionID); err != nil {
		t.Fatal(err)
	}
	return docID, versionID
}

// TestDocumentSearchService_SearchDocuments exercises the application
// boundary wrapper: filters pass through to the store, and an authorized
// reader gets a citation-carrying hit for a query only they can see.
func TestDocumentSearchService_SearchDocuments(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	const tenant, owner, reader = "tenant-search", "u-owner", "u-reader"

	docID, versionID := deployedSearchFixture(t, svc, ctx, tenant, owner, "Travel Policy", "# Travel\n\nExpense and travel policy details.\n")
	if err := svc.ShareDocument(ctx, tenant, owner, docID, reader, ""); err != nil {
		t.Fatal(err)
	}

	search := newDocumentSearchService(svc.store)
	res, err := search.SearchDocuments(ctx, tenant, reader, "travel", DocumentSearchFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].DocumentID != docID || res.Hits[0].VersionID != versionID {
		t.Fatalf("unexpected search result: %+v", res)
	}
	if res.Hits[0].Status != "deployed" || res.Hits[0].DeployedAt.IsZero() {
		t.Fatalf("missing citation fields: %+v", res.Hits[0])
	}

	// A stranger with no grant sees nothing, even unfiltered.
	res, err = search.SearchDocuments(ctx, tenant, "u-stranger", "travel", DocumentSearchFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("stranger saw a restricted document: %+v", res)
	}

	// An empty actor ID is refused outright.
	if _, err := search.SearchDocuments(ctx, tenant, "", "travel", DocumentSearchFilters{}); err == nil {
		t.Fatal("empty actor accepted")
	}

	// A locale filter that does not match excludes the hit.
	res, err = search.SearchDocuments(ctx, tenant, reader, "travel", DocumentSearchFilters{Locale: "de-DE"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("mismatched locale filter still matched: %+v", res)
	}
}
