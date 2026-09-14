package productui

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// peoplePage is the route adapter. It is the only People component that sees
// the broad page projection; every child receives a purpose-built props value.
func peoplePage(view View) ui.Node {
	// The directory population is the admitted population: rows, facet
	// options, and counts all derive from it, so denied workers appear
	// nowhere once the server speaks.
	population := admittedPeople(view)
	scoped := view
	scoped.People = population
	filtered := filteredPeople(scoped)
	ordered := sortedPeople(filtered, view.PeopleSort, view.PeopleDirection)
	window := paginatePeople(ordered, view.PeoplePage, view.PeoplePageSize)
	filterActive := view.Query != "" || view.PeopleTeam != "" || view.PeopleLocation != "" || view.PeopleEligibleOnly
	props := PeoplePageProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Summary: PeopleSummaryProps{
			CountLabel: peopleCountLabel(view.Locale, filterActive, len(filtered), len(population)),
			ScopeLabel: view.Locale.Text("people.scope"),
		},
		Filter: PeopleFilterProps{
			Query: view.Query, Team: view.PeopleTeam, Location: view.PeopleLocation, EligibleOnly: view.PeopleEligibleOnly,
			Teams:     peopleFilterOptions(peopleFacetOptions(population, func(person Person) string { return person.Team })),
			Locations: peopleFilterOptions(peopleFacetOptions(population, func(person Person) string { return person.Location })),
			Sort:      view.PeopleSort, Direction: view.PeopleDirection,
			PageSize: view.PeoplePageSize,
			Action:   pageHref(PagePeople), ClearHref: peopleClearHref(view),
			NavCollapsed: view.NavCollapsed, Navigate: view.Navigate,
		},
		Empty: PeopleEmptyStateProps{
			ClearHref: peopleClearHref(view), Navigate: view.Navigate,
		},
	}
	if view.Navigate != nil {
		props.Filter.OnFilter = func(query, team, location string, eligibleOnly bool) {
			view.Navigate(peopleDirectoryHref(view, 1, strings.TrimSpace(query), strings.TrimSpace(team), strings.TrimSpace(location), eligibleOnly, view.PeopleSort, view.PeopleDirection))
		}
	}
	if window.Total > 0 {
		props.Directory = peopleDirectoryProps(view, window)
	}
	return ui.CreateElement(PeoplePage, props)
}

func peopleDirectoryProps(view View, window peoplePageWindow) *PeopleDirectoryProps {
	props := &PeopleDirectoryProps{
		I18nProps:  I18nProps{Locale: view.Locale},
		Rows:       peopleRowProps(view, window),
		Columns:    peopleSortColumns(view),
		Pagination: peoplePaginationProps(view, window),
		Refreshing: view.RefreshingRegion == RefreshRegionPeopleDirectory,
	}
	props.InputKey = peopleDirectoryInputKey(*props)
	if view.UpdatePeopleDirectory == nil {
		return props
	}
	props.CommitSort = view.UpdatePeopleDirectory
	props.ResolveSort = func(field string, descending bool) PeopleDirectoryProps {
		next := view
		next.PeoplePage = 1
		next.PeopleSort = field
		next.PeopleDirection = peopleSortAscending
		if descending {
			next.PeopleDirection = peopleSortDescending
		}
		filtered := filteredPeople(next)
		window := paginatePeople(sortedPeople(filtered, next.PeopleSort, next.PeopleDirection), next.PeoplePage, next.PeoplePageSize)
		return *peopleDirectoryProps(next, window)
	}
	return props
}

