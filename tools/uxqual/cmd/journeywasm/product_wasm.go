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
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/taskmux"
)

const productPathPrefix = "/workspace/app/"

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

func startProduct(ctx context.Context, cfg journeyclient.Config, service journeyclient.Service) error {
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
	if preferenceService, ok := service.(journeyclient.PreferenceService); ok {
		liveService.GetPreferences = preferenceService.GetProductPreferences
		liveService.GetWorkerIDPolicy = preferenceService.GetWorkerIDPolicy
		liveService.GetRoleAccess = preferenceService.GetRoleAccess
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
	navigateProduct := func(href string) {
		productScroll.BeginSoftwareNavigation()
		productHistory.Navigate(productRouter.Navigate, href)
	}
	// Menu filtering is local component state. Debounce only its shareable URL
	// state so typing never reruns page loaders or refetches workforce data.
	navigationDebounce := newNavigationDebouncerWithScheduler(browserReplaceURL, browserDebounceScheduler)
	journeyStore := journey.NewStore(journey.Page{})
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
				resolvedHref := productclient.ResolvedCanonicalHref(state, view)
				view.Navigate = navigateProduct
				applyBrowserHistoryNavigation(&view)
				view.NavigateDebounced = navigationDebounce.Schedule
				view.CancelDebouncedNavigation = navigationDebounce.Cancel
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
				if liveService.InsertWorkflowPaletteEntry != nil && view.WorkflowDraft != nil {
					draft := *view.WorkflowDraft
					view.InsertWorkflowPaletteEntry = func(entry productui.WorkflowPaletteItem) {
						setWorkflowAuthoringBusy(true, view.Locale.Text("workflow_draft.saving_change"))
						workflowAuthoring.Insert(draft, entry, func(err error) {
							if err != nil {
								setWorkflowAuthoringBusy(false, view.Locale.Text("workflow_draft.save_failed"))
								return
							}
							if !revalidateWorkflowDraft(productRouteRetry) {
								setWorkflowAuthoringBusy(false, view.Locale.Text("workflow_draft.save_failed"))
							}
						})
					}
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
					complete := func(err error) {
						if err != nil {
							setWorkflowAuthoringBusy(false, view.Locale.Text("workflow_draft.save_failed"))
							return
						}
						if !revalidateWorkflowDraft(productRouteRetry) {
							setWorkflowAuthoringBusy(false, view.Locale.Text("workflow_draft.save_failed"))
						}
					}
					if liveService.UpdateWorkflowDraftNode != nil {
						view.UpdateWorkflowDraftNode = func(change productui.WorkflowNodeParameterChange) {
							setWorkflowAuthoringBusy(true, view.Locale.Text("workflow_draft.saving_change"))
							workflowAuthoring.UpdateNode(draft, change, complete)
						}
					}
					if liveService.SetWorkflowDraftOutcome != nil {
						view.SetWorkflowDraftOutcome = func(change productui.WorkflowOutcomeChange) {
							setWorkflowAuthoringBusy(true, view.Locale.Text("workflow_draft.saving_change"))
							workflowAuthoring.SetOutcome(draft, change, complete)
						}
					}
					if liveService.BindWorkflowDraftInput != nil {
						view.BindWorkflowDraftInput = func(change productui.WorkflowInputBindingChange) {
							setWorkflowAuthoringBusy(true, view.Locale.Text("workflow_draft.saving_change"))
							workflowAuthoring.BindInput(draft, change, complete)
						}
					}
					if liveService.MoveWorkflowDraftNode != nil {
						view.MoveWorkflowDraftNode = func(change productui.WorkflowNodeMove) {
							setWorkflowAuthoringBusy(true, view.Locale.Text("workflow_draft.saving_change"))
							workflowAuthoring.MoveNode(draft, change, complete)
						}
					}
					if liveService.NavigateWorkflowDraftHistory != nil {
						view.NavigateWorkflowDraftHistory = func(direction string) {
							setWorkflowAuthoringBusy(true, view.Locale.Text("workflow_draft.saving_change"))
							workflowAuthoring.NavigateHistory(draft, direction, complete)
						}
					}
					if liveService.ApplyWorkflowTemplateOverlay != nil {
						view.ApplyWorkflowOverlay = func(change productui.WorkflowTemplateOverlayChange) {
							setWorkflowAuthoringBusy(true, view.Locale.Text("workflow_draft.saving_change"))
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
				setActiveProductLayout(resolved, state.Page != productui.PageJourneys)
				if shouldSettleWorkflowAuthoringBusy(state.Page, loadErr) {
					setWorkflowAuthoringBusy(false, "")
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
				// REV-091-03: a live-update revalidation keeps the page exactly
				// as it is while the loader re-reads.
				quietRefresh := consumeProductQuietRefresh()
				contentTransition := lastResolvedProductView != nil && !warmRefresh
				if warmRefresh {
					// Same-page network effects retain the last authorized projection.
					// This avoids a skeleton flash for fast filters, sorts and paging;
					// the resolved response still replaces the tree atomically.
					view = *lastResolvedProductView
					view.Refreshing = true
					currentRoute := currentPath() + "?" + currentQuery()
					if peopleDirectoryOnlyRouteChange(lastFocusedProductRoute, currentRoute) {
						// Sorting, filtering and paging affect only the directory surface.
						// Keep the shell and page heading mounted and scope busy/progress
						// semantics to the table-plus-pagination component.
						view.RefreshingRegion = productui.RefreshRegionPeopleDirectory
					}
				} else if contentTransition {
					// Cross-page navigation reuses the authorized shell and only swaps
					// the main region for a destination-shaped loading proxy. The
					// reconciler can therefore preserve header/sidebar DOM and state.
					view = productclient.ContentLoadingView(*lastResolvedProductView, state)
				}
				view.Navigate = navigateProduct
				applyBrowserHistoryNavigation(&view)
				view.NavigateDebounced = navigationDebounce.Schedule
				view.CancelDebouncedNavigation = navigationDebounce.Cancel
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
				showHeading := state.Page != productui.PageJourneys
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
					return productui.BuildPageContent(view)
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

func setWorkflowAuthoringBusy(busy bool, message string) {
	document := js.Global().Get("document")
	statusNode := document.Call("getElementById", "workflow-designer-status")
	if statusNode.Truthy() {
		statusNode.Set("textContent", message)
	}
	page := document.Call("querySelector", ".workflow-designer-page")
	if page.Truthy() {
		if busy {
			page.Call("setAttribute", "aria-busy", "true")
		} else {
			page.Call("removeAttribute", "aria-busy")
		}
	}
	selector := `.workflow-draft-create:not(:disabled),.workflow-palette-insert:not(:disabled)`
	if !busy {
		selector = `[data-authoring-busy-disabled="true"]`
	}
	buttons := document.Call("querySelectorAll", selector)
	for index := 0; index < buttons.Get("length").Int(); index++ {
		button := buttons.Index(index)
		if busy {
			button.Call("setAttribute", "data-authoring-busy-disabled", "true")
			button.Set("disabled", true)
		} else {
			button.Call("removeAttribute", "data-authoring-busy-disabled")
			button.Set("disabled", false)
		}
	}
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
	showHeading := view.Page != productui.PageJourneys
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
	return ui.CreateElement(renderProductShellLayout, productShellLayoutProps{Outlet: outlet, View: activeProductLayoutView, ShowHeading: activeProductLayoutShowHeading})
}

type productShellLayoutProps struct {
	Outlet      ui.Node
	View        productui.View
	ShowHeading bool
}

func renderProductShellLayout(props productShellLayoutProps) ui.Node {
	view := props.View
	if view.Page == "" {
		view = productui.NewView(productui.PageHome, "", "", "")
		view.Loading = true
	}
	return productui.BuildShell(view, props.Outlet, props.ShowHeading)
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

// keepRouteFocus re-applies route focus for a few frames if a late commit
// replaced the focused heading and dropped focus to <body>. It never takes
// focus from an element the reader has since moved to.
func keepRouteFocus(selector string, frame int) {
	if frame >= 6 {
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
