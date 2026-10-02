package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type agentUXPage7DocumentClient struct {
	documentv1.DocumentServiceClient
}

func (agentUXPage7DocumentClient) ListDocuments(context.Context, *documentv1.ListDocumentsRequest, ...grpc.CallOption) (*documentv1.ListDocumentsResponse, error) {
	return &documentv1.ListDocumentsResponse{Documents: []*documentv1.DocumentSummary{
		{DocumentId: "one", Title: "Leave policy", OwnerId: "viewer", FolderId: "people", UpdatedAt: timestamppb.New(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)), Match: "title", Snippet: "hidden"},
		{DocumentId: "two", Title: "Leave policy", OwnerId: "owner-two", FolderId: "legal", UpdatedAt: timestamppb.New(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)), Match: "text", Snippet: "Sick leave is paid."},
	}}, nil
}

func (agentUXPage7DocumentClient) GetDocumentPreviews(context.Context, *documentv1.GetDocumentPreviewsRequest, ...grpc.CallOption) (*documentv1.GetDocumentPreviewsResponse, error) {
	return &documentv1.GetDocumentPreviewsResponse{Previews: []*documentv1.DocumentPreview{
		{DocumentId: "one", Readable: true, OwnerId: "viewer", OwnerName: "Ana Flores"},
		{DocumentId: "two", Readable: true, OwnerId: "owner-two", OwnerName: "Nora Chen"},
	}}, nil
}

func (agentUXPage7DocumentClient) GetDocumentLibrary(context.Context, *documentv1.GetDocumentLibraryRequest, ...grpc.CallOption) (*documentv1.GetDocumentLibraryResponse, error) {
	return &documentv1.GetDocumentLibraryResponse{Folders: []*documentv1.DocumentFolder{{FolderId: "people", Name: "People"}, {FolderId: "legal", Name: "Legal"}}}, nil
}

func (agentUXPage7DocumentClient) ListDocumentVersions(context.Context, *documentv1.ListDocumentVersionsRequest, ...grpc.CallOption) (*documentv1.ListDocumentVersionsResponse, error) {
	return &documentv1.ListDocumentVersionsResponse{Versions: []*documentv1.DocumentVersionSummary{{VersionId: "published", IsCurrent: true}}}, nil
}

func TestAgentUXPage7_DocumentSearchDisambiguatesEqualTitles(t *testing.T) {
	items, err := searchAgentRequestDocumentSuggestions(context.Background(), agentUXPage7DocumentClient{}, "leave", "viewer", productui.ResolveProductLocale("en-US"), time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %#v", items)
	}
	if items[0].Owner != "You" || items[0].Folder != "People" || items[0].Updated != "Sep 25" || items[0].Snippet != "" {
		t.Fatalf("title match = %#v", items[0])
	}
	if items[1].Owner != "Nora Chen" || items[1].Folder != "Legal" || items[1].Snippet != "Sick leave is paid." {
		t.Fatalf("text match = %#v", items[1])
	}
}

func TestAgentUXPage7_PickerClosesAndDatesUseViewerZone(t *testing.T) {
	picker, err := os.ReadFile("agentux_page3_agents_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(picker)
	for _, want := range []string{
		`setAgentRequestDocumentPickerOpen(root, domAttribute(button, "aria-expanded") != "true")`,
		`setAgentRequestDocumentPickerOpen(root, false)`,
		`ResolveProductLocale(cfg.Locale).WithTimeZone(agentViewerTimeZone())`,
		`AgentRequestDocumentChip(locale, documentID, title, anchor)`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("request picker source missing %q", want)
		}
	}
	zone, err := os.ReadFile("agentux_page7_browser_zone_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(zone), `resolvedOptions`) || !strings.Contains(string(zone), `Get("timeZone")`) {
		t.Fatalf("browser adapter does not resolve the viewer's IANA time zone: %s", zone)
	}
}
