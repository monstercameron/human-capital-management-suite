// Package productclient projects canonical JourneyService responses onto the
// reusable product UI components. It owns no business facts and has no
// transport runtime dependency: the WASM composition supplies two RPC
// closures backed by the generated gRPC client.
package productclient

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/uicomponents"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Service is the read-only portion of JourneyService needed by the product
// shell. Writes stay on the dedicated journey workflow surface.
type Service struct {
	ListJourneys      func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error)
	ListWorkers       func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error)
	GetPreferences    func(context.Context, *journeyv1.GetProductPreferencesRequest) (*journeyv1.GetProductPreferencesResponse, error)
	GetWorkerIDPolicy func(context.Context, *journeyv1.GetWorkerIDPolicyRequest) (*journeyv1.GetWorkerIDPolicyResponse, error)
	GetRoleAccess     func(context.Context, *journeyv1.GetRoleAccessRequest) (*journeyv1.GetRoleAccessResponse, error)
}

// Session contains display facts from the server-admitted configuration
// island. The server authorizes every RPC independently; these values never
// grant authority.
type Session struct {
	Tenant    string
	Principal string
	// Scope is the shell's persistent, page-independent "authorized scope"
	// label (rendered on every product route). UXAUDIT-007: it must be a
	// real authorized-scope fact or empty, never a task-specific purpose --
	// tools/uxqual/cmd/journeywasm/product_wasm.go previously set this from
	// the admitted principal's data-processing Purpose (e.g.
	// "compensation_review"), which persisted a task-specific badge on
	// every unrelated page. A purpose belongs inside the workflow it
	// actually governs, not here.
	Scope                 string
	Roles                 []string
	Permissions           []productui.RolePagePermission
	EnforceRoleVisibility bool
	LogoutHref            string
}

// State is presentation-only address-bar state.
type State struct {
	Page     productui.PageID
	Request  productui.PageRequest
	Provided map[string]bool
}

// LoadingView builds the non-authoritative shell shown while Load is waiting
// for the cell. It shares the resolved view's humanized session labels and
// address state without manufacturing any business records or counts.
func LoadingView(session Session, state State) productui.View {
	view := productui.NewView(state.Page, displayLabel(session.Tenant), displayLabel(session.Principal), displayLabel(session.Scope))
	view.LogoutHref = session.LogoutHref
	if session.EnforceRoleVisibility {
		view = productui.ApplyRoleVisibility(view, session.Roles)
	}
	if len(session.Permissions) > 0 {
		view = productui.ApplyPagePermissions(view, session.Permissions)
	}
	return productui.ApplyRequest(view, state.Request)
}

// ContentLoadingView retargets a previously authorized shell to a destination
// route without discarding its resolved chrome. Only address-derived
// presentation state is changed; the destination's business data remains a
// loading proxy until Load returns the newly authorized projection.
func ContentLoadingView(previous productui.View, state State) productui.View {
	previous.Page = state.Page
	previous.LoadError = ""
	previous.Loading = false
	previous.ContentLoading = true
	previous.Refreshing = false
	previous.RefreshingRegion = ""
	return productui.ApplyRequest(previous, state.Request)
}

// ParseState resolves a production product route and its presentation query.
func ParseState(pathname, rawQuery string) (State, error) {
	definition, ok := productui.LookupRoute(pathname)
	if !ok {
		return State{}, fmt.Errorf("productclient: unknown product route %q", pathname)
	}
	// Route state is presentation-only, but it still crosses an untrusted
	// address-bar boundary. Keep recognized state strict: an ambiguous
	// repeated key must not silently win by map iteration/order. Unknown keys
	// are ignored below for forward compatibility and are never copied into a
	// PageRequest, so a copied link cannot smuggle an action/credential
	// parameter into the product router.
	if len(rawQuery) > maxRouteQueryBytes {
		return State{}, errors.New("productclient: route state is too large")
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return State{}, errors.New("productclient: route state is malformed")
	}
	allowed := routeQueryKeys(definition.ID)
	provided := make(map[string]bool, len(values))
	for key, entries := range values {
		// Unknown and wrong-page keys are intentionally omitted. The loader
		// replaces the address with CanonicalHref before it issues a read, so
		// credentials, action names, and stale selectors cannot survive in
		// browser history or become a future route feature by accident.
		if !allowed[key] {
			values.Del(key)
			continue
		}
		if len(entries) != 1 || !safeRouteValue(entries[0]) {
			return State{}, errors.New("productclient: route state is malformed")
		}
		if key == "nav" && entries[0] != "" && entries[0] != "collapsed" && entries[0] != "expanded" {
			return State{}, errors.New("productclient: route state is malformed")
		}
		provided[key] = true
	}
	peoplePage, err := parsePositiveRouteInt(values, "page", 1)
	if err != nil {
		return State{}, err
	}
	peoplePageSize, err := parsePageSizeRouteInt(values, "page_size")
	if err != nil {
		return State{}, err
	}
	historyPage, err := parsePositiveRouteInt(values, "history_page", 1)
	if err != nil {
		return State{}, err
	}
	historyPageSize, err := parsePageSizeRouteInt(values, "history_page_size")
	if err != nil {
		return State{}, err
	}
	if !validControlledRouteValues(definition.ID, values) {
		return State{}, errors.New("productclient: route state is malformed")
	}
	state := State{Page: definition.ID, Provided: provided, Request: productui.PageRequest{
		Page: definition.ID, Locale: routeValue(values, "locale"), Query: routeValue(values, "q"), Mode: routeValue(values, "mode"),
		SelectedWork: routeValue(values, "selected"), SelectedPerson: routeValue(values, "person"),
		PeoplePage: peoplePage, PeoplePageSize: peoplePageSize, PeopleTeam: routeValue(values, "team"), PeopleLocation: routeValue(values, "location"), PeopleEligibleOnly: routeValue(values, "eligible") == "1", PeopleSort: routeValue(values, "sort"), PeopleDirection: routeValue(values, "dir"),
		OrganizationView: routeValue(values, "org_view"),
		WorkflowQuery:    routeValue(values, "workflow_q"), HistoryQuery: routeValue(values, "history_q"), HistoryOutcome: routeValue(values, "outcome"),
		HistoryPerson: routeValue(values, "history_person"), HistoryYear: routeValue(values, "history_year"), HistorySort: routeValue(values, "history_sort"), HistoryDirection: routeValue(values, "history_dir"), HistoryPage: historyPage, HistoryPageSize: historyPageSize,
		WorkFilter: routeValue(values, "filter"), NavCollapsed: routeValue(values, "nav") == "collapsed",
		JourneyID: routeValue(values, "journey"), JourneyWorker: routeValue(values, "worker"), JourneyMode: routeValue(values, "mode"),
		MenuQuery: routeValue(values, "menu_q"), FavoritePages: parseFavoritePages(routeValue(values, "favorites")),
	}}
	if state.Page == productui.PageJourneys {
		if state.Request.JourneyID != "" {
			state.Request.Mode = ""
			state.Request.JourneyMode = ""
			state.Request.JourneyWorker = ""
			delete(state.Provided, "mode")
			delete(state.Provided, "worker")
		} else if state.Request.JourneyMode != "" && state.Request.JourneyMode != "new" {
			return State{}, errors.New("productclient: route state is malformed")
		}
	}
	return state, nil
}

