package main

import (
	"context"
	"errors"
	"testing"
	"time"

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// agentUX039FaultClient lists five documents. One answers NOT_FOUND for its
// versions, one PERMISSION_DENIED, and one never answers until its own
// deadline passes; two are readable.
type agentUX039FaultClient struct {
	documentv1.DocumentServiceClient
	listErr error
}

func (c agentUX039FaultClient) ListDocuments(context.Context, *documentv1.ListDocumentsRequest, ...grpc.CallOption) (*documentv1.ListDocumentsResponse, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	return &documentv1.ListDocumentsResponse{Documents: []*documentv1.DocumentSummary{
		{DocumentId: "doc-first", Title: "Holiday guide"},
		{DocumentId: "doc-gone", Title: "Removed draft"},
		{DocumentId: "doc-denied", Title: "Restricted handbook"},
		{DocumentId: "doc-slow", Title: "Slow archive"},
		{DocumentId: "doc-last", Title: "Paid time off policy"},
	}}, nil
}

func (agentUX039FaultClient) GetDocumentLibrary(context.Context, *documentv1.GetDocumentLibraryRequest, ...grpc.CallOption) (*documentv1.GetDocumentLibraryResponse, error) {
	return nil, status.Error(codes.Unavailable, "library")
}

func (agentUX039FaultClient) ListDocumentVersions(ctx context.Context, request *documentv1.ListDocumentVersionsRequest, _ ...grpc.CallOption) (*documentv1.ListDocumentVersionsResponse, error) {
	switch request.GetDocumentId() {
	case "doc-gone":
		return nil, status.Error(codes.NotFound, "document.unavailable")
	case "doc-denied":
		return nil, status.Error(codes.PermissionDenied, "document.denied")
	case "doc-slow":
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	return &documentv1.ListDocumentVersionsResponse{Versions: []*documentv1.DocumentVersionSummary{{VersionId: "v1", IsCurrent: true}}}, nil
}

func TestTodo_AGENTUX_039_Fault(t *testing.T) {
	locale := productui.ResolveProductLocale("en-US")
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

	// One document not found, one denied and one that times out, among five:
	// the two readable documents are still offered and the search does not fail.
	started := time.Now()
	items, err := searchPersonaAdminDocuments(context.Background(), agentUX039FaultClient{}, "", locale, now)
	if err != nil {
		t.Fatalf("unreadable documents failed the whole search: %v", err)
	}
	if len(items) != 2 || items[0].DocumentID != "doc-first" || items[1].DocumentID != "doc-last" {
		t.Fatalf("the readable documents are not offered: %#v", items)
	}
	if elapsed := time.Since(started); elapsed < personaDocumentVersionTimeout || elapsed > personaDocumentVersionTimeout+5*time.Second {
		t.Fatalf("the slow document was not bounded by its own deadline: %s", elapsed)
	}

	// When the search runs out of time after finding documents, it offers what
	// it found; with nothing found it fails as a timeout.
	short, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	partial, err := searchPersonaAdminDocuments(short, agentUX039FaultClient{}, "", locale, now)
	cancel()
	if err != nil || len(partial) != 1 || partial[0].DocumentID != "doc-first" {
		t.Fatalf("a search that ran out of time lost what it had found: %#v, %v", partial, err)
	}

	// The search itself failing is the only failure, and each cause has a name
	// the picker turns into a sentence.
	for _, tc := range []struct {
		err       error
		kind      string
		attribute string
	}{
		{status.Error(codes.DeadlineExceeded, "slow"), "timeout", "agentdocFailedTimeout"},
		{context.DeadlineExceeded, "timeout", "agentdocFailedTimeout"},
		{status.Error(codes.PermissionDenied, "no"), "denied", "agentdocFailedDenied"},
		{status.Error(codes.Unauthenticated, "no"), "denied", "agentdocFailedDenied"},
		{status.Error(codes.Unavailable, "down"), "unavailable", "agentdocFailedUnavailable"},
		{status.Error(codes.Internal, "boom"), "", "agentdocFailed"},
		{errors.New("transport closed"), "", "agentdocFailed"},
	} {
		_, searchErr := searchPersonaAdminDocuments(context.Background(), agentUX039FaultClient{listErr: tc.err}, "paid", locale, now)
		if searchErr == nil {
			t.Fatalf("a failed listing (%v) reported success", tc.err)
		}
		if kind := personaDocumentSearchFailure(searchErr); kind != tc.kind || personaDocumentSearchFailureAttribute(kind) != tc.attribute {
			t.Fatalf("failure %v is named %q (%s), want %q (%s)", tc.err, kind, personaDocumentSearchFailureAttribute(kind), tc.kind, tc.attribute)
		}
	}
	if personaDocumentSearchFailure(nil) != "" {
		t.Fatal("no error was named as a failure")
	}
}
