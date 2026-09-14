package productui

import (
	stdhtml "html"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// promoux012Work is the PROMOUX-012 population, each item carrying the
// server's viewer projection exactly as tools/uxqual/productclient projects
// it. It holds every condition the todo names: a passive wait with no action
// (one the viewer proposed and one they do not relate to), an item the viewer
// initiated but does not own, an item assigned to the viewer, a claimable
// manager approval and a finance approval, and an active and a past workflow
// for one worker (Dana).
func promoux012Work() []WorkItem {
	journey := func(id string) string { return "/workspace/app/journeys?journey=" + id }
	return []WorkItem{
		{ID: "observed-wait", Person: "Olu Mensah", PersonRef: "worker-olu", Summary: "OPS2 → OPS3", Status: "Waiting for effective date", Tone: "neutral",
			Due: "2027-01-01", EffectiveDate: "2027-01-01", NextStep: "await_effective_date", WaitingOn: "system", Href: journey("observed-wait"),
			ViewerResponsibility: "OBSERVING"},
		{ID: "passive-wait", Person: "Pia Laine", PersonRef: "worker-pia", Summary: "FIN2 → FIN3", Status: "Waiting for effective date", Tone: "neutral",
			Due: "2027-02-01", EffectiveDate: "2027-02-01", NextStep: "await_effective_date", WaitingOn: "system", Href: journey("passive-wait"),
			ViewerRelationships: []string{"INITIATOR"}, ViewerResponsibility: "TRACKING"},
		{ID: "tracked-manager", Person: "Tariq Haddad", PersonRef: "worker-tariq", Summary: "ENG2 → ENG3", Status: "Manager approval", Tone: "warning",
			Due: "2026-11-01", EffectiveDate: "2026-11-01", NextStep: "manager_decision", WaitingOn: "manager", AwaitsPerson: true, Href: journey("tracked-manager"),
			ViewerRelationships: []string{"INITIATOR"}, ViewerResponsibility: "TRACKING"},
		{ID: "observed-finance", Person: "Omar Reyes", PersonRef: "worker-omar", Summary: "OPS2 → OPS3", Status: "Finance approval", Tone: "warning",
			Due: "2026-10-15", EffectiveDate: "2026-10-15", NextStep: "finance_decision", WaitingOn: "finance", AwaitsPerson: true, Href: journey("observed-finance"),
			ViewerResponsibility: "OBSERVING"},
		{ID: "assigned-finance", Person: "Dana Wu", PersonRef: "worker-dana", Summary: "HR2 → HR3", Status: "Finance approval", Tone: "warning",
			Due: "2026-10-01", EffectiveDate: "2026-10-01", NextStep: "finance_decision", WaitingOn: "finance", AwaitsPerson: true, Href: journey("assigned-finance"),
			WorkSummary: true, ViewerMembership: "ASSIGNEE", WorkDue: "2026-09-20", PermittedActions: []string{"claim"},
			ViewerRelationships: []string{"ASSIGNEE"}, ViewerResponsibility: "ACTION_REQUIRED"},
		{ID: "claimable-manager", Person: "Mika Sato", PersonRef: "worker-mika", Summary: "DES2 → DES3", Status: "Manager approval", Tone: "warning",
			Due: "2026-10-05", EffectiveDate: "2026-10-05", NextStep: "manager_decision", WaitingOn: "manager", AwaitsPerson: true, Href: journey("claimable-manager"),
			WorkSummary: true, ViewerMembership: "CANDIDATE", WorkDue: "2026-09-25", PermittedActions: []string{"claim"},
			ViewerRelationships: []string{"CANDIDATE"}, ViewerResponsibility: "ACTION_REQUIRED"},
		{ID: "own-blocked", Person: "Bea Novak", PersonRef: "worker-bea", Summary: "LEG2 → LEG3", Status: "Blocked", Tone: "warning",
			Due: "2026-12-01", EffectiveDate: "2026-12-01", NextStep: "correct_proposal", WaitingOn: "proposer", AwaitsPerson: true, Href: journey("own-blocked"),
			ViewerRelationships: []string{"INITIATOR"}, ViewerResponsibility: "ACTION_REQUIRED"},
		{ID: "past-dana", Person: "Dana Wu", PersonRef: "worker-dana", Summary: "HR1 → HR2", Status: "Completed", Tone: "success", Terminal: true,
			EffectiveDate: "2025-04-01", CompletedAt: "2 Apr 2025 · 09:00 UTC", Href: journey("past-dana"),
			ViewerRelationships: []string{"INITIATOR"}, ViewerResponsibility: "CLOSED"},
	}
}

func promoux012View(page PageID) View {
	view := testView(page)
	view.Work = promoux012Work()
	view.SelectedWork = ""
	view.People = append(view.People, Person{ID: "worker-dana", Initials: "DW", Name: "Dana Wu", Role: "HR2 · P2", Team: "People", Location: "Austin",
		// The verdict deliberately disagrees with the open journey: the
		// profile must still never offer a duplicate start.
		PromotionAvailability: PromotionEligible})
	view.SelectedPerson = "worker-dana"
	return view
}

func promoux012IDs(items []WorkItem) string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return strings.Join(ids, ",")
}