const maxRouteQueryBytes = 4096

var shellRouteQueryKeys = []string{"locale", "nav", "menu_q", "favorites"}

var pageRouteQueryKeys = map[productui.PageID][]string{
	productui.PageMyself:       {"workflow_q", "history_q", "outcome", "history_person", "history_year", "history_sort", "history_dir", "history_page", "history_page_size"},
	productui.PageJourneys:     {"journey", "mode", "worker"},
	productui.PageWork:         {"filter", "selected"},
	productui.PageHistory:      {"history_q", "outcome", "history_person", "history_year", "history_sort", "history_dir", "history_page", "history_page_size"},
	productui.PagePeople:       {"q", "page", "page_size", "team", "location", "eligible", "sort", "dir"},
	productui.PagePerson:       {"person", "q", "page", "page_size", "team", "location", "eligible", "sort", "dir", "workflow_q", "history_q", "outcome", "history_person", "history_year", "history_sort", "history_dir", "history_page", "history_page_size"},
	productui.PageOrganization: {"org_view"},
	productui.PageStudio:       {"mode"},
	productui.PageRoles:        {"q"},
}

func safeRouteValue(value string) bool {
	return len(value) <= maxRouteQueryBytes && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}

func routeQueryKeys(page productui.PageID) map[string]bool {
	keys := make(map[string]bool, len(shellRouteQueryKeys)+len(pageRouteQueryKeys[page]))
	for _, key := range shellRouteQueryKeys {
		keys[key] = true
	}
	for _, key := range pageRouteQueryKeys[page] {
		keys[key] = true
	}
	return keys
}

func parsePositiveRouteInt(values url.Values, key string, absent int) (int, error) {
	raw, ok := values[key]
	if !ok {
		return absent, nil
	}
	value, err := strconv.Atoi(raw[0])
	if err != nil || value < 1 {
		return 0, errors.New("productclient: route state is malformed")
	}
	return value, nil
}

func parsePageSizeRouteInt(values url.Values, key string) (int, error) {
	value, err := parsePositiveRouteInt(values, key, 0)
	if err != nil || value != 0 && value != 10 && value != 20 && value != 50 && value != 100 {
		return 0, errors.New("productclient: route state is malformed")
	}
	return value, nil
}

func validControlledRouteValues(page productui.PageID, values url.Values) bool {
	oneOf := func(key string, allowed ...string) bool {
		value := strings.ToLower(strings.TrimSpace(values.Get(key)))
		return value == "" || slices.Contains(allowed, value)
	}
	if !oneOf("sort", "name", "role", "team", "manager", "location") ||
		!oneOf("dir", "asc", "desc") ||
		!oneOf("eligible", "1") ||
		!oneOf("history_sort", "person", "change", "closed", "outcome") ||
		!oneOf("history_dir", "asc", "desc") ||
		!oneOf("outcome", "completed", "rejected", "failed") {
		return false
	}
	switch page {
	case productui.PageWork:
		return oneOf("filter", "review", "blocked", "complete", "mine")
	case productui.PageOrganization:
		return oneOf("org_view", "flat", "tree")
	case productui.PageStudio:
		return oneOf("mode", "preview", "validate")
	default:
		return true
	}
}

func routeValue(values url.Values, key string) string {
	return strings.TrimSpace(values.Get(key))
}

// CanonicalHref returns the only browser address representation of parsed
// product state. It serializes page-scoped presentation values and nothing
// else; it can never carry a credential, action token, or service response.
func CanonicalHref(state State) string {
	values := url.Values{}
	set := func(key, value string) {
		if state.Provided[key] {
			values.Set(key, value)
		}
	}
	request := state.Request
	set("locale", request.Locale)
	if state.Provided["nav"] {
		if request.NavCollapsed {
			values.Set("nav", "collapsed")
		} else {
			values.Set("nav", "expanded")
		}
	}
	set("menu_q", request.MenuQuery)
	if state.Provided["favorites"] {
		favorites := make([]string, 0, len(request.FavoritePages))
		for _, page := range request.FavoritePages {
			favorites = append(favorites, string(page))
		}
		values.Set("favorites", strings.Join(favorites, ","))
	}
	switch state.Page {
	case productui.PageMyself:
		setHistoryRouteValues(values, state)
		set("workflow_q", request.WorkflowQuery)
	case productui.PageJourneys:
		set("journey", request.JourneyID)
		set("mode", request.JourneyMode)
		set("worker", request.JourneyWorker)
	case productui.PageWork:
		set("filter", request.WorkFilter)
		set("selected", request.SelectedWork)
	case productui.PageHistory:
		setHistoryRouteValues(values, state)
	case productui.PagePeople:
		setPeopleRouteValues(values, state)
	case productui.PagePerson:
		set("person", request.SelectedPerson)
		setPeopleRouteValues(values, state)
		set("workflow_q", request.WorkflowQuery)
		setHistoryRouteValues(values, state)
	case productui.PageOrganization:
		set("org_view", request.OrganizationView)
	case productui.PageRoles:
		set("q", request.Query)
	case productui.PageStudio:
		set("mode", request.Mode)
	}
	href := productui.Path(state.Page)
	if query := values.Encode(); query != "" {
		return href + "?" + query
	}
	return href
}

