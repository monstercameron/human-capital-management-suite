package journey

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UXLIVE-032's RED was measured on the live tracker: headings read
// "Amara8CF888 — Open request", gluing the reference to the name, the last
// reference character wrapped onto its own line at 390 px, and the status
// was repeated as a chip without improving the heading.

func uxlive032Card() JourneyCard {
	return JourneyCard{
		IntentID: "01a0b189-e04c-7265-8926-9095318cf888", Href: "#/journeys/01a0b189-e04c-7265-8926-9095318cf888", WorkerName: "Amara",
		Headline: "OPS-HRBP2 · P2 → OPS-HRBP3 · P3", EffectiveDate: "1 Oct 2026", Updated: "18 Sep 2026, 10:00 UTC",
		Stage: "AWAITING_APPROVAL", StageLabel: "Awaiting approval", StageTone: toneWarning,
	}
}

func uxlive032Render(t *testing.T, locale string, card JourneyCard) string {
	t.Helper()
	markup, err := ui.RenderToString(journeyCardLocale(locale, card))
	if err != nil {
		t.Fatalf("render card: %v", err)
	}
	return markup
}

var uxlive032Heading = regexp.MustCompile(`(?s)<h3>(.*?)</h3>`)

// TestTodo_UXLIVE_032 is the primary red/green test: the heading is a human
// task label, the reference is separate secondary metadata, and the status
// is shown once.
func TestTodo_UXLIVE_032(t *testing.T) {
	markup := uxlive032Render(t, "en-US", uxlive032Card())
	heading := uxlive032Heading.FindStringSubmatch(markup)
	if heading == nil {
		t.Fatalf("card has no heading:\n%s", markup)
	}
	if !strings.Contains(heading[1], ">Promotion for Amara</a>") {
		t.Fatalf("heading is not the task label: %s", heading[1])
	}
	visibleHeading := regexp.MustCompile(`aria-label="[^"]*"`).ReplaceAllString(heading[1], "")
	if strings.Contains(visibleHeading, "8CF888") || strings.Contains(visibleHeading, "Awaiting approval") {
		t.Fatalf("the heading's visible text still carries the reference or status: %s", heading[1])
	}
	if !strings.Contains(markup, `<p class="jn-journey-refline"><span class="jn-journey-reflabel">Request</span><span class="jn-journey-ref" dir="ltr" translate="no">8CF888</span>`) {
		t.Fatalf("the reference is not distinct secondary metadata:\n%s", markup)
	}
	if got := strings.Count(markup, ">Awaiting approval<"); got != 1 {
		t.Fatalf("the status is shown %d times, want once:\n%s", got, markup)
	}
}

// TestTodo_UXLIVE_032_Golden pins the identity header and reference line.
func TestTodo_UXLIVE_032_Golden(t *testing.T) {
	const want = `<div class="jn-journey-top"><h3><a aria-label="Promotion for Amara, Request 8CF888, Awaiting approval — Open request" href="#/journeys/01a0b189-e04c-7265-8926-9095318cf888">Promotion for Amara</a></h3><span aria-hidden="true" class="jn-journey-status"><span class="jn-chip" data-tone="warning"><svg aria-hidden="true" class="jn-chip-icon" fill="none" focusable="false" height="18" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24" width="18" xmlns="http://www.w3.org/2000/svg"><path d="M10.3 4.3 2.6 17.5A2 2 0 0 0 4.3 20.5h15.4a2 2 0 0 0 1.7-3L13.7 4.3a2 2 0 0 0-3.4 0Z"></path><path d="M12 10v4"></path><path d="M12 17h.01"></path></svg>Awaiting approval</span></span></div><p class="jn-journey-refline"><span class="jn-journey-reflabel">Request</span><span class="jn-journey-ref" dir="ltr" translate="no">8CF888</span><button aria-label="Copy request reference 8CF888" class="jn-copy-btn" type="button">Copy</button></p>`
	card := uxlive032Card()
	got, err := ui.RenderToString(ui.Fragment(journeyCardHead("en-US", card), journeyCardReference("en-US", card)))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("identity header drifted:\ngot:  %s\nwant: %s", got, want)
	}
}