func promoux012Order(t *testing.T, doc string, names ...string) {
	t.Helper()
	last := -1
	for _, name := range names {
		at := strings.Index(doc, name)
		if at < 0 {
			t.Fatalf("%q is missing from the document", name)
		}
		if at < last {
			t.Fatalf("%q rendered out of order in %v", name, names)
		}
		last = at
	}
}

// TestTodo_PROMOUX_012 is the surface half of the PRIMARY.
func TestTodo_PROMOUX_012(t *testing.T) {
	t.Run("My Work holds only the viewer's decisions and tasks, with owner, due date and next action", func(t *testing.T) {
		view := promoux012View(PageWork)
		if got := promoux012IDs(ActionableWorkItems(view.Work)); got != "assigned-finance,claimable-manager,own-blocked" {
			t.Fatalf("actionable = %s", got)
		}
		doc, err := ui.RenderToString(workPage(view))
		if err != nil {
			t.Fatal(err)
		}
		for _, absent := range []string{"Olu Mensah", "Pia Laine", "Tariq Haddad", "Omar Reyes"} {
			if strings.Contains(doc, absent) {
				t.Fatalf("My Work lists %s, whose journey asks nothing of the viewer", absent)
			}
		}
		// Assigned before claimable before the proposer's own correction:
		// the UXAUDIT-017 ownership key, now within actionable work only.
		promoux012Order(t, doc, "Dana Wu", "Mika Sato", "Bea Novak")
		for _, want := range []string{
			`class="row-assignment">` + view.Locale.Text("work.row_assigned_to_you") + `<`,
			`class="row-assignment">` + view.Locale.Text("work.row_claimable_by_you") + `<`,
			`class="row-work-due">` + view.Locale.Text("work.row_due", map[string]string{"date": "2026-09-20"}) + `<`,
			`class="row-next-action">` + view.Locale.Text("work.row_next_action", map[string]string{"action": view.Locale.Text("work.action.claim")}) + `<`,
			`class="row-next-step">` + view.Locale.Text("work.row_next_step", map[string]string{"step": view.Locale.Text("work.next_step.correct_proposal")}) + `<`,
			`<span class="count">` + view.Locale.Plural("work.item_count", 3) + `</span>`,
		} {
			if !strings.Contains(doc, want) {
				t.Fatalf("My Work is missing %q: %s", want, doc)
			}
		}
		if strings.Contains(doc, "row-tracking") {
			t.Fatal("an actionable row was labeled as needing no action")
		}
	})

	t.Run("Awaiting approval holds manager and finance approvals the viewer must decide, Blocked the viewer's own correction", func(t *testing.T) {
		view := promoux012View(PageWork)
		if got := promoux012IDs(FilterWorkCollection(view.Work, ParseWorkCollectionFilter("review"))); got != "assigned-finance,claimable-manager" {
			t.Fatalf("review = %s, want the finance and manager approvals the viewer holds or may claim", got)
		}
		if got := promoux012IDs(FilterWorkCollection(view.Work, ParseWorkCollectionFilter("blocked"))); got != "own-blocked" {
			t.Fatalf("blocked = %s", got)
		}
	})

	t.Run("Tracked requests holds what the viewer proposed, passive waits included, with next transition and owner", func(t *testing.T) {
		view := ApplyRequest(promoux012View(PageWork), PageRequest{WorkFilter: "tracked"})
		if got := promoux012IDs(view.Work); got != "passive-wait,tracked-manager,own-blocked" {
			t.Fatalf("tracked = %s", got)
		}
		doc, err := ui.RenderToString(workPage(view))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "Olu Mensah") || strings.Contains(doc, "Omar Reyes") || strings.Contains(doc, "Dana Wu") {
			t.Fatalf("tracked requests lists a journey the viewer did not initiate: %s", doc)
		}
		row := promoux012RowFor(t, doc, "Pia Laine")
		for _, want := range []string{
			`class="row-next-step">` + view.Locale.Text("work.row_next_step", map[string]string{"step": view.Locale.Text("work.next_step.await_effective_date")}) + `<`,
			`class="row-waiting-on">` + view.Locale.Text("work.row_waiting_on", map[string]string{"actor": view.Locale.Text("work.waiting_on.system")}) + `<`,
			`class="row-tracking">` + view.Locale.Text("work.row_no_action_needed") + `<`,
		} {
			if !strings.Contains(row, want) {
				t.Fatalf("the passive wait's row is missing %q: %s", want, row)
			}
		}
		if strings.Contains(row, "row-next-action") {
			t.Fatalf("a passive wait offered an action: %s", row)
		}
		bea := promoux012RowFor(t, doc, "Bea Novak")
		if strings.Contains(bea, "row-tracking") {
			t.Fatalf("the viewer's own blocked proposal was labeled as needing no action: %s", bea)
		}
	})

	t.Run("passive waits and tracked requests never inflate an actionable count", func(t *testing.T) {
		view := promoux012View(PageWork)
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		want := `aria-label="` + view.Locale.Text("shell.work_overview") + ", " + view.Locale.Plural("shell.work_count", 3) + `"`
		if !strings.Contains(doc, want) {
			t.Fatalf("notification summary is not the actionable count %q", want)
		}
		for _, stale := range []string{"need attention", "needs attention", "visible in this scope"} {
			if strings.Contains(doc, stale) {
				t.Fatalf("My Work still says %q", stale)
			}
		}
		if len(ActionableWorkItems(view.Work)) != 3 || len(OpenWorkItems(view.Work)) != 7 {
			t.Fatal("fixture no longer separates open work from actionable work")
		}
	})

	t.Run("the person profile lists active and past workflows with direct links and no duplicate start", func(t *testing.T) {
		view := promoux012View(PagePerson)
		person, ok := exactPerson(view)
		if !ok {
			t.Fatal("fixture resolves no person")
		}
		profile := personProfileProps(view, person, PagePerson)
		if len(profile.Active.Items) != 1 || profile.Active.Items[0].ID != "assigned-finance" {
			t.Fatalf("active workflows = %+v, want Dana's open finance approval", profile.Active.Items)
		}
		active := profile.Active.Items[0]
		if active.Href != "/workspace/app/journeys?journey=assigned-finance" || active.Action != view.Locale.Text("person.workflow_resume") {
			t.Fatalf("active workflow link = %+v, want Resume to the journey", active)
		}
		if active.NextStep == "" || active.WaitingOn == "" {
			t.Fatalf("active workflow omits its next transition or owner: %+v", active)
		}
		if len(profile.History.Items) != 1 || profile.History.Items[0].Href != "/workspace/app/journeys?journey=past-dana" {
			t.Fatalf("past workflows = %+v, want Dana's completed journey with its link", profile.History.Items)
		}
		start := JourneyProposalHref(view, "worker-dana")
		sawOpen := false
		for _, card := range profile.Workflows.Workflows {
			if card.Href == start {
				t.Fatalf("the profile offers a duplicate start beside an active promotion: %+v", card)
			}
			if card.Name == view.Locale.Text("people.open_active_promotion") && card.Href == JourneyDetailHref(view, "assigned-finance") {
				sawOpen = true
			}
		}
		if !sawOpen {
			t.Fatalf("the launcher does not route to the active promotion: %+v", profile.Workflows.Workflows)
		}

		// A workflow the viewer only observes opens rather than resumes.
		observed := promoux012View(PagePerson)
		observed.SelectedPerson = "worker-dana"
		observed.Work[4].ViewerResponsibility, observed.Work[4].ViewerRelationships = "OBSERVING", nil
		if got := personActiveWorkflowsProps(observed, person, PagePerson).Items[0].Action; got != view.Locale.Text("person.workflow_open") {
			t.Fatalf("an observed journey is offered as %q", got)
		}
		if personActiveWorkflowsProps(view, person, PageMyself).Show {
			t.Fatal("the self-service route shows another route's active workflows section")
		}
	})
}

