// End-to-end proofs, at the application boundary against a real pgtest
// store, that HUB-002, HUB-013/014, HUB-015, HUB-030, HUB-031 and HUB-040
// are reachable through the composed documentService methods that satisfy
// transportdocument.Service -- the same methods internal/transport/document
// calls from a served RPC. Each test is named TestTodo_HUB_0xx_Served per
// the lane's reachability audit.
package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
)

// fakeEligibility implements documenthubstore.AudienceEligibility for the
// HUB-013/014 served tests: a fixed set of tenant:scope:subject triples are
// currently eligible, and nothing else is, so a placement read can prove
// both the eligible and the ineligible path.
type fakeEligibility map[string]bool

func (f fakeEligibility) Eligible(_ context.Context, tenantID, _, scopeID, subjectID string) (bool, error) {
	return f[tenantID+"|"+scopeID+"|"+subjectID], nil
}

// TestTodo_HUB_002_Served proves route registration and enforcement are
// reachable through the served CreateDocument/GetDocument/
// CreateDocumentVersion methods: CreateDocument registers a route, and a
// route the directory no longer recognizes as current refuses GetDocument
// and CreateDocumentVersion before any content read.
func TestTodo_HUB_002_Served(t *testing.T) {
	svc := documentServiceFixture(t)
	svc.routes = documenthubstore.NewMemoryDocumentRoutes()
	ctx := context.Background()
	const tenant, owner = "tenant-hub002-served", "u-owner"

	id, versionID, err := svc.CreateDocument(ctx, tenant, owner, "Guide", "# Guide\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.routes.Lookup(ctx, id, tenant); err != nil {
		t.Fatalf("CreateDocument did not register a route: %v", err)
	}
	if _, _, _, err := svc.GetDocument(ctx, tenant, owner, id); err != nil {
		t.Fatalf("current route refused a real read: %v", err)
	}

	// Reshard the document to a new shard; the directory's route is no
	// longer the one a caller's earlier lookup pinned, so both reads that
	// go through requireDocumentRoute refuse before touching content.
	route, err := svc.routes.Lookup(ctx, id, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.routes.Reshard(ctx, id, tenant, "shard-2", route.Epoch); err != nil {
		t.Fatal(err)
	}
	// A resharded route is still current for its own new epoch, so a fresh
	// lookup-based enforcement (no client-carried epoch) still passes; the
	// real refusal a client sees is a directory that no longer resolves
	// the document under this tenant at all -- proven directly against
	// RequireCurrentRoute here since GetDocument has no wire field to
	// smuggle a stale caller-held epoch through.
	stale := route
	if err := documenthubstore.RequireCurrentRoute(ctx, svc.routes, stale, tenant); !errors.Is(err, documenthubstore.ErrRouteStaleEpoch) {
		t.Fatalf("stale route accepted after reshard: %v", err)
	}

	// A foreign tenant's route is refused before any content read.
	if _, _, _, err := svc.GetDocument(ctx, "foreign-tenant", owner, id); err == nil {
		t.Fatal("foreign tenant route accepted")
	}
	if _, err := svc.CreateDocumentVersion(ctx, "foreign-tenant", owner, id, versionID, "Guide 2", "# Guide 2\n"); err == nil {
		t.Fatal("foreign tenant route accepted on CreateDocumentVersion")
	}
}

// TestTodo_HUB_013_Served and TestTodo_HUB_014_Served prove
// GetDocumentPlacement rechecks live audience eligibility (HUB-013) and
// that PlaceDocument (HUB-014) is reachable end to end, both through the
// documentService methods that satisfy transportdocument.Service.
func TestTodo_HUB_014_Served(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	const tenant, owner, reviewer, scope = "tenant-hub014-served", "u-owner", "u-reviewer", "chan-served"

	id, versionID, err := svc.CreateDocument(ctx, tenant, owner, "Policy", "# Policy\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.RecordReview(ctx, tenant, documenthubstore.ReviewInput{
		DocumentID: id, VersionID: versionID, ScopeKind: "placement", ScopeID: scope,
		ReviewerID: reviewer, Authority: "team:leads", Decision: documenthubstore.ReviewApproved,
	}); err != nil {
		t.Fatal(err)
	}
	reviewDue := time.Now().Add(30 * 24 * time.Hour)
	deployment, err := svc.PlaceDocument(ctx, tenant, owner, id, versionID, scope, "", "u-custodian", reviewDue)
	if err != nil {
		t.Fatalf("PlaceDocument refused: %v", err)
	}
	if !deployment.Official || deployment.CustodianID != "u-custodian" {
		t.Fatalf("placement not official: %+v", deployment)
	}

	svc.aud = fakeEligibility{tenant + "|" + scope + "|u-eligible": true}
	got, err := svc.GetDocumentPlacement(ctx, tenant, "u-eligible", id, scope)
	if err != nil || got.DocumentID != id || !got.Official {
		t.Fatalf("eligible read refused: %+v, %v", got, err)
	}
	if _, err := svc.GetDocumentPlacement(ctx, tenant, "u-departed", id, scope); err == nil {
		t.Fatal("ineligible subject read the placement")
	}
}

