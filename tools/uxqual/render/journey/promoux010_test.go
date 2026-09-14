package journey

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// This file is PROMOUX-010's whole test matrix. The todo's own RED was
// measured live against the running server (see the todo's Live evidence):
// opening a review pushed "Propose and simulate" 473px below the fold with
// scrollY at 0, shifted two unrelated <section>s by +37px and -54px,
// dropped focus to <body>, and offered no Cancel at all -- the whole
// surface was one lone submit button. actionCard's old <details
// class="jn-confirm"> block reproduced the same shape structurally: a
// plain in-flow reveal, no focus/escape/busy wiring, and (for the
// ProposalForm path) no confirmation step whatsoever.
//
// review_surface.go fixes this by keeping the <details>/<summary>
// disclosure (so a script-free reader still gets a working review) but
// rendering its content through github.com/monstercameron/GoWebComponents
// /v5/ui.Overlay in Modal mode once a live client mirrors the disclosure's
// open state -- the library's own tested focus-trap/restore-focus/
// escape-to-dismiss primitive, not a hand-rolled one -- with CSS
// (typed_journey_c.go's .jn-confirm-surface/.jn-confirm-backdrop/
// .jn-confirm-actionbar) that keeps it a viewport-anchored, non-reflowing
// panel instead of an in-flow block.
//
// Every test below runs against ui.RenderToString (the native/SSR path),
// which is this package's own test path (see components.go's file
// doc-comment): ui.Overlay's native build renders its children as a plain
// Fragment regardless of Open (see review_surface.go's own doc comment),
// so the <details> disclosure, the facts, the note, the real Cancel button
// and the busy status line are all directly assertable here. What is NOT
// assertable here -- the live focus trap, Escape-to-dismiss and
// restore-focus wiring ui.Overlay itself provides -- is compiled and
// linked (GOOS=js GOARCH=wasm go build ./tools/uxqual/cmd/journeywasm/)
// but only provable against a real browser, exactly as PROMOUX-011's own
// evidence records for this package's other js&&wasm-only behavior.

func promoux010Action(confirmation []Fact, note string) Action {
	return Action{
		ID: "approve", Label: "Approve", Variant: "primary", Action: "/approve",
		Confirmation:     confirmation,
		ConfirmationNote: note,
	}
}

