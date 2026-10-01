package journeyclient

import (
	"strings"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTodo_UXBLIND_095_Projector(t *testing.T) {
	got := card(Config{Locale: "en-US"}, &journeyv1.Journey{
		WorkerName: "Ana Reyes", Current: &journeyv1.Placement{JobCode: "IR-JCP", Grade: "C3"}, Target: &journeyv1.Placement{JobCode: "IR-FMN", Grade: "C4"},
		Currency: "USD", CurrentBase: "34.50", ProposedBase: "40.00", CurrentPayBasis: "HOURLY_RATE", ProposedPayBasis: "HOURLY_RATE",
		BusinessReason: "Ana has run the framing crew on two jobs.",
	})
	if strings.Contains(got.PayLine, "Business reason") || strings.Contains(got.Headline, "[") || strings.Contains(got.Headline, "IR-") {
		t.Fatalf("projector still mixes presentation facts: %+v", got)
	}
	if got.PlacementCodes != "IR-JCP → IR-FMN" || got.BusinessReason == "" {
		t.Fatalf("projector did not carry separate code and reason facts: %+v", got)
	}
}

func TestTodo_UXBLIND_095_Browser(t *testing.T) {
	if got := summaryPayLine("en-US", "IR-JCP", "C3", "IR-FMN", "C4", "USD", "34.50", "40.00", "HOURLY_RATE", "HOURLY_RATE", "reason"); strings.Contains(got, "reason") || strings.Contains(got, "Business") {
		t.Fatalf("pay projection still contains the business reason: %q", got)
	}
}

func TestTodo_UXBLIND_097(t *testing.T) {
	previous := time.Local
	defer func() { time.Local = previous }()
	time.Local, _ = time.LoadLocation("America/New_York")
	got := formatTimeLocale("en-US", timestamppb.New(time.Date(2026, time.September, 28, 18, 5, 0, 0, time.UTC)))
	if got != "28 Sep 2026, 14:05 EDT" {
		t.Fatalf("journey timestamp = %q, want viewer-zone timestamp", got)
	}
}

func TestTodo_UXBLIND_097_Browser(t *testing.T) {
	if got := productui.ResolveProductLocale("en-US").WithTimeZone("America/New_York").FormatTimestamp(time.Date(2026, time.September, 28, 18, 5, 0, 0, time.UTC)); got != "28 Sep 2026, 14:05 EDT" {
		t.Fatalf("shared formatter = %q, want EDT", got)
	}
}
