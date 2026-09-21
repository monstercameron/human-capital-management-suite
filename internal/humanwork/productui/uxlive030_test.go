package productui

import (
	"html"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UXLIVE-030: a populated Home is an operational summary -- bounded attention
// queue, tracked and exception journeys, a small workforce snapshot -- over
// the server's one authorized journey summary (UXLIVE-027), and every number
// on it drills into the exact population it counts.

var homeFactPattern = regexp.MustCompile(`<dt>([^<]+)</dt><dd>(?:<a [^>]*class="fact-link"[^>]*href="([^"]*)"[^>]*>|<a [^>]*href="([^"]*)"[^>]*class="fact-link"[^>]*>)?(\d+)`)

// homeFactValue returns the value Home renders for one summary fact label, or
// "" when the fact is absent.
func homeFactValue(markup, label string) string {
	value, _ := homeFact(markup, label)
	return value
}

// homeFact returns a fact's value and its drill-down href ("" when the value
// is not a link).
func homeFact(markup, label string) (value, href string) {
	for _, match := range homeFactPattern.FindAllStringSubmatch(markup, -1) {
		if html.UnescapeString(match[1]) == label {
			href = match[2]
			if href == "" {
				href = match[3]
			}
			return match[4], html.UnescapeString(href)
		}
	}
	return "", ""
}

// uxlive030View is promoux012's populated workspace plus two exception
// journeys and the server summary over exactly that list.
func uxlive030View() View {
	view := promoux012View(PageHome)
	view.SelectedPerson = ""
	view.Work = append(view.Work,
		WorkItem{ID: "failed-run", Person: "Iris Holm", PersonRef: "worker-iris", Title: "Promotion journey", Status: "Failed", StatusKey: "journey.stage_failed",
			Terminal: true, Href: "/workspace/app/journeys?journey=failed-run", ViewerResponsibility: "CLOSED"},
		WorkItem{ID: "repair-run", Person: "Noor Aziz", PersonRef: "worker-noor", Title: "Promotion journey", Status: "Repair required", StatusKey: "journey.stage_repair_required",
			Href: "/workspace/app/journeys?journey=repair-run", ViewerResponsibility: "OBSERVING"},
	)
	for index := range view.Work {
		if view.Work[index].ID == "own-blocked" {
			view.Work[index].StatusKey = "journey.stage_blocked"
		}
	}
	population := JourneyPopulation{}
	for _, item := range view.Work {
		population.Total++
		if item.Terminal {
			population.Closed++
		} else {
			population.Active++
			if item.ViewerResponsibility == "ACTION_REQUIRED" {
				population.NeedsAction++
			}
			if WorkViewerInitiated(item) {
				population.Tracking++
			}
		}
		if workIsException(item) {
			population.Exceptions++
		}
	}
	view.JourneyPopulation = &population
	return view
}

func uxlive030Render(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return markup
}

// TestTodo_UXLIVE_030 is the PRIMARY case: the populated Home shows the
// bounded attention queue, tracked journeys, the exceptions card and the
// workforce snapshot, with every count taken from the server summary.
func TestTodo_UXLIVE_030(t *testing.T) {
	view := uxlive030View()
	totals := *view.JourneyPopulation
	markup := uxlive030Render(t, homePage(view))

	for _, section := range []string{
		view.Locale.Text("home.attention_title"), view.Locale.Text("home.tracked_title"),
		view.Locale.Text("home.recent_requests_title"), view.Locale.Text("home.exceptions_title"),
		view.Locale.Text("home.recent_completed_title"), view.Locale.Text("home.activity_title"),
	} {
		if !strings.Contains(markup, "<h2>"+section+"</h2>") {
			t.Fatalf("populated Home is missing %q:\n%s", section, markup)
		}
	}
	if strings.Contains(markup, view.Locale.Text("home.empty_title")) {
		t.Fatalf("populated Home printed the quiet-workspace claim:\n%s", markup)
	}
	for label, want := range map[string]int{
		view.Locale.Text("home.needs_action"): totals.NeedsAction,
		view.Locale.Text("home.following"):    totals.Tracking,
		view.Locale.Text("home.in_progress"):  totals.Active,
		view.Locale.Text("home.exceptions"):   totals.Exceptions,
		view.Locale.Text("home.closed"):       totals.Closed,
	} {
		if got := homeFactValue(markup, label); got != strconv.Itoa(want) {
			t.Fatalf("Home %q = %q, server summary says %d:\n%s", label, got, want, markup)
		}
	}
	if got := homeFactValue(markup, view.Locale.Text("home.visible_workers")); got != strconv.Itoa(len(admittedPeople(view))) {
		t.Fatalf("workforce snapshot = %q, want %d visible workers", got, len(admittedPeople(view)))
	}

	// The summary is authoritative: a server count Home was not handed rows
	// for still decides the number, so Home never recounts the collection.
	view.JourneyPopulation.NeedsAction = 41
	if got := homeFactValue(uxlive030Render(t, homePage(view)), view.Locale.Text("home.needs_action")); got != "41" {
		t.Fatalf("Home recounted its actions (%s) instead of reading the server summary", got)
	}

	// No card prints empty copy over a non-empty population: the viewer
	// tracks journeys and there are closed ones, so neither empty line shows.
	for _, empty := range []string{view.Locale.Text("home.tracked_empty_title"), view.Locale.Text("home.recent_completed_empty_title"),
		view.Locale.Text("home.drafts_empty_title"), view.Locale.Text("home.recent_people_empty_title")} {
		if strings.Contains(markup, empty) {
			t.Fatalf("Home printed %q over a non-empty population:\n%s", empty, markup)
		}
	}
	// Recent requests lists open visible journeys, bounded, each linked;
	// recently completed links each closed journey.
	recent, shown := homeRecentRequests(uxlive030View())
	if !shown || len(recent.Items) != homeRecentRequestLimit {
		t.Fatalf("recent requests = %d rows, want %d", len(recent.Items), homeRecentRequestLimit)
	}
	for _, row := range recent.Items {
		if row.Href == "" || !row.Open {
			t.Fatalf("recent request %+v is not an open, linked journey", row)
		}
	}
	if !strings.Contains(markup, `href="/workspace/app/journeys?journey=past-dana"`) {
		t.Fatalf("recently completed does not open its journey:\n%s", markup)
	}
	// The same word never carries two numbers: the viewer's tab strip says
	// "Blocked"; the visible-request fact does not.
	if strings.Contains(view.Locale.Text("home.exceptions"), view.Locale.Text("work.blocked")) {
		t.Fatalf("the exceptions fact %q reuses the tab label %q", view.Locale.Text("home.exceptions"), view.Locale.Text("work.blocked"))
	}
	for _, group := range []string{"home.group_your_work", "home.group_requests", "home.group_people"} {
		if !strings.Contains(markup, `<h3 class="fact-group-title">`+view.Locale.Text(group)+`</h3>`) {
			t.Fatalf("summary card lacks the %q scope heading:\n%s", view.Locale.Text(group), markup)
		}
	}

	// The exceptions card is bounded and lists only exception journeys.
	exceptions, shown := homeExceptions(uxlive030View(), totals)
	if !shown || len(exceptions.Items) != totals.Exceptions || len(exceptions.Items) > homeExceptionLimit {
		t.Fatalf("exceptions card = %d rows (shown %t), want %d bounded by %d", len(exceptions.Items), shown, totals.Exceptions, homeExceptionLimit)
	}
	for _, row := range exceptions.Items {
		if row.Href == "" || !strings.Contains("own-blocked failed-run repair-run", row.ID) {
			t.Fatalf("exceptions card row %+v is not a linked exception journey", row)
		}
	}
}

// TestTodo_UXLIVE_030_Browser renders the full product shell and checks each
// drill-down addresses the page and filter that list what it counts.
func TestTodo_UXLIVE_030_Browser(t *testing.T) {
	view := uxlive030View()
	markup := uxlive030Render(t, Build(view))
	for label, want := range map[string]string{
		view.Locale.Text("home.needs_action"):     "/workspace/app/work",
		view.Locale.Text("home.following"):        "/workspace/app/work?filter=tracked",
		view.Locale.Text("home.in_progress"):      "/workspace/app/journeys?journey_status=open",
		view.Locale.Text("home.exceptions"):       "/workspace/app/journeys?journey_status=issue",
		view.Locale.Text("home.closed"):           "/workspace/app/history",
		view.Locale.Text("home.visible_workers"):  "/workspace/app/people",
		view.Locale.Text("home.eligible_workers"): "/workspace/app/people?eligible=1",
	} {
		_, href := homeFact(markup, label)
		if path, _, _ := strings.Cut(href, "?"); path != strings.SplitN(want, "?", 2)[0] || (strings.Contains(want, "?") && !strings.Contains(href, strings.SplitN(want, "?", 2)[1])) {
			t.Fatalf("%q drills into %q, want %q", label, href, want)
		}
	}
	if !strings.Contains(markup, "/workspace/app/journeys?journey=repair-run") {
		t.Fatalf("the exceptions card does not open the journey it lists:\n%s", markup)
	}

	// The true all-empty state stays UXSCAN-004's compact, action-led page.
	quiet := testView(PageHome)
	quiet.Work = nil
	quiet.JourneyPopulation = &JourneyPopulation{}
	quiet.People = nil
	quietMarkup := uxlive030Render(t, homePage(quiet))
	if !strings.Contains(quietMarkup, "home-grid-empty") || !strings.Contains(quietMarkup, quiet.Locale.Text("home.empty_title")) ||
		strings.Contains(quietMarkup, quiet.Locale.Text("home.exceptions_title")) {
		t.Fatalf("the all-empty Home lost its compact composition:\n%s", quietMarkup)
	}
}

// TestTodo_UXLIVE_030_Accessibility: a drill-down is announced with its
// label, not as a bare number, and localized in every supported locale.
func TestTodo_UXLIVE_030_Accessibility(t *testing.T) {
	view := uxlive030View()
	markup := uxlive030Render(t, homePage(view))
	link := regexp.MustCompile(`<a [^>]*class="fact-link"[^>]*>(\d+)<span class="sr-only"> ([^<]+)</span></a>`)
	links := link.FindAllStringSubmatch(markup, -1)
	if len(links) < 7 {
		t.Fatalf("fact links = %d, want every summary number linked:\n%s", len(links), markup)
	}
	for _, match := range links {
		if strings.TrimSpace(match[2]) == "" {
			t.Fatalf("drill-down %q has no accessible label", match[0])
		}
	}
	// On one column the attention queue leads and the summary follows it,
	// ahead of the lists.
	assertCSSContains(t, "display:contents", `.work-list[data-work-kind="action-queue"]`, "order:-2", ".home-summary-card", "order:-1")
	// Summary numbers use the product link style: accent colour, underline
	// only on hover or focus, tabular numerals.
	assertCSSContains(t, ".facts dd .fact-link", "color:var(--accent)", "text-decoration:none", "font-variant-numeric:tabular-nums")
	assertCSSContains(t, ".facts dd .fact-link:hover,.facts dd .fact-link:focus-visible", "text-decoration:underline")
	for _, locale := range SupportedProductLocales() {
		localized := ApplyLocale(uxlive030View(), ResolveProductLocale(locale))
		for _, key := range []string{"home.following", "home.exceptions", "home.in_progress", "home.eligible_workers", "home.exceptions_title", "home.exceptions_description", "home.exceptions_view_all",
			"home.group_your_work", "home.group_requests", "home.group_people", "home.recent_requests_title", "home.recent_requests_description"} {
			text := localized.Locale.Text(key)
			if text == "" || text == key || strings.HasPrefix(text, "home.") {
				t.Fatalf("%s has no %s copy: %q", locale, key, text)
			}
			if locale != DefaultProductLocale && text == ApplyLocale(uxlive030View(), ResolveProductLocale(DefaultProductLocale)).Locale.Text(key) {
				t.Fatalf("%s falls back to English for %s", locale, key)
			}
		}
	}
}

// TestTodo_UXLIVE_030_Regression: an unbound viewer is offered no My Work
// count its destination would not list, and a failed read makes no "no work"
// claim.
func TestTodo_UXLIVE_030_Regression(t *testing.T) {
	unbound := uxlive030View()
	unbound.Viewer = ViewerProfile{}
	markup := uxlive030Render(t, homePage(unbound))
	if homeFactValue(markup, unbound.Locale.Text("home.needs_action")) != "" || homeFactValue(markup, unbound.Locale.Text("home.following")) != "" {
		t.Fatalf("an unbound viewer was offered My Work counts:\n%s", markup)
	}
	if homeFactValue(markup, unbound.Locale.Text("home.closed")) == "" {
		t.Fatalf("an unbound viewer lost the counts whose destinations do list them:\n%s", markup)
	}

	failed := testView(PageHome)
	failed.Work, failed.People, failed.JourneyPopulation = nil, nil, nil
	failed.LoadError = "We couldn't load this page. Try again."
	failedMarkup := uxlive030Render(t, homePage(failed))
	if strings.Contains(failedMarkup, failed.Locale.Text("home.empty_title")) {
		t.Fatalf("Home claimed a quiet workspace over a failed read:\n%s", failedMarkup)
	}
}

// TestTodo_UXLIVE_030_RowPresentation pins the Home rows' presentation
// review: human task labels, one human status, labelled product dates, the
// description on the card inset, and a success check only for a successful
// outcome.
func TestTodo_UXLIVE_030_RowPresentation(t *testing.T) {
	view := uxlive030View()
	for index := range view.Work {
		view.Work[index].CompletedAt = "19 Sep 2026 · 10:15 UTC"
		view.Work[index].Title, view.Work[index].TitleKey = "Promotion journey", "journey.detail_title"
	}
	exceptions, _ := homeExceptions(view, *view.JourneyPopulation)
	recent, _ := homeRecentRequests(view)
	for _, row := range append(append([]TrackedRequestProps(nil), exceptions.Items...), recent.Items...) {
		if !strings.HasPrefix(row.Title, "Promotion for ") || row.Person != "" {
			t.Fatalf("row %+v is not titled with its human task label", row)
		}
		if !row.Open {
			t.Fatalf("row %+v would append a raw closed suffix to its status", row)
		}
	}
	for _, row := range recent.Items {
		if row.Due != "Updated 19 Sep 2026" {
			t.Fatalf("recent request date = %q, want a labelled product date", row.Due)
		}
	}
	for _, row := range exceptions.Items {
		if row.Due != "" && !regexp.MustCompile(`^Effective \d{1,2} [A-Z][a-z]{2} \d{4}$`).MatchString(row.Due) {
			t.Fatalf("exception date = %q, want \"Effective 1 Dec 2026\"", row.Due)
		}
	}
	assertCSSContains(t, ".tracked-requests-panel>div>p.muted,.recent-people-panel .recent-people-intro", "padding-inline:22px", "font-size:0.875rem", "line-height:1.5")

	markup := uxlive030Render(t, ActivityList([]ActivityProps{
		{Title: "Promotion for Dana", Status: "Completed", Tone: "success"},
		{Title: "Promotion for Iris", Status: "Failed", Tone: "danger"},
		{Title: "Promotion for Ola", Status: "Rejected", Tone: "neutral"},
	}, "", ""))
	if strings.Count(markup, `class="check"`) != 1 || strings.Count(markup, "activity-outcome-") != 2 ||
		!strings.Contains(markup, `<span class="status danger">Failed</span>`) {
		t.Fatalf("only the successful outcome may carry the check:\n%s", markup)
	}
}

// TestTodo_UXLIVE_030_CatalogCoverage: every string the operational Home
// renders is translated in de-DE and ar, so no English leaks into a row
// (the ar "Effective {date}" leak the live review found).
func TestTodo_UXLIVE_030_CatalogCoverage(t *testing.T) {
	keys := []string{
		"home.following", "home.exceptions", "home.in_progress", "home.eligible_workers", "home.exceptions_title",
		"home.exceptions_description", "home.exceptions_view_all", "home.group_your_work", "home.group_requests",
		"home.group_people", "home.recent_requests_title", "home.recent_requests_description", "home.row_updated",
		"work.row_effective_date", "workflow.promotion_in_progress_title", "journey.card_title", "journey.card_title_unnamed",
		"home.needs_action", "home.closed", "home.visible_workers", "home.activity_title", "home.recent_completed_title",
	}
	english := ResolveProductLocale(DefaultProductLocale)
	for _, locale := range []string{"de-DE", "ar"} {
		localized := ResolveProductLocale(locale)
		for _, key := range keys {
			want := english.Text(key, map[string]string{"date": "D", "name": "N"})
			got := localized.Text(key, map[string]string{"date": "D", "name": "N"})
			if got == "" || got == want || strings.Contains(got, key) {
				t.Errorf("%s %s = %q, the English text leaks or is missing", locale, key, got)
			}
		}
		view := ApplyLocale(uxlive030View(), localized)
		for index := range view.Work {
			view.Work[index].CompletedAt = "19 Sep 2026 · 10:15 UTC"
		}
		exceptions, _ := homeExceptions(view, *view.JourneyPopulation)
		recent, _ := homeRecentRequests(view)
		for _, row := range append(exceptions.Items, recent.Items...) {
			if strings.Contains(row.Due, "Effective") || strings.Contains(row.Due, "Updated") {
				t.Fatalf("%s row date %q leaks English", locale, row.Due)
			}
		}
	}
}

// TestTodo_UXLIVE_030_LocalizedDigits: summary counts on Home and Insights
// use the locale's number formatter, like the rest of the page.
func TestTodo_UXLIVE_030_LocalizedDigits(t *testing.T) {
	arabic := ResolveProductLocale("ar")
	six := arabic.FormatNumber("6", 0)
	if six == "6" {
		t.Fatalf("fixture assumption broken: the ar formatter emits Western digits")
	}
	home := ApplyLocale(uxlive030View(), arabic)
	population := *home.JourneyPopulation
	population.Active = 6
	home.JourneyPopulation = &population
	markup := uxlive030Render(t, homePage(home))
	if !strings.Contains(markup, ">"+six+`<span class="sr-only">`) {
		t.Fatalf("Home summary count is not localized (%q):\n%s", six, markup)
	}
	insights := home
	insights.Page = PageInsights
	if insightsMarkup := uxlive030Render(t, insightsPage(insights)); !strings.Contains(insightsMarkup, "<strong>"+six+"</strong>") {
		t.Fatalf("Insights total is not localized (%q):\n%s", six, insightsMarkup)
	}
}
