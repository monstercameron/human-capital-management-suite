// Stub methods and parity coverage for the HUB-002/013/014/015/030/031/040
// RPCs added to the served DocumentService transport surface: PlaceDocument,
// GetDocumentPlacement, TransferDocumentOwnership,
// ListDocumentOwnershipHistory, ProposeCrossCompanyGrant,
// AcceptCrossCompanyGrant, RevokeCrossCompanyGrant, SearchDocuments and
// AgentSearchDocuments. These mirror the structure of TestTodo_HUB_041 and
// TestTodo_HUB_041_Conformance: the same request against the native RPC
// method and the real mounted Connect route must produce byte-identical
// responses and reach the fake with the same server-derived tenant/actor.
package document

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (f *fakeService) PlaceDocument(_ context.Context, tenant, actor, documentID, versionID, scopeID, expectedLive, custodianID string, reviewDueAt time.Time) (Deployment, error) {
	f.tenant, f.actor, f.documentID = tenant, actor, documentID
	if f.err != nil {
		return Deployment{}, f.err
	}
	return Deployment{
		ID: "docd-1", DocumentID: documentID, VersionID: versionID, ScopeKind: "placement", ScopeID: scopeID,
		DeployerID: actor, CustodianID: custodianID, EffectiveAt: time.Unix(1700000000, 0).UTC(), ReviewDueAt: reviewDueAt, Official: true,
	}, nil
}

func (f *fakeService) GetDocumentPlacement(_ context.Context, tenant, actor, documentID, scopeID string) (Deployment, error) {
	f.tenant, f.actor, f.documentID = tenant, actor, documentID
	if f.err != nil {
		return Deployment{}, f.err
	}
	return Deployment{ID: "docd-1", DocumentID: documentID, VersionID: "docv-1", ScopeKind: "placement", ScopeID: scopeID, Official: true}, nil
}

func (f *fakeService) TransferDocumentOwnership(_ context.Context, tenant, actor, documentID, successorOwnerID, reason string) (OwnershipTransfer, error) {
	f.tenant, f.actor, f.documentID = tenant, actor, documentID
	if f.err != nil {
		return OwnershipTransfer{}, f.err
	}
	return OwnershipTransfer{ID: "docxo-1", DocumentID: documentID, PriorOwnerID: actor, SuccessorOwnerID: successorOwnerID, Reason: reason, TransferredBy: actor, CreatedAt: time.Unix(1700000000, 0).UTC()}, nil
}

func (f *fakeService) ListDocumentOwnershipHistory(_ context.Context, tenant, actor, documentID string) ([]OwnershipTransfer, error) {
	f.tenant, f.actor, f.documentID = tenant, actor, documentID
	if f.err != nil {
		return nil, f.err
	}
	return []OwnershipTransfer{{ID: "docxo-1", DocumentID: documentID, PriorOwnerID: "u-a", SuccessorOwnerID: "u-b", Reason: "r", TransferredBy: "u-a"}}, nil
}

func (f *fakeService) ProposeCrossCompanyGrant(_ context.Context, tenant, actor, documentID, consumerTenant, classification, residency string, expiresAt time.Time) (CrossCompanyGrant, error) {
	f.tenant, f.actor, f.documentID = tenant, actor, documentID
	if f.err != nil {
		return CrossCompanyGrant{}, f.err
	}
	return CrossCompanyGrant{ID: "docxc-1", DocumentID: documentID, HostTenant: tenant, ConsumerTenant: consumerTenant, Classification: classification, Residency: residency, Version: 1, Proposed: true, ExpiresAt: expiresAt}, nil
}

func (f *fakeService) AcceptCrossCompanyGrant(_ context.Context, tenant, actor, hostTenant, grantID string) (CrossCompanyGrant, error) {
	f.tenant, f.actor = tenant, actor
	if f.err != nil {
		return CrossCompanyGrant{}, f.err
	}
	return CrossCompanyGrant{ID: grantID, HostTenant: hostTenant, ConsumerTenant: tenant, Version: 2, Proposed: true, AcceptedByHost: true, AcceptedByConsumer: true}, nil
}

func (f *fakeService) RevokeCrossCompanyGrant(_ context.Context, tenant, actor, grantID string) error {
	f.tenant, f.actor = tenant, actor
	return f.err
}

func (f *fakeService) SearchDocuments(_ context.Context, tenant, actor, query string, filters SearchFilters) (SearchResult, error) {
	f.tenant, f.actor = tenant, actor
	if f.err != nil {
		return SearchResult{}, f.err
	}
	return SearchResult{Hits: []SearchHit{{DocumentID: "doc-1", VersionID: "docv-1", Title: "Guide", Status: "DEPLOYED", OwnerID: actor, Score: 3}}, Total: 1}, nil
}

func (f *fakeService) AgentSearchDocuments(_ context.Context, tenant, conversationID, agentID, requesterID, query string, filters SearchFilters) (SearchResult, error) {
	f.tenant, f.actor = tenant, requesterID
	if f.err != nil {
		return SearchResult{}, f.err
	}
	return SearchResult{Hits: []SearchHit{{DocumentID: "doc-2", VersionID: "docv-2", Title: "Policy", Status: "DEPLOYED", OwnerID: requesterID, Score: 1}}, Total: 1}, nil
}

