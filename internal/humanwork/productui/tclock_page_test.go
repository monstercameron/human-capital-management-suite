package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_TCLOCK016_UnavailableClockPageFailsClosed(t *testing.T) {
	markup, err := ui.RenderToString(ClockPage(testView(PageClock), ClockProjection{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Time clock", "Clock information is not available", "aria-labelledby=\"clock-page-title\""} {
		if !strings.Contains(markup, want) {
			t.Fatalf("unavailable clock page missing %q in %s", want, markup)
		}
	}
	if strings.Contains(markup, "Clock out") || strings.Contains(markup, "kiosk") || strings.Contains(markup, "href=") {
		t.Fatalf("unavailable page exposed an action: %s", markup)
	}
}

func TestTodo_TCLOCK017_ReadyClockPageUsesOnlyAuthorizedProjection(t *testing.T) {
	view := testView(PageClock)
	view.EffectivePermissions = []RolePagePermission{{Page: PageClock, View: true, Create: true, Update: true}}
	view.EffectiveFeatures = []RoleFeaturePermission{
		{Page: PageClock, Feature: FeatureContent, View: true},
		{Page: PageClock, Feature: "clock_actions", View: true, Create: true, Update: true},
		{Page: PageClock, Feature: "kiosk_access", View: true, Create: true, Update: true},
	}
	projection := ClockProjection{
		State:             ClockProjectionReady,
		WorkerLabel:       "Taylor",
		ScheduleLabel:     "Provided schedule",
		StatusLabel:       "Not clocked in",
		LastEventLabel:    "No event provided",
		ReceiptTraceLabel: "receipt-123",
		ClockIn:           &ActionLinkProps{Label: "Clock in", Href: "/workspace/app/time/clock/in"},
		LaunchKiosk:       &ActionLinkProps{Label: "Launch kiosk", Href: "/workspace/app/time/clock/kiosk"},
		ClockOut:          &ActionLinkProps{Label: "Clock out"},
	}
	markup, err := ui.RenderToString(ClockPage(view, projection))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Provided schedule", "Not clocked in", "No event provided", "receipt-123", `href="/workspace/app/time/clock/in"`, `href="/workspace/app/time/clock/kiosk"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("ready clock page missing %q in %s", want, markup)
		}
	}
	if strings.Contains(markup, "Clock out") {
		t.Fatalf("action without an authoritative destination rendered: %s", markup)
	}
}

func TestTodo_TCLOCK017_ClockPageLocalizesStatusLabels(t *testing.T) {
	for _, tc := range []struct {
		name, locale, title, status string
	}{
		{name: "german", locale: "de-DE", title: "Zeiterfassung", status: "Aktueller Zeiterfassungsstatus"},
		{name: "arabic", locale: "ar-SA", title: "ساعة الدوام", status: "حالة الساعة الحالية"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := testView(PageClock)
			view.Locale = ResolveProductLocale(tc.locale)
			markup, err := ui.RenderToString(ClockPage(view, ClockProjection{State: ClockProjectionReady}))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{tc.title, tc.status} {
				if !strings.Contains(markup, want) {
					t.Fatalf("locale %s missing %q in %s", tc.locale, want, markup)
				}
			}
		})
	}
}

func TestTodo_TCLOCK017_ClockPageFeaturesExposeActionBoundaries(t *testing.T) {
	features := (clockPageModuleRenderer{}).PageFeatures()
	if len(features) != 3 {
		t.Fatalf("clock feature count = %d, want 3", len(features))
	}
	for _, feature := range features {
		if !feature.View {
			t.Fatalf("feature %q is not readable", feature.ID)
		}
		if feature.ID == "clock_status" && (feature.Create || feature.Update || feature.Delete) {
			t.Fatalf("status feature has actions: %+v", feature)
		}
	}
}

