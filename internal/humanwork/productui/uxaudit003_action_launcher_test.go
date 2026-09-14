package productui

// UXAUDIT-003: "Make the global launcher perform actions rather than
// duplicate navigation."
//
// RED (measured live at /workspace/app/home): "Start an action" offered
// exactly two plain navigation links (Journeys, People) and no actions at
// all, and Escape did not close it (the trigger's aria-expanded stayed
// "true").
//
// GREEN: the launcher lists ranked authorized actions (such as promoting
// an eligible worker), explains a disabled action without disclosure,
// supports keyboard search and Escape/outside-focus dismissal, and uses a
// navigation-framed label instead of an action-framed one once no ranked
// action survives for the viewer.
//
// REFACTOR: the launcher's items come from the same registry the People
// directory rows already use -- the PersonWorkflow catalogue on View plus
// the per-worker PromotionAvailability verdict, resolved through the one
// shared function personWorkflowActions (page_people.go) -- not a
// launcher-local, hand-maintained list. No formal "semantic-action
// registry" type exists yet in this codebase; PersonWorkflow plus
// PromotionAvailability is the closest existing thing, and this todo
// reuses it rather than inventing a parallel one. See the type doc on
// personActionLauncherItems (action_launcher.go) for the authorization
// contract this file exercises.

import (
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	xhtml "golang.org/x/net/html"
)

// disabledItemMentioning finds the first non-launchable, explained item
// (Href == "" && Reason != "") mentioning needle. Plain itemMentioning is
// not enough once a person has more than one workflow, since an
// unrelated but still-available workflow (e.g. sabbatical leave) also
// mentions the same worker and would otherwise shadow the blocked one.
func disabledItemMentioning(items []ActionLauncherItem, needle string) *ActionLauncherItem {
	for index := range items {
		item := &items[index]
		if item.Href != "" || item.Reason == "" {
			continue
		}
		if strings.Contains(item.Label+item.Description, needle) {
			return item
		}
	}
	return nil
}

func uxaudit003Workflows() []PersonWorkflow {
	return []PersonWorkflow{
		{ID: "promotion", Name: "Promotion", Category: "Career & compensation",
			LaunchHref: func(id string) string { return "/workspace/app/journeys?mode=new&worker=" + id }},
		// A workflow the launcher has never been special-cased for. If it
		// shows up automatically, the launcher is reading the catalogue,
		// not a hand-maintained inventory of "promotion" plus whatever
		// else someone remembered to wire in.
		{ID: "sabbatical", Name: "Sabbatical leave", Category: "Time away", Description: "Request extended unpaid leave.",
			LaunchHref: func(id string) string { return "/workspace/app/journeys?mode=new&worker=" + id + "&type=sabbatical" }},
	}
}

