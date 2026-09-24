//go:build js && wasm

package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/router"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	positionv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/position/v1"
	reviewparticipantsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/reviewparticipants/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/taskmux"
	"google.golang.org/grpc"
)

// knowledgeSearchService is the optional knowledge facet consumed by this
// product entrypoint. It stays beside the consumer so journeyclient remains
// focused on the workflow client seam.
type knowledgeSearchService interface {
	SearchKnowledge(context.Context, *journeyv1.SearchKnowledgeRequest) (*journeyv1.SearchKnowledgeResponse, error)
}

const (
	productPathPrefix = "/workspace/app/"
	// productFailureKey marks route data whose read failed on a page covered
	// by the async-region failure contract (REV-090-01).
	productFailureKey = "product-failure"
	// productRetryFocusSelector is where focus goes after a Retry settles.
	productRetryFocusSelector = ".async-region-retry, " + productPageFocusSelector
)

const productViewKey = "product-view"
const productJourneyStoreKey = "product-journey-store"
const productSourceHrefKey = "product-source-href"
const productResolvedHrefKey = "product-resolved-href"

var lastFocusedProductRoute string
var productNavigationGroups *browserNavigationGroupController
var productTransientPopovers *browserTransientPopoverController
var lastResolvedProductView *productui.View

// lastProductRouteFailed is true while the last route read failed.
var lastProductRouteFailed bool
var activeProductLayoutView productui.View
var activeProductLayoutShowHeading = true

func isProductPath(path string) bool {
	// A path with surrounding whitespace is not a canonical product address;
	// it is left to document routing rather than trimmed into one.
	return path == strings.TrimSpace(path) && strings.HasPrefix(path, productPathPrefix)
}

