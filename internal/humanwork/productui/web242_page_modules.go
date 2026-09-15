package productui

import (
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PageRenderer renders an already-authorized page projection. Modules hold a
// stateless implementation instead of a function value so their immutable
// registry can be initialized without a dependency cycle through page links.
type PageRenderer interface {
	Render(View) ui.Node
}

// PageAccessAudience is the declarative role audience for a page module.
type PageAccessAudience string

const (
	PageAudiencePublic         PageAccessAudience = "public"
	PageAudiencePortal         PageAccessAudience = "portal"
	PageAudienceWorker         PageAccessAudience = "worker"
	PageAudienceWorkerFinance  PageAccessAudience = "worker_finance"
	PageAudienceManager        PageAccessAudience = "manager"
	PageAudienceManagerFinance PageAccessAudience = "manager_finance"
	PageAudienceHRPartner      PageAccessAudience = "hr_partner"
	PageAudienceDenied         PageAccessAudience = "denied"
)

// PageAccessPolicy describes visibility without carrying credentials or
// runtime authorization state. HCM admin and comp admin remain the explicit
// compatibility bypasses in PageVisible.
type PageAccessPolicy struct {
	Audience PageAccessAudience
}

// RouteProfile identifies a reusable address/navigation state contract. It is
// metadata only; it carries no credentials, records, or authorization
// decisions. Consumers use the profile's semantic operations rather than
// growing page-ID switches.
type RouteProfile string

const (
	RouteProfileWorkspace    RouteProfile = "workspace"
	RouteProfileNested       RouteProfile = "nested"
	RouteProfileSupport      RouteProfile = "support"
	RouteProfileAdmin        RouteProfile = "admin"
	RouteProfilePublic       RouteProfile = "public"
	RouteProfileHome         RouteProfile = "home"
	RouteProfileInsights     RouteProfile = "insights"
	RouteProfileMyself       RouteProfile = "myself"
	RouteProfileJourneys     RouteProfile = "journeys"
	RouteProfileWork         RouteProfile = "work"
	RouteProfileHistory      RouteProfile = "history"
	RouteProfilePeople       RouteProfile = "people"
	RouteProfilePerson       RouteProfile = "person"
	RouteProfileOrganization RouteProfile = "organization"
	RouteProfileOrgOutline   RouteProfile = "organization_outline"
	RouteProfileRoles        RouteProfile = "roles"
	RouteProfileStudio       RouteProfile = "studio"
)

// DataProfile identifies a reusable authorized dataset contract. It carries
// no records, credentials, or business decisions.
type DataProfile string

const (
	DataProfileNone            DataProfile = "none"
	DataProfileWorkers         DataProfile = "workers"
	DataProfileJourneysWorkers DataProfile = "journeys_workers"
)

// DataRequirements is the exact dataset read contract for a page module.
type DataRequirements struct {
	Journeys bool
	Workers  bool
}

func (profile DataProfile) Requirements() DataRequirements {
	switch profile {
	case DataProfileWorkers:
		return DataRequirements{Workers: true}
	case DataProfileJourneysWorkers:
		return DataRequirements{Journeys: true, Workers: true}
	default:
		return DataRequirements{}
	}
}

// RouteStateProfile is the immutable semantic description used by address,
// request, and warm-refresh consumers. QueryKeys is copied before returning.
type RouteStateProfile struct {
	QueryKeys             []string
	PeopleDirectory       bool
	History               bool
	HistorySelectedPerson bool
	HistoryViewer         bool
	WorkflowQuery         bool
	Journeys              bool
	Work                  bool
	Organization          bool
	OrganizationOutline   bool
	Roles                 bool
	Studio                bool
	Person                bool
}

var shellRouteStateKeys = []string{"locale", "nav", "menu_q", "favorites"}
var organizationRouteStateKeys = []string{"org_view", "q", "person"}

func routeStateProfile(profile RouteProfile) RouteStateProfile {
	result := RouteStateProfile{}
	switch profile {
	case RouteProfileMyself:
		result.History, result.HistoryViewer = true, true
		result.WorkflowQuery = true
		result.QueryKeys = []string{"workflow_q", "history_q", "outcome", "history_person", "history_year", "history_sort", "history_dir", "history_page", "history_page_size"}
	case RouteProfileJourneys:
		result.Journeys = true
		result.QueryKeys = []string{"journey", "mode", "worker"}
	case RouteProfileWork:
		result.Work = true
		result.QueryKeys = []string{"filter", "selected"}
	case RouteProfileHistory:
		result.History = true
		result.QueryKeys = []string{"history_q", "outcome", "history_person", "history_year", "history_sort", "history_dir", "history_page", "history_page_size"}
	case RouteProfilePeople:
		result.PeopleDirectory = true
		result.QueryKeys = []string{"q", "page", "page_size", "team", "location", "eligible", "sort", "dir"}
	case RouteProfilePerson:
		result.PeopleDirectory, result.Person = true, true
		result.History, result.HistorySelectedPerson = true, true
		result.WorkflowQuery = true
		result.QueryKeys = []string{"person", "q", "page", "page_size", "team", "location", "eligible", "sort", "dir", "workflow_q", "history_q", "outcome", "history_person", "history_year", "history_sort", "history_dir", "history_page", "history_page_size"}
	case RouteProfileOrganization, RouteProfileOrgOutline:
		result.Organization = true
		result.OrganizationOutline = profile == RouteProfileOrgOutline
		result.QueryKeys = organizationRouteStateKeys
	case RouteProfileRoles:
		result.Roles = true
		result.QueryKeys = []string{"q", "role_page"}
	case RouteProfileStudio:
		result.Studio = true
		result.QueryKeys = []string{"mode"}
	}
	result.QueryKeys = append(append([]string(nil), shellRouteStateKeys...), result.QueryKeys...)
	return result
}

// StateProfile returns an independent query-key slice for this profile.
func (profile RouteProfile) StateProfile() RouteStateProfile { return routeStateProfile(profile) }

// QueryKeys returns all shell and page-scoped query keys admitted by profile.
func (profile RouteProfile) QueryKeys() []string { return profile.StateProfile().QueryKeys }

// ValidControlledValues validates profile-specific enumerated address values.
func (profile RouteProfile) ValidControlledValues(values url.Values) bool {
	oneOf := func(key string, allowed ...string) bool {
		value := strings.ToLower(strings.TrimSpace(values.Get(key)))
		for _, candidate := range allowed {
			if value == candidate {
				return true
			}
		}
		return value == ""
	}
	if !oneOf("sort", "name", "role", "team", "manager", "location") ||
		!oneOf("dir", "asc", "desc") || !oneOf("eligible", "1") ||
		!oneOf("history_sort", "person", "change", "closed", "outcome") ||
		!oneOf("history_dir", "asc", "desc") || !oneOf("outcome", "completed", "rejected", "failed") {
		return false
	}
	spec := profile.StateProfile()
	if spec.Work && !oneOf("filter", "review", "blocked", "complete", "mine", "tracked") {
		return false
	}
	if spec.Organization && !oneOf("org_view", "flat", "tree") {
		return false
	}
	if spec.Studio && !oneOf("mode", "preview", "validate") {
		return false
	}
	return true
}

func setProfileValue(values url.Values, provided map[string]bool, key, value string) {
	if provided[key] {
		values.Set(key, value)
	}
}

func setProfileInt(values url.Values, provided map[string]bool, key string, value int) {
	if provided[key] {
		values.Set(key, strconv.Itoa(value))
	}
}

// CanonicalValues serializes only values admitted by this profile and marked
// as present by the address parser. Organization outline intentionally emits
// its fixed tree presentation even when org_view was omitted.
func (profile RouteProfile) CanonicalValues(request PageRequest, provided map[string]bool) url.Values {
	values := url.Values{}
	setProfileValue(values, provided, "locale", request.Locale)
	if provided["nav"] {
		if request.NavCollapsed {
			values.Set("nav", "collapsed")
		} else {
			values.Set("nav", "expanded")
		}
	}
	setProfileValue(values, provided, "menu_q", request.MenuQuery)
	if provided["favorites"] {
		favoritePages := make([]string, 0, len(request.FavoritePages))
		for _, page := range request.FavoritePages {
			favoritePages = append(favoritePages, string(page))
		}
		values.Set("favorites", strings.Join(favoritePages, ","))
	}
	spec := profile.StateProfile()
	if spec.Journeys {
		setProfileValue(values, provided, "journey", request.JourneyID)
		setProfileValue(values, provided, "mode", request.JourneyMode)
		setProfileValue(values, provided, "worker", request.JourneyWorker)
	}
	if spec.Work {
		setProfileValue(values, provided, "filter", request.WorkFilter)
		setProfileValue(values, provided, "selected", request.SelectedWork)
	}
	if spec.PeopleDirectory {
		setProfileValue(values, provided, "q", request.Query)
		setProfileInt(values, provided, "page", request.PeoplePage)
		setProfileInt(values, provided, "page_size", request.PeoplePageSize)
		setProfileValue(values, provided, "team", request.PeopleTeam)
		setProfileValue(values, provided, "location", request.PeopleLocation)
		eligible := ""
		if request.PeopleEligibleOnly {
			eligible = "1"
		}
		setProfileValue(values, provided, "eligible", eligible)
		setProfileValue(values, provided, "sort", request.PeopleSort)
		setProfileValue(values, provided, "dir", request.PeopleDirection)
		if spec.Person {
			setProfileValue(values, provided, "person", request.SelectedPerson)
		}
	}
	if spec.History {
		setProfileValue(values, provided, "history_q", request.HistoryQuery)
		setProfileValue(values, provided, "outcome", request.HistoryOutcome)
		setProfileValue(values, provided, "history_person", request.HistoryPerson)
		setProfileValue(values, provided, "history_year", request.HistoryYear)
		setProfileValue(values, provided, "history_sort", request.HistorySort)
		setProfileValue(values, provided, "history_dir", request.HistoryDirection)
		setProfileInt(values, provided, "history_page", request.HistoryPage)
		setProfileInt(values, provided, "history_page_size", request.HistoryPageSize)
	}
	if spec.WorkflowQuery {
		setProfileValue(values, provided, "workflow_q", request.WorkflowQuery)
	}
	if spec.Organization {
		if spec.OrganizationOutline {
			values.Set("org_view", "tree")
		} else {
			setProfileValue(values, provided, "org_view", request.OrganizationView)
		}
		setProfileValue(values, provided, "q", request.Query)
		setProfileValue(values, provided, "person", request.SelectedPerson)
	}
	if spec.Roles {
		setProfileValue(values, provided, "q", request.Query)
		setProfileInt(values, provided, "role_page", request.RolePage)
	}
	if spec.Studio {
		setProfileValue(values, provided, "mode", request.Mode)
	}
	return values
}

// AddressValues adds this profile's non-empty state to a shell navigation
// address. Unlike CanonicalValues, this is used for generated links and does
// not depend on parser Provided bits.
func (profile RouteProfile) AddressValues(values url.Values, view View) {
	spec := profile.StateProfile()
	if spec.Journeys {
		if view.JourneyID != "" {
			values.Set("journey", view.JourneyID)
		}
		if view.JourneyMode != "" {
			values.Set("mode", view.JourneyMode)
		}
		if view.JourneyWorker != "" {
			values.Set("worker", view.JourneyWorker)
		}
	}
	if spec.Work {
		if view.WorkFilter != "" {
			values.Set("filter", view.WorkFilter)
		}
		if view.SelectedWork != "" {
			values.Set("selected", view.SelectedWork)
		}
	}
	if spec.PeopleDirectory {
		if view.Query != "" {
			values.Set("q", view.Query)
		}
		if view.PeopleTeam != "" {
			values.Set("team", view.PeopleTeam)
		}
		if view.PeopleLocation != "" {
			values.Set("location", view.PeopleLocation)
		}
		if view.PeopleSort != "" && view.PeopleSort != peopleSortName {
			values.Set("sort", view.PeopleSort)
		}
		if view.PeopleDirection == peopleSortDescending {
			values.Set("dir", view.PeopleDirection)
		}
		if view.PeoplePage > 1 {
			values.Set("page", strconv.Itoa(view.PeoplePage))
		}
		if normalized := normalizePageSize(view.PeoplePageSize); normalized != defaultPageSize {
			values.Set("page_size", strconv.Itoa(normalized))
		}
		if view.PeopleEligibleOnly {
			values.Set("eligible", "1")
		}
		if spec.Person && view.SelectedPerson != "" {
			values.Set("person", view.SelectedPerson)
		}
	}
	if spec.History {
		if view.HistoryQuery != "" {
			values.Set("history_q", view.HistoryQuery)
		}
		if view.HistoryOutcome != "" {
			values.Set("outcome", view.HistoryOutcome)
		}
		if view.HistoryPerson != "" {
			values.Set("history_person", view.HistoryPerson)
		}
		if view.HistoryYear != "" {
			values.Set("history_year", view.HistoryYear)
		}
		if view.HistorySort != "" {
			values.Set("history_sort", view.HistorySort)
		}
		if view.HistoryDirection != "" {
			values.Set("history_dir", view.HistoryDirection)
		}
		if view.HistoryPage > 1 {
			values.Set("history_page", strconv.Itoa(view.HistoryPage))
		}
		if normalized := normalizePageSize(view.HistoryPageSize); normalized != defaultPageSize {
			values.Set("history_page_size", strconv.Itoa(normalized))
		}
	}
	if spec.WorkflowQuery && view.WorkflowQuery != "" {
		values.Set("workflow_q", view.WorkflowQuery)
	}
	if spec.Organization {
		if spec.OrganizationOutline {
			values.Set("org_view", "tree")
		} else if view.OrganizationView != "" {
			values.Set("org_view", view.OrganizationView)
		}
		if view.Query != "" {
			values.Set("q", view.Query)
		}
		if view.SelectedPerson != "" {
			values.Set("person", view.SelectedPerson)
		}
	}
	if spec.Roles {
		if view.Query != "" {
			values.Set("q", view.Query)
		}
		if view.RolePage > 1 {
			values.Set("role_page", strconv.Itoa(view.RolePage))
		}
	}
	if spec.Studio && view.Mode != "" {
		values.Set("mode", view.Mode)
	}
}

// IdentityMatches reports whether warm content can remain visible while this
// profile's address is loading. It compares only route-owned subject state.
func (profile RouteProfile) IdentityMatches(view View, request PageRequest) bool {
	spec := profile.StateProfile()
	trimmedEqual := func(a, b string) bool { return strings.TrimSpace(a) == strings.TrimSpace(b) }
	if spec.Person && !trimmedEqual(view.SelectedPerson, request.SelectedPerson) {
		return false
	}
	if spec.Work && !trimmedEqual(view.SelectedWork, request.SelectedWork) {
		return false
	}
	if spec.History && !spec.HistoryViewer && !trimmedEqual(view.HistoryPerson, request.HistoryPerson) {
		return false
	}
	if spec.Organization && !trimmedEqual(view.SelectedPerson, request.SelectedPerson) {
		return false
	}
	return true
}

// PageModule is the immutable registration contract for a product page. Its
// definition, renderer, access policy, feature catalogue, and typed profiles
// are assembled once at package initialization.
type PageModule struct {
	Definition   PageDefinition
	Render       PageRenderer
	Access       PageAccessPolicy
	Features     []FeatureDefinition
	RouteProfile RouteProfile
	DataProfile  DataProfile
}

func routeProfileFor(route string) RouteProfile {
	switch route {
	case "/workspace/app/home":
		return RouteProfileHome
	case "/workspace/app/insights":
		return RouteProfileInsights
	case "/workspace/app/myself":
		return RouteProfileMyself
	case "/workspace/app/journeys":
		return RouteProfileJourneys
	case "/workspace/app/work":
		return RouteProfileWork
	case "/workspace/app/history":
		return RouteProfileHistory
	case "/workspace/app/people":
		return RouteProfilePeople
	case "/workspace/app/person":
		return RouteProfilePerson
	case "/workspace/app/organization", "/workspace/app/organization/explorer", "/workspace/app/organization/responsive":
		return RouteProfileOrganization
	case "/workspace/app/organization/outline":
		return RouteProfileOrgOutline
	case "/workspace/app/admin/roles":
		return RouteProfileRoles
	case "/workspace/app/studio":
		return RouteProfileStudio
	}
	switch {
	case strings.Contains(route, "/admin/"):
		return RouteProfileAdmin
	case strings.HasSuffix(route, "/help") || strings.HasSuffix(route, "/settings") || strings.Contains(route, "/help/"):
		return RouteProfileSupport
	case strings.HasSuffix(route, "/portal"):
		return RouteProfilePublic
	case strings.Count(route, "/") > 3:
		return RouteProfileNested
	default:
		return RouteProfileWorkspace
	}
}

func dataProfileFor(route string) DataProfile {
	profile := routeProfileFor(route)
	switch profile {
	case RouteProfileHome, RouteProfileInsights, RouteProfileMyself, RouteProfileJourneys, RouteProfileWork,
		RouteProfileHistory, RouteProfilePeople, RouteProfilePerson:
		return DataProfileJourneysWorkers
	case RouteProfileOrganization, RouteProfileOrgOutline, RouteProfileRoles:
		return DataProfileWorkers
	default:
		if route == "/workspace/app/admin/organization-visibility" {
			return DataProfileWorkers
		}
		return DataProfileNone
	}
}

func pageModule(definition PageDefinition, render PageRenderer, access PageAccessPolicy, routeProfile RouteProfile, dataProfile DataProfile, specialized ...FeatureDefinition) PageModule {
	if provider, ok := render.(PageFeatureProvider); ok {
		specialized = append(specialized, provider.PageFeatures()...)
	}
	definition.Features = pageFeatureDefinitions(specialized...)
	return PageModule{
		Definition:   definition,
		Render:       render,
		Access:       access,
		Features:     append([]FeatureDefinition(nil), definition.Features...),
		RouteProfile: routeProfile,
		DataProfile:  dataProfile,
	}
}

type pageModuleIDIndex struct {
	id    PageID
	index int
}

type pageModuleRouteIndex struct {
	route string
	index int
}

// pageModuleRegistry is built as one value and never mutated. Sorted slice
// indexes retain allocation-free logarithmic lookup without introducing a
// package-level mutable map registry.
type pageModuleRegistry struct {
	modules []PageModule
	pages   []PageDefinition
	ids     []pageModuleIDIndex
	routes  []pageModuleRouteIndex
}

var fixedPageRegistry = mustBuildPageModuleRegistry(buildRegisteredModules())

func mustBuildPageModuleRegistry(modules []PageModule) pageModuleRegistry {
	if err := ValidatePageModules(modules); err != nil {
		panic(err)
	}
	registry := pageModuleRegistry{
		modules: modules,
		pages:   make([]PageDefinition, len(modules)),
		ids:     make([]pageModuleIDIndex, len(modules)),
		routes:  make([]pageModuleRouteIndex, len(modules)),
	}
	for index, module := range modules {
		registry.pages[index] = module.Definition
		registry.ids[index] = pageModuleIDIndex{id: module.Definition.ID, index: index}
		registry.routes[index] = pageModuleRouteIndex{route: module.Definition.Route, index: index}
	}
	sort.Slice(registry.ids, func(i, j int) bool { return registry.ids[i].id < registry.ids[j].id })
	sort.Slice(registry.routes, func(i, j int) bool { return registry.routes[i].route < registry.routes[j].route })
	return registry
}

func clonePageModule(module PageModule) PageModule {
	module.Definition = clonePageDefinition(module.Definition)
	module.Features = append([]FeatureDefinition(nil), module.Features...)
	return module
}

func registeredPages() []PageDefinition {
	return fixedPageRegistry.pages
}

// PageModules returns independent copies of the immutable page-module
// registry. Mutating a returned module cannot affect rendering or visibility.
func PageModules() []PageModule {
	result := make([]PageModule, len(fixedPageRegistry.modules))
	for index, module := range fixedPageRegistry.modules {
		result[index] = clonePageModule(module)
	}
	return result
}

func LookupPageModule(id PageID) (PageModule, bool) {
	position := sort.Search(len(fixedPageRegistry.ids), func(index int) bool { return fixedPageRegistry.ids[index].id >= id })
	if position == len(fixedPageRegistry.ids) || fixedPageRegistry.ids[position].id != id {
		return PageModule{}, false
	}
	return clonePageModule(fixedPageRegistry.modules[fixedPageRegistry.ids[position].index]), true
}

func LookupRouteModule(route string) (PageModule, bool) {
	position := sort.Search(len(fixedPageRegistry.routes), func(index int) bool { return fixedPageRegistry.routes[index].route >= route })
	if position == len(fixedPageRegistry.routes) || fixedPageRegistry.routes[position].route != route {
		return PageModule{}, false
	}
	return clonePageModule(fixedPageRegistry.modules[fixedPageRegistry.routes[position].index]), true
}

// PageProfiles returns the immutable route and data contracts for a page
// without cloning the larger contributor-facing PageModule value. Runtime
// routing and refresh paths should prefer this allocation-free lookup.
func PageProfiles(id PageID) (RouteProfile, DataProfile, bool) {
	module, ok := pageModuleFor(id)
	if !ok {
		return "", "", false
	}
	return module.RouteProfile, module.DataProfile, true
}

// RouteProfiles resolves a canonical route to the page and its immutable
// route/data contracts without exposing registry-owned slices.
func RouteProfiles(route string) (PageID, RouteProfile, DataProfile, bool) {
	position := sort.Search(len(fixedPageRegistry.routes), func(index int) bool {
		return fixedPageRegistry.routes[index].route >= route
	})
	if position == len(fixedPageRegistry.routes) || fixedPageRegistry.routes[position].route != route {
		return "", "", "", false
	}
	module := fixedPageRegistry.modules[fixedPageRegistry.routes[position].index]
	return module.Definition.ID, module.RouteProfile, module.DataProfile, true
}

func pageModuleFor(id PageID) (PageModule, bool) {
	position := sort.Search(len(fixedPageRegistry.ids), func(index int) bool { return fixedPageRegistry.ids[index].id >= id })
	if position == len(fixedPageRegistry.ids) || fixedPageRegistry.ids[position].id != id {
		return PageModule{}, false
	}
	return fixedPageRegistry.modules[fixedPageRegistry.ids[position].index], true
}

func pageVisibleForPolicy(policy PageAccessPolicy, roles []string) bool {
	switch policy.Audience {
	case PageAudiencePublic, PageAudiencePortal:
		return true
	case PageAudienceWorker:
		return hasAnyProductRole(roles, "worker_self", "manager", "hr_partner", "hiring_manager", "payroll_manager")
	case PageAudienceWorkerFinance:
		return hasAnyProductRole(roles, "worker_self", "manager", "hr_partner", "hiring_manager", "payroll_manager", "finance_partner")
	case PageAudienceHRPartner:
		return hasProductRole(roles, "hr_partner")
	case PageAudienceManager:
		return hasAnyProductRole(roles, "manager", "hr_partner", "comp_admin", "hiring_manager", "payroll_manager")
	case PageAudienceManagerFinance:
		return hasAnyProductRole(roles, "manager", "hr_partner", "comp_admin", "hiring_manager", "payroll_manager", "finance_partner")
	default:
		return false
	}
}

func validPageAccessAudience(audience PageAccessAudience) bool {
	switch audience {
	case PageAudiencePublic, PageAudiencePortal, PageAudienceWorker, PageAudienceWorkerFinance,
		PageAudienceManager, PageAudienceManagerFinance, PageAudienceHRPartner, PageAudienceDenied:
		return true
	default:
		return false
	}
}

func validFeatureID(id FeatureID) bool {
	if id == "" {
		return false
	}
	for index, character := range string(id) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '_' || character == '-' {
			if index == 0 && character >= '0' && character <= '9' {
				return false
			}
			continue
		}
		return false
	}
	return true
}

