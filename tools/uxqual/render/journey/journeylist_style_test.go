package journey

import (
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The journeys list looked like a wall of text for two reasons, and this
// file pins both so they cannot come back quietly.
//
// The first is that the subject-group markup shipped with no rules at all,
// so its head stacked as three block boxes and every group opened its own
// multi-column grid for the single card it usually holds.
//
// The second is subtler and is the one worth a test: the product type scale
// reached into this embedded page through
// `:where(.app-shell,.jn-embedded) :is(.prose,.prose p,...,p,li,dd,dt,...)`.
// :where() scores nothing, but :is() takes the specificity of its most
// specific argument, and `.prose p` made the whole thing (0,1,1) -- more
// than a plain class here. Every paragraph on a card was therefore set at
// body size no matter what this stylesheet asked for.
//
// That rule has since been split product-wide, so its bare-element half
// scores nothing and all 61 components it was flattening are heard again.
// The scoped rules this file pins remain deliberately: the card's scale is
// what the page is read by, and pinning it means a future rule cannot
// flatten it again by accident.

func journeyListSheet(t *testing.T) string {
	t.Helper()
	return Stylesheet()
}

// remOf reads a rule's font-size in rem, or 0 when it declares none.
func remOf(rule string) float64 {
	at := strings.Index(rule, "font-size:")
	if at < 0 {
		return 0
	}
	value := rule[at+len("font-size:"):]
	if end := strings.IndexAny(value, ";}"); end >= 0 {
		value = value[:end]
	}
	value = strings.TrimSuffix(strings.TrimSpace(value), "rem")
	out, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return out
}

// ruleFor returns the declarations of the last rule whose selector is
// exactly sel, which is the one that wins on source order.
func ruleFor(sheet, sel string) string {
	out := ""
	for _, chunk := range strings.Split(sheet, "}") {
		open := strings.Index(chunk, "{")
		if open < 0 {
			continue
		}
		if strings.TrimSpace(chunk[:open]) == sel {
			out = strings.TrimSpace(chunk[open+1:])
		}
	}
	return out
}

// TestJourneyCardTypeScaleOutranksTheProductScale is the specificity
// contract. A plain class here loses to the embedding page's own paragraph
// rule, so each size the card depends on is scoped to the page it belongs
// to. Scoping is what makes it win: (0,2,0) over (0,1,1), with no
// !important and without touching the product scale.
func TestJourneyCardTypeScaleOutranksTheProductScale(t *testing.T) {
	sheet := journeyListSheet(t)
	// Sizes are compared as numbers, not as the text that spells them:
	// ".875rem" and "0.875rem" are the same size, and a test that can tell
	// them apart fails for reasons no reader could see.
	for sel, size := range map[string]float64{
		":is(.jn-page,.jn-embedded) .jn-journey-headline": .875,
		":is(.jn-page,.jn-embedded) .jn-journey-pay":      1.125,
		":is(.jn-page,.jn-embedded) .jn-meta":             .8125,
		":is(.jn-page,.jn-embedded) .jn-journey-next":     .8125,
		":is(.jn-page,.jn-embedded) .jn-journey-foot":     .8125,
	} {
		rule := ruleFor(sheet, sel)
		if rule == "" {
			t.Errorf("%s has no rule; its paragraphs fall back to body size and the card loses its hierarchy", sel)
			continue
		}
		if got := remOf(rule); got != size {
			t.Errorf("%s = %q (%grem), want %grem", sel, rule, got, size)
		}
	}

	// The card's own sizes have to differ from each other, or there is no
	// hierarchy to protect in the first place.
	headline := remOf(ruleFor(sheet, ":is(.jn-page,.jn-embedded) .jn-journey-headline"))
	pay := remOf(ruleFor(sheet, ":is(.jn-page,.jn-embedded) .jn-journey-pay"))
	meta := remOf(ruleFor(sheet, ":is(.jn-page,.jn-embedded) .jn-meta"))
	if !(pay > headline && headline > meta) {
		t.Fatalf("the card has no size hierarchy: pay %grem, role change %grem, dates %grem", pay, headline, meta)
	}
	// The pay figure has to outrank the card title too, or the loudest fact
	// on the card is whatever happens to be at the top of it.
	if title := remOf(ruleFor(sheet, ".jn-journey h3")); title > 0 && pay <= title {
		t.Fatalf("the pay figure (%grem) does not outrank the card title (%grem)", pay, title)
	}

	// The same product rule caps every paragraph at the prose measure, which
	// is a reading width for running text, not for a card.
	if !strings.Contains(ruleFor(sheet, ":is(.jn-page,.jn-embedded) .jn-journey :is(p,li,dd,dt)"), "max-inline-size:none") {
		t.Fatalf("a card's paragraphs are still capped at the prose measure")
	}
}

// TestJourneySubjectGroupsAreLaidOut pins the layout the group markup always
// assumed: a head that reads as one line, and groups that flow into the
// width a card wants instead of one per row.
func TestJourneySubjectGroupsAreLaidOut(t *testing.T) {
	sheet := journeyListSheet(t)

	head := ruleFor(sheet, ".jn-journey-group-head")
	if !strings.Contains(head, "display:flex") {
		t.Fatalf("the group head still stacks its subject, count and statuses as blocks: %q", head)
	}
	if !strings.Contains(head, "align-items:baseline") {
		t.Fatalf("the group head does not sit its parts on one baseline: %q", head)
	}

	groups := ruleFor(sheet, ".jn-journey-groups")
	if !strings.Contains(groups, "grid-template-columns:repeat(auto-fill") {
		t.Fatalf("groups still take one row each, whatever they hold: %q", groups)
	}
	// A subject with several requests needs the whole row so its own cards
	// can sit beside each other.
	span := ruleFor(sheet, ".jn-journey-group:has(.jn-griditem+.jn-griditem)")
	if !strings.Contains(span, "grid-column:1/-1") {
		t.Fatalf("a multi-request subject does not take the full row: %q", span)
	}
}

// TestJourneyCardControlsSitAboveTheStretchedLink is the interaction the
// card's own title quietly breaks. The title is a stretched link whose
// overlay covers the whole card, so any control below it in paint order is
// unreachable -- a click on "Technical details" opens the request instead.
func TestJourneyCardControlsSitAboveTheStretchedLink(t *testing.T) {
	sheet := journeyListSheet(t)

	// The overlay this guards against is still there; without it the rule
	// below would be protecting nothing.
	if !strings.Contains(sheet, ".jn-journey h3 a::after{") {
		t.Fatalf("the card no longer stretches its title link; this test is guarding a hazard that is gone")
	}
	rule := ruleFor(sheet, ".jn-journey-technical,.jn-journey-technical summary,.jn-journey .jn-copy-btn")
	if !strings.Contains(rule, "position:relative") || !strings.Contains(rule, "z-index:1") {
		t.Fatalf("the card's controls sit under the title's overlay: %q", rule)
	}
}

// TestJourneyCardSeparatesFactsWithoutAddingWords keeps the card's dividers
// out of the accessibility tree. A card is narrow enough that its dates wrap
// often, and both a punctuation glyph and a border behave badly there -- one
// is announced, the other draws a rule at the start of a line with nothing
// before it.
func TestJourneyCardSeparatesFactsWithoutAddingWords(t *testing.T) {
	sheet := journeyListSheet(t)

	if strings.Contains(sheet, ".jn-journey>.jn-meta .jn-meta-item+.jn-meta-item{") {
		t.Fatalf("the wrapping meta row is divided by a border again")
	}
	meta := ruleFor(sheet, ".jn-journey>.jn-meta")
	if !strings.Contains(meta, "column-gap") {
		t.Fatalf("the meta facts have nothing separating them at all: %q", meta)
	}

	// Nothing on this card may carry text as generated content, which a
	// screen reader reads as part of the element's name.
	for _, chunk := range strings.Split(sheet, "}") {
		open := strings.Index(chunk, "{")
		if open < 0 || !strings.Contains(chunk[:open], "jn-journey") {
			continue
		}
		// `content:"` and not `content:`: justify-content and align-content
		// both end in the same eight characters, and only a string value can
		// put words into the accessibility tree.
		body := chunk[open+1:]
		if !strings.Contains(body, `content:"`) {
			continue
		}
		if !strings.Contains(body, `content:""`) {
			t.Errorf("%s writes text as generated content, which is announced: %q", strings.TrimSpace(chunk[:open]), body)
		}
	}
}

// TestJourneyCardOrdersItsPartsForAScreenReader keeps the reading order the
// same as the visual one. The card is scanned, not read, so its parts are
// set at different sizes -- but a size is not an order, and anyone who
// cannot see the sizes gets whatever the markup says.
func TestJourneyCardOrdersItsPartsForAScreenReader(t *testing.T) {
	markup, err := ui.RenderToString(journeyCardLocale("en-US", JourneyCard{
		IntentID: "01a0b26b-9bb0-7ac6-84e4-8cbb4cf54f9d", WorkerName: "Adrian",
		Stage: "PROPOSED", StageLabel: "Ready to start approval", StageTone: toneNeutral,
		Headline: "SAL-AE3 · P4 → SAL-DIR · M4", PayLine: "USD 135,000.00 → 165,000.00 (+22.2%)",
		EffectiveDate: "1 Dec 2026", Updated: "18 Sep 2026, 02:49 UTC", NextStep: "Start approval",
	}))
	if err != nil {
		t.Fatalf("render card: %v", err)
	}
	order := []string{"Adrian", "Ready to start approval", "SAL-AE3", "USD 135,000.00", "Effective", "Updated", "Next step"}
	at := -1
	for _, part := range order {
		next := strings.Index(markup, part)
		if next < 0 {
			t.Fatalf("the card no longer carries %q:\n%s", part, markup)
		}
		if next < at {
			t.Fatalf("%q comes before something that should precede it; the markup order no longer matches the visual one", part)
		}
		at = next
	}
	// The stage is a chip with a dot, and the dot is decoration. The label
	// beside it is what carries the status, so the status is never colour
	// alone.
	if !strings.Contains(markup, `aria-hidden="true"`) || !strings.Contains(markup, "Ready to start approval") {
		t.Fatalf("the stage is not stated in text beside its dot:\n%s", markup)
	}
}
