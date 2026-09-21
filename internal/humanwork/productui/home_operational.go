package productui

import (
	"fmt"
	"strings"
	"time"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UXLIVE-030: the populated Home is an operational summary composed from the
// shared cards (WorkCollection, TrackedRequests, SummaryCard) over the
// server's one authorized journey summary. Every number on it links to the
// page that lists exactly the population it counts.

const (
	homeExceptionLimit     = 3
	homeRecentRequestLimit = 3
)

// FactLink renders a fact's value as a drill-down. The visible text is the
// number; the fact's label is repeated for assistive technology so the link
// is not announced as a bare number.
func FactLink(props FactProps) ui.Node {
	return softwareLink(props.Navigate, html.Props{Class: "fact-link"}, props.Href,
		ui.Text(props.Value),
		html.Span(html.Props{Class: "sr-only"}, ui.Text(" "+props.Label)),
	)
}

// homeOperationalGroups is the Home summary card: three scopes, each titled,
// so one word never carries two different numbers (UXLIVE-030). Journey
// counts come from the server summary; people counts from the authorized
// directory. Each number links to the page that lists exactly what it counts:
//
//   - Your work: needs your action -> My Work's default queue; requests you
//     started -> My Work's tracked tab.
//   - Requests you can see: in progress -> Journeys, status open; stopped
//     with a problem -> Journeys, status issue (blocked, failed, repair);
//     completed or closed -> History (closed journeys).
//   - People you can see: visible -> People; eligible -> People, eligible.
func homeOperationalGroups(view View, totals JourneyPopulation, workVisible, historyVisible, peopleVisible bool, visiblePeople []Person) []FactGroupProps {
	groups := make([]FactGroupProps, 0, 3)
	fact := func(key string, value int, allowed bool, page PageID, pairs ...string) FactProps {
		props := FactProps{Label: view.Locale.Text(key), Value: view.Locale.FormatNumber(fmt.Sprint(value), 0), Navigate: view.Navigate}
		if allowed {
			props.Href = statefulHref(view, page, pairs...)
		}
		return props
	}
	// My Work lists nothing for a viewer the directory could not bind to a
	// worker (UXAUDIT-017), so its counts are offered only to a bound viewer:
	// a number never links to a page that lists fewer.
	if workVisible && view.Viewer.PersonID != "" {
		groups = append(groups, FactGroupProps{Title: view.Locale.Text("home.group_your_work"), Facts: []FactProps{
			fact("home.needs_action", totals.NeedsAction, true, PageWork),
			fact("home.following", totals.Tracking, true, PageWork, "filter", "tracked"),
		}})
	}
	journeysVisible := view.Allows(PageJourneys, "view")
	var requests []FactProps
	if workVisible || journeysVisible {
		requests = append(requests,
			fact("home.in_progress", totals.Active, journeysVisible, PageJourneys, JourneyListStatusKey, JourneyListStatusOpen),
			fact("home.exceptions", totals.Exceptions, journeysVisible, PageJourneys, JourneyListStatusKey, JourneyListStatusIssue),
		)
	}
	if historyVisible {
		requests = append(requests, fact("home.closed", totals.Closed, true, PageHistory))
	}
	if len(requests) > 0 {
		groups = append(groups, FactGroupProps{Title: view.Locale.Text("home.group_requests"), Facts: requests})
	}
	if peopleVisible {
		eligible := 0
		for _, person := range visiblePeople {
			if personPromotionEligible(person) {
				eligible++
			}
		}
		groups = append(groups, FactGroupProps{Title: view.Locale.Text("home.group_people"), Facts: []FactProps{
			fact("home.visible_workers", len(visiblePeople), true, PagePeople),
			fact("home.eligible_workers", eligible, true, PagePeople, "eligible", "1"),
		}})
	}
	return groups
}

// homeRecentRequests is the bounded continuity card over the open requests
// the viewer can see, most recently changed first (the server's list order),
// each opening its journey. It is hidden when there are none.
func homeRecentRequests(view View) (TrackedRequestsProps, bool) {
	var open []WorkItem
	for _, item := range admittedWork(view) {
		if !item.Terminal {
			open = append(open, item)
		}
	}
	if len(open) == 0 {
		return TrackedRequestsProps{}, false
	}
	props := homeJourneyRows(view, open, true)
	props.Title = view.Locale.Text("home.recent_requests_title")
	props.Description = view.Locale.Text("home.recent_requests_description")
	if len(props.Items) > homeRecentRequestLimit {
		props.Items = props.Items[:homeRecentRequestLimit]
	}
	if view.Allows(PageJourneys, "view") {
		props.More = ActionLinkProps{Label: view.Locale.Text("home.exceptions_view_all"), Href: statefulHref(view, PageJourneys, JourneyListStatusKey, JourneyListStatusOpen), Navigate: view.Navigate}
	}
	return props, len(props.Items) > 0
}

// homeExceptions is the bounded exceptions card: the shared TrackedRequests
// card over the journeys the server counted as exceptions, newest first as
// the admitted list orders them. It is empty (and hidden) when the summary
// counts none.
func homeExceptions(view View, totals JourneyPopulation) (TrackedRequestsProps, bool) {
	if totals.Exceptions == 0 {
		return TrackedRequestsProps{}, false
	}
	var items []WorkItem
	for _, item := range admittedWork(view) {
		if workIsException(item) {
			items = append(items, item)
		}
	}
	props := homeJourneyRows(view, items, false)
	props.Title = view.Locale.Text("home.exceptions_title")
	props.Description = view.Locale.Text("home.exceptions_description")
	if len(props.Items) > homeExceptionLimit {
		props.Items = props.Items[:homeExceptionLimit]
	}
	if view.Allows(PageJourneys, "view") {
		props.More = ActionLinkProps{Label: view.Locale.Text("home.exceptions_view_all"), Href: statefulHref(view, PageJourneys, JourneyListStatusKey, JourneyListStatusIssue), Navigate: view.Navigate}
	}
	return props, len(props.Items) > 0
}

// homeJourneyRows builds the rows of Home's journey cards on the shared
// TrackedRequests card: the human task label ("Promotion for Andre"), one
// localized status, and one labelled date in the product date format --
// when the request last changed for recent requests, its effective date for
// requests with problems.
func homeJourneyRows(view View, items []WorkItem, updated bool) TrackedRequestsProps {
	props := TrackedRequestsProps{I18nProps: I18nProps{Locale: view.Locale}}
	for _, item := range items {
		date := ""
		if updated {
			if at, ok := parseWorkInstant(item.CompletedAt); ok {
				date = view.Locale.Text("home.row_updated", map[string]string{"date": formatCivilDateLabel(view.Locale, at)})
			}
		} else if at, err := time.Parse("2006-01-02", strings.TrimSpace(item.EffectiveDate)); err == nil {
			date = view.Locale.Text("work.row_effective_date", map[string]string{"date": formatCivilDateLabel(view.Locale, at)})
		}
		props.Items = append(props.Items, TrackedRequestProps{
			ID: item.ID, Title: homeTaskLabel(view, item), Status: localizedWorkStatus(view.Locale, item),
			// Open only suppresses the card's closed suffix: the status
			// already says how a closed request ended.
			Due: date, Open: true, Href: item.Href, Navigate: view.Navigate,
		})
	}
	return props
}

// homeTaskLabel is the human task label UXLIVE-032 introduced for journey
// cards: "Promotion for Andre". The name is shown only when the person is
// one the viewer may see (a per-record verdict that withholds the person
// withholds the name too); otherwise, and for a journey without a name, the
// item keeps its own localized title.
func homeTaskLabel(view View, item WorkItem) string {
	name := strings.TrimSpace(item.Person)
	if name != "" && peopleVerdictsPresent(view) {
		personID := stablePersonID(view.People, item.PersonRef)
		if personID == "" {
			personID = item.PersonRef
		}
		if !DiscoveryAdmitted(personID, view.RecordVerdicts) {
			name = ""
		}
	}
	promotion := item.TitleKey == "journey.detail_title" || item.TitleKey == "" && item.Title == ""
	if name != "" && promotion {
		return view.Locale.Text("journey.card_title", map[string]string{"name": name})
	}
	if title := strings.TrimSpace(localizedWorkTitle(view.Locale, item)); title != "" {
		return title
	}
	return view.Locale.Text("journey.card_title_unnamed")
}

// parseWorkInstant reads the instant the product client stores on a work
// item ("2 Jan 2006 · 15:04 UTC").
func parseWorkInstant(value string) (time.Time, bool) {
	at, err := time.Parse("2 Jan 2006 · 15:04 UTC", strings.TrimSpace(value))
	return at, err == nil
}

// formatCivilDateLabel renders a date in the product vocabulary: "1 Dec
// 2026" in en-US, the localized date elsewhere.
func formatCivilDateLabel(locale LocaleContext, at time.Time) string {
	if locale.Resolved == "" || locale.Resolved == DefaultProductLocale {
		return at.UTC().Format("2 Jan 2006")
	}
	return locale.FormatDate(at.UTC())
}

// formatInstantLabel renders an instant in the product's one date
// vocabulary (UXLIVE-016): "2 Jan 2006, 15:04 UTC", localized date first
// outside en-US.
func formatInstantLabel(locale LocaleContext, at time.Time) string {
	at = at.UTC()
	if locale.Resolved == "" || locale.Resolved == DefaultProductLocale {
		return at.Format("2 Jan 2006, 15:04") + " UTC"
	}
	return locale.FormatDate(at) + ", " + at.Format("15:04") + " UTC"
}

// homeOperationalStylesheet: summary numbers read as the product's links --
// accent colour, underline only on hover or focus, tabular numerals -- and
// their labels and group titles are readable body text.
func homeOperationalStylesheet() string {
	return buildTypedSheet(func() {
		declareGlobal(".facts dd .fact-link",
			gwccss.Raw("color", "var(--accent)"),
			gwccss.Raw("text-decoration", "none"),
			gwccss.Raw("font-variant-numeric", "tabular-nums"),
			gwccss.Raw("font-weight", "700"),
		)
		declareGlobal(".facts dd .fact-link:hover,.facts dd .fact-link:focus-visible",
			gwccss.Raw("text-decoration", "underline"),
			gwccss.Raw("text-underline-offset", "3px"),
		)
		declareGlobal(".fact-group .facts dt",
			gwccss.Raw("color", "var(--ink)"),
			gwccss.FontSize(gwccss.Rem(0.875)),
		)
		declareGlobal(".fact-group-title",
			gwccss.Raw("margin", "14px 22px 0"),
			gwccss.FontSize(gwccss.Rem(0.75)),
			gwccss.Raw("font-weight", "700"),
			gwccss.Raw("letter-spacing", "0.02em"),
			gwccss.Raw("text-transform", "uppercase"),
			gwccss.Raw("color", "var(--muted)"),
		)
		// A card description sits on the same inset as its title and rows.
		// Card descriptions use the one section-head description style
		// (.section-head p: 0.875rem, line-height 1.5) on the card inset.
		declareGlobal(".tracked-requests-panel>div>p.muted,.recent-people-panel .recent-people-intro",
			gwccss.Raw("padding-inline", "22px"),
			gwccss.Raw("margin", "0 0 8px"),
			gwccss.FontSize(gwccss.Rem(0.875)),
			gwccss.Raw("line-height", "1.5"),
		)
		// A non-successful outcome is not marked with a success check.
		declareGlobal(".activity>.check.activity-outcome",
			gwccss.Raw("background", "var(--surface-subtle,var(--surface))"),
			gwccss.Raw("color", "var(--muted)"),
		)
		declareGlobal(".activity>.check.activity-outcome-danger",
			gwccss.Raw("color", "var(--danger)"),
		)
		declareGlobal(".fact-group+.fact-group",
			gwccss.Raw("border-top", "1px solid var(--line)"),
		)
		// On a single-column Home the rails dissolve into one ordered list:
		// the attention queue, then the summary, then the populated lists.
		declareGlobal(".home-grid:not(.home-grid-empty)>.home-primary-rail,.home-grid:not(.home-grid-empty)>.home-supporting-rail",
			mediaRule(gwccss.MaxW(990), gwccss.Raw("display", "contents")),
		)
		declareGlobal(".home-grid .work-list[data-work-kind=\"action-queue\"]",
			mediaRule(gwccss.MaxW(990), gwccss.Raw("order", "-2")),
		)
		declareGlobal(".home-grid .home-summary-card",
			mediaRule(gwccss.MaxW(990), gwccss.Raw("order", "-1")),
		)
	})
}
