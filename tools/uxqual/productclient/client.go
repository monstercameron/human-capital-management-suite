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
	"time"
	"unicode"
	"unicode/utf8"

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	positionv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/position/v1"
	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/uicomponents"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Service is the bounded RPC surface needed by the product shell. Most pages
// consume reads; workflow authoring adds only its explicit draft mutations.
type Service struct {
	ListDocuments func(context.Context, *documentv1.ListDocumentsRequest) (*documentv1.ListDocumentsResponse, error)
	GetDocument   func(context.Context, *documentv1.GetDocumentRequest) (*documentv1.GetDocumentResponse, error)
	// ResolveDocsProjectTaskPreview returns only a task projection the current
	// principal may read. A false found value represents denial and absence.
	ResolveDocsProjectTaskPreview func(context.Context, productui.DocsProjectTaskReference) (productui.DocsProjectTaskPreview, bool, error)
	// ResolveDocsJourneyPreview returns only a journey quick look the current
	// viewer is authorized to read; found=false for refused or missing ones.
	ResolveDocsJourneyPreview    func(context.Context, string) (productui.DocsJourneyPreview, bool, error)
	ListDocumentComments         func(context.Context, *documentv1.ListDocumentCommentsRequest) (*documentv1.ListDocumentCommentsResponse, error)
	ShareDocument                func(context.Context, *documentv1.ShareDocumentRequest) (*documentv1.ShareDocumentResponse, error)
	GetDocumentLibrary           func(context.Context, *documentv1.GetDocumentLibraryRequest) (*documentv1.GetDocumentLibraryResponse, error)
	GetPositionObject            func(context.Context, string) (*productui.PositionObjectProjection, error)
	GetPositionOccupancy         func(context.Context, string) (*productui.PositionOccupancyProjection, error)
	ListPositionObjectOptions    func(context.Context, *positionv1.ListPositionObjectOptionsRequest) (*positionv1.ListPositionObjectOptionsResponse, error)
	ListPositionOccupancyOptions func(context.Context, *positionv1.ListPositionOccupancyOptionsRequest) (*positionv1.ListPositionOccupancyOptionsResponse, error)
	GetReviewParticipants        func(context.Context) (*productui.ReviewParticipantsProjection, error)
	ListJourneys                 func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error)
	ListWorkers                  func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error)
	GetPreferences               func(context.Context, *journeyv1.GetProductPreferencesRequest) (*journeyv1.GetProductPreferencesResponse, error)
	GetWorkerIDPolicy            func(context.Context, *journeyv1.GetWorkerIDPolicyRequest) (*journeyv1.GetWorkerIDPolicyResponse, error)
	GetRoleAccess                func(context.Context, *journeyv1.GetRoleAccessRequest) (*journeyv1.GetRoleAccessResponse, error)
	SearchKnowledge              func(context.Context, *journeyv1.SearchKnowledgeRequest) (*journeyv1.SearchKnowledgeResponse, error)
	ListWorkflowPublications     func(context.Context, *workflowv1.ListWorkflowPublicationsRequest) (*workflowv1.ListWorkflowPublicationsResponse, error)
	GetWorkflowDefinitionView    func(context.Context, *workflowv1.GetWorkflowDefinitionViewRequest) (*workflowv1.GetWorkflowDefinitionViewResponse, error)
	ListWorkflowBlocks           func(context.Context, *workflowv1.ListWorkflowBlocksRequest) (*workflowv1.ListWorkflowBlocksResponse, error)
	CreateWorkflowDraft          func(context.Context, *workflowv1.CreateWorkflowDraftRequest) (*workflowv1.CreateWorkflowDraftResponse, error)
	GetWorkflowDraft             func(context.Context, *workflowv1.GetWorkflowDraftRequest) (*workflowv1.GetWorkflowDraftResponse, error)
	InsertWorkflowPaletteEntry   func(context.Context, *workflowv1.InsertWorkflowPaletteEntryRequest) (*workflowv1.InsertWorkflowPaletteEntryResponse, error)
	UpdateWorkflowDraftNode      func(context.Context, *workflowv1.UpdateWorkflowDraftNodeRequest) (*workflowv1.UpdateWorkflowDraftNodeResponse, error)
	SetWorkflowDraftOutcome      func(context.Context, *workflowv1.SetWorkflowDraftOutcomeRequest) (*workflowv1.SetWorkflowDraftOutcomeResponse, error)
	BindWorkflowDraftInput       func(context.Context, *workflowv1.BindWorkflowDraftInputRequest) (*workflowv1.BindWorkflowDraftInputResponse, error)
	MoveWorkflowDraftNode        func(context.Context, *workflowv1.MoveWorkflowDraftNodeRequest) (*workflowv1.MoveWorkflowDraftNodeResponse, error)
	RemoveWorkflowDraftNode      func(context.Context, *workflowv1.RemoveWorkflowDraftNodeRequest) (*workflowv1.RemoveWorkflowDraftNodeResponse, error)
	ClearWorkflowDraftOutcome    func(context.Context, *workflowv1.ClearWorkflowDraftOutcomeRequest) (*workflowv1.ClearWorkflowDraftOutcomeResponse, error)
	RenameWorkflowDraft          func(context.Context, *workflowv1.RenameWorkflowDraftRequest) (*workflowv1.RenameWorkflowDraftResponse, error)
	NavigateWorkflowDraftHistory func(context.Context, *workflowv1.NavigateWorkflowDraftHistoryRequest) (*workflowv1.NavigateWorkflowDraftHistoryResponse, error)
	ApplyWorkflowTemplateOverlay func(context.Context, *workflowv1.ApplyWorkflowTemplateOverlayRequest) (*workflowv1.ApplyWorkflowTemplateOverlayResponse, error)
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
	FeaturePermissions    []productui.RoleFeaturePermission
	LauncherActions       []productui.LauncherActionProjection
	EnforceRoleVisibility bool
	LogoutHref            string
	// TenantName is the company's own display name when the server supplied
	// one; empty derives the label from Tenant.
	TenantName string
}

