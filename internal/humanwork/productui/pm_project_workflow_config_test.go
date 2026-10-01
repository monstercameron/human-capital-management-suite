package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
	projectpresentation "github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
)

func renderPM032(t *testing.T, props ProjectWorkflowConfigProps) string {
	t.Helper()
	markup, err := ui.RenderToString(ProjectWorkflowConfiguration(props))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func pm032Config() projectworkflow.Config {
	return projectworkflow.Config{
		Statuses:  []projectworkflow.Status{{ID: "todo", Name: "To do", Category: projectworkflow.CategoryNotStarted}, {ID: "done", Name: "Done", Category: projectworkflow.CategoryDone}},
		Fields:    []projectworkflow.Field{{ID: "owner", Name: "Owner", Type: projectworkflow.FieldPerson, Classification: "PROJECT"}},
		Columns:   []projectworkflow.Column{{ID: "open", Name: "Open", StatusIDs: []string{"todo"}}, {ID: "closed", Name: "Closed", StatusIDs: []string{"done"}}},
		TaskTypes: []projectworkflow.TaskType{{ID: "task", Name: "Task", InitialStatus: "todo"}},
	}
}

func TestTodo_PM_032(t *testing.T) {
	markup := renderPM032(t, ProjectWorkflowConfigProps{
		Locale: ResolveProductLocale("en-US"), ProjectID: "p-1", State: ProjectWorkflowConfigReady, DraftRevision: 7, PublishedRevision: 4, Draft: pm032Config(), CanConfigure: true,
		OnSave: func(ProjectWorkflowSaveRequest) {}, OnPreview: func(ProjectWorkflowPreviewRequest) {},
		Preview:    ProjectWorkflowPreview{DraftRevision: 7, Valid: true, ReviewedDigest: "sha256:draft", MigrationPlanDigest: "sha256:plan", AffectedTaskCount: 2, AffectedTaskIDs: []string{"task-1", "task-2"}, Diff: []ProjectWorkflowDiff{{Path: "statuses[1].name", Before: "Closed", After: "Complete"}}},
		CanPublish: true, OnPublish: func(ProjectWorkflowPublishRequest) {},
	})
	for _, want := range []string{"Configure project workflow", "Draft 7", "Published 4", "Migration preview", "Exact changes", "statuses[1].name", "task-1", "task-2", `data-projectui-action="preview-workflow-draft"`, `data-projectui-action="publish-workflow-draft"`, `data-reviewed-digest="sha256:draft"`, `data-migration-plan-digest="sha256:plan"`, "Publish reviewed version"} {
		if !strings.Contains(markup, want) {
			t.Errorf("PM-032 markup missing %q: %s", want, markup)
		}
	}
}

func TestTodo_PM_032_Browser(t *testing.T) {
	markup := renderPM032(t, ProjectWorkflowConfigProps{Locale: ResolveProductLocale("en-US"), State: ProjectWorkflowConfigReady, Draft: pm032Config(), CanConfigure: true, OnPreview: func(ProjectWorkflowPreviewRequest) {}})
	if !strings.Contains(markup, `disabled`) {
		t.Fatalf("unreviewed draft offered publication: %s", markup)
	}
	if strings.Contains(markup, `data-projectui-action="publish-workflow-draft"`) {
		t.Fatalf("unreviewed draft exposed a publish command: %s", markup)
	}
}

func TestTodo_PM_032_Accessibility(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup := renderPM032(t, ProjectWorkflowConfigProps{Locale: ResolveProductLocale(locale), State: ProjectWorkflowConfigReady, ProjectID: "p-1", Draft: pm032Config(), Preview: ProjectWorkflowPreview{Valid: false, Diagnostics: []ProjectWorkflowDiagnostic{{Code: "REQUIRED", Path: "statuses[1]", Message: "A status is required."}}}})
		for _, want := range []string{`role="region"`, `aria-label=`, `project-workflow-status-0`, `project-workflow-column-0`, `role="alert"`, "REQUIRED"} {
			if !strings.Contains(markup, want) {
				t.Errorf("%s workflow config missing %q: %s", locale, want, markup)
			}
		}
	}
}

func TestTodo_PM_032_Fault(t *testing.T) {
	markup := renderPM032(t, ProjectWorkflowConfigProps{Locale: ResolveProductLocale("en-US"), State: ProjectWorkflowConfigFailed, ProjectID: "p-1"})
	if !strings.Contains(markup, `role="alert"`) || !strings.Contains(markup, "could not be loaded") || strings.Contains(markup, "Save draft") || strings.Contains(markup, "Publish") {
		t.Fatalf("failed workflow configuration exposed controls: %s", markup)
	}
}