// peopleDirectoryInputKey identifies a fresh parent projection without tying
// component state to slice addresses. A local sort keeps its original input
// key; filters, paging, locale changes, or new worker data produce a new key
// and reset the directory from the incoming props.
func peopleDirectoryInputKey(props PeopleDirectoryProps) string {
	hash := fnv.New64a()
	write := func(values ...string) {
		for _, value := range values {
			_, _ = fmt.Fprintf(hash, "%d:%s|", len(value), value)
		}
	}
	write(props.Locale.Resolved, strconv.Itoa(props.Pagination.Page), strconv.Itoa(props.Pagination.PageCount), strconv.Itoa(props.Pagination.PageSize.Value), strconv.Itoa(props.Pagination.First), strconv.Itoa(props.Pagination.Last), strconv.Itoa(props.Pagination.Total))
	for _, column := range props.Columns {
		write(column.ID, column.Label, column.Href, strconv.FormatBool(column.Active), strconv.FormatBool(column.Descending))
	}
	for _, row := range props.Rows {
		write(row.ID, row.Name, row.WorkerNumber, row.Role, row.Team, row.Manager, row.Location, row.PhotoURL, row.Href, row.WorkflowsUnavailableReason)
		for _, action := range row.QuickActions {
			write(action.Label, action.AccessibleLabel, action.Href, strconv.FormatBool(action.Frequent))
		}
	}
	return fmt.Sprintf("%x", hash.Sum64())
}

func peopleClearHref(view View) string {
	href := peopleDirectoryHref(view, 1, "", "", "", false, view.PeopleSort, view.PeopleDirection)
	return withExplicitEmptyQuery(href, "q", "team", "location", "eligible")
}

func peopleCountLabel(locale LocaleContext, filtered bool, filteredCount, totalCount int) string {
	if filtered {
		return locale.Text("people.filtered_count", map[string]string{"filtered": fmt.Sprint(filteredCount), "total": fmt.Sprint(totalCount)})
	}
	return locale.Plural("people.count", int64(totalCount))
}

func peopleFilterOptions(values []string) []PeopleFilterOption {
	options := make([]PeopleFilterOption, 0, len(values))
	for _, value := range values {
		options = append(options, PeopleFilterOption{Value: value, Label: value})
	}
	return options
}

func peopleSortColumns(view View) []PeopleSortColumnProps {
	active := normalizePeopleSort(view.PeopleSort)
	direction := normalizePeopleDirection(view.PeopleDirection)
	columns := []struct {
		field string
		label string
	}{
		{peopleSortName, view.Locale.Text("people.column.person")},
		{peopleSortRole, view.Locale.Text("people.column.role")},
		{peopleSortTeam, view.Locale.Text("people.column.team")},
		{peopleSortManager, view.Locale.Text("people.column.manager")},
		{peopleSortLocation, view.Locale.Text("people.column.location")},
	}
	result := make([]PeopleSortColumnProps, 0, len(columns))
	for _, column := range columns {
		nextDirection := peopleSortAscending
		if active == column.field && direction == peopleSortAscending {
			nextDirection = peopleSortDescending
		}
		result = append(result, PeopleSortColumnProps{
			ID: column.field, Label: column.label, Active: active == column.field, Descending: active == column.field && direction == peopleSortDescending,
			Href: peopleDirectoryHref(view, 1, view.Query, view.PeopleTeam, view.PeopleLocation, view.PeopleEligibleOnly, column.field, nextDirection), Navigate: view.Navigate,
		})
	}
	return result
}

func peopleRowProps(view View, window peoplePageWindow) []PeopleRowProps {
	rows := make([]PeopleRowProps, 0, len(window.People))
	workflows := rankedPersonWorkflows(view.PersonWorkflows, view.WorkflowUses)
	unauthorized := len(view.EffectivePermissions) > 0 && !view.Can(PageJourneys, "create")
	if unauthorized {
		workflows = nil
	}
	for _, person := range window.People {
		reason := ""
		if unauthorized {
			// The same reason PromotionAvailability would have resolved to
			// for this viewer had a per-worker verdict even been asked for:
			// every worker collapses to the identical, non-revealing
			// withheld text once no workflow at all is offered, rather than
			// the bare, unexplained fallback this gate used to leave behind.
			reason = PromotionAvailabilityReason(view.Locale, PromotionWithheld)
		}
		actions, workflowReason, _ := personWorkflowActions(view, person, workflows)
		if workflowReason != "" {
			reason = workflowReason
		}
		rows = append(rows, PeopleRowProps{
			ID: person.ID, Initials: person.Initials, PhotoURL: person.PhotoURL, Name: person.Name, WorkerNumber: person.WorkerNumber, Role: person.Role, Team: person.Team,
			Manager: person.Manager, Location: person.Location, Navigate: view.Navigate,
			Href: peoplePersonHref(view, person.ID, window.Page), QuickActions: actions,
			WorkflowsUnavailableReason: reason,
		})
	}
	return rows
}

