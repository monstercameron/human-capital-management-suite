package productui

import "strings"

// PageRequest is transport-neutral navigation state. A production provider can
// resolve it through gRPC-backed projections without changing any component.
type PageRequest struct {
	Page               PageID
	Locale             string
	Query              string
	RolePage           int
	PeoplePage         int
	PeoplePageSize     int
	PeopleTeam         string
	PeopleLocation     string
	PeopleEligibleOnly bool
	PeopleSort         string
	PeopleDirection    string
	OrganizationView   string
	WorkflowQuery      string
	HistoryQuery       string
	HistoryOutcome     string
	HistoryPerson      string
	HistoryYear        string
	HistorySort        string
	HistoryDirection   string
	HistoryPage        int
	HistoryPageSize    int
	Mode               string
	SelectedWork       string
	SelectedPerson     string
	WorkFilter         string
	JourneyID          string
	JourneyWorker      string
	JourneyMode        string
	NavCollapsed       bool
	MenuQuery          string
	FavoritePages      []PageID
}

// ApplyRequest applies address-bar presentation state to a live projection.
// It never adds records; those must already have come from an authorized
// server answer.
func ApplyRequest(view View, request PageRequest) View {
	view = ApplyLocale(view, ResolveProductLocale(request.Locale))
	view.Query = strings.TrimSpace(request.Query)
	view.RolePage = request.RolePage
	if view.RolePage < 1 {
		view.RolePage = 1
	}
	view.PeoplePage = request.PeoplePage
	if view.PeoplePage < 1 {
		view.PeoplePage = 1
	}
	view.PeoplePageSize = normalizePageSize(request.PeoplePageSize)
	view.PeopleTeam = strings.TrimSpace(request.PeopleTeam)
	view.PeopleLocation = strings.TrimSpace(request.PeopleLocation)
	view.PeopleEligibleOnly = request.PeopleEligibleOnly
	view.PeopleSort = normalizePeopleSort(request.PeopleSort)
	view.PeopleDirection = normalizePeopleDirection(request.PeopleDirection)
	view.OrganizationView = normalizeOrganizationView(request.OrganizationView)
	if view.Page == PageOrgOutline {
		// The outline route has one intentionally fixed semantic presentation.
		// Keep shell, search, and navigation links aligned with what it renders
		// even when the incoming address omitted (or contradicted) org_view.
		view.OrganizationView = organizationViewTree
	}
	view.WorkflowQuery = strings.TrimSpace(request.WorkflowQuery)
	view.HistoryQuery = strings.TrimSpace(request.HistoryQuery)
	view.HistoryOutcome = strings.ToLower(strings.TrimSpace(request.HistoryOutcome))
	view.HistoryPerson = strings.TrimSpace(request.HistoryPerson)
	view.HistoryYear = strings.TrimSpace(request.HistoryYear)
	view.HistorySort = normalizeHistorySort(request.HistorySort)
	view.HistoryDirection = normalizeHistoryDirection(request.HistoryDirection)
	view.HistoryPage = request.HistoryPage
	if view.HistoryPage < 1 {
		view.HistoryPage = 1
	}
	view.HistoryPageSize = normalizePageSize(request.HistoryPageSize)
	view.Mode = strings.TrimSpace(request.Mode)
	view.JourneyID = strings.TrimSpace(request.JourneyID)
	view.JourneyWorker = strings.TrimSpace(request.JourneyWorker)
	view.JourneyMode = strings.TrimSpace(request.JourneyMode)
	view.NavCollapsed = request.NavCollapsed
	view.MenuQuery = strings.TrimSpace(request.MenuQuery)
	view.FavoritePages = authorizedFavoritePages(view.Navigation, request.FavoritePages)
	if selected := strings.TrimSpace(request.SelectedWork); selected != "" {
		view.SelectedWork = selected
	}
	if person := strings.TrimSpace(request.SelectedPerson); person != "" {
		view.SelectedPerson = stablePersonID(view.People, person)
		if isOrganizationRoute(view.Page) && !personIDPresent(view.People, view.SelectedPerson) {
			view.SelectedPerson = ""
		}
	}
	if filter := strings.TrimSpace(request.WorkFilter); filter != "" {
		view.WorkFilter = filter
		view.Work = filterWork(view.Work, filter)
	}
	if view.Page == PagePeople || view.Page == PagePerson {
		view.PeoplePage = paginatePeople(filteredPeople(view), view.PeoplePage, view.PeoplePageSize).Page
	}
	if view.Page == PageRoles {
		view.RolePage = roleDirectoryWindowPage(view.People, view.Query, view.RolePage)
	}
	switch view.Page {
	case PageHistory:
		view.HistoryPage = paginateHistory(filteredHistory(view, ""), view.HistoryPage, view.HistoryPageSize).Page
	case PagePerson:
		view.HistoryPage = paginateHistory(filteredHistory(view, view.SelectedPerson), view.HistoryPage, view.HistoryPageSize).Page
	case PageMyself:
		personID := ""
		if person, ok := viewerPerson(view); ok {
			personID = person.ID
		}
		view.HistoryPage = paginateHistory(filteredHistory(view, personID), view.HistoryPage, view.HistoryPageSize).Page
	}
	return view
}

func isOrganizationRoute(page PageID) bool {
	switch page {
	case PageOrganization, PageOrgExplorer, PageOrgOutline, PageOrgResponsive:
		return true
	default:
		return false
	}
}

func authorizedFavoritePages(navigation []NavItem, requested []PageID) []PageID {
	allowed := make(map[PageID]bool)
	var collect func([]NavItem)
	collect = func(items []NavItem) {
		for _, item := range items {
			allowed[item.Page] = true
			collect(item.Children)
		}
	}
	collect(navigation)
	result := make([]PageID, 0, len(requested))
	seen := make(map[PageID]bool)
	for _, page := range requested {
		if allowed[page] && !seen[page] {
			result = append(result, page)
			seen[page] = true
		}
	}
	return result
}

// stablePersonID accepts either the workforce row's public reference or the
// canonical entity reference a durable journey records. It normalizes both
// to the public reference used by profile navigation and workflow launchers.
func stablePersonID(people []Person, ref string) string {
	for _, person := range people {
		if person.ID == ref {
			return person.ID
		}
	}
	workerID := ref
	if strings.HasPrefix(ref, "eref:") {
		if split := strings.LastIndexByte(ref, ':'); split >= 0 {
			workerID = ref[split+1:]
		}
	}
	for _, person := range people {
		if person.WorkerID != "" && person.WorkerID == workerID {
			return person.ID
		}
	}
	return ref
}

func personIDPresent(people []Person, id string) bool {
	for _, person := range people {
		if person.ID != "" && person.ID == id {
			return true
		}
	}
	return false
}

func filterWork(items []WorkItem, filter string) []WorkItem {
	return FilterWorkCollection(items, ParseWorkCollectionFilter(filter))
}
