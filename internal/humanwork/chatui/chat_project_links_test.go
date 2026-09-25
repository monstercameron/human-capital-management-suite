package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestProjectTaskReferencesAcceptOnlyStableTokensAndCanonicalSameOriginURLs(t *testing.T) {
	origin := "https://hcm.example"
	body := "task:task-19 https://hcm.example/workspace/app/project?project=proj-7&task=task-19"
	refs := ProjectTaskReferences(body, origin)
	if len(refs) != 2 || refs[0].TaskID != "task-19" || refs[0].ProjectID != "" || refs[1].ProjectID != "proj-7" || refs[1].TaskID != "task-19" {
		t.Fatalf("references = %#v", refs)
	}
	for _, bad := range []string{
		"task:",
		"task:../private",
		"task:bad/id",
		"task:" + strings.Repeat("x", 129),
		"https://evil.example/workspace/app/project?project=proj-7&task=task-19",
		"https://hcm.example/workspace/app/project?project=proj-7&task=../private",
		"https://hcm.example/workspace/app/project?project=proj-7&task=task-19&task=other",
		"https://hcm.example/workspace/app/project?task=task-19&project=proj-7",
		"https://hcm.example.evil/workspace/app/project?project=proj-7&task=task-19",
	} {
		if got := ProjectTaskReferences(bad, origin); len(got) != 0 {
			t.Errorf("accepted %q: %#v", bad, got)
		}
	}
}

func TestProjectTaskReferenceURLRoundTripsCanonicalRoute(t *testing.T) {
	want := "/workspace/app/project?project=proj-7&task=task-19"
	if got := ProjectTaskReferenceURL("proj-7", "task-19"); got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
	refs := ProjectTaskReferences(want, "https://hcm.example")
	if len(refs) != 1 || refs[0].ProjectID != "proj-7" || refs[0].TaskID != "task-19" {
		t.Fatalf("round trip = %#v", refs)
	}
	if got := ProjectTaskReferenceURL("../project", "task-19"); got != "" {
		t.Fatalf("invalid ID URL = %q", got)
	}
}

func TestProjectTaskReferenceBodyUsesNeutralCopyUntilAuthorizedPreview(t *testing.T) {
	body := "See task:private-9 or /workspace/app/project?project=secret-proj&task=private-9."
	neutral := ProjectTaskReferenceBody(body, "https://hcm.example", "Project task unavailable", nil)
	markup, err := ui.RenderToString(html.P(html.Props{}, neutral...))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "private-9") || strings.Contains(markup, "secret-proj") {
		t.Fatalf("unresolved task reference leaked its address: %s", markup)
	}
	if strings.Count(markup, ">Project task unavailable</span>") != 2 {
		t.Fatalf("neutral references not rendered: %s", markup)
	}

	// An unreadable server response may accidentally carry a title; it remains
	// hidden until the authoritative read says it is both ready and readable.
	previews := map[string]ProjectTaskPreview{
		ProjectTaskPreviewKey("secret-proj", "private-9"): {ProjectID: "secret-proj", TaskID: "private-9", Title: "Confidential launch", State: "ready", Readable: false},
	}
	restricted := ProjectTaskReferenceBody(body, "https://hcm.example", "Project task unavailable", previews)
	markup, err = ui.RenderToString(html.P(html.Props{}, restricted...))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "Confidential launch") || strings.Contains(markup, "private-9") || strings.Contains(markup, "secret-proj") {
		t.Fatalf("restricted preview leaked task data: %s", markup)
	}

	previews[ProjectTaskPreviewKey("secret-proj", "private-9")] = ProjectTaskPreview{ProjectID: "secret-proj", TaskID: "private-9", Title: "Launch board", State: "ready", Readable: true}
	readable := ProjectTaskReferenceBody(body, "https://hcm.example", "Project task unavailable", previews)
	markup, err = ui.RenderToString(html.P(html.Props{}, readable...))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(markup, "Launch board") != 2 || !strings.Contains(markup, `href="/workspace/app/project?project=secret-proj&amp;task=private-9"`) {
		t.Fatalf("authorized preview did not render task links: %s", markup)
	}
}

func TestProjectTaskReferenceBodyRejectsPreviewForDifferentProject(t *testing.T) {
	body := "/workspace/app/project?project=proj-7&task=task-19"
	previews := map[string]ProjectTaskPreview{ProjectTaskPreviewKey("other-project", "task-19"): {ProjectID: "other-project", TaskID: "task-19", Title: "Wrong project title", State: "ready", Readable: true}}
	nodes := ProjectTaskReferenceBody(body, "https://hcm.example", "Restricted", previews)
	markup, err := ui.RenderToString(html.P(html.Props{}, nodes...))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "Wrong project title") || strings.Contains(markup, "task-19") || !strings.Contains(markup, "Restricted") {
		t.Fatalf("preview for mismatched project was rendered: %s", markup)
	}
}

func TestMarkdownMessageBodyRendersAuthorizedProjectTaskAsInAppLink(t *testing.T) {
	projectID, taskID := "proj-7", "task-19"
	model := Model{EmbedOrigin: "https://hcm.example", ProjectTaskPreviews: map[string]ProjectTaskPreview{
		ProjectTaskPreviewKey(projectID, taskID): {ProjectID: projectID, TaskID: taskID, Title: "Review launch plan", Readable: true, State: "ready"},
	}}
	markup, err := ui.RenderToString(html.Div(html.Props{}, markdownMessageBody(model, "Please see https://hcm.example"+ProjectTaskReferenceURL(projectID, taskID)+".")...))
	if err != nil {
		t.Fatal(err)
	}
	wantHref := `href="/workspace/app/project?project=proj-7&amp;task=task-19"`
	if !strings.Contains(markup, `>Review launch plan</a>`) || !strings.Contains(markup, wantHref) || !strings.Contains(markup, `data-action="open-project-task-reference"`) {
		t.Fatalf("markdown body did not render a navigable project task link: %s", markup)
	}
}

func TestMarkdownMessageBodyKeepsTaskTokensNeutralWithoutProjectSelector(t *testing.T) {
	model := Model{EmbedOrigin: "https://hcm.example", ProjectTaskPreviews: map[string]ProjectTaskPreview{
		ProjectTaskPreviewKey("some-project", "task-19"): {ProjectID: "some-project", TaskID: "task-19", Title: "Secret title", Readable: true, State: "ready"},
	}}
	markup, err := ui.RenderToString(html.Div(html.Props{}, markdownMessageBody(model, "task:task-19")...))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "task-19") || strings.Contains(markup, "Secret title") || !strings.Contains(markup, "Project task unavailable") {
		t.Fatalf("project-less task token did not stay neutral: %s", markup)
	}
}
