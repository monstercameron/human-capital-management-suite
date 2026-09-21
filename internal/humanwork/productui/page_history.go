package productui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const (
	historySortPerson  = "person"
	historySortChange  = "change"
	historySortClosed  = "closed"
	historySortOutcome = "outcome"
)

func historyPage(view View) ui.Node {
	props := workflowHistoryProps(view, "", view.Locale.Text("history.global_title"),
		view.Locale.Text("history.global_detail"), true)
	return ui.CreateElement(WorkflowHistory, props)
}

func workflowHistoryProps(view View, personID, title, description string, withFilter bool) WorkflowHistoryProps {
	return workflowHistoryPropsForTarget(view, personID, targetHistoryPage(personID), title, description, withFilter)
}

func workflowHistoryPropsForTarget(view View, personID string, target PageID, title, description string, withFilter bool) WorkflowHistoryProps {
	universe := historyUniverse(view, personID)
	items := filteredHistory(view, personID)
	window := paginateHistory(items, view.HistoryPage, view.HistoryPageSize)
	rows := make([]WorkflowHistoryItemProps, 0, len(window.Items))
	for _, item := range window.Items {
		personHref := ""
		if personID == "" {
			if id := stablePersonID(view.People, item.PersonRef); id != "" {
				personHref = statefulHref(view, PagePerson, "person", id)
			}
		}
		rows = append(rows, WorkflowHistoryItemProps{
			Type: view.Locale.Text("history.promotion"), Person: item.Person, PersonHref: personHref, Navigate: view.Navigate,
			Initials: item.Initials, PhotoURL: item.PhotoURL, Summary: item.Summary,
			Outcome: historyOutcomeLabel(view.Locale, localizedWorkStatus(view.Locale, item)), Tone: item.Tone,
			EffectiveDate: historyEffectiveDateLabel(view.Locale, item.EffectiveDate),
			CompletedAt:   historyCompletedAtLabel(view.Locale, item.CompletedAt), Href: item.Href,
			Provenance: item.Provenance,
		})
	}
	props := WorkflowHistoryProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Title:     title, Description: description,
		EmptyText: view.Locale.Text("history.empty_terminal"),
		Items:     rows, FilteredCount: len(items), TotalCount: len(universe),
		Density: historyDensityFromTheme(view.EffectiveAppearance()),
	}
	if len(universe) > 0 && len(items) == 0 {
		props.EmptyText = view.Locale.Text("history.none_detail")
	}
	if len(items) > 0 {
		props.Pagination = historyPaginationProps(view, personID, target, window)
	}
	if !withFilter {
		return props
	}

	showPerson := personID == ""
	if isIndividualHistoryTarget(target) {
		view.HistoryPerson = ""
	}
	sortKey := effectiveHistorySort(view.HistorySort)
	direction := effectiveHistoryDirection(view.HistoryDirection)
	filterPersonID := personID
	if target == PageMyself {
		filterPersonID = ""
	}
	filter := &WorkflowHistoryFilterProps{
		Query: view.HistoryQuery, Outcome: view.HistoryOutcome, Person: view.HistoryPerson, Year: view.HistoryYear,
		People: historyPeopleOptions(view, universe), Years: historyYearOptions(universe), ShowPerson: showPerson,
		Action: pageHref(target), ClearHref: historyClearHref(view, target, personID, sortKey, direction),
		PersonID: filterPersonID, DirectoryQuery: view.Query, DirectoryPage: view.PeoplePage, DirectoryTeam: view.PeopleTeam,
		DirectoryPageSize: view.PeoplePageSize, HistoryPageSize: view.HistoryPageSize,
		DirectoryLocation: view.PeopleLocation, DirectorySort: view.PeopleSort, DirectoryDirection: view.PeopleDirection, WorkflowQuery: view.WorkflowQuery,
		Sort: sortKey, Direction: direction, NavCollapsed: view.NavCollapsed, Navigate: view.Navigate,
	}
	if showPerson {
		filter.SearchPlaceholder = view.Locale.Text("history.search_placeholder")
	} else {
		filter.SearchPlaceholder = view.Locale.Text("history.search_person_placeholder")
	}
	if view.Navigate != nil {
		filter.OnFilter = func(query, outcome, selectedPerson, year string) {
			view.Navigate(historyHref(view, target, personID, strings.TrimSpace(query), strings.ToLower(strings.TrimSpace(outcome)),
				strings.TrimSpace(selectedPerson), strings.TrimSpace(year), sortKey, direction))
		}
	}
	props.Filter = filter
	props.Columns = historySortColumns(view, target, personID, sortKey, direction)
	return props
}

