package productui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ---------------------------------------------------------------------
// TestTodo_UXAUDIT_017 -- PRIMARY.
//
// UXAUDIT-017 GREEN: "My Work is an action queue ordered by urgency and
// ownership". This package's honest boundary (see this todo's report) is
// that the journey summary carries neither an owner nor a per-item action
// set, so this test proves the two things that ARE buildable from what the
// summary actually carries: a computed, testable urgency rank, and a row
// date that is labeled for what it is (an effective date) rather than
// implied to be a deadline.
// ---------------------------------------------------------------------

func TestTodo_UXAUDIT_017(t *testing.T) {
	t.Run("urgency rank orders danger before warning before neutral before terminal", func(t *testing.T) {
		cases := []struct {
			name string
			item WorkItem
			want int
		}{
			{"danger, open", WorkItem{Tone: "danger"}, 0},
			{"warning, open", WorkItem{Tone: "warning"}, 1},
			{"neutral, open", WorkItem{Tone: "neutral"}, 2},
			{"empty tone, open", WorkItem{}, 2},
			{"success terminal outranked by everything open", WorkItem{Tone: "success", Terminal: true}, 3},
			{"even a danger tone ranks last once terminal", WorkItem{Tone: "danger", Terminal: true}, 3},
		}
		for _, c := range cases {
			if got := WorkUrgencyRank(c.item); got != c.want {
				t.Errorf("%s: WorkUrgencyRank = %d, want %d", c.name, got, c.want)
			}
		}
	})

	t.Run("the queue shows the most urgent row first, ahead of admission order", func(t *testing.T) {
		view := testView(PageWork)
		// Admission (server/recency) order is deliberately the opposite of
		// urgency order: the neutral, merely-waiting item was admitted
		// first and the blocked item last.
		view.Work = []WorkItem{
			{ID: "waiting-1", Person: "Casey Nakamura", PersonRef: "worker-casey", Status: "Waiting for effective date", Tone: "neutral", Href: "/workspace/app/journeys?journey=waiting-1", EffectiveDate: "2026-12-01", Due: "2026-12-01"},
			{ID: "blocked-1", Person: "Bailey Osei", PersonRef: "worker-bailey", Status: "Blocked", Tone: "warning", Href: "/workspace/app/journeys?journey=blocked-1", EffectiveDate: "2026-11-01", Due: "2026-11-01"},
		}
		view.SelectedWork = ""
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		blockedIndex := strings.Index(doc, "Bailey Osei")
		waitingIndex := strings.Index(doc, "Casey Nakamura")
		if blockedIndex < 0 || waitingIndex < 0 {
			t.Fatalf("both rows must render: %s", doc)
		}
		if blockedIndex > waitingIndex {
			t.Fatalf("Blocked (urgent) rendered after Waiting-for-effective-date (not urgent): blocked at %d, waiting at %d", blockedIndex, waitingIndex)
		}
		// The preview panel must agree with the row order: with no explicit
		// selection, "first" means the same thing in both places.
		if !strings.Contains(doc, "Bailey Osei") || strings.Index(doc, `class="work-row selected"`) > strings.Index(doc, "Casey Nakamura") {
			t.Fatalf("selected row is not the urgency-first item: %s", doc)
		}
		preview := workPreviewProps(view, selectedOpenWork(view))
		if preview.Person != "Bailey Osei" {
			t.Fatalf("preview.Person = %q, want the most urgent item Bailey Osei", preview.Person)
		}
	})

	t.Run("the row date is labeled as an effective date, never a bare/implied deadline", func(t *testing.T) {
		view := testView(PageWork)
		view.Work = []WorkItem{
			{ID: "intent-1", Person: "Jordan Lee", Status: "Awaiting approval", Tone: "warning", Href: "/workspace/app/journeys?journey=intent-1", EffectiveDate: "2026-09-15", Due: "2026-09-15"},
		}
		doc, err := ui.RenderToString(workPage(view))
		if err != nil {
			t.Fatal(err)
		}
		want := view.Locale.Text("work.row_effective_date", map[string]string{"date": "2026-09-15"})
		if !strings.Contains(doc, want) {
			t.Fatalf("row missing labeled effective date %q: %s", want, doc)
		}
	})
}

