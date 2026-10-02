package main

import (
	"context"
	"testing"
	"time"

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type agentDocPickerDocumentClient struct {
	documentv1.DocumentServiceClient
}

func (agentDocPickerDocumentClient) ListDocuments(context.Context, *documentv1.ListDocumentsRequest, ...grpc.CallOption) (*documentv1.ListDocumentsResponse, error) {
	return &documentv1.ListDocumentsResponse{Documents: []*documentv1.DocumentSummary{{DocumentId: "doc-policy", Title: "Benefits policy", FolderId: "folder-people", UpdatedAt: timestamppb.New(time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC))}}}, nil
}

func (agentDocPickerDocumentClient) GetDocumentLibrary(context.Context, *documentv1.GetDocumentLibraryRequest, ...grpc.CallOption) (*documentv1.GetDocumentLibraryResponse, error) {
	return &documentv1.GetDocumentLibraryResponse{Folders: []*documentv1.DocumentFolder{{FolderId: "folder-people", Name: "People operations"}}}, nil
}

func (agentDocPickerDocumentClient) ListDocumentVersions(context.Context, *documentv1.ListDocumentVersionsRequest, ...grpc.CallOption) (*documentv1.ListDocumentVersionsResponse, error) {
	return &documentv1.ListDocumentVersionsResponse{Versions: []*documentv1.DocumentVersionSummary{{VersionId: "v1"}, {VersionId: "v2"}, {VersionId: "v3", IsCurrent: true}}}, nil
}

func TestAgentDocPersonaDocumentSearchUsesAuthorizedHubProjection(t *testing.T) {
	items, err := searchPersonaAdminDocuments(context.Background(), agentDocPickerDocumentClient{}, "benefits", productui.ResolveProductLocale("en-US"), time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].DocumentID != "doc-policy" || items[0].Location != "People operations" || items[0].PublishedVersion != 3 || items[0].Updated == "" {
		t.Fatalf("search result = %#v", items)
	}
}
