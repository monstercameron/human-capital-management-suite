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
	DocumentFolder     string
	DocumentSort       string
	DocumentOwner      string
	DocumentPage       int
	DocumentPerPage    int
	DocumentSearchMode string
	DocumentEditing    bool
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
	OrganizationAsOf   string
	OrganizationUnit   string
	WorkflowQuery      string
	HistoryQuery       string
	HistoryOutcome     string
	HistoryPerson      string
	HistoryRequester   string
	HistoryYear        string
	HistorySort        string
	HistoryDirection   string
	HistoryPage        int
	HistoryPageSize    int
	Mode               string
	SelectedWork       string
	AgentTaskID        string
	SelectedPerson     string
	PositionReference  string
	WorkFilter         string
	JourneyID          string
	JourneyWorker      string
	JourneyMode        string
	JourneyList        JourneyListFilter
	WorkflowID         string
	WorkflowRunID      string
	WorkflowDraftID    string
	WorkflowNodeID     string
	// WorkflowShowReferences preserves the designer's reference catalog toggle.
	WorkflowShowReferences bool
	NavCollapsed           bool
	MenuQuery              string
	FavoritePages          []PageID
}

// ApplyRequest applies address-bar presentation state to a live projection.
// It never adds records; those must already have come from an authorized
// server answer.
func ApplyRequest(view View, request PageRequest) View {
	view = ApplyLocale(view, ResolveProductLocale(request.Locale))
	view.Query = strings.TrimSpace(request.Query)
	positionReference := strings.TrimSpace(request.PositionReference)
	if positionReference != view.PositionReference {
		view.PositionObject = nil
		view.PositionOccupancy = nil
	}
	view.PositionReference = positionReference
	if view.Page != PagePositionObject {
		view.PositionObject = nil
	}
	if view.Page != PagePositionOccupancy {
		view.PositionOccupancy = nil
	}
	if view.Page != PagePositionObject && view.Page != PagePositionOccupancy {
		view.PositionOptions = nil
	}
	view.DocumentID = strings.TrimSpace(request.DocumentID)
	view.DocumentQuery = strings.TrimSpace(request.DocumentQuery)
	view.DocumentCollection = strings.TrimSpace(request.DocumentCollection)
	view.DocumentPageToken = strings.TrimSpace(request.DocumentPageToken)
	view.DocumentFolder = strings.TrimSpace(request.DocumentFolder)
	view.DocumentSort = strings.TrimSpace(request.DocumentSort)
	view.DocumentOwner = strings.TrimSpace(request.DocumentOwner)
	view.DocumentPage = max(request.DocumentPage, 1)
	view.DocumentPerPage = NormalizeDocumentPerPage(request.DocumentPerPage)
	view.DocumentSearchMode = strings.TrimSpace(request.DocumentSearchMode)
	view.DocumentEditing = request.DocumentEditing && strings.TrimSpace(request.DocumentID) != ""
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
	view.OrganizationAsOf = strings.TrimSpace(request.OrganizationAsOf)
	view.SelectedOrganizationUnit = strings.TrimSpace(request.OrganizationUnit)
	routeProfile, _, hasProfile := PageProfiles(view.Page)
	stateProfile := RouteStateProfile{}
	if hasProfile {
		stateProfile = routeProfile.StateProfile()
	}
	if routeProfile == RouteProfileAgents && view.AgentsProjection != nil {
		// The server projection is already filtered to this owner. Clear any
		// previously selected detail before resolving the address selector, and
		// admit only an exact task from that authorized snapshot. A copied task
		// keeps the request-local view from aliasing the projection's task list.
		projection := *view.AgentsProjection
		snapshot := projection.Snapshot
		snapshot.SelectedTask = nil
		if projection.Enabled && snapshot.Availability == AgentsAvailable {
			requested := strings.TrimSpace(request.AgentTaskID)
			if requested != "" {
				for index := range snapshot.Tasks {
					if strings.TrimSpace(snapshot.Tasks[index].ID) == requested {
						selected := cloneAgentTask(snapshot.Tasks[index])
						snapshot.SelectedTask = &selected
						break
					}
				}
			}
		}
		projection.Snapshot = snapshot
		view.AgentsProjection = &projection
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
	view.HistoryRequester = strings.TrimSpace(request.HistoryRequester)
	view.HistoryYear = strings.TrimSpace(request.HistoryYear)
	view.HistorySort = normalizeHistorySort(request.HistorySort)
	if view.Page == PageWorkflowHistory {
		view.HistorySort = normalizeWorkflowHistorySort(request.HistorySort)
	}
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
	view.WorkflowShowReferences = request.WorkflowShowReferences
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

func normalizeWorkflowHistorySort(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "started", "updated", "status":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return normalizeHistorySort(value)
	}
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
	for index := range people {
		person := &people[index]
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
	for index := range people {
		person := &people[index]
		if person.WorkerID != "" && person.WorkerID == workerID {
			return person.ID
		}
	}
	return ref
}

func personIDPresent(people []Person, id string) bool {
	for index := range people {
		person := &people[index]
		if person.ID != "" && person.ID == id {
			return true
		}
	}
	return false
}

func cloneAgentTask(task AgentTask) AgentTask {
	clone := task
	clone.Steps = append([]AgentTaskStep(nil), task.Steps...)
	clone.Checkpoints = append([]AgentCheckpoint(nil), task.Checkpoints...)
	clone.Artifacts = append([]AgentArtifact(nil), task.Artifacts...)
	clone.Approvals = append([]AgentApproval(nil), task.Approvals...)
	for index, approval := range task.Approvals {
		clone.Approvals[index] = approval
		clone.Approvals[index].Sources = append([]string(nil), approval.Sources...)
	}
	clone.SubmittedIntents = append([]AgentIntentStatus(nil), task.SubmittedIntents...)
	return clone
}

func filterWork(items []WorkItem, filter string) []WorkItem {
	return FilterWorkCollection(items, ParseWorkCollectionFilter(filter))
}