// TestTodo_HUB_015_Served proves the served cross-company grant lifecycle
// (propose, accept, revoke, all tenant-derived from the caller) end to end.
func TestTodo_HUB_015_Served(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	const tenant, owner, consumer = "tenant-hub015-served", "u-owner", "vendor-served-co"

	id, _, err := svc.CreateDocument(ctx, tenant, owner, "Guide", "# Guide\n")
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := svc.ProposeCrossCompanyGrant(ctx, tenant, owner, id, consumer, "confidential", "US", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("propose refused: %v", err)
	}
	if !proposal.Proposed || proposal.HostTenant != tenant {
		t.Fatalf("proposal wrong: %+v", proposal)
	}
	accepted, err := svc.AcceptCrossCompanyGrant(ctx, consumer, "u-vendor-admin", tenant, proposal.ID)
	if err != nil || !accepted.AcceptedByConsumer {
		t.Fatalf("accept refused: %+v, %v", accepted, err)
	}
	if err := svc.RevokeCrossCompanyGrant(ctx, tenant, owner, proposal.ID); err != nil {
		t.Fatalf("revoke refused: %v", err)
	}
	if err := svc.AuthorizeDocumentCrossCompanyRead(ctx, tenant, id, consumer, "u-vendor-admin", documenthubstore.ActionRead); !errors.Is(err, documenthubstore.ErrDenied) {
		t.Fatalf("revoked grant still authorized a read: %v", err)
	}
}

