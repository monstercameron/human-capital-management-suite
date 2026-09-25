package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestProjectReferencesAcceptBoardAndSharedViewAddresses(t *testing.T) {
	origin := "https://hcm.example"
	cases := []struct {
		body, project, task string
	}{
		{"https://hcm.example/workspace/app/project?project=proj-7", "proj-7", ""},
		{"https://hcm.example/workspace/app/project?board_view=default&locale=en-US&nav=expanded&project=proj-7", "proj-7", ""},
		{"https://hcm.example/workspace/app/project?board_view=default&project=proj-7&task=task-19&view=task", "proj-7", "task-19"},
	}
	for _, tc := range cases {
		refs := ProjectTaskReferences(tc.body, origin)
		if len(refs) != 1 || refs[0].ProjectID != tc.project || refs[0].TaskID != tc.task {
			t.Errorf("%s: refs = %#v", tc.body, refs)
		}
	}
	for _, bad := range []string{
		"https://hcm.example/workspace/app/project?evil=1&project=proj-7",
		"https://hcm.example/workspace/app/project?project=proj-7&project=proj-8",
		"https://hcm.example/workspace/app/project?view=task&project=proj-7",
		"https://hcm.example/workspace/app/project?task=task-19",
	} {
		if got := ProjectTaskReferences(bad, origin); len(got) != 0 {
			t.Errorf("accepted %q: %#v", bad, got)
		}
	}
}

func TestProjectPreviewEmbedsRenderTaskAndBoardCards(t *testing.T) {
	origin := "https://hcm.example"
	body := "See https://hcm.example/workspace/app/project?project=proj-7&task=task-19 and the board https://hcm.example/workspace/app/project?project=proj-7"
	m := Model{EmbedOrigin: origin, ProjectTaskPreviews: map[string]ProjectTaskPreview{
		ProjectTaskPreviewKey("proj-7", "task-19"): {ProjectID: "proj-7", TaskID: "task-19", Title: "Draft announcement", Readable: true, State: "ready", ProjectName: "Q4 Benefits", Key: "T-19", Status: "In progress", StatusTone: "doing", Assignee: "Evelyn Morgan", Priority: "High", PriorityID: "TASK_PRIORITY_HIGH", Due: time.Now().AddDate(0, 0, 2).Format("2006-01-02")},
		ProjectTaskPreviewKey("proj-7", ""):        {ProjectID: "proj-7", Title: "Q4 Benefits", Readable: true, State: "ready", Done: 13, Total: 21},
	}}
	markup, err := ui.RenderToString(ui.Fragment(projectPreviewEmbeds(m, body)...))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Draft announcement", "Q4 Benefits", "T-19", "In progress", "Evelyn Morgan", `data-level="high"`, `data-tone="soon"`, "13 of 21 done", "8 open", `href="/workspace/app/project?project=proj-7&amp;task=task-19"`, `href="/workspace/app/project?project=proj-7"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("embeds missing %q:\n%s", want, markup)
		}
	}
}

func TestProjectPreviewEmbedNeverNamesARestrictedTask(t *testing.T) {
	origin := "https://hcm.example"
	body := "https://hcm.example/workspace/app/project?project=proj-7&task=task-19"
	m := Model{EmbedOrigin: origin, ProjectTaskPreviews: map[string]ProjectTaskPreview{
		ProjectTaskPreviewKey("proj-7", "task-19"): {ProjectID: "proj-7", TaskID: "task-19", Title: "Secret reorg", Readable: false, State: "restricted"},
	}}
	markup, err := ui.RenderToString(ui.Fragment(projectPreviewEmbeds(m, body)...))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "Secret reorg") || strings.Contains(markup, "href=") || !strings.Contains(markup, "is-locked") {
		t.Fatalf("restricted embed leaked or linked:\n%s", markup)
	}
	loading, _ := ui.RenderToString(ui.Fragment(projectPreviewEmbeds(Model{EmbedOrigin: origin}, body)...))
	if !strings.Contains(loading, "is-loading") || strings.Contains(loading, "href=") {
		t.Fatalf("unresolved embed should be a skeleton:\n%s", loading)
	}
}