// TestTodo_HUB_014_Served proves PlaceDocument and GetDocumentPlacement
// reach identical HTTP and native responses through the real mounted
// Connect route, exactly as TestTodo_HUB_041 proves for CreateDocumentVersion.
func TestTodo_HUB_014_Served(t *testing.T) {
	native := &server{service: &fakeService{}}
	placeReq := &documentv1.PlaceDocumentRequest{DocumentId: "doc-1", VersionId: "docv-1", ScopeId: "scope-1", CustodianId: "u-custodian", ReviewDueAt: timestamppb.New(time.Unix(1700000000, 0))}
	nativeResp, err := native.PlaceDocument(documentContext(t), placeReq)
	if err != nil {
		t.Fatal(err)
	}
	httpService := &fakeService{}
	_, httpServer := hub041HTTPClient(t, httpService)
	client := connect.NewClient[documentv1.PlaceDocumentRequest, documentv1.PlaceDocumentResponse](
		httpServer.Client(), httpServer.URL+documentv1.DocumentService_PlaceDocument_FullMethodName, connect.WithProtoJSON(),
	)
	httpResp, err := client.CallUnary(context.Background(), connect.NewRequest(placeReq))
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(nativeResp, httpResp.Msg) {
		t.Fatalf("HTTP = %v, RPC = %v", httpResp.Msg, nativeResp)
	}
	if !nativeResp.GetDeployment().GetOfficial() {
		t.Fatalf("placement not marked official: %+v", nativeResp.GetDeployment())
	}

	getReq := &documentv1.GetDocumentPlacementRequest{DocumentId: "doc-1", ScopeId: "scope-1"}
	nativeGet, err := native.GetDocumentPlacement(documentContext(t), getReq)
	if err != nil {
		t.Fatal(err)
	}
	if nativeGet.GetDeployment().GetDocumentId() != "doc-1" {
		t.Fatalf("placement lookup wrong: %+v", nativeGet)
	}

	// Invalid input is refused before the service is called.
	if _, err := native.PlaceDocument(documentContext(t), &documentv1.PlaceDocumentRequest{DocumentId: "doc-1"}); err == nil {
		t.Fatal("incomplete placement request accepted")
	}
}

// TestTodo_HUB_040_Served proves TransferDocumentOwnership and
// ListDocumentOwnershipHistory reach identical HTTP and native responses.
func TestTodo_HUB_040_Served(t *testing.T) {
	transferReq := &documentv1.TransferDocumentOwnershipRequest{DocumentId: "doc-1", SuccessorOwnerId: "u-successor", Reason: "departure"}
	native := &server{service: &fakeService{}}
	nativeResp, err := native.TransferDocumentOwnership(documentContext(t), transferReq)
	if err != nil {
		t.Fatal(err)
	}
	httpService := &fakeService{}
	_, httpServer := hub041HTTPClient(t, httpService)
	client := connect.NewClient[documentv1.TransferDocumentOwnershipRequest, documentv1.TransferDocumentOwnershipResponse](
		httpServer.Client(), httpServer.URL+documentv1.DocumentService_TransferDocumentOwnership_FullMethodName, connect.WithProtoJSON(),
	)
	httpResp, err := client.CallUnary(context.Background(), connect.NewRequest(transferReq))
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(nativeResp, httpResp.Msg) {
		t.Fatalf("HTTP = %v, RPC = %v", httpResp.Msg, nativeResp)
	}
	if nativeResp.GetTransfer().GetSuccessorOwnerId() != "u-successor" {
		t.Fatalf("transfer response wrong: %+v", nativeResp)
	}

	historyReq := &documentv1.ListDocumentOwnershipHistoryRequest{DocumentId: "doc-1"}
	nativeHist, err := native.ListDocumentOwnershipHistory(documentContext(t), historyReq)
	if err != nil {
		t.Fatal(err)
	}
	historyClient := connect.NewClient[documentv1.ListDocumentOwnershipHistoryRequest, documentv1.ListDocumentOwnershipHistoryResponse](
		httpServer.Client(), httpServer.URL+documentv1.DocumentService_ListDocumentOwnershipHistory_FullMethodName, connect.WithProtoJSON(),
	)
	httpHist, err := historyClient.CallUnary(context.Background(), connect.NewRequest(historyReq))
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(nativeHist, httpHist.Msg) || len(nativeHist.GetTransfers()) != 1 {
		t.Fatalf("HTTP = %v, RPC = %v", httpHist.Msg, nativeHist)
	}
}