// TestTodo_UXLIVE_032_Browser keeps the reference whole at every width: it
// is an isolated left-to-right run that never wraps, outside the heading,
// so neither a narrow card nor 200% zoom can split "8CF888".
func TestTodo_UXLIVE_032_Browser(t *testing.T) {
	sheet := Stylesheet()
	rule := regexp.MustCompile(`\.jn-journey-refline \.jn-journey-ref\{[^}]*\}`).FindString(sheet)
	for _, want := range []string{"white-space:nowrap", "direction:ltr"} {
		if !strings.Contains(rule, want) {
			t.Fatalf("the reference rule lacks %s: %q", want, rule)
		}
	}
	if !regexp.MustCompile(`\.jn-journey-ref\{[^}]*unicode-bidi:isolate`).MatchString(sheet) {
		t.Fatal("the reference is no longer bidi-isolated")
	}
	if line := regexp.MustCompile(`\.jn-journey-refline\{[^}]*\}`).FindString(sheet); !strings.Contains(line, "display:flex") || !strings.Contains(line, "flex-wrap:wrap") {
		t.Fatal("the reference line cannot move its label and token as whole items")
	}
	markup := uxlive032Render(t, "en-US", uxlive032Card())
	heading := uxlive032Heading.FindStringSubmatch(markup)
	if heading == nil || strings.Contains(heading[1], "jn-journey-ref") {
		t.Fatalf("the reference is still inside the heading, where it wraps with the name:\n%s", markup)
	}
}

// TestTodo_UXLIVE_032_Accessibility keeps the full identity in the heading
// link's accessible name, hides the duplicate visual status from assistive
// technology, and names the copy control after what it copies.
func TestTodo_UXLIVE_032_Accessibility(t *testing.T) {
	markup := uxlive032Render(t, "en-US", uxlive032Card())
	if !strings.Contains(markup, `aria-label="Promotion for Amara, Request 8CF888, Awaiting approval — Open request"`) {
		t.Fatalf("the heading's accessible name does not carry person, request and state:\n%s", markup)
	}
	if !strings.Contains(markup, `<span aria-hidden="true" class="jn-journey-status">`) {
		t.Fatalf("the visual status is announced a second time:\n%s", markup)
	}
	if !strings.Contains(markup, `aria-label="Copy request reference 8CF888"`) {
		t.Fatalf("the copy control does not name what it copies:\n%s", markup)
	}
	other := uxlive032Card()
	other.IntentID = "01a0b182-28a0-78a5-a93a-8a30a0a89ac8"
	a := regexp.MustCompile(`<h3><a aria-label="([^"]*)"`).FindStringSubmatch(markup)
	b := regexp.MustCompile(`<h3><a aria-label="([^"]*)"`).FindStringSubmatch(uxlive032Render(t, "en-US", other))
	if a == nil || b == nil || a[1] == b[1] {
		t.Fatal("two requests for the same person share an accessible heading name")
	}
}

// TestTodo_UXLIVE_032_I18N renders German and Arabic: the task label and the
// reference label are translated, the Arabic accessible name uses the Arabic
// comma, and the reference token is the same unbroken run in every locale.
func TestTodo_UXLIVE_032_I18N(t *testing.T) {
	for locale, want := range map[string][]string{
		"de-DE": {">Beförderung für Amara</a>", `<span class="jn-journey-reflabel">Antrag</span>`, `aria-label="Beförderung für Amara, Antrag 8CF888, Awaiting approval — Antrag öffnen"`, `aria-label="Antragsreferenz 8CF888 kopieren"`},
		"ar":    {">ترقية Amara</a>", `<span class="jn-journey-reflabel">الطلب</span>`, `aria-label="ترقية Amara، الطلب 8CF888، Awaiting approval — فتح الطلب"`},
	} {
		markup := uxlive032Render(t, locale, uxlive032Card())
		for _, fragment := range want {
			if !strings.Contains(markup, fragment) {
				t.Fatalf("%s card is missing %q:\n%s", locale, fragment, markup)
			}
		}
		if !strings.Contains(markup, `<span class="jn-journey-ref" dir="ltr" translate="no">8CF888</span>`) {
			t.Fatalf("%s card broke or translated the reference token:\n%s", locale, markup)
		}
		if strings.Contains(markup, "⟦") {
			t.Fatalf("%s card has an unresolved catalog key:\n%s", locale, markup)
		}
	}
}

// TestTodo_UXLIVE_032_Regression keeps a card with no intent id exactly
// honest: no invented reference, no copy control, and the heading still
// names the person.
func TestTodo_UXLIVE_032_Regression(t *testing.T) {
	card := uxlive032Card()
	card.IntentID = ""
	markup := uxlive032Render(t, "en-US", card)
	if strings.Contains(markup, "jn-journey-refline") || strings.Contains(markup, "jn-copy-btn") {
		t.Fatalf("a card with no intent id invented a reference:\n%s", markup)
	}
	if !strings.Contains(markup, `aria-label="Promotion for Amara, Awaiting approval — Open request"`) {
		t.Fatalf("the accessible name without a reference is wrong:\n%s", markup)
	}
	nameless := uxlive032Card()
	nameless.WorkerName = " "
	if markup := uxlive032Render(t, "en-US", nameless); !strings.Contains(markup, ">Promotion request</a>") {
		t.Fatalf("a nameless card has no task label:\n%s", markup)
	}
}