// ResolvedCanonicalHref updates address-backed pagination with the effective
// window selected from the freshly authorized projection. Parsing can
// normalize syntax before a read, but only the resolved record set can clamp
// an out-of-range page without disclosing or guessing a count.
func ResolvedCanonicalHref(state State, view productui.View) string {
	if view.Page != state.Page {
		return CanonicalHref(state)
	}
	resolved := state
	resolved.Request = state.Request
	if state.Page == productui.PagePeople || state.Page == productui.PagePerson {
		if state.Provided["page"] {
			resolved.Request.PeoplePage = view.PeoplePage
		}
		if state.Provided["page_size"] {
			resolved.Request.PeoplePageSize = view.PeoplePageSize
		}
	}
	if state.Page == productui.PageHistory || state.Page == productui.PagePerson || state.Page == productui.PageMyself {
		if state.Provided["history_page"] {
			resolved.Request.HistoryPage = view.HistoryPage
		}
		if state.Provided["history_page_size"] {
			resolved.Request.HistoryPageSize = view.HistoryPageSize
		}
	}
	return CanonicalHref(resolved)
}

func setPeopleRouteValues(values url.Values, state State) {
	request := state.Request
	setProvidedRouteValue(values, state, "q", request.Query)
	setProvidedRouteInt(values, state, "page", request.PeoplePage)
	setProvidedRouteInt(values, state, "page_size", request.PeoplePageSize)
	setProvidedRouteValue(values, state, "team", request.PeopleTeam)
	setProvidedRouteValue(values, state, "location", request.PeopleLocation)
	eligibleValue := ""
	if request.PeopleEligibleOnly {
		eligibleValue = "1"
	}
	setProvidedRouteValue(values, state, "eligible", eligibleValue)
	setProvidedRouteValue(values, state, "sort", request.PeopleSort)
	setProvidedRouteValue(values, state, "dir", request.PeopleDirection)
}

func setHistoryRouteValues(values url.Values, state State) {
	request := state.Request
	setProvidedRouteValue(values, state, "history_q", request.HistoryQuery)
	setProvidedRouteValue(values, state, "outcome", request.HistoryOutcome)
	setProvidedRouteValue(values, state, "history_person", request.HistoryPerson)
	setProvidedRouteValue(values, state, "history_year", request.HistoryYear)
	setProvidedRouteValue(values, state, "history_sort", request.HistorySort)
	setProvidedRouteValue(values, state, "history_dir", request.HistoryDirection)
	setProvidedRouteInt(values, state, "history_page", request.HistoryPage)
	setProvidedRouteInt(values, state, "history_page_size", request.HistoryPageSize)
}

func setProvidedRouteValue(values url.Values, state State, key, value string) {
	if state.Provided[key] {
		values.Set(key, value)
	}
}

func setProvidedRouteInt(values url.Values, state State, key string, value int) {
	if state.Provided[key] {
		values.Set(key, strconv.Itoa(value))
	}
}

func parseFavoritePages(raw string) []productui.PageID {
	parts := strings.Split(raw, ",")
	pages := make([]productui.PageID, 0, len(parts))
	for _, part := range parts {
		if page := productui.PageID(strings.TrimSpace(part)); page != "" {
			if _, ok := productui.LookupPage(page); !ok || slices.Contains(pages, page) {
				continue
			}
			pages = append(pages, page)
		}
	}
	return pages
}

// Load calls the live service and returns an authorized component view. A cold
// load resolves the complete shell projection so global search, identity, and
// notifications are ready regardless of the initial route.
func Load(ctx context.Context, service Service, session Session, state State) (productui.View, error) {
	return load(ctx, service, session, state, nil)
}

// LoadWithBaseline reuses already-authorized shell data and refreshes only the
// datasets the destination page consumes. Preferences remain live on every
// route; page data is never reused across sessions because the caller supplies
// the baseline from the same admitted browser composition.
func LoadWithBaseline(ctx context.Context, service Service, session Session, state State, baseline productui.View) (productui.View, error) {
	return load(ctx, service, session, state, &baseline)
}

type pageDataRequirements struct {
	journeys bool
	workers  bool
}

func requirementsForPage(page productui.PageID) pageDataRequirements {
	switch page {
	case productui.PageHome, productui.PageMyself, productui.PageWork, productui.PageHistory,
		productui.PagePerson, productui.PageInsights:
		return pageDataRequirements{journeys: true, workers: true}
	case productui.PagePeople:
		// PROMOUX-001: the directory's per-worker promotion-availability
		// verdict needs to know about a nonterminal journey already in
		// flight for that worker (PromotionActiveConflict), so People also
		// reads journeys now -- not just workers.
		return pageDataRequirements{journeys: true, workers: true}
	case productui.PageOrganization, productui.PageRoles, productui.PageOrganizationVisibility:
		return pageDataRequirements{workers: true}
	default:
		return pageDataRequirements{}
	}
}