func historyClearHref(view View, target PageID, personID, sortKey, direction string) string {
	href := historyHref(view, target, personID, "", "", "", "", sortKey, direction)
	return withExplicitEmptyQuery(href, "history_q", "outcome", "history_person", "history_year")
}

type historyPageWindow struct {
	Page, PageCount, First, Last, Total int
	Items                               []WorkItem
}

// paginateHistory shares its arithmetic with People's paginatePeople via
// PaginateCollection (data_table.go); UXAUDIT-008 REFACTOR removed the
// second, previously identical copy of this math that lived here.
func paginateHistory(items []WorkItem, requestedPage, requestedSize int) historyPageWindow {
	return historyPageWindow(PaginateCollection(items, requestedPage, normalizePageSize(requestedSize)))
}

func targetHistoryPage(personID string) PageID {
	if personID == "" {
		return PageHistory
	}
	return PagePerson
}

func isIndividualHistoryTarget(target PageID) bool {
	return target == PagePerson || target == PageMyself
}

func historyPaginationProps(view View, personID string, target PageID, window historyPageWindow) *PeoplePaginationProps {
	link := func(page int, disabled bool, label string) PaginationLinkProps {
		return PaginationLinkProps{Label: label, Disabled: disabled, Navigate: view.Navigate,
			Href: historyHrefPage(view, target, personID, page, view.HistoryPageSize)}
	}
	fields := map[string]string{"history_q": view.HistoryQuery, "outcome": view.HistoryOutcome, "history_person": view.HistoryPerson, "history_year": view.HistoryYear, "history_sort": view.HistorySort, "history_dir": view.HistoryDirection}
	if target == PagePerson && personID != "" {
		fields["person"] = personID
	}
	return &PeoplePaginationProps{AriaLabel: view.Locale.Text("history.pages"), First: window.First, Last: window.Last, Total: window.Total, Page: window.Page, PageCount: window.PageCount,
		Previous: link(window.Page-1, window.Page <= 1, view.Locale.Text("common.previous")), Next: link(window.Page+1, window.Page >= window.PageCount, view.Locale.Text("common.next")),
		PageSize: pageSizeControlProps(view, target, "history_page_size", view.HistoryPageSize, fields)}
}

func historyHrefPage(view View, target PageID, personID string, page, size int) string {
	href := historyHref(view, target, personID, view.HistoryQuery, view.HistoryOutcome, view.HistoryPerson, view.HistoryYear, effectiveHistorySort(view.HistorySort), effectiveHistoryDirection(view.HistoryDirection))
	separator := "?"
	if strings.Contains(href, "?") {
		separator = "&"
	}
	if page > 1 {
		href += separator + "history_page=" + fmt.Sprint(page)
		separator = "&"
	}
	if normalizePageSize(size) != defaultPageSize {
		href += separator + "history_page_size=" + fmt.Sprint(normalizePageSize(size))
	}
	return href
}

func historyUniverse(view View, personID string) []WorkItem {
	population := admittedWork(view)
	items := make([]WorkItem, 0, len(population))
	for _, item := range population {
		if !item.Terminal {
			continue
		}
		if personID != "" && stablePersonID(view.People, item.PersonRef) != personID {
			continue
		}
		items = append(items, item)
	}
	return items
}

