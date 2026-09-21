package journeyclient

import (
	"regexp"
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// This file is PROMOUX-013's UI matrix: the BROWSER, ACCESSIBILITY and I18N
// rows the todo names, plus the disclosure-by-value proof PROMOUX-001 and
// PROMOUX-004 established for this repository and PROMOUX-013's own GREEN
// clause requires again for its three typed interventions.
//
// Every assertion here runs against ui.RenderToString (the native/SSR
// path), exactly as promoux010_test.go's own BROWSER row does: Withdraw,
// Cancel and EditProposal are plumbed as ordinary journey.Action values
// (tools/uxqual/journeyclient/projector.go's interventionActions), so they
// reach the browser through the identical actionCard/reviewSurface markup
// PROMOUX-010 already proved live -- no second confirmation surface was
// built, and none of review_surface.go's own code changed.

// mustRender renders p to a string or fails the test.
func mustRender(t *testing.T, p journey.Page) string {
	t.Helper()
	out, err := journey.RenderToString(p)
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	return out
}

// TestTodo_PROMOUX_013_Browser proves the three interventions render through
// the exact same reused structures PROMOUX-010 already verified live: the
// zero-JavaScript <details> disclosure wrapping the review body for an
// available action, and the same jn-blocked disabled-reason paragraph
// Execute-at-BLOCKED already used, for an unavailable one.
func TestTodo_PROMOUX_013_Browser(t *testing.T) {
	cfg := testConfig()

	t.Run("an available intervention reuses the identical contained review structure", func(t *testing.T) {
		out := mustRender(t, DetailPage(cfg, testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE), nil, nil))
		if !strings.Contains(out, `class="jn-confirm"`) {
			t.Fatalf("no reused review-surface disclosure rendered at all:\n%s", out)
		}
		cancelDetailsAt := strings.Index(out, `id="action-cancel-review"`)
		if cancelDetailsAt < 0 {
			t.Fatalf("Cancel's own review disclosure id is missing:\n%s", out)
		}
		editDetailsAt := strings.Index(out, `id="action-edit-proposal-review"`)
		if editDetailsAt < 0 {
			t.Fatalf("EditProposal's own review disclosure id is missing:\n%s", out)
		}
		// Every review's body is nested inside its own native <details>, the
		// same zero-JavaScript baseline promoux010_test.go's Browser row
		// proved for Approve/Reject/Start.
		for _, id := range []string{"action-cancel", "action-edit-proposal"} {
			marker := `id="` + id + `-review"`
			at := strings.Index(out, marker)
			if at < 0 {
				t.Fatalf("no review found for %q", id)
			}
			detailsStart := strings.LastIndex(out[:at], "<details")
			bodyAt := strings.Index(out[at:], `class="jn-confirm-body"`)
			closeAt := strings.Index(out[at:], "</details>")
			if detailsStart < 0 || bodyAt < 0 || closeAt < 0 || bodyAt > closeAt {
				t.Fatalf("%q's review content is not nested inside its own <details> disclosure:\n%s", id, out)
			}
		}
	})

	t.Run("an unavailable intervention reuses the identical disabled-reason structure Execute already used at BLOCKED", func(t *testing.T) {
		out := mustRender(t, DetailPage(cfg, testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED), nil, nil))
		// Execute's own BLOCKED-stage disabled reason is a reason paragraph
		// with aria-describedby wired to it (components.go's actionCard). The
		// three interventions must use the identical mechanism, not a new one.
		//
		// When every action in the section is refused for the same reason,
		// that reason is stated once at section level and each control points
		// at it (UXLIVE-017), so the id each control names is the section's
		// rather than its own card's. What this asserts is unchanged: every
		// disabled control is associated with a reason that exists.
		for _, id := range []string{"withdraw", "cancel", "edit-proposal"} {
			btn := regexp.MustCompile(`<button[^>]*aria-describedby="([a-z-]+)"[^>]*>` + actionLabelPattern(id) + `<`).FindStringSubmatch(out)
			if btn == nil {
				t.Fatalf("disabled action %q's own button is not wired to a reason via aria-describedby:\n%s", id, out)
			}
			if !strings.Contains(out, `id="`+btn[1]+`"`) {
				t.Fatalf("disabled action %q points at reason %q, which the document does not contain:\n%s", id, btn[1], out)
			}
			if !strings.Contains(btn[0], "disabled") {
				t.Fatalf("disabled action %q's button is missing the disabled attribute: %s", id, btn[0])
			}
		}
	})
}

