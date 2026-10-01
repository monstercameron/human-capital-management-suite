package journeyclient

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_UXBLIND_075(t *testing.T) {
	ts := fakeTimestamp{seconds: time.Date(2026, 9, 28, 21, 53, 0, 0, time.UTC).Unix()}
	for _, locale := range productui.SupportedProductLocales() {
		t.Run(locale, func(t *testing.T) {
			copy := productui.ResolveProductLocale(locale)
			instant, ok := timeOf(ts)
			if !ok {
				t.Fatal("test timestamp was treated as unset")
			}
			if got, want := formatDateLocale(locale, "2026-09-28"), copy.FormatDate(instant); got != want {
				t.Fatalf("date = %q, want shared formatter %q", got, want)
			}
			if got, want := formatTimeLocale(locale, ts), copy.FormatTimestamp(instant); got != want {
				t.Fatalf("timestamp = %q, want viewer formatter %q", got, want)
			}
			for _, basis := range []string{"ANNUAL_SALARY", payBasisHourly} {
				got := formatAmountWithPayUnitLocale(locale, "USD", "93000", basis)
				wantUnit := "annual"
				if basis == payBasisHourly {
					wantUnit = "hourly_rate"
				}
				want := copy.FormatMoneyWithPayUnit("93000", "USD", wantUnit, 2)
				if got != want || !strings.Contains(got, copy.Text("format.pay_unit."+map[string]string{"annual": "year", "hourly_rate": "hour"}[wantUnit])) {
					t.Fatalf("%s amount = %q, want shared %q", basis, got, want)
				}
			}
		})
	}
}

func TestTodo_UXBLIND_075_Browser(t *testing.T) {
	for _, locale := range productui.SupportedProductLocales() {
		got := summaryPayLine(locale, "PPL-HRBP2", "P2", "PPL-HRBP3", "P3", "USD", "93000", "98000", "ANNUAL_SALARY", "ANNUAL_SALARY", "scope")
		copy := productui.ResolveProductLocale(locale)
		for _, amount := range []string{"93000", "98000"} {
			want := copy.FormatMoneyWithPayUnit(amount, "USD", "annual", 2)
			if !strings.Contains(got, want) {
				t.Fatalf("%s summary %q missing shared amount %q", locale, got, want)
			}
		}
	}
}