func filteredHistory(view View, personID string) []WorkItem {
	query := strings.ToLower(strings.TrimSpace(view.HistoryQuery))
	outcome := strings.ToLower(strings.TrimSpace(view.HistoryOutcome))
	selectedPerson := strings.TrimSpace(view.HistoryPerson)
	year := strings.TrimSpace(view.HistoryYear)
	items := make([]WorkItem, 0, len(view.Work))
	for _, item := range historyUniverse(view, personID) {
		if selectedPerson != "" && personID == "" && stablePersonID(view.People, item.PersonRef) != selectedPerson {
			continue
		}
		if outcome != "" && historyOutcomeCategory(item.Status) != outcome {
			continue
		}
		if year != "" && historyEffectiveYear(item.EffectiveDate) != year {
			continue
		}
		if query != "" {
			searchable := strings.ToLower(strings.Join([]string{
				item.Title, localizedWorkTitle(view.Locale, item), view.Locale.Text("history.promotion"),
				item.Person, item.Summary, item.Status, localizedWorkStatus(view.Locale, item),
				item.EffectiveDate, historyEffectiveDateLabel(view.Locale, item.EffectiveDate),
				item.CompletedAt, historyCompletedAtLabel(view.Locale, item.CompletedAt), item.InstanceID, item.ID,
			}, " "))
			if !strings.Contains(searchable, query) {
				continue
			}
		}
		items = append(items, item)
	}
	sortHistory(items, effectiveHistorySort(view.HistorySort), effectiveHistoryDirection(view.HistoryDirection))
	return items
}

func historyOutcomeCategory(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "recorded":
		return "completed"
	case "rejected":
		return "rejected"
	case "failed":
		return "failed"
	default:
		return ""
	}
}

// Dates are formatted only for display. Filtering and ordering continue to
// consume canonical service values, not locale-dependent labels.
// historyEffectiveDateLabel renders a stored civil date for the viewer.
//
// Every locale prints the product's civil-date form ("1 Dec 2026" in
// English). The default locale used to keep the stored ISO key because the
// only English form on offer was the ambiguous "12/01/2026"; the product
// formatter now writes English dates the way the rest of the product does,
// so the ISO row was the last second vocabulary (UXLIVE-016).
func historyEffectiveDateLabel(locale LocaleContext, value string) string {
	stamp, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	if err != nil {
		return value
	}
	// An effective date is a civil date, not an instant. Applying the viewer's
	// time zone to midnight UTC could silently move it to the prior day.
	dateLocale := locale.normalized()
	dateLocale.TimeZone = "UTC"
	return dateLocale.FormatDate(stamp)
}

func historyCompletedAtLabel(locale LocaleContext, value string) string {
	if locale.normalized().Resolved == DefaultProductLocale {
		return value
	}
	var stamp time.Time
	var err error
	for _, layout := range []string{"2 Jan 2006 · 15:04 MST", time.RFC3339Nano, time.RFC3339} {
		if stamp, err = time.Parse(layout, strings.TrimSpace(value)); err == nil {
			break
		}
	}
	if err != nil {
		return value
	}
	zone, zoneErr := time.LoadLocation(locale.normalized().TimeZone)
	if zoneErr != nil {
		zone = time.UTC
	}
	clock := stamp.In(zone).Format("15:04 MST")
	if strings.HasPrefix(locale.normalized().Resolved, "ar") {
		clock = strings.Map(func(digit rune) rune {
			if digit >= '0' && digit <= '9' {
				return '٠' + digit - '0'
			}
			return digit
		}, clock)
		// Keep the Latin timezone abbreviation and clock in one LTR run
		// inside the surrounding Arabic label and civil date.
		clock = "\u2066" + clock + "\u2069"
	}
	return locale.FormatDate(stamp) + " · " + clock
}

func historyPeopleOptions(view View, items []WorkItem) []HistoryFilterOption {
	labels := map[string]string{}
	for _, item := range items {
		id := stablePersonID(view.People, item.PersonRef)
		if id != "" && item.Person != "" {
			labels[id] = item.Person
		}
	}
	options := make([]HistoryFilterOption, 0, len(labels))
	for value, label := range labels {
		options = append(options, HistoryFilterOption{Value: value, Label: label})
	}
	sort.Slice(options, func(i, j int) bool { return strings.ToLower(options[i].Label) < strings.ToLower(options[j].Label) })
	return options
}

func historyYearOptions(items []WorkItem) []HistoryFilterOption {
	seen := map[string]bool{}
	for _, item := range items {
		if year := historyEffectiveYear(item.EffectiveDate); year != "" {
			seen[year] = true
		}
	}
	years := make([]string, 0, len(seen))
	for year := range seen {
		years = append(years, year)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(years)))
	options := make([]HistoryFilterOption, 0, len(years))
	for _, year := range years {
		options = append(options, HistoryFilterOption{Value: year, Label: year})
	}
	return options
}

