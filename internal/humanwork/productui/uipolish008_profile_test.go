package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UIPOLISH_008_ProfileFactStatusDensity(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	for _, tc := range []struct {
		name   string
		status WorkerFactStatus
		value  string
		badge  bool
	}{
		{name: "present", status: WorkerFactPresent, value: "HC-21059"},
		{name: "missing", status: WorkerFactMissing, value: "Not reported"},
		{name: "missing with alternate value", status: WorkerFactMissing, value: "Unavailable", badge: true},
		{name: "unknown", status: WorkerFactUnknown, value: "Not reported", badge: true},
		{name: "withheld", status: WorkerFactWithheld, value: "Restricted", badge: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			markup, err := ui.RenderToString(ui.CreateElement(ProfileFact, ProfileFactProps{
				I18nProps: I18nProps{Locale: locale}, Label: "Worker number", Value: tc.value, Status: tc.status,
			}))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(markup, `data-fact-status="`+string(tc.status)+`"`) || !strings.Contains(markup, tc.value) {
				t.Fatalf("fact lost its exact value or data state: %s", markup)
			}
			if got := strings.Contains(markup, `class="profile-fact-status `); got != tc.badge {
				t.Fatalf("fact status badge present = %t, want %t: %s", got, tc.badge, markup)
			}
			if tc.status == WorkerFactPresent && strings.Contains(markup, ">Available<") {
				t.Fatalf("ordinary present fact repeats an availability badge: %s", markup)
			}
		})
	}
}

func TestTodo_UIPOLISH_008_ProfileMissingStateLocalization(t *testing.T) {
	for _, tag := range []string{"en-US", "de-DE", "ar"} {
		t.Run(tag, func(t *testing.T) {
			locale := ResolveProductLocale(tag)
			value := locale.Text("common.not_reported")
			markup, err := ui.RenderToString(ui.CreateElement(ProfileFact, ProfileFactProps{
				I18nProps: I18nProps{Locale: locale}, Label: "Employment type", Value: value, Status: WorkerFactMissing,
			}))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(markup, `data-fact-status="MISSING"`) || !strings.Contains(markup, value) || strings.Contains(markup, `class="profile-fact-status `) {
				t.Fatalf("localized missing fact lost its meaning or repeats a badge: %s", markup)
			}
		})
	}
}

func TestTodo_UIPOLISH_008_EmptyWorkflowCountIsNotRepeated(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	empty, err := ui.RenderToString(ui.CreateElement(WorkflowLauncher, WorkflowLauncherProps{
		I18nProps: I18nProps{Locale: locale}, PersonName: "Marisol", TotalCount: 0,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(empty, locale.Text("workflow.unavailable")) || strings.Contains(empty, `class="count"`) {
		t.Fatalf("empty workflow launcher repeats a zero count or lost its explanation: %s", empty)
	}
	available, err := ui.RenderToString(ui.CreateElement(WorkflowLauncher, WorkflowLauncherProps{
		I18nProps: I18nProps{Locale: locale}, PersonName: "Isaac", TotalCount: 1,
		Workflows: []WorkflowCardProps{{Name: "Promotion", Href: "/workspace/app/journeys?mode=new"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(available, `class="count"`) || !strings.Contains(available, locale.Plural("workflow.available_count", 1)) {
		t.Fatalf("populated workflow launcher lost its result count: %s", available)
	}
}

func TestTodo_UIPOLISH_008_ProgressivelyDiscloseMultipleMissingFacts(t *testing.T) {
	for _, tag := range []string{"en-US", "de-DE", "ar"} {
		t.Run(tag, func(t *testing.T) {
			locale := ResolveProductLocale(tag)
			props := EmploymentDetailsProps{
				I18nProps: I18nProps{Locale: locale}, Title: "Employment overview", Description: "Current details",
				Facts: []ProfileFactProps{
					{Label: "Worker number", Value: "HC-21059", Status: WorkerFactPresent},
					{Label: "Employment type", Value: locale.Text("common.not_reported"), Status: WorkerFactMissing},
					{Label: "Manager", Value: "Withheld", Status: WorkerFactUnknown},
					{Label: "Special case", Value: "Unavailable", Status: WorkerFactMissing},
					{Label: "Time type", Value: locale.Text("common.not_reported"), Status: WorkerFactMissing},
				},
			}
			markup, err := ui.RenderToString(ui.CreateElement(EmploymentDetails, props))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(markup, `<details class="profile-missing-details">`) ||
				!strings.Contains(markup, locale.Plural("person.unreported_fields", 3)) ||
				!strings.Contains(markup, `data-fact-status="UNKNOWN"`) ||
				strings.Index(markup, "Worker number") > strings.Index(markup, `<details class="profile-missing-details">`) {
				t.Fatalf("missing-fact disclosure loses known/unknown facts or locale: %s", markup)
			}
			if strings.Count(markup, `data-fact-status="MISSING"`) != 3 || strings.Contains(markup, `<details class="profile-missing-details" open`) ||
				strings.Index(markup, "Special case") < strings.Index(markup, `<details class="profile-missing-details">`) {
				t.Fatalf("missing facts are not preserved behind a closed native disclosure: %s", markup)
			}
		})
	}

	locale := ResolveProductLocale("en-US")
	one, err := ui.RenderToString(ui.CreateElement(EmploymentDetails, EmploymentDetailsProps{
		I18nProps: I18nProps{Locale: locale}, Title: "Compensation", Description: "Pay details",
		Facts: []ProfileFactProps{{Label: "Base pay", Value: "$100", Status: WorkerFactPresent}, {Label: "Pay frequency", Value: locale.Text("common.not_reported"), Status: WorkerFactMissing}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(one, `profile-missing-details`) || strings.Index(one, "Base pay") > strings.Index(one, "Pay frequency") {
		t.Fatalf("a single missing fact should stay inline and in source order: %s", one)
	}
}