func startProduct(ctx context.Context, cfg journeyclient.Config, service journeyclient.Service, conn grpc.ClientConnInterface) error {
	configureChatBrowser(conn, cfg)
	configureChatRetention(conn)
	// Finite RPC work shares a bounded lane. WatchJourney subscriptions stay
	// outside it, so an open detail page cannot reduce navigation/click
	// capacity. Four slots allow the independent projection reads and a user
	// action to overlap without permitting an unbounded goroutine burst.
	frontendTasks := taskmux.New(taskmux.Options{MaxRunning: 4, MaxQueued: 64, PriorityBurst: 8})
	liveService := productclient.Service{
		ListJourneys: func(ctx context.Context, request *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return service.ListJourneys(ctx, request)
		},
		ListWorkers: func(ctx context.Context, request *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return service.ListWorkers(ctx, request)
		},
	}
	// Docs uses the same authenticated gRPC connection as the rest of the
	// workspace. A disabled document service reports UNAVAILABLE server-side.
	documentService := documentv1.NewDocumentServiceClient(conn)
	positionService := positionv1.NewPositionServiceClient(conn)
	liveService.ListPositionObjectOptions = func(ctx context.Context, request *positionv1.ListPositionObjectOptionsRequest) (*positionv1.ListPositionObjectOptionsResponse, error) {
		return positionService.ListPositionObjectOptions(chatRPCContext(ctx, cfg), request)
	}
	liveService.ListPositionOccupancyOptions = func(ctx context.Context, request *positionv1.ListPositionOccupancyOptionsRequest) (*positionv1.ListPositionOccupancyOptionsResponse, error) {
		return positionService.ListPositionOccupancyOptions(chatRPCContext(ctx, cfg), request)
	}
	reviewParticipantsService := reviewparticipantsv1.NewReviewParticipantsServiceClient(conn)
	liveService.GetReviewParticipants = func(ctx context.Context) (*productui.ReviewParticipantsProjection, error) {
		response, err := reviewParticipantsService.GetReviewParticipants(chatRPCContext(ctx, cfg), &reviewparticipantsv1.GetReviewParticipantsRequest{})
		if err != nil {
			return nil, err
		}
		projection := &productui.ReviewParticipantsProjection{Cycles: make([]productui.ReviewParticipantsCycleProjection, 0, len(response.GetCycles()))}
		for _, cycle := range response.GetCycles() {
			if cycle == nil {
				continue
			}
			item := productui.ReviewParticipantsCycleProjection{
				CycleID: cycle.GetCycleId(), CycleRevision: cycle.GetCycleRevision(),
				GraphRevision: cycle.GetGraphRevision(), GraphDigest: cycle.GetGraphDigest(),
				Assignments: make([]productui.ReviewParticipantAssignmentProjection, 0, len(cycle.GetAssignments())),
			}
			for _, assignment := range cycle.GetAssignments() {
				if assignment != nil {
					item.Assignments = append(item.Assignments, productui.ReviewParticipantAssignmentProjection{
						ParticipantID: assignment.GetParticipantId(), ReviewerID: assignment.GetReviewerId(), Relationship: assignment.GetRelationship(),
					})
				}
			}
			projection.Cycles = append(projection.Cycles, item)
		}
		return projection, nil
	}
	liveService.GetPositionObject = func(ctx context.Context, ref string) (*productui.PositionObjectProjection, error) {
		response, err := positionService.GetPositionObject(chatRPCContext(ctx, cfg), &positionv1.GetPositionObjectRequest{PositionRevisionRef: ref})
		if err != nil {
			return nil, err
		}
		if !response.GetExists() {
			return nil, nil
		}
		return &productui.PositionObjectProjection{
			PositionID: response.GetPositionId(), Revision: response.GetRevision(), JobCode: response.GetJobCode(),
			OrgUnit: response.GetOrgUnit(), Lifecycle: response.GetLifecycle(), Compatible: response.GetCompatible(),
		}, nil
	}
	liveService.GetPositionOccupancy = func(ctx context.Context, ref string) (*productui.PositionOccupancyProjection, error) {
		response, err := positionService.GetPositionOccupancy(chatRPCContext(ctx, cfg), &positionv1.GetPositionOccupancyRequest{PositionRevisionRef: ref})
		if err != nil {
			return nil, err
		}
		if !response.GetExists() {
			return nil, nil
		}
		projection := &productui.PositionOccupancyProjection{
			PositionID: response.GetPositionId(), CapacityFTE: response.GetCapacityFte(), CapacityHeads: response.GetCapacityHeads(),
			ConsumedFTE: response.GetConsumedFte(), ConsumedHeads: response.GetConsumedHeads(),
			AvailableFTE: response.GetAvailableFte(), AvailableHeads: response.GetAvailableHeads(),
			Occupants: make([]productui.PositionOccupantProjection, 0, len(response.GetOccupants())),
		}
		for _, occupant := range response.GetOccupants() {
			if occupant != nil {
				projection.Occupants = append(projection.Occupants, productui.PositionOccupantProjection{WorkerID: occupant.GetWorkerId(), FTE: occupant.GetFte()})
			}
		}
		return projection, nil
	}
	createDocument := documentService.CreateDocument
	liveService.ListDocuments = func(ctx context.Context, request *documentv1.ListDocumentsRequest) (*documentv1.ListDocumentsResponse, error) {
		return documentService.ListDocuments(chatRPCContext(ctx, cfg), request)
	}
	liveService.GetDocument = func(ctx context.Context, request *documentv1.GetDocumentRequest) (*documentv1.GetDocumentResponse, error) {
		return documentService.GetDocument(chatRPCContext(ctx, cfg), request)
	}
	liveService.ListDocumentComments = func(ctx context.Context, request *documentv1.ListDocumentCommentsRequest) (*documentv1.ListDocumentCommentsResponse, error) {
		return documentService.ListDocumentComments(chatRPCContext(ctx, cfg), request)
	}
	liveService.ShareDocument = func(ctx context.Context, request *documentv1.ShareDocumentRequest) (*documentv1.ShareDocumentResponse, error) {
		return documentService.ShareDocument(chatRPCContext(ctx, cfg), request)
	}
	liveService.GetDocumentLibrary = func(ctx context.Context, request *documentv1.GetDocumentLibraryRequest) (*documentv1.GetDocumentLibraryResponse, error) {
		return documentService.GetDocumentLibrary(chatRPCContext(ctx, cfg), request)
	}
	if preferenceService, ok := service.(journeyclient.PreferenceService); ok {
		liveService.GetPreferences = preferenceService.GetProductPreferences
		liveService.GetWorkerIDPolicy = preferenceService.GetWorkerIDPolicy
		liveService.GetRoleAccess = preferenceService.GetRoleAccess
	}
	if knowledgeService, ok := service.(knowledgeSearchService); ok {
		liveService.SearchKnowledge = knowledgeService.SearchKnowledge
	}
	if workflowService, ok := service.(journeyclient.WorkflowViewerService); ok {
		liveService.ListWorkflowPublications = workflowService.ListWorkflowPublications
		liveService.GetWorkflowDefinitionView = workflowService.GetWorkflowDefinitionView
		liveService.ListWorkflowBlocks = workflowService.ListWorkflowBlocks
		liveService.CreateWorkflowDraft = workflowService.CreateWorkflowDraft
		liveService.GetWorkflowDraft = workflowService.GetWorkflowDraft
		liveService.InsertWorkflowPaletteEntry = workflowService.InsertWorkflowPaletteEntry
		liveService.UpdateWorkflowDraftNode = workflowService.UpdateWorkflowDraftNode
		liveService.SetWorkflowDraftOutcome = workflowService.SetWorkflowDraftOutcome
		liveService.BindWorkflowDraftInput = workflowService.BindWorkflowDraftInput
		liveService.MoveWorkflowDraftNode = workflowService.MoveWorkflowDraftNode
		liveService.RemoveWorkflowDraftNode = workflowService.RemoveWorkflowDraftNode
		liveService.ClearWorkflowDraftOutcome = workflowService.ClearWorkflowDraftOutcome
		liveService.RenameWorkflowDraft = workflowService.RenameWorkflowDraft
		liveService.NavigateWorkflowDraftHistory = workflowService.NavigateWorkflowDraftHistory
		liveService.ApplyWorkflowTemplateOverlay = workflowService.ApplyWorkflowTemplateOverlay
	}
	pagePermissions := make([]productui.RolePagePermission, 0, len(cfg.PagePermissions))
	for _, permission := range cfg.PagePermissions {
		pagePermissions = append(pagePermissions, productui.RolePagePermission{
			Version: permission.Version, RoleID: permission.RoleID, Page: productui.PageID(permission.PageID),
			View: permission.View, Create: permission.Create, Update: permission.Update, Delete: permission.Delete,
		})
	}
	var featurePermissions []productui.RoleFeaturePermission
	if cfg.FeaturePermissions != nil {
		featurePermissions = make([]productui.RoleFeaturePermission, 0, len(cfg.FeaturePermissions))
		for _, permission := range cfg.FeaturePermissions {
			featurePermissions = append(featurePermissions, productui.RoleFeaturePermission{
				Version: permission.Version, RoleID: permission.RoleID, Page: productui.PageID(permission.PageID), Feature: productui.FeatureID(permission.FeatureID),
				View: permission.View, Create: permission.Create, Update: permission.Update, Delete: permission.Delete,
			})
		}
	}
	launcherActions := projectLauncherActions(cfg.LauncherActions)
	// UXAUDIT-007: cfg.Purpose is the admitted principal's authorized
	// data-processing purpose, not the product shell's "authorized scope"
	// -- do not carry it into Session.Scope, which LoadingView renders as a
	// persistent, page-independent header badge on every product route.
	// cfg still reaches journeyApp below unchanged, which is what actually
	// surfaces the purpose where GREEN requires: inside the Promotion
	// journey experience itself (tools/uxqual/render/journey's
	// principalChip), with a visually-hidden "Purpose: " explanation and
	// its own sign-out exit action, mounted only on PageJourneys.
	session := productclient.Session{Tenant: cfg.Tenant, Principal: cfg.Subject, Roles: cfg.Roles, Permissions: pagePermissions, FeaturePermissions: featurePermissions, LauncherActions: launcherActions, EnforceRoleVisibility: true, LogoutHref: cfg.LogoutPath}
	preferences := newServerPreferenceController(ctx, service)
	workflowAuthoring := newWorkflowAuthoringController(ctx, liveService)
	// No Apply here. Both controllers start from defaults, and the document
	// the server sent already carries the stored theme and preferences on
	// <html>; applying the defaults over them is what made a light workspace
	// flash dark on a dark device. They take over at the first Load.
	appearance := newBrowserThemeController(productui.DisplayLabel(cfg.Tenant), preferences.SaveTheme)
	accessibility := newBrowserAccessibilityController(preferences.SaveAccessibility)
	productNavigationGroups = newBrowserNavigationGroupController(preferences.SaveNavigationGroups)
	productNavigationGroups.Bind()
	productTransientPopovers = newBrowserTransientPopoverController()
	productTransientPopovers.Bind()
	// Normalize the cold address before GWC constructs its first loader key.
	// This keeps unknown, wrong-page, and action/credential-shaped parameters
	// out of both the visible address and the router's internal loader cache.
	// A copied embedded journey link still uses its standalone fragment href.
	// Resolve it before the history router forms the cold loader key.
	if href, ok := productclient.ProductJourneyHashHref(currentPath(), currentHash(), currentQuery()); ok {
		browserReplaceURL(href)
	}
	canonicalizeCurrentProductLocation()
	productRouter := router.NewHistoryRouter(router.RouterOptions{DefaultRoute: productui.Path(productui.PageHome)})
	// Product routing has page-aware focus continuity: collection controls keep
	// focus for local filter/sort/paging changes, shell toggles keep their own
	// focus, and only a true destination change focuses the page heading. Turn
	// off GWC's generic container focus so loading and resolved renders cannot
	// move focus a second time.
	productRouter.SetFocusManagement(false)
	// REV-090-01: a failed region's Retry re-runs the same route read.
	productRouteRetry = productRouter.Revalidate
	// The application shell is a persistent layout route. Leaf routes own only
	// the outlet below /workspace/app, so navigation cannot temporarily unmount
	// the header, sidebar, or their local interaction state.
	productRouter.Register("/workspace/app", productShellLayoutComponent, router.Options{Layout: true})
	productHistory = newBrowserProductHistoryController()
	productScroll = newBrowserProductScrollController()
	productScroll.Bind()
	// Server-backed search-as-you-type reruns the route loader in place:
	// NavigateReplace reads the next result set without adding a history
	// entry per pause, and any deliberate navigation cancels a pending one.
	// A search that starts a typing session is a new history step; later
	// keystrokes in the session update that step in place.
	searchPush := false
	searchDebounce := newNavigationDebouncerWithScheduler(func(href string) {
		if searchPush {
			searchPush = false
			productScroll.BeginSoftwareNavigation()
			productHistory.Navigate(productRouter.Navigate, href)
			return
		}
		productHistory.Replace(productRouter.NavigateReplace, href)
	}, browserDebounceScheduler)
	scheduleSearch := func(href string, push bool) {
		if push {
			searchPush = true
		}
		searchDebounce.Schedule(href)
	}
	docsReplaceRoute = func(href string) { productHistory.Replace(productRouter.NavigateReplace, href) }
	navigateProduct := func(href string) {
		searchDebounce.Cancel()
		searchPush = false
		productScroll.BeginSoftwareNavigation()
		productHistory.Navigate(productRouter.Navigate, href)
	}
	// replaceProduct is navigateProduct without a new history step.
	replaceProduct := func(href string) {
		searchDebounce.Cancel()
		searchPush = false
		productHistory.Replace(productRouter.NavigateReplace, href)
	}
	bindProductLinkFallback(navigateProduct)
	// Menu filtering is local component state. Debounce only its shareable URL
	// state so typing never reruns page loaders or refetches workforce data.
	navigationDebounce := newNavigationDebouncerWithScheduler(browserReplaceURL, browserDebounceScheduler)
	journeyStore := journey.NewStore(journey.Page{})
	peopleColumnDraft := &productui.ColumnChooserDraft{}
	bindActionableNoticeFocus(journeyStore)
	journeyApp := journeyclient.New(cfg, service, journeyStore, time.Now)
	journeyApp.FocusField = focusJourneyField
	journeyApp.Tasks = frontendTasks
	journeyApp.Locate = func(fragment string) {
		navigateProduct(productclient.ProductJourneyHref(fragment, currentQuery()))
	}
	journeyApp.NavigateProduct = navigateProduct
	journeys := &productJourneyBridge{ctx: ctx, app: journeyApp}
	canViewPage := func(page productui.PageID) bool {
		if len(cfg.PagePermissions) > 0 {
			return cfg.CanPageAction(string(page), "view")
		}
		return productui.PageVisible(page, cfg.Roles)
	}
	for _, definition := range productui.PageDefinitions() {
		// A viewer with My Work but not Journeys (a finance approver) reaches
		// one journey's detail from their queue and nothing else; the server
		// shell admits the same route (workspace.assignedJourneyDetail).
		detailOnly := false
		if !canViewPage(definition.ID) {
			if definition.ID != productui.PageJourneys || !canViewPage(productui.PageWork) {
				continue
			}
			detailOnly = true
		}
		definition := definition
		productRouter.Register(definition.Route, productRouteComponent, router.Options{
			Title: definition.Title + " · Human Capital Management Suite",
			Loader: func(loadCtx context.Context, routeContext router.RouteContext) (router.Attrs, error) {
				if detailOnly && strings.TrimSpace(routeContext.Query.Get("journey")) == "" {
					return nil, errors.New(productui.ResolveProductLocale(routeContext.Query.Get("locale")).Text("journey.error_denied_detail"))
				}
				navigationDebounce.Cancel()
				persistPresentation := productHistory != nil && productHistory.ClaimSoftwareNavigation(routeContext.Path, routeContext.Query.Encode())
				// A route change discards an unsaved preview and reapplies the last
				// customer selection before the next component tree is mounted.
				appearance.Reapply()
				accessibility.Reapply()
				// GWC v5 binds RouteContext to the loader's route generation. Reading
				// location.search here would race a later push/pop navigation and let
				// the older generation issue reads for the newer address.
				state, parseErr := productclient.ParseState(routeContext.Path, routeContext.Query.Encode())
				if parseErr != nil {
					if loadCtx.Err() == nil && browserRouteGenerationMatches(routeContext.Path, routeContext.Query.Encode()) {
						browserReplaceURL(routeContext.Path)
					}
					return nil, parseErr
				}
				canonicalHref := productclient.CanonicalHref(state)
				currentHref := currentProductHref()
				if loadCtx.Err() == nil && browserRouteGenerationMatches(routeContext.Path, routeContext.Query.Encode()) && canonicalHref != currentHref {
					// Preserve the history ledger state while removing unknown,
					// wrong-page, and stale route fields before any service read.
					browserReplaceURL(canonicalHref)
				}
				var view productui.View
				var chatModel chatui.Model
				var chatLoadErr error
				var loadErr error
				var baseline *productui.View
				if lastResolvedProductView != nil {
					previous := productBaselineForLoad(*lastResolvedProductView, state, journeys.fragment, productclient.JourneyFragment(state.Request))
					baseline = &previous
				}
				handle, scheduleErr := frontendTasks.Submit(loadCtx, taskmux.Spec{
					Key: "product:route-projection", Priority: taskmux.UserVisible, Duplicate: taskmux.ReplaceExisting,
				}, func(taskCtx context.Context) error {
					// The URL is a presentation bookmark, never an authorization or
					// data snapshot. LoadWithBaseline still rereads every authoritative
					// dataset consumed by this destination (and live preferences), while
					// retaining the already-authorized shell projection for pages that
					// do not consume that dataset.
					if baseline != nil {
						view, loadErr = productclient.LoadWithBaseline(taskCtx, liveService, session, state, *baseline)
					} else {
						view, loadErr = productclient.Load(taskCtx, liveService, session, state)
					}
					if loadErr == nil && state.Page == productui.PageChatSettings {
						loadChatRetentionPolicy(taskCtx, cfg, &view)
					}
					return nil
				})
				if scheduleErr != nil {
					return nil, scheduleErr
				}
				select {
				case <-handle.Done():
					result := handle.Result()
					if result.Err != nil {
						return nil, result.Err
					}
				case <-loadCtx.Done():
					handle.Cancel()
					return nil, loadCtx.Err()
				}
				if loadCtx.Err() != nil || currentProductHref() != canonicalHref {
					// The service is allowed to ignore cancellation. Do not let an
					// answer for an address the browser has since left adopt local
					// state, start a journey read, or enqueue a preference write.
					return nil, context.Canceled
				}
				if state.Page == productui.PageChat {
					chatModel, chatLoadErr = loadChatProjection(loadCtx)
					if chatLoadErr != nil && chatModel.Error == "" {
						chatModel.State, chatModel.Error = chatui.StateError, chatLoadErr.Error()
					}
				}
				resolvedHref := productclient.ResolvedCanonicalHref(state, view)
				view.PeopleColumnDraft = peopleColumnDraft
				view.Chat = chatModel
				view.Navigate = navigateProduct
				view.NavigateReplace = replaceProduct
				applyBrowserHistoryNavigation(&view)
				view.NavigateDebounced = navigationDebounce.Schedule
				view.CancelDebouncedNavigation = navigationDebounce.Cancel
				view.SearchDebounced = scheduleSearch
				view.CancelSearchDebounced = searchDebounce.Cancel
				preferences.Adopt(view)
				appearance.SetPersonalDensity(view.StoredPreferences.Density)
				appearance.Load(view.Appearance)
				accessibility.Load(view.Accessibility)
				groups := make(map[string]bool, len(view.NavigationGroupOpen))
				for page, open := range view.NavigationGroupOpen {
					groups[string(page)] = open
				}
				productNavigationGroups.Load(groups)
				if persistPresentation {
					preferences.PersistView(view)
				}
				if state.Page == productui.PageJourneys && state.Request.JourneyMode == "new" && state.Request.JourneyWorker != "" {
					if persistPresentation {
						preferences.RecordWorkflowUse("promotion", state.Request.JourneyWorker)
					}
				} else {
					preferences.ResetWorkflowUseMarker()
				}
				view.Appearance = appearance.Saved()
				view.PreviewTheme = appearance.Preview
				view.SaveTheme = appearance.Save
				view.ResetTheme = appearance.Reset
				view.SaveWorkerIDPolicy = func(policy productui.WorkerIDPolicy) {
					preferences.SaveWorkerIDPolicy(policy, func(err error) {
						statusNode := js.Global().Get("document").Call("getElementById", "worker-id-status")
						if statusNode.Truthy() {
							if err != nil {
								statusNode.Set("textContent", "We couldn't save these rules. Review your changes and try again.")
								return
							}
							statusNode.Set("textContent", "Worker ID rules saved for this organization.")
						}
						if err == nil {
							navigateProduct(currentPath() + "?" + currentQuery())
						}
					})
				}
				view.SaveChatRetentionPolicy = func(policy productui.ChatRetentionPolicy) {
					saveChatRetentionPolicy(cfg, policy, func() {
						navigateProduct(currentProductHref())
					})
				}
				view.SaveOrganizationVisibility = func(policy productui.OrganizationVisibilityPolicy) {
					preferences.SaveOrganizationVisibility(policy, func(err error) {
						statusNode := js.Global().Get("document").Call("getElementById", "organization-visibility-status")
						if statusNode.Truthy() {
							if err != nil {
								statusNode.Set("textContent", "We couldn't save visibility. Review your changes and try again.")
								return
							}
							statusNode.Set("textContent", "Organization visibility saved.")
						}
						if err == nil {
							navigateProduct(currentPath() + "?" + currentQuery())
						}
					})
				}
				view.SaveAccessRole = func(role productui.AccessRole) {
					preferences.SaveAccessRole(role, func(err error) {
						statusNode := js.Global().Get("document").Call("getElementById", "role-access-status")
						if statusNode.Truthy() {
							if err != nil {
								statusNode.Set("textContent", "We couldn't save this role. Review your changes and try again.")
								return
							}
							statusNode.Set("textContent", "Role created.")
						}
						if err == nil {
							navigateProduct(currentPath() + "?" + currentQuery())
						}
					})
				}
				view.SaveWorkerRoleAssignment = func(assignment productui.WorkerRoleAssignment) {
					preferences.SaveWorkerRoleAssignment(assignment, func(err error) {
						if err == nil {
							navigateProduct(currentPath() + "?" + currentQuery())
						}
					})
				}
				view.PreviewRoleVisibility = roleAccessPreviewRequest(ctx, frontendTasks, service)
				view.SaveRoleVisibility = func(policy productui.OrganizationVisibilityPolicy) {
					preferences.SaveRoleVisibility(policy, func(err error) {
						statusNode := js.Global().Get("document").Call("getElementById", "organization-visibility-status-"+policy.RoleID)
						if statusNode.Truthy() {
							if err != nil {
								statusNode.Set("textContent", "We couldn't save visibility. Review your changes and try again.")
								return
							}
							statusNode.Set("textContent", "Role visibility saved.")
						}
						if err == nil {
							navigateProduct(currentPath() + "?" + currentQuery())
						}
					})
				}
				view.SaveRolePagePermission = func(permission productui.RolePagePermission) {
					preferences.SaveRolePagePermission(permission, func(err error) {
						statusNode := js.Global().Get("document").Call("getElementById", "role-page-permission-status-"+permission.RoleID+"-"+string(permission.Page))
						if statusNode.Truthy() {
							if err != nil {
								statusNode.Set("textContent", "We couldn't save page access. Review your changes and try again.")
								return
							}
							statusNode.Set("textContent", "Page access saved.")
						}
						if err == nil {
							navigateProduct(currentPath() + "?" + currentQuery())
						}
					})
				}
				view.SaveRoleFeaturePermission = func(permission productui.RoleFeaturePermission) {
					preferences.SaveRoleFeaturePermission(permission, func(err error) {
						if err == nil {
							navigateProduct(currentPath() + "?" + currentQuery())
						}
					})
				}
				view.UpdatePeopleDirectory = func(change productui.PeopleDirectoryChange) {
					// The component applies the sort optimistically, then the normal
					// software route verifies the authoritative projection and persists
					// presentation state. That route drives the same table-scoped busy
					// contract used by paging and page-size changes.
					navigatePeopleDirectoryChange(navigateProduct, change)
				}
				view.Accessibility = accessibility.Saved()
				view.PreviewAccessibility = accessibility.Preview
				view.SaveAccessibility = accessibility.Save
				view.ResetAccessibility = accessibility.Reset
				view.SaveDensity = func(density string) {
					preferences.SaveDensity(density, func(err error) {
						if err == nil {
							appearance.SetPersonalDensity(density)
							navigateProduct(currentPath() + "?" + currentQuery())
						}
					})
				}
				if liveService.CreateWorkflowDraft != nil {
					view.CreateWorkflowDraft = func(request productui.WorkflowDraftCreateRequest) {
						setWorkflowAuthoringBusy(true, view.Locale.Text("workflow_draft.saving_new"))
						workflowAuthoring.Create(request, func(draftID string, err error) {
							if err != nil || draftID == "" {
								setWorkflowAuthoringBusy(false, view.Locale.Text("workflow_draft.create_failed"))
								return
							}
							navigateWorkflowDraft(navigateProduct, draftID)
						})
					}
				}
				if createDocument != nil && view.Page == productui.PageDocs {
					view.CreateDocument = func(request productui.DocumentCreateRequest, done func(error)) {
						go func() {
							response, err := createDocument(chatRPCContext(ctx, cfg), &documentv1.CreateDocumentRequest{Title: request.Title, Markdown: request.Markdown})
							if err == nil && (response == nil || response.GetDocumentId() == "") {
								err = errors.New("document create returned no document")
							}
							done(err)
							if err == nil {
								docsQuietRefresh()
							}
						}()
					}
				}
				if view.Page == productui.PageDocs {
					view.AddDocumentComment = func(request productui.DocumentCommentCreateRequest, done func(error)) {
						go func() {
							add := &documentv1.AddDocumentCommentRequest{DocumentId: request.DocumentID, VersionId: request.VersionID, Body: request.Body, ParentCommentId: request.ParentID}
							if request.Quote != "" {
								add.Anchor = &documentv1.CommentAnchor{Quote: request.Quote, Prefix: request.Prefix, Suffix: request.Suffix}
							}
							_, err := documentService.AddDocumentComment(chatRPCContext(ctx, cfg), add)
							done(err)
							if err == nil {
								docsQuietRefresh()
							}
						}()
					}
					view.CreateDocumentVersion = func(request productui.DocumentEditRequest, done func(error)) {
						go func() {
							response, err := documentService.CreateDocumentVersion(chatRPCContext(ctx, cfg), &documentv1.CreateDocumentVersionRequest{DocumentId: request.DocumentID, BaseVersionId: request.BaseVersionID, Title: request.Title, Markdown: request.Markdown})
							if err == nil && (response == nil || response.GetVersionId() == "") {
								err = errors.New("document version create returned no version")
							}
							done(err)
							if err == nil {
								docsQuietRefresh()
							}
						}()
					}
					view.CompareDocumentVersions = func(documentID, versionID string, done func(productui.DocumentVersionProjection, error)) {
						go func() {
							response, err := documentService.GetDocumentVersion(chatRPCContext(ctx, cfg), &documentv1.GetDocumentVersionRequest{DocumentId: documentID, VersionId: versionID})
							if err != nil {
								done(productui.DocumentVersionProjection{}, err)
								return
							}
							done(productui.DocumentVersionProjection{
								DocumentID: response.GetDocumentId(), VersionID: response.GetVersionId(),
								Title: response.GetTitle(), Markdown: response.GetMarkdown(), Readable: response.GetReadable(),
							}, nil)
						}()
					}
					view.LoadDocumentBacklinks = func(documentID string, done func([]productui.DocumentBacklink, error)) {
						go func() {
							response, err := documentService.GetDocumentBacklinks(chatRPCContext(ctx, cfg), &documentv1.GetDocumentBacklinksRequest{DocumentId: documentID})
							if err != nil {
								done(nil, err)
								return
							}
							rows := make([]productui.DocumentBacklink, 0, len(response.GetBacklinks()))
							for _, row := range response.GetBacklinks() {
								if row == nil {
									continue
								}
								rows = append(rows, productui.DocumentBacklink{
									SourceDocumentID: row.GetSourceDocumentId(), SourceVersionID: row.GetSourceVersionId(), SourceTitle: row.GetSourceTitle(),
									Label: row.GetLabel(), Block: row.GetBlockId(), State: row.GetState(),
								})
							}
							done(rows, nil)
						}()
					}
				}
				if liveService.ShareDocument != nil && view.Page == productui.PageDocs {
					if origin := js.Global().Get("location").Get("origin").String(); strings.HasPrefix(origin, "http://") || strings.HasPrefix(origin, "https://") {
						view.DocumentOrigin = origin
					}
					view.ShareDocument = func(request productui.DocumentShareRequest, done func(error)) {
						go func() {
							_, err := liveService.ShareDocument(chatRPCContext(ctx, cfg), &documentv1.ShareDocumentRequest{DocumentId: request.DocumentID, RecipientId: request.RecipientID, Role: request.Role})
							done(err)
						}()
					}
				}
				if view.Page == productui.PageDocs {
					rememberDocsListHref(&view)
					wireDocumentLibrary(&view, documentService, func(ctx context.Context) context.Context { return chatRPCContext(ctx, cfg) }, ctx)
					view.DocumentMedia = documentMediaPort(cfg)
					wireDocsReferenceSuggest(&view, documentService, cfg)
				}
				if view.WorkflowDraft != nil {
					draft := *view.WorkflowDraft
					view.SelectWorkflowDraftNode = func(nodeID string) {
						values, err := url.ParseQuery(currentQuery())
						if err != nil {
							return
						}
						nodeID = strings.TrimSpace(nodeID)
						if nodeID == "" {
							values.Del("node")
						} else {
							values.Set("node", nodeID)
						}
						browserReplaceURL(currentPath() + "?" + values.Encode())
					}
					// Every edit ends the same way: reload the authoritative
					// draft. A refused edit reloads too, because the refusal may
					// be the first sign that this view is behind the server, and
					// a control still showing the rejected choice would be lying.
					complete := func(err error) {
						if err != nil {
							message := view.Locale.Text(workflowAuthoringErrorKey(err))
							if !revalidateWorkflowDraft(productRouteRetry) {
								setWorkflowAuthoringBusy(false, message)
								return
							}
							setWorkflowAuthoringMessage(message)
							return
						}
						if !revalidateWorkflowDraft(productRouteRetry) {
							setWorkflowAuthoringBusy(false, view.Locale.Text("workflow_draft.save_failed"))
						}
					}
					begin := func() {
						setWorkflowAuthoringFlag("data-workflow-edited")
						setWorkflowAuthoringBusy(true, view.Locale.Text("workflow_draft.saving_change"))
					}
					if liveService.InsertWorkflowPaletteEntry != nil {
						view.InsertWorkflowPaletteEntry = func(entry productui.WorkflowPaletteItem) {
							begin()
							workflowAuthoring.InsertReporting(draft, entry, func(inserted []string, err error) {
								if err == nil && len(inserted) > 0 {
									selectWorkflowNodeInAddress(inserted[0])
									setWorkflowAuthoringFlag("data-workflow-name-next")
								}
								complete(err)
							})
						}
					}
					if liveService.InsertWorkflowPaletteEntry != nil && liveService.SetWorkflowDraftOutcome != nil {
						view.InsertWorkflowStepAfter = func(entry productui.WorkflowPaletteItem, from productui.WorkflowOutcomeChange) {
							begin()
							workflowAuthoring.InsertAfter(draft, entry, from, func(inserted []string, err error) {
								if len(inserted) > 0 {
									selectWorkflowNodeInAddress(inserted[0])
									setWorkflowAuthoringFlag("data-workflow-name-next")
								}
								complete(err)
							})
						}
					}
					if liveService.UpdateWorkflowDraftNode != nil {
						view.UpdateWorkflowDraftNode = func(change productui.WorkflowNodeParameterChange) {
							begin()
							workflowAuthoring.UpdateNode(draft, change, complete)
						}
					}
					if liveService.SetWorkflowDraftOutcome != nil {
						view.SetWorkflowDraftOutcome = func(change productui.WorkflowOutcomeChange) {
							begin()
							workflowAuthoring.SetOutcome(draft, change, complete)
						}
					}
					if liveService.SetWorkflowDraftOutcome != nil {
						view.SetWorkflowDraftOutcomes = func(changes []productui.WorkflowOutcomeChange) {
							begin()
							workflowAuthoring.SetOutcomes(draft, changes, complete)
						}
					}
					if liveService.ClearWorkflowDraftOutcome != nil {
						view.ClearWorkflowDraftOutcome = func(change productui.WorkflowOutcomeChange) {
							begin()
							workflowAuthoring.ClearOutcome(draft, change, complete)
						}
					}
					if liveService.BindWorkflowDraftInput != nil {
						view.BindWorkflowDraftInput = func(change productui.WorkflowInputBindingChange) {
							begin()
							workflowAuthoring.BindInput(draft, change, complete)
						}
					}
					if liveService.RemoveWorkflowDraftNode != nil {
						view.RemoveWorkflowDraftNode = func(nodeID string) {
							begin()
							workflowAuthoring.RemoveNode(draft, nodeID, complete)
						}
					}
					if liveService.RenameWorkflowDraft != nil {
						view.RenameWorkflowDraft = func(name string) {
							begin()
							workflowAuthoring.Rename(draft, name, complete)
						}
					}
					if liveService.NavigateWorkflowDraftHistory != nil {
						view.NavigateWorkflowDraftHistory = func(direction string) {
							begin()
							workflowAuthoring.NavigateHistory(draft, direction, complete)
						}
					}
					if liveService.ApplyWorkflowTemplateOverlay != nil {
						view.ApplyWorkflowOverlay = func(change productui.WorkflowTemplateOverlayChange) {
							begin()
							workflowAuthoring.ApplyOverlay(draft, change, complete)
						}
					}
				}
				attrs := router.Attrs{productViewKey: view, productSourceHrefKey: canonicalHref, productResolvedHrefKey: resolvedHref}
				if state.Page == productui.PageJourneys {
					attrs[productJourneyStoreKey] = journeyStore
				}
				if loadErr != nil {
					// REV-090-01: a covered region fails through the async-region
					// contract, in the shape it loads in; other pages keep the
					// page-level LoadError degradation.
					failedView, regionFailed := productRouteFailure(view, state.Page, loadErr)
					view = failedView
					attrs[productViewKey] = view
					if regionFailed {
						attrs[productFailureKey] = true
					}
				}
				// Publish the stable shell projection at the loader completion
				// boundary. Waiting for the leaf component to render leaves a small
				// interval in which a fast follow-up navigation can incorrectly look
				// like a cold boot and replace global controls with placeholders.
				if loadCtx.Err() != nil {
					return nil, loadCtx.Err()
				}
				if state.Page == productui.PageJourneys {
					journeys.Load(productclient.JourneyFragment(state.Request), view.Locale.Resolved)
				} else {
					journeys.Leave()
				}
				resolved := view
				resolved.Loading = false
				resolved.ContentLoading = false
				resolved.Refreshing = false
				resolved.RefreshingRegion = ""
				lastResolvedProductView = &resolved
				lastProductRouteFailed = loadErr != nil
				if loadErr == nil {
					productViews.Put(canonicalHref, resolved)
				}
				setActiveProductLayout(resolved, state.Page != productui.PageJourneys && !productui.PageOwnsHeading(state.Page))
				if shouldSettleWorkflowAuthoringBusy(state.Page, loadErr) {
					// An idle editor is a saved one and says so; a refusal held
					// over from the edit that caused this reload takes its place.
					message := takeWorkflowAuthoringMessage()
					edited := takeWorkflowAuthoringFlag("data-workflow-edited")
					if message == "" && edited && view.WorkflowDraft != nil {
						message = view.Locale.Text("workflow_editor.saved_all")
					}
					setWorkflowAuthoringBusy(false, message)
					if takeWorkflowAuthoringFlag("data-workflow-name-next") {
						focusWorkflowStepName()
					}
				}
				return attrs, nil
			},
			Loading: func(_ router.Attrs) *router.Element {
				state, stateErr := productclient.ParseState(currentPath(), currentQuery())
				if stateErr != nil {
					state = productclient.State{Page: definition.ID, Request: productui.PageRequest{Page: definition.ID}}
				}
				view := productclient.LoadingView(session, state)
				// A failed read is not a resolved projection to refresh in place:
				// retrying it shows the loading shape, not the empty page.
				warmRefresh := lastResolvedProductView != nil && !lastProductRouteFailed && keepResolvedProductViewDuringLoad(*lastResolvedProductView, state, journeys.fragment, productclient.JourneyFragment(state.Request))
				var warmView productui.View
				if warmRefresh {
					warmView = *lastResolvedProductView
				} else if cached, ok := productViews.Get(productclient.CanonicalHref(state)); ok && lastResolvedProductView != nil {
					// Returning to a page already seen (Back, Forward, the header
					// arrows, a link back to it) paints it as it was while the
					// loader re-reads, instead of flashing a loading proxy.
					warmRefresh, warmView = true, cached
				}
				// REV-091-03: a live-update revalidation keeps the page exactly
				// as it is while the loader re-reads.
				quietRefresh := consumeProductQuietRefresh()
				contentTransition := lastResolvedProductView != nil && !warmRefresh
				if warmRefresh {
					// Same-page network effects retain the last authorized projection.
					// This avoids a skeleton flash for fast filters, sorts and paging;
					// the resolved response still replaces the tree atomically.
					view = warmView
					view.Refreshing = true
					currentRoute := currentPath() + "?" + currentQuery()
					if peopleDirectoryOnlyRouteChange(lastFocusedProductRoute, currentRoute) {
						// Sorting, filtering and paging affect only the directory surface.
						// Keep the shell and page heading mounted and scope busy/progress
						// semantics to the table-plus-pagination component.
						view.RefreshingRegion = productui.RefreshRegionPeopleDirectory
					} else if docsLibraryOnlyRouteChange(lastFocusedProductRoute, currentRoute) {
						view.RefreshingRegion = productui.RefreshRegionDocuments
					}
				} else if contentTransition {
					// Cross-page navigation reuses the authorized shell and only swaps
					// the main region for a destination-shaped loading proxy. The
					// reconciler can therefore preserve header/sidebar DOM and state.
					view = productclient.ContentLoadingView(*lastResolvedProductView, state)
				}
				view.Navigate = navigateProduct
				view.NavigateReplace = replaceProduct
				applyBrowserHistoryNavigation(&view)
				view.NavigateDebounced = navigationDebounce.Schedule
				view.CancelDebouncedNavigation = navigationDebounce.Cancel
				view.SearchDebounced = scheduleSearch
				view.CancelSearchDebounced = searchDebounce.Cancel
				view.Appearance = appearance.Saved()
				view.Accessibility = accessibility.Saved()
				if productNavigationGroups != nil {
					view.NavigationGroupOpen = make(map[productui.PageID]bool)
					for page, open := range productNavigationGroups.State() {
						view.NavigationGroupOpen[productui.PageID(page)] = open
					}
				}
				applyThemeDocumentIdentity(productui.ResolveDocumentPageTitle(view), view.Appearance, view.Tenant)
				applyLocaleDocumentIdentity(view.Locale)
				showHeading := state.Page != productui.PageJourneys && !productui.PageOwnsHeading(state.Page)
				if warmRefresh {
					view.Loading = false
					view.ContentLoading = false
					view.Refreshing = view.RefreshingRegion == "" && !quietRefresh
					setActiveProductLayout(view, showHeading)
					if state.Page == productui.PageJourneys {
						content := journey.LiveContentComponent(journeyStore)
						content.Props["key"] = productclient.CanonicalHref(state)
						return content
					}
					return warmProductRouteContent(view)
				}
				if contentTransition {
					view.Loading = false
					view.ContentLoading = true
					view.Refreshing = false
					setActiveProductLayout(view, showHeading)
					return productui.LoadingProxy(productui.LoadingProxyProps{Page: view.Page})
				}
				view.Loading = true
				view.ContentLoading = false
				view.Refreshing = false
				setActiveProductLayout(view, showHeading)
				return productui.LoadingProxy(productui.LoadingProxyProps{Page: view.Page})
			},
		})
	}
	if err := hydrateProductRouter(productRouter); err != nil {
		return err
	}
	// The shell may fall back to a separate startup mount when hydration
	// fails. Bind document-level review listeners only after the live product
	// tree is committed, so that fallback cannot leave a stale store behind.
	bindConfirmationDialogs(journeyStore)
	startProductInvalidations(ctx, cfg, service, canViewPage, journeyApp)
	bindNoteComposerFocus(journeyStore)
	bindUnsavedFormGuard()
	return nil
}

