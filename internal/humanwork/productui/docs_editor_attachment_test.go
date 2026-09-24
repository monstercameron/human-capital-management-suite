package productui

import "testing"

func TestDocsEditorKeepsAttachmentLinks(t *testing.T) {
	if !docsEditorSafeHref("attachment:docm-57d5e210-0930") {
		t.Fatal("attachment links must survive the formatted pane")
	}
	if docsEditorSafeHref("attachment:../../etc") {
		t.Fatal("malformed attachment ids must not pass")
	}
}