// TestTodo_UXAUDIT_003 is the PRIMARY matrix entry. It proves the pure
// resolution rules GREEN and REFACTOR name: an eligible worker gets a
// launchable, person-specific action; a newly added, previously-unknown
// catalogue workflow reaches the launcher automatically; an ineligible
// worker's action is explained rather than omitted or silently dropped;
// the zero-value PromotionAvailability never renders as launchable; and
// the control's own navigation-vs-action framing switches with the
// content it actually has.
func TestTodo_UXAUDIT_003(t *testing.T) {
	t.Run("ranked authorized action for an eligible worker", func(t *testing.T) {
		view := testView(PageHome)
		view.People = []Person{{ID: "worker-avery", Name: "Avery Patel", PromotionAvailability: PromotionEligible}}
		view.PersonWorkflows = uxaudit003Workflows()
		items := personActionLauncherItems(view)
		promo := itemMentioning(items, "worker-avery")
		if promo == nil || promo.Href == "" || promo.IsNavigationDestination {
			t.Fatalf("no launchable promotion action for the eligible worker: %+v", items)
		}
		if !strings.Contains(promo.Label, "Avery Patel") {
			t.Fatalf("action label %q does not name the worker it acts on", promo.Label)
		}
	})

	t.Run("REFACTOR: a workflow this file never special-cases still reaches the launcher", func(t *testing.T) {
		view := testView(PageHome)
		view.People = []Person{{ID: "worker-avery", Name: "Avery Patel", PromotionAvailability: PromotionEligible}}
		view.PersonWorkflows = uxaudit003Workflows()
		items := personActionLauncherItems(view)
		found := false
		for _, item := range items {
			if item.Href != "" && strings.Contains(item.Href, "type=sabbatical") {
				found = true
			}
		}
		if !found {
			t.Fatalf("adding a new PersonWorkflow catalogue entry did not surface in the launcher without launcher-specific code: %+v", items)
		}
	})

	t.Run("a blocked-but-authorized worker is explained, not omitted", func(t *testing.T) {
		view := testView(PageHome)
		view.EffectivePermissions = []RolePagePermission{{Page: PageJourneys, View: true, Create: true}}
		view.People = []Person{{ID: "worker-ineligible", Name: "Ineligible Worker", PromotionAvailability: PromotionIneligible}}
		view.PersonWorkflows = uxaudit003Workflows()
		items := personActionLauncherItems(view)
		row := disabledItemMentioning(items, "Ineligible Worker")
		if row == nil {
			t.Fatal("a blocked worker disappeared from the launcher instead of being explained")
		}
		if row.Href != "" {
			t.Fatalf("a blocked action rendered launchable: %+v", row)
		}
		if row.Reason == "" || row.Availability.Reason != row.Reason {
			t.Fatalf("a blocked action carried no explanation: %+v", row)
		}
	})

	t.Run("the zero-value PromotionAvailability never renders launchable", func(t *testing.T) {
		view := testView(PageHome)
		view.EffectivePermissions = []RolePagePermission{{Page: PageJourneys, View: true, Create: true}}
		view.People = []Person{{ID: "worker-unset", Name: "No Server Verdict"}}
		view.PersonWorkflows = []PersonWorkflow{
			{ID: "promotion", Name: "Promotion", LaunchHref: func(id string) string { return "/workspace/app/journeys?mode=new&worker=" + id }},
		}
		items := personActionLauncherItems(view)
		for _, item := range items {
			if item.Href != "" {
				t.Fatalf("a person with no server verdict was offered a launchable action: %+v", item)
			}
		}
	})

	t.Run("navigation-only framing when no ranked action survives", func(t *testing.T) {
		view := testView(PageHome)
		view.PersonWorkflows = nil // no catalogue at all -- nothing to rank.
		items := append(personActionLauncherItems(view), navigationLauncherItems(view)...)
		if !actionLauncherIsNavigationOnly(items) {
			t.Fatalf("with no workflow catalogue, the launcher still claims action framing: %+v", items)
		}
	})

	t.Run("action framing once a ranked action exists", func(t *testing.T) {
		view := testView(PageHome)
		view.PersonWorkflows = uxaudit003Workflows()
		items := append(personActionLauncherItems(view), navigationLauncherItems(view)...)
		if actionLauncherIsNavigationOnly(items) {
			t.Fatal("a real ranked action is present, but the launcher still claims navigation-only framing")
		}
	})
}