// TestTodo_PROMOUX_010 is the PRIMARY matrix test: the shared review
// surface's own content contract, exercised through both call sites the
// REFACTOR names (actionCard for Approve/Reject/Withdraw/Cancel, and
// proposalFormSection for Start), plus the branches that must NOT show it.
func TestTodo_PROMOUX_010(t *testing.T) {
	t.Run("action with confirmation renders one shared review surface with facts, note, cancel before submit", func(t *testing.T) {
		a := promoux010Action(
			[]Fact{{Label: "Employee", Value: "Priya Raghunathan"}, {Label: "Change", Value: "P2 to P3"}, {Label: "Effective date", Value: "1 Jun 2026"}},
			"This records a governed promotion fact once submitted.",
		)
		out := mustRenderNode(t, actionCard(live{}, a))
		for _, want := range []string{
			`class="jn-confirm"`, "Review and approve", "Cancel review", `class="jn-confirm-close-label"`,
			"Confirm approval", "Priya Raghunathan", "P2 to P3", "1 Jun 2026",
			"This records a governed promotion fact once submitted.",
			`class="jn-confirm-actionbar"`, `class="jn-btn jn-confirm-cancel"`, `type="button"`, ">Cancel<",
			`class="jn-confirm-status"`,
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("review surface missing %q in:\n%s", want, out)
			}
		}
		cancelAt := strings.Index(out, "jn-confirm-cancel")
		submitAt := strings.Index(out, `type="submit">Approve<`)
		if cancelAt < 0 || submitAt < 0 || cancelAt > submitAt {
			t.Fatalf("cancel (%d) does not precede the final action (%d) in document order:\n%s", cancelAt, submitAt, out)
		}
		if strings.Contains(out, `data-variant="primary"`) == false {
			t.Fatal("the final action lost its own primary variant")
		}
		if !regexp.MustCompile(`jn-confirm-cancel"[^>]*data-variant="secondary"|data-variant="secondary"[^>]*jn-confirm-cancel`).MatchString(out) {
			t.Fatalf("cancel must stay a plain secondary control regardless of the guarded action's own variant:\n%s", out)
		}
	})

	t.Run("action without confirmation submits directly with no review surface", func(t *testing.T) {
		a := promoux010Action(nil, "")
		out := mustRenderNode(t, actionCard(live{}, a))
		if strings.Contains(out, "jn-confirm") {
			t.Fatalf("an action with no Confirmation and no ConfirmationNote must not render a review surface:\n%s", out)
		}
		if !strings.Contains(out, ">Approve<") {
			t.Fatal("the action lost its own direct submit button")
		}
	})

	t.Run("a disabled action never shows the review surface even with confirmation set", func(t *testing.T) {
		a := promoux010Action([]Fact{{Label: "Employee", Value: "Priya"}}, "note")
		a.Disabled = true
		a.DisabledReason = "Routed to a different approver."
		out := mustRenderNode(t, actionCard(live{}, a))
		if strings.Contains(out, "jn-confirm") {
			t.Fatalf("a disabled action must not offer a review surface for a submission it cannot take:\n%s", out)
		}
	})

	t.Run("proposal form with confirmation reuses the identical review surface markup shape", func(t *testing.T) {
		f := ProposalForm{
			Action: "/workspace/journeys/propose", Submit: "Propose and simulate",
			Confirmation: []Fact{
				{Label: "Employee", Value: "Peter Tan"},
				{Label: "Change", Value: "ENG-SWE3 · P3 to ENG-SWE4 · P4"},
				{Label: "Effective date", Value: "1 Jul 2026"},
			},
			ConfirmationNote: "Nothing executes until the gate admits the simulated plan.",
		}
		out := mustRenderNode(t, proposalFormSection(live{}, f, "Propose a promotion for Peter Tan"))
		for _, want := range []string{
			`class="jn-confirm"`, "Review and propose", "Cancel review",
			"Confirm propose and simulate", "Peter Tan", "ENG-SWE3 · P3 to ENG-SWE4 · P4", "1 Jul 2026",
			"Nothing executes until the gate admits the simulated plan.",
			`class="jn-confirm-actionbar"`, `class="jn-btn jn-confirm-cancel"`, ">Propose and simulate<",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("proposal review surface missing %q in:\n%s", want, out)
			}
		}
	})

	t.Run("proposal form without confirmation still submits directly, unchanged from before PROMOUX-010", func(t *testing.T) {
		f := ProposalForm{Action: "/workspace/journeys/propose", Submit: "Propose and simulate"}
		out := mustRenderNode(t, proposalFormSection(live{}, f, "Propose a promotion"))
		if strings.Contains(out, "jn-confirm") {
			t.Fatalf("a ProposalForm with no Confirmation must keep submitting directly:\n%s", out)
		}
		if !strings.Contains(out, ">Propose and simulate<") || !strings.Contains(out, "jn-help") {
			t.Fatal("the direct-submit proposal form lost its own submit button or help text")
		}
	})

	t.Run("busy keeps the action bar and cancel mounted, disables submit, and announces status", func(t *testing.T) {
		a := promoux010Action([]Fact{{Label: "Employee", Value: "Priya"}}, "note")
		a.Busy = true
		a.BusyLabel = "Submitting your approval…"
		out := mustRenderNode(t, actionCard(live{}, a))
		if !strings.Contains(out, `class="jn-confirm-actionbar"`) || !strings.Contains(out, `class="jn-btn jn-confirm-cancel"`) {
			t.Fatalf("busy must not remove the action bar or the cancel control:\n%s", out)
		}
		if !strings.Contains(out, "Submitting your approval…") {
			t.Fatalf("busy status text is missing:\n%s", out)
		}
		if !strings.Contains(out, `role="status"`) {
			t.Fatalf("the busy status line must be announced via role=status:\n%s", out)
		}
		submitButton := regexp.MustCompile(`<button[^>]*data-variant="primary"[^>]*>Approve</button>`).FindString(out)
		if submitButton == "" {
			t.Fatalf("could not locate the guarded submit button:\n%s", out)
		}
		if !strings.Contains(submitButton, "disabled") || !strings.Contains(submitButton, `aria-busy="true"`) {
			t.Fatalf("busy submit button must be disabled and marked aria-busy: %s", submitButton)
		}
		cancelButton := regexp.MustCompile(`<button[^>]*jn-confirm-cancel[^>]*>Cancel</button>`).FindString(out)
		if cancelButton == "" {
			t.Fatalf("could not locate the cancel control:\n%s", out)
		}
		if strings.Contains(cancelButton, "disabled") {
			t.Fatalf("cancel must stay usable while a submission is in flight: %s", cancelButton)
		}
	})
}

