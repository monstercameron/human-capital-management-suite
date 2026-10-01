package productui

import (
	"html"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func uxblind123Reasons() []ClockReason {
	return []ClockReason{ClockReasonNotEnabled, ClockReasonNoWorkerRecord, ClockReasonNoAssignment, ClockReasonNoTimeProfile, ClockReasonExempt, ClockReasonCaptureNotPunch}
}

func uxblind123View(locale string, availability *ClockAvailabilityProjection, projection ClockProjection) View {
	view := testView(PageClock)
	view.Locale = ResolveProductLocale(locale)
	view.ClockProjection = projection
	if availability != nil {
		view = ApplyClockAvailability(view, *availability)
		view.Locale = ResolveProductLocale(locale)
	}
	return view
}

func uxblind123Render(t *testing.T, view View) string {
	t.Helper()
	markup, err := ui.RenderToString(clockPage(view))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func navHasPage(items []NavItem, page PageID) bool {
	for _, item := range items {
		if item.Page == page || navHasPage(item.Children, page) {
			return true
		}
	}
	return false
}

// TestTodo_UXBLIND_123 proves the unavailable clock page states the real
// reason for every closed reason, tells only an administrator how to change it,
// and never sends anybody to a supervisor for access a supervisor cannot give.
func TestTodo_UXBLIND_123(t *testing.T) {
	for _, reason := range uxblind123Reasons() {
		for _, admin := range []bool{false, true} {
			name := string(reason) + "/worker"
			if admin {
				name = string(reason) + "/admin"
			}
			t.Run(name, func(t *testing.T) {
				availability := &ClockAvailabilityProjection{Enabled: reason != ClockReasonNotEnabled, ViewerIsAdmin: admin}
				view := uxblind123View("en-US", availability, ClockProjection{State: ClockProjectionUnavailable, Reason: reason})
				page := uxblind123Render(t, view)
				title := html.EscapeString(clockText(view.Locale, "reason."+string(reason)+".title"))
				help := html.EscapeString(clockText(view.Locale, "reason."+string(reason)+".help"))
				adminHelp := html.EscapeString(clockText(view.Locale, "reason."+string(reason)+".admin"))
				for _, want := range []string{title, help, `data-clock-reason="` + string(reason) + `"`} {
					if want == "" || !strings.Contains(page, want) {
						t.Fatalf("page does not state %q: %s", want, page)
					}
				}
				if got := strings.Contains(page, adminHelp); got != admin {
					t.Fatalf("administrator guidance shown = %v, want %v: %s", got, admin, page)
				}
				if got := strings.Contains(page, clockText(view.Locale, "admin_badge")); got != admin {
					t.Fatalf("administrator badge shown = %v, want %v", got, admin)
				}
				for _, forbidden := range []string{"supervisor", "<button", "href=", "Clock in", "Clock out"} {
					if strings.Contains(page, forbidden) {
						t.Fatalf("unavailable page must not contain %q: %s", forbidden, page)
					}
				}
				if strings.Contains(page, "Try again") {
					t.Fatalf("a final eligibility decision must not invite a retry: %s", page)
				}
				if strings.Count(page, "<h1") != 1 {
					t.Fatalf("want exactly one h1: %s", page)
				}
			})
		}
	}

	t.Run("workspace not running the clock states it whatever the worker read said", func(t *testing.T) {
		view := uxblind123View("en-US", &ClockAvailabilityProjection{Enabled: false}, ClockProjection{State: ClockProjectionUnavailable, Reason: ClockReasonExempt})
		page := uxblind123Render(t, view)
		if !strings.Contains(page, clockText(view.Locale, "reason.NOT_ENABLED.title")) || strings.Contains(page, clockText(view.Locale, "reason.EXEMPT.title")) {
			t.Fatalf("the workspace answer must outrank the worker read: %s", page)
		}
	})

	t.Run("a failed read is a retryable outage and never a verdict", func(t *testing.T) {
		view := uxblind123View("en-US", &ClockAvailabilityProjection{Enabled: true, ViewerIsAdmin: true}, ClockProjection{})
		page := uxblind123Render(t, view)
		for _, want := range []string{"Clock information is not available", "Try again in a moment", "system administrator", `data-clock-reason=""`} {
			if !strings.Contains(page, want) {
				t.Fatalf("outage page missing %q: %s", want, page)
			}
		}
		for _, forbidden := range []string{"supervisor", clockText(view.Locale, "admin_badge")} {
			if strings.Contains(page, forbidden) {
				t.Fatalf("outage page must not contain %q: %s", forbidden, page)
			}
		}
	})

	t.Run("an unknown reason is not trusted", func(t *testing.T) {
		if got := NormalizeClockReason("ASK_YOUR_SUPERVISOR"); got != ClockReasonNone {
			t.Fatalf("unknown reason normalized to %q", got)
		}
		view := uxblind123View("en-US", &ClockAvailabilityProjection{Enabled: true}, ClockProjection{State: ClockProjectionUnavailable, Reason: "ASK_YOUR_SUPERVISOR"})
		if page := uxblind123Render(t, view); !strings.Contains(page, `data-clock-reason=""`) || strings.Contains(page, "ASK_YOUR_SUPERVISOR") {
			t.Fatalf("an unknown reason reached the page: %s", page)
		}
	})

	t.Run("a ready projection is untouched by availability", func(t *testing.T) {
		view := uxblind123View("en-US", &ClockAvailabilityProjection{Enabled: true}, ClockProjection{State: ClockProjectionReady, WorkerLabel: "Ben", ScheduleLabel: "Foreman", StatusLabel: "Clocked out", LastEventLabel: "No event", Reason: ClockReasonExempt})
		page := uxblind123Render(t, view)
		if strings.Contains(page, "clock-page-unavailable") || strings.Contains(page, "data-clock-reason") {
			t.Fatalf("a ready clock rendered the unavailable state: %s", page)
		}
	})

	// One projection drives the menu, global search and the page.
	for _, tc := range []struct {
		name         string
		availability *ClockAvailabilityProjection
		wantNav      bool
		wantAdmin    bool
	}{
		{name: "enabled worker", availability: &ClockAvailabilityProjection{Enabled: true}, wantNav: true},
		{name: "enabled admin", availability: &ClockAvailabilityProjection{Enabled: true, ViewerIsAdmin: true}, wantNav: true, wantAdmin: true},
		{name: "not running admin", availability: &ClockAvailabilityProjection{ViewerIsAdmin: true}, wantNav: true, wantAdmin: true},
		{name: "not running worker", availability: &ClockAvailabilityProjection{}, wantNav: false},
		{name: "preview", availability: nil, wantNav: true},
	} {
		t.Run("navigation "+tc.name, func(t *testing.T) {
			view := uxblind123View("en-US", tc.availability, ClockProjection{})
			surface := ResolveClockSurface(view.ClockAvailability)
			if surface.NavVisible != tc.wantNav || surface.AdminHelp != tc.wantAdmin {
				t.Fatalf("surface = %+v, want nav %v admin %v", surface, tc.wantNav, tc.wantAdmin)
			}
			if got := navHasPage(view.Navigation, PageClock); got != tc.wantNav {
				t.Fatalf("navigation lists the Time clock = %v, want %v", got, tc.wantNav)
			}
			if got := authorizedNavigationPages(view)[PageClock]; tc.availability != nil && got != tc.wantNav {
				t.Fatalf("authorized destinations list the Time clock = %v, want %v", got, tc.wantNav)
			}
		})
	}
}

// TestTodo_UXBLIND_123_Localized proves every reason reads in German and
// Arabic, with the same three lines per reason as English and no English left
// behind.
func TestTodo_UXBLIND_123_Localized(t *testing.T) {
	for locale, catalog := range map[string]map[string]string{"en-US": clockCopyEN, "de-DE": clockCopyDE, "ar": clockCopyAR} {
		for _, reason := range uxblind123Reasons() {
			for _, part := range []string{"title", "help", "admin"} {
				key := "clock_page.reason." + string(reason) + "." + part
				if strings.TrimSpace(catalog[key]) == "" {
					t.Fatalf("%s catalog is missing %s", locale, key)
				}
				if locale != "en-US" && catalog[key] == clockCopyEN[key] {
					t.Fatalf("%s catalog left %s in English", locale, key)
				}
			}
		}
		for _, key := range []string{"clock_page.admin_badge", "clock_page.unavailable_help", "clock_page.since_in", "clock_page.last_punch"} {
			if strings.TrimSpace(catalog[key]) == "" || (locale != "en-US" && catalog[key] == clockCopyEN[key]) {
				t.Fatalf("%s catalog has no translated %s", locale, key)
			}
		}
		if strings.Contains(strings.ToLower(catalog["clock_page.unavailable_help"]), "supervisor") {
			t.Fatalf("%s outage copy still points at a supervisor", locale)
		}
	}
	for _, locale := range []string{"de-DE", "ar"} {
		view := uxblind123View(locale, &ClockAvailabilityProjection{Enabled: true, ViewerIsAdmin: true}, ClockProjection{State: ClockProjectionUnavailable, Reason: ClockReasonNoTimeProfile})
		page := uxblind123Render(t, view)
		if !strings.Contains(page, clockText(view.Locale, "reason.NO_TIME_PROFILE.title")) || strings.Contains(page, clockCopyEN["clock_page.reason.NO_TIME_PROFILE.title"]) {
			t.Fatalf("%s page did not render the translated reason: %s", locale, page)
		}
	}
}

func TestTodo_UXBLIND_123_BreakActionsAndEligibilityRetry(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			view := uxblind123View(locale, &ClockAvailabilityProjection{Enabled: true}, ClockProjection{})
			view.EffectivePermissions = []RolePagePermission{{Page: PageClock, View: true, Create: true, Update: true}}
			view.EffectiveFeatures = []RoleFeaturePermission{
				{Page: PageClock, Feature: "clock_actions", View: true, Create: true, Update: true},
				{Page: PageClock, Feature: FeatureContent, View: true},
			}
			for _, tc := range []struct {
				name       string
				phase      ClockPhase
				primary    string
				secondary  string
				projection ClockProjection
			}{
				{name: "working", phase: ClockPhaseIn, primary: "Clock out", secondary: clockText(view.Locale, "start_break"), projection: ClockProjection{ClockOut: &ActionLinkProps{Label: "Clock out", Href: "/workspace/app/time/clock/out"}, StartBreak: &ActionLinkProps{Label: clockText(view.Locale, "start_break"), Href: "/workspace/app/time/clock/break/start"}}},
				{name: "on break", phase: ClockPhaseBreak, primary: clockText(view.Locale, "end_break"), secondary: "Clock out", projection: ClockProjection{ClockOut: &ActionLinkProps{Label: "Clock out", Href: "/workspace/app/time/clock/out"}, EndBreak: &ActionLinkProps{Label: clockText(view.Locale, "end_break"), Href: "/workspace/app/time/clock/break/end"}}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					projection := tc.projection
					projection.State, projection.Phase = ClockProjectionReady, tc.phase
					markup := uxblind123Render(t, func() View { view.ClockProjection = projection; return view }())
					for _, want := range []string{tc.primary, tc.secondary} {
						if !strings.Contains(markup, html.EscapeString(want)) {
							t.Fatalf("%s projection lacks %q: %s", locale, want, markup)
						}
					}
				})
			}
			for _, key := range []string{"start_break", "end_break"} {
				if got := clockText(view.Locale, key); strings.Contains(got, "clock_page.") || got == "" {
					t.Fatalf("%s break action %s is not localized: %q", locale, key, got)
				}
			}
		})
	}

	view := uxblind123View("en-US", &ClockAvailabilityProjection{Enabled: true, ViewerIsAdmin: true}, ClockProjection{State: ClockProjectionUnavailable, Reason: ClockReasonExempt})
	page := uxblind123Render(t, view)
	if strings.Contains(page, "Try again") || !strings.Contains(page, clockText(view.Locale, "reason.EXEMPT.title")) || !strings.Contains(page, clockText(view.Locale, "reason.EXEMPT.admin")) {
		t.Fatalf("EXEMPT must state the final reason and admin guidance without offering retry: %s", page)
	}
}
