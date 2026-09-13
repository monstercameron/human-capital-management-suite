package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// unauthorizedTechnicalCard is fixtureSubject with every technical
// identifier populated but DiagnosticsAuthorized false: the shape an
// ordinary, non-diagnostics viewer's projector hands the renderer once the
// server has withheld nothing extra, only decided this viewer may not see
// it disclosed.
func unauthorizedTechnicalCard() JourneyCard {
	c := fixtureSubject()
	c.DiagnosticsAuthorized = false
	return c
}

// TestTodo_PROMOUX_008 is the PRIMARY matrix test. It proves the rendering
// rule PROMOUX-008 rests on: the Technical details disclosure -- on the
// journeys list card, on the detail hero, and the workflow/outcome/evidence
// panels on the detail page -- exists in the markup if and only if
// DiagnosticsAuthorized is true, never because a raw identifier happens to
// be non-empty; and when it does exist, every identifier it shows is
// redacted on screen and carries its own copy control rather than the raw
// value.
func TestTodo_PROMOUX_008(t *testing.T) {
	t.Run("unauthorized card discloses nothing despite full identifiers", func(t *testing.T) {
		out := mustRenderNode(t, journeyCard(unauthorizedTechnicalCard()))
		for _, forbidden := range []string{"jn-journey-technical", "Technical details", "worker:NW-40118", fixtureInstanceID} {
			if strings.Contains(out, forbidden) {
				t.Fatalf("unauthorized journey card leaked %q:\n%s", forbidden, out)
			}
		}
	})

	t.Run("authorized card discloses redacted identifiers with copy controls", func(t *testing.T) {
		out := mustRenderNode(t, journeyCard(fixtureSubject()))
		if !strings.Contains(out, `class="jn-journey-technical"`) || !strings.Contains(out, "Technical details") {
			t.Fatal("authorized journey card did not render the disclosure")
		}
		if strings.Contains(out, "worker:NW-40118") {
			t.Fatal("authorized card rendered the raw worker reference instead of a redacted form")
		}
		if !strings.Contains(out, maskIdentifier("worker:NW-40118")) {
			t.Fatal("authorized card did not render the redacted worker reference")
		}
		if !strings.Contains(out, `class="jn-copy-btn"`) || !strings.Contains(out, `type="button"`) || !strings.Contains(out, ">Copy<") {
			t.Fatal("authorized card did not render a copy control")
		}
	})

	t.Run("unauthorized hero discloses nothing", func(t *testing.T) {
		card := unauthorizedTechnicalCard()
		card.IntentID = "int-should-not-leak"
		out := mustRenderNode(t, heroSection(card))
		for _, forbidden := range []string{"jn-journey-technical", "Technical details", "int-should-not-leak", "worker:NW-40118", fixtureInstanceID} {
			if strings.Contains(out, forbidden) {
				t.Fatalf("unauthorized hero leaked %q:\n%s", forbidden, out)
			}
		}
	})

	t.Run("authorized hero discloses all three redacted identifiers", func(t *testing.T) {
		card := fixtureSubject()
		card.IntentID = "int-authorized-visible"
		out := mustRenderNode(t, heroSection(card))
		for _, label := range []string{">Worker </span>", ">Intent </span>", ">Instance </span>"} {
			if !strings.Contains(out, label) {
				t.Fatalf("authorized hero disclosure is missing the %q row", label)
			}
		}
		if strings.Contains(out, "int-authorized-visible") {
			t.Fatal("authorized hero rendered the raw intent id instead of a redacted form")
		}
	})

	t.Run("unauthorized detail page omits the diagnostic panels but keeps the business action", func(t *testing.T) {
		p := SampleDetailPage()
		p.Detail.Journey.DiagnosticsAuthorized = false
		out := mustRender(t, p)
		for _, forbidden := range []string{`id="workflow-heading"`, `id="outcome-heading"`, `id="evidence-heading"`, "jn-journey-technical"} {
			if strings.Contains(out, forbidden) {
				t.Fatalf("unauthorized detail page still renders %q", forbidden)
			}
		}
		if !strings.Contains(out, `id="actions-heading"`) {
			t.Fatal("unauthorized detail page lost the business actions section, which does not depend on diagnostics authority")
		}
	})

	t.Run("maskIdentifier redacts everything but the last four characters", func(t *testing.T) {
		cases := map[string]string{
			"":                  "",
			"ab":                "••",
			"eref:v1:t:w:12345": "••••2345",
		}
		for in, want := range cases {
			if got := maskIdentifier(in); got != want {
				t.Errorf("maskIdentifier(%q) = %q, want %q", in, got, want)
			}
		}
	})
}

// mustRenderNode renders a single component subtree (as opposed to a whole
// Page) and fails the test on error.
func mustRenderNode(t *testing.T, n ui.Node) string {
	t.Helper()
	out, err := ui.RenderToString(n)
	if err != nil {
		t.Fatalf("rendering the node: %v", err)
	}
	return out
}