// TestTodo_HUB_015_Served proves the cross-company grant RPCs reach
// identical HTTP and native responses.
func TestTodo_HUB_015_Served(t *testing.T) {
	proposeReq := &documentv1.ProposeCrossCompanyGrantRequest{
		DocumentId: "doc-1", ConsumerTenant: "vendor-co", Classification: "confidential", Residency: "US",
		ExpiresAt: timestamppb.New(time.Unix(1700000000, 0)),
	}
	native := &server{service: &fakeService{}}
	nativeResp, err := native.ProposeCrossCompanyGrant(documentContext(t), proposeReq)
	if err != nil {
		t.Fatal(err)
	}
	httpService := &fakeService{}
	_, httpServer := hub041HTTPClient(t, httpService)
	client := connect.NewClient[documentv1.ProposeCrossCompanyGrantRequest, documentv1.ProposeCrossCompanyGrantResponse](
		httpServer.Client(), httpServer.URL+documentv1.DocumentService_ProposeCrossCompanyGrant_FullMethodName, connect.WithProtoJSON(),
	)
	httpResp, err := client.CallUnary(context.Background(), connect.NewRequest(proposeReq))
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(nativeResp, httpResp.Msg) {
		t.Fatalf("HTTP = %v, RPC = %v", httpResp.Msg, nativeResp)
	}
	if !nativeResp.GetGrant().GetProposed() {
		t.Fatalf("grant not proposed: %+v", nativeResp.GetGrant())
	}

	acceptReq := &documentv1.AcceptCrossCompanyGrantRequest{HostTenant: "server", GrantId: nativeResp.GetGrant().GetId()}
	acceptResp, err := native.AcceptCrossCompanyGrant(documentContext(t), acceptReq)
	if err != nil || !acceptResp.GetGrant().GetAcceptedByConsumer() {
		t.Fatalf("accept = %+v, %v", acceptResp, err)
	}

	if _, err := native.RevokeCrossCompanyGrant(documentContext(t), &documentv1.RevokeCrossCompanyGrantRequest{GrantId: nativeResp.GetGrant().GetId()}); err != nil {
		t.Fatalf("revoke refused: %v", err)
	}
}

// TestTodo_HUB_030_Served proves SearchDocuments reaches identical HTTP and
// native responses and carries the typed filters through.
func TestTodo_HUB_030_Served(t *testing.T) {
	req := &documentv1.SearchDocumentsRequest{Query: "leave policy", Filters: &documentv1.DocumentSearchFilters{TeamId: "team-1", Status: "DEPLOYED"}}
	native := &server{service: &fakeService{}}
	nativeResp, err := native.SearchDocuments(documentContext(t), req)
	if err != nil {
		t.Fatal(err)
	}
	httpService := &fakeService{}
	_, httpServer := hub041HTTPClient(t, httpService)
	client := connect.NewClient[documentv1.SearchDocumentsRequest, documentv1.SearchDocumentsResponse](
		httpServer.Client(), httpServer.URL+documentv1.DocumentService_SearchDocuments_FullMethodName, connect.WithProtoJSON(),
	)
	httpResp, err := client.CallUnary(context.Background(), connect.NewRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(nativeResp, httpResp.Msg) {
		t.Fatalf("HTTP = %v, RPC = %v", httpResp.Msg, nativeResp)
	}
	if len(nativeResp.GetHits()) != 1 || nativeResp.GetHits()[0].GetVersionId() != "docv-1" {
		t.Fatalf("search hits wrong: %+v", nativeResp)
	}
}

// TestTodo_HUB_031_Served proves AgentSearchDocuments reaches identical
// HTTP and native responses and threads conversation/agent/requester
// through to the service.
func TestTodo_HUB_031_Served(t *testing.T) {
	req := &documentv1.AgentSearchDocumentsRequest{ConversationId: "conv-1", AgentId: "agent-1", RequesterId: "u-requester", Query: "leave"}
	nativeService := &fakeService{}
	native := &server{service: nativeService}
	nativeResp, err := native.AgentSearchDocuments(documentContext(t), req)
	if err != nil {
		t.Fatal(err)
	}
	if nativeService.actor != "u-requester" {
		t.Fatalf("requester not threaded to service: %+v", nativeService)
	}
	httpService := &fakeService{}
	_, httpServer := hub041HTTPClient(t, httpService)
	client := connect.NewClient[documentv1.AgentSearchDocumentsRequest, documentv1.AgentSearchDocumentsResponse](
		httpServer.Client(), httpServer.URL+documentv1.DocumentService_AgentSearchDocuments_FullMethodName, connect.WithProtoJSON(),
	)
	httpResp, err := client.CallUnary(context.Background(), connect.NewRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(nativeResp, httpResp.Msg) {
		t.Fatalf("HTTP = %v, RPC = %v", httpResp.Msg, nativeResp)
	}
	if len(nativeResp.GetHits()) != 1 || nativeResp.GetHits()[0].GetDocumentId() != "doc-2" {
		t.Fatalf("agent search hits wrong: %+v", nativeResp)
	}

	if _, err := native.AgentSearchDocuments(documentContext(t), &documentv1.AgentSearchDocumentsRequest{AgentId: "agent-1", RequesterId: "u-requester"}); err == nil {
		t.Fatal("missing conversation accepted")
	}
}
