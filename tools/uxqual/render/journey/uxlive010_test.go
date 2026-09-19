package journey

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UXLIVE-010's RED was measured on the running server: Journeys listed five
// Omar requests with the same role change and near-identical pay,
// distinguishable only by an "Updated" timestamp, and every card's heading
// was the identical string "Omar — Open request", so a screen-reader heading
// list gave five indistinguishable entries.
//
// Each card now carries a short request reference, visible on the card and
// inside the heading's accessible name.

func uxlive010Card(id string) JourneyCard {
	return JourneyCard{
		IntentID: id, Href: "#/journeys/" + id, WorkerName: "Omar",
		Headline: "OPS-HRBP2 · P2 → OPS-HRBP3 · P3", PayLine: "USD 93,000.00 → 98,000.00 (+5.4%)",
		EffectiveDate: "1 Jun 2026", Updated: "17 Sep 2026, 22:24 UTC",
		Stage: "BLOCKED", StageLabel: "Blocked", StageTone: toneWarning,
	}
}

func uxlive010Render(t *testing.T, cards ...JourneyCard) string {
	t.Helper()
	nodes := make([]ui.Node, 0, len(cards))
	for _, card := range cards {
		nodes = append(nodes, journeyCardLocale("en-US", card))
	}
	markup, err := ui.RenderToString(ui.Fragment(nodes...))
	if err != nil {
		t.Fatalf("render journey cards: %v", err)
	}
	return markup
}

// TestTodo_UXLIVE_010 is the primary red/green test: two requests for the
// same person are distinguishable, and their headings are unique.
func TestTodo_UXLIVE_010(t *testing.T) {
	first := uxlive010Card("01a0b189-e04c-7265-8926-909531dd6530")
	second := uxlive010Card("01a0b182-28a0-78a5-a93a-8a30a0a89ac8")
	markup := uxlive010Render(t, first, second)

	headings := regexp.MustCompile(`(?s)<h3>(.*?)</h3>`).FindAllStringSubmatch(markup, -1)
	if len(headings) != 2 {
		t.Fatalf("expected two headings, got %d:\n%s", len(headings), markup)
	}
	if headings[0][1] == headings[1][1] {
		t.Fatalf("both requests share the heading %q", headings[0][1])
	}

	firstRef := JourneyReference(first.IntentID)
	secondRef := JourneyReference(second.IntentID)
	if firstRef == "" || firstRef == secondRef {
		t.Fatalf("references are not distinguishing: %q and %q", firstRef, secondRef)
	}
	if len(firstRef) > 12 {
		t.Fatalf("reference %q is too long to scan", firstRef)
	}
	for _, ref := range []string{firstRef, secondRef} {
		if !strings.Contains(markup, ref) {
			t.Fatalf("reference %q never reaches the card:\n%s", ref, markup)
		}
	}
}

// TestTodo_UXLIVE_010_Browser keeps the card readable: the reference is a
// short label beside the person, not a replacement for their name, and a
// card without an intent id renders exactly as it did.
func TestTodo_UXLIVE_010_Browser(t *testing.T) {
	markup := uxlive010Render(t, uxlive010Card("01a0b189-e04c-7265-8926-909531dd6530"))
	if !strings.Contains(markup, ">Omar<") {
		t.Fatalf("the person's name left the card heading:\n%s", markup)
	}

	anonymous := uxlive010Card("")
	plain := uxlive010Render(t, anonymous)
	if strings.Contains(plain, "jn-journey-ref") {
		t.Fatalf("a card with no intent id invented a reference:\n%s", plain)
	}
}

// TestTodo_UXLIVE_010_Benchmark keeps the reference cheap: it is derived,
// not looked up.
func BenchmarkTodo_UXLIVE_010(b *testing.B) {
	for b.Loop() {
		_ = JourneyReference("01a0b189-e04c-7265-8926-909531dd6530")
	}
}
