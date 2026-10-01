package productui

import (
	"strings"
	"testing"
)

func TestTodo_UXBLIND_006(t *testing.T) {
	profile := PersonProfileProps{Workflows: WorkflowLauncherProps{
		TotalCount: 2,
		Workflows: []WorkflowCardProps{
			{Name: "Promotion", Href: "/workspace/app/journeys?mode=new&worker=worker-self"},
			{Name: "Internal transfer", Href: "/workspace/app/journeys?mode=transfer&worker=worker-self"},
		},
	}}
	got := myselfProfileWithoutSelfPromotion(profile, "self promotion unavailable")
	if len(got.Workflows.Workflows) != 1 || got.Workflows.Workflows[0].Name != "Internal transfer" {
		t.Fatalf("self promotion was not removed: %+v", got.Workflows.Workflows)
	}
	if got.Workflows.TotalCount != 1 || got.Workflows.UnavailableDetail != "self promotion unavailable" {
		t.Fatalf("self promotion refusal was not projected: %+v", got.Workflows)
	}
}

func TestTodo_UXBLIND_006_Browser(t *testing.T) {
	view := testView(PageMyself)
	view.Viewer = ViewerProfile{PersonID: "worker-avery", Name: "Avery Patel", Initials: "AP"}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "mode=new&amp;worker=worker-avery") || strings.Contains(doc, "Start Promotion") {
		t.Fatal("Myself still rendered a self-promotion entry point")
	}
}

func TestTodo_UXBLIND_006_Security(t *testing.T) {
	for _, href := range []string{
		"/workspace/app/journeys?mode=new&worker=worker-self",
		"/workspace/app/journeys?mode=new&worker=worker-other",
	} {
		profile := myselfProfileWithoutSelfPromotion(PersonProfileProps{Workflows: WorkflowLauncherProps{
			Workflows: []WorkflowCardProps{{Name: "Promotion", Href: href}}, TotalCount: 1,
		}}, "not available")
		if len(profile.Workflows.Workflows) != 0 {
			t.Fatalf("promotion launcher survived self-service boundary for %q", href)
		}
	}
}
