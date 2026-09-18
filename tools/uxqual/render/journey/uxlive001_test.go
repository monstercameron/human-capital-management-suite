package journey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UXLIVE-001's RED was measured on the running server: three journeys that
// changed no employee record -- a FAILED run whose finance review was
// cancelled, a BLOCKED run whose revalidation refused, and a
// REPAIR_REQUIRED run -- each rendered the same positive panel:
//
//	Recorded outcome                      [Recorded]
//	Result             Promotion outcome recorded
//	Effective date     1 Jun 2026
//	Outcome recorded   2026-09-17 22:24 UTC
//
// The panel's only input was "is there a terminal ledger record", and a
// terminal record exists for every terminal outcome, including the ones
// that recorded a refusal. The fix carries the outcome's own value onto
// the card so the panel states what was recorded rather than assuming it
// was a promotion.

func uxlive001Ledger() *LedgerCard {
	return &LedgerCard{
		StreamKey:      "worker:NW-40118/promotion",
		Sequence:       "1",
		SchemaRef:      "hcmnext.workflow.PromotionOutcome/v2",
		Digest:         "sha256:e0a37175e60c83c6",
		IdempotencyKey: "execute:01a0b178:29d691b2",
		RecordedAt:     "17 Sep 2026, 22:24 UTC",
		EffectiveAt:    "1 Jun 2026",
	}
}

func uxlive001Markup(t *testing.T, card *LedgerCard) string {
	t.Helper()
	markup, err := ui.RenderToString(outcomeSectionLocale("en-US", card, ""))
	if err != nil {
		t.Fatalf("render outcome section: %v", err)
	}
	return markup
}

// TestTodo_UXLIVE_001 is the primary red/green test: a terminal record that
// did not record the promotion must not be presented as one.
func TestTodo_UXLIVE_001(t *testing.T) {
	card := uxlive001Ledger()
	card.Recorded = false
	card.StatusLabel = "Failed"
	card.StatusTone = toneDanger

	markup := uxlive001Markup(t, card)

	if strings.Contains(markup, "Promotion outcome recorded") {
		t.Fatalf("a run that recorded no promotion still claims one:\n%s", markup)
	}
	if !strings.Contains(markup, "Promotion not recorded") {
		t.Fatalf("outcome panel does not state that no promotion was recorded:\n%s", markup)
	}
	if strings.Contains(markup, "Effective date") || strings.Contains(markup, "1 Jun 2026") {
		t.Fatalf("outcome panel dates a change that never took effect:\n%s", markup)
	}
	if !strings.Contains(markup, "Failed") {
		t.Fatalf("outcome panel drops the terminal status it is describing:\n%s", markup)
	}
	if !strings.Contains(markup, `data-tone="danger"`) {
		t.Fatalf("outcome panel keeps a non-danger tone for a failed run:\n%s", markup)
	}
	if strings.Contains(markup, `data-tone="success"`) {
		t.Fatalf("outcome panel still carries a success tone:\n%s", markup)
	}
	if !strings.Contains(markup, "17 Sep 2026, 22:24 UTC") {
		t.Fatalf("outcome panel drops the time the outcome was recorded:\n%s", markup)
	}

	recorded := uxlive001Ledger()
	recorded.Recorded = true
	recorded.StatusLabel = "Recorded"
	recorded.StatusTone = toneSuccess

	recordedMarkup := uxlive001Markup(t, recorded)
	for _, want := range []string{"Promotion outcome recorded", "Effective date", "1 Jun 2026", `data-tone="success"`} {
		if !strings.Contains(recordedMarkup, want) {
			t.Fatalf("a recorded promotion lost %q:\n%s", want, recordedMarkup)
		}
	}
}

// TestTodo_UXLIVE_001_Browser proves the wiring through the whole detail
// view, which is what a reader actually opens: the hero says one thing and
// the outcome panel must not say the opposite.
func TestTodo_UXLIVE_001_Browser(t *testing.T) {
	view := DetailView{
		Journey: JourneyCard{
			WorkerName: "Omar",
			Headline:   "OPS-HRBP2 · P2 → OPS-HRBP3 · P3",
			Stage:      "FAILED",
			StageLabel: "Failed",
			StageTone:  toneDanger,
			Group:      JourneyGroupClosed,
			Closed:     true,
		},
		Ledger: uxlive001Ledger(),
	}
	view.Ledger.Recorded = false
	view.Ledger.StatusLabel = "Failed"
	view.Ledger.StatusTone = toneDanger

	markup, err := ui.RenderToString(detailView(Page{Locale: "en-US"}, view))
	if err != nil {
		t.Fatalf("render detail view: %v", err)
	}
	if strings.Contains(markup, "Promotion outcome recorded") {
		t.Fatalf("detail view claims a promotion was recorded on a failed journey:\n%s", markup)
	}
	if !strings.Contains(markup, "Promotion not recorded") {
		t.Fatalf("detail view never states that no promotion was recorded:\n%s", markup)
	}
}

// TestTodo_UXLIVE_001_Golden pins the exact panel wording per terminal
// outcome against a checked-in oracle, so a future copy change has to be a
// deliberate edit of the oracle rather than a silent drift back to claiming
// every terminal record is a promotion.
func TestTodo_UXLIVE_001_Golden(t *testing.T) {
	cases := []struct {
		name   string
		status string
		tone   string
		ok     bool
	}{
		{name: "recorded", status: "Recorded", tone: toneSuccess, ok: true},
		{name: "blocked", status: "Blocked", tone: toneWarning},
		{name: "failed", status: "Failed", tone: toneDanger},
		{name: "repair_required", status: "Needs repair", tone: toneDanger},
		{name: "rejected", status: "Rejected", tone: toneDanger},
	}

	var got strings.Builder
	for _, c := range cases {
		card := uxlive001Ledger()
		card.Recorded = c.ok
		card.StatusLabel = c.status
		card.StatusTone = c.tone
		line := func(cells ...string) {
			got.WriteString(strings.TrimRight("  "+strings.Join(cells, " | "), " ") + "\n")
		}
		got.WriteString(c.name + "\n")
		for _, fact := range outcomeFacts("en-US", card) {
			line(fact.Label, fact.Value, fact.Tone)
		}
		label, tone := outcomeChip("en-US", card)
		line("chip", label, tone)
	}

	oracle := filepath.Join("testdata", "uxlive001_outcome.txt")
	want, err := os.ReadFile(oracle)
	if err != nil {
		t.Fatalf("read oracle %s: %v", oracle, err)
	}
	if got.String() != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("outcome panel drifted from %s:\n--- got ---\n%s", oracle, got.String())
	}
}