func load(ctx context.Context, service Service, session Session, state State, baseline *productui.View) (productui.View, error) {
	view := LoadingView(session, state)
	requirements := pageDataRequirements{journeys: true, workers: true}
	if baseline != nil && baselineMatchesSession(*baseline, view) {
		requirements = requirementsForPage(state.Page)
		seedBaselineProjection(&view, *baseline)
		// Work filters are applied destructively to the route projection. Never
		// mistake a filtered previous page for the complete shell dataset.
		if baseline.Page == productui.PageWork && baseline.WorkFilter != "" {
			requirements.journeys = true
		}
	}
	var failures []error
	var journeysResponse *journeyv1.ListJourneysResponse
	var workersResponse *journeyv1.ListWorkersResponse
	var preferencesResponse *journeyv1.GetProductPreferencesResponse
	var workerIDResponse *journeyv1.GetWorkerIDPolicyResponse
	var roleAccessResponse *journeyv1.GetRoleAccessResponse
	var journeysErr, workersErr, preferencesErr error
	var workerIDErr error
	var roleAccessErr error
	var reads sync.WaitGroup
	if !requirements.journeys {
		// The authorized baseline already supplies shell counts/search records.
	} else if session.EnforceRoleVisibility && !productui.PageVisible(productui.PageJourneys, session.Roles) {
		journeysResponse = &journeyv1.ListJourneysResponse{}
	} else if service.ListJourneys == nil {
		journeysErr = errors.New("JourneyService.ListJourneys is not connected")
	} else {
		reads.Add(1)
		go func() {
			defer reads.Done()
			journeysResponse, journeysErr = service.ListJourneys(ctx, &journeyv1.ListJourneysRequest{})
			if journeysErr != nil {
				journeysErr = fmt.Errorf("list journeys: %w", journeysErr)
			}
		}()
	}
	if !requirements.workers {
		// The destination does not consume workforce records; keep the baseline.
	} else if service.ListWorkers == nil {
		workersErr = errors.New("JourneyService.ListWorkers is not connected")
	} else {
		reads.Add(1)
		go func() {
			defer reads.Done()
			workersResponse, workersErr = service.ListWorkers(ctx, &journeyv1.ListWorkersRequest{})
			if workersErr != nil {
				workersErr = fmt.Errorf("list workers: %w", workersErr)
			}
		}()
	}
	if service.GetPreferences != nil {
		reads.Add(1)
		go func() {
			defer reads.Done()
			preferencesResponse, preferencesErr = service.GetPreferences(ctx, &journeyv1.GetProductPreferencesRequest{})
			if preferencesErr != nil {
				preferencesErr = fmt.Errorf("load product preferences: %w", preferencesErr)
			}
		}()
	}
	if state.Page == productui.PageWorkerIDs {
		if service.GetWorkerIDPolicy == nil {
			workerIDErr = errors.New("JourneyService.GetWorkerIDPolicy is not connected")
		} else {
			reads.Add(1)
			go func() {
				defer reads.Done()
				workerIDResponse, workerIDErr = service.GetWorkerIDPolicy(ctx, &journeyv1.GetWorkerIDPolicyRequest{})
				if workerIDErr != nil {
					workerIDErr = fmt.Errorf("load worker ID policy: %w", workerIDErr)
				}
			}()
		}
	}
	if state.Page == productui.PageRoles || state.Page == productui.PageOrganizationVisibility {
		if service.GetRoleAccess == nil {
			roleAccessErr = errors.New("JourneyService.GetRoleAccess is not connected")
		} else {
			reads.Add(1)
			go func() {
				defer reads.Done()
				roleAccessResponse, roleAccessErr = service.GetRoleAccess(ctx, &journeyv1.GetRoleAccessRequest{})
				if roleAccessErr != nil {
					roleAccessErr = fmt.Errorf("load role access: %w", roleAccessErr)
				}
			}()
		}
	}
	reads.Wait()
	if roleAccessErr != nil {
		failures = append(failures, roleAccessErr)
	}
	if roleAccessResponse != nil {
		applyRoleAccess(&view, roleAccessResponse)
	}
	if preferencesErr != nil {
		failures = append(failures, preferencesErr)
	}
	if preferencesResponse != nil {
		applyPreferences(&view, &state, preferencesResponse)
	}
	if workerIDErr != nil {
		failures = append(failures, workerIDErr)
	}
	if workerIDResponse != nil {
		view.WorkerIDPolicy = projectWorkerIDPolicy(workerIDResponse.GetPolicy(), workerIDResponse.GetPreviews())
	}

	if journeysErr != nil {
		failures = append(failures, journeysErr)
		// A required authorization/read refresh fails closed. Retaining an
		// older projection here could outlive a server-side access revocation.
		view.Work = nil
	} else if requirements.journeys {
		var projectionErr error
		view.Work, projectionErr = projectJourneys(journeysResponse.GetJourneys())
		if projectionErr != nil {
			failures = append(failures, projectionErr)
		}
	}
	if workersErr != nil {
		failures = append(failures, workersErr)
		view.People = nil
	} else if requirements.workers {
		var projectionErr error
		view.People, projectionErr = projectWorkers(workersResponse.GetWorkers())
		if options := workersResponse.GetOptions(); options != nil {
			// PROMOUX-001: the server-owned four-state verdict. authorized
			// is the same create-authority gate the page adapters already
			// use to hide every workflow from a denied viewer; asking it
			// first is what keeps an unauthorized read from leaking which
			// workers would otherwise have been eligible, ineligible or
			// conflicted (every one of them collapses to PromotionWithheld
			// alike). activeConflicts comes from the journeys this same
			// load just answered with -- requirementsForPage now asks for
			// them on PagePeople specifically so this is never stale.
			authorized := len(view.EffectivePermissions) == 0 || view.Can(productui.PageJourneys, "create")
			activeConflicts := activePromotionConflicts(view.Work)
			availability := make(map[string]productui.PromotionAvailabilityCode, len(workersResponse.GetWorkers()))
			for _, worker := range workersResponse.GetWorkers() {
				if worker == nil {
					continue
				}
				hasPath := journeyclient.HasPromotionChoices(options, worker)
				availability[worker.GetWorkerRef()] = productui.ResolvePromotionAvailability(authorized, hasPath, activeConflicts[worker.GetWorkerRef()])
			}
			for index := range view.People {
				view.People[index].PromotionAvailability = availability[view.People[index].ID]
			}
		}
		if projectionErr != nil {
			failures = append(failures, projectionErr)
		}
	}
	view.Viewer = projectViewerProfile(session, view.People)
	photosByWorker := make(map[string]string, len(view.People))
	for _, person := range view.People {
		photosByWorker[person.ID] = person.PhotoURL
	}
	for index := range view.Work {
		if photo := photosByWorker[view.Work[index].PersonRef]; photo != "" {
			view.Work[index].PhotoURL = photo
		}
	}
	for index := range view.Navigation {
		if view.Navigation[index].Page == productui.PageWork {
			view.Navigation[index].Count = len(productui.OpenWorkItems(view.Work))
		}
	}
	view = productui.ApplyRequest(view, state.Request)
	for index := range view.Work {
		view.Work[index].Href = productui.JourneyDetailHref(view, view.Work[index].ID)
	}
	view.PersonWorkflows = projectPersonWorkflows(view, view.SelectedPerson)
	return view, errors.Join(failures...)
}