func navigateWorkflowDraft(navigate func(string), draftID string) {
	values, err := url.ParseQuery(currentQuery())
	if err != nil {
		values = make(url.Values)
	}
	values.Set("draft", draftID)
	values.Del("workflow")
	values.Del("run")
	if navigate != nil {
		navigate(currentPath() + "?" + values.Encode())
	}
}

// setWorkflowAuthoringBusy announces an edit in flight and fences the
// editor while it is. The fence is one attribute on the page root, which the
// stylesheet turns into controls the pointer cannot reach; it used to set
// disabled on two button classes by hand, which left every inspector control
// live during a save and fought the renderer for ownership of the attribute.
// Focus is deliberately left where it is, so a keyboard author is still on
// the control they changed when the reloaded draft arrives.
func setWorkflowAuthoringBusy(busy bool, message string) {
	document := js.Global().Get("document")
	statusNode := document.Call("getElementById", "workflow-designer-status")
	if statusNode.Truthy() {
		statusNode.Set("textContent", message)
	}
	page := document.Call("querySelector", ".workflow-designer-page")
	if !page.Truthy() {
		return
	}
	if busy {
		page.Call("setAttribute", "aria-busy", "true")
		return
	}
	page.Call("removeAttribute", "aria-busy")
}

// setWorkflowAuthoringMessage holds a message for the reload that follows a
// refused edit, so the explanation survives the re-render that clears the
// busy state.
func setWorkflowAuthoringMessage(message string) {
	js.Global().Get("document").Get("documentElement").Call("setAttribute", "data-workflow-message", message)
}