// cssRule extracts the one declaration block for selector from css
// (selector{...}), or fails the test: every geometry assertion below pins
// literal property:value pairs rather than merely checking the selector
// exists, but must not embed gwccss's content-hashed animation-name in the
// literal (see styles_test.go's own TestStylesheetAnimationNamesHaveKeyframes
// for why that hash is intentionally volatile).
func cssRule(t *testing.T, css, selector string) string {
	t.Helper()
	start := strings.Index(css, selector+"{")
	if start < 0 {
		t.Fatalf("stylesheet has no rule for %q", selector)
	}
	end := strings.Index(css[start:], "}")
	if end < 0 {
		t.Fatalf("rule for %q is not terminated", selector)
	}
	return css[start : start+end+1]
}

// TestTodo_PROMOUX_010_Browser is the BROWSER matrix test. It cannot drive
// a real viewport (this package's own test path is SSR, not a browser --
// see the file doc comment), so it proves what a browser test would need
// to already be true in the shipped markup and stylesheet: the
// zero-JavaScript <details> baseline still exists and still encloses the
// review's content (so a script-free reader reaches Cancel and the final
// action by clicking once), and the containment CSS a live client's
// ui.Overlay-rendered panel depends on is actually declared in the one
// stylesheet the CSP pins.
func TestTodo_PROMOUX_010_Browser(t *testing.T) {
	a := promoux010Action([]Fact{{Label: "Employee", Value: "Priya"}}, "note")
	out := mustRenderNode(t, actionCard(live{}, a))

	t.Run("the review's content is a descendant of the zero-JavaScript details disclosure", func(t *testing.T) {
		detailsAt := strings.Index(out, `<details class="jn-confirm"`)
		closeAt := strings.LastIndex(out, "</details>")
		bodyAt := strings.Index(out, `class="jn-confirm-body"`)
		if detailsAt < 0 || closeAt < 0 || bodyAt < 0 || !(detailsAt < bodyAt && bodyAt < closeAt) {
			t.Fatalf("review content is not nested inside the native <details> disclosure:\n%s", out)
		}
	})

	css := Stylesheet()
	t.Run("the live panel is viewport-fixed with a bounded, internally scrollable height", func(t *testing.T) {
		rule := cssRule(t, css, ".jn-confirm-surface")
		for _, want := range []string{"position:fixed", "max-height:calc(100vh - 2rem)", "overflow-y:auto"} {
			if !strings.Contains(rule, want) {
				t.Fatalf(".jn-confirm-surface rule missing %q: %s", want, rule)
			}
		}
	})
	t.Run("the backdrop is viewport-fixed and full-bleed, not an in-flow element", func(t *testing.T) {
		rule := cssRule(t, css, ".jn-confirm-backdrop")
		for _, want := range []string{"position:fixed", "inset:0"} {
			if !strings.Contains(rule, want) {
				t.Fatalf(".jn-confirm-backdrop rule missing %q: %s", want, rule)
			}
		}
	})
	t.Run("the action bar stays pinned to the panel's own bottom edge as it scrolls", func(t *testing.T) {
		rule := cssRule(t, css, ".jn-confirm-actionbar")
		for _, want := range []string{"position:sticky", "bottom:0"} {
			if !strings.Contains(rule, want) {
				t.Fatalf(".jn-confirm-actionbar rule missing %q: %s", want, rule)
			}
		}
	})

	// A prior version of this suite only ever exercised actionCard here:
	// every assertion above would have passed unchanged while Start's own
	// path -- proposalFormSection, the one the todo's live evidence was
	// measured against -- rendered a lone submit button in a plain
	// jn-formfoot div, no <details>, no jn-confirm anywhere. This subtest
	// is what closes that hole: it renders the Start path specifically and
	// requires the identical contained-disclosure structure the Action
	// path already had proven above.
	t.Run("the proposal form's own final action uses the identical contained review structure", func(t *testing.T) {
		f := ProposalForm{
			Action: "/workspace/journeys/propose", Submit: "Propose and simulate",
			Confirmation:     []Fact{{Label: "Employee", Value: "Peter Tan"}},
			ConfirmationNote: "note",
		}
		proposalOut := mustRenderNode(t, proposalFormSection(live{}, f, "Propose a promotion for Peter Tan"))
		detailsAt := strings.Index(proposalOut, `<details class="jn-confirm"`)
		closeAt := strings.LastIndex(proposalOut, "</details>")
		bodyAt := strings.Index(proposalOut, `class="jn-confirm-body"`)
		if detailsAt < 0 || closeAt < 0 || bodyAt < 0 || !(detailsAt < bodyAt && bodyAt < closeAt) {
			t.Fatalf("Start's own review content is not nested inside the native <details> disclosure:\n%s", proposalOut)
		}
	})
}

