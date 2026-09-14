package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func uxscan002View() View {
	view := testView(PageHome)
	view.People = []Person{
		{ID: "a", Name: "Alex", WorkerNumber: "HC-101", Role: "Engineer", PromotionAvailability: PromotionEligible},
		{ID: "b", Name: "Blair", WorkerNumber: "HC-102", Role: "Manager", PromotionAvailability: PromotionIneligible},
	}
	return view
}

func TestTodo_UXSCAN_002(t *testing.T) {
	view := uxscan002View()
	items := actionLauncherProps(view).Items
	initial := RankActionLauncherItems(items, "", actionLauncherInitialLimit)
	for _, item := range initial {
		if strings.HasPrefix(item.ID, "action:") || strings.HasPrefix(item.ID, "action-unavailable:") {
			t.Fatalf("initial launcher crowded with a worker-specific result: %+v", item)
		}
	}
	if len(initial) == 0 || initial[0].ID != SemanticActionPromoteWorker {
		t.Fatalf("initial launcher lacks the clear choose-worker action: %+v", initial)
	}
	if len(initial) > actionLauncherInitialLimit || len(initial) > 1 && initial[1].ID != actionLauncherBrowsePeople {
		t.Fatalf("initial launcher does not prioritize the employee path: %+v", initial)
	}
	results := RankActionLauncherItems(items, "Alex", actionLauncherLimit)
	found := false
	for _, item := range results {
		if strings.HasPrefix(item.ID, "action:a:") && strings.Contains(item.Label, "HC-101") {
			found = true
		}
	}
	if !found {
		t.Fatalf("specific employee search did not reveal executable action: %+v", results)
	}
}

func TestTodo_UXSCAN_002_Browser(t *testing.T) {
	items := actionLauncherProps(uxscan002View()).Items
	markup, err := ui.RenderToString(ui.CreateElement(ActionLauncher, ActionLauncherProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Items: items, InitialQuery: "Blair"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Blair · HC-102") || strings.Count(markup, "No eligible promotion role") != 1 || strings.Contains(markup, "Ask your HR administrator") {
		t.Fatalf("named ineligible result is not concise: %s", markup)
	}
}

func TestTodo_UXSCAN_002_Accessibility(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(ActionLauncher, ActionLauncherProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Items: actionLauncherProps(uxscan002View()).Items, InitialQuery: "Alex"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `aria-activedescendant="action-launcher-result-0"`) || !strings.Contains(markup, "Alex · HC-101") {
		t.Fatalf("launcher result lacks navigable identity: %s", markup)
	}
}

func TestTodo_UXSCAN_002_Security(t *testing.T) {
	view := uxscan002View()
	view.EffectivePermissions = []RolePagePermission{{Page: PageJourneys, View: true, Create: false}, {Page: PagePeople, View: true}}
	items := actionLauncherProps(view).Items
	for _, item := range items {
		if strings.HasPrefix(item.ID, "action:") && item.Href != "" {
			t.Fatalf("unauthorized launcher action has a route: %+v", item)
		}
	}
}

func TestTodo_UXSCAN_002_Performance(t *testing.T) {
	items := actionLauncherProps(uxscan002View()).Items
	start := time.Now()
	for i := 0; i < 1000; i++ {
		_ = RankActionLauncherItems(items, "Alex", actionLauncherLimit)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("local launcher ranking took %s for 1000 small queries", elapsed)
	}
}

func TestTodo_UXSCAN_002_Regression(t *testing.T) {
	items := actionLauncherProps(uxscan002View()).Items
	for _, item := range RankActionLauncherItems(items, "promotion", actionLauncherLimit) {
		if strings.HasPrefix(item.ID, "action-unavailable:") {
			t.Fatalf("generic promotion query surfaced a worker-specific refusal: %+v", item)
		}
	}
	view := uxscan002View()
	view.People[1].Name = "Amina"
	for _, item := range RankActionLauncherItems(actionLauncherProps(view).Items, "Amina", actionLauncherLimit) {
		if item.ID == actionLauncherDestinationID(PageAdmin) {
			t.Fatalf("a strong employee match pulled an unrelated administration destination: %+v", item)
		}
	}
}
