package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func docsProjectEmbedView(markdown string) (View, string) {
	view := ApplyLocale(NewView(PageDocs, "tenant-a", "reader-a", "scope-a"), ResolveProductLocale("en-US"))
	view.DocumentOrigin = "https://hcm.example"
	view.Document = &DocumentDetail{ProjectTasks: []DocsProjectTaskPreview{
		{ProjectID: "proj-2", TaskID: "task-17", Title: "Prepare launch", Status: "In progress", Authorized: true, ProjectName: "Q4 Benefits", Key: "T-TASK17", StatusTone: "doing", Assignee: "Evelyn Morgan", PriorityID: "TASK_PRIORITY_HIGH", DueDate: "2020-01-02"},
		{ProjectID: "proj-2", Title: "Q4 Benefits", Authorized: true, Done: 3, Total: 4},
	}}
	return view, markdown
}

func renderDocsMarkdownForTest(t *testing.T, view View, markdown string) string {
	t.Helper()
	markup, err := ui.RenderToString(html.Div(html.Props{Class: "docs-markdown"}, docsASTMarkdownNodes(view, markdown)...))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func TestDocsProjectLinkAloneInParagraphUnfurlsIntoCard(t *testing.T) {
	view, md := docsProjectEmbedView("Status for the team:\n\nhttps://hcm.example/workspace/app/project?board_view=default&project=proj-2&task=task-17&view=task\n\n[Board](/workspace/app/project?project=proj-2)\n\nInline [task](/workspace/app/project?project=proj-2&task=task-17) stays a chip.\n")
	markup := renderDocsMarkdownForTest(t, view, md)
	if strings.Count(markup, `class="docs-project-embed"`) != 1 || strings.Count(markup, `class="docs-project-embed is-board"`) != 1 {
		t.Fatalf("want one task card and one board card:\n%s", markup)
	}
	for _, want := range []string{"Prepare launch", "T-TASK17", "Q4 Benefits", "In progress", "Evelyn Morgan", `data-level="high"`, `data-tone="overdue"`, "3 of 4 done", `class="docs-project-task-link"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("markup missing %q:\n%s", want, markup)
		}
	}
}

func TestDocsProjectForeignOriginLinkIsNeverLinked(t *testing.T) {
	view, md := docsProjectEmbedView("https://evil.example/workspace/app/project?project=proj-2&task=task-17\n")
	markup := renderDocsMarkdownForTest(t, view, md)
	if strings.Contains(markup, "docs-project-embed") || strings.Contains(markup, `href="/workspace/app/project`) {
		t.Fatalf("foreign origin rendered as an in-app project link:\n%s", markup)
	}
}
