package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func uxblindAEWorkItem() WorkItem {
	return WorkItem{
		ID: "intent-uxblind-ae", Initials: "LT", Title: "Beförderungsantrag", Person: "Linh Tran", Summary: "P4 → P5",
		CurrentBase: testMoney("132000", "USD"), ProposedBase: testMoney("160000", "USD"),
		CurrentPayBasis: "ANNUAL_SALARY", ProposedPayBasis: "ANNUAL_SALARY", NextStep: "start_approval",
	}
}

func TestTodo_UXBLIND_117(t *testing.T) {
	view := testView(PageWork)
	view.Locale = ResolveProductLocale("de-DE")
	item := uxblindAEWorkItem()
	props := workPreviewProps(view, item)
	markup, err := ui.RenderToString(WorkPreview(props))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		view.Locale.FormatMoneyWithUnit("132000", "USD", "annual", 2),
		view.Locale.FormatMoneyWithUnit("160000", "USD", "annual", 2),
		`lang="de-DE"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("selected work rail omitted %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "/Std.") || strings.Contains(markup, "/hr") || strings.Contains(markup, "160.000 USD") {
		t.Fatalf("selected work rail retained an hourly or rounded pay rendering: %s", markup)
	}
}

func TestTodo_UXBLIND_117_Browser(t *testing.T) {
	for _, localeName := range []string{"en-US", "de-DE", "ar"} {
		view := testView(PageWork)
		view.Locale = ResolveProductLocale(localeName)
		item := uxblindAEWorkItem()
		if localeName == "en-US" {
			item.CurrentPayBasis, item.ProposedPayBasis = "HOURLY_RATE", "HOURLY_RATE"
			item.CurrentBase, item.ProposedBase = testMoney("34.5", "USD"), testMoney("40", "USD")
		}
		markup, err := ui.RenderToString(WorkPreview(workPreviewProps(view, item)))
		if err != nil {
			t.Fatalf("render %s: %v", localeName, err)
		}
		basis := "annual"
		current, proposed := "132000", "160000"
		if localeName == "en-US" {
			basis, current, proposed = "hourly_rate", "34.5", "40"
		}
		for _, amount := range []string{current, proposed} {
			if want := view.Locale.FormatMoneyWithUnit(amount, "USD", basis, 2); !strings.Contains(markup, want) {
				t.Errorf("%s rail omitted shared pay rendering %q: %s", localeName, want, markup)
			}
		}
		css := Stylesheet()
		for _, rule := range []string{"hyphens:auto", "overflow-wrap:normal", "word-break:normal"} {
			if !strings.Contains(css, rule) {
				t.Errorf("%s rail title stylesheet omitted %q", localeName, rule)
			}
		}
	}
}

func TestTodo_UXBLIND_118(t *testing.T) {
	for _, localeName := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeName)
		got := ResolveNextStepLabel(locale, "start_approval")
		want := locale.Text("work.next_step.start_approval")
		if got != want || (localeName != "en-US" && got == "Start approval") {
			t.Errorf("%s next-step label = %q, want localized catalog value %q", localeName, got, want)
		}
	}
}

func TestTodo_UXBLIND_118_Browser(t *testing.T) {
	for _, localeName := range []string{"en-US", "de-DE", "ar"} {
		view := testView(PageWork)
		view.Locale = ResolveProductLocale(localeName)
		item := uxblindAEWorkItem()
		markup, err := ui.RenderToString(WorkPreview(workPreviewProps(view, item)))
		if err != nil {
			t.Fatalf("render %s: %v", localeName, err)
		}
		want := view.Locale.Text("work.next_step.start_approval")
		if !strings.Contains(markup, want) || (localeName != "en-US" && strings.Contains(markup, "Start approval")) {
			t.Errorf("%s work detail leaked an untranslated next-step label: %s", localeName, markup)
		}
	}
}