// TestTodo_PROMOUX_010_Accessibility is the ACCESSIBILITY matrix test:
// Cancel's accessible name and operability, the heading id the live
// overlay's aria-labelledby depends on, and the busy announcement's
// role=status wiring.
func TestTodo_PROMOUX_010_Accessibility(t *testing.T) {
	a := promoux010Action([]Fact{{Label: "Employee", Value: "Priya"}}, "note")
	out := mustRenderNode(t, actionCard(live{}, a))

	t.Run("cancel is a real, keyboard-operable button named Cancel", func(t *testing.T) {
		m := regexp.MustCompile(`<button[^>]*class="jn-btn jn-confirm-cancel"[^>]*>([^<]*)</button>`).FindStringSubmatch(out)
		if m == nil {
			t.Fatalf("no distinct cancel <button> found:\n%s", out)
		}
		if strings.TrimSpace(m[1]) != "Cancel" {
			t.Fatalf("cancel button's accessible name is %q, want exactly %q", m[1], "Cancel")
		}
		full := regexp.MustCompile(`<button[^>]*class="jn-btn jn-confirm-cancel"[^>]*>`).FindString(out)
		if !strings.Contains(full, `type="button"`) {
			t.Fatalf("cancel must be type=button so it never behaves as a submit: %s", full)
		}
	})

	t.Run("the review heading carries a stable id for aria-labelledby", func(t *testing.T) {
		tag := regexp.MustCompile(`<p[^>]*class="jn-confirm-title"[^>]*>Confirm approval</p>`).FindString(out)
		if tag == "" {
			t.Fatalf("review heading not found:\n%s", out)
		}
		if !strings.Contains(tag, `id="action-approve-review-heading"`) {
			t.Fatalf("review heading is missing its stable id: %s", tag)
		}
	})

	t.Run("the busy status line is a role=status region, present even when idle so its geometry never appears or disappears", func(t *testing.T) {
		idleTag := regexp.MustCompile(`<p[^>]*class="jn-confirm-status"[^>]*></p>`).FindString(out)
		if idleTag == "" {
			t.Fatalf("idle review must still mount an empty role=status line:\n%s", out)
		}
		if !strings.Contains(idleTag, `role="status"`) || !strings.Contains(idleTag, `aria-live="polite"`) {
			t.Fatalf("idle status line lost its role=status/aria-live wiring: %s", idleTag)
		}
		busy := a
		busy.Busy = true
		busyOut := mustRenderNode(t, actionCard(live{}, busy))
		busyTag := regexp.MustCompile(`<p[^>]*class="jn-confirm-status"[^>]*>Submitting…</p>`).FindString(busyOut)
		if busyTag == "" {
			t.Fatalf("busy review did not announce the default busy label through the same element:\n%s", busyOut)
		}
		if !strings.Contains(busyTag, `role="status"`) || !strings.Contains(busyTag, `aria-live="polite"`) {
			t.Fatalf("busy status line lost its role=status/aria-live wiring: %s", busyTag)
		}
	})

	t.Run("reduced motion is honored: the panel's own animation is subject to the sheet's blanket reduced-motion override", func(t *testing.T) {
		css := Stylesheet()
		rule := cssRule(t, css, ".jn-confirm-surface")
		if !strings.Contains(rule, "animation-duration:") || !strings.Contains(rule, "animation-name:jn-slidein-") {
			t.Fatalf(".jn-confirm-surface declares no animation for the override to apply to: %s", rule)
		}
		if !strings.Contains(css, "prefers-reduced-motion:reduce") || !strings.Contains(css, "animation-duration:.001ms !important") {
			t.Fatal("stylesheet lost the blanket reduced-motion override every animation (including .jn-confirm-surface's) relies on")
		}
	})
}

