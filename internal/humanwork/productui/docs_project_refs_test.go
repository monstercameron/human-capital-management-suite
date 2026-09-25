package productui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestDocsProjectTaskReferenceParse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  DocsProjectTaskReference
		ok    bool
	}{
		{name: "typed ID", input: "task:task-17", want: DocsProjectTaskReference{TaskID: "task-17"}, ok: true},
		{name: "canonical route", input: "/workspace/app/project?project=proj-2&task=task-17", want: DocsProjectTaskReference{ProjectID: "proj-2", TaskID: "task-17"}, ok: true},
		{name: "canonical route preserves supported view selectors", input: "/workspace/app/project?project=proj-2&task=task-17&view=list", want: DocsProjectTaskReference{ProjectID: "proj-2", TaskID: "task-17"}, ok: true},
		{name: "canonical query order is normalized", input: "/workspace/app/project?task=task-17&project=proj-2", want: DocsProjectTaskReference{ProjectID: "proj-2", TaskID: "task-17"}, ok: true},
		{name: "duplicate parameter", input: "/workspace/app/project?project=proj-2&task=task-17&task=task-18"},
		{name: "absolute URL", input: "https://evil.invalid/workspace/app/project?project=proj-2&task=task-17"},
		{name: "other path", input: "/workspace/app/projects?project=proj-2&task=task-17"},
		{name: "unsafe typed ID", input: "task:../task-17"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := ParseDocsProjectTaskReference(test.input)
			if ok != test.ok || ok && got != test.want {
				t.Fatalf("ParseDocsProjectTaskReference(%q) = %+v, %v; want %+v, %v", test.input, got, ok, test.want, test.ok)
			}
		})
	}
	if got := DocsProjectTaskHref(DocsProjectTaskReference{ProjectID: "proj-2", TaskID: "task-17"}); got != "/workspace/app/project?project=proj-2&task=task-17" {
		t.Fatalf("canonical href = %q", got)
	}
}

func TestDocsProjectTaskReferencesExtractDistinctMarkdownLinks(t *testing.T) {
	markdown := "[one](task:task-1) [two](/workspace/app/project?project=proj-a&task=task-2) [again](/workspace/app/project?project=proj-a&task=task-2) `[/workspace/app/project?project=proj-a&task=hidden]`"
	want := []DocsProjectTaskReference{{TaskID: "task-1"}, {ProjectID: "proj-a", TaskID: "task-2"}}
	if got := DocsProjectTaskReferences(markdown); !reflect.DeepEqual(got, want) {
		t.Fatalf("Markdown task references = %+v, want %+v", got, want)
	}
}

func TestDocsProjectTaskPreviewIsAuthorizedAndNeutral(t *testing.T) {
	ref := DocsProjectTaskReference{TaskID: "task-17"}
	preview := DocsProjectTaskPreview{ProjectID: "proj-2", TaskID: "task-17", Title: "Prepare launch", Status: "In progress", Authorized: true}
	ready, err := ui.RenderToString(html.Div(html.Props{Class: "docs-markdown"}, docsProjectTaskLinkNode("en-US", ref, preview, func(string) {})))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`href="/workspace/app/project?project=proj-2&amp;task=task-17"`, `data-docs-project-task=`, `Prepare launch`, `In progress`, `aria-label="Open project task: Prepare launch (In progress)"`} {
		if !strings.Contains(ready, want) {
			t.Errorf("authorized link omitted %q: %s", want, ready)
		}
	}

	for _, caseRef := range []struct {
		ref     DocsProjectTaskReference
		preview DocsProjectTaskPreview
	}{
		{ref: ref, preview: DocsProjectTaskPreview{ProjectID: "proj-2", TaskID: "task-17", Title: "Secret launch", Status: "Blocked"}},
		{ref: DocsProjectTaskReference{ProjectID: "proj-expected", TaskID: "task-17"}, preview: DocsProjectTaskPreview{ProjectID: "proj-other", TaskID: "task-17", Title: "Secret launch", Status: "Blocked", Authorized: true}},
	} {
		out, err := ui.RenderToString(docsProjectTaskLinkNode("en-US", caseRef.ref, caseRef.preview, nil))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "Project task unavailable") || strings.Contains(out, "Secret launch") || strings.Contains(out, "Blocked") || strings.Contains(out, "proj-") || strings.Contains(out, "href=") {
			t.Errorf("restricted preview disclosed details: %s", out)
		}
	}
}

