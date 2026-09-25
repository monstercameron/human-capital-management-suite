package productclient

import (
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"testing"
)

func TestDocumentBrowseRouteRoundTrip(t *testing.T) {
	state, err := ParseState("/workspace/app/docs", "docs_q=policy&collection=shared&cursor=abc123")
	if err != nil {
		t.Fatal(err)
	}
	if state.Request.DocumentQuery != "policy" || state.Request.DocumentCollection != "shared" || state.Request.DocumentPageToken != "abc123" {
		t.Fatalf("parsed state = %+v", state.Request)
	}
	if got := CanonicalHref(state); got != "/workspace/app/docs?collection=shared&cursor=abc123&docs_q=policy" {
		t.Fatalf("canonical route = %q", got)
	}
}

// TestDocumentLibraryRouteFullRoundTrip is NAV-01's pure URL<->state mapping
// coverage for the Docs list address: every param the audit found losing its
// browser-history agreement (folder/collection, docs_sort, docs_page,
// docs_size, docs_mode, docs_owner) must parse into the matching request
// field and the resolved view must re-emit the identical canonical query, so
// a reload or a popstate that lands back on this address reconstructs
// exactly the same list -- not a default one.
func TestDocumentLibraryRouteFullRoundTrip(t *testing.T) {
	query := "collection=shared&docs_mode=contains&docs_owner=worker-9&docs_page=3&docs_q=policy&docs_size=100&docs_sort=title&folder=folder-7"
	state, err := ParseState("/workspace/app/docs", query)
	if err != nil {
		t.Fatal(err)
	}
	request := state.Request
	if request.DocumentQuery != "policy" || request.DocumentCollection != "shared" || request.DocumentFolder != "folder-7" ||
		request.DocumentSort != "title" || request.DocumentOwner != "worker-9" || request.DocumentPage != 3 ||
		request.DocumentPerPage != 100 || request.DocumentSearchMode != "contains" {
		t.Fatalf("parsed request = %+v", request)
	}
	view := productui.ApplyRequest(productui.NewView(productui.PageDocs, "tenant", "actor", "scope"), request)
	if got := ResolvedCanonicalHref(state, view); got != "/workspace/app/docs?"+query {
		t.Fatalf("re-emitted route = %q, want /workspace/app/docs?%s", got, query)
	}
}

// TestDocumentLibraryRouteFolderClearsCollection is the same round trip for a
// folder selection, which the address treats as an alternative to a
// collection (docs_library.go's route.view clears one when the other is
// set): the parsed request and the re-emitted href must agree on which one
// the URL names.
func TestDocumentLibraryRouteFolderClearsCollection(t *testing.T) {
	state, err := ParseState("/workspace/app/docs", "folder=folder-1&docs_sort=updated_asc")
	if err != nil {
		t.Fatal(err)
	}
	if state.Request.DocumentFolder != "folder-1" || state.Request.DocumentSort != "updated_asc" {
		t.Fatalf("parsed request = %+v", state.Request)
	}
	view := productui.ApplyRequest(productui.NewView(productui.PageDocs, "tenant", "actor", "scope"), state.Request)
	if got := ResolvedCanonicalHref(state, view); got != "/workspace/app/docs?docs_sort=updated_asc&folder=folder-1" {
		t.Fatalf("re-emitted route = %q", got)
	}
}

func TestDocumentExpiredCursorCanonicalFallback(t *testing.T) {
	state, err := ParseState("/workspace/app/docs", "collection=private&docs_q=policy&cursor=expired")
	if err != nil {
		t.Fatal(err)
	}
	view := productui.ApplyRequest(productui.NewView(productui.PageDocs, "tenant", "actor", "scope"), state.Request)
	view.DocumentPageToken = ""
	if got := ResolvedCanonicalHref(state, view); got != "/workspace/app/docs?collection=private&docs_q=policy" {
		t.Fatalf("reset route = %q", got)
	}
}
