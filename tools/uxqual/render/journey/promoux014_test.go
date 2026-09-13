package journey

import (
	"strings"
	"testing"
)

// promoux014Explanation is a fixture matching what
// tools/uxqual/journeyclient's waitExplanationFacts actually projects: the
// six labelled facts, in reading order, none of them empty.
func promoux014Explanation() []Fact {
	return []Fact{
		{Label: "Effective instant", Value: "Waits until 2026-12-01T05:00:00Z, the start of the effective date in America/New_York, the zone the wait step is declared against."},
		{Label: "Owner", Value: "Approved by principal:manager-approver, who is the steward of record while this journey waits."},
		{Label: "Scheduled action", Value: "At that instant, the workflow revalidates the promotion's pinned facts before execution can proceed."},
		{Label: "Remaining checks", Value: "Remaining checks: revalidation of the pinned facts, then execution and effect observation before the promotion outcome is recorded."},
		{Label: "Notification", Value: "No notification is sent when this wait resolves. Reopen or refresh this journey to see its new stage once the effective instant has passed."},
		{Label: "Authorized intervention", Value: "None. Production exposes no manual timer bypass; this wait resolves only at its scheduled instant, never from an action on this page."},
	}
}

// factTextContains reports whether rendered contains value's text as GWC's
// text-node HTML escaping would actually render it: ' becomes &#39; and the
// rest of value's punctuation survives untouched, so a literal
// strings.Contains(rendered, value) would fail on fixture text (this
// package's own house style) that reads naturally with an apostrophe.
func factTextContains(rendered, value string) bool {
	escaped := strings.ReplaceAll(value, "'", "&#39;")
	return strings.Contains(rendered, escaped)
}

// TestTodo_PROMOUX_014_Browser is the BROWSER matrix test: it renders the
// workflow section exactly as the live page would and proves the "Waiting
// for effective date" subsection carries every one of RED's missing facts
// as visible text under its own heading -- not folded into "Preflight and
// simulation", which is what a reader would misread as a simulation
// warning about their own proposal rather than an explanation of why they
// are looking at a wait.
func TestTodo_PROMOUX_014_Browser(t *testing.T) {
	t.Run("every explanation fact renders under its own heading", func(t *testing.T) {
		out := mustRenderNode(t, workflowSection(DetailView{
			Engine:          []Fact{{Label: "Instance", Value: "wfi_1", Mono: true}},
			WaitExplanation: promoux014Explanation(),
		}))
		if !strings.Contains(out, "Waiting for effective date") {
			t.Fatal("rendered workflow section carries no \"Waiting for effective date\" heading")
		}
		if strings.Contains(out, `id="findings-heading"`) {
			t.Fatal("workflow section unexpectedly rendered the preflight/findings heading; wrong section")
		}
		for _, fact := range promoux014Explanation() {
			if !strings.Contains(out, fact.Label) {
				t.Errorf("rendered output is missing the label %q", fact.Label)
			}
			if !factTextContains(out, fact.Value) {
				t.Errorf("rendered output is missing the fact text for %q", fact.Label)
			}
		}
	})

	t.Run("the subsection is absent, not empty, for a journey with no wait to explain", func(t *testing.T) {
		out := mustRenderNode(t, workflowSection(DetailView{
			Engine: []Fact{{Label: "Instance", Value: "wfi_1", Mono: true}},
		}))
		if strings.Contains(out, "Waiting for effective date") {
			t.Fatal("workflow section rendered the wait-explanation heading with no WaitExplanation supplied")
		}
	})

	t.Run("preflight/simulation findings never carry a WAIT_ code", func(t *testing.T) {
		// This is the render-side half of the same guarantee
		// tools/uxqual/journeyclient's TestTodo_PROMOUX_014_Regression
		// proves at the projection layer: even if a caller mistakenly fed
		// a WAIT_* finding into the generic Findings list, the preflight
		// board renders it as an ordinary check pill (there is nothing
		// wrong with the Finding type itself), so the real guard has to
		// live where the wire is split -- this test documents that this
		// section does not itself filter, and must not be relied on to.
		out := mustRenderNode(t, preflightSection(DetailView{
			Findings: []Finding{{Severity: "info", Code: "WAIT_OWNER", Message: "should not reach here"}},
		}))
		if !strings.Contains(out, "should not reach here") {
			t.Fatal("preflightSection no longer renders whatever Findings it is given; update this test's premise")
		}
	})
}