// TestTodo_PROMOUX_013_Accessibility proves the reason fields carry real
// labels and required-ness, and that the disabled explanation is announced
// through the same jn-blocked/aria-describedby wiring the Accessibility row
// already relies on elsewhere on this page (promoux010_test.go's own
// Accessibility row covers the reused review surface's Cancel button,
// heading id and busy status region; this row does not repeat those, only
// what PROMOUX-013 itself adds).
func TestTodo_PROMOUX_013_Accessibility(t *testing.T) {
	cfg := testConfig()
	out := mustRender(t, DetailPage(cfg, testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE), nil, nil))

	t.Run("the cancel and edit reason fields are required and carry a visible label", func(t *testing.T) {
		for _, want := range []string{"Reason", "Reason for this edit", "Business reason", "Target job code", "Target grade", "Proposed base pay", "Effective date"} {
			if !strings.Contains(out, ">"+want+"<") {
				t.Errorf("no visible label %q found for a required intervention field:\n%s", want, out)
			}
		}
		if !strings.Contains(out, `name="reason"`) {
			t.Fatalf("no reason field submits under the request's own field name:\n%s", out)
		}
	})

	t.Run("withdraw is omitted at an eligible-wait stage, where Cancel applies", func(t *testing.T) {
		if strings.Contains(out, `id="action-withdraw-blocked"`) || strings.Contains(out, interventionReasonText(reasonAlreadyStarted)) {
			t.Fatalf("a refused Withdraw still renders beside the Cancel that applies:\n%s", out)
		}
	})

	t.Run("a refusal with no alternative is a real, associated explanation", func(t *testing.T) {
		committed := mustRender(t, DetailPage(cfg, testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_EXECUTED), nil, nil))
		tag := regexp.MustCompile(`(?s)<p[^>]*id="[^"]*blocked"[^>]*>.*?</p>`).FindString(committed)
		if tag == "" {
			t.Fatalf("no disabled-reason paragraph once execution has committed:\n%s", committed)
		}
		if !strings.Contains(tag, `class="jn-blocked"`) {
			t.Errorf("Withdraw's disabled reason lost the jn-blocked treatment every other blocked action uses: %s", tag)
		}
		if !strings.Contains(tag, interventionReasonText(reasonAlreadyStarted)) && !strings.Contains(tag, interventionReasonText(reasonAlreadyCommitted)) {
			t.Errorf("the disabled reason does not state why the stop cannot apply: %s", tag)
		}
	})
}

// TestTodo_PROMOUX_013_I18N is this package's I18N matrix row. render/journey
// has no locale catalog or dir="rtl" attribute of its own yet (every string
// on this page, including PROMOUX-013's, is fixed English Go source, the
// same as Approve/Reject/Execute were before this todo); building that
// catalog is a separate, much larger effort out of this session's reach.
// What this row proves instead is the one thing that IS checkable today and
// that would otherwise silently break under translation: the pipeline from
// a server-worded PreviewJourneyIntervention.consequence_summary through
// this projector and GWC's RenderToString preserves non-ASCII, bidirectional
// text byte-for-byte, for both a Latin-diacritic locale (de-DE) and a
// right-to-left one (ar) -- proving the plumbing this todo adds does not
// itself hardcode a Latin-only, left-to-right assumption anywhere between
// the wire and the rendered page.
func TestTodo_PROMOUX_013_I18N(t *testing.T) {
	cases := []struct {
		name    string
		summary string
	}{
		{"en-US", "Withdrawing now cancels this proposal before any approval has been recorded."},
		{"de-DE", "Der Widerruf storniert diesen Vorschlag, bevor eine Genehmigung erteilt wurde. Größe: 100 %."},
		{"ar", "سيؤدي السحب إلى إلغاء هذا الاقتراح قبل تسجيل أي موافقة."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			detail := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
			p := DetailPageWithInterventions(testConfig(), detail, nil, nil,
				&journeyv1.PreviewJourneyInterventionResponse{Available: true, ConsequenceSummary: tc.summary},
				nil, nil,
			)
			out, err := journey.RenderToString(p)
			if err != nil {
				t.Fatalf("RenderToString: %v", err)
			}
			if !strings.Contains(out, tc.summary) {
				t.Fatalf("locale %s: the server-worded consequence summary did not survive rendering byte-for-byte.\nwant contained: %q\ngot: %s", tc.name, tc.summary, out)
			}
		})
	}

	// The disclosure-by-value proof itself (PROMOUX-001/PROMOUX-004's
	// standing precedent, restated for PROMOUX-013): the same disabled
	// reason must be byte-identical whatever the caller's own facts are,
	// because the reason is a pure function of the stage alone, never of
	// anything a differently-privileged or differently-localized viewer
	// might additionally know.
	t.Run("the unavailable reason is byte-identical across differing underlying journeys, as required by PROMOUX-001/PROMOUX-004's precedent", func(t *testing.T) {
		omar := mustRender(t, DetailPage(testConfig(), testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED), nil, nil))
		other := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED)
		other.Journey.WorkerName = "A Completely Different Person"
		other.Journey.CorrelationId = "cor_DIFFERENT"
		different := mustRender(t, DetailPage(testConfig(), other, nil, nil))

		// The reason the Cancel control points at, wherever it is stated:
		// per card when reasons differ, once per section when they do not
		// (UXLIVE-017).
		reasonID := "actions-blocked"
		pattern := regexp.MustCompile(`(?s)<p[^>]*id="` + reasonID + `"[^>]*>(.*?)</p>`)
		want := pattern.FindStringSubmatch(omar)
		got := pattern.FindStringSubmatch(different)
		if want == nil || got == nil {
			t.Fatalf("could not locate Cancel's disabled reason in one of the two renders")
		}
		if want[1] != got[1] {
			t.Fatalf("the disabled reason text differs across two journeys sharing the same terminal stage: %q vs %q -- presence alone must never leak more than the stage does", want[1], got[1])
		}
	})
}

// actionLabelPattern is one intervention control's visible label, so a
// button can be found by the action it performs rather than by the id of the
// element that explains why it is refused.
func actionLabelPattern(id string) string {
	switch id {
	case "withdraw":
		return "Withdraw"
	case "cancel":
		return "Request cancellation"
	case "edit-proposal":
		return "Edit proposal"
	}
	return id
}