// setWorkflowAuthoringFlag and takeWorkflowAuthoringFlag carry a one-shot fact
// across the reload that follows an edit: that something was saved, or that
// the step just added should have its name ready to type over.
func setWorkflowAuthoringFlag(name string) {
	js.Global().Get("document").Get("documentElement").Call("setAttribute", name, "1")
}

func takeWorkflowAuthoringFlag(name string) bool {
	root := js.Global().Get("document").Get("documentElement")
	set := root.Call("hasAttribute", name).Bool()
	root.Call("removeAttribute", name)
	return set
}

// focusWorkflowStepName puts the caret in the new step's name with the
// generated name selected, so the first thing an author types replaces
// "Task 1" with what the step is for.
func focusWorkflowStepName() {
	window := js.Global()
	var focus js.Func
	focus = js.FuncOf(func(js.Value, []js.Value) any {
		focus.Release()
		field := window.Get("document").Call("getElementById", "workflow-inspector-name")
		if field.Truthy() {
			options := map[string]any{"preventScroll": true}
			field.Call("focus", options)
			field.Call("select")
		}
		return nil
	})
	window.Call("requestAnimationFrame", focus)
}

func takeWorkflowAuthoringMessage() string {
	root := js.Global().Get("document").Get("documentElement")
	message := root.Call("getAttribute", "data-workflow-message")
	root.Call("removeAttribute", "data-workflow-message")
	if message.IsNull() || message.IsUndefined() {
		return ""
	}
	return message.String()
}

