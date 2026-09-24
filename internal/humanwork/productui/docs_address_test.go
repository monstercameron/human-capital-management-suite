package productui

import (
	"net/url"
	"testing"
)

// Shell links that restate the current address (the navigation toggle,
// locale, favorites) must keep the open document, or they land on the list.
func TestDocsAddressKeepsTheOpenDocument(t *testing.T) {
	values := url.Values{}
	RouteProfileDocs.AddressValues(values, View{Page: PageDocs, DocumentID: "doc-1", DocumentEditing: true})
	if values.Get("document") != "doc-1" || values.Get("docs_edit") != "1" {
		t.Fatalf("address values = %v, want document=doc-1 and docs_edit=1", values)
	}
}
