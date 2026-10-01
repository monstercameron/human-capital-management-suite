package journeyclient

import (
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func uxblindAEJourney() *journeyv1.Journey {
	return &journeyv1.Journey{
		Current:     &journeyv1.Placement{JobCode: "PPL-HRBP3", Grade: "P4"},
		Target:      &journeyv1.Placement{JobCode: "PPL-HRBP4", Grade: "P5"},
		CurrentBase: "132000", ProposedBase: "160000", Currency: "USD",
		CurrentPayBasis: "ANNUAL_SALARY", ProposedPayBasis: "ANNUAL_SALARY",
		Viewer: &journeyv1.JourneyViewerProjection{
			NextStep: journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_START_APPROVAL,
		},
	}
}

func TestTodo_UXBLIND_117(t *testing.T) {
	for _, localeName := range []string{"en-US", "de-DE", "ar"} {
		locale := productui.ResolveProductLocale(localeName)
		got := card(Config{Locale: localeName}, uxblindAEJourney()).PayLine
		wantCurrent := locale.FormatMoneyWithUnit("132000", "USD", "annual", 2)
		wantProposed := locale.FormatMoneyWithUnit("160000", "USD", "annual", 2)
		if !containsAll(got, wantCurrent, wantProposed) {
			t.Errorf("%s Journeys pay line = %q, want %q and %q", localeName, got, wantCurrent, wantProposed)
		}
	}
}

func TestTodo_UXBLIND_117_Browser(t *testing.T) {
	journey := uxblindAEJourney()
	journey.CurrentPayBasis, journey.ProposedPayBasis = payBasisHourly, payBasisHourly
	journey.CurrentBase, journey.ProposedBase = "34.5", "40"
	got := card(Config{Locale: "en-US"}, journey).PayLine
	if want := productui.ResolveProductLocale("en-US").FormatMoneyWithUnit("34.5", "USD", "hourly_rate", 2); !containsAll(got, want) {
		t.Fatalf("hourly Journeys card pay line = %q, want %q", got, want)
	}
}

func TestTodo_UXBLIND_118(t *testing.T) {
	for _, localeName := range []string{"en-US", "de-DE", "ar"} {
		locale := productui.ResolveProductLocale(localeName)
		got := card(Config{Locale: localeName}, uxblindAEJourney()).NextStep
		want := locale.Text("work.next_step.start_approval")
		if got != want || (localeName != "en-US" && got == "Start approval") {
			t.Errorf("%s Journeys next step = %q, want localized catalog value %q", localeName, got, want)
		}
	}
}

func TestTodo_UXBLIND_118_Browser(t *testing.T) {
	for _, localeName := range []string{"en-US", "de-DE", "ar"} {
		got := card(Config{Locale: localeName}, uxblindAEJourney()).NextStep
		if got == "" || (localeName != "en-US" && got == "Start approval") {
			t.Errorf("%s Journeys card exposed raw or empty next step %q", localeName, got)
		}
	}
}

func containsAll(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(value, fragment) {
			return false
		}
	}
	return true
}