// promoux012RowFor returns the rendered <li> that contains name.
func promoux012RowFor(t *testing.T, doc, name string) string {
	t.Helper()
	at := strings.Index(doc, name)
	if at < 0 {
		t.Fatalf("%s is not rendered", name)
	}
	start := strings.LastIndex(doc[:at], `<li class="work-row-item">`)
	end := strings.Index(doc[at:], "</li>")
	if start < 0 || end < 0 {
		t.Fatalf("%s is not inside a work row", name)
	}
	return doc[start : at+end]
}

// TestTodo_PROMOUX_012_Browser asserts on the composed server document the
// production handler serves (Render), the same SSR path the WASM mount
// hydrates, for both surfaces this todo changes: the handles a live pass
// selects on exist and carry the right links, and nothing fabricates a
// control the server never granted.
func TestTodo_PROMOUX_012_Browser(t *testing.T) {
	work := ApplyRequest(promoux012View(PageWork), PageRequest{WorkFilter: "tracked"})
	doc, err := Render(work)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`href="/workspace/app/work?filter="`, `href="/workspace/app/work?filter=review"`, `href="/workspace/app/work?filter=mine"`,
		`aria-current="page" class="tab active" href="/workspace/app/work?filter=tracked"`,
		`class="row-tracking"`, `class="row-next-step"`, `class="row-waiting-on"`,
		`href="/workspace/app/work?filter=tracked&amp;selected=tracked-manager"`, `href="/workspace/app/journeys?journey=passive-wait"`,
		`>` + work.Locale.Text("work.tracked") + `<`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("tracked requests document is missing %q", want)
		}
	}
	list := doc[strings.Index(doc, `class="surface work-list"`):]
	list = list[:strings.Index(list, "</section>")]
	if strings.Contains(list, "<button") || strings.Contains(list, "<form") {
		t.Fatalf("the tracked list rendered an action control: %s", list)
	}

	empty := ApplyRequest(testView(PageWork), PageRequest{WorkFilter: "tracked"})
	emptyDoc, err := Render(empty)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(emptyDoc, stdhtml.EscapeString(empty.Locale.Text("work.tracked_empty_title"))) || strings.Contains(emptyDoc, empty.Locale.Text("work.empty_title")) {
		t.Fatal("the tracked view's empty state is the action queue's")
	}

	person := promoux012View(PagePerson)
	personDoc, err := Render(person)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`aria-labelledby="person-active-workflows-title" class="surface person-active-workflows"`,
		`id="person-active-workflows-title"`,
		`href="/workspace/app/journeys?journey=assigned-finance"`,
		`href="/workspace/app/journeys?journey=past-dana"`,
	} {
		if !strings.Contains(personDoc, want) {
			t.Errorf("person document is missing %q", want)
		}
	}
	if strings.Contains(personDoc, `href="/workspace/app/journeys?mode=new&amp;worker=worker-dana"`) {
		t.Fatal("the person document offers a duplicate start")
	}
	if strings.Index(personDoc, "person-active-workflows") > strings.Index(personDoc, "workflow-history-title") {
		t.Fatal("active workflows render after the past ones")
	}
}