// personWorkflowActions resolves the quick actions launchable for one
// person against an already permission-filtered, ranked workflow
// catalogue, plus the single disclosure-safe reason to show when a named
// workflow is not currently actionable for this specific person. It is
// the one place a person and the shared PersonWorkflow catalogue turn
// into launchable actions -- the People directory rows (above) and the
// shell action launcher (UXAUDIT-003, action_launcher.go) both call it
// so neither keeps its own, page-specific action inventory.
//
// reasonWorkflow names the catalogue workflow reason explains (empty when
// reason is empty), so a caller that lists actions per workflow does not
// have to re-derive which workflow the reason belongs to.
func personWorkflowActions(view View, person Person, workflows []PersonWorkflow) (actions []PeopleQuickActionProps, reason string, reasonWorkflow string) {
	actions = make([]PeopleQuickActionProps, 0, len(workflows))
	// PROMOUX-012: an open journey in view guards Start even when the
	// availability verdict disagrees, the same rule the profile applies.
	_, hasActiveJourney := activePromotionWorkItem(view, person.ID)
	for _, workflow := range workflows {
		if workflow.ID == "promotion" && (person.PromotionAvailability == PromotionActiveConflict || hasActiveJourney) {
			// PROMOUX-002 GREEN #3: a conflicting worker never loses the
			// action entirely -- Start is replaced with a link to the
			// journey already blocking a new one, so continuity survives
			// the refusal rather than dead-ending at a bare reason.
			if item, ok := activePromotionWorkItem(view, person.ID); ok {
				actions = append(actions, PeopleQuickActionProps{
					Label:           view.Locale.Text("people.open_active_promotion"),
					AccessibleLabel: view.Locale.Text("people.open_active_promotion_aria", map[string]string{"name": person.Name}),
					Href:            JourneyDetailHref(view, item.ID),
				})
				continue
			}
			reason, reasonWorkflow = PromotionAvailabilityReason(view.Locale, PromotionActiveConflict), workflow.Name
			continue
		}
		if workflow.ID == "promotion" && !personPromotionEligible(person) {
			// GREEN #2: a suppressed promotion action always leaves a
			// server-provided reason behind for the empty-workflow-menu
			// fallback, instead of the bare "no available workflows"
			// people_components.go used to render unconditionally.
			reason, reasonWorkflow = PromotionAvailabilityReason(view.Locale, person.PromotionAvailability), workflow.Name
			continue
		}
		href := workflow.Href
		if workflow.LaunchHref != nil {
			href = workflow.LaunchHref(person.ID)
		}
		if href == "" {
			continue
		}
		actions = append(actions, PeopleQuickActionProps{Label: workflow.Name,
			AccessibleLabel: view.Locale.Text("people.workflow_aria", map[string]string{"workflow": workflow.Name, "name": person.Name}),
			Href:            href, Frequent: workflow.UseCount > 0})
	}
	return actions, reason, reasonWorkflow
}

func peoplePaginationProps(view View, window peoplePageWindow) PeoplePaginationProps {
	return PeoplePaginationProps{
		AriaLabel: view.Locale.Text("people.pages"),
		First:     window.First, Last: window.Last, Total: window.Total, Page: window.Page, PageCount: window.PageCount,
		Previous: paginationLinkProps(view, view.Locale.Text("common.previous"), window.Page-1, window.Page <= 1),
		Next:     paginationLinkProps(view, view.Locale.Text("common.next"), window.Page+1, window.Page >= window.PageCount),
		PageSize: pageSizeControlProps(view, PagePeople, "page_size", view.PeoplePageSize, map[string]string{
			"q": view.Query, "team": view.PeopleTeam, "location": view.PeopleLocation, "eligible": eligibleQueryValue(view.PeopleEligibleOnly),
			"sort": view.PeopleSort, "dir": view.PeopleDirection,
		}),
	}
}

