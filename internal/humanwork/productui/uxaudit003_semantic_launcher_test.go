package productui

import (
	"net/url"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

func TestTodo_UXAUDIT_003_MergedUX(t *testing.T) {
	view := actionLauncherAuditView(true)
	items := authorizedActionLauncherItems(view, actionLauncherProps(view).Items)
	promote := launcherItemByID(items, actionLauncherPromoteWorker)
	if promote == nil {
		t.Fatal("authorized launcher does not offer the promotion action")
	}
	if promote.Kind != ActionLauncherAction || promote.Availability.Availability != ActionAvailable {
		t.Fatalf("promotion launcher item is not an available semantic action: %+v", *promote)
	}
	parsed, err := url.Parse(promote.Href)
	if err != nil || parsed.Path != pageHref(PagePeople) || parsed.Query().Get("eligible") != "1" {
		t.Fatalf("promotion action does not open the eligible-worker task path: %q", promote.Href)
	}
	if promote.Label == view.Locale.Text("page.people.label") || promote.Label == view.Locale.Text("page.journeys.label") {
		t.Fatalf("promotion action duplicates a navigation label: %q", promote.Label)
	}

	readOnly := actionLauncherAuditView(false)
	readOnlyItems := authorizedActionLauncherItems(readOnly, actionLauncherProps(readOnly).Items)
	promote = launcherItemByID(readOnlyItems, actionLauncherPromoteWorker)
	if promote == nil || promote.Availability.Availability != ActionUnavailable || strings.TrimSpace(promote.Availability.Reason) == "" {
		t.Fatalf("safe read-only promotion action is not explained as unavailable: %+v", promote)
	}

	destinationsOnly := ApplyPagePermissions(testView(PageHome), []RolePagePermission{
		{Page: PageHome, View: true}, {Page: PagePeople, View: true},
	})
	destinationsOnly.LauncherActions = nil
	destinationItems := authorizedActionLauncherItems(destinationsOnly, actionLauncherProps(destinationsOnly).Items)
	if launcherItemByID(destinationItems, actionLauncherPromoteWorker) != nil {
		t.Fatal("launcher disclosed promotion without an authorized workflow destination")
	}
	trigger, _, _, _ := actionLauncherCopyKeys(destinationItems)
	if trigger != "action_launcher.navigation_trigger" {
		t.Fatalf("destination-only launcher kept an action label: %q", trigger)
	}
}

func TestTodo_UXAUDIT_003_Browser_MergedUX(t *testing.T) {
	view := actionLauncherAuditView(true)
	items := authorizedActionLauncherItems(view, actionLauncherProps(view).Items)
	doc := renderActionLauncherAudit(t, view, items, "promote")
	root := parseActionLauncherAudit(t, doc)
	dialog := findElementByID(root, "action-launcher-dialog")
	if dialog == nil || hasAttr(dialog, "hidden") || attr(dialog, "role") != "dialog" {
		t.Fatal("opened launcher is not a visible non-modal dialog")
	}
	input := findElementByID(root, "action-launcher-input")
	if input == nil || attr(input, "role") != "combobox" || attr(input, "aria-expanded") != "true" || attr(input, "aria-activedescendant") == "" {
		t.Fatal("launcher search does not expose its visible option relationship")
	}
	result := findElementByID(root, "action-launcher-result-0")
	if result == nil || result.Data != "a" || attr(result, "role") != "option" || attr(result, "href") == "" || attr(result, "tabindex") != "-1" {
		t.Fatalf("promotion result is not a software-routed option: %#v", result)
	}
}

func TestTodo_UXAUDIT_003_Accessibility_MergedUX(t *testing.T) {
	view := actionLauncherAuditView(false)
	items := authorizedActionLauncherItems(view, actionLauncherProps(view).Items)
	doc := renderActionLauncherAudit(t, view, items, "promote")
	root := parseActionLauncherAudit(t, doc)
	trigger := findElementByID(root, "action-launcher-trigger")
	if trigger == nil || attr(trigger, "aria-haspopup") != "dialog" || attr(trigger, "aria-controls") != "action-launcher-dialog" {
		t.Fatal("launcher trigger does not name and control its dialog")
	}
	unavailable := findElementByID(root, "action-launcher-result-0")
	if unavailable == nil || unavailable.Data != "button" || !hasAttr(unavailable, "disabled") || attr(unavailable, "aria-disabled") != "true" {
		t.Fatal("unavailable action is not conveyed as a disabled native control")
	}
	if strings.TrimSpace(textContent(unavailable)) == "" || !strings.Contains(textContent(unavailable), view.Locale.Text("action_launcher.promote_unavailable")) {
		t.Fatal("unavailable action has no accessible corrective explanation")
	}
	recoveryID := attr(unavailable, "aria-describedby")
	recovery := findElementByID(root, recoveryID)
	if recoveryID == "" || recovery == nil || !strings.Contains(textContent(recovery), "Learn about access") {
		t.Fatal("unavailable action is not associated with its server-projected recovery")
	}
	css := actionLauncherStylesheet()
	for _, want := range []string{"[aria-disabled=true]", "prefers-reduced-motion:reduce", "forced-colors:active"} {
		if !strings.Contains(css, want) {
			t.Fatalf("launcher styles omit accessibility state %q", want)
		}
	}
}

func TestTodo_UXAUDIT_003_I18n_MergedUX(t *testing.T) {
	for locale, want := range map[string]struct {
		promotion string
		roles     string
		adminText string
	}{
		"de-DE": {promotion: "Mitarbeitende befördern", roles: "Rollen & Zugriff", adminText: "Zugriffsrechte"},
		"ar":    {promotion: "ترقية موظف", roles: "الأدوار والوصول", adminText: "صلاحيات الوصول"},
	} {
		view := testView(PageHome)
		view.Locale = ResolveProductLocale(locale)
		view = ApplyPagePermissions(view, []RolePagePermission{
			{Page: PageHome, View: true}, {Page: PagePeople, View: true}, {Page: PageJourneys, View: true, Create: true},
			{Page: PageAdmin, View: true}, {Page: PageRoles, View: true}, {Page: PageSettings, View: true},
		})
		view.Page = PageSettings
		view.LauncherActions = []LauncherActionProjection{{ID: SemanticActionPromoteWorker, State: ActionState{Availability: ActionAvailable}}}
		items := authorizedActionLauncherItems(view, actionLauncherProps(view).Items)
		promotion := launcherItemByID(items, actionLauncherPromoteWorker)
		roles := launcherItemByID(items, actionLauncherDestinationID(PageRoles))
		admin := launcherItemByID(items, actionLauncherDestinationID(PageAdmin))
		home := launcherItemByID(items, actionLauncherDestinationID(PageHome))
		if promotion == nil || promotion.Label != want.promotion {
			t.Fatalf("locale %s promotion label = %+v, want %q", locale, promotion, want.promotion)
		}
		if roles == nil || roles.Label != want.roles {
			t.Fatalf("locale %s roles label = %+v, want %q", locale, roles, want.roles)
		}
		if admin == nil || !strings.Contains(admin.Description, want.adminText) {
			t.Fatalf("locale %s admin description = %+v, want localized text containing %q", locale, admin, want.adminText)
		}
		if locale == "ar" && (home == nil || strings.Contains(home.Description, "Review live requests")) {
			t.Fatalf("locale %s home destination retained English fallback: %+v", locale, home)
		}
	}
}

func TestTodo_UXAUDIT_003_Security_MergedUX(t *testing.T) {
	view := actionLauncherAuditView(true)
	for _, mutate := range []func(*ActionLauncherItem){
		func(item *ActionLauncherItem) { item.Action = "view" },
		func(item *ActionLauncherItem) { item.Kind = ActionLauncherDestination },
		func(item *ActionLauncherItem) { item.Page = PageJourneys },
		func(item *ActionLauncherItem) { item.Href = "https://example.invalid/promote" },
		func(item *ActionLauncherItem) { item.Href = "/workspace/app/people?eligible=1&token=secret" },
		func(item *ActionLauncherItem) { item.ID = "forged-promotion" },
	} {
		item := actionLauncherProps(view).Items[0]
		mutate(&item)
		if got := authorizedActionLauncherItems(view, []ActionLauncherItem{item}); len(got) != 0 {
			t.Fatalf("forged launcher action survived the bounded registry: %+v", got)
		}
	}

	readOnly := actionLauncherAuditView(false)
	items := authorizedActionLauncherItems(readOnly, actionLauncherProps(readOnly).Items)
	doc := renderActionLauncherAudit(t, readOnly, items, "promote")
	root := parseActionLauncherAudit(t, doc)
	promote := findElementByID(root, "action-launcher-result-0")
	if promote == nil || promote.Data == "a" || attr(promote, "href") != "" {
		t.Fatal("unavailable promotion action retained an executable eligible-worker link")
	}
	// Page CRUD and presentation workflow metadata do not imply a semantic
	// launcher action. Absence, duplicates, and unknown verdicts all fail closed.
	for name, projection := range map[string][]LauncherActionProjection{
		"missing":   nil,
		"duplicate": {{ID: SemanticActionPromoteWorker, State: ActionState{Availability: ActionAvailable}}, {ID: SemanticActionPromoteWorker, State: ActionState{Availability: ActionAvailable}}},
		"blank":     {{ID: SemanticActionPromoteWorker}},
		"unknown":   {{ID: SemanticActionPromoteWorker, State: ActionState{Availability: ActionAvailability("future-state")}}},
		"hidden":    {{ID: SemanticActionPromoteWorker, State: ActionState{Availability: ActionHidden}}},
	} {
		candidate := actionLauncherAuditView(true)
		candidate.LauncherActions = projection
		if got := launcherItemByID(authorizedActionLauncherItems(candidate, actionLauncherProps(candidate).Items), actionLauncherPromoteWorker); got != nil {
			t.Fatalf("%s semantic projection disclosed promotion: %+v", name, *got)
		}
	}
	hiddenDoc := renderActionLauncherAudit(t, readOnly, []ActionLauncherItem{{
		ID: "hidden", Label: "Confidential action", Availability: ActionState{Availability: ActionHidden},
	}}, "")
	if strings.Contains(hiddenDoc, "Confidential action") {
		t.Fatal("launcher component disclosed an explicitly hidden action")
	}
	unknownDoc := renderActionLauncherAudit(t, readOnly, []ActionLauncherItem{{
		ID: "unknown", Label: "Unknown action", Availability: ActionState{Availability: ActionAvailability("future-state")},
	}}, "")
	if strings.Contains(unknownDoc, "Unknown action") {
		t.Fatal("launcher component failed open on an unknown availability state")
	}
}

func TestTodo_UXAUDIT_003_PhoneLauncherEscapesTheHeaderScrollport_MergedUX(t *testing.T) {
	css := Stylesheet()
	for _, contract := range []string{
		`@media (max-width:760px){.action-launcher-dialog{inset-block-start:68px;inset-inline:12px;max-height:calc(100dvh - 80px);position:fixed;width:auto;}`,
		`@media (max-width:430px){.history-navigation{display:none;}`,
		`@media (max-width:430px){.topbar>.header-navigation-tools{overflow:visible;}`,
		`@media (max-width:430px){.topbar,.app-shell.nav-collapsed .topbar{gap:6px;grid-template-columns:82px minmax(0,1fr) auto auto auto;padding-inline:8px;}`,
		`@media (max-width:430px){.topbar>.locale-menu{display:none;}`,
		`@media (max-width:430px){.header-navigation-tools>.global-search{flex:0 0 44px;padding:0;width:44px;}`,
		`@media (max-width:430px){.header-navigation-tools>.global-search:focus-within{inset-block-start:68px;inset-inline:12px;position:fixed;width:auto;z-index:90;}`,
	} {
		if !strings.Contains(css, contract) {
			t.Fatalf("phone launcher contract missing %q", contract)
		}
	}
}

func TestTodo_UXAUDIT_003_Regression_MergedUX(t *testing.T) {
	view := actionLauncherAuditView(true)
	items := authorizedActionLauncherItems(view, actionLauncherProps(view).Items)
	results := RankActionLauncherItems(items, "promte", actionLauncherLimit)
	if len(results) == 0 || results[0].ID != actionLauncherPromoteWorker {
		t.Fatalf("typo-tolerant ranking lost the promotion action: %+v", results)
	}
	results = RankActionLauncherItems(items, "directory", actionLauncherLimit)
	if len(results) == 0 || results[0].ID != actionLauncherBrowsePeople {
		t.Fatalf("destination search did not rank the employee directory: %+v", results)
	}
	first := authorizedActionLauncherItems(view, actionLauncherProps(view).Items)
	second := authorizedActionLauncherItems(view, actionLauncherProps(view).Items)
	if len(first) != len(second) {
		t.Fatal("launcher inventory changed across equivalent projections")
	}
	for index := range first {
		if first[index].ID != second[index].ID || first[index].Href != second[index].Href || first[index].Availability.Availability != second[index].Availability.Availability || first[index].Availability.Reason != second[index].Availability.Reason {
			t.Fatalf("launcher inventory is nondeterministic at %d: %+v != %+v", index, first[index], second[index])
		}
	}
}

func actionLauncherAuditView(canCreate bool) View {
	view := testView(PageHome)
	view = ApplyPagePermissions(view, []RolePagePermission{
		{Page: PageHome, View: true},
		{Page: PagePeople, View: true},
		{Page: PageJourneys, View: true, Create: canCreate},
		{Page: PageHelp, View: true},
	})
	if canCreate {
		view.LauncherActions = []LauncherActionProjection{{ID: SemanticActionPromoteWorker, State: ActionState{Availability: ActionAvailable}}}
	} else {
		view.LauncherActions = []LauncherActionProjection{{
			ID: SemanticActionPromoteWorker,
			State: ActionState{
				Availability: ActionUnavailable,
				Reason:       view.Locale.Text("action_launcher.promote_unavailable"),
				Recovery:     ActionLinkProps{Label: "Learn about access", Href: statefulHref(view, PageHelp)},
			},
		}}
	}
	return view
}

func launcherItemByID(items []ActionLauncherItem, id string) *ActionLauncherItem {
	for index := range items {
		if items[index].ID == id {
			return &items[index]
		}
	}
	return nil
}

func renderActionLauncherAudit(t *testing.T, view View, items []ActionLauncherItem, query string) string {
	t.Helper()
	doc, err := ui.RenderToString(ui.CreateElement(ActionLauncher, ActionLauncherProps{
		I18nProps: I18nProps{Locale: view.Locale}, Items: items, InitialQuery: query,
	}))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func parseActionLauncherAudit(t *testing.T, doc string) *xhtml.Node {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