// promoux010ManyActions builds n actions, each independently confirmed,
// with distinct ids and identical facts otherwise -- used by both the
// Performance and Regression tests below so a per-action cost or footprint
// claim is checked against more than one instance.
func promoux010ManyActions(n int) []Action {
	actions := make([]Action, n)
	for i := range actions {
		actions[i] = Action{
			ID: fmt.Sprintf("act%d", i), Label: "Approve", Variant: "primary", Action: "/approve",
			Confirmation:     []Fact{{Label: "Employee", Value: "Priya Raghunathan"}, {Label: "Effective date", Value: "1 Jun 2026"}},
			ConfirmationNote: "Approval records the governed promotion fact.",
		}
	}
	return actions
}

// promoux010ExtractForm returns the exact <form>...</form> block for
// actionID out of a rendered actions section, or fails the test.
func promoux010ExtractForm(t *testing.T, out, actionID string) string {
	t.Helper()
	marker := `id="action-` + actionID + `-review"`
	at := strings.Index(out, marker)
	if at < 0 {
		t.Fatalf("no review surface found for action %q in:\n%s", actionID, out)
	}
	formStart := strings.LastIndex(out[:at], "<form")
	formEnd := strings.Index(out[at:], "</form>")
	if formStart < 0 || formEnd < 0 {
		t.Fatalf("could not bound the <form> for action %q", actionID)
	}
	return out[formStart : at+formEnd+len("</form>")]
}

// TestTodo_PROMOUX_010_Performance is the PERFORMANCE matrix test. Two of
// this package's wall-clock latency gates (tools/uxqual/render/page and
// productui) have shown environmental flakiness on this machine, so this
// is a structural, bounded-growth assertion instead: each action's own
// review surface renders identical bytes regardless of how many sibling
// actions -- each independently confirmed -- share the page, and the
// number of rendered review surfaces is exactly the number of confirmed
// actions. A renderer that let sibling actions leak into each other's
// facts (a real, easy mistake with shared hook state) or that duplicated a
// review per unrelated action would fail one of these two checks even
// though every individual review still "looks right" in isolation.
func TestTodo_PROMOUX_010_Performance(t *testing.T) {
	small := mustRenderNode(t, actionsSection(live{}, promoux010ManyActions(2)))
	large := mustRenderNode(t, actionsSection(live{}, promoux010ManyActions(50)))

	if got, want := strings.Count(small, `class="jn-confirm-title"`), 2; got != want {
		t.Fatalf("2 confirmed actions rendered %d review surfaces, want %d", got, want)
	}
	if got, want := strings.Count(large, `class="jn-confirm-title"`), 50; got != want {
		t.Fatalf("50 confirmed actions rendered %d review surfaces, want %d (not O(n^2) duplication)", got, want)
	}

	firstSmall := promoux010ExtractForm(t, small, "act0")
	firstLarge := promoux010ExtractForm(t, large, "act0")
	firstSmall = strings.ReplaceAll(firstSmall, `id="action-act0-`, `id="action-actN-`)
	firstLarge = strings.ReplaceAll(firstLarge, `id="action-act0-`, `id="action-actN-`)
	if firstSmall != firstLarge {
		t.Fatalf("action act0's own review surface changed size/content between 2 and 50 sibling actions (want byte-identical, ids normalized):\n--- 2 siblings ---\n%s\n--- 50 siblings ---\n%s", firstSmall, firstLarge)
	}
}