func paginationLinkProps(view View, label string, page int, disabled bool) PaginationLinkProps {
	return PaginationLinkProps{
		Label: label, Disabled: disabled, Navigate: view.Navigate,
		Href: peopleDirectoryHref(view, page, view.Query, view.PeopleTeam, view.PeopleLocation, view.PeopleEligibleOnly, view.PeopleSort, view.PeopleDirection),
	}
}

func peopleDirectoryHref(view View, page int, query, team, location string, eligibleOnly bool, sortField, direction string) string {
	sortField = normalizePeopleSort(sortField)
	direction = normalizePeopleDirection(direction)
	if sortField == peopleSortName {
		sortField = ""
	}
	if direction == peopleSortAscending {
		direction = ""
	}
	return statefulHref(view, PagePeople,
		"q", strings.TrimSpace(query), "team", strings.TrimSpace(team), "location", strings.TrimSpace(location),
		"eligible", eligibleQueryValue(eligibleOnly),
		"sort", sortField, "dir", direction, "page", peoplePageValue(page), "page_size", pageSizeValue(view.PeoplePageSize))
}

// eligibleQueryValue renders the boolean as the one non-empty token the
// route recognizes ("1"); statefulHref already drops empty pairs, so false
// simply omits the parameter.
func eligibleQueryValue(eligibleOnly bool) string {
	if eligibleOnly {
		return "1"
	}
	return ""
}

func peoplePersonHref(view View, personID string, page int) string {
	sortField := normalizePeopleSort(view.PeopleSort)
	direction := normalizePeopleDirection(view.PeopleDirection)
	if sortField == peopleSortName {
		sortField = ""
	}
	if direction == peopleSortAscending {
		direction = ""
	}
	return statefulHref(view, PagePerson,
		"person", personID, "q", view.Query, "team", view.PeopleTeam, "location", view.PeopleLocation,
		"eligible", eligibleQueryValue(view.PeopleEligibleOnly),
		"sort", sortField, "dir", direction, "page", peoplePageValue(page), "page_size", pageSizeValue(view.PeoplePageSize))
}

func peoplePageValue(page int) string {
	if page <= 1 {
		return ""
	}
	return strconv.Itoa(page)
}

func pageSizeValue(size int) string {
	if normalizePageSize(size) == defaultPageSize {
		return ""
	}
	return strconv.Itoa(normalizePageSize(size))
}

func pageSizeControlProps(view View, page PageID, param string, value int, fields map[string]string) PageSizeControlProps {
	if locale := view.Locale.normalized(); locale.Resolved != DefaultProductLocale {
		fields["locale"] = locale.Resolved
	}
	if view.NavCollapsed {
		fields["nav"] = "collapsed"
	}
	return PageSizeControlProps{
		Value: normalizePageSize(value), Name: param, Options: []int{10, 20, 50, 100}, Action: pageHref(page), Fields: fields,
		OnChange: func(size int) {
			if view.Navigate == nil {
				return
			}
			fields[param] = strconv.Itoa(size)
			pairs := make([]string, 0, len(fields)*2)
			for key, item := range fields {
				pairs = append(pairs, key, item)
			}
			view.Navigate(statefulHref(view, page, pairs...))
		},
	}
}

func rankedPersonWorkflows(workflows []PersonWorkflow, usage map[string]int64) []PersonWorkflow {
	result := append([]PersonWorkflow(nil), workflows...)
	for index := range result {
		if usage[result[index].ID] > result[index].UseCount {
			result[index].UseCount = usage[result[index].ID]
		}
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].UseCount == result[right].UseCount {
			return strings.ToLower(result[left].Name) < strings.ToLower(result[right].Name)
		}
		return result[left].UseCount > result[right].UseCount
	})
	return result
}