func TestDocsProjectTaskCopySupportsLocales(t *testing.T) {
	if got := docsProjectTaskCopy("de-DE").unavailable; got != "Projektaufgabe nicht verfügbar" {
		t.Fatalf("German restricted copy = %q", got)
	}
	if got := docsProjectTaskCopy("ar").unavailable; got != "مهمة المشروع غير متاحة" {
		t.Fatalf("Arabic restricted copy = %q", got)
	}
}

func TestDocsProjectTaskMarkdownRendersAuthorizedTypedAndURLLinks(t *testing.T) {
	ref := DocsProjectTaskReference{ProjectID: "proj-2", TaskID: "task-17"}
	view := ApplyLocale(NewView(PageDocs, "tenant-a", "reader-a", "scope-a"), ResolveProductLocale("en-US"))
	view.Document = &DocumentDetail{ProjectTasks: []DocsProjectTaskPreview{{ProjectID: ref.ProjectID, TaskID: ref.TaskID, Title: "Prepare launch", Status: "In progress", Authorized: true}}}
	var navigated string
	view.Navigate = func(href string) { navigated = href }
	markdown := "[task link](task:task-17) [URL link](/workspace/app/project?project=proj-2&task=task-17) task:task-17."
	out, err := ui.RenderToString(html.Div(html.Props{Class: "docs-markdown"}, docsASTMarkdownNodes(view, markdown)...))
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(out, `href="/workspace/app/project?project=proj-2&amp;task=task-17"`); count != 3 {
		t.Fatalf("got %d in-app project task links, want 3: %s", count, out)
	}
	if count := strings.Count(out, "Prepare launch"); count != 6 {
		t.Fatalf("authorized preview title/label missing from three links: %s", out)
	}
	if !strings.Contains(out, "In progress") || strings.Contains(out, "task link") || strings.Contains(out, "URL link") {
		t.Fatalf("task links did not use authorized preview presentation: %s", out)
	}
	if got := DocsProjectTaskHref(ref); got != "/workspace/app/project?project=proj-2&task=task-17" || navigated != "" {
		t.Fatalf("route contract = %q, unexpected eager navigation %q", got, navigated)
	}
}

func TestDocsProjectTaskMarkdownRevocationRemovesPreview(t *testing.T) {
	view := ApplyLocale(NewView(PageDocs, "tenant-a", "reader-a", "scope-a"), ResolveProductLocale("en-US"))
	view.Document = &DocumentDetail{ProjectTasks: []DocsProjectTaskPreview{{ProjectID: "proj-2", TaskID: "task-17", Title: "Private title", Status: "Blocked", Authorized: true}}}
	first, err := ui.RenderToString(html.Div(html.Props{}, docsASTMarkdownNodes(view, "[open](task:task-17)")...))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, "Private title") || !strings.Contains(first, "Blocked") {
		t.Fatalf("authorized preview did not render: %s", first)
	}

	// A later authorized read after revocation replaces the task projection.
	view.Document.ProjectTasks[0] = DocsProjectTaskPreview{ProjectID: "proj-2", TaskID: "task-17"}
	second, err := ui.RenderToString(html.Div(html.Props{}, docsASTMarkdownNodes(view, "[open](task:task-17)")...))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(second, "Project task unavailable") || strings.Contains(second, "Private title") || strings.Contains(second, "Blocked") || strings.Contains(second, "proj-2") || strings.Contains(second, "href=") {
		t.Fatalf("revoked preview remained visible: %s", second)
	}
}
