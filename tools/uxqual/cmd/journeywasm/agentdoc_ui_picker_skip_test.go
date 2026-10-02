package main

import (
	"context"
	"testing"
	"time"

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type agentDocPickerPartialClient struct {
	documentv1.DocumentServiceClient
}

func (agentDocPickerPartialClient) ListDocuments(context.Context, *documentv1.ListDocumentsRequest, ...grpc.CallOption) (*documentv1.ListDocumentsResponse, error) {
	return &documentv1.ListDocumentsResponse{Documents: []*documentv1.DocumentSummary{
		{DocumentId: "doc-gone", Title: "Paid time off draft"},
		{DocumentId: "doc-policy", Title: "Paid time off policy"},
	}}, nil
}

func (agentDocPickerPartialClient) GetDocumentLibrary(context.Context, *documentv1.GetDocumentLibraryRequest, ...grpc.CallOption) (*documentv1.GetDocumentLibraryResponse, error) {
	return &documentv1.GetDocumentLibraryResponse{}, nil
}

func (agentDocPickerPartialClient) ListDocumentVersions(_ context.Context, request *documentv1.ListDocumentVersionsRequest, _ ...grpc.CallOption) (*documentv1.ListDocumentVersionsResponse, error) {
	if request.GetDocumentId() == "doc-gone" {
		return nil, status.Error(codes.NotFound, "document.unavailable")
	}
	return &documentv1.ListDocumentVersionsResponse{Versions: []*documentv1.DocumentVersionSummary{{VersionId: "v1", IsCurrent: true}}}, nil
}

// One unreadable document among the matches is left out; the others are
// still offered. The live cell showed "Search is unavailable" for every
// query because a single listed document answered NOT_FOUND for its versions.
func TestAgentDocPersonaDocumentSearchSkipsAnUnreadableDocument(t *testing.T) {
	items, err := searchPersonaAdminDocuments(context.Background(), agentDocPickerPartialClient{}, "paid", productui.ResolveProductLocale("en-US"), time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("one unreadable document failed the whole search: %v", err)
	}
	if len(items) != 1 || items[0].DocumentID != "doc-policy" {
		t.Fatalf("search result = %#v", items)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := searchPersonaAdminDocuments(cancelled, agentDocPickerPartialClient{}, "paid", productui.ResolveProductLocale("en-US"), time.Now()); err == nil {
		t.Fatal("a cancelled search reported success")
	}
}