// selectWorkflowNodeInAddress makes a step the address's selection without
// navigating. The reload that follows an edit reads it back, which is how a
// step the author just added arrives already selected.
func selectWorkflowNodeInAddress(nodeID string) {
	values, err := url.ParseQuery(currentQuery())
	if err != nil || strings.TrimSpace(nodeID) == "" {
		return
	}
	values.Set("node", strings.TrimSpace(nodeID))
	browserReplaceURL(currentPath() + "?" + values.Encode())
}

// hydrateProductRouter resumes the server-rendered loading shell before the
// router owns future updates. The first hydration commit must complete before
// a loader answer is allowed to schedule a normal render into the same root.
func hydrateProductRouter(productRouter *router.Router) error {
	initial := productRouter.Current()
	if initial == nil {
		return fmt.Errorf("product router did not resolve the initial location")
	}
	// Hydration commits asynchronously. Wait for the framework's completion
	// signal rather than guessing a frame delay before enabling router writes.
	committed := make(chan struct{}, 1)
	options := ui.HydrationOptions{Observability: ui.SSRObservabilityOptions{
		OnEvent: func(event ui.SSRObservation) {
			if event.Hydration != nil {
				select {
				case committed <- struct{}{}:
				default:
				}
			}
		},
	}}
	if _, err := ui.Hydrate(initial, rootSelector, options); err != nil {
		return fmt.Errorf("hydrate product shell: %w", err)
	}
	<-committed
	productRouter.Mount(rootSelector)
	return nil
}

