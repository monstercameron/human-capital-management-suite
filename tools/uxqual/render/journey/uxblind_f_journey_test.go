package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_043(t *testing.T) {
	markup, err := ui.RenderToString(heroSectionLocale("de-DE", JourneyCard{
		WorkerName: "Omar Reyes", Stage: "PROPOSED", StageLabel: "Vorgeschlagen", StageTone: toneInfo,
	}, false))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Bereit, die Genehmigung zu starten") || strings.Contains(markup, "Vorgeschlagen") {
		t.Fatalf("German journey status did not use the stable stage key: %s", markup)
	}
}

func TestTodo_UXBLIND_043_Browser(t *testing.T) {
	markup, err := ui.RenderToString(journeyCardLocale("de-DE", JourneyCard{
		IntentID: "intent-proposed", WorkerName: "Omar Reyes", Stage: "PROPOSED", StageLabel: "Vorgeschlagen", StageTone: toneInfo,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Bereit, die Genehmigung zu starten") {
		t.Fatalf("German card status missing: %s", markup)
	}
}

func TestTodo_UXBLIND_043_I18n(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup, err := ui.RenderToString(heroSectionLocale(locale, JourneyCard{Stage: "PROPOSED", StageTone: toneInfo}, false))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, "data-tone=\"info\"") {
			t.Fatalf("%s lost the shared status tone", locale)
		}
	}
}

func TestTodo_UXBLIND_044(t *testing.T) {
	markup, err := ui.RenderToString(heroSectionLocale("ar", JourneyCard{
		WorkerName: "Omar Reyes", Stage: "PROPOSED", StageTone: toneInfo, PayLine: "١٣٢٬٠٠٠٫٠٠ → ١٦٠٬٠٠٠٫٠٠",
	}, false))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "١٣٢٬٠٠٠٫٠٠ ← ١٦٠٬٠٠٠٫٠٠") {
		t.Fatalf("RTL pay change did not mirror its arrow: %s", markup)
	}
}

func TestTodo_UXBLIND_044_Browser(t *testing.T) {
	markup, err := ui.RenderToString(detailNavigationLocale("ar", DetailView{BackLink: NavLink{Href: "/people/1", Label: "العودة إلى الملف الشخصي"}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "→") || strings.Contains(markup, "← العودة") {
		t.Fatalf("RTL back navigation kept the LTR arrow: %s", markup)
	}
}

func TestTodo_UXBLIND_044_I18n(t *testing.T) {
	markup, err := ui.RenderToString(heroSectionLocale("ar", JourneyCard{Stage: "PROPOSED", StageTone: toneInfo}, false))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="jn-chip"`) {
		t.Fatalf("Arabic journey did not render a status chip: %s", markup)
	}
}
