package productui

// PROMOUX-011: "Invalidate promotion counts and timestamps after every
// durable transition."
//
// TestTodo_PROMOUX_011_Browser renders the two presentation clauses this
// todo's GREEN names through the real server-rendered path
// (ui.RenderToString, the same one promotion_review.go, approval_disposition
// .go and compensation_guardrail.go already use): the displayed "Updated"
// label equals the newest transition's own timestamp rather than "roughly
// now", and each of the five regions renders as its own independently
// addressed container rather than one undifferentiated page.
import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func promoux011BrowserTransition(tenant values.TenantId, occurredAt time.Time, id string) promotion.Transition {
	return promotion.Transition{
		Tenant:     tenant,
		JourneyRef: values.EntityRef{Tenant: tenant, Kind: values.Kind("promotion_journey"), Id: id},
		WorkerRef:  values.EntityRef{Tenant: tenant, Kind: values.Kind("worker"), Id: id},
		OccurredAt: occurredAt,
		Revision:   1,
	}
}

func TestTodo_PROMOUX_011_Browser(t *testing.T) {
	tenant := values.TenantId("acme")

	t.Run("Updated equals the newest transition's own timestamp, not now", func(t *testing.T) {
		oldest := promoux011BrowserTransition(tenant, time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC), "00000000-0000-4000-8000-00000000d001")
		newest := promoux011BrowserTransition(tenant, time.Date(2026, 9, 12, 14, 30, 0, 0, time.UTC), "00000000-0000-4000-8000-00000000d002")
		middle := promoux011BrowserTransition(tenant, time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC), "00000000-0000-4000-8000-00000000d003")

		label := LatestTransitionLastUpdatedLabel([]promotion.Transition{oldest, newest, middle})
		want := TransitionLastUpdatedLabel(newest.OccurredAt)
		if label != want {
			t.Fatalf("last-updated label = %q, want %q (the newest transition's own timestamp)", label, want)
		}
		if label != "12 Sep 2026 · 14:30 UTC" {
			t.Fatalf("last-updated label = %q, want the exact formatted newest transition timestamp", label)
		}
		// Order in the input must not matter: the newest transition's
		// timestamp wins regardless of where it appears in the slice.
		reordered := LatestTransitionLastUpdatedLabel([]promotion.Transition{newest, oldest, middle})
		if reordered != label {
			t.Fatalf("last-updated label depended on input order: %q vs %q", reordered, label)
		}

		markup, err := ui.RenderToString(html.P(html.Props{Class: "promotion-shell-updated"}, ui.Text(label)))
		if err != nil {
			t.Fatalf("render Updated label: %v", err)
		}
		if !strings.Contains(markup, "12 Sep 2026") || !strings.Contains(markup, "14:30 UTC") {
			t.Fatalf("rendered Updated markup missing the newest transition's own timestamp: %s", markup)
		}
		if strings.Contains(markup, "2026-05-01") || strings.Contains(markup, "2026-07-04") || strings.Contains(markup, "09:00") || strings.Contains(markup, "12:00") {
			t.Fatalf("rendered Updated markup leaked an older transition's timestamp: %s", markup)
		}
	})

	t.Run("no transitions renders no fabricated freshness", func(t *testing.T) {
		if label := LatestTransitionLastUpdatedLabel(nil); label != "" {
			t.Fatalf("empty transition set produced a label = %q, want empty (must never fall back to now)", label)
		}
		if label := TransitionLastUpdatedLabel(time.Time{}); label != "" {
			t.Fatalf("zero-value timestamp produced a label = %q, want empty", label)
		}
	})

	t.Run("every region renders as its own independently addressed container", func(t *testing.T) {
		regions := promotion.Regions()
		if len(regions) != 5 {
			t.Fatalf("Regions() = %v, want exactly 5", regions)
		}
		containers := make([]ui.Node, len(regions))
		for i, region := range regions {
			containers[i] = PromotionRegionContainer(region, html.P(html.Props{}, ui.Text(string(region))))
		}
		markup, err := ui.RenderToString(html.Div(html.Props{Class: "promotion-shell"}, containers...))
		if err != nil {
			t.Fatalf("render regions: %v", err)
		}
		seenAttrs := make(map[string]bool, len(regions))
		for _, region := range regions {
			projection, err := region.Projection()
			if err != nil {
				t.Fatal(err)
			}
			attr := `data-promotion-region="` + projection + `"`
			if !strings.Contains(markup, attr) {
				t.Fatalf("region %q missing its own container attribute %s in: %s", region, attr, markup)
			}
			if seenAttrs[attr] {
				t.Fatalf("two regions rendered the identical container attribute %s", attr)
			}
			seenAttrs[attr] = true
		}
		if len(seenAttrs) != len(regions) {
			t.Fatalf("distinct region containers rendered = %d, want %d", len(seenAttrs), len(regions))
		}

		// An unrecognized region still renders, tagged distinctly from every
		// real region rather than colliding with one of their names.
		unknown, err := ui.RenderToString(PromotionRegionContainer(promotion.Region("BOGUS"), ui.Text("x")))
		if err != nil {
			t.Fatalf("render unknown region: %v", err)
		}
		if !strings.Contains(unknown, `data-promotion-region="unknown"`) {
			t.Fatalf("unknown region did not render the fail-safe container attribute: %s", unknown)
		}
	})
}