func browserRouteGenerationMatches(path, encodedQuery string) bool {
	if currentPath() != path {
		return false
	}
	query, err := url.ParseQuery(currentQuery())
	return err == nil && query.Encode() == encodedQuery
}

func currentProductHref() string {
	href := currentPath()
	if query := currentQuery(); query != "" {
		href += "?" + query
	}
	return href
}

func canonicalizeCurrentProductLocation() {
	path := currentPath()
	state, err := productclient.ParseState(path, currentQuery())
	if err != nil {
		if _, known := productui.LookupRoute(path); known {
			browserReplaceURL(path)
		} else {
			browserReplaceURL(productui.Path(productui.PageHome))
		}
		return
	}
	canonical := productclient.CanonicalHref(state)
	current := currentProductHref()
	if canonical != current {
		browserReplaceURL(canonical)
	}
}

type browserDebounceTimer struct {
	id       js.Value
	callback js.Func
	active   bool
}

func browserDebounceScheduler(delay time.Duration, fire func()) debounceTimer {
	timer := &browserDebounceTimer{active: true}
	timer.callback = js.FuncOf(func(js.Value, []js.Value) any {
		if !timer.active {
			return nil
		}
		timer.active = false
		fire()
		timer.callback.Release()
		return nil
	})
	timer.id = js.Global().Call("setTimeout", timer.callback, delay.Milliseconds())
	return timer
}