// TestTodo_UXAUDIT_003_Browser is the BROWSER matrix entry. It pins the
// markup contract a real browser renders: a launchable action is a real
// anchor (so click and middle-click both work); a disabled, explained
// action is never an anchor (there is nothing to navigate to or execute)
// and instead carries its reason through aria-describedby to an element
// that actually holds the text; and the closed launcher's rendered
// subtree contains none of this per-worker content at all, matching the
// pinned web040GoldenDigest fixed alongside this todo.
func TestTodo_UXAUDIT_003_Browser(t *testing.T) {
	view := testView(PageHome)
	view.EffectivePermissions = []RolePagePermission{{Page: PageJourneys, View: true, Create: true}}
	view.People = []Person{
		{ID: "worker-eligible", Name: "Eligible Worker", PromotionAvailability: PromotionEligible},
		{ID: "worker-blocked", Name: "Blocked Worker", PromotionAvailability: PromotionIneligible},
	}
	view.PersonWorkflows = uxaudit003Workflows()
	items := append(personActionLauncherItems(view), navigationLauncherItems(view)...)

	opened, err := ui.RenderToString(ui.CreateElement(ActionLauncher, ActionLauncherProps{
		I18nProps: I18nProps{Locale: view.Locale}, Items: items, InitialQuery: "worker",
	}))
	if err != nil {
		t.Fatal(err)
	}

	root, err := xhtml.Parse(strings.NewReader(opened))
	if err != nil {
		t.Fatal(err)
	}

	eligible := itemMentioning(items, "Eligible Worker")
	if eligible == nil || eligible.Href == "" {
		t.Fatal("fixture lost its launchable eligible-worker action")
	}
	link := linkForRoute(root, eligible.Href)
	if link == nil {
		t.Fatalf("launchable action did not render as a real anchor with href %q", eligible.Href)
	}
	if attr(link, "role") != "option" {
		t.Fatal("launchable action option lost its role=option")
	}

	blocked := disabledItemMentioning(items, "Blocked Worker")
	if blocked == nil || blocked.Reason == "" {
		t.Fatal("fixture lost its blocked-worker explanation")
	}
	if linkForRoute(root, "") != nil {
		// Guard against a false-positive empty href ever matching by
		// accident; the real assertion is the loop below.
		t.Fatal("an anchor with an empty href was rendered")
	}
	var blockedRow *xhtml.Node
	walkElements(root, func(node *xhtml.Node) {
		if blockedRow == nil && attr(node, "aria-disabled") == "true" {
			blockedRow = node
		}
	})
	if blockedRow == nil {
		t.Fatal("the blocked action did not render an aria-disabled row")
	}
	if blockedRow.Data == "a" {
		t.Fatal("the blocked action rendered as a real link -- nothing here should be navigable or executable")
	}
	descID := attr(blockedRow, "aria-describedby")
	if descID == "" {
		t.Fatal("the blocked action's row does not reference its explanation by id")
	}
	description := findElementByID(root, descID)
	if description == nil {
		t.Fatal("the blocked action's aria-describedby target does not exist")
	}
	var descText strings.Builder
	walkElements(description, func(node *xhtml.Node) {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == xhtml.TextNode {
				descText.WriteString(child.Data)
			}
		}
	})
	if descText.String() != compactActionLauncherReason(blocked.Reason) {
		t.Fatalf("described reason text %q is not the concise resolved reason %q", descText.String(), blocked.Reason)
	}

	css := Stylesheet()
	if !strings.Contains(css, ".action-launcher-result-unavailable{") {
		t.Fatal("stylesheet is missing the disabled-action row style")
	}

	// The closed launcher (default SSR state, no query) never embeds this
	// per-worker content at all -- see the results-gate doc comment on
	// ActionLauncher in action_launcher.go.
	closedDoc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	closedRoot, err := xhtml.Parse(strings.NewReader(closedDoc))
	if err != nil {
		t.Fatal(err)
	}
	closedLauncher := findElementByID(closedRoot, "action-launcher")
	if closedLauncher == nil {
		t.Fatal("shell rendered no global action launcher")
	}
	var closedMarkup strings.Builder
	if err := xhtml.Render(&closedMarkup, closedLauncher); err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"Eligible Worker", "Blocked Worker"} {
		if strings.Contains(closedMarkup.String(), leaked) {
			t.Fatalf("closed launcher leaked %q before anyone opened it", leaked)
		}
	}
}