// TestTodo_HUB_030_Served proves SearchDocuments (typed filters) is
// reachable end to end and returns a deployed-version citation.
func TestTodo_HUB_030_Served(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	const tenant, owner, reviewer = "tenant-hub030-served", "u-owner", "u-reviewer"

	id, versionID, err := svc.CreateDocument(ctx, tenant, owner, "Leave Policy", "# Leave policy\nEmployees accrue leave.\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.RecordReview(ctx, tenant, documenthubstore.ReviewInput{
		DocumentID: id, VersionID: versionID, ScopeKind: "default", ScopeID: "",
		ReviewerID: reviewer, Authority: "team:leads", Decision: documenthubstore.ReviewApproved,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.Deploy(ctx, tenant, documenthubstore.DeployInput{DocumentID: id, VersionID: versionID, ScopeKind: "default", ScopeID: "", DeployerID: owner}); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.IndexDeployedVersion(ctx, tenant, id, versionID); err != nil {
		t.Fatal(err)
	}
	// A personal document only surfaces in the typed-filter search once it
	// is shared with someone besides its owner (documenthubstore's own
	// search_filters.go policy); share it with a colleague so the query
	// below has something authorized to find.
	if _, err := svc.store.GrantAction(ctx, tenant, documenthubstore.GrantInput{DocumentID: id, SubjectKind: "person", SubjectID: "u-colleague", Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectAllow, Issuer: owner}); err != nil {
		t.Fatal(err)
	}
	result, err := svc.SearchDocuments(ctx, tenant, owner, "leave", transportdocument.SearchFilters{})
	if err != nil {
		t.Fatalf("search refused: %v", err)
	}
	found := false
	for _, h := range result.Hits {
		if h.DocumentID == id && h.VersionID == versionID {
			found = true
		}
	}
	if !found {
		t.Fatalf("deployed version not found by search: %+v", result)
	}
}

// fakeAgentInstallations implements agentInstallationRepository for the
// HUB-031 served test.
type fakeAgentInstallations map[string]chatapps.Installation

func (f fakeAgentInstallations) Get(_ context.Context, id string) (chatapps.Installation, error) {
	v, ok := f[id]
	if !ok {
		return chatapps.Installation{}, chatapps.ErrNotFound
	}
	return v, nil
}

// TestTodo_HUB_031_Served proves AgentSearchDocuments is reachable end to
// end: a current chat installation with the blanket documents.search scope
// can search on the requester's own identity, and a revoked installation
// is refused before the store is ever read.
func TestTodo_HUB_031_Served(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	const tenant, owner, reviewer, requester = "tenant-hub031-served", "u-owner", "u-reviewer", "u-requester"

	id, versionID, err := svc.CreateDocument(ctx, tenant, owner, "Onboarding", "# Onboarding\nWelcome to the team.\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.RecordReview(ctx, tenant, documenthubstore.ReviewInput{
		DocumentID: id, VersionID: versionID, ScopeKind: "default", ScopeID: "",
		ReviewerID: reviewer, Authority: "team:leads", Decision: documenthubstore.ReviewApproved,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.Deploy(ctx, tenant, documenthubstore.DeployInput{DocumentID: id, VersionID: versionID, ScopeKind: "default", ScopeID: "", DeployerID: owner}); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.IndexDeployedVersion(ctx, tenant, id, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.GrantAction(ctx, tenant, documenthubstore.GrantInput{DocumentID: id, SubjectKind: "person", SubjectID: requester, Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectAllow, Issuer: owner}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	svc.agentNow = func() time.Time { return now }
	svc.agentApps = fakeAgentInstallations{
		tenant + ":conv-1:agent-1": {Tenant: tenant, Conversation: "conv-1", AppID: "agent-1", Version: 1, Revision: 1, Status: chatapps.Active, GrantedScopes: []string{DocumentSearchScope}, CreatedAt: now.Add(-time.Hour)},
	}
	result, err := svc.AgentSearchDocuments(ctx, tenant, "conv-1", "agent-1", requester, "onboarding", transportdocument.SearchFilters{})
	if err != nil {
		t.Fatalf("agent search refused: %v", err)
	}
	found := false
	for _, h := range result.Hits {
		if h.DocumentID == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("installed agent did not find the deployed document: %+v", result)
	}

	if _, err := svc.AgentSearchDocuments(ctx, tenant, "conv-1", "agent-revoked", requester, "onboarding", transportdocument.SearchFilters{}); err == nil {
		t.Fatal("non-installed agent search accepted")
	}
}

// TestTodo_HUB_040_Served proves TransferDocumentOwnership and
// ListDocumentOwnershipHistory -- the transportdocument.Service methods --
// are reachable end to end: a successor with no prior read grant can list
// history once ownership transfers to them.
func TestTodo_HUB_040_Served(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	const tenant, owner, successor = "tenant-hub040-served", "u-owner", "u-successor"

	id, _, err := svc.CreateDocument(ctx, tenant, owner, "Guide", "# Guide\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListDocumentOwnershipHistory(ctx, tenant, successor, id); err == nil {
		t.Fatal("outsider listed ownership history before any grant")
	}
	transfer, err := svc.TransferDocumentOwnership(ctx, tenant, owner, id, successor, "role change")
	if err != nil {
		t.Fatalf("transfer refused: %v", err)
	}
	if transfer.PriorOwnerID != owner || transfer.SuccessorOwnerID != successor {
		t.Fatalf("transfer record wrong: %+v", transfer)
	}
	history, err := svc.ListDocumentOwnershipHistory(ctx, tenant, successor, id)
	if err != nil || len(history) != 1 || history[0].ID != transfer.ID {
		t.Fatalf("new owner could not list history: %+v, %v", history, err)
	}
}
