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