// TestTodo_UXAUDIT_003_Accessibility is the ACCESSIBILITY matrix entry.
// RED's second clause was that Escape did not close the launcher (the
// trigger's aria-expanded stayed "true"). This proves the real dismissal
// and focus contract rather than only the presence of attributes:
// aria-expanded tracks the actual open state on both the trigger and the
// input, the input's Escape handling is wired through the one predicate
// every shell dialog shares (drawerEscapeCloses, landmarks.go) rather
// than a second, launcher-local copy of "== Escape" that could drift,
// and outside-focus/pointer dismissal is wired through the shared
// usePopoverFocusDismissal used by the other shell popovers. The
// Escape-closes-and-restores-focus DOM behavior itself is a
// syscall/js-backed effect (popover_focus_wasm.go) that only runs under
// GOOS=js GOARCH=wasm; this repository's own convention for that lane
// (drawer_focus_wasm_test.go, mount_wasm_test.go, main_wasm_test.go) is a
// same-tagged unit test that CI only builds, never executes, so it is
// not exercised by this package's normal test run either -- documented
// in this todo's report rather than claimed as covered here.
func TestTodo_UXAUDIT_003_Accessibility(t *testing.T) {
	t.Run("aria-expanded tracks the actual open state, closed and open", func(t *testing.T) {
		items := []ActionLauncherItem{{ID: "start:people", Label: "People", Href: "/workspace/app/people", IsNavigationDestination: true}}
		for _, tc := range []struct {
			query, expanded string
		}{{"", "false"}, {"people", "true"}} {
			doc, err := ui.RenderToString(ui.CreateElement(ActionLauncher, ActionLauncherProps{InitialQuery: tc.query, Items: items}))
			if err != nil {
				t.Fatal(err)
			}
			root, err := xhtml.Parse(strings.NewReader(doc))
			if err != nil {
				t.Fatal(err)
			}
			trigger := findElementByID(root, "action-launcher-trigger")
			if trigger == nil || attr(trigger, "aria-expanded") != tc.expanded {
				t.Fatalf("query %q: trigger aria-expanded = %q, want %q", tc.query, attr(trigger, "aria-expanded"), tc.expanded)
			}
			dialog := findElementByID(root, "action-launcher-dialog")
			wantHidden := tc.expanded == "false"
			if hasAttr(dialog, "hidden") != wantHidden {
				t.Fatalf("query %q: dialog hidden = %v, want %v", tc.query, hasAttr(dialog, "hidden"), wantHidden)
			}
		}
	})

	t.Run("Escape is wired through the shared dismissal predicate, not a second copy", func(t *testing.T) {
		source, err := os.ReadFile("action_launcher.go")
		if err != nil {
			t.Fatal(err)
		}
		body := string(source)
		if !strings.Contains(body, "drawerEscapeCloses(event.GetKey())") {
			t.Fatal("ActionLauncher does not wire its keydown handler through the shared drawerEscapeCloses predicate (landmarks.go)")
		}
	})

	t.Run("outside-focus dismissal is wired through the shared popover hook", func(t *testing.T) {
		source, err := os.ReadFile("action_launcher.go")
		if err != nil {
			t.Fatal(err)
		}
		body := string(source)
		if !strings.Contains(body, `usePopoverFocusDismissal("action-launcher", "action-launcher-trigger", open.Get()`) {
			t.Fatal("ActionLauncher does not wire outside-focus/pointer/Escape dismissal through the shared usePopoverFocusDismissal hook")
		}
	})

	t.Run("a disabled action is described, not merely marked", func(t *testing.T) {
		items := []ActionLauncherItem{{ID: "action-unavailable:worker-x", Label: "Start Promotion for Worker X", Reason: "No eligible promotion role is published for this employee.", Description: "No eligible promotion role is published for this employee."}}
		doc, err := ui.RenderToString(ui.CreateElement(ActionLauncher, ActionLauncherProps{InitialQuery: "worker", Items: items}))
		if err != nil {
			t.Fatal(err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		var row *xhtml.Node
		walkElements(root, func(node *xhtml.Node) {
			if row == nil && attr(node, "aria-disabled") == "true" {
				row = node
			}
		})
		if row == nil {
			t.Fatal("no disabled row rendered")
		}
		descID := attr(row, "aria-describedby")
		if descID == "" || findElementByID(root, descID) == nil {
			t.Fatal("disabled row's aria-describedby does not resolve to a real element")
		}
	})

	t.Run("keyboard search narrows the ranked results", func(t *testing.T) {
		view := testView(PageHome)
		view.EffectivePermissions = []RolePagePermission{{Page: PageJourneys, View: true, Create: true}}
		view.People = []Person{
			{ID: "worker-jordan", Name: "Jordan Lee", PromotionAvailability: PromotionEligible},
			{ID: "worker-avery", Name: "Avery Patel", PromotionAvailability: PromotionEligible},
		}
		view.PersonWorkflows = uxaudit003Workflows()
		items := append(personActionLauncherItems(view), navigationLauncherItems(view)...)
		all := RankActionLauncherItems(items, "", actionLauncherLimit)
		narrowed := RankActionLauncherItems(items, "avery", actionLauncherLimit)
		for _, item := range all {
			if item.SearchOnly {
				t.Fatalf("unfiltered launcher includes a named-worker row: %+v", item)
			}
		}
		if len(narrowed) == 0 {
			t.Fatal("typing a specific worker's name found no result")
		}
		for _, item := range narrowed {
			if strings.Contains(item.Label+item.Description, "Jordan Lee") {
				t.Fatalf("search for %q matched an unrelated worker: %+v", "avery", item)
			}
		}
	})
}

// TestTodo_UXAUDIT_003_Security is the SECURITY matrix entry. It proves
// the no-disclosure property by value: a viewer who cannot perform an
// action at all -- not merely one blocked for a specific worker -- sees
// the identical reason for every worker regardless of that worker's real
// underlying state, and the rendered markup for that viewer never
// contains the underlying, more specific facts (an unpublished ladder, an
// in-flight conflict) that an authorized viewer is allowed to see.
func TestTodo_UXAUDIT_003_Security(t *testing.T) {
	underlyingCodes := []PromotionAvailabilityCode{PromotionEligible, PromotionIneligible, PromotionActiveConflict, PromotionAvailabilityCode("")}

	t.Run("an unauthorized viewer sees one indistinguishable reason for every worker", func(t *testing.T) {
		view := testView(PageHome)
		view.EffectivePermissions = []RolePagePermission{{Page: PageJourneys, View: true, Create: false}}
		view.PersonWorkflows = uxaudit003Workflows()
		reasons := map[string]bool{}
		for index, code := range underlyingCodes {
			view.People = []Person{{ID: "worker-under-test", Name: "Under Test", PromotionAvailability: code}}
			items := personActionLauncherItems(view)
			if len(items) != 1 {
				t.Fatalf("code %d=%q: items = %+v, want exactly one explained row", index, code, items)
			}
			if items[0].Href != "" {
				t.Fatalf("code %d=%q: unauthorized viewer received a launchable action", index, code)
			}
			if items[0].Reason == "" {
				t.Fatalf("code %d=%q: unauthorized viewer received no reason at all", index, code)
			}
			reasons[items[0].Reason] = true
		}
		if len(reasons) != 1 {
			t.Fatalf("unauthorized viewer saw %d distinct reasons across eligible/ineligible/conflict/unset workers, want exactly 1: %v", len(reasons), reasons)
		}

		// The rendered markup carries the shared reason and nothing an
		// authorized viewer would additionally learn.
		view.People = []Person{{ID: "worker-conflict", Name: "Conflicted Worker", PromotionAvailability: PromotionActiveConflict}}
		items := personActionLauncherItems(view)
		opened, err := ui.RenderToString(ui.CreateElement(ActionLauncher, ActionLauncherProps{
			I18nProps: I18nProps{Locale: view.Locale}, Items: items, InitialQuery: "conflicted",
		}))
		if err != nil {
			t.Fatal(err)
		}
		for _, fact := range []string{
			view.Locale.Text("workflow.promotion_active_conflict"),
			view.Locale.Text("workflow.no_promotion_path"),
		} {
			if strings.Contains(opened, fact) {
				t.Fatalf("unauthorized viewer's markup leaked the specific underlying fact %q", fact)
			}
		}
		if !strings.Contains(opened, view.Locale.Text("workflow.promotion_withheld")) {
			t.Fatal("unauthorized viewer's markup does not carry the generic, non-revealing reason at all")
		}
	})

	t.Run("an authorized viewer sees the real reasons differ", func(t *testing.T) {
		view := testView(PageHome)
		view.EffectivePermissions = []RolePagePermission{{Page: PageJourneys, View: true, Create: true}}
		view.PersonWorkflows = uxaudit003Workflows()
		view.People = []Person{
			{ID: "worker-ineligible", Name: "Ineligible Worker", PromotionAvailability: PromotionIneligible},
			{ID: "worker-conflict", Name: "Conflicted Worker", PromotionAvailability: PromotionActiveConflict},
		}
		items := personActionLauncherItems(view)
		ineligible := disabledItemMentioning(items, "Ineligible Worker")
		conflict := disabledItemMentioning(items, "Conflicted Worker")
		if ineligible == nil || conflict == nil {
			t.Fatalf("lost a blocked worker's explained row: %+v", items)
		}
		if ineligible.Reason == "" || conflict.Reason == "" {
			t.Fatal("an authorized viewer lost a reason")
		}
		if ineligible.Reason == conflict.Reason {
			t.Fatalf("an authorized viewer saw indistinguishable reasons for ineligible vs active-conflict: %q", ineligible.Reason)
		}
	})
}

// TestTodo_UXAUDIT_003_Regression is the REGRESSION matrix entry. It
// proves the extraction this todo made (personWorkflowActions, factored
// out of page_people.go's peopleRowProps loop so the People directory and
// the shell launcher share one resolver instead of two copies) preserved
// the People directory's own PROMOUX-001/PROMOUX-002 behavior exactly,
// and that the launcher's pre-existing WEB-040 invariants (destinations
// survive as a fallback, no href ever executes) still hold.
func TestTodo_UXAUDIT_003_Regression(t *testing.T) {
	t.Run("the People directory's row actions are unchanged by the extraction", func(t *testing.T) {
		view := testView(PagePeople)
		view.PersonWorkflows = []PersonWorkflow{
			{ID: "promotion", Name: "Promotion", LaunchHref: func(id string) string { return "/workspace/app/journeys?mode=new&worker=" + id }},
		}
		view.People = []Person{
			{ID: "worker-avery", Name: "Avery Patel", PromotionAvailability: PromotionEligible},
			{ID: "worker-ineligible", Name: "Ineligible Worker", PromotionAvailability: PromotionIneligible},
		}
		rows := peopleRowProps(view, peoplePageWindow{People: view.People, Total: 2, PageCount: 1, Page: 1})
		if len(rows) != 2 {
			t.Fatalf("rows = %d, want 2", len(rows))
		}
		eligible, ineligible := rows[0], rows[1]
		if len(eligible.QuickActions) != 1 || eligible.QuickActions[0].Href != "/workspace/app/journeys?mode=new&worker=worker-avery" {
			t.Fatalf("eligible worker's row action shape changed: %+v", eligible.QuickActions)
		}
		if eligible.WorkflowsUnavailableReason != "" {
			t.Fatalf("eligible worker gained an unavailable reason: %q", eligible.WorkflowsUnavailableReason)
		}
		if len(ineligible.QuickActions) != 0 {
			t.Fatalf("ineligible worker gained a row action: %+v", ineligible.QuickActions)
		}
		if ineligible.WorkflowsUnavailableReason != PromotionAvailabilityReason(view.Locale, PromotionIneligible) {
			t.Fatalf("ineligible worker's row reason changed: %q", ineligible.WorkflowsUnavailableReason)
		}
	})

	t.Run("the launcher still falls back to its page destinations", func(t *testing.T) {
		view := testView(PageHome)
		items := append(personActionLauncherItems(view), navigationLauncherItems(view)...)
		if itemWithHref(items, statefulHref(view, PageJourneys)) == nil {
			t.Fatal("launcher lost the authorized promotion-start destination")
		}
		if itemWithHref(items, statefulHref(view, PagePeople)) == nil {
			t.Fatal("launcher lost the authorized worker-selection destination")
		}
	})

	t.Run("no launcher href ever executes rather than navigates", func(t *testing.T) {
		view := testView(PageHome)
		items := append(personActionLauncherItems(view), navigationLauncherItems(view)...)
		for _, item := range items {
			if item.Href == "" {
				continue
			}
			if !strings.HasPrefix(item.Href, "/workspace/app/") {
				t.Fatalf("launcher href %q escapes the application shell", item.Href)
			}
			for _, verb := range []string{"execute", "decide", "approve", "complete", "resume"} {
				if strings.Contains(strings.ToLower(item.Href), verb) {
					t.Fatalf("launcher href %q executes instead of navigating", item.Href)
				}
			}
		}
	})
}