func historyEffectiveYear(date string) string {
	date = strings.TrimSpace(date)
	if len(date) < 4 {
		return ""
	}
	for _, character := range date[:4] {
		if character < '0' || character > '9' {
			return ""
		}
	}
	return date[:4]
}

func normalizeHistorySort(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case historySortPerson, historySortChange, historySortClosed, historySortOutcome:
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func normalizeHistoryDirection(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "asc", "desc":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func effectiveHistorySort(value string) string {
	if normalized := normalizeHistorySort(value); normalized != "" {
		return normalized
	}
	return historySortClosed
}

func effectiveHistoryDirection(value string) string {
	if normalized := normalizeHistoryDirection(value); normalized != "" {
		return normalized
	}
	return "desc"
}

func sortHistory(items []WorkItem, sortKey, direction string) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := historySortValue(items[i], sortKey), historySortValue(items[j], sortKey)
		if left == right {
			left, right = items[i].ID, items[j].ID
		}
		if direction == "desc" {
			return left > right
		}
		return left < right
	})
}

func historySortValue(item WorkItem, sortKey string) string {
	switch sortKey {
	case historySortPerson:
		return strings.ToLower(item.Person)
	case historySortChange:
		return strings.ToLower(item.Summary)
	case historySortOutcome:
		return strings.ToLower(item.Status)
	default:
		for _, layout := range []string{"2 Jan 2006 · 15:04 MST", time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
			if stamp, err := time.Parse(layout, strings.TrimSpace(item.CompletedAt)); err == nil {
				return stamp.UTC().Format(time.RFC3339Nano)
			}
		}
		return strings.ToLower(strings.TrimSpace(item.CompletedAt))
	}
}

func historyOutcomeLabel(locale LocaleContext, outcome string) string {
	switch strings.ToLower(strings.TrimSpace(outcome)) {
	case "completed":
		return locale.Text("history.completed")
	case "rejected":
		return locale.Text("history.rejected")
	case "failed":
		return locale.Text("history.failed")
	case "recorded":
		return locale.Text("journey.stage_recorded")
	default:
		return outcome
	}
}

func historySortColumns(view View, target PageID, personID, sortKey, direction string) []HistorySortColumnProps {
	definitions := []struct {
		key, label string
		sortable   bool
	}{
		{historySortPerson, view.Locale.Text("history.column_employee"), true}, {historySortChange, view.Locale.Text("history.column_change"), true},
		{historySortClosed, view.Locale.Text("history.column_closed"), true}, {historySortOutcome, view.Locale.Text("history.column_outcome"), true},
	}
	if isIndividualHistoryTarget(target) {
		definitions[0].label = view.Locale.Text("history.column_workflow")
		definitions[0].sortable = false
	}
	columns := make([]HistorySortColumnProps, 0, len(definitions))
	for _, definition := range definitions {
		nextDirection := "asc"
		if sortKey == definition.key && direction == "asc" {
			nextDirection = "desc"
		}
		columns = append(columns, HistorySortColumnProps{
			Key: definition.key, Label: definition.label,
			Href:   historyHref(view, target, personID, view.HistoryQuery, view.HistoryOutcome, view.HistoryPerson, view.HistoryYear, definition.key, nextDirection),
			Active: sortKey == definition.key, Descending: sortKey == definition.key && direction == "desc",
			Sortable: definition.sortable, Navigate: view.Navigate,
		})
	}
	return columns
}

func historyHref(view View, target PageID, personID, query, outcome, selectedPerson, year, sortKey, direction string) string {
	if isIndividualHistoryTarget(target) {
		selectedPerson = ""
	}
	values := []string{
		"history_q", query, "outcome", outcome, "history_person", selectedPerson, "history_year", year,
		"history_sort", sortKey, "history_dir", direction,
		"history_page_size", pageSizeValue(view.HistoryPageSize),
	}
	if target == PagePerson {
		values = append(values,
			"person", personID, "q", view.Query, "team", view.PeopleTeam, "location", view.PeopleLocation,
			"sort", view.PeopleSort, "dir", view.PeopleDirection, "page", peoplePageValue(view.PeoplePage), "page_size", pageSizeValue(view.PeoplePageSize), "workflow_q", view.WorkflowQuery,
		)
	} else if target == PageMyself {
		values = append(values, "workflow_q", view.WorkflowQuery)
	}
	return statefulHref(view, target, values...)
}