func validRouteProfile(profile RouteProfile) bool {
	switch profile {
	case RouteProfileWorkspace, RouteProfileNested, RouteProfileSupport, RouteProfileAdmin, RouteProfilePublic,
		RouteProfileHome, RouteProfileInsights, RouteProfileMyself, RouteProfileJourneys, RouteProfileWork,
		RouteProfileHistory, RouteProfilePeople, RouteProfilePerson, RouteProfileOrganization, RouteProfileOrgOutline,
		RouteProfileRoles, RouteProfileStudio:
		return true
	default:
		return false
	}
}

func validDataProfile(profile DataProfile) bool {
	switch profile {
	case DataProfileNone, DataProfileWorkers, DataProfileJourneysWorkers:
		return true
	default:
		return false
	}
}

// ValidatePageModules checks a candidate module list in stable slice order.
func ValidatePageModules(modules []PageModule) error {
	if len(modules) == 0 {
		return fmt.Errorf("productui: page module registry is empty")
	}
	ids := make(map[PageID]struct{}, len(modules))
	routes := make(map[string]struct{}, len(modules))
	previousOrder := 0
	for index, module := range modules {
		definition := module.Definition
		if definition.ID == "" {
			return fmt.Errorf("productui: page module %d has empty ID", index)
		}
		if definition.Route == "" {
			return fmt.Errorf("productui: page module %q has empty route", definition.ID)
		}
		if !strings.HasPrefix(definition.Route, "/workspace/app/") {
			return fmt.Errorf("productui: page module %q has invalid product route %q", definition.ID, definition.Route)
		}
		if definition.Label == "" || definition.Icon == "" || definition.Title == "" || definition.Subtitle == "" ||
			definition.LabelKey == "" || definition.TitleKey == "" || definition.SubtitleKey == "" {
			return fmt.Errorf("productui: page module %q has incomplete identity", definition.ID)
		}
		if definition.RenderOrder <= previousOrder {
			return fmt.Errorf("productui: page module %q has unstable render order %d", definition.ID, definition.RenderOrder)
		}
		previousOrder = definition.RenderOrder
		if _, exists := ids[definition.ID]; exists {
			return fmt.Errorf("productui: duplicate page module ID %q", definition.ID)
		}
		if _, exists := routes[definition.Route]; exists {
			return fmt.Errorf("productui: duplicate page module route %q", definition.Route)
		}
		ids[definition.ID] = struct{}{}
		routes[definition.Route] = struct{}{}
		if module.Render == nil {
			return fmt.Errorf("productui: page module %q has no renderer", definition.ID)
		}
		if !validPageAccessAudience(module.Access.Audience) {
			return fmt.Errorf("productui: page module %q has invalid access audience %q", definition.ID, module.Access.Audience)
		}
		if len(module.Features) == 0 {
			return fmt.Errorf("productui: page module %q has no features", definition.ID)
		}
		featureIDs := make(map[FeatureID]struct{}, len(module.Features))
		for _, feature := range module.Features {
			if !validFeatureID(feature.ID) {
				return fmt.Errorf("productui: page module %q has invalid feature ID %q", definition.ID, feature.ID)
			}
			if _, exists := featureIDs[feature.ID]; exists {
				return fmt.Errorf("productui: page module %q has duplicate feature ID %q", definition.ID, feature.ID)
			}
			featureIDs[feature.ID] = struct{}{}
			if (feature.Create || feature.Update || feature.Delete) && !feature.View {
				return fmt.Errorf("productui: page module %q feature %q exceeds its view ceiling", definition.ID, feature.ID)
			}
		}
		if !slices.Equal(module.Features, definition.Features) {
			return fmt.Errorf("productui: page module %q has divergent feature catalogue", definition.ID)
		}
		if !validRouteProfile(module.RouteProfile) {
			if module.RouteProfile == "" {
				return fmt.Errorf("productui: page module %q has no route profile", definition.ID)
			}
			return fmt.Errorf("productui: page module %q has invalid route profile %q", definition.ID, module.RouteProfile)
		}
		if !validDataProfile(module.DataProfile) {
			if module.DataProfile == "" {
				return fmt.Errorf("productui: page module %q has no data profile", definition.ID)
			}
			return fmt.Errorf("productui: page module %q has invalid data profile %q", definition.ID, module.DataProfile)
		}
	}
	for _, module := range modules {
		if module.Definition.ParentNav == "" {
			continue
		}
		if _, ok := ids[module.Definition.ParentNav]; !ok {
			return fmt.Errorf("productui: page module %q has unknown navigation parent %q", module.Definition.ID, module.Definition.ParentNav)
		}
	}
	return nil
}

// ValidatePageRegistry validates the built-in immutable registry.
func ValidatePageRegistry() error { return ValidatePageModules(fixedPageRegistry.modules) }