// ---------------------------------------------------------------------
// TestTodo_UXAUDIT_017_Accessibility.
//
// The active work-collection tab must expose its state to assistive
// technology through markup, not only through the "active" class name.
// ---------------------------------------------------------------------

func TestTodo_UXAUDIT_017_Accessibility(t *testing.T) {
	activeTab, err := ui.RenderToString(ui.CreateElement(WorkTab, WorkTabProps{Label: "Blocked", Href: "/workspace/app/work?filter=blocked", Active: true}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(activeTab, `class="tab active"`) {
		t.Fatalf("active tab lost its visual state: %s", activeTab)
	}
	if !strings.Contains(activeTab, `aria-current="page"`) {
		t.Fatalf("active tab does not expose its state to assistive technology: %s", activeTab)
	}

	inactiveTab, err := ui.RenderToString(ui.CreateElement(WorkTab, WorkTabProps{Label: "Awaiting approval", Href: "/workspace/app/work?filter=review", Active: false}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(inactiveTab, "aria-current") {
		t.Fatalf("an inactive tab must not claim to be the current page: %s", inactiveTab)
	}
	if strings.Contains(inactiveTab, "active") {
		t.Fatalf("an inactive tab must not carry the active class: %s", inactiveTab)
	}
}

// ---------------------------------------------------------------------
// TestTodo_UXAUDIT_017_Regression.
//
// Pins the urgency ordering GENERICALLY: several seeded, mixed-status
// populations, none of them a hand-built name list, all required to satisfy
// the same computed-rank invariant. A renderer that hardcodes one observed
// order (e.g. "Samuel, Adrian, Naomi") would pass a test built from that
// exact fixture but fail this one, because these fixtures are built
// programmatically and vary in size, tone mix and starting order.
// ---------------------------------------------------------------------

func TestTodo_UXAUDIT_017_Regression(t *testing.T) {
	tones := []string{"danger", "warning", "neutral", "", "warning", "danger", "neutral"}

	populations := [][]WorkItem{
		syntheticWorkPopulation(tones, false),
		syntheticWorkPopulation(reverseTones(tones), false),
		syntheticWorkPopulation(append(tones, "warning", "danger"), false),
		syntheticWorkPopulation(tones, true), // includes terminal items mixed in
	}

	for populationIndex, items := range populations {
		items := items
		t.Run(fmt.Sprintf("population %d (n=%d)", populationIndex, len(items)), func(t *testing.T) {
			sorted := SortWorkByUrgency(items)
			if len(sorted) != len(items) {
				t.Fatalf("SortWorkByUrgency changed population size: got %d, want %d", len(sorted), len(items))
			}
			// The computed rank must be non-decreasing across the whole
			// output: that is the entire content of "ordered by urgency",
			// asserted without ever naming a specific item.
			for i := 1; i < len(sorted); i++ {
				if WorkUrgencyRank(sorted[i-1]) > WorkUrgencyRank(sorted[i]) {
					t.Fatalf("rank regressed at position %d: %+v (rank %d) sorted before %+v (rank %d)",
						i, sorted[i-1], WorkUrgencyRank(sorted[i-1]), sorted[i], WorkUrgencyRank(sorted[i]))
				}
			}
			// Equal-rank items keep their original relative order (stable
			// sort): verified generically by requiring each rank group's
			// IDs to still increase in the group's original relative
			// sequence, using the index encoded in each synthetic ID.
			assertStableWithinRank(t, items, sorted)
			// Blocked/awaiting-approval-shaped items (tone=warning) must
			// never sort after an item merely waiting on a future date
			// (tone=neutral/empty and non-terminal) -- GREEN's explicit
			// requirement, checked without hardcoding any single fixture's
			// shape.
			for _, urgent := range sorted {
				if urgent.Tone != "warning" || urgent.Terminal {
					continue
				}
				for _, passive := range sorted {
					if passive.Terminal || passive.Tone == "danger" || passive.Tone == "warning" {
						continue
					}
					if indexOfWorkItem(sorted, urgent) > indexOfWorkItem(sorted, passive) {
						t.Fatalf("an awaiting/blocked item (%s) sorted after a merely-waiting item (%s)", urgent.ID, passive.ID)
					}
				}
			}
		})
	}
}

func syntheticWorkPopulation(tones []string, mixTerminal bool) []WorkItem {
	items := make([]WorkItem, 0, len(tones))
	for index, tone := range tones {
		items = append(items, WorkItem{
			ID:       fmt.Sprintf("synthetic-%03d", index),
			Person:   fmt.Sprintf("Synthetic Person %d", index),
			Status:   tone,
			Tone:     tone,
			Terminal: mixTerminal && index%5 == 4,
		})
	}
	return items
}

func reverseTones(tones []string) []string {
	reversed := make([]string, len(tones))
	for i, tone := range tones {
		reversed[len(tones)-1-i] = tone
	}
	return reversed
}

func indexOfWorkItem(items []WorkItem, target WorkItem) int {
	for index, item := range items {
		if item.ID == target.ID {
			return index
		}
	}
	return -1
}

// assertStableWithinRank requires that within any group of equal-ranked
// items, their relative order in sorted matches their relative order in
// original -- proving SortWorkByUrgency changes only the urgency dimension.
func assertStableWithinRank(t *testing.T, original, sorted []WorkItem) {
	t.Helper()
	originalIndex := make(map[string]int, len(original))
	for index, item := range original {
		originalIndex[item.ID] = index
	}
	lastSeenByRank := map[int]int{}
	for _, item := range sorted {
		rank := WorkUrgencyRank(item)
		pos := originalIndex[item.ID]
		if last, ok := lastSeenByRank[rank]; ok && pos < last {
			t.Fatalf("item %s (original position %d) sorted before an equal-rank item that was originally later (position %d): stability broken", item.ID, pos, last)
		}
		lastSeenByRank[rank] = pos
	}
}

// ---------------------------------------------------------------------
// TestTodo_UXAUDIT_017_Browser.
//
// BROWSER-matrix entry, in the sense established by
// TestTodo_PROMOUX_002_Browser: it asserts on the browser-observable HTML
// structure (row order, absence of fabricated controls, a labeled date)
// rather than driving an actual browser. The task's own instructions name a
// live server pass against the running dev server as the closing step for
// this todo, run outside this session; this test is what makes that pass
// buildable rather than a substitute for it.
// ---------------------------------------------------------------------

func TestTodo_UXAUDIT_017_Browser(t *testing.T) {
	view := testView(PageWork)
	view.Work = []WorkItem{
		{ID: "waiting-1", Person: "Devon Alvarez", Status: "Waiting for effective date", Tone: "neutral", Href: "/workspace/app/journeys?journey=waiting-1", EffectiveDate: "2027-01-01", Due: "2027-01-01"},
		{ID: "blocked-1", Person: "Reese Kowalski", Status: "Blocked", Tone: "warning", Href: "/workspace/app/journeys?journey=blocked-1", EffectiveDate: "2026-10-01", Due: "2026-10-01"},
		{ID: "repair-1", Person: "Toni Ferreira", Status: "Needs repair", Tone: "danger", Href: "/workspace/app/journeys?journey=repair-1", EffectiveDate: "2026-09-20", Due: "2026-09-20"},
	}
	view.SelectedWork = ""
	doc, err := ui.RenderToString(workPage(view))
	if err != nil {
		t.Fatal(err)
	}
	repair := strings.Index(doc, "Toni Ferreira")
	blocked := strings.Index(doc, "Reese Kowalski")
	waiting := strings.Index(doc, "Devon Alvarez")
	if repair < 0 || blocked < 0 || waiting < 0 {
		t.Fatalf("all three rows must render: %s", doc)
	}
	if !(repair < blocked && blocked < waiting) {
		t.Fatalf("rows are not ordered most-urgent-first: repair@%d blocked@%d waiting@%d", repair, blocked, waiting)
	}
	if strings.Contains(doc, "<button") {
		t.Fatalf("My Work must never render a fabricated action control the server did not authorize: %s", doc)
	}
	if !strings.Contains(doc, view.Locale.Text("work.row_effective_date", map[string]string{"date": "2026-09-20"})) {
		t.Fatalf("row date is not labeled as an effective date: %s", doc)
	}
}
