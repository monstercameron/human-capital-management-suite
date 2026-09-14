package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UIPOLISH_002_HomeContinuitySharesPrimaryGridColumn(t *testing.T) {
	markup, err := ui.RenderToString(HomePage(HomePageProps{
		ShowWork:     true,
		Work:         WorkCollectionProps{Title: "Needs action"},
		ShowOverview: true,
		Overview:     SummaryCardProps{Title: "Overview", Facts: []FactProps{{Label: "Workers", Value: "4"}}},
		QuickStart:   QuickActionsProps{Title: "Start", Actions: []ActionLinkProps{{Label: "Open", Href: "/open"}}},
		ShowDrafts:   true,
		Drafts:       WorkCollectionProps{Title: "Recent work", Rows: []WorkRowProps{{Title: "Draft"}}},
		ShowPeople:   true,
		RecentPeople: RecentPeopleProps{Title: "Recent people", Items: []RecentPerson{{Name: "Avery", Initials: "A"}}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	grid := strings.Index(markup, `class="home-grid"`)
	if grid < 0 {
		t.Fatalf("home grid missing: %s", markup)
	}
	primary := strings.Index(markup[grid:], `class="home-primary-rail side-stack"`)
	supporting := strings.Index(markup[grid:], `class="home-supporting-rail side-stack"`)
	if primary < 0 || supporting < 0 || primary >= supporting {
		t.Fatalf("home rails missing or out of order: %s", markup)
	}
	for _, title := range []string{"Needs action", "Start", "Recent work", "Overview", "Recent people"} {
		if strings.Count(markup, ">"+title+"</h2>") != 1 {
			t.Fatalf("home section %q is not rendered exactly once: %s", title, markup)
		}
	}
	if !strings.Contains(markup[grid:], `class="home-primary-rail side-stack"`) || !strings.Contains(markup[grid:], `class="home-supporting-rail side-stack"`) {
		t.Fatalf("home semantic rails missing: %s", markup)
	}
	if strings.Contains(markup, `grid-row:span`) || strings.Contains(markup, `home-secondary`) {
		t.Fatalf("home layout retained the row-span or legacy continuity hack: %s", markup)
	}
}