func baselineMatchesSession(baseline, current productui.View) bool {
	return baseline.Tenant == current.Tenant && baseline.Principal == current.Principal && baseline.Scope == current.Scope &&
		slices.Equal(baseline.Roles, current.Roles) && slices.Equal(baseline.EffectivePermissions, current.EffectivePermissions)
}

func seedBaselineProjection(view *productui.View, baseline productui.View) {
	view.Work = baseline.Work
	view.People = baseline.People
	view.Viewer = baseline.Viewer
	view.WorkflowUses = baseline.WorkflowUses
	view.PreferenceVersion = baseline.PreferenceVersion
	view.AppearanceVersion = baseline.AppearanceVersion
	view.Appearance = baseline.Appearance
	view.Accessibility = baseline.Accessibility
	view.OrganizationVisibility = baseline.OrganizationVisibility
	view.StoredPreferences = baseline.StoredPreferences
	view.NavigationGroupOpen = baseline.NavigationGroupOpen
	view.Source = baseline.Source
}

func applyRoleAccess(view *productui.View, response *journeyv1.GetRoleAccessResponse) {
	if view == nil || response == nil {
		return
	}
	view.AccessRoles = make([]productui.AccessRole, 0, len(response.GetRoles()))
	for _, role := range response.GetRoles() {
		view.AccessRoles = append(view.AccessRoles, productui.AccessRole{Version: role.GetVersion(), ID: role.GetRoleId(), Name: role.GetName(), Description: role.GetDescription(), System: role.GetSystem(), Active: role.GetActive()})
	}
	view.RoleAssignments = make([]productui.WorkerRoleAssignment, 0, len(response.GetAssignments()))
	for _, assignment := range response.GetAssignments() {
		view.RoleAssignments = append(view.RoleAssignments, productui.WorkerRoleAssignment{Version: assignment.GetVersion(), WorkerRef: assignment.GetWorkerRef(), RoleIDs: append([]string(nil), assignment.GetRoleIds()...)})
	}
	view.RoleVisibilityPolicies = make([]productui.OrganizationVisibilityPolicy, 0, len(response.GetVisibilityPolicies()))
	for _, policy := range response.GetVisibilityPolicies() {
		view.RoleVisibilityPolicies = append(view.RoleVisibilityPolicies, productui.OrganizationVisibilityPolicy{Version: policy.GetVersion(), RoleID: policy.GetRoleId(), Mode: policy.GetMode(), OrganizationUnits: append([]string(nil), policy.GetOrganizationUnits()...)})
	}
	view.RolePagePermissions = make([]productui.RolePagePermission, 0, len(response.GetPagePermissions()))
	for _, permission := range response.GetPagePermissions() {
		view.RolePagePermissions = append(view.RolePagePermissions, productui.RolePagePermission{
			Version: permission.GetVersion(), RoleID: permission.GetRoleId(), Page: productui.PageID(permission.GetPageId()),
			View: permission.GetCanView(), Create: permission.GetCanCreate(), Update: permission.GetCanUpdate(), Delete: permission.GetCanDelete(),
		})
	}
}

func projectWorkerIDPolicy(p *journeyv1.WorkerIDPolicy, previews []string) productui.WorkerIDPolicy {
	if p == nil {
		return productui.WorkerIDPolicy{}
	}
	return productui.WorkerIDPolicy{Version: p.GetVersion(), Prefix: p.GetPrefix(), Suffix: p.GetSuffix(), Separator: p.GetSeparator(), SequenceDigits: int(p.GetSequenceDigits()), StartAt: p.GetStartAt(), NextSequence: p.GetNextSequence(), IncrementBy: p.GetIncrementBy(), ZeroPad: p.GetZeroPad(), YearFormat: p.GetYearFormat(), IncludeUnitCode: p.GetIncludeUnitCode(), CheckDigit: p.GetCheckDigit(), ExcludedRanges: p.GetExcludedRanges(), IssuedCount: p.GetIssuedCount(), Previews: append([]string(nil), previews...)}
}

// projectViewerProfile joins the admitted account identity to an authorized
// worker projection without turning a display fact into authorization. Real
// and local-development deployments both match the authenticated subject to
// a stable worker reference or ID returned by the authorized workforce read.
func projectViewerProfile(session Session, people []productui.Person) productui.ViewerProfile {
	name := displayLabel(session.Principal)
	fallback := productui.ViewerProfile{Name: name, Initials: uicomponents.Initials(name)}
	principal := normalizedIdentity(session.Principal)
	for _, person := range people {
		// Only stable worker identifiers may bind the account to a self-service
		// profile. A display name is mutable and non-unique, so it can never be
		// used as an identity join.
		if principal != "" && (normalizedIdentity(person.ID) == principal || normalizedIdentity(person.WorkerID) == principal) {
			return viewerProfileFromPerson(person)
		}
	}
	return fallback
}

func viewerProfileFromPerson(person productui.Person) productui.ViewerProfile {
	return productui.ViewerProfile{PersonID: person.ID, Name: person.Name, Initials: person.Initials, PhotoURL: person.PhotoURL, Role: person.Role}
}

