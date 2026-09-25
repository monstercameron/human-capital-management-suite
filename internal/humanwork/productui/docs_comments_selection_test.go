package productui

import (
	"strings"
	"testing"
)

func TestTodo_HUB_018_ChatProjectionCommentFallback(t *testing.T) {
	anchor := docsAnchorDraft{Quote: "The team charter is published: who owns what", Prefix: "Rafael Torres", Derived: true}
	request := docsCommentRequest("doc-1", "version-1", "Please clarify this sentence.", "en-US", anchor)
	if request.DocumentID != "doc-1" || request.VersionID != "version-1" || request.Quote != "" || request.Prefix != "" || request.Suffix != "" {
		t.Fatalf("live Chat text became an invalid version anchor: %+v", request)
	}
	if !strings.Contains(request.Body, anchor.Quote) || !strings.HasSuffix(request.Body, "Please clarify this sentence.") {
		t.Fatalf("the selected excerpt or comment was lost: %q", request.Body)
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		label, hint := docsDerivedCommentCopy(locale)
		if label == "" || hint == "" {
			t.Errorf("%s has no explanation for unanchored Chat text", locale)
		}
	}
}

func TestTodo_HUB_018_DocumentSelectionKeepsVersionAnchor(t *testing.T) {
	anchor := docsAnchorDraft{Quote: "Request leave", Prefix: "To ", Suffix: " 30 days"}
	request := docsCommentRequest("doc-2", "version-2", "Check timing.", "en-US", anchor)
	if request.Body != "Check timing." || request.Quote != anchor.Quote || request.Prefix != anchor.Prefix || request.Suffix != anchor.Suffix {
		t.Fatalf("a document passage lost its version anchor: %+v", request)
	}
}
