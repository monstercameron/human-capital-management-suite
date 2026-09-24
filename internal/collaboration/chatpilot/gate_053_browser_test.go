package chatpilot

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
)

// TestTodo_CHAT_053_Browser is the BROWSER matrix test for CHAT-053. Its
// GREEN requires a named design partner to use served chat "with SLO
// dashboards, incident playbook, rollback and observed adoption criteria";
// this scores the SLO dashboard presentation channel that data would be
// rendered into against the same structural accessibility fixture
// TestTodo_PRIV_002_Browser and UX-QUAL-001 use (landmarks, label
// association, live region, keyboard order, WCAG AA contrast, 320px
// reflow). A design partner reading an inaccessible dashboard cannot
// exercise the pilot's own adoption/rollback evidence, so the rendered
// surface itself is part of what GREEN requires.
func TestTodo_CHAT_053_Browser(t *testing.T) {
	t.Run("a ready pilot's dashboard passes every structural criterion", func(t *testing.T) {
		decision := Decision{Ready: true}
		metrics := []Metric{
			{Name: "chat.send_commit.p95", Samples: 1000, Observed: 0.4, Limit: 1, Unit: "seconds"},
			{Name: "workflow.timer_lateness.p99", Samples: 500, Observed: 0.2, Limit: 5, Unit: "seconds"},
		}
		doc, err := RenderDashboardDocument(decision, "tenant-partner", metrics)
		if err != nil {
			t.Fatalf("RenderDashboardDocument: %v", err)
		}

		checks := []qual.CriterionResult{
			qual.CheckKeyboard(doc),
			qual.CheckScreenReaderSemantics(doc),
			qual.CheckContrastAA(),
			qual.CheckReflow(qual.ExtractInlineCSS(doc)),
		}
		for _, c := range checks {
			t.Logf("%s: pass=%v (%s)", c.Name, c.Pass, c.Detail)
			if !c.Pass {
				t.Errorf("accessible dashboard document failed %q: %s", c.Name, c.Detail)
			}
		}
		if !strings.Contains(doc, "Ready: every signed evidence") {
			t.Error("ready dashboard did not render its readiness status")
		}
		if !strings.Contains(doc, "Within budget") {
			t.Error("ready dashboard did not render an in-budget metric status")
		}
	})

	t.Run("a blocked pilot's dashboard surfaces its unresolved reasons accessibly", func(t *testing.T) {
		decision := Decision{Ready: false, Reasons: []string{"missing signed evidence: rollback_drill", "observed adoption is below the signed scope decision threshold"}}
		metrics := []Metric{
			{Name: "chat.watch_delivery.p99", Samples: 800, Observed: 2.0, Limit: 1, Unit: "seconds"},
		}
		doc, err := RenderDashboardDocument(decision, "tenant-partner", metrics)
		if err != nil {
			t.Fatalf("RenderDashboardDocument: %v", err)
		}
		for _, c := range []qual.CriterionResult{
			qual.CheckKeyboard(doc),
			qual.CheckScreenReaderSemantics(doc),
			qual.CheckReflow(qual.ExtractInlineCSS(doc)),
		} {
			if !c.Pass {
				t.Errorf("blocked-pilot dashboard failed %q: %s", c.Name, c.Detail)
			}
		}
		if !strings.Contains(doc, "missing signed evidence: rollback_drill") {
			t.Error("blocked dashboard did not surface its unresolved reasons")
		}
		if !strings.Contains(doc, "Exceeds budget") {
			t.Error("blocked dashboard did not flag the out-of-budget metric")
		}
	})

	t.Run("RED: a screen with no landmarks, label or live region is exactly what an inaccessible dashboard would be", func(t *testing.T) {
		broken := `<html><head><style>.panel{width:900px}</style></head><body><a href="#x">go</a></body></html>`
		result := qual.CheckScreenReaderSemantics(broken)
		if result.Pass {
			t.Fatal("broken fixture unexpectedly passed screen-reader semantics")
		}
		if reflow := qual.CheckReflow(qual.ExtractInlineCSS(broken)); reflow.Pass {
			t.Fatal("broken fixture's fixed 900px panel unexpectedly passed reflow")
		}
	})
}
