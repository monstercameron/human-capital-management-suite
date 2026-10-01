package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func pm022Sources() []CombinedWorkSource {
	return []CombinedWorkSource{
		{ID: "project-1/task-1", Title: "Prepare launch", Kind: MyWorkProjectTask, CompletionAuthority: CompletionAuthorityProject, Freshness: "CURRENT", Href: "/workspace/app/project?project=project-1&task=task-1&selected=task-1"},
		{ID: "work-1", Title: "Approve change", Kind: MyWorkHumanWork, CompletionAuthority: CompletionAuthorityHuman, Freshness: "CURRENT", SafeNextAction: "DECIDE", Href: "/workspace/app/work?selected=work-1"},
	}
}

func TestTodo_PM_022(t *testing.T) {
	got := CombineMyWork(pm022Sources())
	if len(got) != 2 || got[0].Kind != MyWorkProjectTask || got[0].CompletionAuthority != CompletionAuthorityProject || got[0].ActionLabel != "OPEN_PROJECT_TASK" {
		t.Fatalf("project row=%+v", got)
	}
	if got[1].Kind != MyWorkHumanWork || got[1].CompletionAuthority != CompletionAuthorityHuman || got[1].ActionLabel != "DECIDE" {
		t.Fatalf("HCM row=%+v", got)
	}
	human := HumanWorkSources([]WorkItem{{ID: "work-2", Title: "Submit evidence", Href: "/workspace/app/work?selected=work-2", Freshness: "CURRENT", PermittedActions: []string{"complete"}}})
	if len(human) != 1 || human[0].SafeNextAction != "complete" || human[0].CompletionAuthority != CompletionAuthorityHuman {
		t.Fatalf("HCM server projection=%+v", human)
	}
}

func TestTodo_PM_022_Browser(t *testing.T) {
	markup, err := ui.RenderToString(CombinedMyWorkList(CombineMyWork(pm022Sources())))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-work-kind="PROJECT_TASK"`, `data-work-kind="HUMAN_WORK"`, `completion-authority="PROJECT"`, `completion-authority="HUMAN_WORK"`, "OPEN_PROJECT_TASK", "DECIDE"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("combined work markup missing %q: %s", want, markup)
		}
	}
}

func TestTodo_PM_022_Accessibility(t *testing.T) {
	markup, err := ui.RenderToString(CombinedMyWorkList(CombineMyWork(pm022Sources())))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `role="list"`) || !strings.Contains(markup, `aria-label="Combined My Work"`) || strings.Count(markup, `aria-label=`) != 3 {
		t.Fatalf("combined work accessibility markup=%s", markup)
	}
}
