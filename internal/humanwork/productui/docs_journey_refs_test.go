package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestDocsJourneyLinkUnfurls(t *testing.T) {
	view := ApplyLocale(NewView(PageDocs, "tenant-a", "reader-a", "scope-a"), ResolveProductLocale("en-US"))
	view.DocumentOrigin = "https://hcm.example"
	view.Document = &DocumentDetail{Journeys: []DocsJourneyPreview{{IntentID: "01a0-abc", Authorized: true, Worker: "Andre", From: "SEC-ENG · P4", To: "SEC-DIR · M4", Stage: "JOURNEY_STAGE_BLOCKED", StageGroup: "blocked", Effective: "2026-11-01"}}}
	md := "https://hcm.example/workspace/app/journeys?journey=01a0-abc\n\nSee [Andre's promotion](/workspace/app/journeys?journey=01a0-abc) inline.\n"
	if ids := DocsJourneyReferences(md); len(ids) != 1 || ids[0] != "01a0-abc" {
		t.Fatalf("refs = %v", ids)
	}
	out, err := ui.RenderToString(html.Div(html.Props{Class: "docs-markdown"}, docsASTMarkdownNodes(view, md)...))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "docs-journey-embed") || !strings.Contains(out, "SEC-DIR · M4") || !strings.Contains(out, "Blocked") || !strings.Contains(out, "Promotion: Andre") {
		t.Fatalf("journey card or chip missing:\n%s", out)
	}
}