func browserReplaceURL(href string) {
	history, historyOK := browserHistory()
	if !historyOK {
		return
	}
	state, stateOK := browserHistoryState(history)
	if !stateOK {
		return
	}
	browserCall(history, "replaceState", state, "", href)
}

func (t *browserDebounceTimer) Stop() bool {
	if t == nil || !t.active {
		return false
	}
	t.active = false
	js.Global().Call("clearTimeout", t.id)
	t.callback.Release()
	return true
}

func productRouteComponent(_ router.Attrs) *router.Element {
	return ui.CreateElement(renderProductRoute, router.UseRouteData())
}

// A chat click revalidates this route. Its loading and resolved answers must
// enter through the same component boundary, or the reconciler unmounts the
// entire workspace (including every image and focused control) on each click.
func warmProductRouteContent(view productui.View) *router.Element {
	// Chat and Docs hold local state across refreshes (drafts, open dialogs,
	// selections), so their warm render goes through the same component as
	// the resolved route; a different element type would remount the page.
	if view.Page == productui.PageChat || view.Page == productui.PageDocs {
		return ui.CreateElement(renderProductRoute, router.Attrs{productViewKey: view})
	}
	return productui.BuildPageContent(view)
}

func renderProductRoute(data router.Attrs) ui.Node {
	view, ok := data[productViewKey].(productui.View)
	if !ok {
		view = productui.NewView(productui.PageHome, "", "", "")
		view.LoadError = "The requested view did not produce route data."
		view.Navigate = router.Navigate
	}
	if sourceHref, sourceOK := data[productSourceHrefKey].(string); sourceOK && currentProductHref() == sourceHref {
		if resolvedHref, resolvedOK := data[productResolvedHrefKey].(string); resolvedOK && resolvedHref != "" && resolvedHref != sourceHref {
			// GWC must commit the loader result under its original loader key
			// before the address changes. Replacing from the resolved component
			// prevents GWC from correctly suppressing its own answer as stale.
			browserReplaceURL(resolvedHref)
		}
	}
	if productNavigationGroups != nil {
		view.NavigationGroupOpen = make(map[productui.PageID]bool)
		for page, open := range productNavigationGroups.State() {
			view.NavigationGroupOpen[productui.PageID(page)] = open
		}
	}
	applyThemeDocumentIdentity(productui.ResolveDocumentPageTitle(view), view.Appearance, view.Tenant)
	applyLocaleDocumentIdentity(view.Locale)
	focusProductRouteAfterNavigation()
	resolved := view
	resolved.Loading = false
	resolved.ContentLoading = false
	resolved.Refreshing = false
	lastResolvedProductView = &resolved
	showHeading := view.Page != productui.PageJourneys && !productui.PageOwnsHeading(view.Page)
	setActiveProductLayout(view, showHeading)
	var result *router.Element
	if view.Page == productui.PageJourneys {
		if store, storeOK := data[productJourneyStoreKey].(*journey.Store); storeOK {
			result = journey.LiveContentComponent(store)
			// A promotion form, detail, and list are distinct route-owned
			// subscriptions even though they share one long-lived store. Do not
			// reuse the prior route's fiber while its async render is in flight.
			if sourceHref, sourceOK := data[productSourceHrefKey].(string); sourceOK {
				result.Props["key"] = sourceHref
			}
		}
	}
	if result == nil {
		failed, _ := data[productFailureKey].(bool)
		result = productRouteContent(view, failed)
	}
	return result
}

func setActiveProductLayout(view productui.View, showHeading bool) {
	activeProductLayoutView = view
	activeProductLayoutShowHeading = showHeading
}

func productShellLayoutComponent(_ router.Attrs) *router.Element {
	// Route factories run before the reconciler enters a component context.
	// Keep the shell itself behind a component boundary so its software links
	// can safely use GWC event hooks in js/wasm builds. The router's outlet
	// context only exists during this factory call, not the later render.
	outlet := router.GetOutlet()
	if outlet == nil {
		outlet = productui.LoadingProxy(productui.LoadingProxyProps{Page: activeProductLayoutView.Page})
	}
	return ui.CreateElement(renderProductShellLayoutOnce, &productShellLayoutProps{Outlet: outlet, View: activeProductLayoutView, ShowHeading: activeProductLayoutShowHeading})
}

// renderProductShellLayoutOnce takes the shell's props by pointer. GWC
// compares a component's props before re-rendering it, and the value struct
// (a View full of funcs) never compared equal, so every state update
// anywhere in the app -- a chat repaint, a list keystroke -- re-rendered the
// whole shell: 4 to 9 times per chat room switch (Agent P render counts).
// The router re-uses this element until it runs the factory again, so the
// pointer is equal exactly when the props are the ones already rendered;
// the shell's own history state still re-renders it.
func renderProductShellLayoutOnce(props *productShellLayoutProps) ui.Node {
	return renderProductShellLayout(*props)
}

type productShellLayoutProps struct {
	Outlet      ui.Node
	View        productui.View
	ShowHeading bool
}

// productViews paints revisited addresses while their route reloads.
var productViews = newProductViewCache(24, 5*time.Minute, time.Now)

// productHistoryMounted is set once the shell has hydrated; before that the
// arrows must render exactly as the server did.
var productHistoryMounted bool