// TestTodo_PROMOUX_008_Accessibility is the ACCESSIBILITY matrix test. It
// proves the authorized disclosure is a native, keyboard-operable
// <details>/<summary> pair (no bespoke show/hide script an assistive
// technology or keyboard-only user could be locked out of), and that every
// copy control carries its own distinct accessible name -- "Copy" alone,
// repeated three times with no other context, would be indistinguishable to
// a screen reader.
func TestTodo_PROMOUX_008_Accessibility(t *testing.T) {
	out := mustRenderNode(t, heroSection(fixtureSubject()))

	if !strings.Contains(out, "<details") || !strings.Contains(out, "<summary") {
		t.Fatal("the disclosure is not a native <details>/<summary> pair, so it is not keyboard-operable without extra script")
	}
	for _, name := range []string{`aria-label="Copy Worker value"`, `aria-label="Copy Intent value"`, `aria-label="Copy Instance value"`} {
		if !strings.Contains(out, name) {
			t.Fatalf("missing distinct accessible name %q; every copy control must name what it copies", name)
		}
	}
	if strings.Count(out, ">Copy<") < 3 {
		t.Fatal("expected one visible Copy label per disclosed identifier")
	}
	// The redacted value is still associated with its label through plain
	// adjacent text (the jn-meta-key span read immediately before it), not
	// through color or layout alone, so it remains meaningful when CSS or
	// color perception is unavailable.
	if !strings.Contains(out, `<span class="jn-meta-key">Worker </span><span class="jn-meta-value jn-mono">`) {
		t.Fatal("the redacted value is not textually associated with its label")
	}
}

// TestTodo_PROMOUX_008_Golden is the GOLDEN matrix test. It pins the exact
// bytes an unauthorized viewer's journey card and detail hero render to:
// no <details>, no "Technical details" text, no copy control, nothing at
// all past the last business fact (Updated). The pinned strings below were
// produced by this same journeyCard/heroSection call and read in full
// before being pinned, per this repository's golden-test discipline.
func TestTodo_PROMOUX_008_Golden(t *testing.T) {
	const wantCard = `<article class="jn-card jn-journey" data-stage="AWAITING_APPROVAL"><div class="jn-journey-top"><h3><a href="/workspace/journeys/int_01JX6Y8B2C7D9EFG">Omar Reyes<span class="jn-visually-hidden"> — open this journey</span></a></h3><span class="jn-chip" data-tone="warning"><svg aria-hidden="true" class="jn-chip-icon" fill="none" focusable="false" height="18" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24" width="18" xmlns="http://www.w3.org/2000/svg"><path d="M10.3 4.3 2.6 17.5A2 2 0 0 0 4.3 20.5h15.4a2 2 0 0 0 1.7-3L13.7 4.3a2 2 0 0 0-3.4 0Z"></path><path d="M12 10v4"></path><path d="M12 17h.01"></path></svg>Awaiting approval</span></div><p class="jn-journey-headline">OPS-HRBP2 · P2 → OPS-HRBP3 · P3</p><p class="jn-journey-pay">USD 93,000.00 → 98,000.00 (+5.4%)</p><p class="jn-meta"><span class="jn-meta-item"><span class="jn-meta-key">Effective </span><span class="jn-meta-value">1 Jun 2026</span></span><span class="jn-meta-item"><span class="jn-meta-key">Updated </span><span class="jn-meta-value">12 May 2026, 09:12 UTC</span></span></p><p aria-hidden="true" class="jn-journey-foot">Open journey<svg aria-hidden="true" class="jn-journey-arrow" fill="none" focusable="false" height="16" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24" width="16" xmlns="http://www.w3.org/2000/svg"><path d="M5 12h14"></path><path d="m13 6 6 6-6 6"></path></svg></p></article>`
	const wantHero = `<section aria-labelledby="journey-heading" class="jn-panel jn-hero"><div class="jn-hero-top"><div><p class="jn-eyebrow">Promotion journey</p><h1 class="jn-display" id="journey-heading">Omar Reyes</h1><p class="jn-lead">OPS-HRBP2 · P2 → OPS-HRBP3 · P3</p></div><span class="jn-chip" data-tone="warning"><svg aria-hidden="true" class="jn-chip-icon" fill="none" focusable="false" height="18" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24" width="18" xmlns="http://www.w3.org/2000/svg"><path d="M10.3 4.3 2.6 17.5A2 2 0 0 0 4.3 20.5h15.4a2 2 0 0 0 1.7-3L13.7 4.3a2 2 0 0 0-3.4 0Z"></path><path d="M12 10v4"></path><path d="M12 17h.01"></path></svg>Awaiting approval</span></div><p class="jn-hero-pay">USD 93,000.00 → 98,000.00 (+5.4%)</p><p class="jn-meta jn-hero-ids"><span class="jn-meta-item"><span class="jn-meta-key">Effective </span><span class="jn-meta-value">1 Jun 2026</span></span><span class="jn-meta-item"><span class="jn-meta-key">Updated </span><span class="jn-meta-value">12 May 2026, 09:12 UTC</span></span></p></section>`

	if got := mustRenderNode(t, journeyCard(unauthorizedTechnicalCard())); got != wantCard {
		t.Fatalf("unauthorized journey card drifted from its pinned golden:\ngot:  %s\nwant: %s", got, wantCard)
	}
	if got := mustRenderNode(t, heroSection(unauthorizedTechnicalCard())); got != wantHero {
		t.Fatalf("unauthorized hero drifted from its pinned golden:\ngot:  %s\nwant: %s", got, wantHero)
	}
}
