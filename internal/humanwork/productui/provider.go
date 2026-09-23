package productui

import "strings"

// PageRequest is transport-neutral navigation state. A production provider can
// resolve it through gRPC-backed projections without changing any component.
type PageRequest struct {
	Page               PageID
	Locale             string
	Query              string
	DocumentID         string
	DocumentQuery      string
	DocumentCollection string
	DocumentPageToken  string
	RolePage           int
	PeoplePage         int
	PeoplePageSize     int
	PeopleTeam         string
	PeopleLocation     string
	PeopleEligibleOnly bool
	PeopleSort         string
	PeopleColumns      string
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
	JourneyList        JourneyListFilter
	WorkflowID         string
	WorkflowRunID      string
	WorkflowDraftID    string
	WorkflowNodeID     string
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
	view.DocumentID = strings.TrimSpace(request.DocumentID)
	view.DocumentQuery = strings.TrimSpace(request.DocumentQuery)
	view.DocumentCollection = strings.TrimSpace(request.DocumentCollection)
	view.DocumentPageToken = strings.TrimSpace(request.DocumentPageToken)
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
	view.PeopleColumns = NormalizePeopleColumns(request.PeopleColumns)
	view.PeopleDirection = normalizePeopleDirection(request.PeopleDirection)
	view.OrganizationView = normalizeOrganizationView(request.OrganizationView)
	routeProfile, _, hasProfile := PageProfiles(view.Page)
	stateProfile := RouteStateProfile{}
	if hasProfile {
		stateProfile = routeProfile.StateProfile()
	}
	if stateProfile.OrganizationOutline {
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
	view.JourneyList = NormalizeJourneyListFilter(request.JourneyList)
	view.SelectedWorkflowID = strings.TrimSpace(request.WorkflowID)
	view.SelectedWorkflowRunID = strings.TrimSpace(request.WorkflowRunID)
	view.SelectedWorkflowDraftID = strings.TrimSpace(request.WorkflowDraftID)
	view.SelectedWorkflowNodeID = strings.TrimSpace(request.WorkflowNodeID)
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
	// The work tab narrows only My Work's own list; every other page keeps
	// the full authorized population (UXLIVE-027).
	if filter := strings.TrimSpace(request.WorkFilter); filter != "" && stateProfile.Work {
		view.WorkFilter = filter
		view.Work = filterWork(view.Work, filter)
	}
	if stateProfile.PeopleDirectory {
		view.PeoplePage = paginatePeople(filteredPeople(view), view.PeoplePage, view.PeoplePageSize).Page
	}
	if stateProfile.Roles {
		view.RolePage = roleDirectoryWindowPage(view.People, view.Query, view.RolePage)
	}
	if stateProfile.History && !stateProfile.HistorySelectedPerson && !stateProfile.HistoryViewer {
		view.HistoryPage = paginateHistory(filteredHistory(view, ""), view.HistoryPage, view.HistoryPageSize).Page
	} else if stateProfile.HistorySelectedPerson {
		view.HistoryPage = paginateHistory(filteredHistory(view, view.SelectedPerson), view.HistoryPage, view.HistoryPageSize).Page
	} else if stateProfile.HistoryViewer {
		personID := ""
		if person, ok := viewerPerson(view); ok {
			personID = person.ID
		}
		view.HistoryPage = paginateHistory(filteredHistory(view, personID), view.HistoryPage, view.HistoryPageSize).Page
	}
	return view
}

func isOrganizationRoute(page PageID) bool {
	routeProfile, _, ok := PageProfiles(page)
	return ok && routeProfile.StateProfile().Organization
}

func authorizedFavoritePages(navigation []NavItem, requested []PageID) []PageID {
	// Every link on a page asks for the shell's address state, and most
	// viewers have no favourites at all; walking the navigation tree for an
	// empty request was pure cost on every statefulHref call.
	if len(requested) == 0 {
		return nil
	}
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