// tenantLabel is the name the shell shows for the session's company.
func (session Session) tenantLabel() string {
	if name := strings.TrimSpace(session.TenantName); name != "" {
		return name
	}
	return displayLabel(session.Tenant)
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
	view := productui.NewView(state.Page, session.tenantLabel(), displayLabel(session.Principal), displayLabel(session.Scope))
	view.ViewerSubject = strings.TrimSpace(session.Principal)
	view.LogoutHref = session.LogoutHref
	if session.EnforceRoleVisibility {
		view = productui.ApplyRoleVisibility(view, session.Roles)
	}
	if len(session.Permissions) > 0 {
		view = productui.ApplyPagePermissions(view, session.Permissions)
	}
	if session.FeaturePermissions != nil {
		view = productui.ApplyFeaturePermissions(view, session.FeaturePermissions)
	}
	view.LauncherActions = append([]productui.LauncherActionProjection(nil), session.LauncherActions...)
	view = productui.ApplyRequest(view, state.Request)
	if view.Page == productui.PageHome {
		// The authenticated principal is not necessarily the admitted worker's
		// preferred display name. Hold a stable generic document title until
		// the workforce response resolves Viewer, matching the loading H1.
		view.Title = view.Locale.Text("page.home.title")
	}
	return view
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
	page, routeProfile, _, ok := productui.RouteProfiles(pathname)
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
	allowed := make(map[string]bool, len(routeProfile.QueryKeys()))
	for _, key := range routeProfile.QueryKeys() {
		allowed[key] = true
	}
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
	rolePage, err := parsePositiveRouteInt(values, "role_page", 1)
	if err != nil {
		return State{}, err
	}
	documentPage, err := parsePositiveRouteInt(values, "docs_page", 1)
	if err != nil {
		return State{}, err
	}
	documentPerPage, err := parsePositiveRouteInt(values, "docs_size", 0)
	if err != nil {
		return State{}, err
	}
	historyPageSize, err := parsePageSizeRouteInt(values, "history_page_size")
	if err != nil {
		return State{}, err
	}
	if !routeProfile.ValidControlledValues(values) {
		return State{}, errors.New("productclient: route state is malformed")
	}
	state := State{Page: page, Provided: provided, Request: productui.PageRequest{
		Page: page, Locale: routeValue(values, "locale"), Query: routeValue(values, "q"), RolePage: rolePage, Mode: routeValue(values, "mode"),
		SelectedWork: routeValue(values, "selected"), SelectedPerson: routeValue(values, "person"),
		PeoplePage: peoplePage, PeoplePageSize: peoplePageSize, PeopleTeam: routeValue(values, "team"), PeopleLocation: routeValue(values, "location"), PeopleEligibleOnly: routeValue(values, "eligible") == "1", PeopleSort: routeValue(values, "sort"), PeopleDirection: routeValue(values, "dir"),
		PeopleColumns:    routeValue(values, "columns"),
		OrganizationView: routeValue(values, "org_view"), OrganizationAsOf: routeValue(values, "as_of"), OrganizationUnit: routeValue(values, "unit"),
		WorkflowQuery: routeValue(values, "workflow_q"), HistoryQuery: routeValue(values, "history_q"), HistoryOutcome: routeValue(values, "outcome"),
		HistoryPerson: routeValue(values, "history_person"), HistoryYear: routeValue(values, "history_year"), HistorySort: routeValue(values, "history_sort"), HistoryDirection: routeValue(values, "history_dir"), HistoryPage: historyPage, HistoryPageSize: historyPageSize,
		WorkFilter: routeValue(values, "filter"), NavCollapsed: routeValue(values, "nav") == "collapsed",
		JourneyID: routeValue(values, "journey"), JourneyWorker: routeValue(values, "worker"), JourneyMode: routeValue(values, "mode"),
		WorkflowID: routeValue(values, "workflow"), WorkflowRunID: routeValue(values, "run"), WorkflowDraftID: routeValue(values, "draft"), WorkflowNodeID: routeValue(values, "node"), DocumentID: routeValue(values, "document"), DocumentQuery: routeValue(values, "docs_q"), DocumentCollection: routeValue(values, "collection"), DocumentPageToken: routeValue(values, "cursor"),
		DocumentFolder: routeValue(values, "folder"), DocumentSort: routeValue(values, "docs_sort"), DocumentOwner: routeValue(values, "docs_owner"), DocumentPage: documentPage, DocumentPerPage: documentPerPage, DocumentSearchMode: routeValue(values, "docs_mode"), DocumentEditing: routeValue(values, "docs_edit") == "1",
		PositionReference: routeValue(values, "position_ref"),
		MenuQuery:         routeValue(values, "menu_q"), FavoritePages: parseFavoritePages(routeValue(values, "favorites")),
	}}
	if routeProfile.StateProfile().Journeys {
		state.Request.JourneyList = productui.JourneyListFilterFromValues(values)
		if state.Request.JourneyID != "" {
			state.Request.Mode = ""
			state.Request.JourneyMode = ""
			state.Request.JourneyWorker = ""
			state.Request.JourneyList = productui.JourneyListFilter{}
			delete(state.Provided, "mode")
			delete(state.Provided, "worker")
			for _, key := range productui.JourneyListRouteKeys() {
				delete(state.Provided, key)
			}
		} else if state.Request.JourneyMode != "" && state.Request.JourneyMode != "new" {
			return State{}, errors.New("productclient: route state is malformed")
		}
	}
	return state, nil
}

const maxRouteQueryBytes = 4096