func renderProductShellLayout(props productShellLayoutProps) ui.Node {
	historyControlsTick := ui.UseState(0)
	// The arrows this shell last rendered. A same-route push (every chat room
	// switch) or a search-as-you-type replace asks for a refresh, but the
	// arrows rarely change; re-rendering the whole shell and its outlet for
	// nothing cost ~175 ms per chat room switch (Agent P CPU profile).
	renderedArrows := ui.UseRef(productHistoryArrows{})
	// The server renders the history arrows disabled (it cannot see the
	// browser's history). Hydration trusts the server markup to match the
	// first client render, so that render must also be disabled; the arrows
	// take their real state on the render after mount. Otherwise a reload
	// onto an entry with history leaves them stuck disabled. A state update
	// made while hydrating is dropped, so the flag is a package variable and
	// the refresh that applies it runs on the next task.
	ui.UseEffectOf(func() func() {
		productHistoryControlsRefresh = func() {
			if productHistory != nil && productHistoryMounted && productHistoryArrowsOf(productHistory.Props(productui.LocaleContext{})) == renderedArrows.Get() {
				return
			}
			historyControlsTick.Update(func(tick int) int { return tick + 1 })
		}
		// Browser back/forward (and the header arrows, which call
		// history.go) land here; recompute the arrows for the entry reached.
		popstate := js.FuncOf(func(js.Value, []js.Value) any {
			refreshProductHistoryControls()
			return nil
		})
		js.Global().Call("addEventListener", "popstate", popstate)
		productHistoryMounted = true
		var afterHydration js.Func
		afterHydration = js.FuncOf(func(js.Value, []js.Value) any {
			afterHydration.Release()
			refreshProductHistoryControls()
			return nil
		})
		js.Global().Call("setTimeout", afterHydration, 0)
		return func() {
			js.Global().Call("removeEventListener", "popstate", popstate)
			popstate.Release()
			productHistoryControlsRefresh = nil
		}
	}, struct{}{})
	view := props.View
	if productHistory != nil && productHistoryMounted {
		view.HistoryNavigation = productHistory.Props(view.Locale)
	}
	if view.Page == "" {
		view = productui.NewView(productui.PageHome, "", "", "")
		view.Loading = true
	}
	// Record the arrows only once this render has committed: a render that
	// is superseded before commit (a route change mid-navigation) must not
	// make a later refresh believe the header already shows the new state.
	arrows := productHistoryArrowsOf(view.HistoryNavigation)
	ui.UseLayoutEffect(func() func() {
		renderedArrows.Set(arrows)
		return nil
	})
	// A page that owns its heading never gets the shell's, not even on the
	// very first client render before a route has resolved: the default
	// layout flag is true, and a chat surface must not flash the document
	// heading and subtitle it was designed without.
	return productui.BuildShell(view, props.Outlet, props.ShowHeading && !productui.PageOwnsHeading(view.Page))
}

// focusProductRouteAfterNavigation restores the missing browser behavior of
// an SPA route change. Initial page load keeps the browser's natural focus
// order (including the skip link); later routes focus their page heading.
func focusProductRouteAfterNavigation() {
	route := currentPath() + "?" + currentQuery()
	if lastFocusedProductRoute == "" {
		lastFocusedProductRoute = route
		productScroll.Settle(route)
		return
	}
	if route == lastFocusedProductRoute {
		if productRetryFocusPending {
			productRetryFocusPending = false
			focusProductSelectorNextFrame(productRetryFocusSelector)
		}
		return
	}
	productRetryFocusPending = false
	previousRoute := lastFocusedProductRoute
	lastFocusedProductRoute = route
	if navigationCollapsedRouteChange(previousRoute, route) {
		resetCollapsedNavigationScroll()
	}
	// UXLIVE-028: the history router owns the main region's position through
	// one policy keyed by history entry and canonical resource identity. A
	// controller-less embedding (tests, the standalone journey page) keeps
	// the plain destination rule.
	scrollAction, scrollTop := productScrollKeep, 0.0
	if productScroll != nil {
		scrollAction, scrollTop = productScroll.Settle(route)
	} else if productRouteShouldResetMainScroll(previousRoute, route) {
		scrollAction = productScrollTop
	}
	selector, caretAtEnd := productRouteFocusTarget(previousRoute, route)
	if selector == "" && scrollAction == productScrollKeep && productScroll == nil {
		return
	}
	var callback js.Func
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		defer callback.Release()
		document := js.Global().Get("document")
		applyProductScroll(scrollAction, scrollTop)
		if selector == "" {
			return nil
		}
		target := document.Call("querySelector", selector)
		if target.Truthy() {
			if !caretAtEnd && !target.Call("hasAttribute", "tabindex").Bool() {
				target.Call("setAttribute", "tabindex", "-1")
			}
			target.Call("focus", map[string]any{"preventScroll": true})
			if caretAtEnd {
				length := target.Get("value").Get("length").Int()
				target.Call("setSelectionRange", length, length)
			} else {
				keepRouteFocus(selector, 0)
			}
		}
		return nil
	})
	js.Global().Call("requestAnimationFrame", callback)
}

// keepRouteFocus re-applies route focus for about a second and a half if a
// late commit replaced the focused heading and dropped focus to <body> (a
// document's heading arrives with its data, well after the route commits).
// It never takes focus from an element the reader has since moved to.
func keepRouteFocus(selector string, frame int) {
	if frame >= 90 {
		return
	}
	var callback js.Func
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		defer callback.Release()
		document := js.Global().Get("document")
		active := document.Get("activeElement")
		if active.Truthy() && !active.Equal(document.Get("body")) {
			keepRouteFocus(selector, frame+1)
			return nil
		}
		if target := document.Call("querySelector", selector); target.Truthy() {
			target.Call("focus", map[string]any{"preventScroll": true})
		}
		keepRouteFocus(selector, frame+1)
		return nil
	})
	js.Global().Call("requestAnimationFrame", callback)
}

// resetCollapsedNavigationScroll prevents an expanded menu's independent
// scroll position from stranding the compact icon rail halfway down the list.
func resetCollapsedNavigationScroll() {
	var callback js.Func
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		defer callback.Release()
		navigation := js.Global().Get("document").Call("querySelector", ".primary-nav")
		if navigation.Truthy() {
			navigation.Set("scrollTop", 0)
		}
		return nil
	})
	js.Global().Call("requestAnimationFrame", callback)
}

type productJourneyBridge struct {
	ctx      context.Context
	app      *journeyclient.App
	started  bool
	fragment string
}

func (b *productJourneyBridge) Load(fragment, locale string) {
	if b == nil || b.app == nil || fragment == b.fragment && b.started {
		if b != nil && b.app != nil {
			b.app.SetLocale(locale)
		}
		return
	}
	b.app.SetLocale(locale)
	b.fragment = fragment
	if !b.started {
		b.started = true
		b.app.Start(b.ctx, fragment)
		return
	}
	b.app.OnHashChange(fragment)
}

func (b *productJourneyBridge) Leave() {
	if b == nil || b.app == nil || b.fragment == "" {
		return
	}
	b.fragment = ""
	b.app.Suspend()
}

func currentPath() string {
	location, locationOK := browserProperty(js.Global(), "location")
	if !locationOK {
		return ""
	}
	path, pathOK := browserProperty(location, "pathname")
	if !pathOK || path.Type() != js.TypeString {
		return ""
	}
	return path.String()
}

func currentQuery() string {
	location, locationOK := browserProperty(js.Global(), "location")
	if !locationOK {
		return ""
	}
	search, searchOK := browserProperty(location, "search")
	if !searchOK || search.Type() != js.TypeString {
		return ""
	}
	return strings.TrimPrefix(search.String(), "?")
}

// focusProductSelectorNextFrame focuses the first match of selector after
// the pending commit, without scrolling the main region.
func focusProductSelectorNextFrame(selector string) {
	var callback js.Func
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		defer callback.Release()
		target := js.Global().Get("document").Call("querySelector", selector)
		if target.Truthy() {
			if !target.Call("hasAttribute", "tabindex").Bool() && target.Get("tagName").String() != "BUTTON" {
				target.Call("setAttribute", "tabindex", "-1")
			}
			target.Call("focus", map[string]any{"preventScroll": true})
		}
		return nil
	})
	js.Global().Call("requestAnimationFrame", callback)
}
