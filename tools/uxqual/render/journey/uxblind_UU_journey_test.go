package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func renderUXBLINDUU(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func TestTodo_UXBLIND_108_Browser(t *testing.T) {
	card := JourneyCard{
		WorkerName:     "Ana Flores",
		Headline:       "Journeyman Carpenter (C3) → Foreman (C4)",
		PlacementCodes: "IR-JCP → IR-FMN",
		PayLine:        "USD 34.50 per hour → USD 40.00 per hour (+15.9%)",
		StageLabel:     "Awaiting approval",
		StageTone:      toneWarning,
	}
	markup := renderUXBLINDUU(t, journeyCardLocale("en-US", card))
	headline := strings.Index(markup, `class="jn-journey-headline"`)
	codes := strings.Index(markup, `class="jn-journey-codes"`)
	meta := strings.Index(markup, `class="jn-meta"`)
	if headline < 0 || codes < 0 || meta < 0 || !(headline < codes && codes < meta) {
		t.Fatalf("placement codes are not attached after the role title and before dates: %s", markup)
	}
	if !strings.Contains(markup, `dir="ltr"`) || strings.Contains(markup, "[IR-") {
		t.Fatalf("placement codes lost their isolated machine-code presentation: %s", markup)
	}

	sheet := Stylesheet()
	rule := ruleFor(sheet, ".jn-journey-codes")
	if !strings.Contains(rule, "font-size:0.75rem") || !strings.Contains(rule, "color:var(--jn-ink-muted)") {
		t.Fatalf("codes caption is not small and muted: %q", rule)
	}
}

func TestTodo_UXBLIND_108(t *testing.T) {
	markup := renderUXBLINDUU(t, heroSectionLocale("en-US", JourneyCard{
		WorkerName:     "Ana Flores",
		Headline:       "Journeyman Carpenter (C3) → Foreman (C4)",
		PlacementCodes: "IR-JCP → IR-FMN",
		PayLine:        "USD 34.50 per hour → USD 40.00 per hour (+15.9%)",
	}, false))
	if !strings.Contains(markup, `class="jn-journey-codes"`) || !strings.Contains(markup, "IR-JCP") {
		t.Fatalf("journey header omitted the attached muted code caption: %s", markup)
	}
}