func TestTodo_TCLOCK017_AuthoritativeFeatureProjectionSuppressesActions(t *testing.T) {
	view := testView(PageClock)
	view.EffectivePermissions = []RolePagePermission{{Page: PageClock, View: true, Create: true, Update: true}}
	view.EffectiveFeatures = []RoleFeaturePermission{{Page: PageClock, Feature: FeatureContent, View: true}}
	markup, err := ui.RenderToString(ClockPage(view, ClockProjection{
		State:   ClockProjectionReady,
		ClockIn: &ActionLinkProps{Label: "Clock in", Href: "/clock-in"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "Clock in") || strings.Contains(markup, "/clock-in") {
		t.Fatalf("feature projection did not suppress clock action: %s", markup)
	}
}

func TestTodo_TCLOCK017_BusyClockProjectionSuppressesActionsAndMarksBusy(t *testing.T) {
	view := testView(PageClock)
	view.EffectivePermissions = []RolePagePermission{{Page: PageClock, View: true, Create: true, Update: true}}
	view.EffectiveFeatures = []RoleFeaturePermission{
		{Page: PageClock, Feature: FeatureContent, View: true},
		{Page: PageClock, Feature: "clock_actions", View: true, Create: true, Update: true},
	}
	markup, err := ui.RenderToString(ClockPage(view, ClockProjection{
		State:    ClockProjectionReady,
		Busy:     true,
		ClockIn:  &ActionLinkProps{Label: "Clock in", Href: "/workspace/app/time/clock/in"},
		ClockOut: &ActionLinkProps{Label: "Clock out", Href: "/workspace/app/time/clock/out"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `aria-busy="true"`) {
		t.Fatalf("busy projection missing aria-busy: %s", markup)
	}
	if strings.Contains(markup, `href="/workspace/app/time/clock/in"`) || strings.Contains(markup, `href="/workspace/app/time/clock/out"`) {
		t.Fatalf("busy projection exposed clock action: %s", markup)
	}
}

func TestTodo_TCLOCK017_ErrorClockProjectionUsesAlertWithoutInventingAction(t *testing.T) {
	markup, err := ui.RenderToString(ClockPage(testView(PageClock), ClockProjection{
		State:      ClockProjectionReady,
		ErrorLabel: "Clock service rejected the request.",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `role="alert"`) || !strings.Contains(markup, "Clock service rejected the request.") {
		t.Fatalf("clock error was not announced: %s", markup)
	}
	if strings.Contains(markup, "href=") {
		t.Fatalf("error projection invented an action: %s", markup)
	}
}

func TestTodo_TCLOCK016_ClockPageIsRegisteredAndRoleScoped(t *testing.T) {
	definition, ok := LookupPage(PageClock)
	if !ok {
		t.Fatal("clock page is missing from the registry")
	}
	if definition.Route != "/workspace/app/time/clock" {
		t.Fatalf("clock route = %q", definition.Route)
	}
	if !definition.Admitted || !definition.NavigationPublished || !definition.PrimaryNav {
		t.Fatalf("clock page is not published in the menu: %+v", definition)
	}
	foundWorker := false
	for _, item := range navigationForRoles(ResolveProductLocale("en-US"), []string{"worker_self"}) {
		if item.Page == PageClock {
			foundWorker = true
		}
	}
	if !foundWorker {
		t.Fatal("worker_self cannot discover the published clock page")
	}
	for _, item := range navigationForRoles(ResolveProductLocale("en-US"), []string{"finance_partner"}) {
		if item.Page == PageClock {
			t.Fatal("finance_partner unexpectedly discovered the clock page")
		}
	}
}

func TestTodo_TCLOCK017_ClockRendererUsesViewProjection(t *testing.T) {
	view := testView(PageClock)
	view.EffectivePermissions = []RolePagePermission{{Page: PageClock, View: true, Create: true, Update: true}}
	view.EffectiveFeatures = []RoleFeaturePermission{
		{Page: PageClock, Feature: FeatureContent, View: true},
		{Page: PageClock, Feature: "clock_actions", View: true, Create: true, Update: true},
	}
	view.ClockProjection = ClockProjection{State: ClockProjectionReady, StatusLabel: "Ready from view"}
	markup, err := ui.RenderToString(clockPage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Ready from view") || strings.Contains(markup, "Clock information is not available") {
		t.Fatalf("clock renderer ignored view projection: %s", markup)
	}
}

func TestTodo_TCLOCK017_ClockCatalogHasPageAndStateCopy(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar-SA"} {
		catalog := clockMessages()
		if locale != "en-US" {
			catalog = clockTranslations(locale)
		}
		for _, key := range []string{"page.time_clock.label", "page.time_clock.title", "page.time_clock.subtitle", "clock_page.description", "clock_page.service_state", "clock_page.status_title", "clock_page.receipt_trace", "clock_page.clock_in", "clock_page.clock_out", "clock_page.action_busy", "clock_page.action_error", "clock_page.action_success"} {
			if catalog[key].Text == "" {
				t.Fatalf("%s catalog missing %s", locale, key)
			}
		}
	}
}

func TestTodo_TCLOCK017_ClockUnavailableCopySurvivesCatalogRewrite(t *testing.T) {
	for _, tc := range []struct {
		locale, want string
	}{
		{locale: "en-US", want: "Clock information is not available for this workspace yet."},
		{locale: "de-DE", want: "Zeiterfassungsinformationen sind für diesen Arbeitsbereich noch nicht verfügbar."},
		{locale: "ar-SA", want: "معلومات الساعة غير متاحة لمساحة العمل هذه بعد."},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			view := testView(PageClock)
			view.Locale = ResolveProductLocale(tc.locale)
			markup, err := ui.RenderToString(clockPage(view))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(markup, tc.want) {
				t.Fatalf("%s unavailable copy missing %q in %s", tc.locale, tc.want, markup)
			}
		})
	}
}