func safeRouteValue(value string) bool {
	return len(value) <= maxRouteQueryBytes && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0
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

func routeValue(values url.Values, key string) string {
	return strings.TrimSpace(values.Get(key))
}

// CanonicalHref returns the only browser address representation of parsed
// product state. It serializes page-scoped presentation values and nothing
// else; it can never carry a credential, action token, or service response.
func CanonicalHref(state State) string {
	routeProfile, _, ok := productui.PageProfiles(state.Page)
	if !ok {
		return productui.Path(state.Page)
	}
	values := routeProfile.CanonicalValues(state.Request, state.Provided)
	if state.Page == productui.PagePositionObject || state.Page == productui.PagePositionOccupancy {
		if reference := strings.TrimSpace(state.Request.PositionReference); reference != "" {
			values.Set("position_ref", reference)
		}
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
	if profile := routeProfileFor(state.Page); profile.PeopleDirectory {
		if state.Provided["page"] {
			resolved.Request.PeoplePage = view.PeoplePage
		}
		if state.Provided["page_size"] {
			resolved.Request.PeoplePageSize = view.PeoplePageSize
		}
	}
	if profile := routeProfileFor(state.Page); profile.CarriesDirectoryQuery && !state.Provided["q"] && strings.TrimSpace(view.Query) != "" {
		// REV-095-04: a search carried in from the People directory is
		// written into this page's own address, so the filtered browse is
		// shareable, reloads the same and is reversible with Back. Provided
		// is copied: the parsed state belongs to the caller.
		provided := make(map[string]bool, len(state.Provided)+1)
		for key, value := range state.Provided {
			provided[key] = value
		}
		provided["q"] = true
		resolved.Provided = provided
		resolved.Request.Query = strings.TrimSpace(view.Query)
	}
	if profile := routeProfileFor(state.Page); profile.Roles && state.Provided["role_page"] {
		resolved.Request.RolePage = view.RolePage
	}
	if profile := routeProfileFor(state.Page); profile.History {
		if state.Provided["history_page"] {
			resolved.Request.HistoryPage = view.HistoryPage
		}
		if state.Provided["history_page_size"] {
			resolved.Request.HistoryPageSize = view.HistoryPageSize
		}
	}
	if state.Page == productui.PageDocs && state.Provided["cursor"] {
		resolved.Request.DocumentPageToken = view.DocumentPageToken
		if view.DocumentPageToken == "" {
			provided := make(map[string]bool, len(resolved.Provided))
			for key, value := range resolved.Provided {
				if key != "cursor" {
					provided[key] = value
				}
			}
			resolved.Provided = provided
		}
	}
	return CanonicalHref(resolved)
}

func routeProfileFor(page productui.PageID) productui.RouteStateProfile {
	routeProfile, _, ok := productui.PageProfiles(page)
	if !ok {
		return productui.RouteStateProfile{}
	}
	return routeProfile.StateProfile()
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
	journeys           bool
	workers            bool
	workflows          bool
	documents          bool
	knowledge          bool
	positionObject     bool
	positionOccupancy  bool
	reviewParticipants bool
}

func requirementsForPage(page productui.PageID) pageDataRequirements {
	_, dataProfile, ok := productui.PageProfiles(page)
	if !ok {
		return pageDataRequirements{}
	}
	requirements := dataProfile.Requirements()
	return pageDataRequirements{
		journeys: requirements.Journeys, workers: requirements.Workers,
		workflows: page == productui.PageWorkflowDesigner, documents: page == productui.PageDocs,
		knowledge:      page == productui.PageKnowledgeSearch,
		positionObject: page == productui.PagePositionObject, positionOccupancy: page == productui.PagePositionOccupancy,
		reviewParticipants: page == productui.PageReviewParticipants,
	}
}

func load(ctx context.Context, service Service, session Session, state State, baseline *productui.View) (productui.View, error) {
	view := LoadingView(session, state)
	requirements := pageDataRequirements{
		journeys: true, workers: true, workflows: state.Page == productui.PageWorkflowDesigner,
		documents: state.Page == productui.PageDocs, knowledge: state.Page == productui.PageKnowledgeSearch,
		positionObject: state.Page == productui.PagePositionObject, positionOccupancy: state.Page == productui.PagePositionOccupancy,
		reviewParticipants: state.Page == productui.PageReviewParticipants,
	}
	if baseline != nil && baselineMatchesSession(*baseline, view) {
		requirements = requirementsForPage(state.Page)
		seedBaselineProjection(&view, *baseline)
		view.DocumentUnavailable = false
		if state.Page == productui.PageDocs && strings.TrimSpace(state.Request.DocumentID) == "" {
			view.Document = nil
		}
		if state.Page == productui.PageWorkflowDesigner {
			// A route names either one mutable draft or one immutable
			// publication/run. Never carry the other route's leaf projection
			// across navigation: doing so made a published URL render the last
			// mutable draft until a hard reload.
			if strings.TrimSpace(state.Request.WorkflowDraftID) == "" {
				view.WorkflowDraft = nil
				view.SelectedWorkflowDraftID = ""
			} else {
				view.WorkflowView = nil
				view.SelectedWorkflowID = ""
				view.SelectedWorkflowRunID = ""
			}
		}
		// Work filters are applied destructively to the route projection. Never
		// mistake a filtered previous page for the complete shell dataset.
		if baselineRoute, _, ok := productui.PageProfiles(baseline.Page); ok && baselineRoute.StateProfile().Work && baseline.WorkFilter != "" {
			requirements.journeys = true
		}
	}
	var failures []error
	var journeysResponse *journeyv1.ListJourneysResponse
	var workersResponse *journeyv1.ListWorkersResponse
	var preferencesResponse *journeyv1.GetProductPreferencesResponse
	var workerIDResponse *journeyv1.GetWorkerIDPolicyResponse
	var roleAccessResponse *journeyv1.GetRoleAccessResponse
	var workflowCatalogResponse *workflowv1.ListWorkflowPublicationsResponse
	var workflowBlocksResponse *workflowv1.ListWorkflowBlocksResponse
	var workflowDraftResponse *workflowv1.GetWorkflowDraftResponse
	var documentsResponse *documentv1.ListDocumentsResponse
	var libraryResponse *documentv1.GetDocumentLibraryResponse
	var documentResponse *documentv1.GetDocumentResponse
	var documentCommentsResponse *documentv1.ListDocumentCommentsResponse
	var documentProjectTasks []productui.DocsProjectTaskPreview
	var documentJourneys []productui.DocsJourneyPreview
	var knowledgeResponse *journeyv1.SearchKnowledgeResponse
	var positionObject *productui.PositionObjectProjection
	var positionOccupancy *productui.PositionOccupancyProjection
	var positionObjectOptions *positionv1.ListPositionObjectOptionsResponse
	var positionOccupancyOptions *positionv1.ListPositionOccupancyOptionsResponse
	var reviewParticipants *productui.ReviewParticipantsProjection
	var journeysErr, workersErr, preferencesErr error
	var documentsErr error
	var documentCursorReset bool
	var documentErr error
	var documentCommentsErr error
	var knowledgeErr error
	var positionObjectErr, positionOccupancyErr error
	var positionOptionsErr error
	var reviewParticipantsErr error
	var workerIDErr error
	var roleAccessErr error
	var workflowCatalogErr error
	var workflowBlocksErr error
	var workflowDraftErr error
	var reads sync.WaitGroup
	if requirements.positionObject {
		if service.ListPositionObjectOptions == nil {
			positionOptionsErr = errors.New("PositionService.ListPositionObjectOptions is not connected")
		} else {
			reads.Add(1)
			go func() {
				defer reads.Done()
				positionObjectOptions, positionOptionsErr = service.ListPositionObjectOptions(ctx, &positionv1.ListPositionObjectOptionsRequest{})
				if positionOptionsErr != nil {
					positionOptionsErr = fmt.Errorf("list position object options: %w", positionOptionsErr)
				}
			}()
		}
		if strings.TrimSpace(state.Request.PositionReference) != "" {
			if service.GetPositionObject == nil {
				positionObjectErr = errors.New("PositionService.GetPositionObject is not connected")
			} else {
				reads.Add(1)
				go func() {
					defer reads.Done()
					positionObject, positionObjectErr = service.GetPositionObject(ctx, state.Request.PositionReference)
					if positionObjectErr != nil {
						positionObjectErr = fmt.Errorf("load position object: %w", positionObjectErr)
					}
				}()
			}
		}
	}
	if requirements.positionOccupancy {
		if service.ListPositionOccupancyOptions == nil {
			positionOptionsErr = errors.New("PositionService.ListPositionOccupancyOptions is not connected")
		} else {
			reads.Add(1)
			go func() {
				defer reads.Done()
				positionOccupancyOptions, positionOptionsErr = service.ListPositionOccupancyOptions(ctx, &positionv1.ListPositionOccupancyOptionsRequest{})
				if positionOptionsErr != nil {
					positionOptionsErr = fmt.Errorf("list position occupancy options: %w", positionOptionsErr)
				}
			}()
		}
		if strings.TrimSpace(state.Request.PositionReference) != "" {
			if service.GetPositionOccupancy == nil {
				positionOccupancyErr = errors.New("PositionService.GetPositionOccupancy is not connected")
			} else {
				reads.Add(1)
				go func() {
					defer reads.Done()
					positionOccupancy, positionOccupancyErr = service.GetPositionOccupancy(ctx, state.Request.PositionReference)
					if positionOccupancyErr != nil {
						positionOccupancyErr = fmt.Errorf("load position occupancy: %w", positionOccupancyErr)
					}
				}()
			}
		}
	}
	if requirements.reviewParticipants {
		if service.GetReviewParticipants == nil {
			reviewParticipantsErr = errors.New("ReviewParticipantsService.GetReviewParticipants is not connected")
		} else {
			reads.Add(1)
			go func() {
				defer reads.Done()
				reviewParticipants, reviewParticipantsErr = service.GetReviewParticipants(ctx)
				if reviewParticipantsErr != nil {
					reviewParticipantsErr = fmt.Errorf("load review participants: %w", reviewParticipantsErr)
				}
			}()
		}
	}
	if requirements.documents && service.GetDocumentLibrary != nil && strings.TrimSpace(state.Request.DocumentID) == "" {
		// The library (folders and counts) only decorates the list; a
		// failure leaves the list usable without its navigation badges.
		reads.Add(1)
		go func() {
			defer reads.Done()
			libraryResponse, _ = service.GetDocumentLibrary(ctx, &documentv1.GetDocumentLibraryRequest{})
		}()
	}
	if requirements.documents {
		if service.ListDocuments == nil {
			documentsErr = errors.New("DocumentService.ListDocuments is not connected")
		} else {
			reads.Add(1)
			go func() {
				defer reads.Done()
				request := documentListRequest(state.Request)
				documentsResponse, documentsErr = service.ListDocuments(ctx, request)
				if documentsErr != nil && request.PageToken != "" && status.Code(documentsErr) == codes.InvalidArgument {
					request.PageToken = ""
					documentsResponse, documentsErr = service.ListDocuments(ctx, request)
					if documentsErr == nil {
						documentCursorReset = true
					}
				}
				if documentsErr != nil {
					documentsErr = fmt.Errorf("list documents: %w", documentsErr)
				}
			}()
		}
		if state.Request.DocumentID != "" {
			if service.GetDocument == nil {
				documentErr = errors.New("DocumentService.GetDocument is not connected")
			} else {
				reads.Add(1)
				go func() {
					defer reads.Done()
					documentResponse, documentErr = service.GetDocument(ctx, &documentv1.GetDocumentRequest{DocumentId: state.Request.DocumentID})
					if documentErr == nil && documentResponse != nil && documentResponse.GetDocument() != nil {
						if service.ListDocumentComments == nil {
							documentCommentsErr = errors.New("DocumentService.ListDocumentComments is not connected")
						} else {
							documentCommentsResponse, documentCommentsErr = service.ListDocumentComments(ctx, &documentv1.ListDocumentCommentsRequest{DocumentId: state.Request.DocumentID, VersionId: documentResponse.GetDocument().GetVersionId()})
						}
					}
				}()
			}
		}
	}
	if requirements.knowledge && strings.TrimSpace(state.Request.Query) != "" {
		if service.SearchKnowledge == nil {
			knowledgeErr = errors.New("JourneyService.SearchKnowledge is not connected")
		} else {
			reads.Add(1)
			go func() {
				defer reads.Done()
				knowledgeResponse, knowledgeErr = service.SearchKnowledge(ctx, &journeyv1.SearchKnowledgeRequest{
					Query: state.Request.Query, Locale: state.Request.Locale,
				})
			}()
		}
	}
	if !requirements.journeys {
		// The authorized baseline already supplies shell counts/search records.
	} else if session.EnforceRoleVisibility && !productui.PageVisible(productui.PageJourneys, session.Roles) &&
		!productui.PageVisible(productui.PageWork, session.Roles) {
		// The journey list backs My Work as well as Journeys, and the server
		// admits either page's grant. Skipping it for everyone without the
		// Journeys page left a finance approver's My Work empty: the review
		// routed to them never appeared anywhere they could reach.
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
	if requirements.workflows {
		if service.ListWorkflowPublications == nil {
			workflowCatalogErr = errors.New("WorkflowService.ListWorkflowPublications is not connected")
		} else {
			reads.Add(1)
			go func() {
				defer reads.Done()
				workflowCatalogResponse, workflowCatalogErr = service.ListWorkflowPublications(ctx, &workflowv1.ListWorkflowPublicationsRequest{})
				if workflowCatalogErr != nil {
					workflowCatalogErr = fmt.Errorf("list workflow publications: %w", workflowCatalogErr)
				}
			}()
		}
		if service.ListWorkflowBlocks == nil {
			workflowBlocksErr = errors.New("WorkflowService.ListWorkflowBlocks is not connected")
		} else {
			reads.Add(1)
			go func() {
				defer reads.Done()
				workflowBlocksResponse, workflowBlocksErr = service.ListWorkflowBlocks(ctx, &workflowv1.ListWorkflowBlocksRequest{})
				if workflowBlocksErr != nil {
					workflowBlocksErr = fmt.Errorf("list workflow blocks: %w", workflowBlocksErr)
				}
			}()
		}
		if draftID := strings.TrimSpace(state.Request.WorkflowDraftID); draftID != "" {
			if service.GetWorkflowDraft == nil {
				workflowDraftErr = errors.New("WorkflowService.GetWorkflowDraft is not connected")
			} else {
				reads.Add(1)
				go func() {
					defer reads.Done()
					workflowDraftResponse, workflowDraftErr = service.GetWorkflowDraft(ctx, &workflowv1.GetWorkflowDraftRequest{DraftId: draftID})
					if workflowDraftErr != nil {
						workflowDraftErr = fmt.Errorf("get workflow draft: %w", workflowDraftErr)
					}
				}()
			}
		}
	}
	reads.Wait()
	if documentResponse != nil && documentResponse.GetDocument() != nil && service.ResolveDocsProjectTaskPreview != nil {
		documentProjectTasks = resolveDocsProjectTaskPreviews(ctx, documentResponse.GetMarkdown(), service.ResolveDocsProjectTaskPreview)
	}
	if documentResponse != nil && documentResponse.GetDocument() != nil && service.ResolveDocsJourneyPreview != nil {
		for _, id := range productui.DocsJourneyReferences(documentResponse.GetMarkdown()) {
			if len(documentJourneys) >= 16 {
				break
			}
			if preview, found, err := service.ResolveDocsJourneyPreview(ctx, id); err == nil && found && preview.IntentID == id {
				preview.Authorized = true
				documentJourneys = append(documentJourneys, preview)
			}
		}
	}
	if positionOptionsErr != nil {
		failures = append(failures, positionOptionsErr)
		view.PositionOptions = nil
	} else if positionObjectOptions != nil || positionOccupancyOptions != nil {
		options := []*positionv1.PositionOption{}
		if positionObjectOptions != nil {
			options = positionObjectOptions.GetOptions()
		} else {
			options = positionOccupancyOptions.GetOptions()
		}
		view.PositionOptions = make([]productui.PositionOptionProjection, 0, len(options))
		for _, option := range options {
			if option != nil {
				view.PositionOptions = append(view.PositionOptions, productui.PositionOptionProjection{
					Reference: option.GetPositionRevisionRef(), PositionID: option.GetPositionId(), Title: option.GetTitle(),
					Organization: option.GetOrganization(), JobCode: option.GetJobCode(), OrgUnit: option.GetOrgUnit(),
				})
			}
		}
	} else {
		view.PositionOptions = nil
	}
	if requirements.positionObject {
		if positionObjectErr != nil {
			failures = append(failures, positionObjectErr)
			view.PositionObject = nil
		} else {
			view.PositionObject = positionObject
		}
		view.PositionOccupancy = nil
	} else if requirements.positionOccupancy {
		if positionOccupancyErr != nil {
			failures = append(failures, positionOccupancyErr)
			view.PositionOccupancy = nil
		} else {
			view.PositionOccupancy = positionOccupancy
		}
		view.PositionObject = nil
	} else {
		view.PositionObject, view.PositionOccupancy = nil, nil
	}
	if requirements.reviewParticipants {
		if reviewParticipantsErr != nil {
			failures = append(failures, reviewParticipantsErr)
			view.ReviewParticipants = nil
		} else {
			view.ReviewParticipants = reviewParticipants
		}
	} else {
		view.ReviewParticipants = nil
	}
	if documentsErr != nil {
		failures = append(failures, documentsErr)
		view.Documents = nil
		view.DocumentsReady = false
	} else if requirements.documents && documentsResponse != nil {
		view.Documents = projectDocuments(documentsResponse.GetDocuments())
		view.DocumentsReady = true
		view.DocumentNextPageToken = documentsResponse.GetNextPageToken()
		view.DocumentTotal = int(documentsResponse.GetTotalCount())
		view.DocumentSearchModeUsed = documentsResponse.GetSearchMode()
		view.DocumentSemanticAvailable = documentsResponse.GetSemanticAvailable()
		view.DocumentSemanticPending = int(documentsResponse.GetSemanticPending())
		if view.DocumentTotal < len(view.Documents) {
			view.DocumentTotal = len(view.Documents)
		}
		view.DocumentLibrary = projectDocumentLibrary(libraryResponse)
		if documentCursorReset {
			view.DocumentPageToken = ""
		}
	}
	if documentErr != nil && (status.Code(errors.Unwrap(documentErr)) == codes.NotFound || status.Code(errors.Unwrap(documentErr)) == codes.PermissionDenied || status.Code(documentErr) == codes.NotFound) {
		// A document the reader cannot open (removed, or never shared with
		// them) is an answer, not an outage: the page says so plainly.
		view.Document = nil
		view.DocumentUnavailable = true
	} else if documentErr != nil {
		failures = append(failures, documentErr)
		view.Document = nil
	} else if documentResponse != nil {
		view.Document = projectDocument(documentResponse, documentCommentsResponse, documentCommentsErr != nil)
		view.Document.ProjectTasks = documentProjectTasks
		view.Document.Journeys = documentJourneys
	}
	directoryDenied := false
	if workersErr != nil && status.Code(errors.Unwrap(workersErr)) == codes.PermissionDenied {
		// A role without the People directory (a finance approver on My
		// Work) is refused the workforce read by design. That is an empty
		// directory for this viewer, not a failed page: it fails closed to no
		// records, and the rest of the page still loads.
		workersErr, workersResponse, directoryDenied = nil, &journeyv1.ListWorkersResponse{}, true
	}
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
	if workflowCatalogErr != nil {
		failures = append(failures, workflowCatalogErr)
		view.PublishedWorkflows, view.WorkflowView = nil, nil
	} else if requirements.workflows {
		view.PublishedWorkflows = projectWorkflowCatalog(workflowCatalogResponse.GetPublications())
		workflowID := strings.TrimSpace(state.Request.WorkflowID)
		instanceID := strings.TrimSpace(state.Request.WorkflowRunID)
		if workflowID == "" && instanceID == "" && len(view.PublishedWorkflows) > 0 {
			workflowID = view.PublishedWorkflows[0].WorkflowID
		}
		if workflowID != "" || instanceID != "" {
			if service.GetWorkflowDefinitionView == nil {
				failures = append(failures, errors.New("WorkflowService.GetWorkflowDefinitionView is not connected"))
			} else {
				response, readErr := service.GetWorkflowDefinitionView(ctx, &workflowv1.GetWorkflowDefinitionViewRequest{WorkflowId: workflowID, InstanceId: instanceID})
				if readErr != nil {
					failures = append(failures, fmt.Errorf("get workflow definition view: %w", readErr))
				} else if projected, projectionErr := projectWorkflowView(response.GetView()); projectionErr != nil {
					failures = append(failures, projectionErr)
				} else {
					view.WorkflowView = &projected
					view.SelectedWorkflowID = projected.WorkflowID
					view.SelectedWorkflowRunID = projected.InstanceID
				}
			}
		}
	}
	if workflowBlocksErr != nil {
		failures = append(failures, workflowBlocksErr)
		view.WorkflowPalette = nil
	} else if requirements.workflows {
		view.WorkflowPalette = projectWorkflowPalette(workflowBlocksResponse.GetEntries())
	}
	if workflowDraftErr != nil {
		failures = append(failures, workflowDraftErr)
		view.WorkflowDraft = nil
	} else if requirements.workflows && workflowDraftResponse != nil {
		projected, projectionErr := projectWorkflowDraft(workflowDraftResponse.GetDraft())
		if projectionErr != nil {
			failures = append(failures, projectionErr)
		} else {
			view.WorkflowDraft = &projected
			view.SelectedWorkflowDraftID = projected.DraftID
		}
	}

	if journeysErr != nil {
		failures = append(failures, journeysErr)
		// A required authorization/read refresh fails closed. Retaining an
		// older projection here could outlive a server-side access revocation.
		view.Work = nil
		view.JourneyPopulation = nil
		view.WorkflowNotifications = nil
		view.NotificationsUnavailable = true
	} else if requirements.journeys {
		var projectionErr error
		view.Work, projectionErr = projectJourneys(journeysResponse.GetJourneys())
		// UXLIVE-027: the server's summary of exactly these journeys.
		view.JourneyPopulation = projectPopulation(journeysResponse.GetPopulation())
		view.NotificationsUnavailable = journeysResponse.GetNotificationsUnavailable()
		view.WorkflowNotifications = nil
		for _, notice := range journeysResponse.GetNotifications() {
			if notice == nil {
				continue
			}
			view.WorkflowNotifications = append(view.WorkflowNotifications, productui.WorkflowNotification{
				ID: notice.GetNotificationId(), JourneyID: notice.GetJourneyId(), WorkerName: notice.GetWorkerName(),
				Purpose: notice.GetPurpose(), Status: notice.GetStatus(),
			})
		}
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
		// Journeys may name the same admitted worker by its stable worker ID
		// or entity reference while the directory uses WorkerRef. Resolve that
		// identity once before deriving conflicts or person-scoped links.
		linkJourneyWorkers(view.Work, workersResponse.GetWorkers(), session.Tenant)
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
	ownerNames := make(map[string]string, len(view.People)+1)
	for _, person := range view.People {
		if person.ID != "" && person.Name != "" {
			ownerNames[person.ID] = person.Name
		}
		if person.WorkerID != "" && person.Name != "" {
			ownerNames[person.WorkerID] = person.Name
		}
		if person.SubjectID != "" && person.Name != "" {
			ownerNames[person.SubjectID] = person.Name
		}
	}
	if view.Principal != "" && view.Viewer.Name != "" {
		ownerNames[view.Principal] = view.Viewer.Name
	}
	if view.ViewerSubject != "" && view.Viewer.Name != "" && ownerNames[view.ViewerSubject] == "" {
		ownerNames[view.ViewerSubject] = view.Viewer.Name
	}
	for i := range view.Documents {
		if name := ownerNames[view.Documents[i].OwnerID]; name != "" {
			view.Documents[i].Owner = name
		}
	}
	if view.Document != nil {
		if name := ownerNames[view.Document.Summary.OwnerID]; name != "" {
			view.Document.Summary.Owner = name
		}
		for index := range view.Document.Comments {
			if name := ownerNames[view.Document.Comments[index].AuthorID]; name != "" {
				view.Document.Comments[index].Author = name
			}
		}
	}
	if directoryDenied {
		bindViewerToRoutedWork(&view.Viewer, session, view.Work)
	}
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
			// PROMOUX-012: the badge counts actionable work only.
			view.Navigation[index].Count = len(productui.ActionableWorkItems(view.Work))
		}
	}
	view = productui.ApplyRequest(view, state.Request)
	if requirements.knowledge {
		// Search is a request-scoped projection. The renderer invokes this
		// callback with its current query; it exposes only the response fields
		// the authorized JourneyService returned for this exact route query.
		requestedQuery := strings.TrimSpace(state.Request.Query)
		response, searchErr := knowledgeResponse, knowledgeErr
		view.SearchKnowledge = func(query string) ([]productui.KnowledgeSearchResult, error) {
			if strings.TrimSpace(query) != requestedQuery || requestedQuery == "" {
				return nil, nil
			}
			if searchErr != nil {
				return nil, searchErr
			}
			if response == nil {
				return nil, errors.New("JourneyService.SearchKnowledge returned no response")
			}
			results := make([]productui.KnowledgeSearchResult, 0, len(response.GetMatches()))
			for _, match := range response.GetMatches() {
				if match == nil {
					continue
				}
				results = append(results, productui.KnowledgeSearchResult{ArticleID: match.GetArticleId(), Revision: match.GetRevision(), Locale: match.GetLocale(), Title: match.GetTitle(), Summary: match.GetSummary()})
			}
			return results, nil
		}
	}
	if view.WorkflowView != nil {
		// An absent selector deliberately resolves to the server's preferred
		// publication without rewriting the address. Preserve that resolved
		// identity after ApplyRequest applies the empty query value.
		view.SelectedWorkflowID = view.WorkflowView.WorkflowID
		view.SelectedWorkflowRunID = view.WorkflowView.InstanceID
	}
	for index := range view.Work {
		view.Work[index].Href = productui.JourneyDetailHref(view, view.Work[index].ID)
	}
	view.PersonWorkflows = projectPersonWorkflows(view, view.SelectedPerson)
	loadErr := errors.Join(failures...)
	if journeyclient.ErrorHasCode(loadErr, codes.Unauthenticated) {
		view.SignedOut = productui.UnauthenticatedRecovery()
	}
	return view, loadErr
}

// linkJourneyWorkers only rewrites a journey's person link when exactly one
// worker in this authorized response matches it. An unknown or ambiguous
// reference remains unlinked rather than attributing a request to someone
// else in the directory.
func linkJourneyWorkers(work []productui.WorkItem, workers []*journeyv1.Worker, tenant string) {
	for index := range work {
		ref := strings.TrimSpace(work[index].PersonRef)
		var match string
		for _, worker := range workers {
			if worker == nil || !journeyWorkerMatches(ref, worker, tenant) {
				continue
			}
			if match != "" {
				match = ""
				break
			}
			match = worker.GetWorkerRef()
		}
		if match != "" {
			work[index].PersonRef = match
		}
	}
}

func projectDocuments(records []*documentv1.DocumentSummary) []productui.DocumentSummary {
	projected := make([]productui.DocumentSummary, 0, len(records))
	for _, record := range records {
		if record == nil || strings.TrimSpace(record.GetDocumentId()) == "" || strings.TrimSpace(record.GetTitle()) == "" {
			continue
		}
		item := productui.DocumentSummary{
			ID: record.GetDocumentId(), Title: record.GetTitle(), Owner: record.GetOwnerId(), OwnerID: record.GetOwnerId(),
			Version: record.GetVersionId(), VersionID: record.GetVersionId(), ScopeKind: record.GetScopeKind(), ScopeID: record.GetScopeId(),
			Sharing: record.GetSharingState(), ReviewDue: documentDate(record.GetReviewDueAt()), UpdatedAt: documentTimestamp(record.GetUpdatedAt()), CanComment: record.GetCanComment(), CanEdit: record.GetCanEdit(),
			Status: productui.DocumentStatus(record.GetStatus()), CanManageAccess: record.GetCanManageAccess(),
			Starred: record.GetStarred(), FolderID: record.GetFolderId(), ReaderCount: int(record.GetReaderCount()),
			Snippet: record.GetSnippet(), Match: record.GetMatch(),
		}
		if item.ScopeKind != "" {
			item.Scope = item.ScopeKind
			if item.ScopeID != "" {
				item.Scope += ":" + item.ScopeID
			}
		}
		projected = append(projected, item)
	}
	return projected
}

func projectDocument(response *documentv1.GetDocumentResponse, commentsResponse *documentv1.ListDocumentCommentsResponse, commentsUnavailable bool) *productui.DocumentDetail {
	if response == nil || response.GetDocument() == nil {
		return nil
	}
	summaries := projectDocuments([]*documentv1.DocumentSummary{response.GetDocument()})
	if len(summaries) != 1 {
		return nil
	}
	detail := &productui.DocumentDetail{Summary: summaries[0], Markdown: response.GetMarkdown(), ContentHash: response.GetContentHash(), CanComment: summaries[0].CanComment, CanEdit: summaries[0].CanEdit, CommentsUnavailable: commentsUnavailable}
	if commentsResponse != nil {
		detail.Comments = projectDocumentComments(commentsResponse.GetComments())
	}
	detail.Chat = projectDocumentChatRefs(response)
	detail.Links = projectDocumentLinks(response.GetLinks())
	return detail
}

// resolveDocsProjectTaskPreviews resolves only canonical references that name
// both selectors. A bare task:<id> cannot be used to discover a project, and
// absent/denied/error responses stay absent so Docs renders one neutral state.
func resolveDocsProjectTaskPreviews(ctx context.Context, markdown string, resolve func(context.Context, productui.DocsProjectTaskReference) (productui.DocsProjectTaskPreview, bool, error)) []productui.DocsProjectTaskPreview {
	refs := productui.DocsProjectTaskReferences(markdown)
	if len(refs) > 64 {
		refs = refs[:64]
	}
	previews := make([]productui.DocsProjectTaskPreview, len(refs))
	var wg sync.WaitGroup
	limit := make(chan struct{}, 8)
	for index, ref := range refs {
		if ref.ProjectID == "" {
			continue
		}
		wg.Add(1)
		go func(index int, ref productui.DocsProjectTaskReference) {
			defer wg.Done()
			select {
			case limit <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-limit }()
			preview, found, err := resolve(ctx, ref)
			if err != nil || !found || preview.ProjectID != ref.ProjectID || preview.TaskID != ref.TaskID || strings.TrimSpace(preview.Title) == "" || ref.TaskID != "" && strings.TrimSpace(preview.Status) == "" {
				return
			}
			preview.Authorized = true
			previews[index] = preview
		}(index, ref)
	}
	wg.Wait()
	result := make([]productui.DocsProjectTaskPreview, 0, len(previews))
	for _, preview := range previews {
		if preview.Authorized {
			result = append(result, preview)
		}
	}
	return result
}

// projectDocumentLinks is the reader's safe view of every doc: target this
// version names (HUB-035): a target the caller cannot read carries readable
// = false and no title, exactly as the server already withheld it.
func projectDocumentLinks(records []*documentv1.DocumentLinkTarget) []productui.DocumentLinkTarget {
	links := make([]productui.DocumentLinkTarget, 0, len(records))
	for _, record := range records {
		if record == nil || strings.TrimSpace(record.GetDocumentId()) == "" {
			continue
		}
		links = append(links, productui.DocumentLinkTarget{DocumentID: record.GetDocumentId(), Title: record.GetTitle(), Readable: record.GetReadable()})
	}
	return links
}

func projectDocumentComments(records []*documentv1.DocumentComment) []productui.DocumentComment {
	comments := make([]productui.DocumentComment, 0, len(records))
	for _, record := range records {
		if record == nil || strings.TrimSpace(record.GetId()) == "" {
			continue
		}
		comment := productui.DocumentComment{ID: record.GetId(), AuthorID: record.GetAuthorId(), Body: record.GetBody(), CreatedAt: documentCommentTimestamp(record.GetCreatedAt()), VersionID: record.GetVersionId(),
			ParentID: record.GetParentCommentId(), Resolved: record.GetResolved(), Orphaned: record.GetAnchorOrphaned(), Start: -1}
		if anchor := record.GetAnchor(); anchor != nil && anchor.GetQuote() != "" {
			comment.Quote, comment.Prefix, comment.Suffix = anchor.GetQuote(), anchor.GetPrefix(), anchor.GetSuffix()
			if !comment.Orphaned {
				comment.Start = int(anchor.GetStart())
			}
		}
		comments = append(comments, comment)
	}
	return comments
}

func documentCommentTimestamp(value *timestamppb.Timestamp) string {
	if value == nil || !value.IsValid() {
		return ""
	}
	return value.AsTime().UTC().Format(time.RFC3339)
}

func documentTimestamp(value *timestamppb.Timestamp) string {
	if value == nil || !value.IsValid() {
		return ""
	}
	return value.AsTime().UTC().Format(time.RFC3339)
}

// documentDate is a calendar date for fields read as a day (review due).
func documentDate(value *timestamppb.Timestamp) string {
	if value == nil || !value.IsValid() {
		return ""
	}
	return value.AsTime().UTC().Format("2006-01-02")
}

func journeyWorkerMatches(ref string, worker *journeyv1.Worker, tenant string) bool {
	if worker == nil || ref == "" {
		return false
	}
	if ref == strings.TrimSpace(worker.GetWorkerRef()) || ref == strings.TrimSpace(worker.GetWorkerId()) {
		return true
	}
	var entity values.EntityRef
	return entity.UnmarshalText([]byte(ref)) == nil && entity.Tenant.String() == tenant && entity.Kind == values.Kind("worker") && entity.Id == strings.TrimSpace(worker.GetWorkerId())
}

func baselineMatchesSession(baseline, current productui.View) bool {
	return baseline.Tenant == current.Tenant && baseline.Principal == current.Principal && baseline.Scope == current.Scope &&
		slices.Equal(baseline.Roles, current.Roles) && slices.Equal(baseline.EffectivePermissions, current.EffectivePermissions)
}

func seedBaselineProjection(view *productui.View, baseline productui.View) {
	view.PositionOptions = nil
	if baseline.Page == view.Page && baseline.PositionReference == view.PositionReference {
		view.PositionOptions = append([]productui.PositionOptionProjection(nil), baseline.PositionOptions...)
	}
	view.Work = baseline.Work
	view.JourneyPopulation = baseline.JourneyPopulation
	view.WorkflowNotifications = baseline.WorkflowNotifications
	view.NotificationsUnavailable = baseline.NotificationsUnavailable
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
	view.PublishedWorkflows = baseline.PublishedWorkflows
	view.WorkflowView = baseline.WorkflowView
	view.SelectedWorkflowID = baseline.SelectedWorkflowID
	view.SelectedWorkflowRunID = baseline.SelectedWorkflowRunID
	view.WorkflowDraft = baseline.WorkflowDraft
	view.SelectedWorkflowDraftID = baseline.SelectedWorkflowDraftID
	if baseline.PositionObject != nil {
		copy := *baseline.PositionObject
		view.PositionObject = &copy
	}
	if baseline.PositionOccupancy != nil {
		copy := *baseline.PositionOccupancy
		copy.Occupants = append([]productui.PositionOccupantProjection(nil), baseline.PositionOccupancy.Occupants...)
		view.PositionOccupancy = &copy
	}
	if baseline.ReviewParticipants != nil {
		copy := *baseline.ReviewParticipants
		copy.Cycles = append([]productui.ReviewParticipantsCycleProjection(nil), baseline.ReviewParticipants.Cycles...)
		for index := range copy.Cycles {
			copy.Cycles[index].Assignments = append([]productui.ReviewParticipantAssignmentProjection(nil), baseline.ReviewParticipants.Cycles[index].Assignments...)
		}
		view.ReviewParticipants = &copy
	}
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
	view.RoleFeaturePermissions = make([]productui.RoleFeaturePermission, 0, len(response.GetFeaturePermissions()))
	for _, permission := range response.GetFeaturePermissions() {
		view.RoleFeaturePermissions = append(view.RoleFeaturePermissions, productui.RoleFeaturePermission{
			Version: permission.GetVersion(), RoleID: permission.GetRoleId(), Page: productui.PageID(permission.GetPageId()), Feature: productui.FeatureID(permission.GetFeatureId()),
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
	name := displayLabel(withoutPersonaPrefix(session.Principal))
	fallback := productui.ViewerProfile{Name: name, Initials: uicomponents.Initials(name)}
	principal := normalizedIdentity(session.Principal)
	for _, person := range people {
		// Only stable worker identifiers may bind the account to a self-service
		// profile. A display name is mutable and non-unique, so it can never be
		// used as an identity join.
		if principal != "" && (normalizedIdentity(person.ID) == principal || normalizedIdentity(person.WorkerID) == principal || normalizedIdentity(person.SubjectID) == principal) {
			return viewerProfileFromPerson(person)
		}
	}
	return fallback
}

// bindViewerToRoutedWork binds a viewer the directory could not resolve --
// because their role may not read it -- to the worker identity the server
// routed work to. The join is the server's assignee principal against the
// authenticated principal, both stable identifiers, never a display name.
// An account no work is routed to (an unbound operator) stays unbound, so
// UXAUDIT-017's "unbound viewers see no assignments" still holds; without
// this a finance approver's own review never reached their My Work.
func bindViewerToRoutedWork(viewer *productui.ViewerProfile, session Session, work []productui.WorkItem) {
	principal := normalizedIdentity(session.Principal)
	if viewer == nil || viewer.PersonID != "" || principal == "" {
		return
	}
	for _, item := range work {
		if normalizedIdentity(item.AssigneeRef) == principal {
			viewer.PersonID = item.AssigneeRef
			return
		}
	}
}

// withoutPersonaPrefix drops a "hc-050-" style directory prefix (letters, then
// a number) from a subject so an unbound viewer is named "Rafael Torres", not
// "Hc 050 Rafael Torres" with the initials "HT".
func withoutPersonaPrefix(subject string) string {
	parts := strings.SplitN(strings.TrimSpace(subject), "-", 3)
	if len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != "" &&
		strings.Trim(parts[1], "0123456789") == "" && strings.Trim(strings.ToLower(parts[0]), "abcdefghijklmnopqrstuvwxyz") == "" {
		return parts[2]
	}
	return subject
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
		stored := productui.StoredUserPreferences{Version: user.GetVersion(), Locale: user.GetLocale(), Accessibility: view.Accessibility, NavCollapsed: user.GetNavCollapsed(), NavigationGroups: view.NavigationGroupOpen, WorkflowUses: user.GetWorkflowUses(), Tables: map[string]productui.StoredTablePreferences{},
			Density: productui.NormalizePersonalDensity(user.GetDensity())}
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
		request.Locale = productui.ResolveProductLocalePreference(request.Locale, user.GetLocale()).Resolved
		if !state.Provided["nav"] {
			request.NavCollapsed = user.GetNavCollapsed()
		}
		if !state.Provided["favorites"] {
			request.FavoritePages = parseFavoritePages(strings.Join(user.GetFavoritePages(), ","))
		}
		applyTableDefaults(request, state.Provided, user.GetTables()["people"], false)
		applyTableDefaults(request, state.Provided, user.GetTables()["history"], true)
		// UXLIVE-027: the saved My Work tab is My Work's own default. Applied on
		// every route it narrowed Home, Insights and History to that tab (a
		// saved "tracked" left Rafael's Home and Insights at zero beside nine
		// visible journeys), so it is adopted only where the tab exists.
		if routeProfile, _, ok := productui.PageProfiles(state.Page); ok && routeProfile.StateProfile().Work {
			applyWorkTableDefaults(request, state.Provided, user.GetTables()["work"])
		}
	}
	if theme := response.GetTheme(); theme != nil {
		view.AppearanceVersion = theme.GetVersion()
		view.Appearance = productui.NormalizeCustomerTheme(productui.CustomerTheme{BrandName: theme.GetBrandName(), BrandMark: theme.GetBrandMark(), BrandLogoURL: theme.GetBrandLogoUrl(), ColorMode: theme.GetColorMode(), Palette: theme.GetPalette(), Shape: theme.GetShape(), Density: theme.GetDensity(), Glyphs: theme.GetGlyphs(), Typeface: theme.GetTypeface(), Navigation: theme.GetNavigation(), Motion: theme.GetMotion(), TokenOverrides: theme.GetTokenOverrides(), DarkTokenOverrides: theme.GetDarkTokenOverrides()})
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
	if !provided["columns"] {
		request.PeopleColumns = table.GetFilters()["columns"]
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
		// PROMOUX-012: one stage vocabulary (journeyclient.StagePresentation)
		// and the server's own closure, shared with the Journeys tracker.
		status, tone := journeyclient.StagePresentation(journey.GetStage())
		terminal := journeyclient.JourneyClosed(journey)
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
		// UXAUDIT-017 / PROMOUX-012: the server-resolved next transition the
		// Journeys tracker renders too, so the two never disagree.
		dimension := journeyclient.JourneyStatusDimension(journey)
		items = append(items, productui.WorkItem{
			NextStep: string(dimension.NextStep), WaitingOn: string(dimension.WaitingOn), AwaitsPerson: dimension.AwaitsPerson,
			ID: journey.GetIntentId(), Initials: uicomponents.Initials(journey.GetWorkerName()), Title: "Promotion journey", TitleKey: "journey.detail_title",
			Person: journey.GetWorkerName(), PersonRef: journey.GetWorkerRef(), Summary: summary, Status: status, StatusKey: journeyStageKey(journey.GetStage()), Tone: tone, Terminal: terminal,
			Due: journey.GetEffectiveDate(), EffectiveDate: journey.GetEffectiveDate(), CompletedAt: timestampLabel(journey.GetUpdatedAt()),
			InstanceID: journey.GetInstanceId(), InstanceVersion: journey.GetInstanceVersion(), MaterialDigest: journey.GetMaterialDigest(),
			CurrentBase: currentBase, ProposedBase: proposedBase,
			ViewerRelationships:  journeyclient.JourneyViewerRelationships(journey),
			ViewerResponsibility: journeyclient.JourneyViewerResponsibility(journey),
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
	workersByRef := make(map[string]int, len(workers))
	for _, worker := range workers {
		if worker == nil {
			continue
		}
		name := workerDisplayName(worker)
		namesByRef[worker.GetWorkerRef()] = name
		namesByRef[worker.GetWorkerId()] = name
		workerIDByRef[worker.GetWorkerRef()] = worker.GetWorkerId()
		workerIDByRef[worker.GetWorkerId()] = worker.GetWorkerId()
		workersByRef[worker.GetWorkerRef()]++
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
		basePay, err := moneyFromWire(worker.GetBasePay(), worker.GetCurrency())
		if err != nil {
			failures = append(failures, fmt.Errorf("project worker %s base pay: %w", worker.GetWorkerRef(), err))
		}
		managerState, managerRef, managerName, relationshipErr := projectManagerRelationship(worker, workersByRef, namesByRef)
		if relationshipErr != nil {
			failures = append(failures, relationshipErr)
		}
		people = append(people, productui.Person{
			ID: worker.GetWorkerRef(), WorkerID: worker.GetWorkerId(), SubjectID: worker.GetSubjectId(), Initials: uicomponents.Initials(name), PhotoURL: photoURL, Name: name,
			LegalName: worker.GetLegalName(), PreferredName: worker.GetPreferredName(), Role: role,
			Team: orgUnitLabel(worker.GetOrgUnit()), Manager: managerName, ManagerID: workerIDByRef[managerRef], ManagerRelationship: managerState, ManagerWorkerRef: managerRef, Location: worker.GetLocation(), WorkerNumber: worker.GetWorkerNumber(),
			JobCode: worker.GetJobCode(), Grade: worker.GetGrade(), PositionID: worker.GetPositionId(),
			PayZone: worker.GetPayZone(), BasePay: basePay,
			EmploymentType: worker.GetEmploymentType(), TimeType: worker.GetTimeType(),
			Company: worker.GetCompany(), BusinessUnit: worker.GetBusinessUnit(),
			CostCenter: worker.GetCostCenter(), WorkArrangement: worker.GetWorkArrangement(),
			PayBasis:    worker.GetPayBasis(),
			BonusTarget: worker.GetBonusTarget(), HireDate: worker.GetHireDate(), Source: worker.GetSource(),
			LifecycleStatus: worker.GetLifecycleStatus(), WorkerType: worker.GetWorkerType(),
			CreatedAt: timestampLabel(worker.GetCreatedAt()),
		})
	}
	productui.IndexPeople(people)
	return people, errors.Join(failures...)
}

func projectManagerRelationship(worker *journeyv1.Worker, workersByRef map[string]int, namesByRef map[string]string) (productui.OrganizationRelationshipState, string, string, error) {
	if worker == nil || worker.GetManagerRelationship() == nil {
		return "", "", "", fmt.Errorf("project worker manager relationship: missing authorized projection")
	}
	relationship := worker.GetManagerRelationship()
	managerRef := strings.TrimSpace(relationship.GetManagerWorkerRef())
	switch relationship.GetDisposition() {
	case journeyv1.ManagerRelationshipProjection_DISPOSITION_ROOT:
		if managerRef != "" {
			return productui.OrganizationRelationshipRoot, "", "", fmt.Errorf("project worker %s manager relationship: ROOT includes an endpoint", worker.GetWorkerRef())
		}
		return productui.OrganizationRelationshipRoot, "", "", nil
	case journeyv1.ManagerRelationshipProjection_DISPOSITION_VISIBLE:
		if managerRef == "" || workersByRef[managerRef] != 1 || managerRef == worker.GetWorkerRef() {
			return productui.OrganizationRelationshipOrphan, "", "", fmt.Errorf("project worker %s manager relationship: VISIBLE endpoint is absent, ambiguous, self, or not admitted", worker.GetWorkerRef())
		}
		return productui.OrganizationRelationshipVisible, managerRef, namesByRef[managerRef], nil
	case journeyv1.ManagerRelationshipProjection_DISPOSITION_WITHHELD:
		if managerRef != "" {
			return productui.OrganizationRelationshipWithheld, "", "", fmt.Errorf("project worker %s manager relationship: WITHHELD leaks an endpoint", worker.GetWorkerRef())
		}
		return productui.OrganizationRelationshipWithheld, "", "", nil
	case journeyv1.ManagerRelationshipProjection_DISPOSITION_ORPHAN:
		if managerRef != "" {
			return productui.OrganizationRelationshipOrphan, "", "", fmt.Errorf("project worker %s manager relationship: ORPHAN includes an endpoint", worker.GetWorkerRef())
		}
		return productui.OrganizationRelationshipOrphan, "", "", nil
	default:
		return "", "", "", fmt.Errorf("project worker %s manager relationship: disposition is unspecified", worker.GetWorkerRef())
	}
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
		ID: "promotion", Name: view.Locale.Text("workflow.promotion_name"), Category: view.Locale.Text("workflow.promotion_category"),
		Description: view.Locale.Text("workflow.promotion_description"),
		Href:        href, UseCount: view.WorkflowUses["promotion"],
		LaunchHref: func(person string) string { return productui.JourneyProposalHref(view, person) },
	}}
}

// journeyStageKey carries the service's typed stage across the locale boundary
// without altering Status, which remains the adapter's fallback and is used
// by work-bucket selection before display.
func journeyStageKey(stage journeyv1.JourneyStage) string {
	switch stage {
	case journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED:
		return "journey.stage_proposed"
	case journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED:
		return "journey.stage_blocked"
	case journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL:
		return "journey.stage_awaiting_approval"
	case journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL:
		return "journey.stage_finance_approval"
	case journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL:
		return "journey.stage_manager_approval"
	case journeyv1.JourneyStage_JOURNEY_STAGE_REAPPROVAL:
		return "journey.stage_reapproval"
	case journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE:
		return "journey.stage_waiting_effective"
	case journeyv1.JourneyStage_JOURNEY_STAGE_REVALIDATION:
		return "journey.stage_revalidation"
	case journeyv1.JourneyStage_JOURNEY_STAGE_EXECUTED:
		return "journey.stage_recording"
	case journeyv1.JourneyStage_JOURNEY_STAGE_OBSERVING_EFFECTS:
		return "journey.stage_observing_effects"
	case journeyv1.JourneyStage_JOURNEY_STAGE_REPAIR_REQUIRED:
		return "journey.stage_repair_required"
	case journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_ACKNOWLEDGEMENT:
		return "journey.stage_awaiting_acknowledgement"
	case journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED:
		return "journey.stage_completed"
	case journeyv1.JourneyStage_JOURNEY_STAGE_RECORDED:
		return "journey.stage_recorded"
	case journeyv1.JourneyStage_JOURNEY_STAGE_REJECTED:
		return "journey.stage_rejected"
	case journeyv1.JourneyStage_JOURNEY_STAGE_FAILED:
		return "journey.stage_failed"
	default:
		return "journey.stage_unknown"
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

// documentListRequest maps the docs route onto one list selection. "starred"
// is a collection in the address but a filter on the wire.
func documentListRequest(request productui.PageRequest) *documentv1.ListDocumentsRequest {
	collection := request.DocumentCollection
	starred := collection == "starred"
	if starred {
		collection = "all"
	}
	return &documentv1.ListDocumentsRequest{
		Query: request.DocumentQuery, Collection: collection, PageToken: request.DocumentPageToken,
		FolderId: request.DocumentFolder, StarredOnly: starred, Sort: request.DocumentSort, OwnerId: request.DocumentOwner,
		SearchMode: request.DocumentSearchMode, Page: int32(max(request.DocumentPage, 1)), PageSize: int32(productui.NormalizeDocumentPerPage(request.DocumentPerPage)),
	}
}

func projectDocumentLibrary(response *documentv1.GetDocumentLibraryResponse) *productui.DocumentLibrary {
	if response == nil {
		return nil
	}
	library := &productui.DocumentLibrary{All: int(response.GetAllCount()), Mine: int(response.GetMineCount()), Shared: int(response.GetSharedCount()), Starred: int(response.GetStarredCount())}
	for _, folder := range response.GetFolders() {
		if strings.TrimSpace(folder.GetFolderId()) == "" || strings.TrimSpace(folder.GetName()) == "" {
			continue
		}
		library.Folders = append(library.Folders, productui.DocumentFolder{ID: folder.GetFolderId(), Name: folder.GetName(), Count: int(folder.GetDocumentCount())})
	}
	return library
}