// TestTodo_PROMOUX_012_Accessibility: every fact a tracked row states is
// real text inside the row link's accessible content, the resume and open
// links name both the action and the worker, and the copy is localized in
// every supported locale with RTL for Arabic.
func TestTodo_PROMOUX_012_Accessibility(t *testing.T) {
	row, err := ui.RenderToString(ui.CreateElement(WorkRow, WorkRowProps{
		ID: "a11y-12", Person: "Pia Laine", Href: "/workspace/app/work?filter=tracked&selected=a11y-12",
		NextStep: "Next step: Wait for the effective date", WaitingOn: "Waiting on the workflow", Tracking: "No action needed from you",
	}))
	if err != nil {
		t.Fatal(err)
	}
	linkStart, linkEnd := strings.Index(row, "<a "), strings.LastIndex(row, "</a>")
	if linkStart < 0 || linkEnd < 0 {
		t.Fatalf("row is not a link: %s", row)
	}
	link := row[linkStart:linkEnd]
	for _, text := range []string{"Next step: Wait for the effective date", "Waiting on the workflow", "No action needed from you"} {
		at := strings.Index(link, text)
		if at < 0 {
			t.Fatalf("%q is outside the row link: %s", text, row)
		}
		if hidden := strings.LastIndex(link[:at], `aria-hidden="true"`); hidden >= 0 && !strings.Contains(link[hidden:at], "</span>") {
			t.Fatalf("%q sits inside an aria-hidden element: %s", text, row)
		}
	}

	view := promoux012View(PagePerson)
	person, _ := exactPerson(view)
	section, err := ui.RenderToString(ui.CreateElement(PersonActiveWorkflows, personActiveWorkflowsProps(view, person, PagePerson)))
	if err != nil {
		t.Fatal(err)
	}
	wantLabel := `aria-label="` + view.Locale.Text("person.workflow_link_label", map[string]string{"action": view.Locale.Text("person.workflow_resume"), "name": "Dana Wu"}) + `"`
	if !strings.Contains(section, wantLabel) {
		t.Fatalf("the resume link does not name its action and worker %s: %s", wantLabel, section)
	}
	if !strings.Contains(section, `role="list"`) || !strings.Contains(section, `<h2 id="person-active-workflows-title">`) {
		t.Fatalf("the section is not a labelled list: %s", section)
	}

	keys := []string{"work.all", "work.tracked", "work.row_no_action_needed", "work.tracked_empty_title", "work.tracked_empty_detail",
		"person.active_workflows", "person.active_workflows_detail", "person.active_workflows_empty", "person.workflow_resume",
		"person.workflow_open", "person.workflow_link_label", "page.work.subtitle"}
	english := ResolveProductLocale("en-US")
	for _, locale := range []string{"de-DE", "ar"} {
		resolved := ResolveProductLocale(locale)
		for _, key := range keys {
			text := resolved.Text(key)
			if strings.HasPrefix(text, "⟦") || text == english.Text(key) {
				t.Errorf("%s: %s is not localized (%q)", locale, key, text)
			}
		}
		if count := resolved.Plural("shell.work_count", 3); strings.HasPrefix(count, "⟦") || count == english.Plural("shell.work_count", 3) {
			t.Errorf("%s: the actionable count is not localized (%q)", locale, count)
		}
	}
	arabic := promoux012View(PageWork)
	arabic.Locale = ResolveProductLocale("ar")
	doc, err := Render(ApplyRequest(arabic, PageRequest{Locale: "ar", WorkFilter: "tracked"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `dir="rtl"`) || !strings.Contains(doc, ResolveProductLocale("ar").Text("work.row_no_action_needed")) {
		t.Fatal("the Arabic tracked view is not right-to-left or not localized")
	}
}

// TestTodo_PROMOUX_012_Regression pins the three defects UXAUDIT-017 recorded
// and this todo closes, generically rather than by one fixture's names.
func TestTodo_PROMOUX_012_Regression(t *testing.T) {
	t.Run("the approvals view never matches on a status label", func(t *testing.T) {
		for _, step := range []string{"approval_decision", "manager_decision", "finance_decision", "reapproval_decision"} {
			for _, label := range []string{"Awaiting approval", "Manager approval", "Finance approval", "Approval required again", "anything"} {
				item := WorkItem{ID: step, Status: label, NextStep: step, ViewerResponsibility: "ACTION_REQUIRED"}
				if len(FilterWorkCollection([]WorkItem{item}, WorkCollectionReview)) != 1 {
					t.Errorf("an actionable %s labeled %q is missing from Awaiting approval", step, label)
				}
				item.ViewerResponsibility = "OBSERVING"
				if len(FilterWorkCollection([]WorkItem{item}, WorkCollectionReview)) != 0 {
					t.Errorf("an observed %s labeled %q entered Awaiting approval", step, label)
				}
			}
		}
		literal := WorkItem{ID: "literal", Status: "Awaiting approval", NextStep: "await_effective_date", ViewerResponsibility: "ACTION_REQUIRED"}
		if len(FilterWorkCollection([]WorkItem{literal}, WorkCollectionReview)) != 0 {
			t.Error("the literal Awaiting approval label still selects a non-decision item")
		}
	})

	t.Run("no responsibility, an unknown one, or a closed journey is never actionable or counted", func(t *testing.T) {
		for _, responsibility := range []string{"", "TRACKING", "OBSERVING", "CLOSED", "action_required", "ACTION"} {
			item := WorkItem{ID: "x", NextStep: "manager_decision", ViewerResponsibility: responsibility, ViewerMembership: "NONE"}
			if WorkNeedsViewerAction(item) || len(ActionableWorkItems([]WorkItem{item})) != 0 {
				t.Errorf("responsibility %q was treated as actionable", responsibility)
			}
		}
		closed := WorkItem{ID: "closed", Terminal: true, ViewerResponsibility: "ACTION_REQUIRED", ViewerRelationships: []string{"INITIATOR"}}
		if WorkNeedsViewerAction(closed) || len(FilterWorkCollection([]WorkItem{closed}, WorkCollectionTracked)) != 0 {
			t.Error("a closed journey stayed actionable or tracked")
		}
		for _, relationships := range [][]string{nil, {"ASSIGNEE"}, {"CANDIDATE"}, {"FOLLOWER"}, {"initiator"}} {
			if WorkViewerInitiated(WorkItem{ViewerRelationships: relationships}) {
				t.Errorf("relationships %v were read as initiated", relationships)
			}
		}
	})

	t.Run("a passive wait is recognised by owner, never by label", func(t *testing.T) {
		if !WorkIsPassiveWait(WorkItem{NextStep: "system_processing"}) || WorkIsPassiveWait(WorkItem{NextStep: "manager_decision", AwaitsPerson: true}) ||
			WorkIsPassiveWait(WorkItem{Terminal: true, NextStep: "await_effective_date"}) || WorkIsPassiveWait(WorkItem{Status: "Waiting for effective date"}) {
			t.Fatal("passive wait classification is wrong")
		}
	})

	t.Run("operational attention counts every approval decision and blocked proposal", func(t *testing.T) {
		view := promoux012View(PageInsights)
		doc, err := ui.RenderToString(insightsPage(view))
		if err != nil {
			t.Fatal(err)
		}
		// Only the viewer's assigned finance, claimable manager and own
		// correction are attention; other approvers' work is not.
		if !strings.Contains(doc, ">3<") {
			t.Fatalf("insights attention is not 3: %s", doc)
		}
	})

	t.Run("the person profile resolves a journey named by its entity reference", func(t *testing.T) {
		view := promoux012View(PagePerson)
		for index := range view.People {
			if view.People[index].ID == "worker-dana" {
				view.People[index].WorkerID = "0e30e81f-984f-5611-8570-f8c903470020"
			}
		}
		view.Work[4].PersonRef = "eref:v1:tenant:worker:0e30e81f-984f-5611-8570-f8c903470020"
		if _, ok := activePromotionWorkItem(view, "worker-dana"); !ok {
			t.Fatal("an active journey named by entity reference does not guard the start")
		}
		person, _ := exactPerson(view)
		if len(personActiveWorkflowsProps(view, person, PagePerson).Items) != 1 {
			t.Fatal("an active journey named by entity reference is missing from the profile")
		}
	})
}