func TestTodo_PM_032_Recovery(t *testing.T) {
	markup := renderPM032(t, ProjectWorkflowConfigProps{Locale: ResolveProductLocale("en-US"), State: ProjectWorkflowConfigReady, DraftRevision: 9, Draft: pm032Config(), Preview: ProjectWorkflowPreview{DraftRevision: 8, Valid: true, ReviewedDigest: "old", MigrationPlanDigest: "old-plan"}, CanPublish: true, OnPublish: func(ProjectWorkflowPublishRequest) {}})
	if !strings.Contains(markup, `disabled`) || strings.Contains(markup, `data-projectui-action="publish-workflow-draft"`) {
		t.Fatalf("stale preview was accepted for publication: %s", markup)
	}
}

func TestProjectWorkflowConfigurationStylesAreResponsiveAndReducedMotionSafe(t *testing.T) {
	styles := projectWorkflowConfigurationStylesheet()
	for _, want := range []string{"max-width:40rem", "prefers-reduced-motion", "grid-template-columns", "var(--hcm-radius-control)"} {
		if !strings.Contains(styles, want) {
			t.Errorf("styles missing %q", want)
		}
	}
}

func TestTodo_PM_029(t *testing.T) {
	markup := renderProjectTest(t, projectsPage(View{Title: "Projects", ProjectsState: ProjectProjectionReady, Projects: []ProjectSummaryProjection{
		{ID: "public-1", Name: "Launch", Status: "SUSPENDED", OwnerID: "owner-1"},
	}}))
	for _, want := range []string{"Launch", "Paused", "Owner", "owner-1", "Open Launch"} {
		if !strings.Contains(markup, want) {
			t.Errorf("project shell missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "private-project") || !strings.Contains(markup, "aria-label=\"Authorized projects\"") {
		t.Fatalf("project shell exposed an unauthorized name or lost its accessible list: %s", markup)
	}
	empty := renderProjectTest(t, projectsPage(View{Title: "Projects", ProjectsState: ProjectProjectionReady}))
	if !strings.Contains(empty, "No projects are available") {
		t.Fatalf("authorized empty project state missing: %s", empty)
	}
}

func TestTodo_PM_029_Accessibility(t *testing.T) {
	markup := renderProjectTest(t, projectsPage(View{Title: "Projects", ProjectsState: ProjectProjectionReady, Projects: []ProjectSummaryProjection{{ID: "p-1", Name: "Launch", Status: "ACTIVE", OwnerID: "owner-1"}}}))
	for _, want := range []string{`role="region"`, `role="list"`, `aria-label="Authorized projects"`, `href="/workspace/app/project?project=p-1"`, "Open Launch"} {
		if !strings.Contains(markup, want) {
			t.Errorf("project shell accessibility missing %q: %s", want, markup)
		}
	}
}

func TestTodo_PM_030_Browser(t *testing.T) {
	markup := renderProjectTest(t, projectpresentation.Board(projectpresentation.Model{
		Title: "Launch", Columns: []projectpresentation.Column{{ID: "todo", Label: "To do", Statuses: []projectpresentation.Status{{ID: "todo", Label: "To do"}}}},
		Cards: []projectpresentation.Card{{ID: "task-1", Title: "Prepare", StatusID: "todo", StatusOptions: []projectpresentation.Status{{ID: "todo", Label: "To do"}}, CanMoveStatus: true, Pending: true}},
		Page:  projectpresentation.Page{Number: 1, Total: 100, HasNext: true, NextHref: "/board?page=2"},
	}))
	for _, want := range []string{"Prepare", "Saving", "Next page", `data-projectui-action="move-status"`, " disabled"} {
		if !strings.Contains(markup, want) {
			t.Errorf("board browser projection missing %q: %s", want, markup)
		}
	}
}

func TestTodo_PM_031_Browser(t *testing.T) {
	markup := renderProjectTest(t, projectpresentation.TaskDetail(projectpresentation.DetailModel{Title: "Task", Status: "Active", Assignee: "Owner", DueDate: "2026-10-03", Comments: []projectpresentation.Comment{{Author: "Owner", Body: "Comment"}}, Activity: []projectpresentation.Activity{{Actor: "Owner", Label: "Moved"}}, Links: []projectpresentation.Reference{{Kind: "Docs", State: projectpresentation.ReferenceReady, Title: "Guide", Href: "/docs/1"}}}))
	for _, want := range []string{"Task details", "Owner", "Oct 3", "Comment", "Moved", "Guide"} {
		if !strings.Contains(markup, want) {
			t.Errorf("task detail browser projection missing %q: %s", want, markup)
		}
	}
}

func TestTodo_PM_031_Security(t *testing.T) {
	markup := renderProjectTest(t, projectpresentation.TaskDetail(projectpresentation.DetailModel{Title: "Task", Links: []projectpresentation.Reference{{Kind: "Docs", State: projectpresentation.ReferenceRestricted, Title: "Private title", Href: "/private"}}}))
	if strings.Contains(markup, "Private title") || strings.Contains(markup, "/private") || !strings.Contains(markup, "Access restricted") {
		t.Fatalf("restricted linked title was disclosed: %s", markup)
	}
}