func normalizedIdentity(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func applyPreferences(view *productui.View, state *State, response *journeyv1.GetProductPreferencesResponse) {
	if view == nil || state == nil || response == nil {
		return
	}
	user := response.GetUser()
	if user != nil {
		view.PreferenceVersion = user.GetVersion()
		view.WorkflowUses = user.GetWorkflowUses()
		if access := user.GetAccessibility(); access != nil {
			view.Accessibility = productui.NormalizeAccessibilityPreferences(productui.AccessibilityPreferences{TextSize: access.GetTextSize(), Contrast: access.GetContrast(), Motion: access.GetMotion(), Links: access.GetLinks()})
		}
		view.NavigationGroupOpen = make(map[productui.PageID]bool, len(user.GetNavigationGroups()))
		for key, open := range user.GetNavigationGroups() {
			view.NavigationGroupOpen[productui.PageID(key)] = open
		}
		stored := productui.StoredUserPreferences{Version: user.GetVersion(), Locale: user.GetLocale(), Accessibility: view.Accessibility, NavCollapsed: user.GetNavCollapsed(), NavigationGroups: view.NavigationGroupOpen, WorkflowUses: user.GetWorkflowUses(), Tables: map[string]productui.StoredTablePreferences{}}
		for _, page := range user.GetFavoritePages() {
			stored.FavoritePages = append(stored.FavoritePages, productui.PageID(page))
		}
		for key, table := range user.GetTables() {
			if table != nil {
				stored.Tables[key] = productui.StoredTablePreferences{PageSize: int(table.GetPageSize()), Filters: table.GetFilters(), Sort: table.GetSort(), Direction: table.GetDirection()}
			}
		}
		view.StoredPreferences = stored
		request := &state.Request
		if !state.Provided["locale"] && user.GetLocale() != "" {
			request.Locale = user.GetLocale()
		}
		if !state.Provided["nav"] {
			request.NavCollapsed = user.GetNavCollapsed()
		}
		if !state.Provided["favorites"] {
			request.FavoritePages = parseFavoritePages(strings.Join(user.GetFavoritePages(), ","))
		}
		applyTableDefaults(request, state.Provided, user.GetTables()["people"], false)
		applyTableDefaults(request, state.Provided, user.GetTables()["history"], true)
		applyWorkTableDefaults(request, state.Provided, user.GetTables()["work"])
	}
	if theme := response.GetTheme(); theme != nil {
		view.AppearanceVersion = theme.GetVersion()
		view.Appearance = productui.NormalizeCustomerTheme(productui.CustomerTheme{BrandName: theme.GetBrandName(), BrandMark: theme.GetBrandMark(), BrandLogoURL: theme.GetBrandLogoUrl(), ColorMode: theme.GetColorMode(), Palette: theme.GetPalette(), Shape: theme.GetShape(), Density: theme.GetDensity(), Glyphs: theme.GetGlyphs(), Typeface: theme.GetTypeface(), Navigation: theme.GetNavigation(), Motion: theme.GetMotion()})
	}
	if policy := response.GetOrganizationVisibility(); policy != nil {
		view.OrganizationVisibility = productui.OrganizationVisibilityPolicy{Version: policy.GetVersion(), Mode: policy.GetMode(), OrganizationUnits: append([]string(nil), policy.GetOrganizationUnits()...)}
	}
}

func applyTableDefaults(request *productui.PageRequest, provided map[string]bool, table *journeyv1.TablePreferences, history bool) {
	if request == nil || table == nil {
		return
	}
	if history {
		if !provided["history_page_size"] {
			request.HistoryPageSize = int(table.GetPageSize())
		}
		if !provided["history_q"] {
			request.HistoryQuery = table.GetFilters()["query"]
		}
		if !provided["outcome"] {
			request.HistoryOutcome = table.GetFilters()["outcome"]
		}
		if !provided["history_person"] {
			request.HistoryPerson = table.GetFilters()["person"]
		}
		if !provided["history_year"] {
			request.HistoryYear = table.GetFilters()["year"]
		}
		if !provided["history_sort"] {
			request.HistorySort = table.GetSort()
		}
		// A newly selected sort column has an implicit ascending direction.
		// Do not splice a stale saved direction onto that explicit column.
		if !provided["history_dir"] && !provided["history_sort"] {
			request.HistoryDirection = table.GetDirection()
		}
		return
	}
	if !provided["page_size"] {
		request.PeoplePageSize = int(table.GetPageSize())
	}
	if !provided["q"] {
		request.Query = table.GetFilters()["query"]
	}
	if !provided["team"] {
		request.PeopleTeam = table.GetFilters()["team"]
	}
	if !provided["location"] {
		request.PeopleLocation = table.GetFilters()["location"]
	}
	if !provided["sort"] {
		request.PeopleSort = table.GetSort()
	}
	// sort=<column> without dir is the canonical ascending address. Loading a
	// previously saved direction here made a first click appear descending.
	if !provided["dir"] && !provided["sort"] {
		request.PeopleDirection = table.GetDirection()
	}
}

// applyWorkTableDefaults is applyTableDefaults' My Work sibling (UXAUDIT-017,
// GREEN's "retain filters on return"). It reuses the exact mechanism
// UXAUDIT-008 built for the People and Workflow History tables --
// preferences.TablePreferences, projected here from the generic
// GetProductPreferencesResponse.tables map -- rather than inventing a
// second retention path: My Work's tab filter is a one-field table
// preference the same way History's filters are a multi-field one. Only the
// stored default is applied, and only when the address bar did not already
// say so, so an explicit link (e.g. a shared "?filter=blocked" URL) always
// wins over whatever the viewer saved last.
func applyWorkTableDefaults(request *productui.PageRequest, provided map[string]bool, table *journeyv1.TablePreferences) {
	if request == nil || table == nil {
		return
	}
	if !provided["filter"] {
		request.WorkFilter = table.GetFilters()["filter"]
	}
}

// activePromotionConflicts reports, per worker reference, whether a
// nonterminal promotion journey already exists for them. Every journey this
// service returns is a promotion journey (projectJourneys hard-codes the
// title), so PersonRef plus !Terminal is a complete answer with no need to
// filter by workflow kind.
func activePromotionConflicts(work []productui.WorkItem) map[string]bool {
	conflicts := make(map[string]bool, len(work))
	for _, item := range work {
		if !item.Terminal && item.PersonRef != "" {
			conflicts[item.PersonRef] = true
		}
	}
	return conflicts
}

func projectJourneys(journeys []*journeyv1.Journey) ([]productui.WorkItem, error) {
	items := make([]productui.WorkItem, 0, len(journeys))
	var failures []error
	for _, journey := range journeys {
		if journey == nil {
			continue
		}
		status, tone, terminal := stagePresentation(journey.GetStage())
		current, target := journey.GetCurrent(), journey.GetTarget()
		currentJob, targetJob := "", ""
		if current != nil {
			currentJob = concisePlacementLabel(current.GetJobCode(), current.GetGrade())
		}
		if target != nil {
			targetJob = concisePlacementLabel(target.GetJobCode(), target.GetGrade())
		}
		summary := strings.TrimSpace(currentJob + " → " + targetJob)
		currentBase, err := moneyFromWire(journey.GetCurrentBase(), journey.GetCurrency())
		if err != nil {
			failures = append(failures, fmt.Errorf("project journey %s current base: %w", journey.GetIntentId(), err))
		}
		proposedBase, err := moneyFromWire(journey.GetProposedBase(), journey.GetCurrency())
		if err != nil {
			failures = append(failures, fmt.Errorf("project journey %s proposed base: %w", journey.GetIntentId(), err))
		}
		// UXAUDIT-017: the same stage dimension the Journeys tracker renders,
		// so My Work's next step and waiting-on class never disagree with it.
		dimension := journeyclient.StageStatusDimension(journey.GetStage())
		items = append(items, productui.WorkItem{
			NextStep: string(dimension.NextStep), WaitingOn: string(dimension.WaitingOn), AwaitsPerson: dimension.AwaitsPerson,
			ID: journey.GetIntentId(), Initials: uicomponents.Initials(journey.GetWorkerName()), PhotoURL: employeePhotoURL(journey.GetWorkerRef(), journey.GetWorkerName()), Title: "Promotion journey",
			Person: journey.GetWorkerName(), PersonRef: journey.GetWorkerRef(), Summary: summary, Status: status, Tone: tone, Terminal: terminal,
			Due: journey.GetEffectiveDate(), EffectiveDate: journey.GetEffectiveDate(), CompletedAt: timestampLabel(journey.GetUpdatedAt()),
			InstanceID: journey.GetInstanceId(), InstanceVersion: journey.GetInstanceVersion(), MaterialDigest: journey.GetMaterialDigest(),
			CurrentBase: currentBase, ProposedBase: proposedBase,
		})
		applyWorkItemSummary(&items[len(items)-1], journey.GetCurrentWorkItem())
	}
	return items, errors.Join(failures...)
}

// applyWorkItemSummary copies the server's viewer-scoped current work item
// summary (UXAUDIT-017) onto a My Work item. The server has already applied
// the work item visibility rules; an absent summary leaves every field empty
// so the page keeps its stage-derived fallback and honest note, and nothing
// here fills a gap with a guess.
func applyWorkItemSummary(item *productui.WorkItem, summary *journeyv1.JourneyWorkItemSummary) {
	if item == nil || summary == nil {
		return
	}
	item.WorkSummary = true
	item.ViewerMembership = summary.GetViewerMembership()
	item.PermittedActions = append([]string(nil), summary.GetViewerPermittedActions()...)
	if ref := strings.TrimSpace(summary.GetAssigneePrincipalId()); ref != "" {
		item.AssigneeRef = ref
		item.AssigneeName = strings.TrimSpace(summary.GetAssigneeDisplayName())
		if item.AssigneeName == "" {
			item.AssigneeName = displayLabel(ref)
		}
	}
	if due := summary.GetDueAt(); due != nil && due.CheckValid() == nil && !due.AsTime().IsZero() {
		item.WorkDue = due.AsTime().UTC().Format("2006-01-02")
	}
}

func timestampLabel(stamp *timestamppb.Timestamp) string {
	if stamp == nil || stamp.CheckValid() != nil {
		return ""
	}
	return stamp.AsTime().UTC().Format("2 Jan 2006 · 15:04 UTC")
}

func projectWorkers(workers []*journeyv1.Worker) ([]productui.Person, error) {
	people := make([]productui.Person, 0, len(workers))
	var failures []error
	namesByRef := make(map[string]string, len(workers)*2)
	// workerIDByRef resolves EITHER form of a manager reference this wire
	// format can carry (the public WorkerRef or the raw WorkerId) to the raw
	// WorkerId, so productui's UXAUDIT-004 relationship resolver -- which
	// needs the canonical worker identity, not the display-oriented public
	// reference -- can look up a worker's manager by id rather than by name.
	workerIDByRef := make(map[string]string, len(workers)*2)
	for _, worker := range workers {
		if worker == nil {
			continue
		}
		name := workerDisplayName(worker)
		namesByRef[worker.GetWorkerRef()] = name
		namesByRef[worker.GetWorkerId()] = name
		workerIDByRef[worker.GetWorkerRef()] = worker.GetWorkerId()
		workerIDByRef[worker.GetWorkerId()] = worker.GetWorkerId()
	}
	for _, worker := range workers {
		if worker == nil {
			continue
		}
		name := workerDisplayName(worker)
		title := strings.TrimSpace(worker.GetJobTitle())
		if title == "" {
			// A job code is more truthful and legible than title-casing an acronym
			// when the authoritative service has not published a job title.
			title = strings.TrimSpace(worker.GetJobCode())
		}
		role := strings.TrimSpace(title + " · " + worker.GetGrade())
		photoURL := strings.TrimSpace(worker.GetProfilePhotoUrl())
		if photoURL == "" {
			photoURL = employeePhotoURL(worker.GetWorkerRef(), name)
		}
		basePay, err := moneyFromWire(worker.GetBasePay(), worker.GetCurrency())
		if err != nil {
			failures = append(failures, fmt.Errorf("project worker %s base pay: %w", worker.GetWorkerRef(), err))
		}
		people = append(people, productui.Person{
			ID: worker.GetWorkerRef(), WorkerID: worker.GetWorkerId(), Initials: uicomponents.Initials(name), PhotoURL: photoURL, Name: name,
			LegalName: worker.GetLegalName(), PreferredName: worker.GetPreferredName(), Role: role,
			Team: orgUnitLabel(worker.GetOrgUnit()), Manager: managerLabel(worker.GetManagerRef(), namesByRef), ManagerID: workerIDByRef[worker.GetManagerRef()], Location: worker.GetLocation(), WorkerNumber: worker.GetWorkerNumber(),
			JobCode: worker.GetJobCode(), Grade: worker.GetGrade(), PositionID: worker.GetPositionId(),
			PayZone: worker.GetPayZone(), BasePay: basePay,
			BonusTarget: worker.GetBonusTarget(), HireDate: worker.GetHireDate(), Source: worker.GetSource(),
			CreatedAt: timestampLabel(worker.GetCreatedAt()),
		})
	}
	productui.IndexPeople(people)
	return people, errors.Join(failures...)
}

func workerDisplayName(worker *journeyv1.Worker) string {
	if worker == nil {
		return ""
	}
	name := strings.TrimSpace(worker.GetPreferredName())
	if name == "" {
		name = strings.TrimSpace(worker.GetLegalName())
	}
	return name
}

func managerLabel(ref string, namesByRef map[string]string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if name := namesByRef[ref]; name != "" {
		return name
	}
	lowerRef := strings.ToLower(ref)
	if strings.HasPrefix(lowerRef, "board:") {
		return "HarborCare Board"
	}
	if strings.HasPrefix(lowerRef, "rel:") || strings.HasPrefix(lowerRef, "rel-") || strings.HasPrefix(lowerRef, "rel_") || strings.HasPrefix(lowerRef, "eref:") {
		return "Not available"
	}
	return displayLabel(ref)
}

func concisePlacementLabel(jobCode, grade string) string {
	jobCode = strings.TrimSpace(jobCode)
	for prefixLength := 1; prefixLength <= len(jobCode)/2; prefixLength++ {
		if len(jobCode)%prefixLength != 0 {
			continue
		}
		prefix := jobCode[:prefixLength]
		if strings.Repeat(prefix, len(jobCode)/prefixLength) == jobCode {
			jobCode = prefix
			break
		}
	}
	return strings.TrimSpace(jobCode + " " + strings.TrimSpace(grade))
}

// employeePhotoURL binds the stable demo employees exposed by the seeded cell
// to same-origin product assets. Unknown workers retain the shared initials
// fallback instead of receiving a misleading stock portrait.
func employeePhotoURL(workerRef, name string) string {
	identity := strings.ToLower(strings.TrimSpace(workerRef + " " + name))
	for _, employee := range []struct {
		key, asset string
	}{
		{"priya", "person-priya-small.jpg"},
		{"jane", "person-jane-small.jpg"},
		{"omar", "person-omar-small.jpg"},
		{"lena", "person-lena-small.jpg"},
		{"noor", "person-noor-small.jpg"},
	} {
		if strings.Contains(identity, employee.key) {
			return "/workspace/assets/" + employee.asset
		}
	}
	return ""
}

// moneyFromWire is the only product-UI boundary that accepts the journey
// service's decimal text plus currency pair. It immediately binds them into
// the shared exact Money value object; neither the component model nor any
// renderer can subsequently perform binary floating-point money arithmetic.
func moneyFromWire(amount, currency string) (values.Money, error) {
	amount = strings.TrimSpace(amount)
	currency = strings.TrimSpace(currency)
	if amount == "" {
		return values.Money{}, nil
	}
	scale := int32(0)
	if dot := strings.IndexByte(amount, '.'); dot >= 0 {
		fractionalDigits := len(amount) - dot - 1
		if fractionalDigits > int(values.MaxScale) {
			return values.Money{}, fmt.Errorf("decimal scale %d exceeds %d", fractionalDigits, values.MaxScale)
		}
		scale = int32(fractionalDigits)
	}
	return values.NewMoney(amount, currency, scale, values.RoundingExactRequired)
}

func projectPersonWorkflows(view productui.View, workerRef string) []productui.PersonWorkflow {
	workerRef = strings.TrimSpace(workerRef)
	href := ""
	if workerRef != "" {
		href = productui.JourneyProposalHref(view, workerRef)
	}
	return []productui.PersonWorkflow{{
		ID: "promotion", Name: "Promotion", Category: "Career & compensation",
		Description: "Propose a governed job, grade, position, and compensation change.",
		Href:        href, UseCount: view.WorkflowUses["promotion"],
		LaunchHref: func(person string) string { return productui.JourneyProposalHref(view, person) },
	}}
}

func stagePresentation(stage journeyv1.JourneyStage) (status, tone string, terminal bool) {
	switch stage {
	case journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED:
		return "Ready to start approval", "neutral", false
	case journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED:
		return "Blocked", "warning", false
	case journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL:
		return "Awaiting approval", "warning", false
	case journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED:
		return "Completed", "success", true
	case journeyv1.JourneyStage_JOURNEY_STAGE_RECORDED:
		return "Recorded", "success", true
	case journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL:
		return "Finance approval", "warning", false
	case journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL:
		return "Manager approval", "warning", false
	case journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE:
		return "Waiting for effective date", "neutral", false
	case journeyv1.JourneyStage_JOURNEY_STAGE_REVALIDATION:
		return "Final checks", "neutral", false
	case journeyv1.JourneyStage_JOURNEY_STAGE_REAPPROVAL:
		return "Approval required again", "warning", false
	case journeyv1.JourneyStage_JOURNEY_STAGE_EXECUTED:
		return "Recording promotion", "neutral", false
	case journeyv1.JourneyStage_JOURNEY_STAGE_OBSERVING_EFFECTS:
		return "Checking downstream effects", "neutral", false
	case journeyv1.JourneyStage_JOURNEY_STAGE_REPAIR_REQUIRED:
		return "Needs repair", "danger", false
	case journeyv1.JourneyStage_JOURNEY_STAGE_REJECTED:
		return "Rejected", "neutral", true
	case journeyv1.JourneyStage_JOURNEY_STAGE_FAILED:
		return "Failed", "danger", true
	default:
		return "Status unavailable", "warning", false
	}
}

func displayLabel(value string) string {
	return productui.DisplayLabel(value)
}

func orgUnitLabel(code string) string {
	if label, ok := map[string]string{
		"care-operations":      "Care Operations",
		"data-analytics":       "Data & Analytics",
		"eng-platform":         "Engineering Platform",
		"engineering-platform": "Engineering Platform",
		"growth-customer":      "Growth & Customer",
		"legal-compliance":     "Legal & Compliance",
		"people-ops":           "People Operations",
		"people-operations":    "People Operations",
		"product-technology":   "Product & Technology",
		"quality-safety":       "Quality & Safety",
		"security-it":          "Security & IT",
	}[strings.ToLower(strings.TrimSpace(code))]; ok {
		return label
	}
	return displayLabel(code)
}