// promoux010SectionOutsideActions strips the <section
// aria-labelledby="actions-heading">...</section> block that
// TestTodo_PROMOUX_010_Regression is deliberately allowed to change, so the
// comparison below is exactly "everything else on the page."
var promoux010ActionsSectionRE = regexp.MustCompile(`(?s)<section aria-labelledby="actions-heading">.*?</section>`)

func promoux010SectionOutsideActions(t *testing.T, out string) string {
	t.Helper()
	if !promoux010ActionsSectionRE.MatchString(out) {
		t.Fatalf("fixture has no actions section to strip -- this would make the comparison vacuous:\n%s", out)
	}
	return promoux010ActionsSectionRE.ReplaceAllString(out, "")
}

// TestTodo_PROMOUX_010_Regression is the REGRESSION matrix test, and pins
// PROMOUX-010's two geometry invariants as values rather than presence
// checks a defect could pass trivially:
//
//  1. the action bar's visibility relative to the viewport: the panel is
//     bounded to the viewport (max-height:calc(100vh - 2rem)) with its own
//     scroll (overflow-y:auto) and the action bar is pinned inside it
//     (position:sticky;bottom:0) -- the literal values, not merely that
//     some CSS exists;
//  2. sections outside the surface do not move when it opens: rendering
//     SampleDetailPage() -- which has a hero, a stepper, a proposal detail
//     section, comparison/findings/engine/timeline panels, i.e. real
//     content outside the actions rail, not a fixture built to have none
//     -- with and without one action's Confirmation set produces
//     byte-identical markup for every section outside the actions rail.
func TestTodo_PROMOUX_010_Regression(t *testing.T) {
	t.Run("geometry invariant: the panel is viewport-bounded and its action bar is pinned inside it", func(t *testing.T) {
		css := Stylesheet()
		surface := cssRule(t, css, ".jn-confirm-surface")
		if !strings.Contains(surface, "max-height:calc(100vh - 2rem)") {
			t.Fatalf("panel lost its viewport-relative height bound: %s", surface)
		}
		if !strings.Contains(surface, "overflow-y:auto") {
			t.Fatalf("panel lost its own internal scroll, so overflow would push the action bar off-screen again: %s", surface)
		}
		actionbar := cssRule(t, css, ".jn-confirm-actionbar")
		if !strings.Contains(actionbar, "position:sticky") || !strings.Contains(actionbar, "bottom:0") {
			t.Fatalf("action bar lost position:sticky;bottom:0 -- exactly the RED clause \"pushes the final action below the viewport\": %s", actionbar)
		}
	})

	t.Run("geometry invariant: opening one action's review does not reflow any section outside it", func(t *testing.T) {
		without := SampleDetailPage()
		outWithout := mustRender(t, without)

		with := SampleDetailPage()
		actions := make([]Action, len(with.Detail.Actions))
		copy(actions, with.Detail.Actions)
		if len(actions) == 0 {
			t.Fatal("fixture has no actions -- this would make the comparison vacuous")
		}
		actions[0].Confirmation = []Fact{{Label: "Employee", Value: "Omar Reyes"}, {Label: "Effective date", Value: "1 Jun 2026"}}
		actions[0].ConfirmationNote = "Approval records the governed promotion fact."
		with.Detail.Actions = actions
		outWith := mustRender(t, with)

		if outWithout == outWith {
			t.Fatal("setting Confirmation produced no change at all -- the review surface never rendered, so this test would prove nothing")
		}

		restWithout := promoux010SectionOutsideActions(t, outWithout)
		restWith := promoux010SectionOutsideActions(t, outWith)
		if restWithout != restWith {
			t.Fatalf("a section outside the actions rail changed when a review surface opened up inside it:\n--- without confirmation ---\n%s\n--- with confirmation ---\n%s", restWithout, restWith)
		}
	})

	// This is Start's own instance of the same invariant, on the actual
	// page the todo's live evidence measured it against: the People table
	// and the Journeys list sit above and below the proposal form on
	// /workspace/journey, and SampleListPage() carries a real four-worker
	// People panel and a real Journeys list, not a fixture built to have
	// neither -- so this is not vacuous the way an empty-page comparison
	// would be.
	t.Run("geometry invariant: Start's own review does not reflow People or Journeys around it", func(t *testing.T) {
		without := SampleListPage()
		outWithout := mustRender(t, without)

		with := SampleListPage()
		if len(with.List.People.Workers) == 0 || len(with.List.Journeys) == 0 {
			t.Fatal("fixture has no People or Journeys content outside the proposal form -- this would make the comparison vacuous")
		}
		with.List.Form.Confirmation = []Fact{{Label: "Employee", Value: "Priya Raghunathan"}, {Label: "Effective date", Value: "1 Jun 2026"}}
		with.List.Form.ConfirmationNote = "The proposal is simulated on submit; nothing is executed until the gate admits it."
		outWith := mustRender(t, with)

		if outWithout == outWith {
			t.Fatal("setting Form.Confirmation produced no change at all -- Start's review surface never rendered, so this test would prove nothing")
		}

		proposeSectionRE := regexp.MustCompile(`(?s)<section aria-labelledby="propose-heading" class="jn-panel">.*?</section>`)
		if !proposeSectionRE.MatchString(outWithout) {
			t.Fatalf("fixture has no propose section to strip -- this would make the comparison vacuous:\n%s", outWithout)
		}
		restWithout := proposeSectionRE.ReplaceAllString(outWithout, "")
		restWith := proposeSectionRE.ReplaceAllString(outWith, "")
		if restWithout != restWith {
			t.Fatalf("People or Journeys moved when Start's review surface opened up inside the proposal section:\n--- without confirmation ---\n%s\n--- with confirmation ---\n%s", restWithout, restWith)
		}
	})

	// The two subtests above render the review surface open and closed on
	// one already-mounted page; they cannot and do not exercise the Start
	// TRANSITION itself -- the live-measured residual where Page.List is
	// swapped for Page.Proposal wholesale, focus falls to <body>, and
	// nothing brings the new page's below-the-fold content into view.
	// Re-architecting that swap is out of scope (see review_surface.go and
	// components.go's own doc comments); focusOnMount/useFocusOnMount close
	// the focus half of it instead.
	//
	// Two live measurements shaped this: targeting the page's own heading
	// (kept below for its own, different reason -- screen-reader route
	// announcement) closed NEITHER clause, because the heading already sits
	// inside the viewport and focusing an in-view element cannot scroll
	// anything. Targeting the review surface's own trigger closed both,
	// because the trigger is the element genuinely below the fold. A
	// second, separate defect was fixed alongside the retarget: the first
	// version called the focus effect directly inside proposalView, a
	// plain function invoked from Build's conditionally-shaped tree, which
	// (per live.go's own doc comment on LiveComponent) shares
	// LiveComponent's single positional hook list rather than getting its
	// own -- exactly the "conditional hook" corruption that comment warns
	// against. focusOnMount now runs through its own ui.CreateElement
	// boundary instead.
	//
	// This subtest proves the one thing an SSR test can prove about all of
	// that: both target ids exist with the right attributes, in the states
	// where each is expected to (the heading always; the trigger only once
	// Confirmation is set and Loading has cleared -- see the comment on
	// focusOnMount's placement in proposalView for why it must not mount
	// any earlier), and neither leaked onto the List page. The focus MOVE
	// itself is a live claim this file cannot execute; see the PROMOUX-010
	// report for what was measured.
	t.Run("Start's transition targets: the heading stays a script-focusable landmark, and the trigger is what focusOnMount actually targets", func(t *testing.T) {
		if proposeReviewTriggerID != "propose-review-trigger" {
			t.Fatalf("proposeReviewTriggerID = %q, want the derived id proposalFormSection's reviewSurface actually renders", proposeReviewTriggerID)
		}

		heading := regexp.MustCompile(`<h1[^>]*>Promote Peter Tan</h1>`)
		confirmedForm := ProposalForm{
			Action: "/propose", Submit: "Propose and simulate",
			Confirmation: []Fact{{Label: "Employee", Value: "Peter Tan"}},
		}

		loading := mustRender(t, Page{Proposal: &ProposalView{
			Subject: &PromotionSubject{Name: "Peter Tan"}, Loading: true,
		}})
		loaded := mustRender(t, Page{Proposal: &ProposalView{
			Subject: &PromotionSubject{Name: "Peter Tan"}, Form: confirmedForm,
		}})

		// The heading: present and script-focusable in both states, per
		// GREEN's separate screen-reader-announcement need.
		for _, out := range []string{loading, loaded} {
			tag := heading.FindString(out)
			if tag == "" {
				t.Fatalf("proposal page heading not found:\n%s", out)
			}
			if !strings.Contains(tag, `id="`+proposalHeadingID+`"`) {
				t.Fatalf("proposal page heading is missing its id (%q): %s", proposalHeadingID, tag)
			}
			if !strings.Contains(tag, `tabIndex="-1"`) {
				t.Fatalf("proposal page heading is not script-focusable (needs tabindex=-1 since an <h1> is not natively focusable): %s", tag)
			}
		}

		// The trigger: absent while Loading (nothing to focus yet), present
		// with the exact id focusOnMount is given once the review surface
		// has something to guard.
		if strings.Contains(loading, `id="`+proposeReviewTriggerID+`"`) {
			t.Fatalf("the review trigger must not exist yet while the proposal is still loading:\n%s", loading)
		}
		trigger := regexp.MustCompile(`<summary[^>]*id="` + proposeReviewTriggerID + `"[^>]*>`).FindString(loaded)
		if trigger == "" {
			t.Fatalf("loaded proposal page has no review trigger carrying the id focusOnMount targets (%q):\n%s", proposeReviewTriggerID, loaded)
		}

		// The fix is scoped to the proposal page: List's own heading must
		// not have picked up an id or tabindex it never had before.
		listOut := mustRender(t, SampleListPage())
		if strings.Contains(listOut, `id="`+proposalHeadingID+`"`) {
			t.Fatalf("the proposal heading id leaked onto the list page:\n%s", listOut)
		}
		if m := regexp.MustCompile(`<h1[^>]*>`).FindString(listOut); strings.Contains(strings.ToLower(m), "tabindex") {
			t.Fatalf("the list page's own heading unexpectedly became script-focusable: %s", m)
		}
	})

	// The subtest above proves both ids exist in the right states, but not
	// which one focusOnMount is actually wired to move focus to -- that
	// value never reaches rendered markup (focusOnMount renders nil on
	// every platform), so a RenderToString-based assertion cannot see a
	// regression that retargets it back at the heading while leaving both
	// ids present in the document. This is exactly the gap a first draft of
	// this subtest had: it passed unchanged when TargetID was reverted to
	// proposalHeadingID, because the heading and the trigger both still
	// existed somewhere on the page. focusOnMountHook (mount_focus.go) is
	// the seam that closes it: swapped for a spy, it records the actual
	// argument focusOnMount's effect receives.
	t.Run("focusOnMount is wired to the review trigger, not the heading", func(t *testing.T) {
		var got []string
		prev := focusOnMountHook
		focusOnMountHook = func(targetID string) { got = append(got, targetID) }
		defer func() { focusOnMountHook = prev }()

		mustRender(t, Page{Proposal: &ProposalView{
			Subject: &PromotionSubject{Name: "Peter Tan"},
			Form: ProposalForm{
				Action: "/propose", Submit: "Propose and simulate",
				Confirmation: []Fact{{Label: "Employee", Value: "Peter Tan"}},
			},
		}})

		if len(got) != 1 {
			t.Fatalf("focusOnMount's effect ran %d times, want exactly 1: %v", len(got), got)
		}
		if got[0] != proposeReviewTriggerID {
			t.Fatalf("focusOnMount was told to focus %q, want the review trigger %q", got[0], proposeReviewTriggerID)
		}
		if got[0] == proposalHeadingID {
			t.Fatal("focusOnMount is targeting the heading again -- live measurement showed that closes neither RED clause, since the heading already sits inside the viewport")
		}
	})
}
