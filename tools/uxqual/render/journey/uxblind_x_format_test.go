package journey

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_UXBLIND_075(t *testing.T) {
	for _, locale := range productui.SupportedProductLocales() {
		t.Run(locale, func(t *testing.T) {
			copy := productui.ResolveProductLocale(locale)
			j := JourneyCard{
				WorkerName: "Omar Reyes", Stage: "PROPOSED", StageTone: toneInfo,
				EffectiveDate: copy.FormatDate(parseJourneyTestTime()), Updated: copy.FormatTimestamp(parseJourneyTestTime()),
				PayLine: "legacy pay", Edit: EditDefaults{Currency: "USD", CurrentBase: "93000", Base: "98000", CurrentPayBasis: "ANNUAL_SALARY", ProposedPayBasis: "HOURLY_RATE"},
			}
			markup, err := ui.RenderToString(ui.Fragment(heroSectionLocale(locale, j, false), journeyCardLocale(locale, j)))
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range []struct{ amount, unit string }{{"93000", "annual"}, {"98000", "hourly_rate"}} {
				want := copy.FormatMoneyWithPayUnit(value.amount, "USD", value.unit, 2)
				if strings.Count(markup, want) != 2 {
					t.Fatalf("%s markup has %d occurrences of %q, want header and card: %s", locale, strings.Count(markup, want), want, markup)
				}
			}
			if !strings.Contains(markup, copy.FormatDate(parseJourneyTestTime())) || !strings.Contains(markup, copy.FormatTimestamp(parseJourneyTestTime())) {
				t.Fatalf("%s markup did not retain shared date/timestamp values: %s", locale, markup)
			}
		})
	}
}

func TestTodo_UXBLIND_075_Browser(t *testing.T) {
	copy := productui.ResolveProductLocale("de-DE")
	got := journeyPayLineLocale("de-DE", JourneyCard{PayLine: "fallback", Edit: EditDefaults{Currency: "USD", CurrentBase: "93000", Base: "98000", CurrentPayBasis: "ANNUAL_SALARY"}})
	if !strings.Contains(got, copy.FormatMoneyWithPayUnit("93000", "USD", "annual", 2)) || !strings.Contains(got, copy.FormatMoneyWithPayUnit("98000", "USD", "annual", 2)) {
		t.Fatalf("renderer did not use shared German formatter: %q", got)
	}
}

func parseJourneyTestTime() (t time.Time) { return time.Date(2026, 9, 28, 21, 53, 0, 0, time.UTC) }
