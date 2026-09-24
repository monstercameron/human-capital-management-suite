package page_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/workspacecontract"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/gwc"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/page"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/ssr"
)

// TestTodo_UIPOLISH_002_Regression proves both production renderer entry
// points carry the shared page primitive stylesheet, without introducing a
// page-to-renderer test import cycle.
func TestTodo_UIPOLISH_002_Regression(t *testing.T) {
	contractValue := contract.WorkspaceContract{Title: "Promotion", WorkspaceID: "workspace-1"}
	ssrDocument, err := ssr.Render(contractValue)
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(ssrDocument, "<style>")
	end := strings.Index(ssrDocument, "</style>")
	if start < 0 || end <= start+len("<style>") {
		t.Fatalf("SSR document has no stylesheet block")
	}
	ssrStylesheet := ssrDocument[start+len("<style>") : end]
	gwcStylesheet := gwc.Stylesheet()
	if ssrStylesheet != gwcStylesheet {
		t.Fatal("SSR and GWC production stylesheets diverged")
	}
	if !strings.Contains(ssrStylesheet, page.LayoutCSS()) {
		t.Fatal("production renderers omit the shared page layout stylesheet")
	}
}
