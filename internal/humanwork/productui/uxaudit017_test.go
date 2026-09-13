package productui

import (
	"fmt"
	stdhtml "html"
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

	t.Run("within one urgency tier the queue ranks person-held work first, then the soonest date", func(t *testing.T) {
		view := testView(PageWork)
		// Admission order is deliberately the reverse of queue order on every
		// key, and the fixture carries both conditions under test: two owners
		// of the next step (a person vs the workflow) within the neutral tier,
		// and three different dates within the warning tier.
		view.Work = []WorkItem{
			{ID: "sys-soon", Person: "Quinn Adebayo", Status: "Waiting for effective date", Tone: "neutral", Due: "2026-09-20", NextStep: "await_effective_date", WaitingOn: "system"},
			{ID: "person-late", Person: "Rowan Iversen", Status: "Ready to start approval", Tone: "neutral", Due: "2027-03-01", NextStep: "start_approval", WaitingOn: "proposer", AwaitsPerson: true},
			{ID: "warn-undated", Person: "Sasha Brandt", Status: "Manager approval", Tone: "warning", NextStep: "manager_decision", WaitingOn: "manager", AwaitsPerson: true},
			{ID: "warn-late", Person: "Tomas Lindqvist", Status: "Finance approval", Tone: "warning", Due: "2026-12-01", NextStep: "finance_decision", WaitingOn: "finance", AwaitsPerson: true},
			{ID: "warn-soon", Person: "Uma Castellanos", Status: "Blocked", Tone: "warning", Due: "2026-10-01", NextStep: "correct_proposal", WaitingOn: "proposer", AwaitsPerson: true},
		}
		view.SelectedWork = ""
		doc, err := ui.RenderToString(workPage(view))
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"Uma Castellanos", "Tomas Lindqvist", "Sasha Brandt", "Rowan Iversen", "Quinn Adebayo"}
		last := -1
		for _, name := range want {
			at := strings.Index(doc, name)
			if at < 0 {
				t.Fatalf("row %q did not render: %s", name, doc)
			}
			if at < last {
				t.Fatalf("queue order broken at %q: want %v", name, want)
			}
			last = at
		}
	})

	t.Run("each row leads with its next step and whose turn it is, and names no fabricated assignee", func(t *testing.T) {
		view := testView(PageWork)
		view.Work = []WorkItem{
			{ID: "mgr-1", Person: "Vera Okonkwo", Status: "Manager approval", Tone: "warning", Due: "2026-10-01", NextStep: "manager_decision", WaitingOn: "manager", AwaitsPerson: true, Href: "/workspace/app/journeys?journey=mgr-1"},
		}
		doc, err := ui.RenderToString(workPage(view))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			`class="row-next-step">` + view.Locale.Text("work.row_next_step", map[string]string{"step": view.Locale.Text("work.next_step.manager_decision")}) + `<`,
			`class="row-waiting-on">` + view.Locale.Text("work.row_waiting_on", map[string]string{"actor": view.Locale.Text("work.waiting_on.manager")}) + `<`,
			`class="work-list-note">` + stdhtml.EscapeString(view.Locale.Text("work.assignee_note")) + `<`,
		} {
			if !strings.Contains(doc, want) {
				t.Fatalf("My Work is missing %q: %s", want, doc)
			}
		}
		if strings.Contains(doc, `class="row-assignment"`) || strings.Contains(doc, `class="row-work-due"`) || strings.Contains(doc, `class="row-next-action"`) {
			t.Fatalf("My Work named an assignee, deadline or action the server never disclosed: %s", doc)
		}
		// The preview repeats both as facts.
		preview := workPreviewProps(view, view.Work[0])
		if preview.Facts[0].Value != view.Locale.Text("work.next_step.manager_decision") || preview.Facts[1].Value != view.Locale.Text("work.waiting_on.manager") {
			t.Fatalf("preview facts do not lead with the next step and waiting-on: %+v", preview.Facts)
		}
	})

	t.Run("work the server says the viewer holds or may claim leads, then the real deadline", func(t *testing.T) {
		view := testView(PageWork)
		// All five share one urgency tier (warning) and person-held stage, so
		// only the summary keys decide. Admission order is the reverse.
		view.Work = []WorkItem{
			{ID: "undisclosed", Person: "Aiko Brandt", Status: "Manager approval", Tone: "warning", Due: "2026-09-15", AwaitsPerson: true},
			{ID: "other-owner", Person: "Bram Osei", Status: "Finance approval", Tone: "warning", AwaitsPerson: true, WorkSummary: true, ViewerMembership: "NONE", WorkDue: "2026-09-14"},
			{ID: "candidate", Person: "Cleo Ruiz", Status: "Manager approval", Tone: "warning", AwaitsPerson: true, WorkSummary: true, ViewerMembership: "CANDIDATE", WorkDue: "2026-09-16", PermittedActions: []string{"claim"}},
			{ID: "mine-late", Person: "Dev Anand", Status: "Finance approval", Tone: "warning", AwaitsPerson: true, WorkSummary: true, ViewerMembership: "ASSIGNEE", WorkDue: "2026-10-30", PermittedActions: []string{"claim"}},
			{ID: "mine-soon", Person: "Esme Park", Status: "Finance approval", Tone: "warning", AwaitsPerson: true, WorkSummary: true, ViewerMembership: "CLAIMANT", WorkDue: "2026-09-20", PermittedActions: []string{"release", "complete", "decide_approval"}},
		}
		view.SelectedWork = ""
		doc, err := ui.RenderToString(workPage(view))
		if err != nil {
			t.Fatal(err)
		}
		last := -1
		for _, name := range []string{"Esme Park", "Dev Anand", "Cleo Ruiz", "Bram Osei", "Aiko Brandt"} {
			at := strings.Index(doc, name)
			if at < 0 || at < last {
				t.Fatalf("queue order broken at %q (at %d, previous %d)", name, at, last)
			}
			last = at
		}
		for _, want := range []string{
			`class="row-next-action">` + view.Locale.Text("work.row_next_action", map[string]string{"action": view.Locale.Text("work.action.decide_approval")}) + `<`,
			`class="row-assignment">` + view.Locale.Text("work.row_assigned_to_you") + `<`,
			`class="row-next-action">` + view.Locale.Text("work.row_next_action", map[string]string{"action": view.Locale.Text("work.action.claim")}) + `<`,
			`class="row-assignment">` + view.Locale.Text("work.row_claimable_by_you") + `<`,
			`class="row-work-due">` + view.Locale.Text("work.row_due", map[string]string{"date": "2026-09-20"}) + `<`,
		} {
			if !strings.Contains(doc, want) {
				t.Fatalf("My Work is missing %q: %s", want, doc)
			}
		}
		// Exactly one row (Esme) was granted decide_approval; the claim-only
		// rows must not be promoted to it.
		if got := strings.Count(doc, `class="row-next-action">`+view.Locale.Text("work.row_next_action", map[string]string{"action": view.Locale.Text("work.action.decide_approval")})); got != 1 {
			t.Fatalf("decide approval offered on %d rows, the server granted it on 1: %s", got, doc)
		}
		// One row (Aiko) has no summary, so the honest note stays.
		if !strings.Contains(doc, `class="work-list-note"`) {
			t.Fatalf("the note must remain while any row lacks a summary: %s", doc)
		}
	})

	t.Run("a disclosed assignee replaces the stage waiting-on and the note goes when every row is disclosed", func(t *testing.T) {
		view := testView(PageWork)
		view.Work = []WorkItem{
			{ID: "fin-1", Person: "Farah Idris", Status: "Finance approval", Tone: "warning", NextStep: "finance_decision", WaitingOn: "finance", AwaitsPerson: true,
				WorkSummary: true, ViewerMembership: "NONE", AssigneeRef: "worker-gil", AssigneeName: "Gil Moreau", WorkDue: "2026-11-02"},
		}
		doc, err := ui.RenderToString(workPage(view))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(doc, `class="row-assignment">`+view.Locale.Text("work.row_assigned_to", map[string]string{"assignee": "Gil Moreau"})+`<`) {
			t.Fatalf("disclosed assignee missing: %s", doc)
		}
		if strings.Contains(doc, "row-waiting-on") || strings.Contains(doc, "row-next-action") || strings.Contains(doc, "work-list-note") {
			t.Fatalf("row repeated whose turn it is, invented an action, or kept the note: %s", doc)
		}
	})

	t.Run("the Assigned to me view keeps only open work the viewer holds or may claim", func(t *testing.T) {
		items := []WorkItem{
			{ID: "held", ViewerMembership: "ASSIGNEE"},
			{ID: "claiming", ViewerMembership: "CLAIMANT"},
			{ID: "claimable", ViewerMembership: "CANDIDATE"},
			{ID: "someone-else", ViewerMembership: "NONE", WorkSummary: true},
			{ID: "undisclosed"},
			{ID: "held-but-closed", ViewerMembership: "ASSIGNEE", Terminal: true},
		}
		got := []string{}
		for _, item := range FilterWorkCollection(items, ParseWorkCollectionFilter("mine")) {
			got = append(got, item.ID)
		}
		if strings.Join(got, ",") != "held,claiming,claimable" {
			t.Fatalf("mine = %v, want held,claiming,claimable", got)
		}
	})

	t.Run("a disposition's own waiting-for replaces the stage-derived waiting-on", func(t *testing.T) {
		row, err := ui.RenderToString(ui.CreateElement(WorkRow, WorkRowProps{
			ID: "d-1", Person: "Wen Hollis", NextStep: "Next step: Approval decision", WaitingOn: "Waiting on the approver",
			Disposition: ApprovalDispositionCardProps{Show: true, WaitingFor: "Waiting for Finance Partner"},
		}))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(row, "row-waiting-on") || !strings.Contains(row, "Waiting for Finance Partner") {
			t.Fatalf("row must state whose turn it is exactly once, from the disposition: %s", row)
		}
	})

	t.Run("filter links always state their filter so a saved filter can be cleared", func(t *testing.T) {
		view := testView(PageWork)
		view.WorkFilter = "blocked"
		props := workCollectionProps(view, workCollectionOptions{Title: "t", ListDetail: true})
		hrefs := map[string]string{}
		for _, tab := range props.Tabs {
			hrefs[tab.Label] = tab.Href
		}
		if got := hrefs[view.Locale.Text("work.all")]; !strings.Contains(got, "filter=") || strings.Contains(got, "filter=blocked") {
			t.Fatalf("Open work tab href = %q, want an explicit empty filter", got)
		}
		if got := hrefs[view.Locale.Text("work.blocked")]; !strings.Contains(got, "filter=blocked") {
			t.Fatalf("Blocked tab href = %q, want filter=blocked", got)
		}
		unfiltered := testView(PageWork)
		for _, row := range workCollectionProps(unfiltered, workCollectionOptions{Title: "t", ListDetail: true}).Rows {
			if !strings.Contains(row.Href, "filter=&") && !strings.HasSuffix(row.Href, "filter=") {
				t.Fatalf("an unfiltered row selection href %q would re-adopt the saved filter", row.Href)
			}
		}
		if got := workPreviewProps(unfiltered, WorkItem{}).Action.Href; !strings.Contains(got, "filter=") {
			t.Fatalf("Show all work href = %q, want an explicit empty filter", got)
		}
	})

	t.Run("the empty queue names the action task, not a generic empty list", func(t *testing.T) {
		view := testView(PageWork)
		view.Work = nil
		doc, err := ui.RenderToString(workPage(view))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(doc, "Nothing needs your action in this view") {
			t.Fatalf("My Work empty state is not action-queue specific: %s", doc)
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

	// The next step and waiting-on are real text inside the row's link, so
	// they are part of its accessible name -- never a colour, an icon, or an
	// aria-hidden decoration a screen reader skips.
	row, err := ui.RenderToString(ui.CreateElement(WorkRow, WorkRowProps{
		ID: "a11y-1", Person: "Xiomara Duarte", Href: "/workspace/app/work?filter=&selected=a11y-1",
		NextStep: "Next step: Finance decision", NextAction: "Your next action: Claim", Assignment: "Assigned to you", WorkDue: "Due 2026-10-01",
	}))
	if err != nil {
		t.Fatal(err)
	}
	linkStart, linkEnd := strings.Index(row, "<a "), strings.LastIndex(row, "</a>")
	if linkStart < 0 || linkEnd < 0 {
		t.Fatalf("row is not a link: %s", row)
	}
	link := row[linkStart:linkEnd]
	for _, text := range []string{"Next step: Finance decision", "Your next action: Claim", "Assigned to you", "Due 2026-10-01"} {
		at := strings.Index(link, text)
		if at < 0 {
			t.Fatalf("%q is not inside the row link's accessible content: %s", text, row)
		}
		if hidden := strings.LastIndex(link[:at], `aria-hidden="true"`); hidden >= 0 && !strings.Contains(link[hidden:at], "</span>") {
			t.Fatalf("%q sits inside an aria-hidden element: %s", text, row)
		}
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

// TestWorkRowActionFactsSurviveNarrowWidths is the live-browser regression
// the HTML-string tests above could not see: a pre-existing
// `.work-row .row-main small+small { display:none }` at max-width:1190px hid
// every row line after the first <small>, so at 390px the queue rendered
// without its next action, next step, disposition, assignment or due date --
// the facts UXAUDIT-017 exists to emphasise. Only the grade-change summary
// may be dropped at narrow widths, and it must be dropped by its own class.
func TestWorkRowActionFactsSurviveNarrowWidths(t *testing.T) {
	facts := []string{"row-next-action", "row-next-step", "row-disposition", "row-assignment", "row-waiting-on", "row-work-due"}
	css := Stylesheet()
	sawSummaryHide := false
	for _, rule := range cssRulePattern.FindAllStringSubmatch(css, -1) {
		if !strings.Contains(rule[2], "display:none") {
			continue
		}
		for _, item := range strings.Split(rule[1], ",") {
			selector := strings.TrimSpace(item)
			if !strings.Contains(selector, "work-row") {
				continue
			}
			if strings.Contains(selector, "small+small") || strings.Contains(selector, "small + small") || strings.Contains(selector, "small~small") {
				t.Fatalf("a positional sibling selector hides work-row lines and will swallow action facts: %q", selector)
			}
			for _, fact := range facts {
				if strings.Contains(selector, fact) {
					t.Fatalf("work-row action fact %q is hidden by %q", fact, selector)
				}
			}
			if selector == ".work-row .row-main small.row-summary" {
				sawSummaryHide = true
			}
		}
	}
	if !sawSummaryHide {
		t.Fatal("expected the narrow-width rule to hide only .row-summary")
	}

	doc, err := ui.RenderToString(ui.CreateElement(WorkRow, WorkRowProps{ID: "a", Title: "Promotion journey", Person: "Dominic", Summary: "PPL-TA3 P3 → PPL-HRBP3 P4", NextAction: "Your next action: Claim", WorkDue: "Due 2026-09-15"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `class="row-summary"`) {
		t.Fatalf("the grade-change summary line must carry row-summary so only it is dropped when narrow: %s", doc)
	}
}

func TestTodo_UXAUDIT_017_Regression(t *testing.T) {
	tones := []string{"danger", "warning", "neutral", "", "warning", "danger", "neutral"}

	populations := [][]WorkItem{
		syntheticWorkPopulation(tones, false),
		syntheticWorkPopulation(reverseTones(tones), false),
		syntheticWorkPopulation(append(tones, "warning", "danger"), false),
		syntheticWorkPopulation(tones, true), // includes terminal items mixed in
		syntheticOwnedDatedPopulation(tones, 1),
		syntheticOwnedDatedPopulation(reverseTones(tones), 3),
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
			assertQueueKeyMonotonic(t, sorted)
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

// syntheticOwnedDatedPopulation varies ownership and date independently of
// tone (seeded by stride), including undated items, so the ownership and
// date keys are genuinely exercised rather than constant.
func syntheticOwnedDatedPopulation(tones []string, stride int) []WorkItem {
	dates := []string{"2027-01-15", "", "2026-09-30", "2026-11-02", "2026-09-30"}
	memberships := []string{"", "NONE", "CANDIDATE", "ASSIGNEE", "CLAIMANT"}
	items := make([]WorkItem, 0, len(tones))
	for index, tone := range tones {
		items = append(items, WorkItem{
			ID:               fmt.Sprintf("owned-%03d", index),
			Tone:             tone,
			AwaitsPerson:     (index*stride)%3 != 0,
			Due:              dates[(index*stride)%len(dates)],
			ViewerMembership: memberships[(index*stride+1)%len(memberships)],
			WorkDue:          dates[(index*stride+2)%len(dates)],
		})
	}
	return items
}

// assertQueueKeyMonotonic requires the full queue key -- urgency rank, then
// person-held before workflow-held, then soonest date with undated last -- to
// be non-decreasing across sorted.
func assertQueueKeyMonotonic(t *testing.T, sorted []WorkItem) {
	t.Helper()
	for i := 1; i < len(sorted); i++ {
		a, b := sorted[i-1], sorted[i]
		if WorkUrgencyRank(a) != WorkUrgencyRank(b) {
			continue
		}
		if WorkViewerOwnershipRank(a) != WorkViewerOwnershipRank(b) {
			if WorkViewerOwnershipRank(a) > WorkViewerOwnershipRank(b) {
				t.Fatalf("%s (ownership %d) sorted before %s (ownership %d) in the same tier", a.ID, WorkViewerOwnershipRank(a), b.ID, WorkViewerOwnershipRank(b))
			}
			continue
		}
		if a.AwaitsPerson != b.AwaitsPerson {
			if !a.AwaitsPerson {
				t.Fatalf("workflow-held %s sorted before person-held %s in the same tier", a.ID, b.ID)
			}
			continue
		}
		if a.WorkDue != b.WorkDue {
			if a.WorkDue == "" || b.WorkDue != "" && a.WorkDue > b.WorkDue {
				t.Fatalf("%s (deadline %q) sorted before %s (deadline %q)", a.ID, a.WorkDue, b.ID, b.WorkDue)
			}
			continue
		}
		if a.Due == "" && b.Due != "" {
			t.Fatalf("undated %s sorted before dated %s", a.ID, b.ID)
		}
		if a.Due != "" && b.Due != "" && a.Due > b.Due {
			t.Fatalf("%s (%s) sorted before sooner %s (%s)", a.ID, a.Due, b.ID, b.Due)
		}
	}
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
	lastSeenByRank := map[string]int{}
	for _, item := range sorted {
		rank := fmt.Sprintf("%d|%d|%t|%s|%s", WorkUrgencyRank(item), WorkViewerOwnershipRank(item), item.AwaitsPerson, item.WorkDue, item.Due)
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

	// The composed document (Render, the SSR path serveProduct uses) carries
	// the handles a live pass selects on: the next-step and waiting-on lines,
	// the collection note, and an "Open work" tab that clears a saved filter.
	composed := testView(PageWork)
	composed.WorkFilter = "blocked"
	composed.Work = []WorkItem{
		{ID: "blocked-2", Person: "Yusuf Brennan", Status: "Blocked", Tone: "warning", Due: "2026-10-01", NextStep: "correct_proposal", WaitingOn: "proposer", AwaitsPerson: true, Href: "/workspace/app/journeys?journey=blocked-2"},
		{ID: "blocked-3", Person: "Zoe Lindahl", Status: "Blocked", Tone: "warning", AwaitsPerson: true, Href: "/workspace/app/journeys?journey=blocked-3",
			WorkSummary: true, ViewerMembership: "ASSIGNEE", WorkDue: "2026-09-25", PermittedActions: []string{"claim"}},
	}
	page, err := Render(composed)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="row-next-step"`, `class="row-waiting-on"`, `class="work-list-note"`,
		`href="/workspace/app/work?filter="`, `href="/workspace/app/work?filter=blocked"`, `href="/workspace/app/work?filter=mine"`,
		`class="row-next-action"`, `class="row-assignment"`, `class="row-work-due"`,
		`href="/workspace/app/journeys?journey=blocked-2"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("composed My Work page is missing %q", want)
		}
	}
}
